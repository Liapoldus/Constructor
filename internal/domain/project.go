package domain

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

var ErrNotFound = errors.New("project file not found")
var ErrConflict = errors.New("project revision conflict")
var ErrInvalidPath = errors.New("invalid project path")
var ErrForbidden = errors.New("permission denied")
var ErrUnsupported = errors.New("operation is not supported by this repository")

type ConflictError struct {
	Path             string `json:"path"`
	ExpectedRevision string `json:"expectedRevision"`
	CurrentRevision  string `json:"currentRevision"`
	Current          []byte `json:"current"`
	Candidate        []byte `json:"candidate"`
}

func (e *ConflictError) Error() string { return "project file conflict" }

type MergeResult struct {
	Path       string `json:"path"`
	Revision   string `json:"revision"`
	Content    []byte `json:"content"`
	Conflicted bool   `json:"conflicted"`
}
type MergeEngine interface {
	Merge(base, current, candidate []byte) ([]byte, bool, error)
}

type Project struct {
	SchemaVersion int      `json:"schemaVersion"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Pages         []string `json:"pages"`
	Components    []string `json:"components"`
	React         struct {
		Entry string `json:"entry"`
	} `json:"react"`
}

var projectIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var projectPageIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
var projectComponentIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{1,61}$`)

func DecodeProjectDocument(raw []byte, documentPath string) (Project, []Diagnostic) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var project Project
	if err := decoder.Decode(&project); err != nil {
		return Project{}, []Diagnostic{{Code: "project.invalid-document", Severity: "error", Path: documentPath, Message: err.Error()}}
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Project{}, []Diagnostic{{Code: "project.trailing-json", Severity: "error", Path: documentPath, Message: "document must contain exactly one JSON value"}}
	}
	diagnostics := make([]Diagnostic, 0)
	add := func(code, message string) {
		diagnostics = append(diagnostics, Diagnostic{Code: code, Severity: "error", Path: documentPath, Message: message})
	}
	if project.SchemaVersion != 1 {
		add("project.schema-version", "schemaVersion must be 1")
	}
	if !projectIDPattern.MatchString(project.ID) {
		add("project.id", "id must be a stable non-empty identifier")
	}
	if strings.TrimSpace(project.Name) == "" {
		add("project.name", "name is required")
	}
	if !strings.HasPrefix(project.React.Entry, "src/") {
		add("project.react-entry", "react.entry must point inside src/")
	}
	pageIDs := map[string]bool{}
	for _, id := range project.Pages {
		if !projectPageIDPattern.MatchString(id) {
			add("project.page-id", "page IDs must be kebab-case")
		}
		if pageIDs[id] {
			add("project.page-duplicate", "page IDs must be unique")
		}
		pageIDs[id] = true
	}
	componentIDs := map[string]bool{}
	for _, id := range project.Components {
		key := strings.ToLower(id)
		if !projectComponentIDPattern.MatchString(id) {
			add("project.component-id", "component IDs must be alphanumeric kebab-case identifiers")
		}
		if componentIDs[key] {
			add("project.component-duplicate", "component IDs must be unique, ignoring case")
		}
		componentIDs[key] = true
	}
	return project, diagnostics
}

type Diagnostic struct {
	Code       string `json:"code"`
	Severity   string `json:"severity"`
	Path       string `json:"path"`
	Message    string `json:"message"`
	PageID     string `json:"pageId,omitempty"`
	InstanceID string `json:"instanceId,omitempty"`
	FieldKey   string `json:"fieldKey,omitempty"`
}

type File struct {
	Path     string `json:"path"`
	Revision string `json:"revision"`
	Content  []byte `json:"content"`
}

type ProjectRepository interface {
	Manifest() (Project, string, error)
	Read(path string) (File, error)
	Write(path, expectedRevision string, content []byte) (File, error)
}

// ProjectRepositoryAtRoot creates the same repository adapter against an
// isolated project checkout, such as a detached snapshot worktree.
type ProjectRepositoryAtRoot interface {
	AtRoot(root string) ProjectRepository
}

type ProjectRepositoryRoot interface {
	Root() string
}

// ProjectFileChange describes one optimistic write or delete in a project batch.
type ProjectFileChange struct {
	Path             string
	ExpectedRevision string
	Content          []byte
	Delete           bool
}

// ProjectFileBatchWriter applies a set of project-file changes as one operation.
type ProjectFileBatchWriter interface {
	ApplyBatch(changes []ProjectFileChange) error
}

// ProjectFileLister exposes a bounded, project-relative directory listing to
// application use cases without leaking filesystem paths into the UI layer.
type ProjectFileLister interface {
	List(prefix string) ([]string, error)
}
