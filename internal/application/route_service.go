package application

import (
	"encoding/json"
	"fmt"
	"github.com/Liapoldus/Constructor/internal/domain"
	"regexp"
	"strings"
)

var environmentID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

type RouteService struct{ projects *ProjectService }

func NewRouteService(projects *ProjectService) *RouteService {
	return &RouteService{projects: projects}
}
func (s *RouteService) path(environment string) (string, error) {
	if !environmentID.MatchString(environment) {
		return "", domain.ErrInvalidPath
	}
	return fmt.Sprintf("liapoldus/routes/%s.json", environment), nil
}
func (s *RouteService) Read(environment string) (domain.File, error) {
	path, err := s.path(environment)
	if err != nil {
		return domain.File{}, err
	}
	return s.projects.Read(path)
}
func (s *RouteService) Save(environment, revision string, raw []byte) (domain.File, []domain.Diagnostic, error) {
	path, err := s.path(environment)
	if err != nil {
		return domain.File{}, nil, err
	}
	project, _, err := s.projects.Manifest()
	if err != nil {
		return domain.File{}, nil, err
	}
	diagnostics, err := domain.ValidateRouteDocument(raw, project.Pages)
	if err != nil {
		return domain.File{}, nil, err
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			return domain.File{}, diagnostics, fmt.Errorf("route document validation failed")
		}
	}
	var document domain.RouteDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return domain.File{}, nil, err
	}
	siteDiagnostics, err := s.projects.validateRouteSiteReferences(path, document.Routes)
	diagnostics = append(diagnostics, siteDiagnostics...)
	if err != nil {
		return domain.File{}, diagnostics, err
	}
	if hasErrors(siteDiagnostics) {
		return domain.File{}, diagnostics, fmt.Errorf("route page Site reference validation failed")
	}
	sourceDiagnostics := s.validateSources(document.Routes)
	diagnostics = append(diagnostics, sourceDiagnostics...)
	if len(sourceDiagnostics) > 0 {
		return domain.File{}, diagnostics, fmt.Errorf("route source validation failed")
	}
	file, err := s.projects.Write(path, revision, raw)
	return file, diagnostics, err
}

func (s *RouteService) validateSources(routes []domain.Route) []domain.Diagnostic {
	var diagnostics []domain.Diagnostic
	for _, route := range routes {
		pagePath := "src/pages/" + pageFileID(route.Page) + ".page.tsx"
		if diagnostic := requireSource(s.projects, pagePath, "route.page-source"); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
		for _, layout := range routeLayouts(route) {
			layoutPath := "src/layouts/" + layout + ".layout.tsx"
			if diagnostic := requireSource(s.projects, layoutPath, "route.layout-source"); diagnostic != nil {
				diagnostics = append(diagnostics, *diagnostic)
			}
		}
	}
	return diagnostics
}

func (s *RouteService) Generate(environment string) (domain.File, []domain.Diagnostic, error) {
	content, diagnostics, err := s.Render(environment)
	if err != nil {
		return domain.File{}, diagnostics, err
	}
	target := "src/generated/routes.tsx"
	current, readErr := s.projects.Read(target)
	expected := ""
	if readErr == nil {
		expected = current.Revision
	} else if readErr != domain.ErrNotFound {
		return domain.File{}, nil, readErr
	}
	generated, err := s.projects.Write(target, expected, content)
	return generated, diagnostics, err
}

