package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type BuildRunner interface {
	Run(context.Context, domain.Snapshot) (domain.BuildArtifact, error)
}
type DeliveryService struct {
	git       domain.GitRepository
	store     domain.DeliveryRepository
	validator func() ([]domain.Diagnostic, error)
	runner    BuildRunner
}

func NewDeliveryService(git domain.GitRepository, store domain.DeliveryRepository, validator func() ([]domain.Diagnostic, error), runner BuildRunner) *DeliveryService {
	return &DeliveryService{git: git, store: store, validator: validator, runner: runner}
}
func (s *DeliveryService) CreateSnapshot(siteID, locale string) (domain.Snapshot, error) {
	return s.CreateSnapshotFor(siteID, locale, s.validator, nil)
}

func (s *DeliveryService) CreateSnapshotFor(siteID, locale string, validator func() ([]domain.Diagnostic, error), prepare func() error) (domain.Snapshot, error) {
	if strings.TrimSpace(siteID) == "" || strings.TrimSpace(locale) == "" {
		return domain.Snapshot{}, domain.ErrInvalidPath
	}
	if prepare != nil {
		if err := prepare(); err != nil {
			return domain.Snapshot{}, fmt.Errorf("snapshot preparation failed: %w", err)
		}
	}
	if validator == nil {
		validator = s.validator
	}
	if validator == nil {
		return domain.Snapshot{}, fmt.Errorf("snapshot validator is not configured")
	}
	diagnostics, err := validator()
	if err != nil {
		return domain.Snapshot{}, err
	}
	for _, d := range diagnostics {
		if d.Severity == "error" {
			return domain.Snapshot{}, domain.StructuredValidationError{Diagnostics: diagnostics}
		}
	}
	return s.persistSnapshot(siteID, locale)
}

// CreateProjectSnapshot serializes all Constructor-managed project mutations
// across generation, validation, and Git revision capture. This keeps generated
// files and their source inputs within one pinned snapshot revision.
func (s *DeliveryService) CreateProjectSnapshot(projects *ProjectService, siteID, locale, environment string) (snapshot domain.Snapshot, resultErr error) {
	if projects == nil {
		return domain.Snapshot{}, fmt.Errorf("project service is not configured")
	}
	if strings.TrimSpace(siteID) == "" || strings.TrimSpace(locale) == "" {
		return domain.Snapshot{}, domain.ErrInvalidPath
	}
	projects.mutation.Lock()
	defer projects.mutation.Unlock()
	if workspaceFactory, ok := s.git.(domain.SnapshotWorkspaceFactory); ok {
		repositoryFactory, repositorySupported := projects.repository.(domain.ProjectRepositoryAtRoot)
		if !repositorySupported {
			return domain.Snapshot{}, fmt.Errorf("project repository cannot open an isolated snapshot worktree")
		}
		var workspace domain.SnapshotWorkspace
		repositoryPath := ""
		projectID := ""
		var err error
		if rootProvider, ok := projects.repository.(domain.ProjectRepositoryRoot); ok {
			sourceRoot := rootProvider.Root()
			repositoryPath, err = filepath.Abs(sourceRoot)
			if err != nil {
				return domain.Snapshot{}, err
			}
			manifest, _, manifestErr := repositoryFactory.AtRoot(repositoryPath).Manifest()
			if manifestErr != nil {
				return domain.Snapshot{}, manifestErr
			}
			projectID = manifest.ID
			rootedFactory, supportsRoot := s.git.(domain.SnapshotWorkspaceFactoryAtRoot)
			if !supportsRoot {
				return domain.Snapshot{}, fmt.Errorf("Git adapter cannot pin the selected project root")
			}
			workspace, err = rootedFactory.CreateSnapshotWorkspaceAt(repositoryPath)
		} else {
			workspace, err = workspaceFactory.CreateSnapshotWorkspace()
		}
		if err != nil {
			return domain.Snapshot{}, err
		}
		defer func() { resultErr = errors.Join(resultErr, workspace.Close()) }()
		snapshotProjects := NewProjectService(repositoryFactory.AtRoot(workspace.Root()))
		snapshotProjects.mutation.Lock()
		if err := validateAndGenerateSnapshot(snapshotProjects, siteID, locale, environment); err != nil {
			snapshotProjects.mutation.Unlock()
			return domain.Snapshot{}, err
		}
		snapshotProjects.mutation.Unlock()
		revision, err := workspace.Head()
		if err != nil {
			return domain.Snapshot{}, err
		}
		if err := workspace.Close(); err != nil {
			return domain.Snapshot{}, err
		}
		return s.persistSnapshotRevision(projectID, repositoryPath, siteID, locale, revision)
	}
	if err := validateAndGenerateSnapshot(projects, siteID, locale, environment); err != nil {
		return domain.Snapshot{}, err
	}
	return s.persistSnapshot(siteID, locale)
}

