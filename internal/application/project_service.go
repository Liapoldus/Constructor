package application

import (
	"encoding/json"
	"github.com/Liapoldus/Constructor/internal/domain"
	"path"
	"regexp"
	"strings"
	"sync"
)

var sitePathSegment = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
var componentSchemaPathPattern = regexp.MustCompile(`^src/components/[a-z][a-z0-9-]{1,61}/schema\.json$`)

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type ProjectService struct {
	repository      domain.ProjectRepository
	mutation        sync.Mutex
	assetProcessing sync.Mutex
}

func NewProjectService(repository domain.ProjectRepository) *ProjectService {
	return &ProjectService{repository: repository}
}

func isContentDocumentPath(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 4 && parts[0] == "liapoldus" && parts[1] == "content" && contentPathSegment.MatchString(parts[2]) && strings.HasSuffix(parts[3], ".json") && localePathSegment.MatchString(strings.TrimSuffix(parts[3], ".json"))
}

func isSiteDocumentPath(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 3 && parts[0] == "liapoldus" && parts[1] == "sites" && sitePathSegment.MatchString(strings.TrimSuffix(parts[2], ".json")) && strings.HasSuffix(parts[2], ".json")
}

func isThemeDocumentPath(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 3 && parts[0] == "liapoldus" && parts[1] == "themes" && sitePathSegment.MatchString(strings.TrimSuffix(parts[2], ".json")) && strings.HasSuffix(parts[2], ".json")
}

func hasErrors(diagnostics []domain.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			return true
		}
	}
	return false
}

func (s *ProjectService) loadComponentSchemas(project domain.Project) (map[string]domain.ComponentSchema, []domain.Diagnostic, error) {
	schemas := map[string]domain.ComponentSchema{}
	var diagnostics []domain.Diagnostic
	for _, component := range project.Components {
		schemaPath := path.Join("src", "components", strings.ToLower(component), "schema.json")
		schemaFile, readErr := s.repository.Read(schemaPath)
		if readErr == domain.ErrNotFound {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "project.component-schema-required", Severity: "error", Path: schemaPath, Message: "declared component schema is missing"})
			continue
		}
		if readErr != nil {
			return nil, nil, readErr
		}
		schemaDiagnostics, schemaErr := domain.ValidateComponentSchema(schemaFile.Content, schemaPath)
		if schemaErr != nil {
			return nil, nil, schemaErr
		}
		diagnostics = append(diagnostics, schemaDiagnostics...)
		if hasErrors(schemaDiagnostics) {
			continue
		}
		var schema domain.ComponentSchema
		if json.Unmarshal(schemaFile.Content, &schema) == nil && schema.ID != "" {
			if schema.ID != strings.ToLower(component) {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "project.component-schema-id", Severity: "error", Path: schemaPath, Message: "schema id must match its component directory"})
			}
			if _, sourceErr := s.repository.Read(schema.Source); sourceErr == domain.ErrNotFound {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "project.component-source-required", Severity: "error", Path: schemaPath, Message: "component source is missing: " + schema.Source})
			} else if sourceErr != nil {
				return nil, nil, sourceErr
			}
			schemas[strings.ToLower(schema.ID)] = schema
		}
	}
	return schemas, diagnostics, nil
}

func (s *ProjectService) validateContentDocument(contentPath string, raw []byte) ([]domain.Diagnostic, error) {
	return s.validateContentDocumentWithPolicy(contentPath, raw, false, true)
}

func (s *ProjectService) validateContentDocumentWithLocalePolicy(contentPath string, raw []byte, allowDisabledSourceLocale bool) ([]domain.Diagnostic, error) {
	return s.validateContentDocumentWithPolicy(contentPath, raw, allowDisabledSourceLocale, false)
}

