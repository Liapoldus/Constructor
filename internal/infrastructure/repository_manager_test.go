package infrastructure

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRepositoryManagerCloneAndWorktree(t *testing.T) {
	workspace := t.TempDir()
	source := filepath.Join(workspace, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, source, "init", "-q")
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("repository fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, source, "add", "README.md")
	gitTestCommand(t, source, "-c", "user.name=Constructor Test", "-c", "user.email=test@example.invalid", "commit", "-m", "fixture")
	commit := strings.TrimSpace(gitTestCommand(t, source, "rev-parse", "HEAD"))
	manager := NewGitRepositoryManager(workspace)

	clonePath, err := manager.Clone(context.Background(), source, "clones/project")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(clonePath, "README.md")); err != nil || string(got) != "repository fixture\n" {
		t.Fatalf("clone content=%q err=%v", got, err)
	}
	worktreePath, err := manager.Worktree(context.Background(), source, commit, "worktrees/revision")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(worktreePath, "README.md")); err != nil || string(got) != "repository fixture\n" {
		t.Fatalf("worktree content=%q err=%v", got, err)
	}
	root, err := manager.workspaceRoot()
	if err != nil {
		t.Fatal(err)
	}
	if journals, err := listProjectFiles(root, worktreeRecoveryDirectory); err != nil || len(journals) != 0 {
		t.Fatalf("successful worktree left recovery records: journals=%v err=%v", journals, err)
	}
	for _, path := range []string{filepath.Join(workspace, ".constructor-state"), filepath.Join(workspace, worktreeRecoveryDirectory)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("recovery directory %q is accessible to group/other: mode=%#o", path, info.Mode().Perm())
		}
	}
}

func TestWorktreeCancellationDuringRepairCleansPublishedTree(t *testing.T) {
	workspace := t.TempDir()
	repository := filepath.Join(workspace, "source")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repository, "init", "-q")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repository, "add", "README.md")
	gitTestCommand(t, repository, "-c", "user.name=Constructor Test", "-c", "user.email=test@example.invalid", "commit", "-m", "fixture")
	commit := strings.TrimSpace(gitTestCommand(t, repository, "rev-parse", "HEAD"))

	bin := filepath.Join(workspace, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	repairStarted := filepath.Join(workspace, "repair-started")
	script := "#!/bin/sh\n" +
		"if [ \"$7\" = worktree ] && [ \"$8\" = add ]; then\n" +
		"  /bin/mkdir -p \"${11}\"\n" +
		"  printf 'gitdir: test-admin\\n' > \"${11}/.git\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$7\" = worktree ] && [ \"$8\" = repair ]; then\n" +
		"  if [ ! -e '" + repairStarted + "' ]; then\n" +
		"    printf started > '" + repairStarted + "'\n" +
		"    /bin/sleep 30\n" +
		"  fi\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	manager := NewGitRepositoryManager(workspace)
	target := filepath.Join(workspace, "worktrees", "revision")
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	_, err := manager.Worktree(ctx, repository, commit, "worktrees/revision")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Worktree error = %v, want request cancellation", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 5*time.Second {
		t.Fatalf("request cancellation took %s; Git move was not cancelled promptly", elapsed)
	}
	if _, err := os.Stat(repairStarted); err != nil {
		t.Fatalf("worktree was not atomically published before metadata repair: %v", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("cancelled publication left target behind: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(workspace, "worktrees"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cancelled publication left staging entries: %#v", entries)
	}
}

func TestInterruptedWorktreeMoveCanRepairGitMetadataAndCleanUp(t *testing.T) {
	workspace := t.TempDir()
	repository := filepath.Join(workspace, "source")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repository, "init", "-q")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repository, "add", "README.md")
	gitTestCommand(t, repository, "-c", "user.name=Constructor Test", "-c", "user.email=test@example.invalid", "commit", "-m", "fixture")
	commit := strings.TrimSpace(gitTestCommand(t, repository, "rev-parse", "HEAD"))

	stage, err := os.MkdirTemp(workspace, ".constructor-worktree-stage-")
	if err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repository, "worktree", "add", "--detach", "--", stage, commit)
	manager := NewGitRepositoryManager(workspace)
	marker, err := manager.worktreeAdminMarker(stage)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workspace, "published")
	if err := os.Rename(stage, target); err != nil {
		t.Fatal(err)
	}
	if !manager.cleanupInterruptedWorktree(repository, stage, target, marker) {
		t.Fatal("interrupted worktree publication was not repaired and cleaned")
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("repaired worktree target remains: %v", err)
	}
	worktrees := gitTestCommand(t, repository, "worktree", "list", "--porcelain")
	if strings.Contains(worktrees, target) {
		t.Fatalf("Git still registers the removed worktree: %s", worktrees)
	}
}

