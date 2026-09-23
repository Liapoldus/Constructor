package application

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/Liapoldus/Constructor/internal/domain"
)

const themeDocumentDirectory = "liapoldus/themes"

type ThemeResource struct {
	domain.ThemeDocument
	Revision string `json:"revision"`
}

func (s *ProjectService) Themes() ([]ThemeResource, error) {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	themes, diagnostics, err := s.themeDocuments()
	if err != nil {
		return nil, err
	}
	if hasErrors(diagnostics) {
		return nil, domain.StructuredValidationError{Diagnostics: diagnostics}
	}
	resources := make([]ThemeResource, 0, len(themes))
	for _, theme := range themes {
		file, readErr := s.repository.Read(path.Join(themeDocumentDirectory, theme.ID+".json"))
		if readErr != nil {
			return nil, readErr
		}
		resources = append(resources, ThemeResource{ThemeDocument: theme, Revision: file.Revision})
	}
	return resources, nil
}

func (s *ProjectService) themeDocuments() ([]domain.ThemeDocument, []domain.Diagnostic, error) {
	lister, ok := s.repository.(domain.ProjectFileLister)
	if !ok {
		return nil, nil, nil
	}
	paths, err := lister.List(themeDocumentDirectory)
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(paths)
	documents := make([]domain.ThemeDocument, 0, len(paths))
	var diagnostics []domain.Diagnostic
	seenIDs := map[string]bool{}
	for _, documentPath := range paths {
		if !strings.HasPrefix(documentPath, themeDocumentDirectory+"/") || !strings.HasSuffix(documentPath, ".json") || strings.Count(strings.TrimPrefix(documentPath, themeDocumentDirectory+"/"), "/") != 0 {
			continue
		}
		file, readErr := s.repository.Read(documentPath)
		if readErr != nil {
			return nil, nil, readErr
		}
		theme, found := domain.DecodeThemeDocument(file.Content, documentPath)
		if strings.TrimSuffix(path.Base(documentPath), ".json") != theme.ID {
			found = append(found, domain.Diagnostic{Code: "theme.id-path", Severity: "error", Path: documentPath, Message: "theme id must match its filename"})
		}
		if seenIDs[theme.ID] {
			found = append(found, domain.Diagnostic{Code: "theme.duplicate-id", Severity: "error", Path: documentPath, Message: "theme IDs must be unique"})
		}
		seenIDs[theme.ID] = true
		documents = append(documents, theme)
		diagnostics = append(diagnostics, found...)
	}
	return documents, diagnostics, nil
}

func resolveSiteTheme(site domain.SiteDocument, themes []domain.ThemeDocument) (*domain.ThemeDocument, []domain.Diagnostic) {
	if len(themes) == 0 {
		if site.ThemeID != "" {
			return nil, []domain.Diagnostic{{Code: "site.theme-not-found", Severity: "error", Path: path.Join("liapoldus", "sites", site.ID+".json"), Message: "selected theme does not exist: " + site.ThemeID}}
		}
		return nil, nil
	}
	selectedID := site.ThemeID
	if selectedID == "" {
		for index := range themes {
			if themes[index].ID == "default" {
				selectedID = "default"
				break
			}
		}
		if selectedID == "" && len(themes) == 1 {
			selectedID = themes[0].ID
		}
	}
	if selectedID == "" {
		return nil, []domain.Diagnostic{{Code: "site.theme-required", Severity: "error", Path: path.Join("liapoldus", "sites", site.ID+".json"), Message: "Site must select a theme when the project contains multiple themes"}}
	}
	for index := range themes {
		if themes[index].ID == selectedID {
			return &themes[index], nil
		}
	}
	return nil, []domain.Diagnostic{{Code: "site.theme-not-found", Severity: "error", Path: path.Join("liapoldus", "sites", site.ID+".json"), Message: "selected theme does not exist: " + selectedID}}
}

func renderThemeCSS(theme *domain.ThemeDocument) ([]byte, error) {
	if theme == nil {
		return []byte(":root {}\n"), nil
	}
	renderValues := func(values map[string]any) string {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var builder strings.Builder
		for _, key := range keys {
			fmt.Fprintf(&builder, "  --%s: %s;\n", strings.ReplaceAll(key, ".", "-"), cssThemeValue(values[key]))
		}
		return builder.String()
	}
	base := make(map[string]any, len(theme.Tokens))
	for key, token := range theme.Tokens {
		base[key] = token.Value
	}
	var css strings.Builder
	css.WriteString(":root {\n")
	css.WriteString(renderValues(base))
	css.WriteString("}\n")
	for _, variant := range []string{"light", "dark"} {
		values := theme.Variants[variant]
		if len(values) == 0 {
			continue
		}
		selector := `:root[data-color-scheme="` + variant + `"]`
		block := selector + " {\n" + renderValues(values) + "}\n"
		css.WriteString(block)
		if variant == "dark" {
			css.WriteString("@media (prefers-color-scheme: dark) {\n  :root:not([data-color-scheme]) {\n")
			css.WriteString(renderValues(values))
			css.WriteString("  }\n}\n")
		}
	}
	return []byte(css.String()), nil
}

func cssThemeValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return string(typed)
	case float64:
		return fmt.Sprintf("%g", typed)
	default:
		return ""
	}
}