func (s *ProjectService) validateContentDocumentWithPolicy(contentPath string, raw []byte, allowDisabledSourceLocale, allowIncompleteLocalized bool) ([]domain.Diagnostic, error) {
	project, _, err := s.repository.Manifest()
	if err != nil {
		return nil, err
	}
	var diagnostics []domain.Diagnostic
	document, contentDiagnostics := domain.DecodeContentDocument(raw, contentPath)
	diagnostics = append(diagnostics, contentDiagnostics...)
	parts := strings.Split(contentPath, "/")
	siteID := parts[2]
	sitePath := path.Join("liapoldus", "sites", siteID+".json")
	pageIDs := map[string]bool{}
	siteFile, err := s.repository.Read(sitePath)
	if err == domain.ErrNotFound {
		diagnostics = append(diagnostics, domain.Diagnostic{Code: "content.site-required", Severity: "error", Path: sitePath, Message: "content references a site without a site document"})
	} else if err != nil {
		return nil, err
	} else {
		site, siteDiagnostics := domain.DecodeSiteDocument(siteFile.Content, sitePath)
		diagnostics = append(diagnostics, siteDiagnostics...)
		if site.ID != "" && site.ID != siteID {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "content.site-id", Severity: "error", Path: sitePath, Message: "site document id does not match its path"})
		}
		if site.ProjectID != "" && site.ProjectID != project.ID {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "content.site-project", Severity: "error", Path: sitePath, Message: "site document belongs to a different project"})
		}
		locale := strings.TrimSuffix(parts[3], ".json")
		if locale != "default" && !allowDisabledSourceLocale {
			enabled := false
			for _, candidate := range site.Locales {
				if candidate == locale {
					enabled = true
					break
				}
			}
			if !enabled {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "content.locale-disabled", Severity: "error", Path: contentPath, Message: "locale is not enabled for this site"})
			}
		}
		for _, page := range site.Pages {
			pageIDs[page.ID] = true
		}
	}
	schemas, schemaDiagnostics, err := s.loadComponentSchemas(project)
	if err != nil {
		return nil, err
	}
	diagnostics = append(diagnostics, schemaDiagnostics...)
	assets, assetCatalogExists, assetDiagnostics, err := s.assetCatalog()
	if err != nil {
		return nil, err
	}
	diagnostics = append(diagnostics, assetDiagnostics...)
	for _, instance := range document.Instances {
		if instance.PageID != "" && !pageIDs[instance.PageID] {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "content.page-reference", Severity: "error", Path: contentPath, Message: "instance " + instance.ID + " references an unknown site page", PageID: instance.PageID, InstanceID: instance.ID})
		}
		schema, exists := schemas[strings.ToLower(instance.Component)]
		if !exists {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "content.component-reference", Severity: "error", Path: contentPath, Message: "instance " + instance.ID + " references a component without a schema", PageID: instance.PageID, InstanceID: instance.ID})
			continue
		}
		if allowIncompleteLocalized {
			diagnostics = append(diagnostics, domain.ValidateInstanceContentForWrite(schema, instance, contentPath)...)
		} else {
			diagnostics = append(diagnostics, domain.ValidateInstanceContent(schema, instance, contentPath)...)
		}
		diagnostics = append(diagnostics, s.validateAssetReferences(instance, schema, assets, assetCatalogExists, contentPath)...)
	}
	return diagnostics, nil
}

