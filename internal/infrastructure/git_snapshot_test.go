package infrastructure

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Liapoldus/Constructor/internal/domain"
)

func TestSnapshotRevisionCapturesWorkingTreeWithoutChangingIndex(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, output, err)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "-q")
	run("config", "user.name", "test")
	run("config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "content.json"), []byte("initial"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "content.json")
	run("commit", "-qm", "initial")
	head := run("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(root, "content.json"), []byte("draft"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.json"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	statusBefore := run("status", "--short")
	revision, err := NewGitCommandRepository(root).SnapshotRevision()
	if err != nil {
		t.Fatal(err)
	}
	if revision == head {
		t.Fatal("snapshot revision did not capture the working tree")
	}
	if got := run("show", revision+":content.json"); got != "draft" {
		t.Fatalf("snapshot omitted modified content: %q", got)
	}
	if got := run("show", revision+":new.json"); got != "new" {
		t.Fatalf("snapshot omitted untracked file: %q", got)
	}
	if got := run("rev-parse", "HEAD"); got != head {
		t.Fatalf("snapshot changed branch HEAD: %s", got)
	}
	if got := run("status", "--short"); got != statusBefore {
		t.Fatalf("snapshot changed working tree/index status: before %q after %q", statusBefore, got)
	}
	reference := "refs/constructor/snapshots/" + revision
	if got := run("rev-parse", "--verify", reference); got != revision {
		t.Fatalf("snapshot revision was not pinned: ref %s points to %s", reference, got)
	}
	run("gc", "--prune=now")
	run("cat-file", "-e", revision+"^{commit}")
}

func TestSnapshotWorkspaceIsDetachedAndCleansOnlyItsTemporaryRef(t *testing.T) {
	root := t.TempDir()
	run := func(directory string, args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", directory}, args...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git -C %s %v: %s: %v", directory, args, output, err)
		}
		return strings.TrimSpace(string(output))
	}
	run(root, "init", "-q")
	run(root, "config", "user.name", "test")
	run(root, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "content.json"), []byte("initial"), 0644); err != nil {
		t.Fatal(err)
	}
	run(root, "add", "content.json")
	run(root, "commit", "-qm", "initial")
	head := run(root, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(root, "content.json"), []byte("draft"), 0644); err != nil {
		t.Fatal(err)
	}
	statusBefore := run(root, "status", "--short")
	repository := NewGitCommandRepository(root)
	workspace, err := repository.CreateSnapshotWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	baseRevision := run(workspace.Root(), "rev-parse", "HEAD")
	if got := run(workspace.Root(), "show", "HEAD:content.json"); got != "draft" {
		t.Fatalf("detached worktree omitted the current draft: %q", got)
	}
	if err := os.WriteFile(filepath.Join(workspace.Root(), "generated.ts"), []byte("export const value = 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	revision, err := workspace.Head()
	if err != nil {
		t.Fatal(err)
	}
	if got := run(root, "show", revision+":generated.ts"); got != "export const value = 1" {
		t.Fatalf("snapshot commit omitted generated output: %q", got)
	}
	if got := run(root, "rev-parse", "HEAD"); got != head {
		t.Fatalf("snapshot workspace changed the active branch: %s", got)
	}
	if got := run(root, "status", "--short"); got != statusBefore {
		t.Fatalf("snapshot workspace changed active checkout: before %q after %q", statusBefore, got)
	}
	if err := workspace.Close(); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Close(); err != nil {
		t.Fatalf("closing an already removed workspace should be idempotent: %v", err)
	}
	if _, err := os.Stat(workspace.Root()); !os.IsNotExist(err) {
		t.Fatalf("temporary worktree remains after close: stat err=%v", err)
	}
	if got := run(root, "worktree", "list", "--porcelain"); strings.Count(got, "worktree ") != 1 {
		t.Fatalf("temporary worktree registration remains: %s", got)
	}
	if _, err := exec.Command("git", "-C", root, "show-ref", "--verify", "refs/constructor/snapshots/"+baseRevision).CombinedOutput(); err == nil {
		t.Fatal("temporary base revision ref was not removed")
	}
	if got := run(root, "rev-parse", "refs/constructor/snapshots/"+revision); got != revision {
		t.Fatalf("final snapshot revision was not retained: %s", got)
	}
}

func TestGitOverviewListsBranchesAndIncludesStagedChangesInDiff(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, output, err)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "-q", "--initial-branch=main")
	run("config", "user.name", "test")
	run("config", "user.email", "test@example.invalid")
	file := filepath.Join(root, "content.json")
	if err := os.WriteFile(file, []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "content.json")
	run("commit", "-qm", "initial")
	if err := os.WriteFile(file, []byte("staged\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "content.json")
	if err := os.WriteFile(file, []byte("working tree\n"), 0644); err != nil {
		t.Fatal(err)
	}
	repository := NewGitCommandRepository(root)
	branches, err := repository.Branches()
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0].Name != "main" || !branches[0].Current {
		t.Fatalf("expected current main branch, got %#v", branches)
	}
	diff, err := repository.Diff()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+working tree") || strings.Contains(diff, "+staged\n") {
		t.Fatalf("diff should represent the complete HEAD-to-working-tree change, got %q", diff)
	}
}

