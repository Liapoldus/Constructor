package application

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"strings"

	"github.com/Liapoldus/Constructor/internal/domain"
	xwebp "golang.org/x/image/webp"
)

const assetCatalogPath = "liapoldus/assets.json"

func (s *ProjectService) verifyAssetItems(items []domain.AssetItem, documentPath string) ([]domain.Diagnostic, error) {
	var diagnostics []domain.Diagnostic
	for _, item := range items {
		binary, readErr := s.repository.Read(item.Path)
		if readErr == domain.ErrNotFound {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "asset.file-required", Severity: "error", Path: documentPath, Message: "asset file is missing: " + item.Path})
			continue
		}
		if readErr != nil {
			return nil, readErr
		}
		if int64(len(binary.Content)) != item.Size {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "asset.size-mismatch", Severity: "error", Path: documentPath, Message: "asset size does not match file: " + item.Path})
		}
		digest := sha256.Sum256(binary.Content)
		if hex.EncodeToString(digest[:]) != item.SHA256 {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "asset.checksum-mismatch", Severity: "error", Path: documentPath, Message: "asset sha256 does not match file: " + item.Path})
		}
		for _, variant := range item.Variants {
			variantFile, variantErr := s.repository.Read(variant.Path)
			if variantErr == domain.ErrNotFound {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "asset.variant-required", Severity: "error", Path: documentPath, Message: "asset variant is missing: " + variant.Path})
				continue
			}
			if variantErr != nil {
				return nil, variantErr
			}
			if int64(len(variantFile.Content)) != variant.Size {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "asset.variant-size-mismatch", Severity: "error", Path: documentPath, Message: "asset variant size does not match file: " + variant.Path})
			}
			variantDigest := sha256.Sum256(variantFile.Content)
			if hex.EncodeToString(variantDigest[:]) != variant.SHA256 {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "asset.variant-checksum-mismatch", Severity: "error", Path: documentPath, Message: "asset variant sha256 does not match file: " + variant.Path})
			}
			config, decodeErr := xwebp.DecodeConfig(bytes.NewReader(variantFile.Content))
			if decodeErr != nil || config.Width != variant.Width || config.Height != variant.Height {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "asset.variant-dimensions-mismatch", Severity: "error", Path: documentPath, Message: "asset variant dimensions do not match metadata: " + variant.Path})
			}
		}
	}
	return diagnostics, nil
}

func (s *ProjectService) assetCatalog() (map[string]domain.AssetItem, bool, []domain.Diagnostic, error) {
	document, exists, diagnostics, err := s.readAssetDocument()
	if err != nil {
		return nil, exists, nil, err
	}
	if !exists || hasErrors(diagnostics) {
		return map[string]domain.AssetItem{}, exists, diagnostics, nil
	}
	assets := make(map[string]domain.AssetItem, len(document.Items))
	for _, item := range document.Items {
		assets[item.ID] = item
	}
	return assets, true, diagnostics, nil
}

// Assets returns the validated registry in its canonical document order.
// The mutation lock keeps the registry and its binary files in one view while
// checksums are verified.
func (s *ProjectService) Assets() ([]domain.AssetItem, error) {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	document, exists, diagnostics, err := s.readAssetDocument()
	if err != nil {
		return nil, err
	}
	if hasErrors(diagnostics) {
		return nil, domain.StructuredValidationError{Diagnostics: diagnostics}
	}
	if !exists {
		return []domain.AssetItem{}, nil
	}
	return document.Items, nil
}

func (s *ProjectService) readAssetDocument() (domain.AssetDocument, bool, []domain.Diagnostic, error) {
	file, err := s.repository.Read(assetCatalogPath)
	if err == domain.ErrNotFound {
		return domain.AssetDocument{}, false, nil, nil
	}
	if err != nil {
		return domain.AssetDocument{}, false, nil, err
	}
	document, diagnostics := domain.DecodeAssetDocument(file.Content, assetCatalogPath)
	if hasErrors(diagnostics) {
		return document, true, diagnostics, nil
	}
	fileDiagnostics, err := s.verifyAssetItems(document.Items, assetCatalogPath)
	if err != nil {
		return domain.AssetDocument{}, true, nil, err
	}
	return document, true, append(diagnostics, fileDiagnostics...), nil
}

