package application

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/Liapoldus/Constructor/internal/domain"
	"strings"
	"testing"
)

type repositoryStub struct{ project domain.Project }

func (r repositoryStub) Manifest() (domain.Project, string, error)         { return r.project, "rev", nil }
func (r repositoryStub) Read(string) (domain.File, error)                  { return domain.File{}, domain.ErrNotFound }
func (r repositoryStub) Write(string, string, []byte) (domain.File, error) { return domain.File{}, nil }
func (r repositoryStub) List(prefix string) ([]string, error) {
	switch prefix {
	case "liapoldus/sites", "liapoldus/routes":
		return []string{}, nil
	default:
		return nil, domain.ErrInvalidPath
	}
}

func TestValidateRequiresAtLeastOneSite(t *testing.T) {
	diagnostics, err := NewProjectService(repositoryStub{}).Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "site.document-required" {
		t.Fatalf("expected missing Site diagnostic, got %#v", diagnostics)
	}
}

type contentValidationRepository struct {
	content, schema []byte
	routes          []byte
	site            []byte
	assetCatalog    []byte
	assetFile       []byte
	pages           []string
	sourceMissing   bool
	writes          *int
	usedRevision    *string
}

type siteListingRepository struct{ repositoryStub }

func (r siteListingRepository) List(prefix string) ([]string, error) {
	if prefix != "liapoldus/sites" {
		return nil, domain.ErrInvalidPath
	}
	return []string{"liapoldus/sites/local-site.json", "liapoldus/sites/readme.md"}, nil
}
func (r siteListingRepository) Read(path string) (domain.File, error) {
	if path == "liapoldus/sites/local-site.json" {
		return domain.File{Path: path, Content: []byte(`{"schemaVersion":1,"id":"local-site","projectId":"demo","name":"Local","pages":["home"],"locales":["ru-RU"]}`)}, nil
	}
	return domain.File{}, domain.ErrNotFound
}

func TestProjectServiceListsValidatedSiteDocuments(t *testing.T) {
	project := domain.Project{SchemaVersion: 1, ID: "demo", Name: "Demo", Pages: []string{"home"}}
	sites, err := NewProjectService(siteListingRepository{repositoryStub{project: project}}).Sites()
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || sites[0].ID != "local-site" {
		t.Fatalf("unexpected Site documents: %#v", sites)
	}
}

func TestProjectServiceListsOnlyIntegrityVerifiedAssets(t *testing.T) {
	assetBytes := []byte("verified image bytes")
	digest := sha256.Sum256(assetBytes)
	repository := contentValidationRepository{
		assetCatalog: []byte(`{"schemaVersion":1,"id":"assets","items":[{"id":"hero-image","type":"image","path":"public/assets/hero.png","mimeType":"image/png","size":` + fmt.Sprint(len(assetBytes)) + `,"sha256":"` + hex.EncodeToString(digest[:]) + `"}]}`),
		assetFile:    assetBytes,
	}
	assets, err := NewProjectService(repository).Assets()
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].ID != "hero-image" {
		t.Fatalf("unexpected verified assets: %#v", assets)
	}

	repository.assetFile = []byte("changed bytes")
	_, err = NewProjectService(repository).Assets()
	var validation domain.StructuredValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("asset integrity failure should be typed validation error, got %v", err)
	}
}

