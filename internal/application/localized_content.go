package application

import (
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type SaveLocalizedContentResult struct {
	ContentRevision        string `json:"contentRevision"`
	DefaultContentRevision string `json:"defaultContentRevision"`
}

type LocalizedContentView struct {
	Document               domain.ContentDocument `json:"document"`
	ContentRevision        string                 `json:"contentRevision,omitempty"`
	DefaultContentRevision string                 `json:"defaultContentRevision,omitempty"`
	FallbackFrom           string                 `json:"fallbackFrom,omitempty"`
}

func (s *ProjectService) LoadLocalizedContent(siteID, locale string) (LocalizedContentView, error) {
	document, sourcePath, diagnostics, err := s.resolvedContentForLocale(siteID, locale)
	if err != nil {
		return LocalizedContentView{}, err
	}
	if hasErrors(diagnostics) {
		return LocalizedContentView{}, domain.StructuredValidationError{Diagnostics: diagnostics}
	}
	if document.ID == "" {
		document = domain.ContentDocument{SchemaVersion: 1, ID: siteID + "-" + locale + "-content", Instances: []domain.ContentInstance{}}
	}
	view := LocalizedContentView{Document: document}
	if sourcePath != "" {
		view.FallbackFrom = strings.TrimSuffix(path.Base(sourcePath), ".json")
	}
	for _, item := range []struct {
		locale string
		target *string
	}{{locale, &view.ContentRevision}, {"default", &view.DefaultContentRevision}} {
		file, readErr := s.repository.Read(path.Join("liapoldus", "content", siteID, item.locale+".json"))
		if readErr == nil {
			*item.target = file.Revision
		} else if readErr != domain.ErrNotFound {
			return LocalizedContentView{}, readErr
		}
	}
	return view, nil
}

// SaveLocalizedContent splits one resolved editor document back into shared
// and locale-specific source documents. Structural changes are propagated to
// every existing locale document in the same optimistic batch.
func (s *ProjectService) SaveLocalizedContent(siteID, locale string, base, candidate domain.ContentDocument, expectedLocaleRevision, expectedDefaultRevision string) (SaveLocalizedContentResult, error) {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	if !sitePathSegment.MatchString(siteID) || !localePathSegment.MatchString(locale) {
		return SaveLocalizedContentResult{}, domain.ErrInvalidPath
	}
	writer, ok := s.repository.(domain.ProjectFileBatchWriter)
	if !ok {
		return SaveLocalizedContentResult{}, domain.ErrUnsupported
	}
	contentDir := path.Join("liapoldus", "content", siteID)
	lister, ok := s.repository.(domain.ProjectFileLister)
	if !ok {
		return SaveLocalizedContentResult{}, domain.ErrUnsupported
	}
	localePath := path.Join(contentDir, locale+".json")
	defaultPath := path.Join(contentDir, "default.json")

	project, _, err := s.repository.Manifest()
	if err != nil {
		return SaveLocalizedContentResult{}, err
	}
	schemas, schemaDiagnostics, err := s.loadComponentSchemas(project)
	if err != nil {
		return SaveLocalizedContentResult{}, err
	}
	if hasErrors(schemaDiagnostics) {
		return SaveLocalizedContentResult{}, domain.StructuredValidationError{Diagnostics: schemaDiagnostics}
	}
	if locale != "default" {
		siteFile, readErr := s.repository.Read(path.Join("liapoldus", "sites", siteID+".json"))
		if readErr != nil {
			return SaveLocalizedContentResult{}, readErr
		}
		site, diagnostics := domain.DecodeSiteDocument(siteFile.Content, path.Join("liapoldus", "sites", siteID+".json"))
		if hasErrors(diagnostics) || site.ID != siteID || site.ProjectID != project.ID || !contains(site.Locales, locale) {
			if !hasErrors(diagnostics) {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "content.locale-disabled", Severity: "error", Path: localePath, Message: "locale must be enabled for this Site"})
			}
			return SaveLocalizedContentResult{}, domain.StructuredValidationError{Diagnostics: diagnostics}
		}
	}
	if diagnostics, validationErr := s.validateContentDocumentWithPolicy(localePath, mustJSON(candidate), true, true); validationErr != nil {
		return SaveLocalizedContentResult{}, validationErr
	} else if hasErrors(diagnostics) {
		return SaveLocalizedContentResult{}, domain.ContentValidationError{Diagnostics: diagnostics}
	}
	currentResolved, _, diagnostics, err := s.resolvedContentForLocale(siteID, locale)
	if err != nil {
		return SaveLocalizedContentResult{}, err
	}
	if hasErrors(diagnostics) {
		return SaveLocalizedContentResult{}, domain.StructuredValidationError{Diagnostics: diagnostics}
	}
	if currentResolved.ID != "" && contentStructure(base) != contentStructure(currentResolved) {
		current, _ := s.repository.Read(localePath)
		return SaveLocalizedContentResult{}, &domain.ConflictError{Path: localePath, ExpectedRevision: expectedLocaleRevision, CurrentRevision: current.Revision, Current: current.Content, Candidate: mustJSON(candidate)}
	}

	paths, err := lister.List(contentDir)
	if err != nil {
		return SaveLocalizedContentResult{}, err
	}
	type storedDocument struct {
		path   string
		locale string
		file   domain.File
		doc    domain.ContentDocument
	}
	stored := map[string]storedDocument{}
	for _, filePath := range paths {
		if !strings.HasPrefix(filePath, contentDir+"/") || !strings.HasSuffix(filePath, ".json") {
			continue
		}
		file, readErr := s.repository.Read(filePath)
		if readErr != nil {
			return SaveLocalizedContentResult{}, readErr
		}
		name := strings.TrimSuffix(path.Base(filePath), ".json")
		if !localePathSegment.MatchString(name) {
			continue
		}
		document, decodeDiagnostics := domain.DecodeContentDocument(file.Content, filePath)
		if hasErrors(decodeDiagnostics) {
			return SaveLocalizedContentResult{}, domain.ContentValidationError{Diagnostics: decodeDiagnostics}
		}
		stored[name] = storedDocument{path: filePath, locale: name, file: file, doc: document}
	}
	for _, item := range []struct{ path, expected string }{{localePath, expectedLocaleRevision}, {defaultPath, expectedDefaultRevision}} {
		file, readErr := s.repository.Read(item.path)
		actual := ""
		if readErr == nil {
			actual = file.Revision
		} else if readErr != domain.ErrNotFound {
			return SaveLocalizedContentResult{}, readErr
		}
		if actual != item.expected {
			return SaveLocalizedContentResult{}, &domain.ConflictError{Path: item.path, ExpectedRevision: item.expected, CurrentRevision: actual, Current: file.Content, Candidate: mustJSON(candidate)}
		}
	}

	if _, exists := stored[locale]; !exists {
		stored[locale] = storedDocument{path: localePath, locale: locale, doc: domain.ContentDocument{SchemaVersion: 1, ID: candidate.ID, Instances: []domain.ContentInstance{}}}
	}
	if _, exists := stored["default"]; !exists {
		stored["default"] = storedDocument{path: defaultPath, locale: "default", doc: domain.ContentDocument{SchemaVersion: 1, ID: siteID + "-default-content", Instances: []domain.ContentInstance{}}}
	}
	baseByID := contentInstancesByID(base)
	changes := make([]domain.ProjectFileChange, 0, len(stored))
	storedLocales := make([]string, 0, len(stored))
	for name := range stored {
		storedLocales = append(storedLocales, name)
	}
	sort.Strings(storedLocales)
	for _, storedLocale := range storedLocales {
		item := stored[storedLocale]
		updated := domain.ContentDocument{SchemaVersion: 1, ID: item.doc.ID, Instances: make([]domain.ContentInstance, 0, len(candidate.Instances))}
		if updated.ID == "" {
			updated.ID = siteID + "-" + item.locale + "-content"
		}
		storedByID := contentInstancesByID(item.doc)
		for _, next := range candidate.Instances {
			schema, found := schemas[strings.ToLower(next.Component)]
			if !found {
				continue
			}
			instance := domain.ContentInstance{ID: next.ID, PageID: next.PageID, Component: next.Component, Fields: map[string]any{}}
			previousStored := storedByID[next.ID]
			previousBase, existedInBase := baseByID[next.ID]
			for _, field := range schema.Fields {
				if value, exists := previousStored.Fields[field.Key]; exists && (field.Localized || item.locale == "default") {
					instance.Fields[field.Key] = value
				}
				candidateValue, candidateHas := next.Fields[field.Key]
				baseValue, baseHas := previousBase.Fields[field.Key]
				changed := !existedInBase || candidateHas != baseHas || candidateHas && !reflect.DeepEqual(candidateValue, baseValue)
				if !changed || item.locale != locale && (field.Localized || item.locale != "default") {
					continue
				}
				if item.locale == "default" && locale == "default" || field.Localized && item.locale == locale || !field.Localized && item.locale == "default" {
					if candidateHas {
						instance.Fields[field.Key] = candidateValue
					} else {
						delete(instance.Fields, field.Key)
					}
				}
			}
			updated.Instances = append(updated.Instances, instance)
		}
		encoded := mustJSON(updated)
		if item.file.Revision != "" && reflect.DeepEqual(item.doc, updated) && item.path != localePath && item.path != defaultPath {
			continue
		}
		expected := item.file.Revision
		changes = append(changes, domain.ProjectFileChange{Path: item.path, ExpectedRevision: expected, Content: encoded})
	}
	if err := writer.ApplyBatch(changes); err != nil {
		return SaveLocalizedContentResult{}, err
	}
	localeFile, err := s.repository.Read(localePath)
	if err != nil {
		return SaveLocalizedContentResult{}, err
	}
	defaultFile, err := s.repository.Read(defaultPath)
	if err != nil {
		return SaveLocalizedContentResult{}, err
	}
	return SaveLocalizedContentResult{ContentRevision: localeFile.Revision, DefaultContentRevision: defaultFile.Revision}, nil
}