func TestGitCommandRepositoryCreatesAndChecksOutBranchesSafely(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, output, err)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "-q", "--initial-branch=main")
	run("config", "user.name", "test")
	run("config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "content.json"), []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "content.json")
	run("commit", "-qm", "initial")

	repository := NewGitCommandRepository(root)
	created, err := repository.CreateBranch(context.Background(), "feature/content-editor")
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "feature/content-editor" || !created.Current || created.Commit == "" {
		t.Fatalf("unexpected created branch: %#v", created)
	}
	if _, err := repository.CreateBranch(context.Background(), "feature/content-editor"); !errors.Is(err, domain.ErrGitBranchExists) {
		t.Fatalf("duplicate branch error=%v", err)
	}
	checkedOut, err := repository.CheckoutBranch(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if checkedOut.Name != "main" || !checkedOut.Current {
		t.Fatalf("unexpected checked out branch: %#v", checkedOut)
	}
	if _, err := repository.CheckoutBranch(context.Background(), "missing"); !errors.Is(err, domain.ErrGitBranchNotFound) {
		t.Fatalf("missing branch error=%v", err)
	}
	if _, err := repository.CreateBranch(context.Background(), "not a branch"); !errors.Is(err, domain.ErrInvalidGitBranch) {
		t.Fatalf("invalid branch error=%v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("keep me\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CheckoutBranch(context.Background(), "feature/content-editor"); !errors.Is(err, domain.ErrGitWorktreeDirty) {
		t.Fatalf("dirty worktree checkout error=%v", err)
	}
	if got := run("branch", "--show-current"); got != "main" {
		t.Fatalf("rejected checkout changed branch to %q", got)
	}
	if content, err := os.ReadFile(filepath.Join(root, "untracked.txt")); err != nil || string(content) != "keep me\n" {
		t.Fatalf("rejected checkout changed untracked data: content=%q err=%v", content, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.CreateBranch(cancelled, "cancelled"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request error=%v", err)
	}
}

func TestGitCommandRepositoryUsesAllowlistedEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake Git executable uses a POSIX shell")
	}
	shimDirectory := t.TempDir()
	shimPath := filepath.Join(shimDirectory, "git")
	shim := `#!/bin/sh
[ "${CONSTRUCTOR_SECRET_SENTINEL-unset}" = "unset" ] || exit 91
[ "${GIT_INDEX_FILE-unset}" = "unset" ] || exit 92
[ "${GIT_CONFIG_NOSYSTEM-unset}" = "1" ] || exit 93
[ "${GIT_TERMINAL_PROMPT-unset}" = "0" ] || exit 94
[ -n "${HOME-}" ] || exit 95
printf 'clean\n'
`
	if err := os.WriteFile(shimPath, []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDirectory)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CONSTRUCTOR_SECRET_SENTINEL", "must-not-leak")
	t.Setenv("GIT_INDEX_FILE", "/private/user/index")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "0")
	t.Setenv("GIT_TERMINAL_PROMPT", "1")

	status, err := NewGitCommandRepository(t.TempDir()).Status()
	if err != nil {
		t.Fatalf("local Git command did not receive its expected environment: %v", err)
	}
	if len(status) != 1 || status[0] != "clean" {
		t.Fatalf("unexpected fake Git status output: %#v", status)
	}
	if err := os.WriteFile(shimPath, []byte("#!/bin/sh\necho secret-from-git-stderr >&2\nexit 8\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = runLocalGit(t.TempDir(), nil, "status")
	if err == nil || strings.Contains(err.Error(), "secret-from-git-stderr") {
		t.Fatalf("Git failure leaked subprocess output or was ignored: %v", err)
	}
}

func TestSnapshotRevisionRejectsWorkingTreeChangedDuringCapture(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, output, err)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "-q", "--initial-branch=main")
	run("config", "user.name", "test")
	run("config", "user.email", "test@example.invalid")
	for name, content := range map[string]string{"a.txt": "base\n", "z.filtered": "filter input\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "--all")
	run("commit", "-qm", "initial")
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("z.filtered filter=race\n"), 0644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "clean-filter.sh")
	filter := "#!/bin/sh\ncat\nprintf 'changed\\n' > " + filepath.Join(root, "a.txt") + "\n"
	if err := os.WriteFile(script, []byte(filter), 0755); err != nil {
		t.Fatal(err)
	}
	run("config", "filter.race.clean", script)
	head := run("rev-parse", "HEAD")

	_, err := NewGitCommandRepository(root).SnapshotRevision()
	if !errors.Is(err, domain.ErrSnapshotSourceChanged) {
		t.Fatalf("expected a conflict when the working tree changes during capture, got %v", err)
	}
	if got := run("rev-parse", "HEAD"); got != head {
		t.Fatalf("snapshot conflict changed branch HEAD: %s", got)
	}
	refs, err := exec.Command("git", "-C", root, "for-each-ref", "--format=%(refname)", "refs/constructor/snapshots").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(refs)) != "" {
		t.Fatalf("snapshot conflict left a pinned reference behind: %q", refs)
	}
	if got := run("show", "HEAD:a.txt"); got != "base" {
		t.Fatalf("snapshot conflict modified committed source: %q", got)
	}
	if got, err := os.ReadFile(filepath.Join(root, "a.txt")); err != nil || string(got) != "changed\n" {
		t.Fatalf("expected external concurrent write to remain visible: %q %v", got, err)
	}
}