func (s *ProjectService) Sites() ([]domain.SiteDocument, error) {
	lister, ok := s.repository.(domain.ProjectFileLister)
	if !ok {
		return nil, domain.ErrUnsupported
	}
	paths, err := lister.List("liapoldus/sites")
	if err != nil {
		return nil, err
	}
	project, _, err := s.repository.Manifest()
	if err != nil {
		return nil, err
	}
	sites := make([]domain.SiteDocument, 0, len(paths))
	seen := map[string]bool{}
	for _, sitePath := range paths {
		name := strings.TrimSuffix(strings.TrimPrefix(sitePath, "liapoldus/sites/"), ".json")
		if !strings.HasPrefix(sitePath, "liapoldus/sites/") || !strings.HasSuffix(sitePath, ".json") || !sitePathSegment.MatchString(name) {
			continue
		}
		file, readErr := s.repository.Read(sitePath)
		if readErr != nil {
			return nil, readErr
		}
		site, diagnostics := domain.DecodeSiteDocument(file.Content, sitePath)
		if site.ID != name {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "site.id-path", Severity: "error", Path: sitePath, Message: "site id must match its filename"})
		}
		if site.ProjectID != project.ID {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "site.project-reference", Severity: "error", Path: sitePath, Message: "site belongs to a different project"})
		}
		for _, page := range site.Pages {
			if !contains(project.Pages, page.ID) {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "site.page-project", Severity: "error", Path: sitePath, Message: "site page " + page.ID + " is not declared by the project manifest"})
			}
		}
		if seen[site.ID] {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "site.duplicate-id", Severity: "error", Path: sitePath, Message: "site IDs must be unique within a project"})
		}
		seen[site.ID] = true
		if hasErrors(diagnostics) {
			return nil, domain.StructuredValidationError{Diagnostics: diagnostics}
		}
		sites = append(sites, site)
	}
	return sites, nil
}

