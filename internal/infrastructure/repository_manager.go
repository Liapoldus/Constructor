package infrastructure

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type GitRepositoryManager struct {
	workspace  string
	operations chan struct{}
}

var commitHashPattern = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

func NewGitRepositoryManager(workspace string) *GitRepositoryManager {
	return &GitRepositoryManager{workspace: workspace, operations: make(chan struct{}, 1)}
}

func (m *GitRepositoryManager) Clone(ctx context.Context, source, target string) (string, error) {
	if err := m.acquire(ctx); err != nil {
		return "", err
	}
	defer m.release()
	if err := m.validateCloneSource(source); err != nil {
		return "", err
	}
	path, err := m.safePath(target)
	if err != nil {
		return "", err
	}
	if err := m.requireAbsent(path); err != nil {
		return "", err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return "", err
	}
	if _, err := m.safePath(target); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(parent, ".constructor-clone-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	if err := m.run(ctx, "clone", "--", source, stage); err != nil {
		return "", fmt.Errorf("git clone failed: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := m.publishClone(ctx, stage, path); err != nil {
		return "", err
	}
	return path, nil
}

func (m *GitRepositoryManager) Worktree(ctx context.Context, repository, commit, target string) (string, error) {
	if err := m.acquire(ctx); err != nil {
		return "", err
	}
	defer m.release()
	repo, err := m.safeRepository(repository)
	if err != nil {
		return "", err
	}
	if !commitHashPattern.MatchString(commit) {
		return "", domain.ErrInvalidCommit
	}
	path, err := m.safePath(target)
	if err != nil {
		return "", err
	}
	if err := m.requireAbsent(path); err != nil {
		return "", err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return "", err
	}
	if _, err := m.safePath(target); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(parent, ".constructor-worktree-")
	if err != nil {
		return "", err
	}
	recovery, journal, err := m.beginWorktreeRecovery(repo, stage, path)
	if err != nil {
		_ = os.RemoveAll(stage)
		return "", err
	}
	if err := m.run(ctx, "-C", repo, "worktree", "add", "--detach", "--", stage, commit); err != nil {
		cleanupErr := m.removeWorktreeChecked(repo, stage)
		if cleanupErr == nil {
			cleanupErr = m.clearWorktreeRecovery(journal)
		}
		return "", errors.Join(fmt.Errorf("git worktree creation failed: %w", err), cleanupErr)
	}
	if err := ctx.Err(); err != nil {
		cleanupErr := m.removeWorktreeChecked(repo, stage)
		if cleanupErr == nil {
			cleanupErr = m.clearWorktreeRecovery(journal)
		}
		return "", errors.Join(err, cleanupErr)
	}
	adminMarker, err := m.worktreeAdminMarker(stage)
	if err != nil {
		cleanupErr := m.removeWorktreeChecked(repo, stage)
		if cleanupErr == nil {
			cleanupErr = m.clearWorktreeRecovery(journal)
		}
		return "", errors.Join(fmt.Errorf("read staged worktree metadata: %w", err), cleanupErr)
	}
	recovery.Phase = "ready"
	recovery.AdminMarker = string(adminMarker)
	root, err := m.workspaceRoot()
	if err == nil {
		err = m.saveWorktreeRecovery(root, journal, recovery)
	}
	if err != nil {
		cleanupErr := m.removeWorktreeChecked(repo, stage)
		if cleanupErr == nil {
			cleanupErr = m.clearWorktreeRecovery(journal)
		}
		return "", errors.Join(fmt.Errorf("persist worktree publication state: %w", err), cleanupErr)
	}
	if err := m.publishWorktree(stage, path); err != nil {
		cleanupErr := m.removeWorktreeChecked(repo, stage)
		if cleanupErr == nil {
			cleanupErr = m.clearWorktreeRecovery(journal)
		}
		return "", errors.Join(err, cleanupErr)
	}
	repairCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := m.run(repairCtx, "-C", repo, "worktree", "repair", path); err != nil {
		if m.cleanupInterruptedWorktree(repo, stage, path, adminMarker) {
			if clearErr := m.clearWorktreeRecovery(journal); clearErr != nil {
				return "", errors.Join(err, clearErr)
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return "", ctxErr
			}
			if repairErr := repairCtx.Err(); repairErr != nil {
				return "", repairErr
			}
			return "", fmt.Errorf("git worktree metadata repair failed after publication")
		}
		return "", fmt.Errorf("git worktree metadata repair failed after publication: %w", err)
	}
	if err := ctx.Err(); err != nil {
		cleanupErr := m.removeWorktreeChecked(repo, path)
		if cleanupErr == nil {
			cleanupErr = m.clearWorktreeRecovery(journal)
		}
		return "", errors.Join(err, cleanupErr)
	}
	if err := m.clearWorktreeRecovery(journal); err != nil {
		cleanupErr := m.removeWorktreeChecked(repo, path)
		if cleanupErr == nil {
			cleanupErr = m.clearWorktreeRecovery(journal)
		}
		return "", errors.Join(fmt.Errorf("clear completed worktree recovery record: %w", err), cleanupErr)
	}
	return path, nil
}

func (m *GitRepositoryManager) publishWorktree(stage, target string) error {
	root, err := m.workspaceRoot()
	if err != nil {
		return err
	}
	if err := publishDirectoryNoReplace(root, stage, target); err != nil {
		if os.IsExist(err) {
			return domain.ErrRepositoryTargetExists
		}
		return fmt.Errorf("worktree publication failed: %w", err)
	}
	return nil
}

func (m *GitRepositoryManager) worktreeAdminMarker(worktree string) ([]byte, error) {
	root, err := m.workspaceRoot()
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(root, worktree)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, domain.ErrInvalidPath
	}
	parent, name, err := openProjectParent(root, filepath.ToSlash(filepath.Join(relative, ".git")), false)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	return readProjectAt(int(parent.Fd()), name)
}