func (r contentValidationRepository) Manifest() (domain.Project, string, error) {
	project := domain.Project{SchemaVersion: 1, ID: "demo", Name: "Demo", Pages: []string{"home"}, Components: []string{"Hero"}}
	if len(r.pages) > 0 {
		project.Pages = r.pages
	}
	project.React.Entry = "src/app.tsx"
	return project, "rev", nil
}
func (r contentValidationRepository) List(prefix string) ([]string, error) {
	switch prefix {
	case "liapoldus/themes":
		return []string{}, nil
	case "liapoldus/sites":
		return []string{"liapoldus/sites/local-site.json"}, nil
	case "liapoldus/routes":
		if r.routes == nil {
			return []string{}, nil
		}
		return []string{"liapoldus/routes/development.json"}, nil
	case "liapoldus/content/local-site":
		if r.content == nil {
			return []string{}, nil
		}
		return []string{"liapoldus/content/local-site/ru-RU.json"}, nil
	default:
		return nil, domain.ErrInvalidPath
	}
}
func (r contentValidationRepository) Read(path string) (domain.File, error) {
	switch path {
	case "liapoldus/sites/local-site.json":
		if r.site != nil {
			return domain.File{Path: path, Content: r.site}, nil
		}
		return domain.File{Path: path, Content: []byte(`{"schemaVersion":1,"id":"local-site","projectId":"demo","name":"Demo","pages":["home"],"locales":["ru-RU"]}`)}, nil
	case "liapoldus/content/local-site/ru-RU.json", "liapoldus/content/local-site/en-US.json":
		return domain.File{Path: path, Content: r.content}, nil
	case "src/components/hero/schema.json":
		return domain.File{Path: path, Content: r.schema}, nil
	case "src/components/hero/component.tsx":
		if r.sourceMissing {
			return domain.File{}, domain.ErrNotFound
		}
		return domain.File{Path: path, Content: []byte("export default function Hero(){return null}")}, nil
	case "liapoldus/routes/development.json":
		if r.routes != nil {
			return domain.File{Path: path, Content: r.routes}, nil
		}
		return domain.File{}, domain.ErrNotFound
	case "src/pages/about.page.tsx":
		return domain.File{Path: path, Content: []byte("export default function AboutPage(){return null}")}, nil
	default:
		if path == assetCatalogPath && r.assetCatalog != nil {
			return domain.File{Path: path, Content: r.assetCatalog}, nil
		}
		if path == "public/assets/hero.png" && r.assetFile != nil {
			return domain.File{Path: path, Content: r.assetFile}, nil
		}
		return domain.File{}, domain.ErrNotFound
	}
}

func TestValidateForSiteLocaleRejectsDisabledLocale(t *testing.T) {
	repository := contentValidationRepository{
		content: []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Hello"}}]}`),
		schema:  []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title"}]}`),
	}
	diagnostics, err := NewProjectService(repository).ValidateForSiteLocale("local-site", "en-US")
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "content.locale-disabled" {
			return
		}
	}
	t.Fatalf("expected disabled-locale diagnostic, got %#v", diagnostics)
}
func (r contentValidationRepository) Write(path, revision string, content []byte) (domain.File, error) {
	if r.writes != nil {
		*r.writes = *r.writes + 1
	}
	if r.usedRevision != nil {
		*r.usedRevision = revision
	}
	return domain.File{Path: path, Revision: "next", Content: content}, nil
}

func TestContentAssetReferenceRequiresMatchingCatalogEntry(t *testing.T) {
	assetBytes := []byte("hero image bytes")
	digest := sha256.Sum256(assetBytes)
	catalog := []byte(`{"schemaVersion":1,"id":"assets","items":[{"id":"hero-image","type":"image","path":"public/assets/hero.png","mimeType":"image/png","size":` + fmt.Sprint(len(assetBytes)) + `,"sha256":"` + hex.EncodeToString(digest[:]) + `"}]}`)
	repository := contentValidationRepository{
		content:      []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"image":{"kind":"asset","id":"hero-image"}}}]}`),
		schema:       []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"image","type":"image","label":"Image"}]}`),
		assetCatalog: catalog,
		assetFile:    assetBytes,
	}
	service := NewProjectService(repository)
	diagnostics, err := service.validateContentDocument("liapoldus/content/local-site/ru-RU.json", repository.content)
	if err != nil {
		t.Fatal(err)
	}
	if hasErrors(diagnostics) {
		t.Fatalf("valid asset reference rejected: %#v", diagnostics)
	}

	repository.assetCatalog = []byte(`{"schemaVersion":1,"id":"assets","items":[]}`)
	diagnostics, err = NewProjectService(repository).validateContentDocument("liapoldus/content/local-site/ru-RU.json", repository.content)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "content.asset-reference" && diagnostic.FieldKey == "image" {
			return
		}
	}
	t.Fatalf("expected missing catalog reference diagnostic, got %#v", diagnostics)
}

