package domain

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

type RouteDocument struct {
	SchemaVersion int     `json:"schemaVersion"`
	ID            string  `json:"id"`
	Routes        []Route `json:"routes"`
}
type Route struct {
	ID       string            `json:"id"`
	Path     string            `json:"path"`
	Page     string            `json:"page"`
	Layout   string            `json:"layout,omitempty"`
	Layouts  []string          `json:"layouts,omitempty"`
	Access   string            `json:"access,omitempty"`
	Chunk    string            `json:"chunk,omitempty"`
	Lazy     bool              `json:"lazy,omitempty"`
	Preload  []string          `json:"preload,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

var routeIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
var routeParamPattern = regexp.MustCompile(`^:[a-zA-Z][a-zA-Z0-9]*$`)

func ValidateRouteDocument(raw []byte, pages []string) ([]Diagnostic, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var document RouteDocument
	if err := decoder.Decode(&document); err != nil {
		return []Diagnostic{{Code: "route.invalid-document", Severity: "error", Path: "liapoldus/routes", Message: err.Error()}}, nil
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return []Diagnostic{{Code: "route.trailing-json", Severity: "error", Path: "liapoldus/routes", Message: "document must contain exactly one JSON value"}}, nil
	}
	var diagnostics []Diagnostic
	add := func(code, message string) {
		diagnostics = append(diagnostics, Diagnostic{Code: code, Severity: "error", Path: "liapoldus/routes", Message: message})
	}
	if document.SchemaVersion != 1 {
		add("route.schema-version", "schemaVersion must be 1")
	}
	if !routeIDPattern.MatchString(document.ID) {
		add("route.document-id", "document id must be kebab-case")
	}
	pageSet := map[string]bool{}
	for _, page := range pages {
		pageSet[page] = true
	}
	ids, paths := map[string]bool{}, map[string]bool{}
	for _, route := range document.Routes {
		if !routeIDPattern.MatchString(route.ID) {
			add("route.id", "route id must be kebab-case")
		}
		if ids[route.ID] {
			add("route.duplicate-id", fmt.Sprintf("duplicate route id %q", route.ID))
		}
		ids[route.ID] = true
		if route.Path == "" || route.Path[0] != '/' || strings.Contains(route.Path, "//") {
			add("route.path", fmt.Sprintf("route %q must use a normalized leading-slash path", route.ID))
		}
		if route.Path != "/" && strings.HasSuffix(route.Path, "/") {
			add("route.path-normalization", fmt.Sprintf("route %q must not end with a slash", route.ID))
		}
		if strings.ContainsAny(route.Path, "?#") || strings.Contains(route.Path, "/./") || strings.Contains(route.Path, "/../") {
			add("route.path-normalization", fmt.Sprintf("route %q contains a non-normalized path", route.ID))
		}
		if paths[route.Path] {
			add("route.duplicate-path", fmt.Sprintf("duplicate route path %q", route.Path))
		}
		paths[route.Path] = true
		segments := strings.Split(strings.Trim(route.Path, "/"), "/")
		for index, segment := range segments {
			if strings.HasPrefix(segment, ":") && !routeParamPattern.MatchString(segment) {
				add("route.path-param", fmt.Sprintf("invalid route parameter %q", segment))
			}
			if strings.Contains(segment, "*") && (segment != "*" || index != len(segments)-1) {
				add("route.catch-all", fmt.Sprintf("route %q catch-all must be a final * segment", route.ID))
			}
		}
		if strings.Contains(route.Path, "*") && !strings.HasSuffix(route.Path, "/*") {
			add("route.catch-all", fmt.Sprintf("catch-all route %q must be final", route.ID))
		}
		if !pageSet[route.Page] {
			add("route.page", fmt.Sprintf("route %q references unknown page %q", route.ID, route.Page))
		}
		if route.Access != "" && !routeIDPattern.MatchString(route.Access) {
			add("route.access", fmt.Sprintf("route %q has an invalid access policy reference", route.ID))
		}
		if route.Layout != "" && !routeIDPattern.MatchString(route.Layout) {
			add("route.layout", fmt.Sprintf("route %q has an invalid layout reference", route.ID))
		}
		seenLayouts := map[string]bool{}
		for _, layout := range route.Layouts {
			if !routeIDPattern.MatchString(layout) {
				add("route.layout", fmt.Sprintf("route %q has an invalid layout reference %q", route.ID, layout))
			}
			if seenLayouts[layout] {
				add("route.layout-duplicate", fmt.Sprintf("route %q repeats layout %q", route.ID, layout))
			}
			seenLayouts[layout] = true
		}
		if route.Chunk != "" && route.Chunk != "same" && route.Chunk != "separate" && route.Chunk != "lazy" && route.Chunk != "preload" {
			add("route.chunk", fmt.Sprintf("route %q has unsupported chunk policy", route.ID))
		}
	}
	for _, route := range document.Routes {
		for _, preload := range route.Preload {
			if !ids[preload] {
				add("route.preload", fmt.Sprintf("route %q preloads unknown route %q", route.ID, preload))
			}
		}
	}
	return diagnostics, nil
}

func SortRoutes(routes []Route) []Route {
	sorted := append([]Route(nil), routes...)
	class := func(route Route) int {
		if strings.Contains(route.Path, "*") {
			return 2
		}
		for _, part := range strings.Split(strings.Trim(route.Path, "/"), "/") {
			if strings.HasPrefix(part, ":") {
				return 1
			}
		}
		return 0
	}
	sort.SliceStable(sorted, func(i, j int) bool { return class(sorted[i]) < class(sorted[j]) })
	return sorted
}
