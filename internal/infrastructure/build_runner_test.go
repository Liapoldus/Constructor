package infrastructure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDirectoryChecksumIsStableAndTracksContents(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	for _, root := range []string{first, second} {
		if err := os.MkdirAll(filepath.Join(root, "nested"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []struct{ path, body string }{
		{"z.txt", "last"}, {"nested/a.txt", "first"},
	} {
		if err := os.WriteFile(filepath.Join(first, file.path), []byte(file.body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []struct{ path, body string }{
		{"nested/a.txt", "first"}, {"z.txt", "last"},
	} {
		if err := os.WriteFile(filepath.Join(second, file.path), []byte(file.body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	firstSum, err := directoryChecksum(first)
	if err != nil {
		t.Fatal(err)
	}
	secondSum, err := directoryChecksum(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstSum != secondSum || len(firstSum) != sha256.Size*2 {
		t.Fatalf("checksum depends on creation order or is not SHA-256: %q != %q", firstSum, secondSum)
	}
	if err := os.WriteFile(filepath.Join(second, "z.txt"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	changed, err := directoryChecksum(second)
	if err != nil {
		t.Fatal(err)
	}
	if changed == firstSum {
		t.Fatal("checksum did not change when artifact content changed")
	}
}

func TestBuildErrorDoesNotExposeCommandOutput(t *testing.T) {
	err := &BuildError{Output: "private-token-and-stack-trace", Err: context.DeadlineExceeded}
	if strings.Contains(err.Error(), err.Output) || err.Error() != "build timed out" {
		t.Fatalf("build error leaked worker output: %q", err.Error())
	}
}

func TestBuildCommandHelperProcess(t *testing.T) {
	if os.Getenv("CONSTRUCTOR_BUILD_HELPER") != "1" {
		return
	}
	select {}
}

func TestBuildCommandHonorsContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBuildCommandHelperProcess$")
	command.Env = append(os.Environ(), "CONSTRUCTOR_BUILD_HELPER=1")
	started := time.Now()
	_, err := runBuildCommand(ctx, command)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runBuildCommand error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("context cancellation took too long: %s", elapsed)
	}
}

func TestDirectoryChecksumRejectsEmptyAndSymlinkArtifacts(t *testing.T) {
	empty := t.TempDir()
	if _, err := directoryChecksum(empty); err == nil {
		t.Fatal("empty artifact should not have a valid checksum")
	}
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if _, err := directoryChecksum(root); err == nil {
		t.Fatal("symlink artifact should be rejected")
	}
}

func TestDirectoryChecksumDigestFormat(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	checksum, err := directoryChecksum(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hex.DecodeString(checksum); err != nil || len(checksum) != 64 {
		t.Fatalf("checksum %q is not a SHA-256 hex digest: %v", checksum, err)
	}
}
