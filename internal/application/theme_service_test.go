package application

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Liapoldus/Constructor/internal/domain"
	"github.com/Liapoldus/Constructor/internal/infrastructure"
)

func TestSiteThemeReferenceIsCheckedBeforeWrite(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"liapoldus/project.json":    `{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":[],"react":{"entry":"src/app.tsx"}}`,
		"liapoldus/sites/main.json": `{"schemaVersion":1,"id":"main","projectId":"demo","name":"Main","pages":["home"],"locales":["ru-RU"]}`,
	}
	for relative, content := range files {
		filePath := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	service := NewProjectService(infrastructure.NewFilesystemRepository(root))
	current, err := service.Read("liapoldus/sites/main.json")
	if err != nil {
		t.Fatal(err)
	}
	candidate := []byte(`{"schemaVersion":1,"id":"main","projectId":"demo","name":"Main","themeId":"missing","pages":["home"],"locales":["ru-RU"]}`)
	_, err = service.Write("liapoldus/sites/main.json", current.Revision, candidate)
	var validation domain.StructuredValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("missing Theme reference was not rejected with typed diagnostics: %v", err)
	}
	found := false
	for _, diagnostic := range validation.Diagnostics {
		found = found || diagnostic.Code == "site.theme-not-found"
	}
	if !found {
		t.Fatalf("missing site.theme-not-found diagnostic: %#v", validation.Diagnostics)
	}
	unchanged, err := service.Read("liapoldus/sites/main.json")
	if err != nil || string(unchanged.Content) != files["liapoldus/sites/main.json"] {
		t.Fatalf("rejected Theme reference mutated Site file: err=%v content=%s", err, unchanged.Content)
	}
}

func TestCreateSiteCopiesThemeSelection(t *testing.T) {
	manifest := `{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":[],"react":{"entry":"src/app.tsx"}}`
	source := `{"schemaVersion":1,"id":"site-a","projectId":"demo","name":"Main","themeId":"brand","pages":["home"],"locales":["ru-RU"]}`
	repository := &pageRepository{files: map[string][]byte{
		"liapoldus/project.json": []byte(manifest), "liapoldus/sites/site-a.json": []byte(source),
	}}
	created, err := NewProjectService(repository).CreateSite(CreateSiteRequest{
		ID: "secondary", Name: "Secondary", SourceSiteID: "site-a",
		Revisions: SiteRevisions{Manifest: pageTestRevision(manifest), Source: pageTestRevision(source)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ThemeID != "brand" {
		t.Fatalf("new Site did not inherit source Theme selection: %#v", created)
	}
	var persisted domain.SiteDocument
	if err := json.Unmarshal(repository.files["liapoldus/sites/secondary.json"], &persisted); err != nil || persisted.ThemeID != "brand" {
		t.Fatalf("persisted Site omitted Theme selection: %#v err=%v", persisted, err)
	}
}

func TestThemeTokenReferencesBlockComponentAndThemeWrites(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"liapoldus/project.json": `{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":["hero"],"react":{"entry":"src/app.tsx"}}`,
		"liapoldus/sites/main.json": `{"schemaVersion":1,"id":"main","projectId":"demo","name":"Main","themeId":"brand","pages":["home"],"locales":["ru-RU"]}`,
		"liapoldus/themes/brand.json": `{"schemaVersion":1,"id":"brand","name":"Brand","tokens":{"colors.primary":{"type":"color","value":"#123456"},"spacing.medium":{"type":"length","value":"16px"}}}`,
		"src/components/hero/schema.json": `{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[],"themeTokens":["colors.primary"]}`,
		"src/components/hero/component.tsx": `export default function Hero(){return null}`,
	}
	for relative, content := range files {
		filePath := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	service := NewProjectService(infrastructure.NewFilesystemRepository(root))
	assertRejected := func(documentPath string, replacement string, diagnosticCode string) {
		t.Helper()
		current, err := service.Read(documentPath)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Write(documentPath, current.Revision, []byte(replacement))
		var validation domain.StructuredValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("write %s should be rejected with diagnostics; err=%v", documentPath, err)
		}
		for _, item := range validation.Diagnostics {
			if item.Code == diagnosticCode {
				return
			}
		}
		t.Fatalf("write %s missed diagnostic %s: %#v", documentPath, diagnosticCode, validation.Diagnostics)
	}
	assertRejected("liapoldus/themes/brand.json", `{"schemaVersion":1,"id":"brand","name":"Brand","tokens":{"spacing.medium":{"type":"length","value":"16px"}}}`, "component.theme-token-reference")
	assertRejected("src/components/hero/schema.json", `{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[],"themeTokens":["colors.missing"]}`, "component.theme-token-reference")
}