func TestRecoverPendingWorktreeAfterProcessRestart(t *testing.T) {
	workspace := t.TempDir()
	repository := filepath.Join(workspace, "source")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repository, "init", "-q")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repository, "add", "README.md")
	gitTestCommand(t, repository, "-c", "user.name=Constructor Test", "-c", "user.email=test@example.invalid", "commit", "-m", "fixture")
	commit := strings.TrimSpace(gitTestCommand(t, repository, "rev-parse", "HEAD"))

	manager := NewGitRepositoryManager(workspace)
	stage, err := os.MkdirTemp(workspace, ".constructor-worktree-")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workspace, "published")
	record, journal, err := manager.beginWorktreeRecovery(repository, stage, target)
	if err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repository, "worktree", "add", "--detach", "--", stage, commit)
	marker, err := manager.worktreeAdminMarker(stage)
	if err != nil {
		t.Fatal(err)
	}
	record.Phase = "ready"
	record.AdminMarker = string(marker)
	root, err := manager.workspaceRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.saveWorktreeRecovery(root, journal, record); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stage, target); err != nil {
		t.Fatal(err)
	}

	restartedManager := NewGitRepositoryManager(workspace)
	if err := restartedManager.RecoverPendingWorktrees(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("recovery left interrupted target behind: %v", err)
	}
	if journals, err := listProjectFiles(root, worktreeRecoveryDirectory); err != nil || len(journals) != 0 {
		t.Fatalf("recovery left journal files: journals=%v err=%v", journals, err)
	}
	worktrees := gitTestCommand(t, repository, "worktree", "list", "--porcelain")
	if strings.Contains(worktrees, target) {
		t.Fatalf("recovery left stale Git worktree registration: %s", worktrees)
	}
}

func TestRecoverWorktreeRecordBeforeReadyPhase(t *testing.T) {
	workspace := t.TempDir()
	repository := filepath.Join(workspace, "source")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repository, "init", "-q")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repository, "add", "README.md")
	gitTestCommand(t, repository, "-c", "user.name=Constructor Test", "-c", "user.email=test@example.invalid", "commit", "-m", "fixture")
	commit := strings.TrimSpace(gitTestCommand(t, repository, "rev-parse", "HEAD"))

	manager := NewGitRepositoryManager(workspace)
	stage, err := os.MkdirTemp(workspace, ".constructor-worktree-")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workspace, "published")
	_, _, err = manager.beginWorktreeRecovery(repository, stage, target)
	if err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repository, "worktree", "add", "--detach", "--", stage, commit)

	if err := NewGitRepositoryManager(workspace).RecoverPendingWorktrees(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(stage); !os.IsNotExist(err) {
		t.Fatalf("recovery left pre-publication worktree stage: %v", err)
	}
	worktrees := gitTestCommand(t, repository, "worktree", "list", "--porcelain")
	if strings.Contains(worktrees, stage) {
		t.Fatalf("recovery left pre-publication Git worktree registration: %s", worktrees)
	}
}

func TestWorktreeRecoveryRejectsJournalPathsOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	repository := filepath.Join(workspace, "source")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repository, "init", "-q")
	target := filepath.Join(t.TempDir(), "must-survive")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewGitRepositoryManager(workspace)
	var recordID [12]byte
	for index := range recordID {
		recordID[index] = byte(index + 1)
	}
	encodedID := hex.EncodeToString(recordID[:])
	journal := filepath.ToSlash(filepath.Join(worktreeRecoveryDirectory, encodedID+".json"))
	record := worktreeRecoveryRecord{
		SchemaVersion: 1,
		ID:            encodedID,
		Repository:    "source",
		Stage:         "../must-survive",
		Target:        "published",
		Phase:         "creating",
	}
	root, err := manager.workspaceRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.saveWorktreeRecovery(root, journal, record); err != nil {
		t.Fatal(err)
	}
	if err := manager.RecoverPendingWorktrees(context.Background()); err == nil {
		t.Fatal("recovery accepted a path outside its workspace")
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != "keep" {
		t.Fatalf("outside target was changed: content=%q err=%v", content, err)
	}
}

func TestWorktreeDoesNotReplaceAnExistingTarget(t *testing.T) {
	workspace := t.TempDir()
	repository := filepath.Join(workspace, "source")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repository, "init", "-q")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repository, "add", "README.md")
	gitTestCommand(t, repository, "-c", "user.name=Constructor Test", "-c", "user.email=test@example.invalid", "commit", "-m", "fixture")
	commit := strings.TrimSpace(gitTestCommand(t, repository, "rev-parse", "HEAD"))

	target := filepath.Join(workspace, "existing")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewGitRepositoryManager(workspace)
	if _, err := manager.Worktree(context.Background(), repository, commit, "existing"); err == nil {
		t.Fatal("worktree unexpectedly replaced an existing target")
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "keep" {
		t.Fatalf("existing target changed: content=%q err=%v", content, err)
	}
}

