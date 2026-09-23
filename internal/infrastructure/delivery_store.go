package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type GitCommandRepository struct {
	root      string
	workspace *ProjectWorkspace
}

func NewGitCommandRepository(root string) *GitCommandRepository {
	return &GitCommandRepository{root: root}
}
func NewGitCommandRepositoryWithWorkspace(workspace *ProjectWorkspace) *GitCommandRepository {
	return &GitCommandRepository{workspace: workspace}
}
func (g *GitCommandRepository) activeRoot() string {
	if g.workspace != nil {
		return g.workspace.Root()
	}
	return g.root
}
func (g *GitCommandRepository) Root() string { return g.activeRoot() }
func (g *GitCommandRepository) Head() (string, error) {
	return g.SnapshotRevision()
}
func (g *GitCommandRepository) SnapshotRevision() (string, error) {
	root := g.activeRoot()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	parentBytes, err := runLocalGitContext(ctx, root, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	parent := strings.TrimSpace(string(parentBytes))
	tmp, err := os.CreateTemp("", "constructor-index-")
	if err != nil {
		return "", err
	}
	index := tmp.Name()
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Remove(index); err != nil {
		return "", err
	}
	defer os.Remove(index)
	env := []string{"GIT_INDEX_FILE=" + filepath.Clean(index)}
	run := func(args ...string) ([]byte, error) {
		return runLocalGitContext(ctx, root, env, args...)
	}
	if output, err := run("read-tree", parent); err != nil {
		return "", fmt.Errorf("git read-tree: %s: %w", strings.TrimSpace(string(output)), err)
	}
	if output, err := run("add", "-A"); err != nil {
		return "", fmt.Errorf("git snapshot stage: %s: %w", strings.TrimSpace(string(output)), err)
	}
	treeOutput, err := run("write-tree")
	if err != nil {
		return "", fmt.Errorf("git write-tree: %s: %w", strings.TrimSpace(string(treeOutput)), err)
	}
	tree := strings.TrimSpace(string(treeOutput))
	if output, err := run("add", "-A"); err != nil {
		return "", fmt.Errorf("git verify snapshot stability: %s: %w", strings.TrimSpace(string(output)), err)
	}
	verifiedTreeOutput, err := run("write-tree")
	if err != nil {
		return "", fmt.Errorf("git verify snapshot tree: %s: %w", strings.TrimSpace(string(verifiedTreeOutput)), err)
	}
	if strings.TrimSpace(string(verifiedTreeOutput)) != tree {
		return "", domain.ErrSnapshotSourceChanged
	}
	commitEnv := append(append([]string{}, env...), "GIT_AUTHOR_NAME=Constructor", "GIT_AUTHOR_EMAIL=constructor@localhost", "GIT_COMMITTER_NAME=Constructor", "GIT_COMMITTER_EMAIL=constructor@localhost")
	output, err := runLocalGitContext(ctx, root, commitEnv, "commit-tree", tree, "-p", parent, "-m", "Constructor immutable snapshot")
	if err != nil {
		return "", err
	}
	revision := strings.TrimSpace(string(output))
	reference := "refs/constructor/snapshots/" + revision
	if _, err := runLocalGitContext(ctx, root, nil, "update-ref", reference, revision); err != nil {
		return "", err
	}
	return revision, nil
}

func (g *GitCommandRepository) CreateSnapshotWorkspace() (domain.SnapshotWorkspace, error) {
	return g.CreateSnapshotWorkspaceAt(g.activeRoot())
}

func (g *GitCommandRepository) CreateSnapshotWorkspaceAt(root string) (domain.SnapshotWorkspace, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	baseRepository := &GitCommandRepository{root: root}
	baseRevision, err := baseRepository.SnapshotRevision()
	if err != nil {
		return nil, err
	}
	target, err := os.MkdirTemp("", "constructor-snapshot-worktree-")
	if err != nil {
		_ = baseRepository.deleteSnapshotRef(baseRevision)
		return nil, err
	}
	if err := os.Remove(target); err != nil {
		_ = os.RemoveAll(target)
		_ = baseRepository.deleteSnapshotRef(baseRevision)
		return nil, err
	}
	_, err = runLocalGit(root, nil, "worktree", "add", "--detach", target, baseRevision)
	if err != nil {
		_, _ = runLocalGit(root, nil, "worktree", "remove", "--force", target)
		_ = os.RemoveAll(target)
		_ = baseRepository.deleteSnapshotRef(baseRevision)
		return nil, fmt.Errorf("create snapshot worktree: %w", err)
	}
	return &gitSnapshotWorkspace{repositoryRoot: root, path: target, baseRevision: baseRevision}, nil
}

func (g *GitCommandRepository) deleteSnapshotRef(revision string) error {
	reference := "refs/constructor/snapshots/" + revision
	_, err := runLocalGit(g.activeRoot(), nil, "update-ref", "-d", reference, revision)
	if err != nil {
		return fmt.Errorf("remove temporary snapshot ref: %w", err)
	}
	return nil
}

type gitSnapshotWorkspace struct {
	repositoryRoot  string
	path            string
	baseRevision    string
	worktreeRemoved bool
	baseRefRemoved  bool
}

func (w *gitSnapshotWorkspace) Root() string { return w.path }
func (w *gitSnapshotWorkspace) Head() (string, error) {
	return NewGitCommandRepository(w.path).SnapshotRevision()
}
func (w *gitSnapshotWorkspace) Close() error {
	if !w.worktreeRemoved {
		_, err := runLocalGit(w.repositoryRoot, nil, "worktree", "remove", "--force", w.path)
		if err != nil {
			return fmt.Errorf("remove snapshot worktree: %w", err)
		}
		w.worktreeRemoved = true
	}
	if !w.baseRefRemoved {
		if err := (&GitCommandRepository{root: w.repositoryRoot}).deleteSnapshotRef(w.baseRevision); err != nil {
			return err
		}
		w.baseRefRemoved = true
	}
	return nil
}
func (g *GitCommandRepository) Status() ([]string, error) {
	out, err := runLocalGit(g.activeRoot(), nil, "status", "--short")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{}, nil
	}
	return lines, nil
}
func (g *GitCommandRepository) Diff() (string, error) {
	out, err := runLocalGit(g.activeRoot(), nil, "diff", "HEAD", "--no-ext-diff", "--binary")
	return string(out), err
}
func (g *GitCommandRepository) Commit(message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("commit message is required")
	}
	if _, err := runLocalGit(g.activeRoot(), nil, "add", "--all"); err != nil {
		return "", err
	}
	_, err := runLocalGit(g.activeRoot(), nil, "commit", "-m", message)
	if err != nil {
		return "", err
	}
	head, err := runLocalGit(g.activeRoot(), nil, "rev-parse", "HEAD")
	return strings.TrimSpace(string(head)), err
}
func (g *GitCommandRepository) Branches() ([]domain.GitBranch, error) {
	out, err := runLocalGit(g.activeRoot(), nil, "for-each-ref", "--format=%(refname:short)%09%(objectname)%09%(HEAD)", "refs/heads")
	if err != nil {
		return nil, err
	}
	var branches []domain.GitBranch
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) >= 2 {
			branches = append(branches, domain.GitBranch{Name: fields[0], Commit: fields[1], Current: len(fields) > 2 && fields[2] == "*"})
		}
	}
	return branches, nil
}

