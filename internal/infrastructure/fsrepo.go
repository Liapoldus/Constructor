package infrastructure

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type FilesystemRepository struct {
	root      string
	workspace *ProjectWorkspace
	mu        sync.RWMutex
}

func NewFilesystemRepository(root string) *FilesystemRepository {
	return &FilesystemRepository{root: root}
}
func (r *FilesystemRepository) AtRoot(root string) domain.ProjectRepository {
	return NewFilesystemRepository(root)
}
func (r *FilesystemRepository) Root() string { return r.activeRoot() }
func NewFilesystemRepositoryWithWorkspace(workspace *ProjectWorkspace) *FilesystemRepository {
	return &FilesystemRepository{workspace: workspace}
}
func (r *FilesystemRepository) activeRoot() string {
	if r.workspace != nil {
		return r.workspace.Root()
	}
	return r.root
}
func (r *FilesystemRepository) Manifest() (domain.Project, string, error) {
	file, err := r.Read("liapoldus/project.json")
	if err != nil {
		return domain.Project{}, "", err
	}
	project, diagnostics := domain.DecodeProjectDocument(file.Content, file.Path)
	if len(diagnostics) > 0 {
		return domain.Project{}, "", domain.StructuredValidationError{Diagnostics: diagnostics}
	}
	return project, file.Revision, nil
}
func (r *FilesystemRepository) Read(path string) (domain.File, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.readFrom(r.activeRoot(), path)
}

func (r *FilesystemRepository) read(path string) (domain.File, error) {
	return r.readFrom(r.activeRoot(), path)
}

func (r *FilesystemRepository) readFrom(root, path string) (domain.File, error) {
	clean, _, err := safeProjectPath(root, path)
	if err != nil {
		return domain.File{}, err
	}
	content, err := readProjectFile(root, clean)
	if err != nil {
		return domain.File{}, err
	}
	return domain.File{Path: clean, Revision: revision(content), Content: content}, nil
}

func safeProjectPath(root, path string) (string, string, error) {
	if path == "" || filepath.IsAbs(path) {
		return "", "", domain.ErrInvalidPath
	}
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		lower := strings.ToLower(segment)
		if segment == ".." || lower == ".git" || lower == "node_modules" || lower == ".constructor-state" || lower == ".env" || strings.HasPrefix(lower, ".env.") {
			return "", "", domain.ErrInvalidPath
		}
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." {
		return "", "", domain.ErrInvalidPath
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	return clean, filepath.Join(root, clean), nil
}

func (r *FilesystemRepository) List(prefix string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	clean := filepath.Clean(prefix)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, domain.ErrInvalidPath
	}
	if _, _, err := safeProjectPath(r.activeRoot(), clean+string(filepath.Separator)+"entry"); err != nil {
		return nil, err
	}
	return listProjectFiles(r.activeRoot(), filepath.ToSlash(clean))
}
func (r *FilesystemRepository) Write(path, expectedRevision string, content []byte) (domain.File, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	root := r.activeRoot()
	current, err := r.readFrom(root, path)
	if err != nil && err != domain.ErrNotFound {
		return domain.File{}, err
	}
	if err == nil && expectedRevision != current.Revision {
		return domain.File{}, &domain.ConflictError{Path: current.Path, ExpectedRevision: expectedRevision, CurrentRevision: current.Revision, Current: current.Content, Candidate: content}
	}
	clean, _, err := safeProjectPath(root, path)
	if err != nil {
		return domain.File{}, err
	}
	if err := writeProjectFile(root, clean, content); err != nil {
		return domain.File{}, err
	}
	return domain.File{Path: clean, Revision: revision(content), Content: content}, nil
}

type stagedFileChange struct {
	change    domain.ProjectFileChange
	parent    *os.File
	name      string
	stage     string
	backup    string
	existed   bool
	backedUp  bool
	published bool
}

