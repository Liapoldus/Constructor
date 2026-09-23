package application

import (
	"context"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type RepositoryService struct{ manager domain.RepositoryManager }

func NewRepositoryService(manager domain.RepositoryManager) *RepositoryService {
	return &RepositoryService{manager: manager}
}
func (s *RepositoryService) Clone(ctx context.Context, url, target string) (string, error) {
	return s.manager.Clone(ctx, url, target)
}
func (s *RepositoryService) Worktree(ctx context.Context, repository, commit, target string) (string, error) {
	return s.manager.Worktree(ctx, repository, commit, target)
}
