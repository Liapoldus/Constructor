package domain

import (
	"strings"
	"testing"
)

func TestDecodeAssetDocumentAcceptsVersionedSafeMetadata(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"id":"assets","items":[{"id":"hero-image","type":"image","path":"public/assets/hero/hero-640.webp","mimeType":"image/webp","size":1234,"sha256":"` + strings.Repeat("a", 64) + `"}]}`)
	document, diagnostics := DecodeAssetDocument(raw, "liapoldus/assets.json")
	if len(diagnostics) != 0 {
		t.Fatalf("valid asset metadata rejected: %#v", diagnostics)
	}
	if len(document.Items) != 1 || document.Items[0].ID != "hero-image" || document.Items[0].Path != "public/assets/hero/hero-640.webp" {
		t.Fatalf("asset metadata was not decoded: %#v", document)
	}
}

func TestDecodeAssetDocumentRejectsUnknownDuplicateAndUnsafeMetadata(t *testing.T) {
	for _, raw := range []string{
		`{"schemaVersion":2,"id":"assets","items":[]}`,
		`{"schemaVersion":1,"id":"assets","items":[],"extra":true}`,
		`{"schemaVersion":1,"id":"assets","items":[{"id":"same","type":"image","path":"public/assets/one.png","mimeType":"image/png","size":1,"sha256":"` + strings.Repeat("a", 64) + `"},{"id":"same","type":"image","path":"public/assets/two.png","mimeType":"image/png","size":1,"sha256":"` + strings.Repeat("b", 64) + `"}]}`,
		`{"schemaVersion":1,"id":"assets","items":[{"id":"hero-image","type":"image","path":"public/assets/../secret.png","mimeType":"image/png","size":1,"sha256":"` + strings.Repeat("a", 64) + `"}]}`,
		`{"schemaVersion":1,"id":"assets","items":[{"id":"hero-image","type":"image","path":"public/assets/hero.png","mimeType":"image/png","size":1,"sha256":"not-a-digest"}]}`,
	} {
		_, diagnostics := DecodeAssetDocument([]byte(raw), "liapoldus/assets.json")
		if len(diagnostics) == 0 {
			t.Fatalf("invalid asset metadata accepted: %s", raw)
		}
	}
}
