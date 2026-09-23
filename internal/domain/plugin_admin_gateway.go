package domain

import "context"

type PluginAdminResponse struct {
	StatusCode  int
	ContentType string
	ETag        string
	RequestID   string
	RetryAfter  string
	Body        []byte
}

type PluginAdminGateway interface {
	Plugins(context.Context) (PluginAdminResponse, error)
	Surface(context.Context, string) (PluginAdminResponse, error)
	Query(context.Context, string, string, string, []byte) (PluginAdminResponse, error)
	Action(context.Context, string, string, string, string, string, string, []byte) (PluginAdminResponse, error)
	Health(context.Context, string, string) (PluginAdminResponse, error)
}
