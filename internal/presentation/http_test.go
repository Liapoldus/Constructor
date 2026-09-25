package presentation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Liapoldus/Constructor/internal/application"
	"github.com/Liapoldus/Constructor/internal/domain"
	"github.com/Liapoldus/Constructor/internal/infrastructure"
)

func TestProjectSitesEndpointReturnsValidatedActiveProjectSites(t *testing.T) {
	root := t.TempDir()
	for relative, content := range map[string]string{
		"liapoldus/project.json":          `{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":[],"react":{"entry":"src/app.tsx"}}`,
		"liapoldus/sites/local-site.json": `{"schemaVersion":1,"id":"local-site","projectId":"demo","name":"Local","pages":[{"id":"home","name":"Главная"}],"locales":["ru-RU"]}`,
	} {
		target := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	projects := application.NewProjectService(infrastructure.NewFilesystemRepository(root))
	handler := NewHandler(projects, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/project/sites", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Sites []domain.SiteDocument `json:"sites"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Sites) != 1 || body.Sites[0].ID != "local-site" || len(body.Sites[0].Pages) != 1 || body.Sites[0].Pages[0].ID != "home" || body.Sites[0].Pages[0].Name != "Главная" {
		t.Fatalf("unexpected Site list: %#v", body.Sites)
	}
}

type gatewayGroupsReadFixture struct{}

func (gatewayGroupsReadFixture) ListGroups(context.Context) (domain.GatewayGroupList, error) {
	current := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	return domain.GatewayGroupList{RequestID: "gateway-groups-request", Items: []domain.GatewayGroup{{ID: "frontend", Kind: "application", Active: true, CurrentRevision: &current, State: "ready"}}}, nil
}

func (gatewayGroupsReadFixture) ListReleases(_ context.Context, groupID, cursor string, limit int) (domain.GatewayGroupRevisionList, error) {
	if groupID != "frontend" || cursor != "next" || limit != 25 {
		return domain.GatewayGroupRevisionList{}, errors.New("unexpected release query")
	}
	return domain.GatewayGroupRevisionList{RequestID: "gateway-releases-request", Items: []domain.GatewayGroupRevisionSummary{{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", GroupID: groupID, CaddyfileDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", CreatedAt: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}}}, nil
}

func TestGatewayGroupReadAPIForwardsMetadataOnlyGroupsAndRevisions(t *testing.T) {
	handler := NewHandlerWithGatewayGroups(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, application.NewAuthService(previewTestAuthorizer{}), nil, nil, application.NewGatewayGroupCatalogService(gatewayGroupsReadFixture{}))
	groups := httptest.NewRecorder()
	handler.ServeHTTP(groups, httptest.NewRequest(http.MethodGet, "/api/v1/gateway/groups", nil))
	if groups.Code != http.StatusOK {
		t.Fatalf("groups returned %d: %s", groups.Code, groups.Body.String())
	}
	var groupList domain.GatewayGroupList
	if err := json.Unmarshal(groups.Body.Bytes(), &groupList); err != nil || groupList.RequestID != "gateway-groups-request" || len(groupList.Items) != 1 || groupList.Items[0].CurrentRevision == nil {
		t.Fatalf("invalid groups response %#v, err=%v", groupList, err)
	}

	releases := httptest.NewRecorder()
	handler.ServeHTTP(releases, httptest.NewRequest(http.MethodGet, "/api/v1/gateway/groups/frontend/releases?cursor=next&limit=25", nil))
	if releases.Code != http.StatusOK {
		t.Fatalf("releases returned %d: %s", releases.Code, releases.Body.String())
	}
	var releaseList domain.GatewayGroupRevisionList
	if err := json.Unmarshal(releases.Body.Bytes(), &releaseList); err != nil || releaseList.RequestID != "gateway-releases-request" || len(releaseList.Items) != 1 || releaseList.NextCursor != nil {
		t.Fatalf("invalid releases response %#v, err=%v", releaseList, err)
	}
	if strings.Contains(releases.Body.String(), "caddyfilePath") || strings.Contains(releases.Body.String(), "artifactPath") || strings.Contains(releases.Body.String(), "caddyfile\"") {
		t.Fatalf("revision listing exposed non-metadata: %s", releases.Body.String())
	}
}

func TestGatewayGroupReadAPIRejectsUnsupportedMethodsAndInvalidIDs(t *testing.T) {
	handler := NewHandlerWithGatewayGroups(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, application.NewAuthService(previewTestAuthorizer{}), nil, nil, application.NewGatewayGroupCatalogService(gatewayGroupsReadFixture{}))
	for _, request := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/gateway/groups"},
		{http.MethodGet, "/api/v1/gateway/groups/Bad-ID/releases"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(request.method, request.path, nil))
		if response.Code != http.StatusMethodNotAllowed && response.Code != http.StatusBadRequest {
			t.Errorf("%s %s returned %d: %s", request.method, request.path, response.Code, response.Body.String())
		}
	}
}

