package domain

import (
	"encoding/json"
	"testing"
)

func TestValidateComponentContent(t *testing.T) {
	schema := ComponentSchema{ID: "hero", Fields: []ComponentField{{Key: "title", Type: "text", Label: "Title", Required: true}, {Key: "layout", Type: "select", Label: "Layout", AllowedValues: []any{"wide", "compact"}}}}
	if got := ValidateComponentContent(schema, map[string]any{"title": 42, "layout": "unknown"}, "content.json"); len(got) != 2 {
		t.Fatalf("expected type and select diagnostics, got %#v", got)
	}
	if got := ValidateComponentContent(schema, map[string]any{}, "content.json"); len(got) != 1 || got[0].Code != "content.required" {
		t.Fatalf("expected required diagnostic, got %#v", got)
	}
}

func TestValidateComponentContentHonorsDefaultsNullabilityAndReferenceTypes(t *testing.T) {
	schema := ComponentSchema{Fields: []ComponentField{
		{Key: "title", Type: "text", Required: true, Default: "Untitled"},
		{Key: "subtitle", Type: "text", Nullable: true},
		{Key: "photo", Type: "image"},
		{Key: "target", Type: "reference"},
		{Key: "mode", Type: "select", AllowedValues: []any{"compact", float64(2), true}},
	}}
	valid := map[string]any{
		"subtitle": nil,
		"photo":    map[string]any{"kind": "image", "id": "hero-image", "alt": "Hero"},
		"target":   map[string]any{"kind": "reference", "type": "page", "id": "home"},
		"mode":     float64(2),
	}
	if diagnostics := ValidateComponentContent(schema, valid, "content.json"); len(diagnostics) != 0 {
		t.Fatalf("valid defaults/references produced diagnostics: %#v", diagnostics)
	}
	valid["subtitle"] = nil
	valid["photo"] = map[string]any{"kind": "file", "id": "document"}
	if diagnostics := ValidateComponentContent(ComponentSchema{Fields: []ComponentField{{Key: "subtitle", Type: "text"}, {Key: "photo", Type: "image"}}}, valid, "content.json"); len(diagnostics) != 2 {
		t.Fatalf("expected nullability and reference-kind errors, got %#v", diagnostics)
	}
}

func TestRequiredLocalizedFieldsCanBeDraftedButFailStrictValidation(t *testing.T) {
	schema := ComponentSchema{Fields: []ComponentField{{Key: "title", Type: "text", Required: true, Localized: true}}}
	instance := ContentInstance{ID: "hero-main", PageID: "home", Component: "hero", Fields: map[string]any{}}
	if diagnostics := ValidateInstanceContentForWrite(schema, instance, "content.json"); len(diagnostics) != 0 {
		t.Fatalf("incomplete localized field should be saveable as a draft: %#v", diagnostics)
	}
	diagnostics := ValidateInstanceContent(schema, instance, "content.json")
	if len(diagnostics) != 1 || diagnostics[0].Code != "content.required" || diagnostics[0].FieldKey != "title" {
		t.Fatalf("strict locale validation should require localized title: %#v", diagnostics)
	}
	instance.Fields["title"] = ""
	if diagnostics := ValidateInstanceContentForWrite(schema, instance, "content.json"); len(diagnostics) != 0 {
		t.Fatalf("empty localized field should remain draftable: %#v", diagnostics)
	}
	if diagnostics := ValidateInstanceContent(schema, instance, "content.json"); len(diagnostics) != 1 || diagnostics[0].Code != "content.required" {
		t.Fatalf("strict locale validation should reject empty localized title: %#v", diagnostics)
	}
}

func TestValidateComponentSchemaRejectsUndeclaredPropertiesAndTypes(t *testing.T) {
	path := "src/components/hero/schema.json"
	diagnostics, err := ValidateComponentSchema([]byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","unexpected":true,"fields":[]}`), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "component.invalid-json" {
		t.Fatalf("expected strict unknown property rejection, got %#v", diagnostics)
	}
	diagnostics, err = ValidateComponentSchema([]byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"unsupported","label":"Title"}]}`), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "component.field-type" {
			return
		}
	}
	t.Fatalf("expected unsupported field type diagnostic, got %#v", diagnostics)
}

func TestValidateComponentSchemaChecksFieldDefaultsAndAllowedValues(t *testing.T) {
	diagnostics, err := ValidateComponentSchema([]byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"variant","type":"select","label":"Variant","allowedValues":["small",2],"default":"large"}]}`), "schema.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "component.field-default" {
			return
		}
	}
	t.Fatalf("expected unsupported default diagnostic, got %#v", diagnostics)
}

func TestValidateComponentSchemaAcceptsAndValidatesThemeTokens(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[],"themeTokens":["colors.primary","spacing.medium"]}`)
	diagnostics, err := ValidateComponentSchema(raw, "src/components/hero/schema.json")
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("valid themeTokens rejected: diagnostics=%#v err=%v", diagnostics, err)
	}
	var schema ComponentSchema
	if err := json.Unmarshal(raw, &schema); err != nil || len(schema.ThemeTokens) != 2 {
		t.Fatalf("themeTokens did not decode: schema=%#v err=%v", schema, err)
	}
	raw = []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[],"themeTokens":["colors.primary","colors.primary","not a token"]}`)
	diagnostics, err = ValidateComponentSchema(raw, "src/components/hero/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, diagnostic := range diagnostics {
		codes[diagnostic.Code] = true
	}
	if !codes["component.theme-token-duplicate"] || !codes["component.theme-token-path"] {
		t.Fatalf("expected duplicate and malformed token diagnostics, got %#v", diagnostics)
	}
}
