package application

import (
	"encoding/json"
	"fmt"
	"github.com/Liapoldus/Constructor/internal/domain"
	"regexp"
	"strings"
)

type GeneratedArtifact struct {
	Path     string `json:"path"`
	Revision string `json:"revision"`
}

var contentPathSegment = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
var localePathSegment = regexp.MustCompile(`^(default|[a-z]{2}(-[A-Z]{2})?)$`)

type generatedLocale struct {
	Pages map[string]generatedPage `json:"pages"`
}

type generatedPage struct {
	Instances map[string]generatedInstance `json:"instances"`
}

type generatedInstance struct {
	Component string         `json:"component"`
	Fields    map[string]any `json:"fields"`
}

func (s *ProjectService) GenerateLocale(siteID, locale string) (GeneratedArtifact, error) {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	generated, err := s.renderLocale(siteID, locale)
	if err != nil {
		return GeneratedArtifact{}, err
	}
	target := fmt.Sprintf("src/generated/content/%s.json", locale)
	current, readErr := s.repository.Read(target)
	expected := ""
	if readErr == nil {
		expected = current.Revision
	} else if readErr != domain.ErrNotFound {
		return GeneratedArtifact{}, readErr
	}
	file, err := s.repository.Write(target, expected, generated)
	if err != nil {
		return GeneratedArtifact{}, err
	}
	return GeneratedArtifact{Path: file.Path, Revision: file.Revision}, nil
}

func (s *ProjectService) renderLocale(siteID, locale string) ([]byte, error) {
	if !contentPathSegment.MatchString(siteID) || !localePathSegment.MatchString(locale) {
		return nil, domain.ErrInvalidPath
	}
	project, _, err := s.repository.Manifest()
	if err != nil {
		return nil, err
	}
	contentPath := fmt.Sprintf("liapoldus/content/%s/%s.json", siteID, locale)
	source, sourcePath, siteDiagnostics, err := s.resolvedContentForLocale(siteID, locale)
	if err != nil {
		return nil, err
	}
	if hasErrors(siteDiagnostics) {
		return nil, domain.StructuredValidationError{Diagnostics: siteDiagnostics}
	}
	if sourcePath == "" {
		return nil, domain.ContentValidationError{Diagnostics: []domain.Diagnostic{{Code: "content.document-required", Severity: "error", Path: contentPath, Message: "site content document is required"}}}
	}
	resolvedBytes, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	contentDiagnostics, err := s.validateContentDocumentWithLocalePolicy(contentPath, resolvedBytes, true)
	if err != nil {
		return nil, err
	}
	if hasErrors(contentDiagnostics) {
		return nil, domain.ContentValidationError{Diagnostics: contentDiagnostics}
	}
	schemas, schemaDiagnostics, err := s.loadComponentSchemas(project)
	if err != nil {
		return nil, err
	}
	if hasErrors(schemaDiagnostics) {
		return nil, domain.StructuredValidationError{Diagnostics: schemaDiagnostics}
	}
	content := source
	document := generatedLocale{Pages: map[string]generatedPage{}}
	for _, instance := range content.Instances {
		fields := make(map[string]any, len(instance.Fields))
		for key, value := range instance.Fields {
			fields[key] = value
		}
		schema := schemas[strings.ToLower(instance.Component)]
		for _, field := range schema.Fields {
			if field.Type != "rich-text" {
				continue
			}
			value, exists := fields[field.Key]
			if !exists {
				continue
			}
			richText, ok := value.(string)
			if !ok {
				continue
			}
			sanitized, sanitizeErr := domain.SanitizeRichText(richText)
			if sanitizeErr != nil {
				return nil, domain.ContentValidationError{Diagnostics: []domain.Diagnostic{{Code: "content.rich-text-sanitize", Severity: "error", Path: contentPath, Message: "rich-text field could not be safely sanitized", PageID: instance.PageID, InstanceID: instance.ID, FieldKey: field.Key}}}
			}
			fields[field.Key] = sanitized
		}
		page := document.Pages[instance.PageID]
		if page.Instances == nil {
			page.Instances = map[string]generatedInstance{}
		}
		page.Instances[instance.ID] = generatedInstance{Component: instance.Component, Fields: fields}
		document.Pages[instance.PageID] = page
	}
	generated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	generated = append(generated, '\n')
	return generated, nil
}