func (s *ProjectService) Manifest() (domain.Project, string, error) { return s.repository.Manifest() }
func (s *ProjectService) Read(path string) (domain.File, error)     { return s.repository.Read(path) }
func (s *ProjectService) ReadAtRoot(root, projectID, path string) (domain.File, error) {
	factory, ok := s.repository.(domain.ProjectRepositoryAtRoot)
	if !ok {
		return domain.File{}, domain.ErrUnsupported
	}
	repository := factory.AtRoot(root)
	manifest, _, err := repository.Manifest()
	if err != nil {
		return domain.File{}, err
	}
	if manifest.ID != projectID {
		return domain.File{}, domain.ErrNotFound
	}
	return repository.Read(path)
}
func (s *ProjectService) ValidateAtRoot(root, projectID string) ([]domain.Diagnostic, error) {
	factory, ok := s.repository.(domain.ProjectRepositoryAtRoot)
	if !ok {
		return nil, domain.ErrUnsupported
	}
	repository := factory.AtRoot(root)
	manifest, _, err := repository.Manifest()
	if err != nil {
		return nil, err
	}
	if manifest.ID != projectID {
		return nil, domain.ErrNotFound
	}
	return NewProjectService(repository).ValidateAllSites()
}
func (s *ProjectService) Write(path, revision string, content []byte) (domain.File, error) {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	return s.write(path, revision, content)
}
func (s *ProjectService) WriteAtRoot(root, projectID, path, revision string, content []byte) (domain.File, error) {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	factory, ok := s.repository.(domain.ProjectRepositoryAtRoot)
	if !ok {
		return domain.File{}, domain.ErrUnsupported
	}
	repository := factory.AtRoot(root)
	manifest, _, err := repository.Manifest()
	if err != nil {
		return domain.File{}, err
	}
	if manifest.ID != projectID {
		return domain.File{}, domain.ErrNotFound
	}
	return NewProjectService(repository).write(path, revision, content)
}
func (s *ProjectService) write(path, revision string, content []byte) (domain.File, error) {
	if path == assetCatalogPath {
		document, diagnostics := domain.DecodeAssetDocument(content, path)
		if !hasErrors(diagnostics) {
			assetDiagnostics, err := s.verifyAssetItems(document.Items, path)
			if err != nil {
				return domain.File{}, err
			}
			diagnostics = append(diagnostics, assetDiagnostics...)
		}
		if hasErrors(diagnostics) {
			return domain.File{}, domain.StructuredValidationError{Diagnostics: diagnostics}
		}
	}
	if path == "liapoldus/project.json" {
		project, diagnostics := domain.DecodeProjectDocument(content, path)
		if hasErrors(diagnostics) {
			return domain.File{}, domain.StructuredValidationError{Diagnostics: diagnostics}
		}
		components, err := s.validateManifestComponents(project, path)
		if err != nil {
			return domain.File{}, err
		}
		diagnostics = append(diagnostics, components...)
		references, err := s.validateManifestPageReferences(project)
		if err != nil {
			return domain.File{}, err
		}
		diagnostics = append(diagnostics, references...)
		if hasErrors(diagnostics) {
			return domain.File{}, domain.StructuredValidationError{Diagnostics: diagnostics}
		}
	}
	if strings.HasPrefix(path, "liapoldus/content/") && !isContentDocumentPath(path) || path == "liapoldus/content.json" {
		return domain.File{}, domain.ErrInvalidPath
	}
	if strings.HasPrefix(path, "liapoldus/sites/") && !isSiteDocumentPath(path) {
		return domain.File{}, domain.ErrInvalidPath
	}
	if strings.HasPrefix(path, "liapoldus/themes/") && !isThemeDocumentPath(path) {
		return domain.File{}, domain.ErrInvalidPath
	}
	if isThemeDocumentPath(path) {
		theme, diagnostics := domain.DecodeThemeDocument(content, path)
		if theme.ID != strings.TrimSuffix(path[len("liapoldus/themes/"):], ".json") {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "theme.id-path", Severity: "error", Path: path, Message: "theme id must match its filename"})
		}
		if hasErrors(diagnostics) {
			return domain.File{}, domain.StructuredValidationError{Diagnostics: diagnostics}
		}
		references, referenceErr := s.validateThemeCandidateReferences(theme)
		if referenceErr != nil {
			return domain.File{}, referenceErr
		}
		if hasErrors(references) {
			return domain.File{}, domain.StructuredValidationError{Diagnostics: references}
		}
	}
	if isSiteDocumentPath(path) {
		project, _, err := s.repository.Manifest()
		if err != nil {
			return domain.File{}, err
		}
		site, diagnostics := domain.DecodeSiteDocument(content, path)
		if site.ID != strings.TrimSuffix(path[len("liapoldus/sites/"):], ".json") {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "site.id-path", Severity: "error", Path: path, Message: "site id must match its filename"})
		}
		if site.ProjectID != project.ID {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "site.project-reference", Severity: "error", Path: path, Message: "site belongs to a different project"})
		}
		for _, page := range site.Pages {
			if !contains(project.Pages, page.ID) {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "site.page-project", Severity: "error", Path: path, Message: "site page " + page.ID + " is not declared by the project manifest"})
			}
		}
		if hasErrors(diagnostics) {
			return domain.File{}, domain.StructuredValidationError{Diagnostics: diagnostics}
		}
		references, err := s.validateSiteContentReferences(path, site)
		if err != nil {
			return domain.File{}, err
		}
		diagnostics = append(diagnostics, references...)
		themes, themeDiagnostics, themeErr := s.themeDocuments()
		if themeErr != nil {
			return domain.File{}, themeErr
		}
		_, siteThemeDiagnostics := resolveSiteTheme(site, themes)
		diagnostics = append(diagnostics, themeDiagnostics...)
		diagnostics = append(diagnostics, siteThemeDiagnostics...)
		if hasErrors(diagnostics) {
			return domain.File{}, domain.StructuredValidationError{Diagnostics: diagnostics}
		}
		tokenReferences, tokenErr := s.validateSiteThemeTokens(site)
		if tokenErr != nil {
			return domain.File{}, tokenErr
		}
		if hasErrors(tokenReferences) {
			return domain.File{}, domain.StructuredValidationError{Diagnostics: tokenReferences}
		}
		routeReferences, err := s.validateSiteRouteReferences(site)
		if err != nil {
			return domain.File{}, err
		}
		diagnostics = append(diagnostics, routeReferences...)
		if hasErrors(diagnostics) {
			return domain.File{}, domain.StructuredValidationError{Diagnostics: diagnostics}
		}
	}
	if strings.HasPrefix(path, "src/components/") && strings.HasSuffix(path, "/schema.json") {
		if !componentSchemaPathPattern.MatchString(path) {
			return domain.File{}, domain.ErrInvalidPath
		}
		diagnostics, err := domain.ValidateComponentSchema(content, path)
		if err != nil {
			return domain.File{}, err
		}
		var schema domain.ComponentSchema
		if !hasErrors(diagnostics) {
			if err := json.Unmarshal(content, &schema); err != nil {
				return domain.File{}, err
			}
			parts := strings.Split(path, "/")
			if schema.ID != parts[2] {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "component.schema-path-id", Severity: "error", Path: path, Message: "schema id must match its component directory"})
			}
			if _, sourceErr := s.repository.Read(schema.Source); sourceErr == domain.ErrNotFound {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "component.source-required", Severity: "error", Path: path, Message: "component source is missing: " + schema.Source})
			} else if sourceErr != nil {
				return domain.File{}, sourceErr
			}
			if !hasErrors(diagnostics) {
				references, referenceErr := s.validateComponentSchemaContent(schema)
				if referenceErr != nil {
					return domain.File{}, referenceErr
				}
				diagnostics = append(diagnostics, references...)
				if !hasErrors(diagnostics) {
					themeReferences, themeErr := s.validateComponentThemeReferences(schema)
					if themeErr != nil {
						return domain.File{}, themeErr
					}
					diagnostics = append(diagnostics, themeReferences...)
				}
			}
		}
		if hasErrors(diagnostics) {
			return domain.File{}, domain.StructuredValidationError{Diagnostics: diagnostics}
		}
	}
	if isContentDocumentPath(path) {
		diagnostics, err := s.validateContentDocument(path, content)
		if err != nil {
			return domain.File{}, err
		}
		if hasErrors(diagnostics) {
			return domain.File{}, domain.ContentValidationError{Diagnostics: diagnostics}
		}
	}
	return s.repository.Write(path, revision, content)
}
func (s *ProjectService) Validate() ([]domain.Diagnostic, error) {
	return s.ValidateAllSites()
}

