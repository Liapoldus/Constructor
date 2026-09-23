package domain

type User struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	System bool   `json:"system"`
}
type Role struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	System bool   `json:"system"`
}
type Permission struct {
	Key string `json:"key"`
}
type RolePermission struct {
	RoleID        string `json:"roleId"`
	PermissionKey string `json:"permissionKey"`
}
type UserRole struct {
	UserID string `json:"userId"`
	RoleID string `json:"roleId"`
}
type RBACRepository interface {
	Users() ([]User, error)
	Roles() ([]Role, error)
	SaveUser(User) error
	SaveRole(Role) error
	Permissions() ([]Permission, error)
	SavePermission(Permission) error
	AssignRole(UserRole) error
	GrantPermission(RolePermission) error
}
