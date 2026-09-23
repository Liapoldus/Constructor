package application

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Liapoldus/Constructor/internal/domain"
	"sort"
	"strings"
	"testing"
)

type generatorRepository struct {
	files   map[string][]byte
	batches int
}

func (r *generatorRepository) Manifest() (domain.Project, string, error) {
	var project domain.Project
	project.SchemaVersion = 1
	project.ID = "demo"
	project.Name = "Demo"
	project.Pages = []string{"home", "about"}
	project.Components = []string{"Hero", "Cta"}
	project.React.Entry = "src/app.tsx"
	return project, "manifest", nil
}

func (r *generatorRepository) List(prefix string) ([]string, error) {
	var paths []string
	for filePath := range r.files {
		if strings.HasPrefix(filePath, prefix) {
			paths = append(paths, filePath)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (r *generatorRepository) ApplyBatch(changes []domain.ProjectFileChange) error {
	r.batches++
	for _, change := range changes {
		_, exists := r.files[change.Path]
		if exists && change.ExpectedRevision != "revision" {
			return domain.ErrConflict
		}
		if !exists && change.ExpectedRevision != "" {
			return domain.ErrConflict
		}
	}
	for _, change := range changes {
		if change.Delete {
			delete(r.files, change.Path)
		} else {
			r.files[change.Path] = append([]byte(nil), change.Content...)
		}
	}
	return nil
}

func generatorFiles(content []byte) map[string][]byte {
	return map[string][]byte{
		"liapoldus/content/site-one/ru-RU.json": content,
		"liapoldus/sites/site-one.json":         []byte(`{"schemaVersion":1,"id":"site-one","projectId":"demo","name":"Demo","pages":["home","about"],"locales":["ru-RU"]}`),
		"src/components/hero/schema.json":       []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title","localized":true}]}`),
		"src/components/cta/schema.json":        []byte(`{"schemaVersion":1,"id":"cta","kind":"component","source":"src/components/cta/component.tsx","fields":[{"key":"label","type":"text","label":"Label","localized":true}]}`),
		"src/components/hero/component.tsx":     []byte("export default function Hero(){return null}"),
		"src/components/cta/component.tsx":      []byte("export default function Cta(){return null}"),
	}
}

func TestGenerateLocaleReturnsTypedValidationDiagnostics(t *testing.T) {
	repository := &generatorRepository{files: generatorFiles([]byte(`{"schemaVersion":1,"id":"home-content","unknown":true,"instances":[]}`))}
	_, err := NewProjectService(repository).GenerateLocale("site-one", "ru-RU")
	var validation domain.ContentValidationError
	if !errors.As(err, &validation) || len(validation.Diagnostics) == 0 || validation.Diagnostics[0].Code != "content.invalid-document" {
		t.Fatalf("expected typed content diagnostics, got %v", err)
	}
	if _, exists := repository.files["src/generated/content/ru-RU.json"]; exists {
		t.Fatal("generation wrote an artifact after validation failure")
	}
}

func TestGenerateProjectArtifactsRendersAllLocalesAndPublishesOneDeterministicBatch(t *testing.T) {
	content := []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Hello"}}]}`)
	files := generatorFiles(content)
	files["liapoldus/sites/site-one.json"] = []byte(`{"schemaVersion":1,"id":"site-one","projectId":"demo","name":"Demo","pages":[{"id":"home","name":"Home"},{"id":"about","name":"About"}],"locales":["ru-RU","en-US"]}`)
	files["liapoldus/content/site-one/en-US.json"] = []byte(strings.Replace(string(content), "Hello", "Hello in English", 1))
	files["liapoldus/routes/development.json"] = []byte(`{"schemaVersion":1,"id":"development-routes","routes":[{"id":"home-route","path":"/","page":"home"}]}`)
	files["src/pages/home.page.tsx"] = []byte("export default function Home(){return null}")
	asset := []byte("webp-fixture")
	digest := sha256.Sum256(asset)
	files["public/assets/hero.webp"] = asset
	files["liapoldus/assets.json"] = []byte(fmt.Sprintf(`{"schemaVersion":1,"id":"assets","items":[{"id":"hero-image","type":"image","path":"public/assets/hero.webp","mimeType":"image/webp","size":%d,"sha256":"%s"}]}`, len(asset), hex.EncodeToString(digest[:])))
	files["liapoldus/themes/default.json"] = []byte(`{"schemaVersion":1,"id":"default","name":"Default","tokens":{"colors.primary":{"type":"color","value":"#123456"}},"variants":{"dark":{"colors.primary":"#abcdef"}}}`)
	repository := &generatorRepository{files: files}
	artifacts, err := NewProjectService(repository).GenerateProjectArtifacts("site-one", "development")
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{"src/generated/assets.ts", "src/generated/content/en-US.json", "src/generated/content/ru-RU.json", "src/generated/routes.tsx", "src/generated/theme.css"}
	if len(artifacts) != len(wantPaths) {
		t.Fatalf("generated artifacts=%#v", artifacts)
	}
	if repository.batches != 1 {
		t.Fatalf("artifact generation published %d batches, want exactly one", repository.batches)
	}
	if !strings.Contains(string(repository.files["src/generated/assets.ts"]), `"hero-image"`) || !strings.Contains(string(repository.files["src/generated/assets.ts"]), `"/assets/hero.webp"`) {
		t.Fatalf("asset manifest does not contain verified public asset: %s", repository.files["src/generated/assets.ts"])
	}
	if themeCSS := string(repository.files["src/generated/theme.css"]); !strings.Contains(themeCSS, "--colors-primary: #123456;") || !strings.Contains(themeCSS, "--colors-primary: #abcdef;") {
		t.Fatalf("generated theme artifact omitted default Theme or dark override: %s", themeCSS)
	}
	for index, want := range wantPaths {
		if artifacts[index].Path != want {
			t.Fatalf("artifact order/path[%d]=%q, want %q", index, artifacts[index].Path, want)
		}
		if len(repository.files[want]) == 0 {
			t.Fatalf("artifact %q was not written", want)
		}
	}
	var english generatedLocale
	if err := json.Unmarshal(repository.files["src/generated/content/en-US.json"], &english); err != nil {
		t.Fatal(err)
	}
	if english.Pages["home"].Instances["hero-main"].Fields["title"] != "Hello in English" {
		t.Fatalf("wrong locale output: %#v", english)
	}
	before := map[string]string{}
	for _, path := range wantPaths {
		before[path] = string(repository.files[path])
	}
	second, err := NewProjectService(repository).GenerateProjectArtifacts("site-one", "development")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range wantPaths {
		if string(repository.files[path]) != before[path] {
			t.Fatalf("generation is not deterministic for %q", path)
		}
	}
	if len(second) != len(artifacts) {
		t.Fatalf("second batch length=%d want %d", len(second), len(artifacts))
	}
	if repository.batches != 2 {
		t.Fatalf("second generation published %d total batches, want 2", repository.batches)
	}
}

func TestGenerateThemeCSSUsesSelectedThemeAndLightDarkOverrides(t *testing.T) {
	themeJSON := `{"schemaVersion":1,"id":"brand","name":"Brand","tokens":{"colors.primary":{"type":"color","value":"#123456"},"spacing.medium":{"type":"length","value":"16px"}},"variants":{"light":{"colors.primary":"#234567"},"dark":{"colors.primary":"#abcdef","spacing.medium":"20px"}}}`
	files := generatorFiles([]byte(`{"schemaVersion":1,"id":"home-content","instances":[]}`))
	files["liapoldus/themes/brand.json"] = []byte(themeJSON)
	files["liapoldus/sites/site-one.json"] = []byte(`{"schemaVersion":1,"id":"site-one","projectId":"demo","name":"Demo","themeId":"brand","pages":["home","about"],"locales":["ru-RU"]}`)
	service := NewProjectService(&generatorRepository{files: files})
	css, err := service.renderThemeForSite(domain.SiteDocument{ID: "site-one", ThemeID: "brand"})
	if err != nil {
		t.Fatal(err)
	}
	output := string(css)
	for _, want := range []string{
		":root {", "--colors-primary: #123456;", "--spacing-medium: 16px;",
		`:root[data-color-scheme="light"]`, "--colors-primary: #234567;",
		`:root[data-color-scheme="dark"]`, "--colors-primary: #abcdef;",
		"@media (prefers-color-scheme: dark)", "--spacing-medium: 20px;",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("generated theme CSS lacks %q:\n%s", want, output)
		}
	}
	if strings.Index(output, "--colors-primary") > strings.Index(output, "--spacing-medium") {
		t.Fatalf("base CSS token order is not deterministic:\n%s", output)
	}
}

func TestThemeSelectionRequiresExplicitChoiceForMultipleThemes(t *testing.T) {
	themes := []domain.ThemeDocument{{ID: "one"}, {ID: "two"}}
	if selected, diagnostics := resolveSiteTheme(domain.SiteDocument{ID: "site-one"}, themes); selected != nil || len(diagnostics) != 1 || diagnostics[0].Code != "site.theme-required" {
		t.Fatalf("ambiguous theme selection was not rejected: selected=%#v diagnostics=%#v", selected, diagnostics)
	}
	if selected, diagnostics := resolveSiteTheme(domain.SiteDocument{ID: "site-one"}, []domain.ThemeDocument{{ID: "default"}}); selected == nil || selected.ID != "default" || len(diagnostics) != 0 {
		t.Fatalf("default theme should be inferred: selected=%#v diagnostics=%#v", selected, diagnostics)
	}
}

func TestGenerateProjectArtifactsDoesNotPublishPartialBatchWhenOneLocaleIsInvalid(t *testing.T) {
	files := generatorFiles([]byte(`{"schemaVersion":1,"id":"home-content","instances":[]}`))
	files["liapoldus/sites/site-one.json"] = []byte(`{"schemaVersion":1,"id":"site-one","projectId":"demo","name":"Demo","pages":[{"id":"home","name":"Home"}],"locales":["ru-RU","en-US"]}`)
	files["liapoldus/content/site-one/en-US.json"] = []byte(`{"schemaVersion":1,"id":"bad","unknown":true,"instances":[]}`)
	files["liapoldus/routes/development.json"] = []byte(`{"schemaVersion":1,"id":"development-routes","routes":[{"id":"home-route","path":"/","page":"home"}]}`)
	files["src/pages/home.page.tsx"] = []byte("export default function Home(){return null}")
	repository := &generatorRepository{files: files}
	if _, err := NewProjectService(repository).GenerateProjectArtifacts("site-one", "development"); err == nil {
		t.Fatal("invalid locale was accepted")
	}
	for path := range repository.files {
		if strings.HasPrefix(path, "src/generated/") {
			t.Fatalf("partial artifact %q was written", path)
		}
	}
}

func TestGenerateProjectArtifactsDoesNotPublishWhenAssetChecksumIsInvalid(t *testing.T) {
	content := []byte(`{"schemaVersion":1,"id":"home-content","instances":[]}`)
	files := generatorFiles(content)
	files["liapoldus/routes/development.json"] = []byte(`{"schemaVersion":1,"id":"development-routes","routes":[{"id":"home-route","path":"/","page":"home"}]}`)
	files["src/pages/home.page.tsx"] = []byte("export default function Home(){return null}")
	files["public/assets/hero.webp"] = []byte("not-the-catalog-bytes")
	files["liapoldus/assets.json"] = []byte(`{"schemaVersion":1,"id":"assets","items":[{"id":"hero-image","type":"image","path":"public/assets/hero.webp","mimeType":"image/webp","size":12,"sha256":"invalid"}]}`)
	repository := &generatorRepository{files: files}
	if _, err := NewProjectService(repository).GenerateProjectArtifacts("site-one", "development"); err == nil {
		t.Fatal("asset with mismatched size/checksum was accepted")
	}
	if repository.batches != 0 {
		t.Fatalf("invalid asset caused %d artifact batches, want none", repository.batches)
	}
}
func (r *generatorRepository) Read(path string) (domain.File, error) {
	value, ok := r.files[path]
	if !ok {
		return domain.File{}, domain.ErrNotFound
	}
	return domain.File{Path: path, Revision: "revision", Content: value}, nil
}
func (r *generatorRepository) Write(path, _ string, content []byte) (domain.File, error) {
	r.files[path] = content
	return domain.File{Path: path, Revision: "generated", Content: content}, nil
}

func TestGenerateLocaleUsesCanonicalContent(t *testing.T) {
	repository := &generatorRepository{files: generatorFiles([]byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Hello"}},{"id":"hero-secondary","pageId":"home","component":"hero","fields":{"title":"Second"}},{"id":"footer-cta","pageId":"about","component":"cta","fields":{"label":"Explore"}}]}`))}
	artifact, err := NewProjectService(repository).GenerateLocale("site-one", "ru-RU")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Path != "src/generated/content/ru-RU.json" {
		t.Fatalf("unexpected artifact path: %s", artifact.Path)
	}
	var generated struct {
		Pages map[string]struct {
			Instances map[string]struct {
				Component string         `json:"component"`
				Fields    map[string]any `json:"fields"`
			} `json:"instances"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(repository.files[artifact.Path], &generated); err != nil {
		t.Fatalf("generated artifact is invalid JSON: %v", err)
	}
	if len(generated.Pages["home"].Instances) != 2 || generated.Pages["home"].Instances["hero-secondary"].Fields["title"] != "Second" || generated.Pages["about"].Instances["footer-cta"].Fields["label"] != "Explore" {
		t.Fatalf("generated artifact did not preserve page/component instances: %#v", generated)
	}
}

func TestGenerateLocaleUsesExactLanguageAndDefaultFallbacks(t *testing.T) {
	content := []byte(`{"schemaVersion":1,"id":"fallback-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"default"}}]}`)
	for _, test := range []struct {
		name  string
		files []string
		want  string
	}{
		{name: "default fallback", files: []string{"default"}, want: "default"},
		{name: "language fallback", files: []string{"default", "ru"}, want: "ru"},
		{name: "exact locale wins", files: []string{"default", "ru", "ru-RU"}, want: "ru-RU"},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := generatorFiles(content)
			delete(files, "liapoldus/content/site-one/ru-RU.json")
			for _, locale := range test.files {
				body := content
				if locale != "default" {
					body = []byte(strings.Replace(string(content), `"default"`, `"`+locale+`"`, 1))
				}
				files["liapoldus/content/site-one/"+locale+".json"] = body
			}
			repository := &generatorRepository{files: files}
			artifact, err := NewProjectService(repository).GenerateLocale("site-one", "ru-RU")
			if err != nil {
				t.Fatal(err)
			}
			if artifact.Path != "src/generated/content/ru-RU.json" {
				t.Fatalf("fallback generated wrong target path: %s", artifact.Path)
			}
			var generated struct {
				Pages map[string]struct {
					Instances map[string]struct {
						Fields map[string]string `json:"fields"`
					} `json:"instances"`
				} `json:"pages"`
			}
			if err := json.Unmarshal(repository.files[artifact.Path], &generated); err != nil {
				t.Fatal(err)
			}
			if got := generated.Pages["home"].Instances["hero-main"].Fields["title"]; got != test.want {
				t.Fatalf("fallback precedence chose %q, want %q", got, test.want)
			}
		})
	}
}

func TestGenerateLocaleSeparatesSharedAndLocalizedFields(t *testing.T) {
	files := generatorFiles([]byte(`{"schemaVersion":1,"id":"default-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"brand":"Shared brand","title":"Default title"}}]}`))
	files["src/components/hero/schema.json"] = []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"brand","type":"text","label":"Brand"},{"key":"title","type":"text","label":"Title","localized":true}]}`)
	files["liapoldus/content/site-one/default.json"] = files["liapoldus/content/site-one/ru-RU.json"]
	files["liapoldus/content/site-one/ru.json"] = []byte(`{"schemaVersion":1,"id":"language-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Language title"}}]}`)
	files["liapoldus/content/site-one/ru-RU.json"] = []byte(`{"schemaVersion":1,"id":"exact-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Exact title"}}]}`)
	repository := &generatorRepository{files: files}
	artifact, err := NewProjectService(repository).GenerateLocale("site-one", "ru-RU")
	if err != nil {
		t.Fatal(err)
	}
	var generated generatedLocale
	if err := json.Unmarshal(repository.files[artifact.Path], &generated); err != nil {
		t.Fatal(err)
	}
	fields := generated.Pages["home"].Instances["hero-main"].Fields
	if fields["brand"] != "Shared brand" || fields["title"] != "Exact title" {
		t.Fatalf("shared/localized resolution = %#v", fields)
	}
}

func TestGenerateLocaleRejectsDifferentInstanceStructureAcrossDocuments(t *testing.T) {
	files := generatorFiles([]byte(`{"schemaVersion":1,"id":"default-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Default"}}]}`))
	files["liapoldus/content/site-one/default.json"] = files["liapoldus/content/site-one/ru-RU.json"]
	files["liapoldus/content/site-one/ru-RU.json"] = []byte(`{"schemaVersion":1,"id":"locale-content","instances":[]}`)
	repository := &generatorRepository{files: files}
	_, err := NewProjectService(repository).GenerateLocale("site-one", "ru-RU")
	var validation domain.ContentValidationError
	if !errors.As(err, &validation) || len(validation.Diagnostics) != 1 || validation.Diagnostics[0].Code != "content.locale-structure" {
		t.Fatalf("expected locale structure diagnostic, got %v", err)
	}
}

func TestValidateForSiteLocaleAcceptsFallbackButRequiresEnabledTarget(t *testing.T) {
	content := []byte(`{"schemaVersion":1,"id":"fallback-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"From default"}}]}`)
	files := generatorFiles(content)
	delete(files, "liapoldus/content/site-one/ru-RU.json")
	files["liapoldus/content/site-one/default.json"] = content
	repository := &generatorRepository{files: files}
	diagnostics, err := NewProjectService(repository).ValidateForSiteLocale("site-one", "ru-RU")
	if err != nil || hasErrors(diagnostics) {
		t.Fatalf("enabled locale should validate through default fallback: diagnostics=%#v err=%v", diagnostics, err)
	}
	diagnostics, err = NewProjectService(repository).ValidateForSiteLocale("site-one", "en-US")
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "content.locale-disabled" {
			return
		}
	}
	t.Fatalf("disabled target locale should remain rejected: %#v", diagnostics)
}

