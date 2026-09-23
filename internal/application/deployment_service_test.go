package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type deliveryRecords struct {
	snapshots map[string]domain.Snapshot
	builds    map[string]domain.Build
}

func (d deliveryRecords) SaveSnapshot(domain.Snapshot) error { return nil }
func (d deliveryRecords) Snapshot(id string) (domain.Snapshot, error) {
	value, ok := d.snapshots[id]
	if !ok {
		return domain.Snapshot{}, domain.ErrUnknownDelivery
	}
	return value, nil
}
func (d deliveryRecords) SaveBuild(domain.Build) error { return nil }
func (d deliveryRecords) Build(id string) (domain.Build, error) {
	value, ok := d.builds[id]
	if !ok {
		return domain.Build{}, domain.ErrUnknownDelivery
	}
	return value, nil
}
func (d deliveryRecords) Snapshots() ([]domain.Snapshot, error) { return nil, nil }
func (d deliveryRecords) Builds() ([]domain.Build, error)       { return nil, nil }

type deploymentRecords struct {
	mu           sync.Mutex
	sites        map[string]domain.Site
	environments map[string]domain.Environment
	deployments  map[string]domain.Deployment
}

func newDeploymentRecords() *deploymentRecords {
	return &deploymentRecords{sites: map[string]domain.Site{}, environments: map[string]domain.Environment{"production": {ID: "production", Name: "Production", Kind: "production"}}, deployments: map[string]domain.Deployment{}}
}
func (r *deploymentRecords) SaveSite(value domain.Site) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sites[value.ID] = value
	return nil
}
func (r *deploymentRecords) Site(id string) (domain.Site, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.sites[id]
	if !ok {
		return domain.Site{}, domain.ErrDeploymentNotFound
	}
	return value, nil
}
func (r *deploymentRecords) SaveEnvironment(value domain.Environment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.environments[value.ID] = value
	return nil
}
func (r *deploymentRecords) Environment(id string) (domain.Environment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.environments[id]
	if !ok {
		return domain.Environment{}, domain.ErrDeploymentNotFound
	}
	return value, nil
}
func (r *deploymentRecords) SaveDeployment(value domain.Deployment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deployments[value.ID] = value
	return nil
}
func (r *deploymentRecords) StartDeployment(value domain.Deployment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.deployments[value.ID]; ok {
		if existing.SiteID != value.SiteID || existing.EnvironmentID != value.EnvironmentID || existing.SnapshotID != value.SnapshotID || existing.BuildID != value.BuildID || existing.Action != value.Action {
			return domain.ErrDeploymentConflict
		}
		if existing.Status != domain.DeploymentFailed {
			return domain.ErrDeploymentInProgress
		}
	}
	for _, existing := range r.deployments {
		if existing.SiteID == value.SiteID && existing.EnvironmentID == value.EnvironmentID && (existing.Status == domain.DeploymentPending || existing.Status == domain.DeploymentApplying) {
			return domain.ErrDeploymentInProgress
		}
	}
	value.Status = domain.DeploymentPending
	r.deployments[value.ID] = value
	return nil
}
func (r *deploymentRecords) Deployment(id string) (domain.Deployment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.deployments[id]
	if !ok {
		return domain.Deployment{}, domain.ErrDeploymentNotFound
	}
	return value, nil
}
func (r *deploymentRecords) ActiveDeployment(siteID, environmentID string) (domain.Deployment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, value := range r.deployments {
		if value.SiteID == siteID && value.EnvironmentID == environmentID && value.Status == domain.DeploymentActive {
			return value, nil
		}
	}
	return domain.Deployment{}, domain.ErrDeploymentNotFound
}
func (r *deploymentRecords) PromoteDeployment(value domain.Deployment, expectedActiveID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	activeID := ""
	for id, existing := range r.deployments {
		if existing.SiteID == value.SiteID && existing.EnvironmentID == value.EnvironmentID && existing.Status == domain.DeploymentActive {
			activeID = id
		}
	}
	if activeID != expectedActiveID {
		return domain.ErrActiveDeployment
	}
	if activeID != "" {
		previous := r.deployments[activeID]
		previous.Status = domain.DeploymentSuperseded
		r.deployments[activeID] = previous
	}
	value.Status = domain.DeploymentActive
	r.deployments[value.ID] = value
	return nil
}
func (r *deploymentRecords) Deployments() ([]domain.Deployment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make([]domain.Deployment, 0, len(r.deployments))
	for _, value := range r.deployments {
		values = append(values, value)
	}
	return values, nil
}

