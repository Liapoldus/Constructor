package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type SitePage struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// UnmarshalJSON keeps existing string-only Site files readable while making
// the stable ID and display name explicit in newly-authored documents.
func (page *SitePage) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var id string
		if err := json.Unmarshal(raw, &id); err != nil {
			return err
		}
		page.ID, page.Name = id, id
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("page must contain exactly one JSON value")
	}
	page.ID, page.Name = value.ID, value.Name
	return nil
}

type SiteDocument struct {
	SchemaVersion int        `json:"schemaVersion"`
	ID            string     `json:"id"`
	ProjectID     string     `json:"projectId"`
	Name          string     `json:"name"`
	ThemeID       string     `json:"themeId,omitempty"`
	Pages         []SitePage `json:"pages"`
	Locales       []string   `json:"locales"`
}

var localeIDPattern = regexp.MustCompile(`^[a-z]{2}(-[A-Z]{2})?$`)
var sitePageIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)

func DecodeSiteDocument(raw []byte, path string) (SiteDocument, []Diagnostic) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var document SiteDocument
	if err := decoder.Decode(&document); err != nil {
		return SiteDocument{}, []Diagnostic{{Code: "site.invalid-document", Severity: "error", Path: path, Message: err.Error()}}
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return SiteDocument{}, []Diagnostic{{Code: "site.trailing-json", Severity: "error", Path: path, Message: "document must contain exactly one JSON value"}}
	}
	var diagnostics []Diagnostic
	add := func(code, message string) {
		diagnostics = append(diagnostics, Diagnostic{Code: code, Severity: "error", Path: path, Message: message})
	}
	if document.SchemaVersion != 1 {
		add("site.schema-version", "schemaVersion must be 1")
	}
	if strings.TrimSpace(document.ID) == "" {
		add("site.id-required", "id is required")
	}
	if strings.TrimSpace(document.ProjectID) == "" {
		add("site.project-required", "projectId is required")
	}
	if strings.TrimSpace(document.Name) == "" {
		add("site.name-required", "name is required")
	}
	if document.ThemeID != "" && !themeIDPattern.MatchString(document.ThemeID) {
		add("site.theme-id", "themeId must be a stable kebab-case identifier")
	}
	pageIDs := map[string]bool{}
	for _, page := range document.Pages {
		if !sitePageIDPattern.MatchString(page.ID) {
			add("site.page-id", "page IDs must be stable kebab-case identifiers")
		}
		if strings.TrimSpace(page.Name) == "" {
			add("site.page-name", "page display names cannot be empty")
		}
		if pageIDs[page.ID] {
			add("site.page-duplicate", "page IDs must be unique")
		}
		pageIDs[page.ID] = true
	}
	locales := map[string]bool{}
	if len(document.Locales) == 0 {
		add("site.locales-required", "at least one enabled locale is required")
	}
	for _, locale := range document.Locales {
		if !localeIDPattern.MatchString(locale) {
			add("site.locale-id", "locale IDs must use language or language-region form")
		}
		if locales[locale] {
			add("site.locale-duplicate", "enabled locales must be unique")
		}
		locales[locale] = true
	}
	return document, diagnostics
}