// ValidateAllSites validates each enabled locale for every Site in the project.
// It intentionally does not inspect content files for disabled locales.
func (s *ProjectService) ValidateAllSites() ([]domain.Diagnostic, error) {
	sites, err := s.Sites()
	if err != nil {
		return nil, err
	}
	if len(sites) == 0 {
		return []domain.Diagnostic{{Code: "site.document-required", Severity: "error", Path: "liapoldus/sites", Message: "project must contain at least one Site document"}}, nil
	}
	var diagnostics []domain.Diagnostic
	seen := map[string]bool{}
	for _, site := range sites {
		for _, locale := range site.Locales {
			items, validationErr := s.ValidateForSiteLocale(site.ID, locale)
			if validationErr != nil {
				return nil, validationErr
			}
			for _, diagnostic := range items {
				encoded, _ := json.Marshal(diagnostic)
				key := string(encoded)
				if !seen[key] {
					seen[key] = true
					diagnostics = append(diagnostics, diagnostic)
				}
			}
		}
	}
	routeDiagnostics, err := s.validateAllRouteReferences(sites)
	if err != nil {
		return nil, err
	}
	diagnostics = append(diagnostics, routeDiagnostics...)
	themeDiagnostics, err := s.validateThemeReferences(sites)
	if err != nil {
		return nil, err
	}
	diagnostics = append(diagnostics, themeDiagnostics...)
	return diagnostics, nil
}

