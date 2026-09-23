package domain

import "testing"

func TestDecodeProjectDocumentRequiresCanonicalVersionedManifest(t *testing.T) {
	valid := []byte(`{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":["Hero"],"react":{"entry":"src/app.tsx"}}`)
	project, diagnostics := DecodeProjectDocument(valid, "liapoldus/project.json")
	if len(diagnostics) != 0 || project.ID != "demo" {
		t.Fatalf("valid project manifest rejected: %#v %#v", project, diagnostics)
	}
	invalid := []byte(`{"schemaVersion":1,"id":"demo","name":"Demo","pages":["Home"],"components":[],"react":{"entry":"src/app.tsx"},"unexpected":true}`)
	_, diagnostics = DecodeProjectDocument(invalid, "liapoldus/project.json")
	if len(diagnostics) != 1 || diagnostics[0].Code != "project.invalid-document" {
		t.Fatalf("unknown manifest property was not rejected: %#v", diagnostics)
	}
	invalid = []byte(`{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home","home"],"components":[],"react":{"entry":"src/app.tsx"}}`)
	_, diagnostics = DecodeProjectDocument(invalid, "liapoldus/project.json")
	if len(diagnostics) != 1 || diagnostics[0].Code != "project.page-duplicate" {
		t.Fatalf("duplicate page IDs were not rejected: %#v", diagnostics)
	}
	invalid = []byte(`{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":["bad component"],"react":{"entry":"src/app.tsx"}}`)
	_, diagnostics = DecodeProjectDocument(invalid, "liapoldus/project.json")
	if len(diagnostics) != 1 || diagnostics[0].Code != "project.component-id" {
		t.Fatalf("unsafe component IDs were not rejected: %#v", diagnostics)
	}
	invalid = []byte(`{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":["Hero","hero"],"react":{"entry":"src/app.tsx"}}`)
	_, diagnostics = DecodeProjectDocument(invalid, "liapoldus/project.json")
	if len(diagnostics) != 1 || diagnostics[0].Code != "project.component-duplicate" {
		t.Fatalf("case-insensitive duplicate component IDs were not rejected: %#v", diagnostics)
	}
}
