package presentation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Liapoldus/Constructor/internal/application"
	"github.com/Liapoldus/Constructor/internal/domain"
	"github.com/Liapoldus/Constructor/internal/infrastructure"
)

type httpBuildRunner struct{}

func (httpBuildRunner) Run(context.Context, domain.Snapshot) (domain.BuildArtifact, error) {
	return domain.BuildArtifact{Path: "/tmp/constructor-artifact", Checksum: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}, nil
}

func TestBuildEndpointReturnsArtifactChecksum(t *testing.T) {
	store := infrastructure.NewMemoryDeliveryStore()
	if err := store.SaveSnapshot(domain.Snapshot{ID: "snapshot-1", SiteID: "site", Locale: "en-US", GitCommit: "commit", Status: domain.SnapshotReady}); err != nil {
		t.Fatal(err)
	}
	delivery := application.NewDeliveryService(nil, store, nil, httpBuildRunner{})
	handler := NewHandler(nil, nil, nil, nil, nil, delivery, nil, nil, nil, nil, application.NewAuthService(previewTestAuthorizer{}), nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/snapshots/snapshot-1/builds", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("build status=%d body=%s", response.Code, response.Body.String())
	}
	var build domain.Build
	if err := json.Unmarshal(response.Body.Bytes(), &build); err != nil {
		t.Fatal(err)
	}
	if build.Status != domain.BuildSucceeded || build.ArtifactChecksum != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("build response=%#v", build)
	}
}

func TestSnapshotGeneratesCompleteArtifactBatchBeforePinningRevision(t *testing.T) {
	registry := infrastructure.NewFilesystemProjectRegistry(t.TempDir())
	project, err := registry.Create(context.Background(), "snapshot-demo", "Snapshot demo")
	if err != nil {
		t.Fatal(err)
	}
	projects := application.NewProjectService(infrastructure.NewFilesystemRepository(project.Path))
	gitCommand := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", project.Path}, args...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, output, err)
		}
		return strings.TrimSpace(string(output))
	}
	beforeHead := gitCommand("rev-parse", "HEAD")
	beforeStatus := gitCommand("status", "--short")
	generatedPaths := []string{
		"src/generated/assets.ts",
		"src/generated/content/ru-RU.json",
		"src/generated/routes.tsx",
		"src/generated/theme.css",
	}
	beforeGenerated := make(map[string]string, len(generatedPaths))
	for _, relative := range generatedPaths {
		content, err := os.ReadFile(filepath.Join(project.Path, relative))
		if err != nil {
			t.Fatal(err)
		}
		beforeGenerated[relative] = string(content)
	}
	store := infrastructure.NewMemoryDeliveryStore()
	delivery := application.NewDeliveryService(infrastructure.NewGitCommandRepository(project.Path), store, nil, nil)
	handler := NewHandler(projects, nil, nil, nil, application.NewRouteService(projects), delivery, nil, nil, nil, nil, application.NewAuthService(previewTestAuthorizer{}), nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/snapshots?siteId=local-site&locale=ru-RU", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("snapshot status=%d body=%s", response.Code, response.Body.String())
	}
	var snapshot domain.Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.ProjectID != "snapshot-demo" || snapshot.RepositoryPath != "" || strings.Contains(response.Body.String(), "repositoryPath") {
		t.Fatalf("snapshot identity leaked local path or lost project ID: %#v body=%s", snapshot, response.Body.String())
	}
	for _, relative := range generatedPaths {
		content := gitCommand("show", snapshot.GitCommit+":"+relative)
		if content == "" {
			t.Errorf("snapshot omitted generated artifact %s", relative)
		}
		active, err := os.ReadFile(filepath.Join(project.Path, relative))
		if err != nil {
			t.Fatal(err)
		}
		if string(active) != beforeGenerated[relative] {
			t.Errorf("snapshot generation modified active checkout file %s", relative)
		}
	}
	if got := gitCommand("rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("snapshot generation changed active branch HEAD: before=%s after=%s", beforeHead, got)
	}
	if got := gitCommand("status", "--short"); got != beforeStatus {
		t.Fatalf("snapshot generation changed active checkout status: before=%q after=%q", beforeStatus, got)
	}
	if got := gitCommand("worktree", "list", "--porcelain"); strings.Count(got, "worktree ") != 1 {
		t.Fatalf("snapshot worktree registration leaked: %s", got)
	}
}