func (s *RouteService) Render(environment string) ([]byte, []domain.Diagnostic, error) {
	routeFile, err := s.Read(environment)
	if err != nil {
		return nil, nil, err
	}
	project, _, err := s.projects.Manifest()
	if err != nil {
		return nil, nil, err
	}
	diagnostics, err := domain.ValidateRouteDocument(routeFile.Content, project.Pages)
	if err != nil {
		return nil, nil, err
	}
	var sourceDocument domain.RouteDocument
	if err := json.Unmarshal(routeFile.Content, &sourceDocument); err != nil {
		return nil, nil, err
	}
	siteDiagnostics, err := s.projects.validateRouteSiteReferences(routeFile.Path, sourceDocument.Routes)
	diagnostics = append(diagnostics, siteDiagnostics...)
	if err != nil {
		return nil, diagnostics, err
	}
	if hasErrors(siteDiagnostics) {
		return nil, diagnostics, fmt.Errorf("route page Site reference validation failed")
	}
	sourceDiagnostics := s.validateSources(sourceDocument.Routes)
	diagnostics = append(diagnostics, sourceDiagnostics...)
	if len(sourceDiagnostics) > 0 {
		return nil, diagnostics, fmt.Errorf("route source validation failed")
	}
	for _, item := range diagnostics {
		if item.Severity == "error" {
			return nil, diagnostics, fmt.Errorf("route document validation failed")
		}
	}
	var document domain.RouteDocument
	if err := json.Unmarshal(routeFile.Content, &document); err != nil {
		return nil, nil, err
	}
	var source strings.Builder
	source.WriteString("import React from 'react'\nimport type { RouteObject } from 'react-router-dom'\n")
	pageImports, layoutImports := map[string]string{}, map[string]string{}
	document.Routes = domain.SortRoutes(document.Routes)
	for _, route := range document.Routes {
		if !route.Lazy && route.Chunk != "lazy" && route.Chunk != "separate" {
			pagePath := "src/pages/" + pageFileID(route.Page) + ".page.tsx"
			if diagnostic := requireSource(s.projects, pagePath, "route.page-source"); diagnostic != nil {
				return nil, []domain.Diagnostic{*diagnostic}, fmt.Errorf("page source %q is missing", route.Page)
			}
			if _, exists := pageImports[route.Page]; !exists {
				name := safePageExport(route.Page)
				pageImports[route.Page] = name
				source.WriteString(fmt.Sprintf("import %s from '../pages/%s.page'\n", name, pageFileID(route.Page)))
			}
		}
		for _, layout := range routeLayouts(route) {
			if _, exists := layoutImports[layout]; exists {
				continue
			}
			layoutPath := "src/layouts/" + layout + ".layout.tsx"
			if diagnostic := requireSource(s.projects, layoutPath, "route.layout-source"); diagnostic != nil {
				return nil, []domain.Diagnostic{*diagnostic}, fmt.Errorf("layout source %q is missing", layout)
			}
			name := safePageExport(layout)
			layoutImports[layout] = name
			source.WriteString(fmt.Sprintf("import %s from '../layouts/%s.layout'\n", name, layout))
		}
	}
	source.WriteString(`
export type AccessCheck = (policy: string) => boolean
type RouteView = React.ComponentType
const withLayouts = (Page: RouteView, layouts: RouteView[]): RouteView => {
  const Composed = () => layouts.reduceRight<React.ReactNode>((child, Layout) => React.createElement(Layout, null, child), React.createElement(Page))
  return Composed
}
const withAccess = (canAccess: AccessCheck, policy: string | undefined, Page: RouteView, layouts: RouteView[]): RouteView => {
  const View = withLayouts(Page, layouts)
  if (!policy) return View
  return () => canAccess(policy) ? React.createElement(View) : React.createElement('main', { role: 'alert', 'data-access-denied': policy }, 'Access denied')
}

export const createRoutes = (canAccess: AccessCheck): RouteObject[] => [
`)
	for _, route := range document.Routes {
		pagePath := "src/pages/" + pageFileID(route.Page) + ".page.tsx"
		if diagnostic := requireSource(s.projects, pagePath, "route.page-source"); diagnostic != nil {
			return nil, []domain.Diagnostic{*diagnostic}, fmt.Errorf("page source %q is missing", route.Page)
		}
		handle, _ := json.Marshal(map[string]any{"layouts": routeLayouts(route), "access": route.Access, "chunk": route.Chunk, "preload": route.Preload, "metadata": route.Metadata})
		layouts := make([]string, 0)
		for _, layout := range routeLayouts(route) {
			layouts = append(layouts, layoutImports[layout])
		}
		layoutExpr := "[" + strings.Join(layouts, ", ") + "]"
		if route.Lazy || route.Chunk == "lazy" || route.Chunk == "separate" {
			source.WriteString(fmt.Sprintf("  { path: %q, lazy: async () => { const Page = (await import('../pages/%s.page')).default; return { Component: withAccess(canAccess, %s, Page, %s) } }, handle: %s },\n", route.Path, pageFileID(route.Page), jsOptionalString(route.Access), layoutExpr, handle))
		} else {
			source.WriteString(fmt.Sprintf("  { path: %q, Component: withAccess(canAccess, %s, %s, %s), handle: %s },\n", route.Path, jsOptionalString(route.Access), pageImports[route.Page], layoutExpr, handle))
		}
	}
	source.WriteString("]\n\n// Protected routes remain denied until the host provides an auth-backed resolver.\nexport const routes: RouteObject[] = createRoutes(() => false)\n")
	return []byte(source.String()), diagnostics, nil
}
func routeLayouts(route domain.Route) []string {
	layouts := append([]string(nil), route.Layouts...)
	if route.Layout != "" {
		// The legacy singular field is the outermost wrapper so older projects
		// retain the same shell behavior when migrating to the ordered chain.
		layouts = append([]string{route.Layout}, layouts...)
	}
	return layouts
}
func jsOptionalString(value string) string {
	if value == "" {
		return "undefined"
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
func requireSource(projects *ProjectService, path, code string) *domain.Diagnostic {
	if _, err := projects.Read(path); err != nil {
		return &domain.Diagnostic{Code: code, Severity: "error", Path: path, Message: "referenced source file is missing"}
	}
	return nil
}
func pageFileID(page string) string {
	var out strings.Builder
	for index, r := range page {
		if r >= 'A' && r <= 'Z' {
			if index > 0 {
				out.WriteByte('-')
			}
			out.WriteRune(r + ('a' - 'A'))
		} else if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			out.WriteRune(r)
		} else {
			out.WriteByte('-')
		}
	}
	return strings.Trim(out.String(), "-")
}
func safePageExport(page string) string {
	id := pageFileID(page)
	if id == "" {
		return "Page"
	}
	parts := strings.Split(id, "-")
	var out strings.Builder
	out.WriteString("Page")
	for _, part := range parts {
		if part != "" {
			out.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return out.String()
}
