package application

import (
	"context"
	"github.com/Liapoldus/Constructor/internal/domain"
)

type AuthService struct{ authorizer domain.Authorizer }

func NewAuthService(authorizer domain.Authorizer) *AuthService {
	return &AuthService{authorizer: authorizer}
}
func (s *AuthService) Session(ctx context.Context) (domain.Principal, error) {
	return s.authorizer.Principal(ctx)
}
func (s *AuthService) Authorize(ctx context.Context, permission string) error {
	principal, err := s.authorizer.Principal(ctx)
	if err != nil {
		return err
	}
	if !s.authorizer.Allows(principal, permission) {
		return domain.ErrForbidden
	}
	return nil
}