type deploymentGateway struct {
	published     []domain.Deployment
	rollbacks     []domain.Deployment
	err           error
	current       string
	sequence      int
	expected      []*string
	replayResults map[string]string
}

func (g *deploymentGateway) CurrentRevision(context.Context, string) (*string, error) {
	if g.current == "" {
		return nil, nil
	}
	value := g.current
	return &value, nil
}
func (g *deploymentGateway) nextRevision() (string, error) {
	if g.err != nil {
		return "", g.err
	}
	g.sequence++
	g.current = strings.Repeat(fmt.Sprintf("%x", g.sequence), 64)
	return g.current, nil
}
func (g *deploymentGateway) Publish(_ context.Context, deployment domain.Deployment, _ domain.Build, expected *string) (string, error) {
	g.published = append(g.published, deployment)
	g.expected = append(g.expected, expected)
	if revision, ok := g.replayResults[deployment.ID]; ok {
		return revision, g.err
	}
	return g.nextRevision()
}
func (g *deploymentGateway) Rollback(_ context.Context, deployment domain.Deployment, _ domain.Build, expected *string) (string, error) {
	g.rollbacks = append(g.rollbacks, deployment)
	g.expected = append(g.expected, expected)
	if revision, ok := g.replayResults[deployment.ID]; ok {
		return revision, g.err
	}
	return g.nextRevision()
}

func readyDeliveryRecords() deliveryRecords {
	return deliveryRecords{
		snapshots: map[string]domain.Snapshot{
			"snapshot-1": {ID: "snapshot-1", SiteID: "site-one", Locale: "en-US", Status: domain.SnapshotReady},
			"snapshot-2": {ID: "snapshot-2", SiteID: "site-one", Locale: "en-US", Status: domain.SnapshotReady},
		},
		builds: map[string]domain.Build{
			"build-1": {ID: "build-1", SnapshotID: "snapshot-1", Status: domain.BuildSucceeded, ArtifactPath: "/artifacts/one", ArtifactChecksum: "sha256-one"},
			"build-2": {ID: "build-2", SnapshotID: "snapshot-2", Status: domain.BuildSucceeded, ArtifactPath: "/artifacts/two", ArtifactChecksum: "sha256-two"},
		},
	}
}

func deploymentRequest(id, snapshot, build string) CreateDeploymentRequest {
	return CreateDeploymentRequest{
		Deployment:      domain.Deployment{ID: id, SiteID: "site-one", EnvironmentID: "production", SnapshotID: snapshot, BuildID: build},
		ConfirmedTarget: "site-one/production",
	}
}

func TestDeploymentRequiresExactTargetConfirmationAndReadyMatchingBuild(t *testing.T) {
	repository := newDeploymentRecords()
	gateway := &deploymentGateway{}
	service := NewDeploymentService(repository, gateway, readyDeliveryRecords())
	request := deploymentRequest("deployment-00000001", "snapshot-1", "build-1")
	request.ConfirmedTarget = "site-one/staging"
	if _, err := service.Create(context.Background(), request); !errors.Is(err, domain.ErrDeploymentConfirmationRequired) {
		t.Fatalf("wrong target confirmation error=%v", err)
	}
	request.ConfirmedTarget = "site-one/production"
	request.BuildID = "build-2"
	if _, err := service.Create(context.Background(), request); !errors.Is(err, domain.ErrDeploymentNotReady) {
		t.Fatalf("mismatched build error=%v", err)
	}
	if len(gateway.published) != 0 {
		t.Fatalf("Gateway was called before deployment preconditions: %#v", gateway.published)
	}
}

