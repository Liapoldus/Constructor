package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type DeploymentService struct {
	repository domain.DeploymentRepository
	gateway    domain.GatewayClient
	delivery   domain.DeliveryRepository
	locksMu    sync.Mutex
	locks      map[string]chan struct{}
}

var deploymentSiteIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
var deploymentEnvironmentIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
var deploymentIdempotencyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{15,127}$`)

func NewDeploymentService(repository domain.DeploymentRepository, gateway domain.GatewayClient, delivery domain.DeliveryRepository) *DeploymentService {
	return &DeploymentService{repository: repository, gateway: gateway, delivery: delivery, locks: map[string]chan struct{}{}}
}

func (s *DeploymentService) lockTarget(ctx context.Context, target string) (func(), error) {
	s.locksMu.Lock()
	lock, exists := s.locks[target]
	if !exists {
		lock = make(chan struct{}, 1)
		lock <- struct{}{}
		s.locks[target] = lock
	}
	s.locksMu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-lock:
		return func() { lock <- struct{}{} }, nil
	}
}
func (s *DeploymentService) SaveSite(site domain.Site) error { return s.repository.SaveSite(site) }
func (s *DeploymentService) SaveEnvironment(environment domain.Environment) error {
	return s.repository.SaveEnvironment(environment)
}
func (s *DeploymentService) Site(id string) (domain.Site, error) { return s.repository.Site(id) }
func (s *DeploymentService) Environment(id string) (domain.Environment, error) {
	return s.repository.Environment(id)
}
func (s *DeploymentService) Deployments() ([]domain.Deployment, error) {
	lister, ok := s.repository.(domain.DeploymentLister)
	if !ok {
		return nil, domain.ErrUnsupported
	}
	return lister.Deployments()
}

// Recover replays in-flight Gateway operations with their original idempotency
// keys and compare-and-swap revisions. Unconfirmed outcomes remain applying so
// the target stays reserved until Gateway confirms the operation result.
func (s *DeploymentService) Recover(ctx context.Context) error {
	lister, ok := s.repository.(domain.DeploymentLister)
	if !ok {
		return domain.ErrUnsupported
	}
	values, err := lister.Deployments()
	if err != nil {
		return err
	}
	var failures []error
	for _, candidate := range values {
		if candidate.Status != domain.DeploymentPending && candidate.Status != domain.DeploymentApplying {
			continue
		}
		target := candidate.SiteID + "/" + candidate.EnvironmentID
		release, err := s.lockTarget(ctx, target)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		value, err := s.repository.Deployment(candidate.ID)
		if err != nil {
			release()
			failures = append(failures, err)
			continue
		}
		if value.Status != domain.DeploymentPending && value.Status != domain.DeploymentApplying {
			release()
			continue
		}
		if value.Status == domain.DeploymentPending {
			value.Status = domain.DeploymentApplying
			if err := s.repository.SaveDeployment(value); err != nil {
				release()
				failures = append(failures, fmt.Errorf("mark deployment %s applying: %w", candidate.ID, err))
				continue
			}
		}
		if s.gateway == nil || s.delivery == nil {
			release()
			failures = append(failures, errors.New("deployment recovery dependencies are not configured"))
			continue
		}
		build, err := s.delivery.Build(value.BuildID)
		if err == nil {
			switch value.Action {
			case "publish":
				var revision string
				revision, err = s.gateway.Publish(ctx, value, build, value.ExpectedGatewayRevision)
				if err == nil {
					value.GatewayRevision = revision
				}
			case "rollback":
				var revision string
				revision, err = s.gateway.Rollback(ctx, value, build, value.ExpectedGatewayRevision)
				if err == nil {
					value.GatewayRevision = revision
				}
			default:
				err = fmt.Errorf("deployment %q has unsupported recovery action", value.ID)
			}
		}
		if err == nil {
			value.Status = domain.DeploymentActive
			err = s.repository.PromoteDeployment(value, value.PreviousID)
		}
		release()
		if err != nil {
			failures = append(failures, fmt.Errorf("recover deployment %s: %w", candidate.ID, err))
		}
	}
	return errors.Join(failures...)
}

type CreateDeploymentRequest struct {
	domain.Deployment
	ConfirmedTarget          string `json:"confirmedTarget"`
	ConfirmedGatewayRevision string `json:"confirmedGatewayRevision"`
}

type DeploymentTargetState struct {
	SiteID                       string  `json:"siteId"`
	EnvironmentID                string  `json:"environmentId"`
	GatewayRevision              *string `json:"gatewayRevision"`
	LocalRevision                string  `json:"localRevision,omitempty"`
	RequiresBaselineConfirmation bool    `json:"requiresBaselineConfirmation"`
}

func (s *DeploymentService) TargetState(ctx context.Context, siteID, environmentID string) (DeploymentTargetState, error) {
	if !deploymentSiteIDPattern.MatchString(siteID) || !deploymentEnvironmentIDPattern.MatchString(environmentID) || s.gateway == nil {
		return DeploymentTargetState{}, domain.ErrInvalidPath
	}
	remote, err := s.gateway.CurrentRevision(ctx, siteID)
	if err != nil {
		return DeploymentTargetState{}, err
	}
	state := DeploymentTargetState{SiteID: siteID, EnvironmentID: environmentID, GatewayRevision: remote}
	active, activeErr := s.repository.ActiveDeployment(siteID, environmentID)
	if activeErr != nil && activeErr != domain.ErrDeploymentNotFound {
		return DeploymentTargetState{}, activeErr
	}
	if activeErr == domain.ErrDeploymentNotFound {
		state.RequiresBaselineConfirmation = remote != nil && *remote != ""
		return state, nil
	}
	state.LocalRevision = active.GatewayRevision
	if active.GatewayRevision == "" || remote == nil || *remote != active.GatewayRevision {
		return DeploymentTargetState{}, domain.ErrGatewayRevisionConflict
	}
	return state, nil
}

func (s *DeploymentService) Create(ctx context.Context, request CreateDeploymentRequest) (domain.Deployment, error) {
	deployment := request.Deployment
	if !deploymentIdempotencyIDPattern.MatchString(deployment.ID) || !deploymentSiteIDPattern.MatchString(deployment.SiteID) || !deploymentEnvironmentIDPattern.MatchString(deployment.EnvironmentID) || deployment.SnapshotID == "" || deployment.BuildID == "" {
		return domain.Deployment{}, domain.ErrInvalidPath
	}
	target := deployment.SiteID + "/" + deployment.EnvironmentID
	if request.ConfirmedTarget != target {
		return domain.Deployment{}, domain.ErrDeploymentConfirmationRequired
	}
	release, err := s.lockTarget(ctx, target)
	if err != nil {
		return domain.Deployment{}, err
	}
	defer release()
	existing, existingErr := s.repository.Deployment(deployment.ID)
	if existingErr == nil {
		if existing.Action != "publish" || existing.SiteID != deployment.SiteID || existing.EnvironmentID != deployment.EnvironmentID || existing.SnapshotID != deployment.SnapshotID || existing.BuildID != deployment.BuildID {
			return domain.Deployment{}, domain.ErrDeploymentConflict
		}
		if existing.Status == domain.DeploymentActive {
			return existing, nil
		}
		if existing.Status == domain.DeploymentSuperseded {
			return existing, nil
		}
		if existing.Status == domain.DeploymentPending || existing.Status == domain.DeploymentApplying {
			return domain.Deployment{}, domain.ErrDeploymentInProgress
		}
	} else if existingErr != domain.ErrDeploymentNotFound {
		return domain.Deployment{}, existingErr
	}
	if s.delivery == nil || s.gateway == nil {
		return domain.Deployment{}, fmt.Errorf("deployment dependencies are not configured")
	}
	snapshot, err := s.delivery.Snapshot(deployment.SnapshotID)
	if err != nil {
		return domain.Deployment{}, err
	}
	build, err := s.delivery.Build(deployment.BuildID)
	if err != nil {
		return domain.Deployment{}, err
	}
	if snapshot.Status != domain.SnapshotReady || snapshot.SiteID != deployment.SiteID || build.Status != domain.BuildSucceeded || build.SnapshotID != snapshot.ID || strings.TrimSpace(build.ArtifactPath) == "" || strings.TrimSpace(build.ArtifactChecksum) == "" {
		return domain.Deployment{}, domain.ErrDeploymentNotReady
	}
	if _, err := s.repository.Environment(deployment.EnvironmentID); err != nil {
		return domain.Deployment{}, err
	}
	remoteRevision, err := s.gateway.CurrentRevision(ctx, deployment.SiteID)
	if err != nil {
		return domain.Deployment{}, err
	}
	previous, previousErr := s.repository.ActiveDeployment(deployment.SiteID, deployment.EnvironmentID)
	if previousErr != nil && previousErr != domain.ErrDeploymentNotFound {
		return domain.Deployment{}, previousErr
	}
	if previousErr == nil {
		deployment.PreviousID = previous.ID
		if previous.GatewayRevision == "" || remoteRevision == nil || *remoteRevision != previous.GatewayRevision {
			return domain.Deployment{}, domain.ErrGatewayRevisionConflict
		}
	} else if remoteRevision != nil && *remoteRevision != "" && request.ConfirmedGatewayRevision != *remoteRevision {
		return domain.Deployment{}, domain.ErrGatewayBaselineConfirmationRequired
	}
	deployment.ExpectedGatewayRevision = remoteRevision
	if existingErr == nil && existing.Status == domain.DeploymentFailed && !sameOptionalRevision(existing.ExpectedGatewayRevision, remoteRevision) {
		return domain.Deployment{}, domain.ErrGatewayRevisionConflict
	}
	deployment.Action = "publish"
	deployment.Status = domain.DeploymentPending
	if err := s.repository.StartDeployment(deployment); err != nil {
		return domain.Deployment{}, err
	}
	deployment.Status = domain.DeploymentApplying
	if err := s.repository.SaveDeployment(deployment); err != nil {
		deployment.Status = domain.DeploymentFailed
		_ = s.repository.SaveDeployment(deployment)
		return domain.Deployment{}, err
	}
	newRevision, err := s.gateway.Publish(ctx, deployment, build, remoteRevision)
	if err != nil {
		// A transport error cannot prove whether Gateway committed the release.
		// Keep the operation applying and reserved for idempotent recovery.
		return deployment, &DeploymentOutcomeUncertain{Deployment: deployment, cause: err}
	}
	deployment.GatewayRevision = newRevision
	deployment.Status = domain.DeploymentActive
	if err := s.repository.PromoteDeployment(deployment, deployment.PreviousID); err != nil {
		deployment.Status = domain.DeploymentFailed
		_ = s.repository.SaveDeployment(deployment)
		return deployment, err
	}
	return deployment, nil
}
func (s *DeploymentService) Rollback(ctx context.Context, id, confirmedTarget string) (domain.Deployment, error) {
	current, err := s.repository.Deployment(id)
	if err != nil {
		return domain.Deployment{}, err
	}
	target := current.SiteID + "/" + current.EnvironmentID
	release, err := s.lockTarget(ctx, target)
	if err != nil {
		return domain.Deployment{}, err
	}
	defer release()
	if lister, ok := s.repository.(domain.DeploymentLister); ok {
		values, err := lister.Deployments()
		if err != nil {
			return domain.Deployment{}, err
		}
		for _, value := range values {
			if value.Action == "rollback" && value.PreviousID == id && (value.Status == domain.DeploymentActive || value.Status == domain.DeploymentSuperseded) {
				if confirmedTarget != value.SiteID+"/"+value.EnvironmentID {
					return domain.Deployment{}, domain.ErrDeploymentConfirmationRequired
				}
				return value, nil
			}
		}
	}
	current, err = s.repository.Deployment(id)
	if err != nil {
		return domain.Deployment{}, err
	}
	if current.Status != domain.DeploymentActive || current.PreviousID == "" {
		return domain.Deployment{}, domain.ErrDeploymentNotFound
	}
	if confirmedTarget != current.SiteID+"/"+current.EnvironmentID {
		return domain.Deployment{}, domain.ErrDeploymentConfirmationRequired
	}
	previous, err := s.repository.Deployment(current.PreviousID)
	if err != nil {
		return domain.Deployment{}, err
	}
	if s.delivery == nil || s.gateway == nil {
		return domain.Deployment{}, fmt.Errorf("deployment dependencies are not configured")
	}
	build, err := s.delivery.Build(previous.BuildID)
	if err != nil {
		return domain.Deployment{}, err
	}
	if build.Status != domain.BuildSucceeded || build.SnapshotID != previous.SnapshotID || build.ArtifactPath == "" || build.ArtifactChecksum == "" {
		return domain.Deployment{}, domain.ErrDeploymentNotReady
	}
	remoteRevision, err := s.gateway.CurrentRevision(ctx, current.SiteID)
	if err != nil {
		return domain.Deployment{}, err
	}
	if current.GatewayRevision == "" || remoteRevision == nil || *remoteRevision != current.GatewayRevision {
		return domain.Deployment{}, domain.ErrGatewayRevisionConflict
	}
	identity := sha256.Sum256([]byte("rollback\x00" + current.ID + "\x00" + previous.ID))
	rollback := domain.Deployment{ID: "rollback-" + hex.EncodeToString(identity[:]), SiteID: current.SiteID, EnvironmentID: current.EnvironmentID, SnapshotID: previous.SnapshotID, BuildID: previous.BuildID, PreviousID: current.ID, Status: domain.DeploymentPending, Action: "rollback"}
	rollback.ExpectedGatewayRevision = remoteRevision
	if err := s.repository.StartDeployment(rollback); err != nil {
		return domain.Deployment{}, err
	}
	rollback.Status = domain.DeploymentApplying
	if err := s.repository.SaveDeployment(rollback); err != nil {
		rollback.Status = domain.DeploymentFailed
		_ = s.repository.SaveDeployment(rollback)
		return domain.Deployment{}, err
	}
	newRevision, err := s.gateway.Rollback(ctx, rollback, build, remoteRevision)
	if err != nil {
		// Preserve the target reservation: the remote outcome may be unknown.
		return rollback, &DeploymentOutcomeUncertain{Deployment: rollback, cause: err}
	}
	rollback.GatewayRevision = newRevision
	rollback.Status = domain.DeploymentActive
	if err := s.repository.PromoteDeployment(rollback, current.ID); err != nil {
		rollback.Status = domain.DeploymentFailed
		_ = s.repository.SaveDeployment(rollback)
		return rollback, err
	}
	return rollback, nil
}

func sameOptionalRevision(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
