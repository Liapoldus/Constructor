package application

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type PageRevisions struct {
	Manifest string `json:"manifest"`
	Site     string `json:"site"`
	Routes   string `json:"routes"`
}

type CreatePageRequest struct {
	SiteID    string        `json:"siteId"`
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	RoutePath string        `json:"routePath"`
	Revisions PageRevisions `json:"revisions"`
}

var reactRoutePathPattern = regexp.MustCompile(`^/(?:[a-zA-Z0-9._~-]+|:[a-zA-Z][a-zA-Z0-9]*)(?:/(?:[a-zA-Z0-9._~-]+|:[a-zA-Z][a-zA-Z0-9]*))*$`)

func (s *ProjectService) CreatePage(request CreatePageRequest) (domain.SitePage, error) {
	s.mutation.Lock()
	defer s.mutation.Unlock()

	writer, ok := s.repository.(domain.ProjectFileBatchWriter)
	if !ok {
		return domain.SitePage{}, domain.ErrUnsupported
	}
	if !sitePathSegment.MatchString(request.SiteID) || !sitePathSegment.MatchString(request.ID) {
		return domain.SitePage{}, domain.ErrInvalidPath
	}
	if strings.TrimSpace(request.Name) == "" || len(request.Name) > 160 || !reactRoutePathPattern.MatchString(request.RoutePath) || strings.Contains(request.RoutePath, "//") || strings.HasSuffix(request.RoutePath, "/") {
		return domain.SitePage{}, pageValidationError("page.input", "page name and normalized React route path are required")
	}
	for _, segment := range strings.Split(strings.Trim(request.RoutePath, "/"), "/") {
		if segment == "." || segment == ".." {
			return domain.SitePage{}, pageValidationError("page.route-path", "React route path cannot contain dot segments")
		}
	}

	manifestFile, err := s.repository.Read("liapoldus/project.json")
	if err != nil {
		return domain.SitePage{}, err
	}
	manifest, diagnostics := domain.DecodeProjectDocument(manifestFile.Content, manifestFile.Path)
	if hasErrors(diagnostics) {
		return domain.SitePage{}, domain.StructuredValidationError{Diagnostics: diagnostics}
	}
	if contains(manifest.Pages, request.ID) {
		return domain.SitePage{}, pageValidationError("page.duplicate", "page ID is already declared by this project")
	}

	sitePath := path.Join("liapoldus", "sites", request.SiteID+".json")
	siteFile, err := s.repository.Read(sitePath)
	if err != nil {
		return domain.SitePage{}, err
	}
	site, diagnostics := domain.DecodeSiteDocument(siteFile.Content, sitePath)
	if hasErrors(diagnostics) || site.ID != request.SiteID || site.ProjectID != manifest.ID {
		if !hasErrors(diagnostics) {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "site.project-reference", Severity: "error", Path: sitePath, Message: "Site must belong to the active project and match its filename"})
		}
		return domain.SitePage{}, domain.StructuredValidationError{Diagnostics: diagnostics}
	}
	for _, site := range site.Pages {
		if site.ID == request.ID {
			return domain.SitePage{}, pageValidationError("page.duplicate", "page ID already exists in the selected Site")
		}
	}

	routesPath := "liapoldus/routes/development.json"
	routesFile, err := s.repository.Read(routesPath)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.SitePage{}, err
	}
	if errors.Is(err, domain.ErrNotFound) {
		routesFile = domain.File{Path: routesPath}
	}
	routes := domain.RouteDocument{SchemaVersion: 1, ID: "development-routes", Routes: []domain.Route{}}
	if len(routesFile.Content) > 0 {
		decoderDiagnostics, decodeErr := domain.ValidateRouteDocument(routesFile.Content, manifest.Pages)
		if decodeErr != nil || hasErrors(decoderDiagnostics) {
			if decodeErr != nil {
				return domain.SitePage{}, decodeErr
			}
			return domain.SitePage{}, domain.StructuredValidationError{Diagnostics: decoderDiagnostics}
		}
		if err := json.Unmarshal(routesFile.Content, &routes); err != nil {
			return domain.SitePage{}, err
		}
	}
	page := domain.SitePage{ID: request.ID, Name: strings.TrimSpace(request.Name)}
	manifest.Pages = append(manifest.Pages, page.ID)
	site.Pages = append(site.Pages, page)
	routes.Routes = append(routes.Routes, domain.Route{ID: page.ID, Path: request.RoutePath, Page: page.ID})

	manifestContent, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return domain.SitePage{}, err
	}
	siteContent, err := json.MarshalIndent(site, "", "  ")
	if err != nil {
		return domain.SitePage{}, err
	}
	routesContent, err := json.MarshalIndent(routes, "", "  ")
	if err != nil {
		return domain.SitePage{}, err
	}
	if routeDiagnostics, validationErr := domain.ValidateRouteDocument(routesContent, manifest.Pages); validationErr != nil || hasErrors(routeDiagnostics) {
		if validationErr != nil {
			return domain.SitePage{}, validationErr
		}
		return domain.SitePage{}, domain.StructuredValidationError{Diagnostics: routeDiagnostics}
	}
	if _, err := s.repository.Read("src/pages/" + page.ID + ".page.tsx"); err == nil {
		return domain.SitePage{}, pageValidationError("page.source-exists", "page source file already exists")
	} else if !errors.Is(err, domain.ErrNotFound) {
		return domain.SitePage{}, err
	}
	pageSource := []byte(fmt.Sprintf("export default function Page_%s() { return <main><h1>{%q}</h1></main> }\n", strings.ReplaceAll(page.ID, "-", "_"), page.Name))
	changes := []domain.ProjectFileChange{
		{Path: manifestFile.Path, ExpectedRevision: request.Revisions.Manifest, Content: append(manifestContent, '\n')},
		{Path: sitePath, ExpectedRevision: request.Revisions.Site, Content: append(siteContent, '\n')},
		{Path: routesPath, ExpectedRevision: request.Revisions.Routes, Content: append(routesContent, '\n')},
		{Path: "src/pages/" + page.ID + ".page.tsx", Content: pageSource},
	}
	if err := writer.ApplyBatch(changes); err != nil {
		return domain.SitePage{}, err
	}
	return page, nil
}

func pageValidationError(code, message string) error {
	return domain.StructuredValidationError{Diagnostics: []domain.Diagnostic{{Code: code, Severity: "error", Path: "liapoldus/project.json", Message: message}}}
}