func TestAssetCatalogWriteRequiresMatchingBinaryMetadata(t *testing.T) {
	writes := 0
	assetBytes := []byte("asset")
	digest := sha256.Sum256(assetBytes)
	badCatalog := []byte(`{"schemaVersion":1,"id":"assets","items":[{"id":"hero-image","type":"image","path":"public/assets/hero.png","mimeType":"image/png","size":99,"sha256":"` + hex.EncodeToString(digest[:]) + `"}]}`)
	repository := contentValidationRepository{writes: &writes, assetFile: assetBytes}
	_, err := NewProjectService(repository).Write(assetCatalogPath, "rev", badCatalog)
	var validationErr domain.StructuredValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected structured asset validation error, got %v", err)
	}
	if writes != 0 {
		t.Fatalf("invalid asset catalog was persisted %d times", writes)
	}
}

func TestValidateChecksContentPageAndSchemaFields(t *testing.T) {
	repository := contentValidationRepository{
		content: []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"missing","component":"hero","fields":{"title":"Hello","unknown":true}}]}`),
		schema:  []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title"}]}`),
	}
	diagnostics, err := NewProjectService(repository).Validate()
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, diagnostic := range diagnostics {
		codes[diagnostic.Code] = true
	}
	if !codes["content.page-reference"] || !codes["content.unknown-field"] {
		t.Fatalf("expected page reference and unknown field diagnostics, got %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		switch diagnostic.Code {
		case "content.page-reference":
			if diagnostic.PageID != "missing" || diagnostic.InstanceID != "hero-main" {
				t.Fatalf("page diagnostic lacks content source coordinates: %#v", diagnostic)
			}
		case "content.unknown-field":
			if diagnostic.PageID != "missing" || diagnostic.InstanceID != "hero-main" || diagnostic.FieldKey != "unknown" {
				t.Fatalf("field diagnostic lacks content source coordinates: %#v", diagnostic)
			}
		}
	}
}

func TestValidateAcceptsContentPageIDFromSiteDocument(t *testing.T) {
	repository := contentValidationRepository{
		content: []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Hello"}}]}`),
		schema:  []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title"}]}`),
	}
	diagnostics, err := NewProjectService(repository).Validate()
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range diagnostics {
		if strings.HasPrefix(diagnostic.Code, "content.") {
			t.Fatalf("unexpected content diagnostic: %#v", diagnostic)
		}
	}
}

func TestValidateRejectsRoutePageMissingFromEverySite(t *testing.T) {
	repository := contentValidationRepository{
		pages:   []string{"home", "about"},
		routes:  []byte(`{"schemaVersion":1,"id":"development-routes","routes":[{"id":"about","path":"/about","page":"about"}]}`),
		content: []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Hello"}}]}`),
		schema:  []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title"}]}`),
	}
	diagnostics, err := NewProjectService(repository).Validate()
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "route.page-site-reference" && diagnostic.PageID == "about" {
			return
		}
	}
	t.Fatalf("expected dangling Site page reference, got %#v", diagnostics)
}

func TestRouteSaveRejectsPageMissingFromEverySite(t *testing.T) {
	writes := 0
	repository := contentValidationRepository{
		pages:   []string{"home", "about"},
		content: []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Hello"}}]}`),
		schema:  []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title"}]}`),
		writes:  &writes,
	}
	route := []byte(`{"schemaVersion":1,"id":"development-routes","routes":[{"id":"about","path":"/about","page":"about"}]}`)
	_, diagnostics, err := NewRouteService(NewProjectService(repository)).Save("development", "", route)
	if err == nil {
		t.Fatal("route to a page outside every Site was saved")
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "route.page-site-reference" && diagnostic.PageID == "about" {
			if writes != 0 {
				t.Fatalf("invalid route reached repository write %d times", writes)
			}
			return
		}
	}
	t.Fatalf("expected a Site page reference diagnostic, got %#v", diagnostics)
}