// cleanupInterruptedWorktree recognizes only a worktree whose Git admin marker
// is the exact marker captured from our staging path before publication.
func (m *GitRepositoryManager) cleanupInterruptedWorktree(repository, stage, target string, marker []byte) bool {
	targetMarker, err := m.worktreeAdminMarker(target)
	if err != nil || string(targetMarker) != string(marker) {
		return false
	}
	if _, err := m.worktreeAdminMarker(stage); err == nil || err != domain.ErrNotFound {
		return false
	}
	repairCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.run(repairCtx, "-C", repository, "worktree", "repair", target); err != nil {
		return false
	}
	if err := m.removeWorktreeChecked(repository, target); err != nil {
		return false
	}
	return true
}

func (m *GitRepositoryManager) acquire(ctx context.Context) error {
	if m.operations == nil {
		return fmt.Errorf("repository manager is not initialized")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case m.operations <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-m.operations
			return err
		}
		return nil
	}
}

func (m *GitRepositoryManager) release() {
	<-m.operations
}

func (m *GitRepositoryManager) validateCloneSource(source string) error {
	source = strings.TrimSpace(source)
	if source == "" || strings.HasPrefix(source, "-") || strings.ContainsAny(source, "\r\n\x00") {
		return domain.ErrInvalidRepositoryURL
	}
	if strings.Contains(source, "://") {
		parsed, err := url.Parse(source)
		if err != nil {
			return domain.ErrInvalidRepositoryURL
		}
		scheme := strings.ToLower(parsed.Scheme)
		switch scheme {
		case "https", "ssh", "git":
			hasPassword := false
			if parsed.User != nil {
				_, hasPassword = parsed.User.Password()
			}
			if parsed.Host == "" || scheme != "ssh" && parsed.User != nil || hasPassword {
				return domain.ErrInvalidRepositoryURL
			}
			return nil
		case "file":
			_, err := m.safeRepository(parsed.Path)
			return err
		default:
			return domain.ErrInvalidRepositoryURL
		}
	}
	if strings.Contains(source, "@") && strings.Contains(source, ":") && !strings.ContainsAny(source, " \t") {
		return nil // SSH scp-style URL, e.g. git@github.com:owner/repository.git
	}
	_, err := m.safeRepository(source)
	return err
}

func (m *GitRepositoryManager) safeRepository(repository string) (string, error) {
	root, err := m.workspaceRoot()
	if err != nil {
		return "", err
	}
	var path string
	if filepath.IsAbs(repository) {
		path, err = filepath.Abs(repository)
	} else {
		path, err = filepath.Abs(filepath.Join(root, repository))
	}
	if err != nil {
		return "", err
	}
	if path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return "", domain.ErrInvalidPath
	}
	if isConstructorStatePath(path, root) {
		return "", domain.ErrInvalidPath
	}
	if _, _, err := checkedPath(root, path); err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", domain.ErrInvalidPath
	}
	return path, nil
}

