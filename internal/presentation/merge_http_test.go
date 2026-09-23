package presentation

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Liapoldus/Constructor/internal/application"
	"github.com/Liapoldus/Constructor/internal/infrastructure"
)

func TestMergeFileReturnsTextAndRevisionForOptimisticRetry(t *testing.T) {
	root := t.TempDir()
	path := "liapoldus/content/site-a/en-US.json"
	base := []byte("{\n  \"left\": {\n    \"value\": 1\n  },\n\n  \"right\": {\n    \"value\": 1\n  }\n}\n")
	current := []byte("{\n  \"left\": {\n    \"value\": 2\n  },\n\n  \"right\": {\n    \"value\": 1\n  }\n}\n")
	candidate := []byte("{\n  \"left\": {\n    \"value\": 1\n  },\n\n  \"right\": {\n    \"value\": 2\n  }\n}\n")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), current, 0o644); err != nil {
		t.Fatal(err)
	}
	repository := infrastructure.NewFilesystemRepository(root)
	currentFile, err := repository.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	merge := application.NewMergeService(repository, infrastructure.NewGitMergeEngine())
	auth := application.NewAuthService(previewTestAuthorizer{})
	handler := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, merge, nil, auth, nil)
	body, err := json.Marshal(map[string]string{"path": path, "base": string(base), "candidate": string(candidate)})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/project/file/merge", bytes.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("ETag") != currentFile.Revision {
		t.Fatalf("ETag = %q, want current revision %q", response.Header().Get("ETag"), currentFile.Revision)
	}
	var result struct {
		Revision   string `json:"revision"`
		Content    string `json:"content"`
		Conflicted bool   `json:"conflicted"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Conflicted || result.Revision != currentFile.Revision {
		t.Fatalf("merge result = %#v", result)
	}
	want := "{\n  \"left\": {\n    \"value\": 2\n  },\n\n  \"right\": {\n    \"value\": 2\n  }\n}\n"
	if result.Content != want {
		t.Fatalf("merged content = %q, want %q", result.Content, want)
	}
	unchanged, err := repository.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unchanged.Content, current) {
		t.Fatal("merge endpoint changed the current file before an explicit optimistic write")
	}
	written, err := repository.Write(path, result.Revision, []byte(result.Content))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written.Content, []byte(want)) {
		t.Fatalf("retried write lost merged data: %q", written.Content)
	}
}

func TestMergeFileReportsOverlappingChangesWithoutWriting(t *testing.T) {
	root := t.TempDir()
	path := "liapoldus/content/site-a/en-US.json"
	base := []byte("{\n  \"title\": \"base\"\n}\n")
	current := []byte("{\n  \"title\": \"external\"\n}\n")
	candidate := []byte("{\n  \"title\": \"draft\"\n}\n")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), current, 0o644); err != nil {
		t.Fatal(err)
	}
	repository := infrastructure.NewFilesystemRepository(root)
	merge := application.NewMergeService(repository, infrastructure.NewGitMergeEngine())
	auth := application.NewAuthService(previewTestAuthorizer{})
	handler := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, merge, nil, auth, nil)
	body, err := json.Marshal(map[string]string{"path": path, "base": string(base), "candidate": string(candidate)})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/project/file/merge", bytes.NewReader(body)))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var result struct {
		Content    string `json:"content"`
		Conflicted bool   `json:"conflicted"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Conflicted || !bytes.Contains([]byte(result.Content), []byte("<<<<<<<")) {
		t.Fatalf("overlap was not exposed for manual reconciliation: %#v", result)
	}
	file, err := repository.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(file.Content, current) {
		t.Fatal("conflicted merge modified the current file")
	}
}
