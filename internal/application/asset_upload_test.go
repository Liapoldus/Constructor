package application

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Liapoldus/Constructor/internal/domain"
	"github.com/Liapoldus/Constructor/internal/infrastructure"
	nativewebp "github.com/gen2brain/webp"
	xwebp "golang.org/x/image/webp"
)

func pngFixture(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	imageValue := image.NewRGBA(image.Rect(0, 0, 2, 2))
	imageValue.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&buffer, imageValue); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func encodedRasterFixture(t *testing.T, format string) []byte {
	t.Helper()
	value := image.NewRGBA(image.Rect(0, 0, 2, 2))
	value.Set(0, 0, color.RGBA{B: 255, A: 255})
	var buffer bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&buffer, value, nil)
	case "gif":
		err = gif.Encode(&buffer, value, nil)
	default:
		t.Fatalf("unsupported fixture format %q", format)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestUploadAssetWritesVerifiedCatalogAndDeduplicatesByDigest(t *testing.T) {
	root := t.TempDir()
	service := NewProjectService(infrastructure.NewFilesystemRepository(root))
	imageBytes := pngFixture(t)
	first, err := service.UploadAsset(imageBytes)
	if err != nil {
		t.Fatal(err)
	}
	if first.Reused || first.Asset.Type != "image" || first.Asset.MIMEType != "image/png" || first.Asset.Size != int64(len(imageBytes)) || !strings.HasPrefix(first.Asset.ID, "asset-") {
		t.Fatalf("unexpected upload result: %#v", first)
	}
	assetFile, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(first.Asset.Path)))
	if err != nil || !bytes.Equal(assetFile, imageBytes) {
		t.Fatalf("uploaded file mismatch: err=%v", err)
	}
	catalogBytes, err := os.ReadFile(filepath.Join(root, "liapoldus/assets.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog domain.AssetDocument
	if err := json.Unmarshal(catalogBytes, &catalog); err != nil || len(catalog.Items) != 1 || !reflect.DeepEqual(catalog.Items[0], first.Asset) {
		t.Fatalf("unexpected catalog: %#v err=%v", catalog, err)
	}

	second, err := service.UploadAsset(imageBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Reused || !reflect.DeepEqual(second.Asset, first.Asset) {
		t.Fatalf("identical upload was not reused: %#v", second)
	}
	assets, err := service.Assets()
	if err != nil || len(assets) != 1 {
		t.Fatalf("duplicate upload changed catalog: %#v err=%v", assets, err)
	}
}

func TestUploadAssetGeneratesResponsiveWebPVariantsAndArtifactManifest(t *testing.T) {
	root := t.TempDir()
	service := NewProjectService(infrastructure.NewFilesystemRepository(root))
	var input bytes.Buffer
	largeImage := image.NewRGBA(image.Rect(0, 0, 640, 320))
	for y := 0; y < 320; y++ {
		for x := 0; x < 640; x++ {
			largeImage.SetRGBA(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 120, A: 255})
		}
	}
	if err := png.Encode(&input, largeImage); err != nil {
		t.Fatal(err)
	}
	result, err := service.UploadAsset(input.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if result.Asset.Width != 640 || result.Asset.Height != 320 || len(result.Asset.Variants) != 1 {
		t.Fatalf("unexpected source dimensions or variant count: %#v", result.Asset)
	}
	variant := result.Asset.Variants[0]
	if variant.Width != 320 || variant.Height != 160 || variant.MIMEType != "image/webp" {
		t.Fatalf("variant did not preserve aspect ratio: %#v", variant)
	}
	variantBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(variant.Path)))
	if err != nil || int64(len(variantBytes)) != variant.Size {
		t.Fatalf("variant file missing or wrong size: err=%v size=%d", err, len(variantBytes))
	}
	config, err := xwebp.DecodeConfig(bytes.NewReader(variantBytes))
	if err != nil || config.Width != variant.Width || config.Height != variant.Height {
		t.Fatalf("invalid WebP variant dimensions: config=%#v err=%v", config, err)
	}
	manifestBytes, err := service.renderAssetManifest()
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]generatedAssetEntry
	manifestJSON := bytes.TrimPrefix(manifestBytes, []byte("export const assets = "))
	manifestJSON = manifestJSON[:bytes.Index(manifestJSON, []byte(" as const"))]
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		t.Fatal(err)
	}
	entry := manifest[result.Asset.ID]
	if len(entry.Variants) != 1 || entry.Variants[0].Width != 320 || entry.Variants[0].Source != "/"+strings.TrimPrefix(variant.Path, "public/") {
		t.Fatalf("generated runtime manifest omitted responsive image variant: %#v", entry)
	}
	if _, err := service.Assets(); err != nil {
		t.Fatalf("verified asset registry rejected generated variant: %v", err)
	}
}

