package application

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type SiteRevisions struct {
	Manifest string `json:"manifest"`
	Source   string `json:"sourceSite"`
}

type CreateSiteRequest struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	SourceSiteID string        `json:"sourceSiteId"`
	Revisions    SiteRevisions `json:"revisions"`
}

// CreateSite clones the active Site's page/locale configuration and creates
// empty localized content documents in one revision-checked filesystem batch.
func (s *ProjectService) CreateSite(request CreateSiteRequest) (domain.SiteDocument, error) {
	s.mutation.Lock()
	defer s.mutation.Unlock()

	writer, ok := s.repository.(domain.ProjectFileBatchWriter)
	if !ok {
		return domain.SiteDocument{}, domain.ErrUnsupported
	}
	if !sitePathSegment.MatchString(request.ID) || !sitePathSegment.MatchString(request.SourceSiteID) {
		return domain.SiteDocument{}, domain.ErrInvalidPath
	}
	name := strings.TrimSpace(request.Name)
	if name == "" || len(name) > 160 {
		return domain.SiteDocument{}, siteValidationError("site.input", "Site name is required and must not exceed 160 characters")
	}
	if request.Revisions.Manifest == "" || request.Revisions.Source == "" {
		return domain.SiteDocument{}, siteValidationError("site.revision-required", "current manifest and source Site revisions are required")
	}

	manifestPath := "liapoldus/project.json"
	manifestFile, err := s.repository.Read(manifestPath)
	if err != nil {
		return domain.SiteDocument{}, err
	}
	project, diagnostics := domain.DecodeProjectDocument(manifestFile.Content, manifestPath)
	if hasErrors(diagnostics) {
		return domain.SiteDocument{}, domain.StructuredValidationError{Diagnostics: diagnostics}
	}
	if manifestFile.Revision != request.Revisions.Manifest {
		return domain.SiteDocument{}, &domain.ConflictError{Path: manifestPath, ExpectedRevision: request.Revisions.Manifest, CurrentRevision: manifestFile.Revision, Current: manifestFile.Content}
	}

	sourcePath := path.Join("liapoldus", "sites", request.SourceSiteID+".json")
	sourceFile, err := s.repository.Read(sourcePath)
	if err != nil {
		return domain.SiteDocument{}, err
	}
	if sourceFile.Revision != request.Revisions.Source {
		return domain.SiteDocument{}, &domain.ConflictError{Path: sourcePath, ExpectedRevision: request.Revisions.Source, CurrentRevision: sourceFile.Revision, Current: sourceFile.Content}
	}
	source, diagnostics := domain.DecodeSiteDocument(sourceFile.Content, sourcePath)
	if hasErrors(diagnostics) || source.ID != request.SourceSiteID || source.ProjectID != project.ID {
		if !hasErrors(diagnostics) {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "site.project-reference", Severity: "error", Path: sourcePath, Message: "source Site must belong to the active project and match its filename"})
		}
		return domain.SiteDocument{}, domain.StructuredValidationError{Diagnostics: diagnostics}
	}
	if len(source.Pages) == 0 {
		return domain.SiteDocument{}, siteValidationError("site.pages-required", "source Site must contain at least one page")
	}
	for _, page := range source.Pages {
		if !contains(project.Pages, page.ID) {
			return domain.SiteDocument{}, siteValidationError("site.page-project", "source Site contains a page not declared by the project manifest")
		}
	}

	site := domain.SiteDocument{
		SchemaVersion: 1,
		ID:            request.ID,
		ProjectID:     project.ID,
		Name:          name,
		ThemeID:       source.ThemeID,
		Pages:         append([]domain.SitePage(nil), source.Pages...),
		Locales:       append([]string(nil), source.Locales...),
	}
	sitePath := path.Join("liapoldus", "sites", site.ID+".json")
	siteContent, err := json.MarshalIndent(site, "", "  ")
	if err != nil {
		return domain.SiteDocument{}, err
	}
	changes := []domain.ProjectFileChange{
		// The manifest is included as a no-op write so its revision participates
		// in the same atomic preflight as the new Site and content files.
		{Path: manifestPath, ExpectedRevision: request.Revisions.Manifest, Content: manifestFile.Content},
		{Path: sitePath, Content: append(siteContent, '\n')},
	}
	for _, locale := range site.Locales {
		contentPath := path.Join("liapoldus", "content", site.ID, locale+".json")
		document := domain.ContentDocument{SchemaVersion: 1, ID: fmt.Sprintf("%s-%s-content", site.ID, strings.ToLower(locale)), Instances: []domain.ContentInstance{}}
		content, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			return domain.SiteDocument{}, err
		}
		changes = append(changes, domain.ProjectFileChange{Path: contentPath, Content: append(content, '\n')})
	}
	if err := writer.ApplyBatch(changes); err != nil {
		return domain.SiteDocument{}, err
	}
	return site, nil
}

func siteValidationError(code, message string) error {
	return domain.StructuredValidationError{Diagnostics: []domain.Diagnostic{{Code: code, Severity: "error", Path: "liapoldus/sites", Message: message}}}
}