func TestWriteBlocksRemovingPageReferencedByContent(t *testing.T) {
	writes := 0
	repository := contentValidationRepository{
		pages: []string{"home", "about"}, writes: &writes,
		content: []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Hello"}}]}`),
		schema:  []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title"}]}`),
	}
	updatedSite := []byte(`{"schemaVersion":1,"id":"local-site","projectId":"demo","name":"Demo","pages":["about"],"locales":["ru-RU"]}`)
	_, err := NewProjectService(repository).Write("liapoldus/sites/local-site.json", "site-revision", updatedSite)
	var validation domain.StructuredValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected a typed page-in-use diagnostic, got %v", err)
	}
	for _, diagnostic := range validation.Diagnostics {
		if diagnostic.Code == "site.page-content-reference" && diagnostic.PageID == "home" && diagnostic.InstanceID == "hero-main" {
			if writes != 0 {
				t.Fatalf("Site mutation reached repository write %d times", writes)
			}
			return
		}
	}
	t.Fatalf("expected source coordinates for the page reference, got %#v", validation.Diagnostics)
}

func TestWriteBlocksRemovingPageReferencedByRoute(t *testing.T) {
	writes := 0
	repository := contentValidationRepository{
		pages: []string{"home", "about"}, writes: &writes,
		site:    []byte(`{"schemaVersion":1,"id":"local-site","projectId":"demo","name":"Demo","pages":[{"id":"home","name":"Home"},{"id":"about","name":"About"}],"locales":["ru-RU"]}`),
		content: []byte(`{"schemaVersion":1,"id":"home-content","instances":[]}`),
		routes:  []byte(`{"schemaVersion":1,"id":"development-routes","routes":[{"id":"about","path":"/about","page":"about"}]}`),
		schema:  []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title"}]}`),
	}
	updatedSite := []byte(`{"schemaVersion":1,"id":"local-site","projectId":"demo","name":"Demo","pages":[{"id":"home","name":"Home"}],"locales":["ru-RU"]}`)
	_, err := NewProjectService(repository).Write("liapoldus/sites/local-site.json", "site-revision", updatedSite)
	var validation domain.StructuredValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected a typed route page-in-use diagnostic, got %v", err)
	}
	for _, diagnostic := range validation.Diagnostics {
		if diagnostic.Code == "route.page-site-reference" && diagnostic.PageID == "about" {
			if writes != 0 {
				t.Fatalf("Site mutation reached repository write %d times", writes)
			}
			return
		}
	}
	t.Fatalf("expected route page reference diagnostic, got %#v", validation.Diagnostics)
}

func TestWriteAllowsPageRenameAndReorderWithoutChangingReferences(t *testing.T) {
	writes := 0
	repository := contentValidationRepository{
		pages: []string{"home", "about"}, writes: &writes,
		site:    []byte(`{"schemaVersion":1,"id":"local-site","projectId":"demo","name":"Demo","pages":[{"id":"home","name":"Home"},{"id":"about","name":"About"}],"locales":["ru-RU"]}`),
		content: []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Hello"}}]}`),
		routes:  []byte(`{"schemaVersion":1,"id":"development-routes","routes":[{"id":"home","path":"/","page":"home"},{"id":"about","path":"/about","page":"about"}]}`),
		schema:  []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title"}]}`),
	}
	updatedSite := []byte(`{"schemaVersion":1,"id":"local-site","projectId":"demo","name":"Demo","pages":[{"id":"about","name":"About us"},{"id":"home","name":"Home"}],"locales":["ru-RU"]}`)
	if _, err := NewProjectService(repository).Write("liapoldus/sites/local-site.json", "site-revision", updatedSite); err != nil {
		t.Fatalf("page rename/reorder was rejected: %v", err)
	}
	if writes != 1 {
		t.Fatalf("page metadata update reached repository %d times, want exactly once", writes)
	}
}