func TestDeploymentPromotesOnlyAfterGatewaySuccessAndRollbackIsAudited(t *testing.T) {
	repository := newDeploymentRecords()
	gateway := &deploymentGateway{}
	service := NewDeploymentService(repository, gateway, readyDeliveryRecords())
	first, err := service.Create(context.Background(), deploymentRequest("deployment-00000001", "snapshot-1", "build-1"))
	if err != nil || first.Status != domain.DeploymentActive {
		t.Fatalf("first deployment=%#v err=%v", first, err)
	}
	if len(first.GatewayRevision) != 64 || first.GatewayRevision != gateway.current {
		t.Fatalf("Gateway revision was not retained: %#v", first)
	}
	second, err := service.Create(context.Background(), deploymentRequest("deployment-00000002", "snapshot-2", "build-2"))
	if err != nil || second.Status != domain.DeploymentActive || second.PreviousID != first.ID {
		t.Fatalf("second deployment=%#v err=%v", second, err)
	}
	active, err := repository.ActiveDeployment("site-one", "production")
	if err != nil || active.ID != second.ID {
		t.Fatalf("active deployment=%#v err=%v", active, err)
	}
	rollback, err := service.Rollback(context.Background(), second.ID, "site-one/production")
	if err != nil || rollback.Action != "rollback" || rollback.Status != domain.DeploymentActive || rollback.BuildID != first.BuildID || rollback.PreviousID != second.ID {
		t.Fatalf("rollback record=%#v err=%v", rollback, err)
	}
	if len(gateway.published) != 2 || len(gateway.rollbacks) != 1 {
		t.Fatalf("unexpected Gateway calls: publish=%d rollback=%d", len(gateway.published), len(gateway.rollbacks))
	}
	retried, err := service.Rollback(context.Background(), second.ID, "site-one/production")
	if err != nil || retried.ID != rollback.ID || len(gateway.rollbacks) != 1 {
		t.Fatalf("rollback retry was not idempotent: %#v err=%v calls=%d", retried, err, len(gateway.rollbacks))
	}
	repeatedFirst, err := service.Create(context.Background(), deploymentRequest("deployment-00000001", "snapshot-1", "build-1"))
	if err != nil || repeatedFirst.Status != domain.DeploymentSuperseded || len(gateway.published) != 2 {
		t.Fatalf("superseded publish retry was not idempotent: %#v err=%v calls=%d", repeatedFirst, err, len(gateway.published))
	}
	rolledBack, err := repository.Deployment(rollback.ID)
	if err != nil || rolledBack.Action != "rollback" || rolledBack.Status != domain.DeploymentActive {
		t.Fatalf("rollback audit record=%#v err=%v", rolledBack, err)
	}
}

func TestDeploymentRequiresExplicitGatewayBaselineAndRejectsDrift(t *testing.T) {
	repository := newDeploymentRecords()
	gateway := &deploymentGateway{current: strings.Repeat("a", 64)}
	service := NewDeploymentService(repository, gateway, readyDeliveryRecords())
	request := deploymentRequest("deployment-00000001", "snapshot-1", "build-1")
	if _, err := service.Create(context.Background(), request); !errors.Is(err, domain.ErrGatewayBaselineConfirmationRequired) {
		t.Fatalf("unconfirmed external release error=%v", err)
	}
	request.ConfirmedGatewayRevision = gateway.current
	created, err := service.Create(context.Background(), request)
	if err != nil || created.GatewayRevision == "" {
		t.Fatalf("confirmed bootstrap deployment=%#v err=%v", created, err)
	}
	if len(gateway.expected) != 1 || gateway.expected[0] == nil || *gateway.expected[0] != strings.Repeat("a", 64) {
		t.Fatalf("publish did not use accepted CAS baseline: %#v", gateway.expected)
	}
	// Simulate a deployment made by another Gateway operator.
	gateway.current = strings.Repeat("f", 64)
	if _, err := service.Create(context.Background(), deploymentRequest("deployment-00000002", "snapshot-2", "build-2")); !errors.Is(err, domain.ErrGatewayRevisionConflict) {
		t.Fatalf("out-of-band Gateway revision error=%v", err)
	}
	if len(gateway.published) != 1 {
		t.Fatalf("drifted target was still published: %d calls", len(gateway.published))
	}
}

