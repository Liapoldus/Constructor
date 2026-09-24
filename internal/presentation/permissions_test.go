package presentation

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Liapoldus/Constructor/internal/application"
)

func TestDocumentedMutationEndpointsRequireTheirDeclaredPermission(t *testing.T) {
	tests := []struct {
		method     string
		path       string
		permission string
	}{
		{http.MethodPost, "/api/v1/projects", "code.project"},
		{http.MethodPost, "/api/v1/projects/demo/activate", "code.project"},
		{http.MethodGet, "/api/v1/projects/demo/files/Caddyfile", "content.read"},
		{http.MethodHead, "/api/v1/projects/demo/files/Caddyfile", "content.read"},
		{http.MethodGet, "/api/v1/projects/demo/files/src/app.tsx", ""},
		{http.MethodPut, "/api/v1/projects/demo/files/src/app.tsx", "content.write"},
		{http.MethodPut, "/api/v1/project/file", "content.write"},
		{http.MethodPut, "/api/v1/project/content", "content.write"},
		{http.MethodPost, "/api/v1/project/assets", "assets.write"},
		{http.MethodPost, "/api/v1/project/sites", "content.write"},
		{http.MethodPost, "/api/v1/project/pages", "content.write"},
		{http.MethodPut, "/api/v1/project/routes", "content.write"},
		{http.MethodPost, "/api/v1/project/routes/generate", "content.write"},
		{http.MethodPost, "/api/v1/project/generate", "content.write"},
		{http.MethodPost, "/api/v1/project/file/merge", "content.write"},
		{http.MethodPost, "/api/v1/preview", "build.execute"},
		{http.MethodDelete, "/api/v1/preview", "build.execute"},
		{http.MethodPost, "/api/v1/snapshots", "snapshots.create"},
		{http.MethodPost, "/api/v1/snapshots/snapshot-1/builds", "build.execute"},
		{http.MethodPost, "/api/v1/builds", "build.execute"},
		{http.MethodPost, "/api/v1/builds/build-1/deployments", "deploy.execute"},
		{http.MethodPost, "/api/v1/git/commit", "code.commit"},
		{http.MethodPost, "/api/v1/git/branches", "code.project"},
		{http.MethodPost, "/api/v1/git/checkout", "code.project"},
		{http.MethodPost, "/api/v1/repositories/clone", "code.project"},
		{http.MethodPost, "/api/v1/repositories/worktree", "code.project"},
		{http.MethodPost, "/api/v1/deployments", "deploy.execute"},
		{http.MethodPost, "/api/v1/deployments/rollback", "deploy.execute"},
		{http.MethodPost, "/api/v1/deployments/deployment-1/rollback", "deploy.execute"},
		{http.MethodPost, "/api/v1/sites", "environment.write"},
		{http.MethodPost, "/api/v1/environments", "environment.write"},
		{http.MethodPost, "/api/v1/users", "admin.roles"},
		{http.MethodPost, "/api/v1/roles", "admin.roles"},
		{http.MethodPost, "/api/v1/permissions", "admin.roles"},
		{http.MethodPost, "/api/v1/user-roles", "admin.roles"},
		{http.MethodPost, "/api/v1/role-permissions", "admin.roles"},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			if got := requiredPermission(test.method, test.path); got != test.permission {
				t.Fatalf("requiredPermission() = %q, want %q", got, test.permission)
			}
		})
	}
}

func TestNamedProjectFileReadsRequireContentReadGrant(t *testing.T) {
	auth := application.NewAuthService(deniedPermissionAuthorizer{})
	handler := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, auth, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/projects/demo/files/Caddyfile", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("named project file GET was not denied without content.read: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestReadOnlyValidationPostsDoNotRequireMutationGrants(t *testing.T) {
	for _, path := range []string{"/api/v1/project/validate", "/api/v1/projects/demo/validate"} {
		if got := requiredPermission(http.MethodPost, path); got != "" {
			t.Errorf("read-only validation %s requires unexpected permission %q", path, got)
		}
	}
}

func TestUnclassifiedMutationsFailClosedAndReadMethodsRemainPublic(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodTrace} {
		if got := requiredPermission(method, "/api/v1/new-endpoint"); got != "admin.roles" {
			t.Errorf("%s unclassified endpoint permission = %q, want admin.roles", method, got)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		if got := requiredPermission(method, "/api/v1/new-endpoint"); got != "" {
			t.Errorf("%s read endpoint permission = %q, want none", method, got)
		}
	}
	auth := application.NewAuthService(deniedPermissionAuthorizer{})
	handler := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, auth, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/new-endpoint", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("unclassified mutation was not rejected before routing: status=%d body=%s", response.Code, response.Body.String())
	}
}
