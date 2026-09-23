package presentation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Liapoldus/Constructor/internal/application"
	"github.com/Liapoldus/Constructor/internal/domain"
	"github.com/Liapoldus/Constructor/internal/infrastructure"
)

func TestAPIProblemUsesRFC9457AndStableRequestID(t *testing.T) {
	auth := application.NewAuthService(previewTestAuthorizer{})
	handler := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, auth, nil)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/project/file", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("problem Content-Type = %q", got)
	}
	var body problemDetail
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Type != "about:blank" || body.Title != "Bad Request" || body.Status != http.StatusBadRequest || body.Code != "bad_request" {
		t.Fatalf("unexpected RFC 9457 problem: %#v", body)
	}
	if body.RequestID == "" || body.RequestID != response.Header().Get("X-Request-ID") || body.Instance != "/api/v1/project/file" {
		t.Fatalf("problem request metadata mismatch: body=%#v header=%q", body, response.Header().Get("X-Request-ID"))
	}
}

func TestSuccessJSONCarriesRequestID(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	requestID, ok := body["requestId"].(string)
	if !ok || requestID == "" || requestID != response.Header().Get("X-Request-ID") {
		t.Fatalf("success response did not carry its request ID: body=%#v header=%q", body, response.Header().Get("X-Request-ID"))
	}
}

func TestCanonicalFileResponseKeepsBytesAndCarriesRequestIDHeader(t *testing.T) {
	root := t.TempDir()
	content := []byte(`{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":[],"react":{"entry":"src/app.tsx"}}`)
	path := filepath.Join(root, "liapoldus", "project.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	projects := application.NewProjectService(infrastructure.NewFilesystemRepository(root))
	handler := NewHandler(projects, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/project/file?path=liapoldus/project.json", nil)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-Request-ID") == "" {
		t.Fatalf("unexpected canonical file response: status=%d headers=%v", response.Code, response.Header())
	}
	if response.Body.String() != string(content) {
		t.Fatalf("canonical document bytes changed: got %q, want %q", response.Body.String(), content)
	}
}

func TestMethodNotAllowedUsesProblemContract(t *testing.T) {
	auth := application.NewAuthService(previewTestAuthorizer{})
	handler := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, auth, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/project/sites", nil))
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("method error did not use problem contract: status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	var body problemDetail
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "method_not_allowed" || body.Status != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected method-not-allowed problem: %#v", body)
	}
}

func TestInternalProblemDoesNotExposeInternalError(t *testing.T) {
	response := httptest.NewRecorder()
	writeError(response, http.StatusInternalServerError, errTestInternal{})
	if response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("unexpected Content-Type: %q", response.Header().Get("Content-Type"))
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "internal_error" || body["detail"] != "Internal Server Error" || body["error"] != "Internal Server Error" {
		t.Fatalf("internal details were not safely redacted: %#v", body)
	}
}

func TestStructuredGenerationDiagnosticsUseValidationProblemContract(t *testing.T) {
	response := httptest.NewRecorder()
	statusForError(response, domain.StructuredValidationError{Diagnostics: []domain.Diagnostic{{
		Code: "theme.schema-unavailable", Severity: "error", Path: "liapoldus/themes/main.json", Message: "Theme generation is unsupported",
	}}})
	if response.Code != http.StatusUnprocessableEntity || response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("structured validation status=%d content-type=%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "project_validation_failed" {
		t.Fatalf("unexpected validation problem code: %#v", body)
	}
	diagnostics, ok := body["diagnostics"].([]any)
	if !ok || len(diagnostics) != 1 || diagnostics[0].(map[string]any)["code"] != "theme.schema-unavailable" {
		t.Fatalf("typed diagnostics missing from problem response: %#v", body)
	}
}

func TestSnapshotSourceChangeReturnsRetryableConflict(t *testing.T) {
	response := httptest.NewRecorder()
	statusForError(response, domain.ErrSnapshotSourceChanged)
	if response.Code != http.StatusConflict || response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("snapshot race status=%d content-type=%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "snapshot_source_changed" || body["detail"] != "Project files changed during snapshot capture. Retry the snapshot." {
		t.Fatalf("snapshot race did not return a retryable typed problem: %#v", body)
	}
}

type errTestInternal struct{}

func (errTestInternal) Error() string { return "/private/path/and/secret" }