func TestRecoverResumesApplyingDeploymentWithThePersistedCASPrecondition(t *testing.T) {
	repository := newDeploymentRecords()
	gateway := &deploymentGateway{current: strings.Repeat("b", 64), replayResults: map[string]string{"deployment-00000001": strings.Repeat("c", 64)}}
	service := NewDeploymentService(repository, gateway, readyDeliveryRecords())
	expected := strings.Repeat("a", 64)
	interrupted := domain.Deployment{ID: "deployment-00000001", SiteID: "site-one", EnvironmentID: "production", SnapshotID: "snapshot-1", BuildID: "build-1", Status: domain.DeploymentApplying, Action: "publish", ExpectedGatewayRevision: &expected}
	if err := repository.StartDeployment(interrupted); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveDeployment(interrupted); err != nil {
		t.Fatal(err)
	}
	if err := service.Recover(context.Background()); err != nil {
		t.Fatalf("recovery error=%v", err)
	}
	active, err := repository.ActiveDeployment(interrupted.SiteID, interrupted.EnvironmentID)
	if err != nil || active.ID != interrupted.ID || active.GatewayRevision != strings.Repeat("c", 64) {
		t.Fatalf("recovered deployment=%#v err=%v", active, err)
	}
	if len(gateway.published) != 1 || len(gateway.expected) != 1 || gateway.expected[0] == nil || *gateway.expected[0] != expected {
		t.Fatalf("recovery did not replay with original CAS value: %#v %#v", gateway.published, gateway.expected)
	}
}

func TestRecoverTransitionsPendingReservationBeforePromoting(t *testing.T) {
	repository := newDeploymentRecords()
	revision := strings.Repeat("f", 64)
	gateway := &deploymentGateway{current: revision, replayResults: map[string]string{"deployment-pending-0001": strings.Repeat("e", 64)}}
	service := NewDeploymentService(repository, gateway, readyDeliveryRecords())
	value := domain.Deployment{ID: "deployment-pending-0001", SiteID: "site-one", EnvironmentID: "production", SnapshotID: "snapshot-1", BuildID: "build-1", Status: domain.DeploymentPending, Action: "publish", ExpectedGatewayRevision: &revision}
	if err := repository.StartDeployment(value); err != nil {
		t.Fatal(err)
	}
	if err := service.Recover(context.Background()); err != nil {
		t.Fatalf("pending recovery error=%v", err)
	}
	active, err := repository.ActiveDeployment(value.SiteID, value.EnvironmentID)
	if err != nil || active.ID != value.ID || active.GatewayRevision != strings.Repeat("e", 64) {
		t.Fatalf("pending reservation recovery=%#v err=%v", active, err)
	}
}

func TestRecoverLeavesUncertainDeploymentReservedWhenGatewayCannotConfirmOutcome(t *testing.T) {
	repository := newDeploymentRecords()
	gateway := &deploymentGateway{err: errors.New("gateway unavailable")}
	service := NewDeploymentService(repository, gateway, readyDeliveryRecords())
	expected := strings.Repeat("a", 64)
	interrupted := domain.Deployment{ID: "deployment-00000001", SiteID: "site-one", EnvironmentID: "production", SnapshotID: "snapshot-1", BuildID: "build-1", Status: domain.DeploymentApplying, Action: "publish", ExpectedGatewayRevision: &expected}
	if err := repository.StartDeployment(interrupted); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveDeployment(interrupted); err != nil {
		t.Fatal(err)
	}
	if err := service.Recover(context.Background()); err == nil {
		t.Fatal("unconfirmed Gateway outcome was reported as recovered")
	}
	stored, err := repository.Deployment(interrupted.ID)
	if err != nil || stored.Status != domain.DeploymentApplying {
		t.Fatalf("uncertain deployment status=%#v err=%v", stored, err)
	}
	if err := repository.StartDeployment(domain.Deployment{ID: "deployment-00000002", SiteID: "site-one", EnvironmentID: "production", SnapshotID: "snapshot-2", BuildID: "build-2", Action: "publish"}); err != domain.ErrDeploymentInProgress {
		t.Fatalf("uncertain operation did not reserve target: %v", err)
	}
}