func (s *ProjectService) validateThemeReferences(sites []domain.SiteDocument) ([]domain.Diagnostic, error) {
	themes, diagnostics, err := s.themeDocuments()
	if err != nil {
		return nil, err
	}
	schemas, err := s.componentSchemas()
	if err != nil {
		return nil, err
	}
	for _, site := range sites {
		selected, siteDiagnostics := resolveSiteTheme(site, themes)
		diagnostics = append(diagnostics, siteDiagnostics...)
		if selected != nil {
			diagnostics = append(diagnostics, themeTokenDiagnostics(selected, schemas, path.Join("liapoldus", "sites", site.ID+".json"))...)
		} else if !hasErrors(siteDiagnostics) {
			for componentID, schema := range schemas {
				if len(schema.ThemeTokens) > 0 {
					diagnostics = append(diagnostics, domain.Diagnostic{Code: "component.theme-required", Severity: "error", Path: path.Join("src", "components", componentID, "schema.json"), Message: "component declares themeTokens but Site " + site.ID + " has no selected Theme"})
				}
			}
		}
	}
	return diagnostics, nil
}

func (s *ProjectService) componentSchemas() (map[string]domain.ComponentSchema, error) {
	project, _, err := s.repository.Manifest()
	if err != nil {
		return nil, err
	}
	schemas, _, err := s.loadComponentSchemas(project)
	return schemas, err
}

func themeTokenDiagnostics(theme *domain.ThemeDocument, schemas map[string]domain.ComponentSchema, sitePath string) []domain.Diagnostic {
	var diagnostics []domain.Diagnostic
	for componentID, schema := range schemas {
		for _, tokenPath := range schema.ThemeTokens {
			if _, exists := theme.Tokens[tokenPath]; !exists {
				diagnostics = append(diagnostics, domain.Diagnostic{
					Code: "component.theme-token-reference", Severity: "error",
					Path:    path.Join("src", "components", componentID, "schema.json"),
					Message: "theme token " + tokenPath + " is not defined by the Theme selected by Site " + strings.TrimSuffix(path.Base(sitePath), ".json"),
				})
			}
		}
	}
	return diagnostics
}

func (s *ProjectService) validateSiteThemeTokens(site domain.SiteDocument) ([]domain.Diagnostic, error) {
	themes, _, err := s.themeDocuments()
	if err != nil {
		return nil, err
	}
	selected, diagnostics := resolveSiteTheme(site, themes)
	schemas, err := s.componentSchemas()
	if err != nil {
		return nil, err
	}
	if selected == nil {
		if !hasErrors(diagnostics) {
			for componentID, schema := range schemas {
				if len(schema.ThemeTokens) > 0 {
					diagnostics = append(diagnostics, domain.Diagnostic{Code: "component.theme-required", Severity: "error", Path: path.Join("src", "components", componentID, "schema.json"), Message: "component declares themeTokens but Site " + site.ID + " has no selected Theme"})
				}
			}
		}
		return diagnostics, nil
	}
	return append(diagnostics, themeTokenDiagnostics(selected, schemas, path.Join("liapoldus", "sites", site.ID+".json"))...), nil
}

func (s *ProjectService) validateThemeCandidateReferences(candidate domain.ThemeDocument) ([]domain.Diagnostic, error) {
	sites, err := s.Sites()
	if err != nil {
		return nil, err
	}
	themes, _, err := s.themeDocuments()
	if err != nil {
		return nil, err
	}
	schemas, err := s.componentSchemas()
	if err != nil {
		return nil, err
	}
	var diagnostics []domain.Diagnostic
	for _, site := range sites {
		selected, selectionDiagnostics := resolveSiteTheme(site, themes)
		if hasErrors(selectionDiagnostics) || selected == nil || selected.ID != candidate.ID {
			continue
		}
		diagnostics = append(diagnostics, themeTokenDiagnostics(&candidate, schemas, path.Join("liapoldus", "sites", site.ID+".json"))...)
	}
	return diagnostics, nil
}

func (s *ProjectService) validateComponentThemeReferences(candidate domain.ComponentSchema) ([]domain.Diagnostic, error) {
	sites, err := s.Sites()
	if err != nil {
		return nil, err
	}
	themes, _, err := s.themeDocuments()
	if err != nil {
		return nil, err
	}
	for _, site := range sites {
		selected, diagnostics := resolveSiteTheme(site, themes)
		if hasErrors(diagnostics) {
			continue
		}
		if selected == nil {
			if len(candidate.ThemeTokens) > 0 {
				return []domain.Diagnostic{{Code: "component.theme-required", Severity: "error", Path: path.Join("src", "components", candidate.ID, "schema.json"), Message: "component declares themeTokens but Site " + site.ID + " has no selected Theme"}}, nil
			}
			continue
		}
		if items := themeTokenDiagnostics(selected, map[string]domain.ComponentSchema{candidate.ID: candidate}, path.Join("liapoldus", "sites", site.ID+".json")); len(items) > 0 {
			return items, nil
		}
	}
	return nil, nil
}

func (s *ProjectService) renderThemeForSite(site domain.SiteDocument) ([]byte, error) {
	themes, diagnostics, err := s.themeDocuments()
	if err != nil {
		return nil, err
	}
	_, siteDiagnostics := resolveSiteTheme(site, themes)
	diagnostics = append(diagnostics, siteDiagnostics...)
	if hasErrors(diagnostics) {
		return nil, domain.StructuredValidationError{Diagnostics: diagnostics}
	}
	selected, _ := resolveSiteTheme(site, themes)
	return renderThemeCSS(selected)
}