func TestDirectoryPublicationDoesNotReplaceAnExistingTarget(t *testing.T) {
	root := t.TempDir()
	parent, err := openProjectRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := os.Mkdir(filepath.Join(root, "stage"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "target"), 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "target", "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	err = renameDirectoryNoReplaceAt(int(parent.Fd()), "stage", "target")
	if !os.IsExist(err) {
		t.Fatalf("publication error = %v, want target-exists", err)
	}
	if _, err := os.Stat(filepath.Join(root, "stage")); err != nil {
		t.Fatalf("failed publication removed staging directory: %v", err)
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "keep" {
		t.Fatalf("existing target changed: content=%q err=%v", content, err)
	}
}

func TestRepositoryManagerRejectsTraversalAndSymlinkTargets(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	source := filepath.Join(workspace, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "linked")); err != nil {
		t.Fatal(err)
	}
	manager := NewGitRepositoryManager(workspace)
	for _, target := range []string{"../escape", "linked/escape", ".constructor-state/forbidden"} {
		if _, err := manager.Clone(context.Background(), source, target); err == nil {
			t.Errorf("Clone target %q unexpectedly succeeded", target)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "escape")); !os.IsNotExist(err) {
		t.Fatalf("clone wrote through symlink: %v", err)
	}
}

func TestRepositoryManagerCloneSourcePolicy(t *testing.T) {
	workspace := t.TempDir()
	local := filepath.Join(workspace, "local-repo")
	if err := os.Mkdir(local, 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewGitRepositoryManager(workspace)
	for _, source := range []string{
		"https://github.com/Liapoldus/Constructor.git",
		"ssh://git@github.com/Liapoldus/Constructor.git",
		"git@github.com:Liapoldus/Constructor.git",
		local,
	} {
		if err := manager.validateCloneSource(source); err != nil {
			t.Errorf("valid source %q rejected: %v", source, err)
		}
	}
	for _, source := range []string{
		"ext::sh -c touch\\\\ bad",
		"https://token:secret@example.invalid/private.git",
		"file:///etc/passwd",
		"-upload-pack=bad",
	} {
		if err := manager.validateCloneSource(source); err == nil {
			t.Errorf("unsafe source %q accepted", source)
		}
	}
}

func TestRepositoryManagerCleansFailedCloneAndWorktree(t *testing.T) {
	workspace := t.TempDir()
	manager := NewGitRepositoryManager(workspace)
	notRepository := filepath.Join(workspace, "not-a-repository")
	if err := os.Mkdir(notRepository, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Clone(context.Background(), notRepository, "clone-failure"); err == nil {
		t.Fatal("clone from a non-repository unexpectedly succeeded")
	}
	if _, err := os.Lstat(filepath.Join(workspace, "clone-failure")); !os.IsNotExist(err) {
		t.Fatalf("failed clone left target behind: %v", err)
	}
	if _, err := manager.Worktree(context.Background(), notRepository, strings.Repeat("a", 40), "worktree-failure"); err == nil {
		t.Fatal("worktree from a non-repository unexpectedly succeeded")
	}
	for _, entry := range []string{"clone-failure", "worktree-failure"} {
		if _, err := os.Lstat(filepath.Join(workspace, entry)); !os.IsNotExist(err) {
			t.Errorf("failed operation left target %q behind: %v", entry, err)
		}
	}
}

func TestRepositoryManagerHonorsCancellationAndRemovesStaging(t *testing.T) {
	workspace := t.TempDir()
	bin := filepath.Join(workspace, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	started := filepath.Join(workspace, "git-started")
	childPID := filepath.Join(workspace, "child.pid")
	childEnv := filepath.Join(workspace, "child.env")
	script := "#!/bin/sh\nprintf started > '" + started + "'\nprintf '%s\\n' \"${CONSTRUCTOR_SECRET_SENTINEL-unset}\" > '" + childEnv + "'\n/bin/sleep 30 &\necho $! > '" + childPID + "'\nwait\n"
	fakeGit := filepath.Join(bin, "git")
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("CONSTRUCTOR_SECRET_SENTINEL", "must-not-leak")
	manager := NewGitRepositoryManager(workspace)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := manager.Clone(ctx, "https://example.invalid/repository.git", "clone-cancelled")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Clone error=%v, want context deadline", err)
	}
	if _, err := os.Stat(started); err != nil {
		t.Fatalf("fake Git was not run: %v", err)
	}
	if data, err := os.ReadFile(childEnv); err != nil || strings.TrimSpace(string(data)) != "unset" {
		t.Fatalf("Git environment was not allowlisted: value=%q err=%v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(workspace, "clone-cancelled")); !os.IsNotExist(err) {
		t.Fatalf("cancelled clone left target behind: %v", err)
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".constructor-clone-") {
			t.Fatalf("cancelled clone left staging directory %q", entry.Name())
		}
	}
	if data, err := os.ReadFile(childPID); err == nil {
		pid := strings.TrimSpace(string(data))
		if result := exec.Command("/bin/kill", "-0", pid).Run(); result == nil {
			t.Fatalf("cancelled Git descendant process %s is still running", pid)
		}
	}
}

func TestRepositoryManagerCancelsWhileWaitingForWorkspaceOperation(t *testing.T) {
	manager := NewGitRepositoryManager(t.TempDir())
	manager.operations <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := manager.Clone(ctx, "https://example.invalid/repository.git", "waiting")
	<-manager.operations
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued Clone error=%v, want context deadline", err)
	}
}

func gitTestCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