func validateAndGenerateSnapshot(projects *ProjectService, siteID, locale, environment string) error {
	diagnostics, err := projects.ValidateForSiteLocale(siteID, locale)
	if err != nil {
		return err
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			return domain.StructuredValidationError{Diagnostics: diagnostics}
		}
	}
	if _, err := projects.generateProjectArtifacts(siteID, environment); err != nil {
		return fmt.Errorf("snapshot preparation failed: %w", err)
	}
	return nil
}

func (s *DeliveryService) persistSnapshot(siteID, locale string) (domain.Snapshot, error) {
	commit, err := s.git.Head()
	if err != nil {
		return domain.Snapshot{}, err
	}
	return s.persistSnapshotRevision("", "", siteID, locale, commit)
}

func (s *DeliveryService) persistSnapshotRevision(projectID, repositoryPath, siteID, locale, commit string) (domain.Snapshot, error) {
	digestInput := strings.Join([]string{projectID, commit, siteID, locale}, "\x00")
	digest := sha256.Sum256([]byte(digestInput))
	identityInput := strings.Join([]string{repositoryPath, digestInput}, "\x00")
	identity := sha256.Sum256([]byte(identityInput))
	snapshot := domain.Snapshot{ID: hex.EncodeToString(identity[:8]), ProjectID: projectID, RepositoryPath: repositoryPath, SiteID: siteID, Locale: locale, GitCommit: commit, ContentDigest: hex.EncodeToString(digest[:]), Status: domain.SnapshotReady}
	return snapshot, s.store.SaveSnapshot(snapshot)
}
func (s *DeliveryService) BuildSnapshot(ctx context.Context, snapshotID string) (domain.Build, error) {
	snapshot, err := s.store.Snapshot(snapshotID)
	if err != nil {
		return domain.Build{}, err
	}
	if snapshot.Status != domain.SnapshotReady {
		return domain.Build{}, domain.ErrSnapshotNotReady
	}
	if s.runner == nil {
		return domain.Build{}, fmt.Errorf("build runner is not configured")
	}
	build := domain.Build{ID: snapshot.ID + "-build", SnapshotID: snapshot.ID, Status: domain.BuildRunning}
	if err := s.store.SaveBuild(build); err != nil {
		return domain.Build{}, err
	}
	artifact, runErr := s.runner.Run(ctx, snapshot)
	if runErr != nil {
		build.Status = domain.BuildFailed
		build.Error = runErr.Error()
		if saveErr := s.store.SaveBuild(build); saveErr != nil {
			return build, fmt.Errorf("build failed (%v) and its result could not be persisted: %w", runErr, saveErr)
		}
		return build, runErr
	}
	build.Status = domain.BuildSucceeded
	build.ArtifactPath = artifact.Path
	build.ArtifactChecksum = artifact.Checksum
	if err := s.store.SaveBuild(build); err != nil {
		return build, err
	}
	return build, nil
}
func (s *DeliveryService) Snapshots() ([]domain.Snapshot, error) { return s.store.Snapshots() }
func (s *DeliveryService) Builds() ([]domain.Build, error)       { return s.store.Builds() }
