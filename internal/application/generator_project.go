package application

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type projectArtifact struct {
	path    string
	content []byte
}

type generatedAssetEntry struct {
	Type     string                  `json:"type"`
	Source   string                  `json:"src"`
	MIMEType string                  `json:"mimeType"`
	Size     int64                   `json:"size"`
	SHA256   string                  `json:"sha256"`
	Width    int                     `json:"width,omitempty"`
	Height   int                     `json:"height,omitempty"`
	Variants []generatedAssetVariant `json:"variants,omitempty"`
}

type generatedAssetVariant struct {
	Source   string `json:"src"`
	MIMEType string `json:"mimeType"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// GenerateProjectArtifacts validates the selected Site and all its enabled
// locales, renders deterministic runtime files, then publishes the complete
// artifact set through one optimistic filesystem batch.
func (s *ProjectService) GenerateProjectArtifacts(siteID, environment string) ([]GeneratedArtifact, error) {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	return s.generateProjectArtifacts(siteID, environment)
}

// generateProjectArtifacts assumes the caller owns s.mutation for the complete
// generation-to-snapshot transaction.
func (s *ProjectService) generateProjectArtifacts(siteID, environment string) ([]GeneratedArtifact, error) {
	if !contentPathSegment.MatchString(siteID) || !environmentID.MatchString(environment) {
		return nil, domain.ErrInvalidPath
	}

	sites, err := s.Sites()
	if err != nil {
		return nil, err
	}
	var selected *domain.SiteDocument
	for index := range sites {
		if sites[index].ID == siteID {
			selected = &sites[index]
			break
		}
	}
	if selected == nil {
		return nil, domain.ErrNotFound
	}
	if len(selected.Locales) == 0 {
		return nil, domain.StructuredValidationError{Diagnostics: []domain.Diagnostic{{Code: "site.locales-required", Severity: "error", Path: path.Join("liapoldus", "sites", siteID+".json"), Message: "Site must enable at least one locale before generation"}}}
	}

	locales := append([]string(nil), selected.Locales...)
	sort.Strings(locales)
	artifacts := make([]projectArtifact, 0, len(locales)+4)
	for _, locale := range locales {
		content, err := s.renderLocale(siteID, locale)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, projectArtifact{path: path.Join("src", "generated", "content", locale+".json"), content: content})
	}
	routes, diagnostics, err := NewRouteService(s).Render(environment)
	if err != nil {
		if len(diagnostics) > 0 {
			return nil, domain.StructuredValidationError{Diagnostics: diagnostics}
		}
		return nil, err
	}
	artifacts = append(artifacts, projectArtifact{path: "src/generated/routes.tsx", content: routes})

	assets, err := s.renderAssetManifest()
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, projectArtifact{path: "src/generated/assets.ts", content: assets})
	themeCSS, err := s.renderThemeForSite(*selected)
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, projectArtifact{path: "src/generated/theme.css", content: themeCSS})
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].path < artifacts[j].path })

	writer, ok := s.repository.(domain.ProjectFileBatchWriter)
	if !ok {
		return nil, domain.ErrUnsupported
	}
	changes := make([]domain.ProjectFileChange, 0, len(artifacts))
	for _, artifact := range artifacts {
		current, readErr := s.repository.Read(artifact.path)
		expected := ""
		if readErr == nil {
			expected = current.Revision
		} else if readErr != domain.ErrNotFound {
			return nil, readErr
		}
		changes = append(changes, domain.ProjectFileChange{Path: artifact.path, ExpectedRevision: expected, Content: artifact.content})
	}
	if err := writer.ApplyBatch(changes); err != nil {
		return nil, err
	}
	result := make([]GeneratedArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		file, err := s.repository.Read(artifact.path)
		if err != nil {
			return nil, fmt.Errorf("read generated artifact %s: %w", artifact.path, err)
		}
		result = append(result, GeneratedArtifact{Path: file.Path, Revision: file.Revision})
	}
	return result, nil
}

func (s *ProjectService) renderAssetManifest() ([]byte, error) {
	file, err := s.repository.Read(assetCatalogPath)
	var document domain.AssetDocument
	if err == domain.ErrNotFound {
		document = domain.AssetDocument{SchemaVersion: 1, ID: "assets", Items: []domain.AssetItem{}}
	} else if err != nil {
		return nil, err
	} else {
		var diagnostics []domain.Diagnostic
		document, diagnostics = domain.DecodeAssetDocument(file.Content, assetCatalogPath)
		if hasErrors(diagnostics) {
			return nil, domain.StructuredValidationError{Diagnostics: diagnostics}
		}
		fileDiagnostics, err := s.verifyAssetItems(document.Items, assetCatalogPath)
		if err != nil {
			return nil, err
		}
		if hasErrors(fileDiagnostics) {
			return nil, domain.StructuredValidationError{Diagnostics: fileDiagnostics}
		}
	}
	entries := make(map[string]generatedAssetEntry, len(document.Items))
	for _, item := range document.Items {
		entry := generatedAssetEntry{
			Type: item.Type, Source: "/" + strings.TrimPrefix(item.Path, "public/"),
			MIMEType: item.MIMEType, Size: item.Size, SHA256: item.SHA256, Width: item.Width, Height: item.Height,
		}
		for _, variant := range item.Variants {
			entry.Variants = append(entry.Variants, generatedAssetVariant{
				Source: "/" + strings.TrimPrefix(variant.Path, "public/"), MIMEType: variant.MIMEType,
				Width: variant.Width, Height: variant.Height,
			})
		}
		entries[item.ID] = entry
	}
	encoded, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return nil, err
	}
	result := append([]byte("export const assets = "), encoded...)
	result = append(result, []byte(" as const\nexport type AssetId = keyof typeof assets\n")...)
	return result, nil
}
