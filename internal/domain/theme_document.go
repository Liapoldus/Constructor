package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
)

type ThemeDocument struct {
	SchemaVersion int                       `json:"schemaVersion"`
	ID            string                    `json:"id"`
	Name          string                    `json:"name"`
	Tokens        map[string]ThemeToken     `json:"tokens"`
	Variants      map[string]map[string]any `json:"variants,omitempty"`
}

type ThemeToken struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

var themeIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
var themeTokenPathPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
var themeColorPattern = regexp.MustCompile(`^#(?:[0-9A-Fa-f]{3,4}|[0-9A-Fa-f]{6}|[0-9A-Fa-f]{8})$`)
var themeFamilyPattern = regexp.MustCompile(`^[A-Za-z0-9 _,'"-]{1,128}$`)
var themeLengthPattern = regexp.MustCompile(`^(?:0|(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)(?:px|rem|em|%|vh|vw|ch))$`)
var themeShadowPattern = regexp.MustCompile(`^(?:none|[-+0-9.]+(?:px|rem)?(?:\s+[-+0-9.]+(?:px|rem)?){1,3}(?:\s+#[0-9A-Fa-f]{3,8})?)$`)
var themeDurationPattern = regexp.MustCompile(`^(?:0|(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)(?:ms|s))$`)

func DecodeThemeDocument(raw []byte, documentPath string) (ThemeDocument, []Diagnostic) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document ThemeDocument
	if err := decoder.Decode(&document); err != nil {
		return ThemeDocument{}, []Diagnostic{{Code: "theme.invalid-document", Severity: "error", Path: documentPath, Message: err.Error()}}
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return ThemeDocument{}, []Diagnostic{{Code: "theme.trailing-json", Severity: "error", Path: documentPath, Message: "document must contain exactly one JSON value"}}
	}
	var diagnostics []Diagnostic
	add := func(code, message string) {
		diagnostics = append(diagnostics, Diagnostic{Code: code, Severity: "error", Path: documentPath, Message: message})
	}
	if document.SchemaVersion != 1 {
		add("theme.schema-version", "schemaVersion must be 1")
	}
	if !themeIDPattern.MatchString(document.ID) {
		add("theme.id", "theme id must be a stable kebab-case identifier")
	}
	if strings.TrimSpace(document.Name) == "" || len(document.Name) > 160 {
		add("theme.name", "theme name must contain 1 to 160 characters")
	}
	if len(document.Tokens) == 0 {
		add("theme.tokens-required", "theme must define at least one token")
	}
	for tokenPath, token := range document.Tokens {
		if !themeTokenPathPattern.MatchString(tokenPath) {
			add("theme.token-path", fmt.Sprintf("token path %q is not a stable dotted path", tokenPath))
			continue
		}
		if !validThemeTokenType(tokenPath, token.Type) {
			add("theme.token-type", fmt.Sprintf("token %q has an unsupported type for its category", tokenPath))
			continue
		}
		if !validThemeTokenValue(token.Type, token.Value) {
			add("theme.token-value", fmt.Sprintf("token %q has a value that does not match type %q", tokenPath, token.Type))
		}
	}
	for variantName, values := range document.Variants {
		if variantName != "light" && variantName != "dark" {
			add("theme.variant", "theme variants must be light or dark")
			continue
		}
		for tokenPath, value := range values {
			token, exists := document.Tokens[tokenPath]
			if !exists {
				add("theme.variant-token", fmt.Sprintf("variant %q references undeclared token %q", variantName, tokenPath))
				continue
			}
			if !validThemeTokenValue(token.Type, value) {
				add("theme.variant-value", fmt.Sprintf("variant %q value for %q does not match type %q", variantName, tokenPath, token.Type))
			}
		}
	}
	return document, diagnostics
}

func validThemeTokenType(tokenPath, tokenType string) bool {
	category, _, _ := strings.Cut(tokenPath, ".")
	switch category {
	case "colors":
		return tokenType == "color"
	case "typography":
		return tokenType == "fontFamily" || tokenType == "fontSize" || tokenType == "fontWeight" || tokenType == "lineHeight"
	case "spacing", "radii", "breakpoints":
		return tokenType == "length"
	case "shadows":
		return tokenType == "shadow"
	case "animations":
		return tokenType == "duration" || tokenType == "easing"
	default:
		return false
	}
}

func validThemeTokenValue(tokenType string, value any) bool {
	switch tokenType {
	case "color":
		text, ok := value.(string)
		return ok && (themeColorPattern.MatchString(text) || text == "transparent")
	case "fontFamily":
		text, ok := value.(string)
		return ok && themeFamilyPattern.MatchString(text)
	case "fontSize", "length":
		text, ok := value.(string)
		return ok && themeLengthPattern.MatchString(text)
	case "fontWeight":
		number, ok := value.(float64)
		return ok && number >= 100 && number <= 900 && math.Mod(number, 100) == 0
	case "lineHeight":
		number, ok := value.(float64)
		if ok {
			return number >= 0.5 && number <= 3 && !math.IsNaN(number)
		}
		text, ok := value.(string)
		return ok && themeLengthPattern.MatchString(text)
	case "shadow":
		text, ok := value.(string)
		return ok && themeShadowPattern.MatchString(text)
	case "duration":
		text, ok := value.(string)
		return ok && themeDurationPattern.MatchString(text)
	case "easing":
		text, ok := value.(string)
		switch text {
		case "linear", "ease", "ease-in", "ease-out", "ease-in-out":
			return ok
		default:
			return false
		}
	default:
		return false
	}
}
