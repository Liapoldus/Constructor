package presentation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Liapoldus/Constructor/internal/application"
	"github.com/Liapoldus/Constructor/internal/domain"
	"github.com/Liapoldus/Constructor/internal/infrastructure"
)

func TestProjectAssetsEndpointReturnsVerifiedRegistry(t *testing.T) {
	root := t.TempDir()
	assetBytes := []byte("asset fixture")
	digest := sha256.Sum256(assetBytes)
	files := map[string][]byte{
		"liapoldus/assets.json":  []byte(fmt.Sprintf(`{"schemaVersion":1,"id":"assets","items":[{"id":"logo-image","type":"image","path":"public/assets/logo.png","mimeType":"image/png","size":%d,"sha256":"%s"}]}`, len(assetBytes), hex.EncodeToString(digest[:]))),
		"public/assets/logo.png": assetBytes,
	}
	for relative, content := range files {
		target := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, content, 0644); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHandler(application.NewProjectService(infrastructure.NewFilesystemRepository(root)), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/project/assets", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("asset list returned %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Assets []domain.AssetItem `json:"assets"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Assets) != 1 || body.Assets[0].ID != "logo-image" || body.Assets[0].Path != "public/assets/logo.png" {
		t.Fatalf("unexpected asset response: %#v", body.Assets)
	}

	if err := os.WriteFile(filepath.Join(root, "public/assets/logo.png"), []byte("tampered"), 0644); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/project/assets", nil))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("corrupt asset should return typed validation failure, got %d: %s", response.Code, response.Body.String())
	}
}

func TestProjectAssetUploadCreatesDeduplicatedRegisteredAsset(t *testing.T) {
	root := t.TempDir()
	var imageBytes bytes.Buffer
	imageValue := image.NewRGBA(image.Rect(0, 0, 2, 2))
	imageValue.Set(0, 0, color.RGBA{G: 255, A: 255})
	if err := png.Encode(&imageBytes, imageValue); err != nil {
		t.Fatal(err)
	}
	projects := application.NewProjectService(infrastructure.NewFilesystemRepository(root))
	handler := NewHandler(projects, nil, nil, nil, nil, nil, nil, nil, nil, nil, application.NewAuthService(previewTestAuthorizer{}), nil)

	upload := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/project/assets", bytes.NewReader(imageBytes.Bytes())))
		return response
	}
	first := upload()
	if first.Code != http.StatusCreated || first.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("upload returned %d %s: %s", first.Code, first.Header().Get("Content-Type"), first.Body.String())
	}
	var result application.AssetUploadResult
	if err := json.Unmarshal(first.Body.Bytes(), &result); err != nil || result.Reused || result.Asset.MIMEType != "image/png" {
		t.Fatalf("unexpected upload response %#v err=%v", result, err)
	}
	second := upload()
	if second.Code != http.StatusOK {
		t.Fatalf("duplicate upload returned %d: %s", second.Code, second.Body.String())
	}
	if err := json.Unmarshal(second.Body.Bytes(), &result); err != nil || !result.Reused {
		t.Fatalf("duplicate response should identify reuse: %#v err=%v", result, err)
	}
	if result.Asset.ID == "" || result.Asset.Path == "" {
		t.Fatalf("upload did not return generated asset metadata: %#v", result)
	}
}

func TestProjectAssetUploadRequiresAssetsWritePermission(t *testing.T) {
	auth := application.NewAuthService(deniedPermissionAuthorizer{})
	handler := NewHandler(application.NewProjectService(nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, auth, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/project/assets", bytes.NewReader([]byte("body"))))
	if response.Code != http.StatusForbidden {
		t.Fatalf("unauthorized upload returned %d: %s", response.Code, response.Body.String())
	}
	if got := requiredPermission(http.MethodPost, "/api/v1/project/assets"); got != "assets.write" {
		t.Fatalf("upload permission = %q, want assets.write", got)
	}
}
