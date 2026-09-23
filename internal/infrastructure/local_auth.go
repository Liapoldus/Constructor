package infrastructure

import (
	"context"
	"github.com/Liapoldus/Constructor/internal/domain"
)

type LocalAuthorizer struct{}

func (LocalAuthorizer) Principal(context.Context) (domain.Principal, error) {
	return domain.Principal{ID: "local-admin", Name: "admin", Permissions: []string{"*"}}, nil
}
func (LocalAuthorizer) Allows(principal domain.Principal, permission string) bool {
	for _, candidate := range principal.Permissions {
		if candidate == "*" || candidate == permission {
			return true
		}
	}
	return false
}
