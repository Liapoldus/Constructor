package infrastructure

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Liapoldus/Constructor/internal/domain"
)

func TestFilesystemRepositoryUsesOptimisticRevision(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "liapoldus")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	r := NewFilesystemRepository(root)
	first, err := r.Write("liapoldus/project.json", "", []byte(`{"name":"one"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Write("liapoldus/project.json", "stale", []byte(`{"name":"two"}`))
	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if string(conflict.Current) != `{"name":"one"}` || string(conflict.Candidate) != `{"name":"two"}` {
		t.Fatalf("unexpected conflict blobs: current=%s candidate=%s", conflict.Current, conflict.Candidate)
	}
	if _, err := r.Write("liapoldus/project.json", first.Revision, []byte(`{"name":"two"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestFilesystemRepositoryRejectsSymlinkTraversal(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	repository := NewFilesystemRepository(root)
	if _, err := repository.Read("linked/secret.txt"); !errors.Is(err, domain.ErrInvalidPath) {
		t.Fatalf("symlink read error = %v, want invalid path", err)
	}
	if _, err := repository.Write("linked/new.txt", "", []byte("outside")); !errors.Is(err, domain.ErrInvalidPath) {
		t.Fatalf("symlink write error = %v, want invalid path", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("write through symlink created an outside file: err=%v", err)
	}
}

func TestFilesystemRepositoryRejectsSymlinkLeaf(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "secret.txt")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	repository := NewFilesystemRepository(root)
	if _, err := repository.Read("secret.txt"); !errors.Is(err, domain.ErrInvalidPath) {
		t.Errorf("symlink leaf read error = %v, want invalid path", err)
	}
	if _, err := repository.Write("secret.txt", "", []byte("overwrite")); !errors.Is(err, domain.ErrInvalidPath) {
		t.Errorf("symlink leaf write error = %v, want invalid path", err)
	}
	content, err := os.ReadFile(outside)
	if err != nil || string(content) != "private" {
		t.Fatalf("outside target changed: content=%q err=%v", content, err)
	}
}

func TestFilesystemRepositoryListRejectsPrivateAndSymlinkDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "safe"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, ".git"), filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	repository := NewFilesystemRepository(root)
	if _, err := repository.List(".git"); !errors.Is(err, domain.ErrInvalidPath) {
		t.Errorf("private directory list error = %v, want invalid path", err)
	}
	if _, err := repository.List("linked"); !errors.Is(err, domain.ErrInvalidPath) {
		t.Errorf("symlink directory list error = %v, want invalid path", err)
	}
}

func TestFilesystemRepositoryRejectsParentTraversal(t *testing.T) {
	repository := NewFilesystemRepository(t.TempDir())
	if _, err := repository.Read("nested/../secret.txt"); !errors.Is(err, domain.ErrInvalidPath) {
		t.Fatalf("parent traversal error = %v, want invalid path", err)
	}
}

func TestFilesystemRepositoryDoesNotExposePrivateProjectPaths(t *testing.T) {
	repository := NewFilesystemRepository(t.TempDir())
	for _, path := range []string{".git/config", ".env", ".env.production", "node_modules/package/index.js", ".constructor-state/worktrees/recovery.json"} {
		if _, err := repository.Read(path); !errors.Is(err, domain.ErrInvalidPath) {
			t.Errorf("read %q error = %v, want invalid path", path, err)
		}
	}
}
