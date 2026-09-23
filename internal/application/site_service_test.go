package application

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Liapoldus/Constructor/internal/domain"
)

func TestCreateSiteCopiesPagesAndLocalesAndCreatesEmptyContentAtomically(t *testing.T) {
	manifest := `{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home","about"],"components":[],"react":{"entry":"src/app.tsx"}}`
	source := `{"schemaVersion":1,"id":"site-a","projectId":"demo","name":"Main","pages":[{"id":"home","name":"Home"},{"id":"about","name":"About"}],"locales":["ru-RU","en-US"]}`
	repository := &pageRepository{files: map[string][]byte{
		"liapoldus/project.json":      []byte(manifest),
		"liapoldus/sites/site-a.json": []byte(source),
	}}
	created, err := NewProjectService(repository).CreateSite(CreateSiteRequest{
		ID: "secondary", Name: "Secondary", SourceSiteID: "site-a",
		Revisions: SiteRevisions{Manifest: pageTestRevision(manifest), Source: pageTestRevision(source)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "secondary" || created.Name != "Secondary" || len(created.Pages) != 2 || len(created.Locales) != 2 {
		t.Fatalf("unexpected created Site: %#v", created)
	}
	for _, locale := range []string{"ru-RU", "en-US"} {
		path := "liapoldus/content/secondary/" + locale + ".json"
		file, err := repository.Read(path)
		if err != nil {
			t.Fatalf("content for %s was not created: %v", locale, err)
		}
		var document domain.ContentDocument
		if err := json.Unmarshal(file.Content, &document); err != nil {
			t.Fatal(err)
		}
		if document.SchemaVersion != 1 || document.ID == "" || len(document.Instances) != 0 {
			t.Fatalf("unexpected initial content for %s: %#v", locale, document)
		}
	}
}

func TestDisablingSiteLocalePreservesItsContentDocument(t *testing.T) {
	manifest := `{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":[],"react":{"entry":"src/app.tsx"}}`
	site := `{"schemaVersion":1,"id":"site-a","projectId":"demo","name":"Site A","pages":["home"],"locales":["en-US","ru-RU"]}`
	russianContent := `{"schemaVersion":1,"id":"site-a-ru-content","instances":[]}`
	repository := &pageRepository{files: map[string][]byte{
		"liapoldus/project.json":              []byte(manifest),
		"liapoldus/sites/site-a.json":         []byte(site),
		"liapoldus/content/site-a/ru-RU.json": []byte(russianContent),
	}}
	service := NewProjectService(repository)
	current, err := service.Read("liapoldus/sites/site-a.json")
	if err != nil {
		t.Fatal(err)
	}
	candidate := []byte(`{"schemaVersion":1,"id":"site-a","projectId":"demo","name":"Site A","pages":["home"],"locales":["en-US"]}`)
	if _, err := service.Write("liapoldus/sites/site-a.json", current.Revision, candidate); err != nil {
		t.Fatalf("disable locale failed: %v", err)
	}
	if got := string(repository.files["liapoldus/content/site-a/ru-RU.json"]); got != russianContent {
		t.Fatalf("disabling locale deleted or modified its content: %q", got)
	}
}

func TestCreateSiteRejectsStaleRevisionWithoutPartialFiles(t *testing.T) {
	manifest := `{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":[],"react":{"entry":"src/app.tsx"}}`
	source := `{"schemaVersion":1,"id":"site-a","projectId":"demo","name":"Main","pages":[{"id":"home","name":"Home"}],"locales":["ru-RU"]}`
	repository := &pageRepository{files: map[string][]byte{
		"liapoldus/project.json":      []byte(manifest),
		"liapoldus/sites/site-a.json": []byte(source),
	}}
	_, err := NewProjectService(repository).CreateSite(CreateSiteRequest{
		ID: "secondary", Name: "Secondary", SourceSiteID: "site-a",
		Revisions: SiteRevisions{Manifest: "stale", Source: pageTestRevision(source)},
	})
	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale revision error = %v, want conflict", err)
	}
	if len(repository.files) != 2 {
		t.Fatalf("stale create partially wrote files: %#v", repository.files)
	}
}

func TestCreateSiteRejectsStaleSourceSiteRevision(t *testing.T) {
	manifest := `{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":[],"react":{"entry":"src/app.tsx"}}`
	source := `{"schemaVersion":1,"id":"site-a","projectId":"demo","name":"Main","pages":[{"id":"home","name":"Home"}],"locales":["ru-RU"]}`
	repository := &pageRepository{files: map[string][]byte{
		"liapoldus/project.json":      []byte(manifest),
		"liapoldus/sites/site-a.json": []byte(source),
	}}
	_, err := NewProjectService(repository).CreateSite(CreateSiteRequest{
		ID: "secondary", Name: "Secondary", SourceSiteID: "site-a",
		Revisions: SiteRevisions{Manifest: pageTestRevision(manifest), Source: "stale"},
	})
	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) || conflict.Path != "liapoldus/sites/site-a.json" {
		t.Fatalf("stale source revision error = %v, want source Site conflict", err)
	}
	if len(repository.files) != 2 {
		t.Fatalf("stale source revision partially wrote files: %#v", repository.files)
	}
}

func TestCreateSiteRejectsPreexistingContentWithoutOverwriting(t *testing.T) {
	manifest := `{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":[],"react":{"entry":"src/app.tsx"}}`
	source := `{"schemaVersion":1,"id":"site-a","projectId":"demo","name":"Main","pages":[{"id":"home","name":"Home"}],"locales":["ru-RU"]}`
	preexisting := []byte(`{"schemaVersion":1,"id":"existing","instances":[]}`)
	repository := &pageRepository{files: map[string][]byte{
		"liapoldus/project.json":                 []byte(manifest),
		"liapoldus/sites/site-a.json":            []byte(source),
		"liapoldus/content/secondary/ru-RU.json": preexisting,
	}}
	_, err := NewProjectService(repository).CreateSite(CreateSiteRequest{
		ID: "secondary", Name: "Secondary", SourceSiteID: "site-a",
		Revisions: SiteRevisions{Manifest: pageTestRevision(manifest), Source: pageTestRevision(source)},
	})
	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("existing content error = %v, want conflict", err)
	}
	if string(repository.files["liapoldus/content/secondary/ru-RU.json"]) != string(preexisting) {
		t.Fatal("create Site overwrote pre-existing content")
	}
	if _, exists := repository.files["liapoldus/sites/secondary.json"]; exists {
		t.Fatal("partial Site document remains after preflight conflict")
	}
}