func mustJSON(value any) []byte {
	encoded, _ := json.MarshalIndent(value, "", "  ")
	return append(encoded, '\n')
}

func contentInstancesByID(document domain.ContentDocument) map[string]domain.ContentInstance {
	instances := make(map[string]domain.ContentInstance, len(document.Instances))
	for _, instance := range document.Instances {
		instances[instance.ID] = instance
	}
	return instances
}

// resolvedContentForLocale composes the editor/build view of a ContentDocument.
// Instance structure is shared by locale documents. Localized fields resolve
// locale -> language -> default; shared fields are read only from default.
func (s *ProjectService) resolvedContentForLocale(siteID, locale string) (domain.ContentDocument, string, []domain.Diagnostic, error) {
	sitePath := path.Join("liapoldus", "sites", siteID+".json")
	siteFile, err := s.repository.Read(sitePath)
	if err != nil {
		if err == domain.ErrNotFound {
			return domain.ContentDocument{}, "", []domain.Diagnostic{{Code: "content.site-required", Severity: "error", Path: sitePath, Message: "content references a site without a site document"}}, nil
		}
		return domain.ContentDocument{}, "", nil, err
	}
	site, siteDiagnostics := domain.DecodeSiteDocument(siteFile.Content, sitePath)
	if site.ID != siteID {
		siteDiagnostics = append(siteDiagnostics, domain.Diagnostic{Code: "site.id-path", Severity: "error", Path: sitePath, Message: "site id must match its filename"})
	}
	if locale != "default" && !contains(site.Locales, locale) {
		siteDiagnostics = append(siteDiagnostics, domain.Diagnostic{Code: "content.locale-disabled", Severity: "error", Path: path.Join("liapoldus", "content", siteID, locale+".json"), Message: "locale is not enabled for this site"})
	}
	if hasErrors(siteDiagnostics) {
		return domain.ContentDocument{}, "", siteDiagnostics, nil
	}
	project, _, err := s.repository.Manifest()
	if err != nil {
		return domain.ContentDocument{}, "", nil, err
	}
	if site.ProjectID != project.ID {
		siteDiagnostics = append(siteDiagnostics, domain.Diagnostic{Code: "site.project-reference", Severity: "error", Path: sitePath, Message: "site belongs to a different project"})
		return domain.ContentDocument{}, "", siteDiagnostics, nil
	}
	schemas, schemaDiagnostics, err := s.loadComponentSchemas(project)
	if err != nil {
		return domain.ContentDocument{}, "", nil, err
	}
	if hasErrors(schemaDiagnostics) {
		return domain.ContentDocument{}, "", schemaDiagnostics, nil
	}

	candidates := []string{locale}
	if separator := strings.IndexByte(locale, '-'); separator > 0 {
		candidates = append(candidates, locale[:separator])
	}
	candidates = append(candidates, "default")
	type sourceDocument struct {
		locale string
		path   string
		doc    domain.ContentDocument
	}
	sources := make([]sourceDocument, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		filePath := path.Join("liapoldus", "content", siteID, candidate+".json")
		file, readErr := s.repository.Read(filePath)
		if readErr == domain.ErrNotFound {
			continue
		}
		if readErr != nil {
			return domain.ContentDocument{}, "", nil, readErr
		}
		document, diagnostics := domain.DecodeContentDocument(file.Content, filePath)
		if hasErrors(diagnostics) {
			return domain.ContentDocument{}, filePath, diagnostics, nil
		}
		sources = append(sources, sourceDocument{locale: candidate, path: filePath, doc: document})
	}
	if len(sources) == 0 {
		return domain.ContentDocument{}, "", siteDiagnostics, nil
	}
	base := sources[0]
	structure := contentStructure(base.doc)
	for _, source := range sources[1:] {
		if contentStructure(source.doc) != structure {
			return domain.ContentDocument{}, source.path, []domain.Diagnostic{{Code: "content.locale-structure", Severity: "error", Path: source.path, Message: fmt.Sprintf("locale document %q must have the same instance/page/component structure as %q", source.locale, base.locale)}}, nil
		}
	}

	resolved := domain.ContentDocument{SchemaVersion: base.doc.SchemaVersion, ID: base.doc.ID, Instances: append([]domain.ContentInstance(nil), base.doc.Instances...)}
	for instanceIndex := range resolved.Instances {
		instance := &resolved.Instances[instanceIndex]
		schema, ok := schemas[strings.ToLower(instance.Component)]
		if !ok {
			continue // The normal content validator reports the missing schema.
		}
		instance.Fields = map[string]any{}
		declaredFields := make(map[string]bool, len(schema.Fields))
		for _, field := range schema.Fields {
			declaredFields[field.Key] = true
		}
		for sourceIndex := len(sources) - 1; sourceIndex >= 0; sourceIndex-- {
			source := sources[sourceIndex]
			for _, candidateInstance := range source.doc.Instances {
				if candidateInstance.ID != instance.ID {
					continue
				}
				for key, value := range candidateInstance.Fields {
					if !declaredFields[key] {
						instance.Fields[key] = value
					}
				}
				break
			}
		}
		for _, field := range schema.Fields {
			fieldSources := sources
			if !field.Localized {
				fieldSources = sources[len(sources)-1:]
				if fieldSources[0].locale != "default" {
					continue
				}
			}
			resolvedField := false
			for _, source := range fieldSources {
				for _, candidateInstance := range source.doc.Instances {
					if candidateInstance.ID != instance.ID {
						continue
					}
					if value, exists := candidateInstance.Fields[field.Key]; exists {
						instance.Fields[field.Key] = value
						resolvedField = true
					}
					break
				}
				if resolvedField {
					break
				}
			}
		}
	}

	return resolved, base.path, siteDiagnostics, nil
}

func contentStructure(document domain.ContentDocument) string {
	items := make([]string, 0, len(document.Instances))
	for _, instance := range document.Instances {
		items = append(items, instance.ID+"\x00"+instance.PageID+"\x00"+instance.Component)
	}
	sort.Strings(items)
	encoded, _ := json.Marshal(items)
	return string(encoded)
}