func TestFailedSnapshotCleansTemporaryWorktreeAndReturnsTypedDiagnostics(t *testing.T) {
	registry := infrastructure.NewFilesystemProjectRegistry(t.TempDir())
	project, err := registry.Create(context.Background(), "invalid-theme", "Invalid theme")
	if err != nil {
		t.Fatal(err)
	}
	themeDirectory := filepath.Join(project.Path, "liapoldus", "themes")
	if err := os.MkdirAll(themeDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themeDirectory, "brand.json"), []byte(`{"schemaVersion":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	gitCommand := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", project.Path}, args...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, output, err)
		}
		return strings.TrimSpace(string(output))
	}
	projects := application.NewProjectService(infrastructure.NewFilesystemRepository(project.Path))
	delivery := application.NewDeliveryService(infrastructure.NewGitCommandRepository(project.Path), infrastructure.NewMemoryDeliveryStore(), nil, nil)
	handler := NewHandler(projects, nil, nil, nil, application.NewRouteService(projects), delivery, nil, nil, nil, nil, application.NewAuthService(previewTestAuthorizer{}), nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/snapshots?siteId=local-site&locale=ru-RU", nil))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("snapshot status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "project_validation_failed" {
		t.Fatalf("snapshot did not return a typed validation problem: %#v", body)
	}
	if got := gitCommand("worktree", "list", "--porcelain"); strings.Count(got, "worktree ") != 1 {
		t.Fatalf("failed snapshot leaked a temporary worktree: %s", got)
	}
	if got := gitCommand("for-each-ref", "--format=%(refname)", "refs/constructor/snapshots"); got != "" {
		t.Fatalf("failed snapshot leaked a pinned temporary ref: %s", got)
	}
}

type httpDeploymentGateway struct {
	publishes, rollbacks int
	revision             string
	err                  error
}

func (gateway *httpDeploymentGateway) CurrentRevision(context.Context, string) (*string, error) {
	if gateway.revision == "" {
		return nil, nil
	}
	value := gateway.revision
	return &value, nil
}
func (gateway *httpDeploymentGateway) Publish(context.Context, domain.Deployment, domain.Build, *string) (string, error) {
	gateway.publishes++
	if gateway.err != nil {
		return "", gateway.err
	}
	gateway.revision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	return gateway.revision, nil
}
func (gateway *httpDeploymentGateway) Rollback(context.Context, domain.Deployment, domain.Build, *string) (string, error) {
	gateway.rollbacks++
	if gateway.err != nil {
		return "", gateway.err
	}
	gateway.revision = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	return gateway.revision, nil
}

func TestDeploymentEndpointReturnsApplyingOperationWhenGatewayOutcomeIsUnknown(t *testing.T) {
	store, err := infrastructure.NewSQLiteDeliveryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSnapshot(domain.Snapshot{ID: "snapshot-1", SiteID: "site-one", Locale: "en-US", GitCommit: "commit", ContentDigest: "digest", Status: domain.SnapshotReady}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveBuild(domain.Build{ID: "build-1", SnapshotID: "snapshot-1", Status: domain.BuildSucceeded, ArtifactPath: "/artifact/site", ArtifactChecksum: "sha256-site"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveEnvironment(domain.Environment{ID: "production", Name: "Production", Kind: "production"}); err != nil {
		t.Fatal(err)
	}
	gateway := &httpDeploymentGateway{err: errors.New("connection interrupted")}
	service := application.NewDeploymentService(store, gateway, store)
	handler := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, service, application.NewAuthService(previewTestAuthorizer{}), nil)
	body := `{"id":"deployment-uncertain-0001","siteId":"site-one","environmentId":"production","snapshotId":"snapshot-1","buildId":"build-1","confirmedTarget":"site-one/production"}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/deployments", strings.NewReader(body)))
	if response.Code != http.StatusAccepted {
		t.Fatalf("uncertain deployment response status=%d body=%s", response.Code, response.Body.String())
	}
	var deployment domain.Deployment
	if err := json.Unmarshal(response.Body.Bytes(), &deployment); err != nil {
		t.Fatal(err)
	}
	if deployment.Status != domain.DeploymentApplying {
		t.Fatalf("uncertain outcome response=%#v", deployment)
	}
	stored, err := store.Deployment(deployment.ID)
	if err != nil || stored.Status != domain.DeploymentApplying {
		t.Fatalf("persisted uncertain deployment=%#v err=%v", stored, err)
	}
}

func TestDeploymentEndpointRequiresTargetConfirmationAndPersistsActiveResult(t *testing.T) {
	store, err := infrastructure.NewSQLiteDeliveryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot := domain.Snapshot{ID: "snapshot-1", SiteID: "site-one", Locale: "en-US", GitCommit: "commit", ContentDigest: "digest", Status: domain.SnapshotReady}
	if err := store.SaveSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	build := domain.Build{ID: "build-1", SnapshotID: snapshot.ID, Status: domain.BuildSucceeded, ArtifactPath: "/artifact/site", ArtifactChecksum: "sha256-site"}
	if err := store.SaveBuild(build); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveEnvironment(domain.Environment{ID: "production", Name: "Production", Kind: "production"}); err != nil {
		t.Fatal(err)
	}
	gateway := &httpDeploymentGateway{}
	service := application.NewDeploymentService(store, gateway, store)
	handler := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, service, application.NewAuthService(previewTestAuthorizer{}), nil)
	body := `{"id":"deployment-00000001","siteId":"site-one","environmentId":"production","snapshotId":"snapshot-1","buildId":"build-1","confirmedTarget":"site-one/staging"}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/deployments", strings.NewReader(body)))
	if response.Code != http.StatusPreconditionRequired || gateway.publishes != 0 {
		t.Fatalf("missing target confirmation status=%d calls=%d body=%s", response.Code, gateway.publishes, response.Body.String())
	}
	body = strings.Replace(body, "site-one/staging", "site-one/production", 1)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/deployments", strings.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("confirmed deployment status=%d body=%s", response.Code, response.Body.String())
	}
	var created domain.Deployment
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.GatewayRevision != strings.Repeat("a", 64) {
		t.Fatalf("deployment response lost Gateway revision: %#v err=%v body=%s", created, err, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-1/deployments", strings.NewReader(body)))
	if response.Code != http.StatusCreated || gateway.publishes != 1 {
		t.Fatalf("idempotent build deployment status=%d calls=%d body=%s", response.Code, gateway.publishes, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/builds/other-build/deployments", strings.NewReader(body)))
	if response.Code != http.StatusConflict || gateway.publishes != 1 {
		t.Fatalf("mismatched build resource status=%d calls=%d body=%s", response.Code, gateway.publishes, response.Body.String())
	}
	snapshot2 := domain.Snapshot{ID: "snapshot-2", SiteID: "site-one", Locale: "en-US", GitCommit: "commit-2", ContentDigest: "digest-2", Status: domain.SnapshotReady}
	if err := store.SaveSnapshot(snapshot2); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveBuild(domain.Build{ID: "build-2", SnapshotID: snapshot2.ID, Status: domain.BuildSucceeded, ArtifactPath: "/artifact/site-2", ArtifactChecksum: "sha256-site-2"}); err != nil {
		t.Fatal(err)
	}
	secondBody := strings.Replace(strings.Replace(body, "deployment-00000001", "deployment-00000002", 1), "snapshot-1", "snapshot-2", 1)
	secondBody = strings.Replace(secondBody, "build-1", "build-2", 1)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-2/deployments", strings.NewReader(secondBody)))
	if response.Code != http.StatusCreated || gateway.publishes != 2 {
		t.Fatalf("second deployment status=%d calls=%d body=%s", response.Code, gateway.publishes, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/deployment-target?siteId=site-one&environmentId=production", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"localRevision":"`+strings.Repeat("a", 64)+`"`) {
		t.Fatalf("target state did not expose matching revision: status=%d body=%s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/deployments/deployment-00000002/rollback?confirmedTarget=site-one%2Fstaging", nil))
	if response.Code != http.StatusPreconditionRequired || gateway.rollbacks != 0 {
		t.Fatalf("wrong rollback target status=%d calls=%d body=%s", response.Code, gateway.rollbacks, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/deployments/deployment-00000002/rollback?confirmedTarget=site-one%2Fproduction", nil))
	if response.Code != http.StatusOK || gateway.rollbacks != 1 {
		t.Fatalf("rollback status=%d calls=%d body=%s", response.Code, gateway.rollbacks, response.Body.String())
	}
	active, err := store.ActiveDeployment("site-one", "production")
	if err != nil || active.Status != domain.DeploymentActive || active.Action != "rollback" || gateway.publishes != 2 {
		t.Fatalf("deployment was not applied and persisted: active=%#v calls=%d err=%v", active, gateway.publishes, err)
	}
}

func TestBuildDeploymentAndRollbackAliasesRequireDeployPermission(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, application.NewAuthService(deniedPermissionAuthorizer{}), nil)
	for _, path := range []string{
		"/api/v1/builds/build-1/deployments",
		"/api/v1/deployments/deployment-1/rollback?confirmedTarget=site-one%2Fproduction",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
		if response.Code != http.StatusForbidden {
			t.Errorf("unauthorized deployment mutation %q returned %d: %s", path, response.Code, response.Body.String())
		}
	}
}