func (s *ProjectService) validateAssetReferences(instance domain.ContentInstance, schema domain.ComponentSchema, assets map[string]domain.AssetItem, catalogExists bool, contentPath string) []domain.Diagnostic {
	var diagnostics []domain.Diagnostic
	for _, field := range schema.Fields {
		if field.Type != "image" && field.Type != "icon" && field.Type != "file" {
			continue
		}
		value, exists := instance.Fields[field.Key]
		if !exists {
			value, exists = field.Default, field.Default != nil
		}
		if !exists || value == nil {
			continue
		}
		reference, ok := value.(map[string]any)
		if !ok {
			continue // the field validator reports malformed reference shapes
		}
		id, ok := reference["id"].(string)
		if !ok || id == "" {
			continue
		}
		if !catalogExists {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "content.asset-catalog-required", Severity: "error", Path: contentPath, Message: "asset reference requires liapoldus/assets.json", PageID: instance.PageID, InstanceID: instance.ID, FieldKey: field.Key})
			continue
		}
		asset, exists := assets[id]
		if !exists {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "content.asset-reference", Severity: "error", Path: contentPath, Message: "asset " + id + " is not declared in the asset catalog", PageID: instance.PageID, InstanceID: instance.ID, FieldKey: field.Key})
			continue
		}
		if field.Type != "file" && asset.Type != field.Type {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "content.asset-type-reference", Severity: "error", Path: contentPath, Message: "asset " + id + " has type " + asset.Type + ", expected " + field.Type, PageID: instance.PageID, InstanceID: instance.ID, FieldKey: field.Key})
		}
	}
	return diagnostics
}

func (s *ProjectService) validateManifestPageReferences(project domain.Project) ([]domain.Diagnostic, error) {
	lister, ok := s.repository.(domain.ProjectFileLister)
	if !ok {
		return nil, nil
	}
	sites, err := s.Sites()
	if err != nil {
		return nil, err
	}
	var diagnostics []domain.Diagnostic
	for _, site := range sites {
		for _, page := range site.Pages {
			if !contains(project.Pages, page.ID) {
				diagnostics = append(diagnostics, domain.Diagnostic{
					Code: "project.page-site-reference", Severity: "error", Path: "liapoldus/project.json",
					PageID: page.ID, Message: "page is still declared by Site " + site.ID,
				})
			}
		}
	}
	routePaths, err := lister.List("liapoldus/routes")
	if err != nil {
		return nil, err
	}
	for _, routePath := range routePaths {
		if !strings.HasPrefix(routePath, "liapoldus/routes/") || !strings.HasSuffix(routePath, ".json") {
			continue
		}
		file, readErr := s.repository.Read(routePath)
		if readErr != nil {
			return nil, readErr
		}
		items, validationErr := domain.ValidateRouteDocument(file.Content, project.Pages)
		if validationErr != nil {
			return nil, validationErr
		}
		diagnostics = append(diagnostics, items...)
	}
	return diagnostics, nil
}

