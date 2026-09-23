package infrastructure

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectWorkspaceSwitchesFilesystemRepository(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	for _, root := range []string{first, second} {
		if err := os.MkdirAll(filepath.Join(root, "liapoldus"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "liapoldus", "content.json"), []byte(root), 0644); err != nil {
			t.Fatal(err)
		}
	}
	workspace := NewProjectWorkspace(first)
	repository := NewFilesystemRepositoryWithWorkspace(workspace)
	value, err := repository.Read("liapoldus/content.json")
	if err != nil || string(value.Content) != first {
		t.Fatalf("first root read failed: %v %q", err, value.Content)
	}
	if err := workspace.Activate(second); err != nil {
		t.Fatal(err)
	}
	value, err = repository.Read("liapoldus/content.json")
	if err != nil || string(value.Content) != second {
		t.Fatalf("second root read failed: %v %q", err, value.Content)
	}
}
