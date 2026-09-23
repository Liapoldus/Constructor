package domain

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type ContentDocument struct {
	SchemaVersion int               `json:"schemaVersion"`
	ID            string            `json:"id"`
	Instances     []ContentInstance `json:"instances"`
}

type ContentInstance struct {
	ID        string         `json:"id"`
	PageID    string         `json:"pageId"`
	Component string         `json:"component"`
	Fields    map[string]any `json:"fields"`
}

type StructuredValidationError struct{ Diagnostics []Diagnostic }

func (e StructuredValidationError) Error() string { return "structured document validation failed" }

type ContentValidationError = StructuredValidationError

func DecodeContentDocument(raw []byte, path string) (ContentDocument, []Diagnostic) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var document ContentDocument
	if err := decoder.Decode(&document); err != nil {
		return ContentDocument{}, []Diagnostic{{Code: "content.invalid-document", Severity: "error", Path: path, Message: err.Error()}}
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return ContentDocument{}, []Diagnostic{{Code: "content.trailing-json", Severity: "error", Path: path, Message: "document must contain exactly one JSON value"}}
	}
	var diagnostics []Diagnostic
	add := func(code, message string) {
		diagnostics = append(diagnostics, Diagnostic{Code: code, Severity: "error", Path: path, Message: message})
	}
	if document.SchemaVersion != 1 {
		add("content.schema-version", "schemaVersion must be 1")
	}
	if strings.TrimSpace(document.ID) == "" {
		add("content.id-required", "id is required")
	}
	instanceIDs := map[string]bool{}
	for _, instance := range document.Instances {
		if strings.TrimSpace(instance.ID) == "" {
			add("content.instance-id", "each instance requires an id")
		}
		if instanceIDs[instance.ID] {
			add("content.instance-duplicate", fmt.Sprintf("duplicate instance id %q", instance.ID))
		}
		instanceIDs[instance.ID] = true
		if strings.TrimSpace(instance.Component) == "" {
			add("content.component-required", fmt.Sprintf("instance %q requires a component", instance.ID))
		}
		if strings.TrimSpace(instance.PageID) == "" {
			add("content.page-required", fmt.Sprintf("instance %q requires a pageId", instance.ID))
		}
		if instance.Fields == nil {
			add("content.fields-required", fmt.Sprintf("instance %q requires fields", instance.ID))
		}
	}
	return document, diagnostics
}

func ValidateInstanceContent(schema ComponentSchema, instance ContentInstance, path string) []Diagnostic {
	return validateInstanceContent(schema, instance, path, false)
}

// ValidateInstanceContentForWrite permits unfinished localized values in drafts.
func ValidateInstanceContentForWrite(schema ComponentSchema, instance ContentInstance, path string) []Diagnostic {
	return validateInstanceContent(schema, instance, path, true)
}

func validateInstanceContent(schema ComponentSchema, instance ContentInstance, path string, allowIncompleteLocalized bool) []Diagnostic {
	var diagnostics []Diagnostic
	if allowIncompleteLocalized {
		diagnostics = ValidateComponentContentForWrite(schema, instance.Fields, path)
	} else {
		diagnostics = ValidateComponentContent(schema, instance.Fields, path)
	}
	for index := range diagnostics {
		diagnostics[index].PageID = instance.PageID
		diagnostics[index].InstanceID = instance.ID
	}
	declared := make(map[string]bool, len(schema.Fields))
	for _, field := range schema.Fields {
		declared[field.Key] = true
	}
	for key := range instance.Fields {
		if !declared[key] {
			diagnostics = append(diagnostics, Diagnostic{Code: "content.unknown-field", Severity: "error", Path: path, Message: fmt.Sprintf("instance %q contains undeclared field %q", instance.ID, key), PageID: instance.PageID, InstanceID: instance.ID, FieldKey: key})
		}
	}
	return diagnostics
}
