package presentation

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Liapoldus/Constructor/internal/application"
	"github.com/Liapoldus/Constructor/internal/infrastructure"
)

func TestNamedProjectFilesReadAndWriteWithoutChangingActiveProject(t *testing.T) {
	workspaceRoot := t.TempDir()
	activeRoot := filepath.Join(workspaceRoot, "active")
	otherRoot := filepath.Join(workspaceRoot, "other")
	for id, root := range map[string]string{"active": activeRoot, "other": otherRoot} {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, "liapoldus"), 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := map[string]any{"schemaVersion": 1, "id": id, "name": id, "pages": []string{"home"}, "components": []string{}, "react": map[string]string{"entry": "src/main.tsx"}}
		content, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "liapoldus", "project.json"), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	filePath := filepath.Join(otherRoot, "src", "main.tsx")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("export const value = 'before'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	projectRegistry := infrastructure.NewFilesystemProjectRegistry(workspaceRoot)
	activeRepository := infrastructure.NewFilesystemRepository(activeRoot)
	activeProjects := application.NewProjectService(activeRepository)
	fileService := application.NewProjectFileService(projectRegistry, activeProjects)
	registryService := application.NewProjectRegistryService(projectRegistry)
	auth := application.NewAuthService(previewTestAuthorizer{})
	handler := NewHandler(activeProjects, nil, registryService, nil, nil, nil, nil, nil, nil, nil, auth, nil, fileService)
	validationRequest := httptest.NewRequest(http.MethodPost, "/api/v1/projects/other/validate", nil)
	validationResponse := httptest.NewRecorder()
	handler.ServeHTTP(validationResponse, validationRequest)
	if validationResponse.Code != http.StatusUnprocessableEntity || !strings.Contains(validationResponse.Body.String(), "site.document-required") {
		t.Fatalf("named project validation status=%d body=%s", validationResponse.Code, validationResponse.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/projects/other/files/src/main.tsx", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "export const value = 'before'\n" {
		t.Fatalf("GET status=%d body=%q", response.Code, response.Body.String())
	}
	revision := response.Header().Get("ETag")
	if revision == "" {
		t.Fatal("GET did not return a file revision")
	}

	updated := []byte("export const value = 'after'\n")
	request = httptest.NewRequest(http.MethodPut, "/api/v1/projects/other/files/src/main.tsx", bytes.NewReader(updated))
	request.Header.Set("If-Match", revision)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("ETag") == revision {
		t.Fatalf("PUT status=%d body=%s ETag=%q", response.Code, response.Body.String(), response.Header().Get("ETag"))
	}
	actual, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(updated) {
		t.Fatalf("other project file = %q", actual)
	}
	request = httptest.NewRequest(http.MethodPut, "/api/v1/projects/other/files/src/main.tsx", bytes.NewReader([]byte("stale")))
	request.Header.Set("If-Match", revision)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("stale PUT status=%d body=%s", response.Code, response.Body.String())
	}
	actual, err = os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(updated) {
		t.Fatalf("stale write replaced current project file: %q", actual)
	}
	if activeRepository.Root() != activeRoot {
		t.Fatalf("named project file request changed active root to %q", activeRepository.Root())
	}
}

func TestNamedProjectFileWriteRequiresContentWritePermission(t *testing.T) {
	auth := application.NewAuthService(deniedPermissionAuthorizer{})
	handler := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, auth, nil)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/projects/other/files/src/main.tsx", strings.NewReader("export {}\n"))
	request.Header.Set("If-Match", "revision")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("denied named-project write status = %d, body = %s", response.Code, response.Body.String())
	}
}