func TestWriteRejectsInvalidContentBeforeRepositoryMutation(t *testing.T) {
	writes := 0
	repository := contentValidationRepository{
		writes: &writes,
		schema: []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title"}]}`),
	}
	_, err := NewProjectService(repository).Write("liapoldus/content/local-site/ru-RU.json", "base-blob", []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"missing","component":"hero","fields":{"title":"Hello","extra":true}}]}`))
	var validation domain.ContentValidationError
	if !errors.As(err, &validation) || len(validation.Diagnostics) < 2 {
		t.Fatalf("expected typed page/field diagnostics, got %v", err)
	}
	if writes != 0 {
		t.Fatalf("invalid document reached repository write %d times", writes)
	}
}

func TestWriteBlocksRemovingPageStillDeclaredBySite(t *testing.T) {
	writes := 0
	repository := contentValidationRepository{writes: &writes}
	manifest := []byte(`{"schemaVersion":1,"id":"demo","name":"Demo","pages":["about"],"components":["Hero"],"react":{"entry":"src/app.tsx"}}`)
	_, err := NewProjectService(repository).Write("liapoldus/project.json", "manifest-revision", manifest)
	var validation domain.StructuredValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected a typed page reference diagnostic, got %v", err)
	}
	for _, diagnostic := range validation.Diagnostics {
		if diagnostic.Code == "project.page-site-reference" && diagnostic.PageID == "home" {
			if writes != 0 {
				t.Fatalf("manifest mutation reached repository write %d times", writes)
			}
			return
		}
	}
	t.Fatalf("expected a dangling Site page reference, got %#v", validation.Diagnostics)
}

func TestWriteManifestRequiresSchemaForEveryDeclaredComponent(t *testing.T) {
	writes := 0
	repository := contentValidationRepository{writes: &writes}
	manifest := []byte(`{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":["Button"],"react":{"entry":"src/app.tsx"}}`)
	_, err := NewProjectService(repository).Write("liapoldus/project.json", "manifest-revision", manifest)
	var validation domain.StructuredValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected component schema diagnostics, got %v", err)
	}
	for _, diagnostic := range validation.Diagnostics {
		if diagnostic.Code == "project.component-schema-required" {
			if writes != 0 {
				t.Fatalf("invalid manifest reached repository write %d times", writes)
			}
			return
		}
	}
	t.Fatalf("expected missing component schema diagnostic, got %#v", validation.Diagnostics)
}

func TestWriteManifestRejectsMissingDeclaredComponentSource(t *testing.T) {
	writes := 0
	repository := contentValidationRepository{
		writes: &writes, sourceMissing: true,
		schema: []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title"}]}`),
	}
	manifest := []byte(`{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":["Hero"],"react":{"entry":"src/app.tsx"}}`)
	_, err := NewProjectService(repository).Write("liapoldus/project.json", "manifest-revision", manifest)
	var validation domain.StructuredValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected component source diagnostics, got %v", err)
	}
	for _, diagnostic := range validation.Diagnostics {
		if diagnostic.Code == "project.component-source-required" {
			if writes != 0 {
				t.Fatalf("invalid manifest reached repository write %d times", writes)
			}
			return
		}
	}
	t.Fatalf("expected missing component source diagnostic, got %#v", validation.Diagnostics)
}

func TestWriteManifestBlocksRemovingComponentReferencedByContent(t *testing.T) {
	writes := 0
	repository := contentValidationRepository{
		writes:  &writes,
		content: []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Hello"}}]}`),
		schema:  []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title"}]}`),
	}
	manifest := []byte(`{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":[],"react":{"entry":"src/app.tsx"}}`)
	_, err := NewProjectService(repository).Write("liapoldus/project.json", "manifest-revision", manifest)
	var validation domain.StructuredValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected component reference diagnostics, got %v", err)
	}
	for _, diagnostic := range validation.Diagnostics {
		if diagnostic.Code == "project.component-content-reference" && diagnostic.InstanceID == "hero-main" {
			if writes != 0 {
				t.Fatalf("invalid manifest reached repository write %d times", writes)
			}
			return
		}
	}
	t.Fatalf("expected content component reference diagnostic, got %#v", validation.Diagnostics)
}

func TestWriteValidatesContentAndPreservesOptimisticRevision(t *testing.T) {
	writes := 0
	usedRevision := ""
	repository := contentValidationRepository{writes: &writes, usedRevision: &usedRevision,
		schema: []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title"}]}`),
	}
	_, err := NewProjectService(repository).Write("liapoldus/content/local-site/ru-RU.json", "base-blob", []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Hello"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if writes != 1 || usedRevision != "base-blob" {
		t.Fatalf("write did not preserve optimistic revision: writes=%d revision=%q", writes, usedRevision)
	}
}