func TestGatewayGroupReadAPIRequiresGatewayGroupsReadPermission(t *testing.T) {
	handler := NewHandlerWithGatewayGroups(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, application.NewAuthService(deniedPermissionAuthorizer{}), nil, nil, application.NewGatewayGroupCatalogService(gatewayGroupsReadFixture{}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/gateway/groups", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("unauthorized group read returned %d: %s", response.Code, response.Body.String())
	}
}

func TestProjectThemesEndpointReturnsValidatedThemeDocuments(t *testing.T) {
	root := t.TempDir()
	theme := `{"schemaVersion":1,"id":"brand","name":"Brand","tokens":{"colors.primary":{"type":"color","value":"#123456"}},"variants":{"dark":{"colors.primary":"#abcdef"}}}`
	target := filepath.Join(root, "liapoldus", "themes", "brand.json")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(theme), 0644); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(application.NewProjectService(infrastructure.NewFilesystemRepository(root)), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/project/themes", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Themes []application.ThemeResource `json:"themes"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Themes) != 1 || body.Themes[0].ID != "brand" || body.Themes[0].Revision == "" || body.Themes[0].Variants["dark"]["colors.primary"] != "#abcdef" {
		t.Fatalf("unexpected Theme list: %#v", body.Themes)
	}
}

