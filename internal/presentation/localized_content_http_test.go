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
	"github.com/Liapoldus/Constructor/internal/domain"
	"github.com/Liapoldus/Constructor/internal/infrastructure"
)

func TestLocalizedContentEndpointLoadsResolvedViewAndAtomicallySplitsWrites(t *testing.T) {
	root := t.TempDir()
	for relative, content := range map[string]string{
		"liapoldus/project.json":                `{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":["hero"],"react":{"entry":"src/app.tsx"}}`,
		"liapoldus/sites/site-a.json":           `{"schemaVersion":1,"id":"site-a","projectId":"demo","name":"Site A","pages":["home"],"locales":["fr-FR"]}`,
		"liapoldus/content/site-a/default.json": `{"schemaVersion":1,"id":"default-content","instances":[{"id":"hero-one","pageId":"home","component":"hero","fields":{"brand":"Common","title":"Base"}}]}`,
		"liapoldus/content/site-a/fr-FR.json":   `{"schemaVersion":1,"id":"fr-content","instances":[{"id":"hero-one","pageId":"home","component":"hero","fields":{"title":"Bonjour"}}]}`,
		"src/components/hero/schema.json":       `{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"brand","type":"text","label":"Brand"},{"key":"title","type":"text","label":"Title","localized":true}]}`,
		"src/components/hero/component.tsx":     `export default function Hero(){return null}`,
	} {
		target := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	projects := application.NewProjectService(infrastructure.NewFilesystemRepository(root))
	auth := application.NewAuthService(previewTestAuthorizer{})
	handler := NewHandler(projects, nil, nil, nil, nil, nil, nil, nil, nil, nil, auth, nil)
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/project/content?siteId=site-a&locale=fr-FR", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
	}
	var view application.LocalizedContentView
	if err := json.Unmarshal(get.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	fields := view.Document.Instances[0].Fields
	if fields["brand"] != "Common" || fields["title"] != "Bonjour" || view.ContentRevision == "" || view.DefaultContentRevision == "" {
		t.Fatalf("GET did not return resolved content/revisions: %#v", view)
	}
	base := view.Document
	candidate := view.Document
	candidate.Instances = append([]domain.ContentInstance(nil), view.Document.Instances...)
	candidate.Instances[0].Fields = map[string]any{"brand": "Commun", "title": "Salut"}
	body, err := json.Marshal(map[string]any{"base": base, "document": candidate, "revisions": map[string]string{"locale": view.ContentRevision, "default": view.DefaultContentRevision}})
	if err != nil {
		t.Fatal(err)
	}
	putRequest := httptest.NewRequest(http.MethodPut, "/api/v1/project/content?siteId=site-a&locale=fr-FR", bytes.NewReader(body))
	put := httptest.NewRecorder()
	handler.ServeHTTP(put, putRequest)
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", put.Code, put.Body.String())
	}
	var shared, localized domain.ContentDocument
	for locale, target := range map[string]*domain.ContentDocument{"default": &shared, "fr-FR": &localized} {
		content, err := os.ReadFile(filepath.Join(root, "liapoldus", "content", "site-a", locale+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(content, target); err != nil {
			t.Fatal(err)
		}
	}
	if shared.Instances[0].Fields["brand"] != "Commun" || shared.Instances[0].Fields["title"] != "Base" || localized.Instances[0].Fields["title"] != "Salut" {
		t.Fatalf("PUT did not split values into canonical documents: shared=%#v localized=%#v", shared, localized)
	}
}
