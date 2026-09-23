package domain

import (
	"context"
	"errors"
)

var ErrInvalidRepositoryURL = errors.New("repository URL is invalid or unsupported")
var ErrInvalidCommit = errors.New("commit must be a full Git object ID")
var ErrRepositoryTargetExists = errors.New("repository target already exists")
var ErrInvalidGitBranch = errors.New("Git branch name is invalid")
var ErrGitBranchExists = errors.New("Git branch already exists")
var ErrGitBranchNotFound = errors.New("Git branch does not exist")
var ErrGitWorktreeDirty = errors.New("Git working tree must be clean for branch operations")

type RepositoryManager interface {
	Clone(ctx context.Context, url, target string) (string, error)
	Worktree(ctx context.Context, repository, commit, target string) (string, error)
}