func TestCreateProjectSiteEndpointClonesConfigurationAndInitializesLocales(t *testing.T) {
	root := t.TempDir()
	for relative, content := range map[string]string{
		"liapoldus/project.json":    `{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":[],"react":{"entry":"src/app.tsx"}}`,
		"liapoldus/sites/main.json": `{"schemaVersion":1,"id":"main","projectId":"demo","name":"Main","pages":[{"id":"home","name":"Home"}],"locales":["ru-RU","en-US"]}`,
	} {
		target := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	projects := application.NewProjectService(infrastructure.NewFilesystemRepository(root))
	auth := application.NewAuthService(previewTestAuthorizer{})
	handler := NewHandler(projects, nil, nil, nil, nil, nil, nil, nil, nil, nil, auth, nil)
	manifestResponse := httptest.NewRecorder()
	handler.ServeHTTP(manifestResponse, httptest.NewRequest(http.MethodGet, "/api/v1/project", nil))
	siteResponse := httptest.NewRecorder()
	handler.ServeHTTP(siteResponse, httptest.NewRequest(http.MethodGet, "/api/v1/project/file?path=liapoldus%2Fsites%2Fmain.json", nil))
	requestBody, err := json.Marshal(application.CreateSiteRequest{
		ID: "campaign", Name: "Campaign", SourceSiteID: "main",
		Revisions: application.SiteRevisions{Manifest: manifestResponse.Header().Get("ETag"), Source: siteResponse.Header().Get("ETag")},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/project/sites", bytes.NewReader(requestBody)))
	if response.Code != http.StatusCreated {
		t.Fatalf("create Site returned %d: %s", response.Code, response.Body.String())
	}
	for _, relative := range []string{"liapoldus/sites/campaign.json", "liapoldus/content/campaign/ru-RU.json", "liapoldus/content/campaign/en-US.json"} {
		if _, err := os.Stat(filepath.Join(root, relative)); err != nil {
			t.Errorf("create Site omitted %s: %v", relative, err)
		}
	}
}

func TestCreateProjectSiteRequiresContentWritePermission(t *testing.T) {
	auth := application.NewAuthService(deniedPermissionAuthorizer{})
	handler := NewHandler(application.NewProjectService(nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, auth, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/project/sites", bytes.NewReader([]byte(`{}`))))
	if response.Code != http.StatusForbidden {
		t.Fatalf("unauthorized Site creation returned %d: %s", response.Code, response.Body.String())
	}
}

func TestCreateProjectPageEndpointWritesVersionedPageDocuments(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"liapoldus/project.json":            `{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":[],"react":{"entry":"src/app.tsx"}}`,
		"liapoldus/sites/site-a.json":       `{"schemaVersion":1,"id":"site-a","projectId":"demo","name":"Site A","pages":[{"id":"home","name":"Home"}],"locales":["ru-RU"]}`,
		"liapoldus/routes/development.json": `{"schemaVersion":1,"id":"development-routes","routes":[{"id":"home","path":"/","page":"home"}]}`,
		"src/pages/home.page.tsx":           `export default function Home() { return <main /> }`,
	}
	for relative, content := range files {
		target := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	projects := application.NewProjectService(infrastructure.NewFilesystemRepository(root))
	handler := NewHandler(projects, nil, nil, nil, application.NewRouteService(projects), nil, nil, nil, nil, nil, application.NewAuthService(previewTestAuthorizer{}), nil)
	manifestResponse := httptest.NewRecorder()
	handler.ServeHTTP(manifestResponse, httptest.NewRequest(http.MethodGet, "/api/v1/project", nil))
	siteResponse := httptest.NewRecorder()
	handler.ServeHTTP(siteResponse, httptest.NewRequest(http.MethodGet, "/api/v1/project/file?path=liapoldus%2Fsites%2Fsite-a.json", nil))
	routeResponse := httptest.NewRecorder()
	handler.ServeHTTP(routeResponse, httptest.NewRequest(http.MethodGet, "/api/v1/project/routes?environment=development", nil))
	requestBody, err := json.Marshal(application.CreatePageRequest{
		SiteID: "site-a", ID: "about", Name: "About", RoutePath: "/about",
		Revisions: application.PageRevisions{
			Manifest: manifestResponse.Header().Get("ETag"),
			Site:     siteResponse.Header().Get("ETag"),
			Routes:   routeResponse.Header().Get("ETag"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/project/pages", bytes.NewReader(requestBody)))
	if response.Code != http.StatusCreated {
		t.Fatalf("create page returned %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Page domain.SitePage `json:"page"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Page.ID != "about" || body.Page.Name != "About" {
		t.Fatalf("unexpected create page response: %#v err=%v", body, err)
	}
	created, err := os.ReadFile(filepath.Join(root, "src", "pages", "about.page.tsx"))
	if err != nil || !bytes.Contains(created, []byte("About")) {
		t.Fatalf("page source was not created: %s err=%v", created, err)
	}
}

func TestValidateEndpointReturnsStructuredContentSourceCoordinates(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"liapoldus/project.json":                  `{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":["Hero"],"react":{"entry":"src/app.tsx"}}`,
		"liapoldus/sites/local-site.json":         `{"schemaVersion":1,"id":"local-site","projectId":"demo","name":"Local","pages":["home"],"locales":["ru-RU"]}`,
		"liapoldus/content/local-site/ru-RU.json": `{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{}}]}`,
		"src/components/hero/schema.json":         `{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title","required":true,"localized":true}]}`,
		"src/components/hero/component.tsx":       `export default function Hero(){return null}`,
	}
	for relative, content := range files {
		target := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	projects := application.NewProjectService(infrastructure.NewFilesystemRepository(root))
	handler := NewHandler(projects, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/project/validate?siteId=local-site&locale=ru-RU", nil))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("validation error did not use RFC 9457: %q", response.Header().Get("Content-Type"))
	}
	var body struct {
		Diagnostics []domain.Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Diagnostics) == 0 {
		t.Fatalf("expected required-field diagnostic: %s", response.Body.String())
	}
	for _, diagnostic := range body.Diagnostics {
		if diagnostic.Code == "content.required" {
			if diagnostic.PageID != "home" || diagnostic.InstanceID != "hero-main" || diagnostic.FieldKey != "title" {
				t.Fatalf("diagnostic lost source coordinates: %#v", diagnostic)
			}
			return
		}
	}
	t.Fatalf("required-field diagnostic missing: %#v", body.Diagnostics)
}

func TestValidateWithoutContextChecksEveryEnabledSiteLocale(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"liapoldus/project.json":                 `{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":[],"react":{"entry":"src/app.tsx"}}`,
		"liapoldus/sites/site-a.json":            `{"schemaVersion":1,"id":"site-a","projectId":"demo","name":"Site A","pages":["home"],"locales":["ru-RU","en-US"]}`,
		"liapoldus/content/site-a/ru-RU.json":    `{"schemaVersion":1,"id":"ru-content","instances":[]}`,
		"liapoldus/content/site-a/disabled.json": `{"schemaVersion":1,"id":"disabled-content","instances":[]}`,
	}
	for relative, content := range files {
		target := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHandler(application.NewProjectService(infrastructure.NewFilesystemRepository(root)), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/project/validate", nil))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Diagnostics []domain.Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	missingEnglish := false
	for _, diagnostic := range body.Diagnostics {
		if diagnostic.Code == "content.document-required" && diagnostic.Path == "liapoldus/content/site-a/en-US.json" {
			missingEnglish = true
		}
		if diagnostic.Path == "liapoldus/content/site-a/disabled.json" {
			t.Fatalf("validation inspected a disabled locale: %#v", diagnostic)
		}
	}
	if !missingEnglish {
		t.Fatalf("project-wide validation missed enabled locale: %#v", body.Diagnostics)
	}
}

func TestValidateRejectsPartialSiteLocaleContext(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/project/validate?siteId=site-a", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
}

func TestGenerateRequiresSiteAndLocale(t *testing.T) {
	handler := &Handler{}
	for _, query := range []string{"", "?siteId=site-a", "?locale=en-US"} {
		response := httptest.NewRecorder()
		handler.generate(response, httptest.NewRequest(http.MethodPost, "/api/v1/project/generate"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("query %q returned %d, want 400: %s", query, response.Code, response.Body.String())
		}
	}
}

func TestSnapshotRequiresSiteAndLocale(t *testing.T) {
	handler := &Handler{}
	for _, query := range []string{"", "?siteId=site-a", "?locale=en-US"} {
		response := httptest.NewRecorder()
		handler.snapshot(response, httptest.NewRequest(http.MethodPost, "/api/v1/snapshots"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("query %q returned %d, want 400: %s", query, response.Code, response.Body.String())
		}
	}
}

func TestIsLoopbackBrowserOrigin(t *testing.T) {
	tests := []struct {
		origin string
		want   bool
	}{
		{origin: "", want: true},
		{origin: "http://localhost:5173", want: true},
		{origin: "https://constructor.localhost", want: false},
		{origin: "http://localhost:5173/path", want: false},
		{origin: "http://user@localhost:5173", want: false},
		{origin: "http://127.0.0.1:5173", want: true},
		{origin: "http://[::1]:5173", want: true},
		{origin: "https://example.com", want: false},
		{origin: "null", want: false},
		{origin: "file://localhost", want: false},
	}
	for _, test := range tests {
		t.Run(test.origin, func(t *testing.T) {
			if got := isLoopbackBrowserOrigin(test.origin); got != test.want {
				t.Fatalf("isLoopbackBrowserOrigin(%q) = %v, want %v", test.origin, got, test.want)
			}
		})
	}
}

type previewTestWorkspace struct{}

func (previewTestWorkspace) Root() string          { return "/tmp/preview-test" }
func (previewTestWorkspace) Activate(string) error { return nil }

type previewTestRunner struct{ active bool }

func (r *previewTestRunner) Start(root string) (domain.PreviewSession, error) {
	r.active = true
	return domain.PreviewSession{URL: "http://127.0.0.1:41000/", Root: root, Active: true}, nil
}
func (r *previewTestRunner) Stop(string) error { r.active = false; return nil }
func (r *previewTestRunner) Status(string) domain.PreviewSession {
	return domain.PreviewSession{URL: "http://127.0.0.1:41000/", Active: r.active}
}

type previewTestAuthorizer struct{}

func (previewTestAuthorizer) Principal(context.Context) (domain.Principal, error) {
	return domain.Principal{ID: "test"}, nil
}
func (previewTestAuthorizer) Allows(domain.Principal, string) bool { return true }

type deniedPermissionAuthorizer struct{}

func (deniedPermissionAuthorizer) Principal(context.Context) (domain.Principal, error) {
	return domain.Principal{ID: "read-only"}, nil
}
func (deniedPermissionAuthorizer) Allows(domain.Principal, string) bool { return false }

func TestPreviewEndpointStartsAndStopsAndRejectsRemoteBrowserOrigins(t *testing.T) {
	runner := &previewTestRunner{}
	preview := application.NewPreviewService(previewTestWorkspace{}, runner)
	auth := application.NewAuthService(previewTestAuthorizer{})
	handler := NewHandler(nil, preview, nil, nil, nil, nil, nil, nil, nil, nil, auth, nil)

	remoteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/preview", nil)
	remoteRequest.Header.Set("Origin", "https://attacker.example")
	remoteResponse := httptest.NewRecorder()
	handler.ServeHTTP(remoteResponse, remoteRequest)
	if remoteResponse.Code != http.StatusForbidden || runner.active {
		t.Fatalf("remote origin must not launch a local preview: status=%d active=%v", remoteResponse.Code, runner.active)
	}

	startRequest := httptest.NewRequest(http.MethodPost, "/api/v1/preview", nil)
	startRequest.Header.Set("Origin", "http://localhost:5173")
	startResponse := httptest.NewRecorder()
	handler.ServeHTTP(startResponse, startRequest)
	if startResponse.Code != http.StatusOK || !runner.active {
		t.Fatalf("loopback origin should start preview: status=%d active=%v", startResponse.Code, runner.active)
	}

	stopRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/preview", nil)
	stopRequest.Header.Set("Origin", "http://127.0.0.1:5173")
	stopResponse := httptest.NewRecorder()
	handler.ServeHTTP(stopResponse, stopRequest)
	if stopResponse.Code != http.StatusOK || runner.active {
		t.Fatalf("loopback origin should stop preview: status=%d active=%v", stopResponse.Code, runner.active)
	}
}