func (s *ProjectService) validateManifestComponents(project domain.Project, documentPath string) ([]domain.Diagnostic, error) {
	var diagnostics []domain.Diagnostic
	componentIDs := make(map[string]bool, len(project.Components))
	for _, component := range project.Components {
		componentID := strings.ToLower(component)
		componentIDs[componentID] = true
		schemaPath := path.Join("src", "components", componentID, "schema.json")
		file, err := s.repository.Read(schemaPath)
		if err == domain.ErrNotFound {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "project.component-schema-required", Severity: "error", Path: documentPath, Message: "component " + component + " has no schema at " + schemaPath})
			continue
		}
		if err != nil {
			return nil, err
		}
		items, validationErr := domain.ValidateComponentSchema(file.Content, schemaPath)
		if validationErr != nil {
			return nil, validationErr
		}
		diagnostics = append(diagnostics, items...)
		if hasErrors(items) {
			continue
		}
		var schema domain.ComponentSchema
		if err := json.Unmarshal(file.Content, &schema); err != nil {
			continue
		}
		if schema.ID != componentID {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "project.component-schema-id", Severity: "error", Path: schemaPath, Message: "schema id must match its component directory"})
			continue
		}
		if _, err := s.repository.Read(schema.Source); err == domain.ErrNotFound {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "project.component-source-required", Severity: "error", Path: schemaPath, Message: "component source is missing: " + schema.Source})
		} else if err != nil {
			return nil, err
		}
	}
	if lister, ok := s.repository.(domain.ProjectFileLister); ok {
		sitePaths, err := lister.List("liapoldus/sites")
		if err != nil {
			return nil, err
		}
		for _, sitePath := range sitePaths {
			if !isSiteDocumentPath(sitePath) {
				continue
			}
			siteFile, err := s.repository.Read(sitePath)
			if err != nil {
				return nil, err
			}
			site, siteDiagnostics := domain.DecodeSiteDocument(siteFile.Content, sitePath)
			if hasErrors(siteDiagnostics) {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "project.site-unreadable", Severity: "error", Path: sitePath, Message: "cannot safely change declared components while a Site document is invalid"})
				continue
			}
			contentPaths, err := lister.List(path.Join("liapoldus", "content", site.ID))
			if err != nil {
				return nil, err
			}
			for _, contentPath := range contentPaths {
				if !isContentDocumentPath(contentPath) || path.Dir(contentPath) != path.Join("liapoldus", "content", site.ID) {
					continue
				}
				contentFile, err := s.repository.Read(contentPath)
				if err != nil {
					return nil, err
				}
				document, contentDiagnostics := domain.DecodeContentDocument(contentFile.Content, contentPath)
				if hasErrors(contentDiagnostics) {
					diagnostics = append(diagnostics, domain.Diagnostic{Code: "project.content-unreadable", Severity: "error", Path: contentPath, Message: "cannot safely change declared components while a content document is invalid"})
					continue
				}
				for _, instance := range document.Instances {
					if !componentIDs[strings.ToLower(instance.Component)] {
						diagnostics = append(diagnostics, domain.Diagnostic{Code: "project.component-content-reference", Severity: "error", Path: documentPath, PageID: instance.PageID, InstanceID: instance.ID, Message: "component " + instance.Component + " is still referenced by content " + contentPath})
					}
				}
			}
		}
	}
	return diagnostics, nil
}

func (s *ProjectService) validateComponentSchemaContent(schema domain.ComponentSchema) ([]domain.Diagnostic, error) {
	lister, ok := s.repository.(domain.ProjectFileLister)
	if !ok {
		return nil, nil
	}
	sitePaths, err := lister.List("liapoldus/sites")
	if err != nil {
		return nil, err
	}
	var diagnostics []domain.Diagnostic
	for _, sitePath := range sitePaths {
		if !isSiteDocumentPath(sitePath) {
			continue
		}
		siteFile, err := s.repository.Read(sitePath)
		if err != nil {
			return nil, err
		}
		site, siteDiagnostics := domain.DecodeSiteDocument(siteFile.Content, sitePath)
		if hasErrors(siteDiagnostics) {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "component.site-unreadable", Severity: "error", Path: sitePath, Message: "cannot safely update a component schema while a Site document is invalid"})
			continue
		}
		contentPaths, err := lister.List(path.Join("liapoldus", "content", site.ID))
		if err != nil {
			return nil, err
		}
		for _, contentPath := range contentPaths {
			if !isContentDocumentPath(contentPath) || path.Dir(contentPath) != path.Join("liapoldus", "content", site.ID) {
				continue
			}
			contentFile, err := s.repository.Read(contentPath)
			if err != nil {
				return nil, err
			}
			document, contentDiagnostics := domain.DecodeContentDocument(contentFile.Content, contentPath)
			if hasErrors(contentDiagnostics) {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "component.content-unreadable", Severity: "error", Path: contentPath, Message: "cannot safely update a component schema while a content document is invalid"})
				continue
			}
			for _, instance := range document.Instances {
				if strings.EqualFold(instance.Component, schema.ID) {
					diagnostics = append(diagnostics, domain.ValidateInstanceContentForWrite(schema, instance, contentPath)...)
				}
			}
		}
	}
	return diagnostics, nil
}

func sitePageSet(sites []domain.SiteDocument) map[string]bool {
	pages := map[string]bool{}
	for _, site := range sites {
		for _, page := range site.Pages {
			pages[page.ID] = true
		}
	}
	return pages
}

