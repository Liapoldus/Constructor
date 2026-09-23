package domain

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
)

type ComponentSchema struct {
	SchemaVersion int              `json:"schemaVersion"`
	ID            string           `json:"id"`
	Kind          string           `json:"kind"`
	Source        string           `json:"source"`
	Fields        []ComponentField `json:"fields"`
	ThemeTokens   []string         `json:"themeTokens,omitempty"`
}
type ComponentField struct {
	Key           string `json:"key"`
	Type          string `json:"type"`
	Label         string `json:"label"`
	Description   string `json:"description,omitempty"`
	Required      bool   `json:"required"`
	Nullable      bool   `json:"nullable,omitempty"`
	Localized     bool   `json:"localized,omitempty"`
	Default       any    `json:"default,omitempty"`
	AllowedValues []any  `json:"allowedValues,omitempty"`
}

func ValidateComponentContent(schema ComponentSchema, values map[string]any, path string) []Diagnostic {
	return validateComponentContent(schema, values, path, false)
}

// ValidateComponentContentForWrite permits unfinished localized fields to be
// saved; strict delivery validation still requires them for each enabled locale.
func ValidateComponentContentForWrite(schema ComponentSchema, values map[string]any, path string) []Diagnostic {
	return validateComponentContent(schema, values, path, true)
}

func validateComponentContent(schema ComponentSchema, values map[string]any, path string, allowIncompleteLocalized bool) []Diagnostic {
	var diagnostics []Diagnostic
	add := func(code, message, fieldKey string) {
		diagnostics = append(diagnostics, Diagnostic{Code: code, Severity: "error", Path: path, Message: message, FieldKey: fieldKey})
	}
	for _, field := range schema.Fields {
		value, exists := values[field.Key]
		if !exists && field.Default != nil {
			value, exists = field.Default, true
		}
		if !exists {
			if field.Required && !(allowIncompleteLocalized && field.Localized) {
				add("content.required", field.Key+" is required", field.Key)
			}
			continue
		}
		if value == nil {
			if !field.Nullable {
				add("content.null", field.Key+" cannot be null", field.Key)
			}
			continue
		}
		if value == "" && field.Required && !(allowIncompleteLocalized && field.Localized) {
			add("content.required", field.Key+" is required", field.Key)
			continue
		}
		if code, message := validateFieldValue(field, value); code != "" {
			add(code, message, field.Key)
		}
	}
	return diagnostics
}

func validateFieldValue(field ComponentField, value any) (string, string) {
	invalid := func(message string) (string, string) { return "content.type", message }
	switch field.Type {
	case "text", "rich-text":
		if _, ok := value.(string); !ok {
			return invalid(field.Key + " must be a string")
		}
	case "select":
		if !isScalar(value) {
			return invalid(field.Key + " must be a string, number, or boolean")
		}
		if len(field.AllowedValues) > 0 {
			for _, allowed := range field.AllowedValues {
				if reflect.DeepEqual(value, allowed) {
					return "", ""
				}
			}
			return "content.select", field.Key + " has an unsupported value"
		}
		if _, ok := value.(string); !ok {
			return invalid(field.Key + " must be a string when allowedValues is not set")
		}
	case "image", "icon", "file":
		reference, ok := value.(map[string]any)
		if !ok {
			return invalid(field.Key + " must be an asset reference")
		}
		id, validID := reference["id"].(string)
		kind, validKind := reference["kind"].(string)
		if !validID || strings.TrimSpace(id) == "" || !validKind || kind != "asset" && kind != field.Type {
			return invalid(field.Key + " must be a matching asset reference with a non-empty id")
		}
		if alt, exists := reference["alt"]; exists && field.Type == "image" {
			if _, ok := alt.(string); !ok {
				return invalid(field.Key + ".alt must be a string")
			}
		}
	case "reference":
		reference, ok := value.(map[string]any)
		if !ok {
			return invalid(field.Key + " must be an entity reference")
		}
		id, validID := reference["id"].(string)
		targetType, validType := reference["type"].(string)
		kind, validKind := reference["kind"].(string)
		if !validID || strings.TrimSpace(id) == "" || !validType || strings.TrimSpace(targetType) == "" || !validKind || kind != "reference" {
			return invalid(field.Key + " must be a valid entity reference")
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return invalid(field.Key + " must be an object")
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return invalid(field.Key + " must be an array")
		}
	default:
		return "component.field-type", fmt.Sprintf("field %q has unsupported type %q", field.Key, field.Type)
	}
	return "", ""
}

func isScalar(value any) bool {
	switch value.(type) {
	case string, float64, bool:
		return true
	default:
		return false
	}
}

func ValidateComponentSchema(raw []byte, path string) ([]Diagnostic, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var schema ComponentSchema
	if err := decoder.Decode(&schema); err != nil {
		return []Diagnostic{{Code: "component.invalid-json", Severity: "error", Path: path, Message: err.Error()}}, nil
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return []Diagnostic{{Code: "component.trailing-json", Severity: "error", Path: path, Message: "schema must contain exactly one JSON value"}}, nil
	}
	result := make([]Diagnostic, 0)
	add := func(code, message string) {
		result = append(result, Diagnostic{Code: code, Severity: "error", Path: path, Message: message})
	}
	if schema.SchemaVersion != 1 {
		add("component.schema-version", "schemaVersion must be 1")
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{1,61}$`).MatchString(schema.ID) {
		add("component.id", "id must be kebab-case")
	}
	if schema.Kind != "component" && schema.Kind != "primitive" {
		add("component.kind", "kind must be component or primitive")
	}
	if len(schema.Source) < 5 || schema.Source[:4] != "src/" {
		add("component.source", "source must point inside src/")
	}
	seen := map[string]bool{}
	themeTokens := map[string]bool{}
	for _, tokenPath := range schema.ThemeTokens {
		if !themeTokenPathPattern.MatchString(tokenPath) {
			add("component.theme-token-path", fmt.Sprintf("invalid theme token path %q", tokenPath))
		}
		if themeTokens[tokenPath] {
			add("component.theme-token-duplicate", fmt.Sprintf("duplicate theme token %q", tokenPath))
		}
		themeTokens[tokenPath] = true
	}
	for _, field := range schema.Fields {
		switch field.Type {
		case "text", "rich-text", "image", "icon", "file", "select", "object", "array", "reference":
		default:
			add("component.field-type", fmt.Sprintf("field %q has unsupported type %q", field.Key, field.Type))
		}
		if !regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`).MatchString(field.Key) {
			add("component.field-key", fmt.Sprintf("invalid field key %q", field.Key))
		}
		if seen[field.Key] {
			add("component.field-duplicate", fmt.Sprintf("duplicate field key %q", field.Key))
		}
		seen[field.Key] = true
		if field.Label == "" {
			add("component.field-label", fmt.Sprintf("field %q requires label", field.Key))
		}
		if field.Type != "select" && len(field.AllowedValues) > 0 {
			add("component.allowed-values", fmt.Sprintf("field %q only supports allowedValues for select", field.Key))
		}
		for _, allowed := range field.AllowedValues {
			if !isScalar(allowed) {
				add("component.allowed-value-type", fmt.Sprintf("field %q allowedValues must be scalar values", field.Key))
				break
			}
		}
		if field.Default != nil {
			if field.Required && field.Default == "" {
				add("component.field-default", fmt.Sprintf("field %q has an empty required default", field.Key))
			} else if code, message := validateFieldValue(field, field.Default); code != "" {
				add("component.field-default", fmt.Sprintf("field %q has invalid default: %s", field.Key, message))
			}
		}
	}
	return result, nil
}