func (g *GitCommandRepository) CreateBranch(ctx context.Context, name string) (domain.GitBranch, error) {
	if err := validateLocalBranchName(ctx, g.activeRoot(), name); err != nil {
		return domain.GitBranch{}, err
	}
	if err := requireCleanGitWorkingTree(ctx, g.activeRoot()); err != nil {
		return domain.GitBranch{}, err
	}
	if _, err := runLocalGitContext(ctx, g.activeRoot(), nil, "switch", "--create", name); err != nil {
		if exists, inspectErr := localBranchExists(ctx, g.activeRoot(), name); inspectErr == nil && exists {
			return domain.GitBranch{}, domain.ErrGitBranchExists
		}
		return domain.GitBranch{}, err
	}
	return currentGitBranch(ctx, g.activeRoot(), name)
}

func (g *GitCommandRepository) CheckoutBranch(ctx context.Context, name string) (domain.GitBranch, error) {
	if err := validateLocalBranchName(ctx, g.activeRoot(), name); err != nil {
		return domain.GitBranch{}, err
	}
	if err := requireCleanGitWorkingTree(ctx, g.activeRoot()); err != nil {
		return domain.GitBranch{}, err
	}
	exists, err := localBranchExists(ctx, g.activeRoot(), name)
	if err != nil {
		return domain.GitBranch{}, err
	}
	if !exists {
		return domain.GitBranch{}, domain.ErrGitBranchNotFound
	}
	if _, err := runLocalGitContext(ctx, g.activeRoot(), nil, "switch", name); err != nil {
		return domain.GitBranch{}, err
	}
	return currentGitBranch(ctx, g.activeRoot(), name)
}

