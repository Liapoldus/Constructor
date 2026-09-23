package domain

import "context"

type Principal struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}
type Authorizer interface {
	Principal(ctx context.Context) (Principal, error)
	Allows(principal Principal, permission string) bool
}
