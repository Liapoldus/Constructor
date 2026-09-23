package application

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Liapoldus/Constructor/internal/domain"
)

func localizedContentFixture() map[string][]byte {
	files := generatorFiles([]byte(`{"schemaVersion":1,"id":"ru-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Русский"}}]}`))
	files["src/components/hero/schema.json"] = []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"brand","type":"text","label":"Brand"},{"key":"title","type":"text","label":"Title","localized":true}]}`)
	files["liapoldus/content/site-one/default.json"] = []byte(`{"schemaVersion":1,"id":"default-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"brand":"Shared","title":"Fallback"}}]}`)
	files["liapoldus/content/site-one/en-US.json"] = []byte(`{"schemaVersion":1,"id":"en-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"English"}}]}`)
	return files
}

func TestSaveLocalizedContentSplitsFieldsAndPreservesOtherLocales(t *testing.T) {
	repository := &generatorRepository{files: localizedContentFixture()}
	service := NewProjectService(repository)
	view, err := service.LoadLocalizedContent("site-one", "ru-RU")
	if err != nil {
		t.Fatal(err)
	}
	fields := view.Document.Instances[0].Fields
	if fields["brand"] != "Shared" || fields["title"] != "Русский" || view.ContentRevision != "revision" || view.DefaultContentRevision != "revision" {
		t.Fatalf("unexpected resolved view: %#v", view)
	}
	base := view.Document
	candidate := view.Document
	candidate.Instances = append([]domain.ContentInstance(nil), view.Document.Instances...)
	candidate.Instances[0].Fields = map[string]any{"brand": "Shared update", "title": "Русский update"}
	if _, err := service.SaveLocalizedContent("site-one", "ru-RU", base, candidate, "revision", "revision"); err != nil {
		t.Fatal(err)
	}
	decode := func(locale string) domain.ContentDocument {
		t.Helper()
		var document domain.ContentDocument
		if err := json.Unmarshal(repository.files["liapoldus/content/site-one/"+locale+".json"], &document); err != nil {
			t.Fatal(err)
		}
		return document
	}
	defaultDoc := decode("default")
	ruDoc := decode("ru-RU")
	enDoc := decode("en-US")
	if defaultDoc.Instances[0].Fields["brand"] != "Shared update" || defaultDoc.Instances[0].Fields["title"] != "Fallback" {
		t.Fatalf("default values were not split/preserved: %#v", defaultDoc.Instances[0].Fields)
	}
	if ruDoc.Instances[0].Fields["title"] != "Русский update" {
		t.Fatalf("localized value was not written to selected locale: %#v", ruDoc.Instances[0].Fields)
	}
	if _, exists := ruDoc.Instances[0].Fields["brand"]; exists {
		t.Fatalf("shared value leaked into locale document: %#v", ruDoc.Instances[0].Fields)
	}
	if enDoc.Instances[0].Fields["title"] != "English" || contentStructure(defaultDoc) != contentStructure(ruDoc) || contentStructure(ruDoc) != contentStructure(enDoc) {
		t.Fatalf("other locale or shared structure changed: en=%#v", enDoc)
	}
}

func TestSaveLocalizedContentRejectsStaleDefaultWithoutPartialWrite(t *testing.T) {
	files := localizedContentFixture()
	repository := &generatorRepository{files: files}
	service := NewProjectService(repository)
	view, err := service.LoadLocalizedContent("site-one", "ru-RU")
	if err != nil {
		t.Fatal(err)
	}
	candidate := view.Document
	candidate.Instances = append([]domain.ContentInstance(nil), view.Document.Instances...)
	candidate.Instances[0].Fields = map[string]any{"brand": "Changed", "title": "Русский"}
	_, err = service.SaveLocalizedContent("site-one", "ru-RU", view.Document, candidate, "revision", "stale")
	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) || conflict.Path != "liapoldus/content/site-one/default.json" {
		t.Fatalf("expected default revision conflict, got %v", err)
	}
	if repository.batches != 0 || string(repository.files["liapoldus/content/site-one/default.json"]) != string(files["liapoldus/content/site-one/default.json"]) {
		t.Fatal("stale default revision caused a partial write")
	}
}

func TestSaveLocalizedContentPropagatesStructureAcrossLocaleDocuments(t *testing.T) {
	repository := &generatorRepository{files: localizedContentFixture()}
	service := NewProjectService(repository)
	view, err := service.LoadLocalizedContent("site-one", "ru-RU")
	if err != nil {
		t.Fatal(err)
	}
	candidate := view.Document
	candidate.Instances = append([]domain.ContentInstance(nil), view.Document.Instances...)
	candidate.Instances = append(candidate.Instances, domain.ContentInstance{ID: "hero-secondary", PageID: "about", Component: "hero", Fields: map[string]any{"brand": "Shared secondary", "title": "Локальный secondary"}})
	if _, err := service.SaveLocalizedContent("site-one", "ru-RU", view.Document, candidate, "revision", "revision"); err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"default", "ru-RU", "en-US"} {
		var document domain.ContentDocument
		if err := json.Unmarshal(repository.files["liapoldus/content/site-one/"+locale+".json"], &document); err != nil {
			t.Fatal(err)
		}
		if len(document.Instances) != 2 || document.Instances[1].ID != "hero-secondary" {
			t.Fatalf("structure did not propagate to %s: %#v", locale, document.Instances)
		}
	}
}

func TestDefaultEditingScopeChangesFallbackWithoutOverwritingLocales(t *testing.T) {
	repository := &generatorRepository{files: localizedContentFixture()}
	service := NewProjectService(repository)
	view, err := service.LoadLocalizedContent("site-one", "default")
	if err != nil {
		t.Fatal(err)
	}
	if view.Document.Instances[0].Fields["title"] != "Fallback" {
		t.Fatalf("default editing scope did not resolve default localized value: %#v", view.Document)
	}
	candidate := view.Document
	candidate.Instances = append([]domain.ContentInstance(nil), view.Document.Instances...)
	candidate.Instances[0].Fields = map[string]any{"brand": "Shared", "title": "New fallback"}
	if _, err := service.SaveLocalizedContent("site-one", "default", view.Document, candidate, "revision", "revision"); err != nil {
		t.Fatal(err)
	}
	var defaultDoc, localeDoc domain.ContentDocument
	if err := json.Unmarshal(repository.files["liapoldus/content/site-one/default.json"], &defaultDoc); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(repository.files["liapoldus/content/site-one/ru-RU.json"], &localeDoc); err != nil {
		t.Fatal(err)
	}
	if defaultDoc.Instances[0].Fields["title"] != "New fallback" || localeDoc.Instances[0].Fields["title"] != "Русский" {
		t.Fatalf("default update overwrote a locale override: default=%#v locale=%#v", defaultDoc, localeDoc)
	}
}
