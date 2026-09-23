package domain

import "testing"

func TestDecodeContentDocumentRejectsUnknownFields(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"id":"home-content","unexpected":true,"instances":[]}`)
	_, diagnostics := DecodeContentDocument(raw, "liapoldus/content/site/ru-RU.json")
	if len(diagnostics) != 1 || diagnostics[0].Code != "content.invalid-document" {
		t.Fatalf("expected unknown-field rejection, got %#v", diagnostics)
	}
}

func TestDecodeContentDocumentRejectsDuplicateInstances(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{}},{"id":"hero-main","pageId":"home","component":"hero","fields":{}}]}`)
	_, diagnostics := DecodeContentDocument(raw, "liapoldus/content/site/ru-RU.json")
	if len(diagnostics) != 1 || diagnostics[0].Code != "content.instance-duplicate" {
		t.Fatalf("expected duplicate-instance diagnostic, got %#v", diagnostics)
	}
}

func TestValidateInstanceContentRejectsUndeclaredField(t *testing.T) {
	schema := ComponentSchema{ID: "hero", Fields: []ComponentField{{Key: "title", Type: "text", Label: "Title"}}}
	instance := ContentInstance{ID: "hero-main", PageID: "home", Component: "hero", Fields: map[string]any{"title": "Welcome", "debug": true}}
	diagnostics := ValidateInstanceContent(schema, instance, "content.json")
	if len(diagnostics) != 1 || diagnostics[0].Code != "content.unknown-field" {
		t.Fatalf("expected unknown-field diagnostic, got %#v", diagnostics)
	}
	if diagnostics[0].PageID != "home" || diagnostics[0].InstanceID != "hero-main" || diagnostics[0].FieldKey != "debug" {
		t.Fatalf("diagnostic is missing content source coordinates: %#v", diagnostics[0])
	}
}

func TestValidateInstanceContentPointsToMissingRequiredField(t *testing.T) {
	schema := ComponentSchema{ID: "hero", Fields: []ComponentField{{Key: "title", Type: "text", Label: "Title", Required: true}}}
	instance := ContentInstance{ID: "hero-main", PageID: "home", Component: "hero", Fields: map[string]any{}}
	diagnostics := ValidateInstanceContent(schema, instance, "content.json")
	if len(diagnostics) != 1 || diagnostics[0].Code != "content.required" || diagnostics[0].FieldKey != "title" || diagnostics[0].InstanceID != instance.ID {
		t.Fatalf("required field diagnostic has wrong source: %#v", diagnostics)
	}
}

func TestDecodeContentDocumentRequiresPageID(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","component":"hero","fields":{}}]}`)
	_, diagnostics := DecodeContentDocument(raw, "content.json")
	if len(diagnostics) != 1 || diagnostics[0].Code != "content.page-required" {
		t.Fatalf("expected pageId requirement diagnostic, got %#v", diagnostics)
	}
}