func (r *FilesystemRepository) ApplyBatch(changes []domain.ProjectFileChange) error {
	if len(changes) == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	root := r.activeRoot()

	staged := make([]stagedFileChange, 0, len(changes))
	cleanup := func() {
		for _, item := range staged {
			if item.stage != "" {
				_ = unix.Unlinkat(int(item.parent.Fd()), item.stage, 0)
			}
			if item.backup != "" {
				_ = unix.Unlinkat(int(item.parent.Fd()), item.backup, 0)
			}
			if item.parent != nil {
				_ = item.parent.Close()
			}
		}
	}
	seen := make(map[string]bool, len(changes))
	for _, change := range changes {
		clean, err := cleanBatchPath(change.Path)
		if err != nil || seen[clean] {
			cleanup()
			return domain.ErrInvalidPath
		}
		seen[clean] = true
		if _, _, err := safeProjectPath(root, clean); err != nil {
			cleanup()
			return err
		}
		parent, name, parentErr := openProjectParent(root, clean, true)
		if parentErr != nil {
			cleanup()
			return parentErr
		}
		currentContent, readErr := readProjectAt(int(parent.Fd()), name)
		exists := readErr == nil
		if readErr != nil && readErr != domain.ErrNotFound {
			_ = parent.Close()
			cleanup()
			return readErr
		}
		currentRevision := ""
		if exists {
			currentRevision = revision(currentContent)
		}
		if change.Delete && !exists {
			_ = parent.Close()
			cleanup()
			return domain.ErrNotFound
		}
		if exists && currentRevision != change.ExpectedRevision || !exists && change.ExpectedRevision != "" {
			_ = parent.Close()
			cleanup()
			return &domain.ConflictError{Path: clean, ExpectedRevision: change.ExpectedRevision, CurrentRevision: currentRevision, Current: currentContent, Candidate: change.Content}
		}
		item := stagedFileChange{change: change, parent: parent, name: name, existed: exists}
		if !change.Delete {
			stage, err := writeProjectAt(int(parent.Fd()), name, change.Content)
			if err != nil {
				staged = append(staged, item)
				cleanup()
				return err
			}
			item.stage = stage
		}
		staged = append(staged, item)
	}

	for index := range staged {
		item := &staged[index]
		if !item.existed {
			continue
		}
		backup, file, err := createTempAt(int(item.parent.Fd()), ".constructor-backup-")
		if err != nil {
			rollbackBatch(staged)
			cleanup()
			return err
		}
		item.backup = backup
		if err := file.Close(); err != nil {
			rollbackBatch(staged)
			cleanup()
			return err
		}
		if err := unix.Unlinkat(int(item.parent.Fd()), item.backup, 0); err != nil {
			rollbackBatch(staged)
			cleanup()
			return err
		}
		if err := unix.Renameat(int(item.parent.Fd()), item.name, int(item.parent.Fd()), item.backup); err != nil {
			rollbackBatch(staged)
			cleanup()
			return err
		}
		item.backedUp = true
	}

	for index := range staged {
		item := &staged[index]
		if !item.change.Delete {
			if err := unix.Renameat(int(item.parent.Fd()), item.stage, int(item.parent.Fd()), item.name); err != nil {
				rollbackBatch(staged)
				cleanup()
				return err
			}
			item.stage = ""
		}
		item.published = true
	}
	cleanup()
	return nil
}

func rollbackBatch(changes []stagedFileChange) {
	for index := len(changes) - 1; index >= 0; index-- {
		item := &changes[index]
		if item.published && !item.change.Delete {
			_ = unix.Unlinkat(int(item.parent.Fd()), item.name, 0)
		}
		if item.backedUp {
			_ = unix.Renameat(int(item.parent.Fd()), item.backup, int(item.parent.Fd()), item.name)
			item.backup = ""
			item.backedUp = false
		}
	}
}

func cleanBatchPath(value string) (string, error) {
	if strings.TrimSpace(value) == "" || filepath.IsAbs(value) {
		return "", domain.ErrInvalidPath
	}
	clean := filepath.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.ToSlash(clean) != value || strings.Contains(value, `\`) {
		return "", domain.ErrInvalidPath
	}
	return clean, nil
}

func revision(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