func TestGenerateLocaleSanitizesRichTextWithoutChangingSourceContent(t *testing.T) {
	source := []byte(`{"schemaVersion":1,"id":"rich-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"body":"<p>Safe<script>alert(1)</script><a href=\"javascript:alert(1)\" onclick=\"alert(2)\">link</a></p>"}}]}`)
	repository := &generatorRepository{files: generatorFiles(source)}
	repository.files["src/components/hero/schema.json"] = []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"body","type":"rich-text","label":"Body","localized":true}]}`)
	artifact, err := NewProjectService(repository).GenerateLocale("site-one", "ru-RU")
	if err != nil {
		t.Fatal(err)
	}
	var generated struct {
		Pages map[string]struct {
			Instances map[string]struct {
				Fields map[string]string `json:"fields"`
			} `json:"instances"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(repository.files[artifact.Path], &generated); err != nil {
		t.Fatal(err)
	}
	value := generated.Pages["home"].Instances["hero-main"].Fields["body"]
	if strings.Contains(value, "script") || strings.Contains(value, "javascript:") || strings.Contains(value, "onclick") || !strings.Contains(value, "Safe") || !strings.Contains(value, "link") {
		t.Fatalf("generated rich text is not safely sanitized: %s", value)
	}
	if string(repository.files["liapoldus/content/site-one/ru-RU.json"]) != string(source) {
		t.Fatal("generation modified canonical source content")
	}
}