func (s *ProjectService) ValidateForSiteLocale(siteID, locale string) ([]domain.Diagnostic, error) {
	contentPath := path.Join("liapoldus", "content", siteID, locale+".json")
	if !isContentDocumentPath(contentPath) {
		return nil, domain.ErrInvalidPath
	}
	project, _, err := s.repository.Manifest()
	if err != nil {
		return nil, err
	}
	var diagnostics []domain.Diagnostic
	contentDocument, resolvedPath, siteDiagnostics, contentErr := s.resolvedContentForLocale(siteID, locale)
	if contentErr != nil {
		return nil, contentErr
	}
	diagnostics = append(diagnostics, siteDiagnostics...)
	if !hasErrors(siteDiagnostics) && resolvedPath != "" {
		resolved, marshalErr := json.Marshal(contentDocument)
		if marshalErr != nil {
			return nil, marshalErr
		}
		contentDiagnostics, validationErr := s.validateContentDocumentWithLocalePolicy(path.Join("liapoldus", "content", siteID, locale+".json"), resolved, true)
		if validationErr != nil {
			return nil, validationErr
		}
		diagnostics = append(diagnostics, contentDiagnostics...)
	} else if !hasErrors(siteDiagnostics) {
		diagnostics = append(diagnostics, domain.Diagnostic{Code: "content.document-required", Severity: "error", Path: contentPath, Message: "site content document is required"})
	}
	if project.SchemaVersion != 1 {
		diagnostics = append(diagnostics, domain.Diagnostic{Code: "project.schema-version", Severity: "error", Path: "liapoldus/project.json", Message: "schemaVersion must be 1"})
	}
	if strings.TrimSpace(project.ID) == "" {
		diagnostics = append(diagnostics, domain.Diagnostic{Code: "project.id-required", Severity: "error", Path: "liapoldus/project.json", Message: "id is required"})
	}
	if strings.TrimSpace(project.Name) == "" {
		diagnostics = append(diagnostics, domain.Diagnostic{Code: "project.name-required", Severity: "error", Path: "liapoldus/project.json", Message: "name is required"})
	}
	if !strings.HasPrefix(project.React.Entry, "src/") {
		diagnostics = append(diagnostics, domain.Diagnostic{Code: "project.react-entry", Severity: "error", Path: "liapoldus/project.json", Message: "react.entry must point inside src/"})
	}
	for _, component := range project.Components {
		path := "src/components/" + strings.ToLower(component) + "/schema.json"
		file, readErr := s.repository.Read(path)
		if readErr == nil {
			componentDiagnostics, _ := domain.ValidateComponentSchema(file.Content, path)
			diagnostics = append(diagnostics, componentDiagnostics...)
		} else if readErr != domain.ErrNotFound {
			return nil, readErr
		}
	}
	if routeFile, routeErr := s.repository.Read("liapoldus/routes/development.json"); routeErr == nil {
		routeDiagnostics, validationErr := domain.ValidateRouteDocument(routeFile.Content, project.Pages)
		if validationErr != nil {
			return nil, validationErr
		}
		diagnostics = append(diagnostics, routeDiagnostics...)
		var routeDocument domain.RouteDocument
		if json.Unmarshal(routeFile.Content, &routeDocument) == nil {
			for _, route := range routeDocument.Routes {
				pagePath := "src/pages/" + pageFileID(route.Page) + ".page.tsx"
				if _, pageErr := s.repository.Read(pagePath); pageErr == domain.ErrNotFound {
					diagnostics = append(diagnostics, domain.Diagnostic{Code: "route.page-source", Severity: "error", Path: pagePath, Message: "route page source is missing"})
				} else if pageErr != nil {
					return nil, pageErr
				}
				for _, layout := range routeLayouts(route) {
					layoutPath := "src/layouts/" + layout + ".layout.tsx"
					if _, layoutErr := s.repository.Read(layoutPath); layoutErr == domain.ErrNotFound {
						diagnostics = append(diagnostics, domain.Diagnostic{Code: "route.layout-source", Severity: "error", Path: layoutPath, Message: "route layout source is missing"})
					} else if layoutErr != nil {
						return nil, layoutErr
					}
				}
			}
		}
	} else if routeErr != domain.ErrNotFound {
		return nil, routeErr
	}
	return diagnostics, nil
}
