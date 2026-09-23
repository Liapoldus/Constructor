package infrastructure

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Liapoldus/Constructor/internal/domain"
)

func TestFilesystemRepositoryApplyBatchCommitsAndDeletesTogether(t *testing.T) {
	root := t.TempDir()
	repository := NewFilesystemRepository(root)
	first, err := repository.Write("liapoldus/project.json", "", []byte("project-v1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.Write("liapoldus/sites/site.json", "", []byte("site-v1"))
	if err != nil {
		t.Fatal(err)
	}

	err = repository.ApplyBatch([]domain.ProjectFileChange{
		{Path: first.Path, ExpectedRevision: first.Revision, Content: []byte("project-v2")},
		{Path: second.Path, ExpectedRevision: second.Revision, Delete: true},
		{Path: "src/pages/about.page.tsx", Content: []byte("export default function About() {}")},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := repository.Read(first.Path)
	if err != nil || string(updated.Content) != "project-v2" {
		t.Fatalf("updated project file = %q, err=%v", updated.Content, err)
	}
	if _, err := repository.Read(second.Path); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted Site file read error = %v, want not found", err)
	}
	created, err := repository.Read("src/pages/about.page.tsx")
	if err != nil || string(created.Content) != "export default function About() {}" {
		t.Fatalf("created page source = %q, err=%v", created.Content, err)
	}
}

func TestFilesystemRepositoryApplyBatchPreflightsAllRevisions(t *testing.T) {
	root := t.TempDir()
	repository := NewFilesystemRepository(root)
	current, err := repository.Write("liapoldus/project.json", "", []byte("project"))
	if err != nil {
		t.Fatal(err)
	}
	err = repository.ApplyBatch([]domain.ProjectFileChange{
		{Path: "src/pages/new.page.tsx", Content: []byte("new page")},
		{Path: current.Path, ExpectedRevision: "stale-revision", Content: []byte("candidate")},
	})
	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale batch error = %v, want ConflictError", err)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "pages", "new.page.tsx")); !os.IsNotExist(err) {
		t.Fatalf("preflight conflict partially published new page: stat error=%v", err)
	}
	unchanged, err := repository.Read(current.Path)
	if err != nil || string(unchanged.Content) != "project" {
		t.Fatalf("conflicting batch changed existing project file: content=%q err=%v", unchanged.Content, err)
	}
}

func TestFilesystemRepositoryApplyBatchRejectsDuplicateAndUnsafePaths(t *testing.T) {
	repository := NewFilesystemRepository(t.TempDir())
	for _, changes := range [][]domain.ProjectFileChange{
		{{Path: "src/pages/home.page.tsx", Content: []byte("one")}, {Path: "src/pages/home.page.tsx", Content: []byte("two")}},
		{{Path: "../outside", Content: []byte("no")}},
		{{Path: "src/../outside", Content: []byte("no")}},
	} {
		if err := repository.ApplyBatch(changes); !errors.Is(err, domain.ErrInvalidPath) {
			t.Fatalf("ApplyBatch(%#v) error = %v, want invalid path", changes, err)
		}
	}
}

func TestFilesystemRepositoryApplyBatchRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	repository := NewFilesystemRepository(root)
	err := repository.ApplyBatch([]domain.ProjectFileChange{{Path: "linked/page.tsx", Content: []byte("outside")}})
	if !errors.Is(err, domain.ErrInvalidPath) {
		t.Fatalf("symlink write error = %v, want invalid path", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "page.tsx")); !os.IsNotExist(err) {
		t.Fatalf("batch wrote outside project root: stat error=%v", err)
	}
}

func TestFilesystemRepositoryApplyBatchRejectsSymlinkLeaf(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "page.tsx")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "page.tsx")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	repository := NewFilesystemRepository(root)
	err := repository.ApplyBatch([]domain.ProjectFileChange{{Path: "page.tsx", Content: []byte("overwrite")}})
	if !errors.Is(err, domain.ErrInvalidPath) {
		t.Fatalf("symlink leaf batch write error = %v, want invalid path", err)
	}
	content, err := os.ReadFile(outside)
	if err != nil || string(content) != "private" {
		t.Fatalf("outside target changed: content=%q err=%v", content, err)
	}
}

func TestRollbackBatchRestoresExistingFilesAndRemovesNewOnes(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "project.json")
	backup := filepath.Join(root, ".constructor-backup-project")
	created := filepath.Join(root, "new.page.tsx")
	if err := os.WriteFile(backup, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	parent, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	rollbackBatch([]stagedFileChange{
		{parent: parent, name: "project.json", backup: ".constructor-backup-project", backedUp: true, existed: true},
		{parent: parent, name: "new.page.tsx", published: true},
	})
	content, err := os.ReadFile(original)
	if err != nil || string(content) != "old" {
		t.Fatalf("rollback did not restore original: content=%q err=%v", content, err)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("rollback left created file: stat error=%v", err)
	}
}
