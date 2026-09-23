package application

import (
	"context"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type GitService struct{ repository domain.GitRepository }

func NewGitService(repository domain.GitRepository) *GitService {
	return &GitService{repository: repository}
}
func (s *GitService) Status() ([]string, error)             { return s.repository.Status() }
func (s *GitService) Diff() (string, error)                 { return s.repository.Diff() }
func (s *GitService) Commit(message string) (string, error) { return s.repository.Commit(message) }
func (s *GitService) Branches() ([]domain.GitBranch, error) { return s.repository.Branches() }
func (s *GitService) History(limit int) ([]domain.GitCommit, error) {
	return s.repository.History(limit)
}
func (s *GitService) CreateBranch(ctx context.Context, name string) (domain.GitBranch, error) {
	return s.repository.CreateBranch(ctx, name)
}
func (s *GitService) CheckoutBranch(ctx context.Context, name string) (domain.GitBranch, error) {
	return s.repository.CheckoutBranch(ctx, name)
}
