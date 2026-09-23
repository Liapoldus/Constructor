package application

import "github.com/Liapoldus/Constructor/internal/domain"

type RBACService struct{ repository domain.RBACRepository }

func NewRBACService(repository domain.RBACRepository) *RBACService {
	return &RBACService{repository: repository}
}
func (s *RBACService) Users() ([]domain.User, error)             { return s.repository.Users() }
func (s *RBACService) Roles() ([]domain.Role, error)             { return s.repository.Roles() }
func (s *RBACService) SaveUser(value domain.User) error          { return s.repository.SaveUser(value) }
func (s *RBACService) SaveRole(value domain.Role) error          { return s.repository.SaveRole(value) }
func (s *RBACService) Permissions() ([]domain.Permission, error) { return s.repository.Permissions() }
func (s *RBACService) SavePermission(value domain.Permission) error {
	return s.repository.SavePermission(value)
}
func (s *RBACService) AssignRole(value domain.UserRole) error { return s.repository.AssignRole(value) }
func (s *RBACService) GrantPermission(value domain.RolePermission) error {
	return s.repository.GrantPermission(value)
}