func TestWriteAllowsIncompleteRequiredLocalizedFieldButLocaleValidationRejectsIt(t *testing.T) {
	writes := 0
	content := []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{}}]}`)
	repository := contentValidationRepository{
		content: content,
		schema:  []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"title","type":"text","label":"Title","required":true,"localized":true}]}`),
		writes:  &writes,
	}
	service := NewProjectService(repository)
	if _, err := service.Write("liapoldus/content/local-site/ru-RU.json", "base-blob", content); err != nil {
		t.Fatalf("saving an incomplete localized draft should succeed: %v", err)
	}
	if writes != 1 {
		t.Fatalf("content write count = %d, want 1", writes)
	}
	diagnostics, err := service.ValidateForSiteLocale("local-site", "ru-RU")
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "content.required" && diagnostic.FieldKey == "title" {
			return
		}
	}
	t.Fatalf("strict enabled-locale validation should report missing title: %#v", diagnostics)
}

func TestWriteComponentSchemaChecksDirectoryAndSourceReferences(t *testing.T) {
	for _, test := range []struct {
		name          string
		path          string
		schema        string
		sourceMissing bool
		wantCode      string
	}{
		{name: "schema id must match directory", path: "src/components/button/schema.json", schema: `{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[]}`, wantCode: "component.schema-path-id"},
		{name: "source must exist", path: "src/components/hero/schema.json", schema: `{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[]}`, sourceMissing: true, wantCode: "component.source-required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			writes := 0
			repository := contentValidationRepository{writes: &writes, sourceMissing: test.sourceMissing}
			_, err := NewProjectService(repository).Write(test.path, "", []byte(test.schema))
			var validation domain.StructuredValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("expected structured schema validation error, got %v", err)
			}
			for _, diagnostic := range validation.Diagnostics {
				if diagnostic.Code == test.wantCode {
					if writes != 0 {
						t.Fatalf("invalid schema reached repository write %d times", writes)
					}
					return
				}
			}
			t.Fatalf("expected %s diagnostic, got %#v", test.wantCode, validation.Diagnostics)
		})
	}
}

func TestWriteComponentSchemaCannotInvalidateExistingContent(t *testing.T) {
	writes := 0
	repository := contentValidationRepository{
		writes:  &writes,
		content: []byte(`{"schemaVersion":1,"id":"home-content","instances":[{"id":"hero-main","pageId":"home","component":"hero","fields":{"title":"Hello"}}]}`),
	}
	changedSchema := []byte(`{"schemaVersion":1,"id":"hero","kind":"component","source":"src/components/hero/component.tsx","fields":[{"key":"headline","type":"text","label":"Headline","required":true}]}`)
	_, err := NewProjectService(repository).Write("src/components/hero/schema.json", "schema-revision", changedSchema)
	var validation domain.ContentValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected typed content/schema diagnostics, got %v", err)
	}
	if len(validation.Diagnostics) == 0 || writes != 0 {
		t.Fatalf("invalidating schema write was not blocked: diagnostics=%#v writes=%d", validation.Diagnostics, writes)
	}
}

func TestWriteRejectsNonCanonicalContentPaths(t *testing.T) {
	writes := 0
	repository := contentValidationRepository{writes: &writes}
	for _, path := range []string{
		"liapoldus/content.json",
		"liapoldus/content/local-site.json",
		"liapoldus/content/local-site/not-a-locale!.json",
	} {
		if _, err := NewProjectService(repository).Write(path, "", []byte(`{}`)); !errors.Is(err, domain.ErrInvalidPath) {
			t.Errorf("Write(%q) error = %v, want ErrInvalidPath", path, err)
		}
	}
	if writes != 0 {
		t.Fatalf("invalid paths reached repository write %d times", writes)
	}
}

func TestWriteValidatesSiteDocumentsBeforeRepositoryMutation(t *testing.T) {
	writes := 0
	repository := contentValidationRepository{writes: &writes}
	service := NewProjectService(repository)
	_, err := service.Write("liapoldus/sites/local-site.json", "", []byte(`{"schemaVersion":1,"id":"another-site","projectId":"wrong","name":"Local","pages":["home"],"locales":["ru-RU"],"unknown":true}`))
	var validation domain.StructuredValidationError
	if !errors.As(err, &validation) || len(validation.Diagnostics) < 3 {
		t.Fatalf("expected typed Site diagnostics, got %v", err)
	}
	if writes != 0 {
		t.Fatalf("invalid Site reached repository write %d times", writes)
	}
}