func routeSiteReferenceDiagnostics(routePath string, routes []domain.Route, pages map[string]bool) []domain.Diagnostic {
	var diagnostics []domain.Diagnostic
	for _, route := range routes {
		if !pages[route.Page] {
			diagnostics = append(diagnostics, domain.Diagnostic{
				Code: "route.page-site-reference", Severity: "error", Path: routePath,
				PageID: route.Page, Message: "route " + route.ID + " references a page that is not present in any Site",
			})
		}
	}
	return diagnostics
}

func (s *ProjectService) validateSiteContentReferences(sitePath string, site domain.SiteDocument) ([]domain.Diagnostic, error) {
	lister, ok := s.repository.(domain.ProjectFileLister)
	if !ok {
		return nil, nil
	}
	contentPaths, err := lister.List(path.Join("liapoldus", "content", site.ID))
	if err != nil {
		return nil, err
	}
	pages := map[string]bool{}
	for _, page := range site.Pages {
		pages[page.ID] = true
	}
	var diagnostics []domain.Diagnostic
	for _, contentPath := range contentPaths {
		if !isContentDocumentPath(contentPath) || path.Dir(contentPath) != path.Join("liapoldus", "content", site.ID) {
			continue
		}
		file, readErr := s.repository.Read(contentPath)
		if readErr != nil {
			return nil, readErr
		}
		document, contentDiagnostics := domain.DecodeContentDocument(file.Content, contentPath)
		if hasErrors(contentDiagnostics) {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "site.content-unreadable", Severity: "error", Path: contentPath, Message: "cannot safely change Site pages while its content document is invalid"})
			continue
		}
		for _, instance := range document.Instances {
			if !pages[instance.PageID] {
				diagnostics = append(diagnostics, domain.Diagnostic{
					Code: "site.page-content-reference", Severity: "error", Path: sitePath,
					PageID: instance.PageID, InstanceID: instance.ID,
					Message: "page is still referenced by content document " + contentPath,
				})
			}
		}
	}
	return diagnostics, nil
}

func (s *ProjectService) validateRouteSiteReferences(routePath string, routes []domain.Route) ([]domain.Diagnostic, error) {
	if _, ok := s.repository.(domain.ProjectFileLister); !ok {
		return nil, nil
	}
	sites, err := s.Sites()
	if err != nil {
		return nil, err
	}
	return routeSiteReferenceDiagnostics(routePath, routes, sitePageSet(sites)), nil
}

func (s *ProjectService) validateSiteRouteReferences(site domain.SiteDocument) ([]domain.Diagnostic, error) {
	if _, ok := s.repository.(domain.ProjectFileLister); !ok {
		return nil, nil
	}
	sites, err := s.Sites()
	if err != nil {
		return nil, err
	}
	replaced := false
	for index := range sites {
		if sites[index].ID == site.ID {
			sites[index] = site
			replaced = true
			break
		}
	}
	if !replaced {
		sites = append(sites, site)
	}
	return s.validateAllRouteReferences(sites)
}

func (s *ProjectService) validateAllRouteReferences(sites []domain.SiteDocument) ([]domain.Diagnostic, error) {
	lister, ok := s.repository.(domain.ProjectFileLister)
	if !ok {
		return nil, domain.ErrUnsupported
	}
	routePaths, err := lister.List("liapoldus/routes")
	if err != nil {
		return nil, err
	}
	project, _, err := s.repository.Manifest()
	if err != nil {
		return nil, err
	}
	pages := sitePageSet(sites)
	var diagnostics []domain.Diagnostic
	for _, routePath := range routePaths {
		if !strings.HasPrefix(routePath, "liapoldus/routes/") || !strings.HasSuffix(routePath, ".json") {
			continue
		}
		file, readErr := s.repository.Read(routePath)
		if readErr != nil {
			return nil, readErr
		}
		items, validationErr := domain.ValidateRouteDocument(file.Content, project.Pages)
		if validationErr != nil {
			return nil, validationErr
		}
		diagnostics = append(diagnostics, items...)
		if hasErrors(items) {
			continue
		}
		var document domain.RouteDocument
		if decodeErr := json.Unmarshal(file.Content, &document); decodeErr != nil {
			continue
		}
		diagnostics = append(diagnostics, routeSiteReferenceDiagnostics(routePath, document.Routes, pages)...)
	}
	return diagnostics, nil
}