func (m *GitRepositoryManager) safePath(target string) (string, error) {
	if target == "" || filepath.IsAbs(target) {
		return "", domain.ErrInvalidPath
	}
	for _, segment := range strings.Split(filepath.ToSlash(target), "/") {
		if segment == ".." {
			return "", domain.ErrInvalidPath
		}
	}
	root, err := m.workspaceRoot()
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(root, target))
	if err != nil {
		return "", err
	}
	if path == root || !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return "", domain.ErrInvalidPath
	}
	if isConstructorStatePath(path, root) {
		return "", domain.ErrInvalidPath
	}
	_, _, err = checkedPath(root, path)
	return path, err
}

func isConstructorStatePath(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return true
	}
	for _, segment := range strings.Split(filepath.ToSlash(relative), "/") {
		if segment == ".constructor-state" {
			return true
		}
	}
	return false
}

func (m *GitRepositoryManager) workspaceRoot() (string, error) {
	if strings.TrimSpace(m.workspace) == "" {
		return "", fmt.Errorf("workspace root is required")
	}
	root, err := filepath.Abs(m.workspace)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return "", err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", domain.ErrInvalidPath
	}
	return root, nil
}

func checkedPath(root, path string) (string, string, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", domain.ErrInvalidPath
	}
	current := root
	segments := strings.Split(relative, string(filepath.Separator))
	for index, segment := range segments {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return relative, path, nil
		}
		if err != nil {
			return "", "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || index < len(segments)-1 && !info.IsDir() {
			return "", "", domain.ErrInvalidPath
		}
	}
	return relative, path, nil
}

func (m *GitRepositoryManager) requireAbsent(path string) error {
	_, err := os.Lstat(path)
	if err == nil {
		return domain.ErrRepositoryTargetExists
	}
	if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *GitRepositoryManager) publishClone(ctx context.Context, stage, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := m.workspaceRoot()
	if err != nil {
		return err
	}
	if err := publishDirectoryNoReplace(root, stage, target); err != nil {
		if os.IsExist(err) {
			return domain.ErrRepositoryTargetExists
		}
		return fmt.Errorf("clone publication failed: %w", err)
	}
	return nil
}

func (m *GitRepositoryManager) removeWorktreeChecked(repository, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.run(ctx, "-C", repository, "worktree", "remove", "--force", path)
	_ = m.run(ctx, "-C", repository, "worktree", "prune")
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	registered, err := m.worktreeRegistered(repository, path)
	if err != nil {
		return err
	}
	if registered {
		return fmt.Errorf("Git still registers worktree after cleanup")
	}
	if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
		return fmt.Errorf("worktree directory remains after cleanup")
	}
	return nil
}

func (m *GitRepositoryManager) run(ctx context.Context, args ...string) error {
	commandArgs := append([]string{"-c", "protocol.ext.allow=never", "-c", "core.hooksPath=/dev/null"}, args...)
	command := gitCommand(ctx, commandArgs...)
	command.Env = gitEnvironment()
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	err := command.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return err
	}
	return nil
}

func (m *GitRepositoryManager) runOutput(ctx context.Context, args ...string) ([]byte, error) {
	commandArgs := append([]string{"-c", "protocol.ext.allow=never", "-c", "core.hooksPath=/dev/null"}, args...)
	command := gitCommand(ctx, commandArgs...)
	command.Env = gitEnvironment()
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

func (m *GitRepositoryManager) worktreeRegistered(repository, path string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := m.runOutput(ctx, "-C", repository, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return false, fmt.Errorf("inspect Git worktree registrations: %w", err)
	}
	for _, field := range bytes.Split(output, []byte{0}) {
		if bytes.HasPrefix(field, []byte("worktree ")) && filepath.Clean(string(field[len("worktree "):])) == filepath.Clean(path) {
			return true, nil
		}
	}
	return false, nil
}

func gitEnvironment() []string {
	allowed := []string{}
	for _, key := range []string{"PATH", "HOME", "SSH_AUTH_SOCK", "SSH_AGENT_PID"} {
		if value, exists := os.LookupEnv(key); exists && value != "" {
			allowed = append(allowed, key+"="+value)
		}
	}
	return append(allowed, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
}