func TestUploadAssetLeavesGIFOriginalWithoutVariants(t *testing.T) {
	service := NewProjectService(infrastructure.NewFilesystemRepository(t.TempDir()))
	result, err := service.UploadAsset(encodedRasterFixture(t, "gif"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Asset.Width != 2 || result.Asset.Height != 2 || len(result.Asset.Variants) != 0 {
		t.Fatalf("GIF should remain the original fallback: %#v", result.Asset)
	}
}

func TestUploadAssetAcceptsWebPThroughResponsivePipeline(t *testing.T) {
	var input bytes.Buffer
	if err := nativewebp.Encode(&input, image.NewRGBA(image.Rect(0, 0, 640, 320)), nativewebp.Options{Quality: 82}); err != nil {
		t.Fatal(err)
	}
	service := NewProjectService(infrastructure.NewFilesystemRepository(t.TempDir()))
	result, err := service.UploadAsset(input.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if result.Asset.Width != 640 || result.Asset.Height != 320 || len(result.Asset.Variants) != 1 || result.Asset.MIMEType != "image/webp" {
		t.Fatalf("WebP upload did not flow through the responsive pipeline: %#v", result.Asset)
	}
}

func TestUploadAssetSanitizesSVGBeforeHashingAndWriting(t *testing.T) {
	root := t.TempDir()
	service := NewProjectService(infrastructure.NewFilesystemRepository(root))
	unsafe := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10" onload="alert(1)"><script>alert(1)</script><rect width="10" height="10" fill="url(https://evil.test/x)"/><path id="shape" d="M0 0L10 10"/></svg>`)
	result, err := service.UploadAsset(unsafe)
	if err != nil {
		t.Fatal(err)
	}
	if result.Asset.Type != "icon" || result.Asset.MIMEType != "image/svg+xml" || filepath.Ext(result.Asset.Path) != ".svg" {
		t.Fatalf("unexpected SVG metadata: %#v", result.Asset)
	}
	sanitized, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(result.Asset.Path)))
	if err != nil {
		t.Fatal(err)
	}
	text := string(sanitized)
	for _, forbidden := range []string{"script", "onload", "evil.test", "url("} {
		if strings.Contains(text, forbidden) {
			t.Errorf("sanitized SVG retained %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `<path id="shape" d="M0 0L10 10"></path>`) {
		t.Fatalf("safe SVG geometry was not retained: %s", text)
	}
}

func TestUploadAssetRejectsUnsupportedMalformedAndOversizedContentWithoutMutation(t *testing.T) {
	root := t.TempDir()
	service := NewProjectService(infrastructure.NewFilesystemRepository(root))
	for _, input := range [][]byte{
		{}, []byte("not an image"), []byte(`<svg><path></svg>`),
		[]byte(`<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///etc/passwd">]><svg>&x;</svg>`),
	} {
		if _, err := service.UploadAsset(input); !errors.Is(err, domain.ErrAssetUploadInvalid) {
			t.Errorf("UploadAsset(%q) error = %v, want invalid upload", input, err)
		}
	}
	if _, err := service.UploadAsset(bytes.Repeat([]byte{'x'}, int(MaxAssetUploadBytes)+1)); !errors.Is(err, domain.ErrAssetTooLarge) {
		t.Fatalf("oversized upload error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "liapoldus/assets.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid upload mutated catalog: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "public/assets"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) > 0 {
		t.Fatalf("invalid upload left asset files: %#v", entries)
	}
}

func TestSanitizeSVGRemovesActiveAndExternalContent(t *testing.T) {
	input := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><a href="https://evil.test"><text>evil</text></a><image href="https://evil.test/x.png"/><g onclick="x()" style="background:url(javascript:alert(1))"><circle cx="2" cy="2" r="1" fill="#abc"/></g></svg>`)
	clean, err := sanitizeSVG(input)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(clean), "evil") || strings.Contains(string(clean), "onclick") || strings.Contains(string(clean), "javascript") || !strings.Contains(string(clean), `<circle cx="2" cy="2" r="1" fill="#abc"></circle>`) {
		t.Fatalf("unexpected sanitized output: %s", clean)
	}
}

func TestUploadAssetValidatesWebPImageHeaderAndDimensions(t *testing.T) {
	webpBytes, err := base64.StdEncoding.DecodeString("UklGRi4AAABXRUJQVlA4ICIAAACwAQCdASoCAAIAAgA0JZwCdAEO/gLsAP7xmznkR0u5AAAA")
	if err != nil {
		t.Fatal(err)
	}
	content, kind, err := validateAndNormalizeAsset(webpBytes)
	if err != nil || !bytes.Equal(content, webpBytes) || kind.mime != "image/webp" || kind.typeName != "image" {
		t.Fatalf("valid WebP rejected: kind=%#v err=%v", kind, err)
	}
	if _, _, err := validateAndNormalizeAsset([]byte("RIFF\x00\x00\x00\x00WEBPVP8 ")); !errors.Is(err, domain.ErrAssetUploadInvalid) {
		t.Fatalf("truncated WebP error = %v", err)
	}
}

func TestUploadAssetAcceptsOnlyValidSupportedRasterEncodings(t *testing.T) {
	fixtures := []struct {
		bytes []byte
		mime  string
		ext   string
	}{
		{bytes: pngFixture(t), mime: "image/png", ext: ".png"},
		{bytes: encodedRasterFixture(t, "jpeg"), mime: "image/jpeg", ext: ".jpg"},
		{bytes: encodedRasterFixture(t, "gif"), mime: "image/gif", ext: ".gif"},
	}
	for _, fixture := range fixtures {
		_, kind, err := validateAndNormalizeAsset(fixture.bytes)
		if err != nil || kind.mime != fixture.mime || "."+kind.ext != fixture.ext {
			t.Errorf("asset format %s: kind=%#v err=%v", fixture.mime, kind, err)
		}
	}
	if _, _, err := validateAndNormalizeAsset([]byte("GIF89a but not an image")); !errors.Is(err, domain.ErrAssetUploadInvalid) {
		t.Fatalf("malformed GIF error = %v", err)
	}
}
