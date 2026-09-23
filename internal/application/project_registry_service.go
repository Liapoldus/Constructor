package application

import (
	"context"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type ProjectRegistryService struct{ registry domain.ProjectRegistry }

func NewProjectRegistryService(registry domain.ProjectRegistry) *ProjectRegistryService {
	return &ProjectRegistryService{registry: registry}
}
func (s *ProjectRegistryService) List() ([]domain.ProjectRecord, error) { return s.registry.List() }
func (s *ProjectRegistryService) Get(id string) (domain.ProjectRecord, error) {
	return s.registry.Get(id)
}
func (s *ProjectRegistryService) Create(ctx context.Context, id, name string) (domain.ProjectRecord, error) {
	return s.registry.Create(ctx, id, name)
}