func validateLocalBranchName(ctx context.Context, root, name string) error {
	if name == "" || strings.TrimSpace(name) != name {
		return domain.ErrInvalidGitBranch
	}
	_, err := runLocalGitContext(ctx, root, nil, "check-ref-format", "--branch", name)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return err
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return domain.ErrInvalidGitBranch
	}
	return err
}

func requireCleanGitWorkingTree(ctx context.Context, root string) error {
	status, err := runLocalGitContext(ctx, root, nil, "status", "--porcelain=v1", "-z")
	if err != nil {
		return err
	}
	if len(status) != 0 {
		return domain.ErrGitWorktreeDirty
	}
	return nil
}

func localBranchExists(ctx context.Context, root, name string) (bool, error) {
	_, err := runLocalGitContext(ctx, root, nil, "show-ref", "--quiet", "--verify", "refs/heads/"+name)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false, err
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return false, nil
	}
	return false, err
}

func currentGitBranch(ctx context.Context, root, name string) (domain.GitBranch, error) {
	commit, err := runLocalGitContext(ctx, root, nil, "rev-parse", "HEAD")
	if err != nil {
		return domain.GitBranch{}, err
	}
	return domain.GitBranch{Name: name, Commit: strings.TrimSpace(string(commit)), Current: true}, nil
}

func (g *GitCommandRepository) History(limit int) ([]domain.GitCommit, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	format := "%H%x09%an%x09%aI%x09%s"
	out, err := runLocalGit(g.activeRoot(), nil, "log", "-n", fmt.Sprint(limit), "--format="+format)
	if err != nil {
		return nil, err
	}
	var commits []domain.GitCommit
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) == 4 {
			commits = append(commits, domain.GitCommit{Hash: fields[0], Author: fields[1], Date: fields[2], Message: fields[3]})
		}
	}
	return commits, nil
}

type MemoryDeliveryStore struct {
	mu        sync.RWMutex
	snapshots map[string]domain.Snapshot
	builds    map[string]domain.Build
}

func NewMemoryDeliveryStore() *MemoryDeliveryStore {
	return &MemoryDeliveryStore{snapshots: map[string]domain.Snapshot{}, builds: map[string]domain.Build{}}
}
func (s *MemoryDeliveryStore) SaveSnapshot(v domain.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[v.ID] = v
	return nil
}
func (s *MemoryDeliveryStore) Snapshot(id string) (domain.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.snapshots[id]
	if !ok {
		return domain.Snapshot{}, domain.ErrUnknownDelivery
	}
	return v, nil
}
func (s *MemoryDeliveryStore) SaveBuild(v domain.Build) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.builds[v.ID] = v
	return nil
}
func (s *MemoryDeliveryStore) Build(id string) (domain.Build, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.builds[id]
	if !ok {
		return domain.Build{}, domain.ErrUnknownDelivery
	}
	return v, nil
}
func (s *MemoryDeliveryStore) Snapshots() ([]domain.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]domain.Snapshot, 0, len(s.snapshots))
	for _, value := range s.snapshots {
		values = append(values, value)
	}
	return values, nil
}
func (s *MemoryDeliveryStore) Builds() ([]domain.Build, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]domain.Build, 0, len(s.builds))
	for _, value := range s.builds {
		values = append(values, value)
	}
	return values, nil
}