func TestRecoverResumesInterruptedRollbackAgainstItsOriginalRevision(t *testing.T) {
	repository := newDeploymentRecords()
	gateway := &deploymentGateway{}
	service := NewDeploymentService(repository, gateway, readyDeliveryRecords())
	first, err := service.Create(context.Background(), deploymentRequest("deployment-00000001", "snapshot-1", "build-1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(context.Background(), deploymentRequest("deployment-00000002", "snapshot-2", "build-2"))
	if err != nil {
		t.Fatal(err)
	}
	expected := second.GatewayRevision
	rollbackID := "rollback-interrupted-0001"
	rollbackRevision := strings.Repeat("d", 64)
	gateway.replayResults = map[string]string{rollbackID: rollbackRevision}
	interrupted := domain.Deployment{ID: rollbackID, SiteID: second.SiteID, EnvironmentID: second.EnvironmentID, SnapshotID: first.SnapshotID, BuildID: first.BuildID, PreviousID: second.ID, Status: domain.DeploymentApplying, Action: "rollback", ExpectedGatewayRevision: &expected}
	if err := repository.StartDeployment(interrupted); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveDeployment(interrupted); err != nil {
		t.Fatal(err)
	}
	if err := service.Recover(context.Background()); err != nil {
		t.Fatalf("rollback recovery error=%v", err)
	}
	active, err := repository.ActiveDeployment(second.SiteID, second.EnvironmentID)
	if err != nil || active.ID != rollbackID || active.BuildID != first.BuildID || active.GatewayRevision != rollbackRevision {
		t.Fatalf("recovered rollback=%#v err=%v", active, err)
	}
	if len(gateway.rollbacks) != 1 || len(gateway.expected) != 3 || gateway.expected[2] == nil || *gateway.expected[2] != expected {
		t.Fatalf("rollback was not replayed with original precondition: calls=%d expected=%#v", len(gateway.rollbacks), gateway.expected)
	}
}

func TestFailedGatewayPublishLeavesPreviousActiveDeploymentUnchanged(t *testing.T) {
	repository := newDeploymentRecords()
	gateway := &deploymentGateway{}
	service := NewDeploymentService(repository, gateway, readyDeliveryRecords())
	first, err := service.Create(context.Background(), deploymentRequest("deployment-00000001", "snapshot-1", "build-1"))
	if err != nil {
		t.Fatal(err)
	}
	gateway.err = errors.New("gateway unavailable")
	uncertain, err := service.Create(context.Background(), deploymentRequest("deployment-00000002", "snapshot-2", "build-2"))
	if err == nil {
		t.Fatal("failed Gateway publish was accepted")
	}
	if uncertain.Status != domain.DeploymentApplying {
		t.Fatalf("unknown remote outcome was mislabeled: %#v", uncertain)
	}
	active, err := repository.ActiveDeployment("site-one", "production")
	if err != nil || active.ID != first.ID {
		t.Fatalf("previous deployment was not preserved: active=%#v err=%v", active, err)
	}
	if _, err := service.Create(context.Background(), deploymentRequest("deployment-00000003", "snapshot-2", "build-2")); !errors.Is(err, domain.ErrDeploymentInProgress) {
		t.Fatalf("uncertain publish did not reserve its target: %v", err)
	}
}

func TestFailedGatewayRollbackDoesNotChangeActiveDeployment(t *testing.T) {
	repository := newDeploymentRecords()
	gateway := &deploymentGateway{}
	service := NewDeploymentService(repository, gateway, readyDeliveryRecords())
	first, err := service.Create(context.Background(), deploymentRequest("deployment-00000001", "snapshot-1", "build-1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(context.Background(), deploymentRequest("deployment-00000002", "snapshot-2", "build-2"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Rollback(context.Background(), second.ID, "site-one/staging"); !errors.Is(err, domain.ErrDeploymentConfirmationRequired) {
		t.Fatalf("rollback accepted wrong target confirmation: %v", err)
	}
	if len(gateway.rollbacks) != 0 {
		t.Fatal("rollback called Gateway before target confirmation")
	}
	gateway.err = errors.New("gateway unavailable")
	uncertain, err := service.Rollback(context.Background(), second.ID, "site-one/production")
	if err == nil {
		t.Fatal("failed Gateway rollback was accepted")
	}
	if uncertain.Status != domain.DeploymentApplying {
		t.Fatalf("unknown rollback outcome was mislabeled: %#v", uncertain)
	}
	active, err := repository.ActiveDeployment("site-one", "production")
	if err != nil || active.ID != second.ID {
		t.Fatalf("failed rollback changed active deployment: active=%#v err=%v; old=%s", active, err, first.ID)
	}
}
