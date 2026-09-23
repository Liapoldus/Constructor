package application

import (
	"context"
	"regexp"

	"github.com/Liapoldus/Constructor/internal/domain"
)

const pluginAdminRequestLimit = 4 << 20

var pluginAdminIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var pluginAdminDigestPattern = regexp.MustCompile(`^(?:sha256:)?[a-fA-F0-9]{64}$`)
var pluginAdminIdempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{15,127}$`)

type PluginAdminService struct {
	gateway domain.PluginAdminGateway
}

func NewPluginAdminService(gateway domain.PluginAdminGateway) *PluginAdminService {
	return &PluginAdminService{gateway: gateway}
}

func (s *PluginAdminService) Plugins(ctx context.Context) (domain.PluginAdminResponse, error) {
	return s.forward(func(gateway domain.PluginAdminGateway) (domain.PluginAdminResponse, error) {
		return gateway.Plugins(ctx)
	})
}

func (s *PluginAdminService) Surface(ctx context.Context, instance string) (domain.PluginAdminResponse, error) {
	if !pluginAdminIDPattern.MatchString(instance) {
		return domain.PluginAdminResponse{}, domain.ErrPluginAdminInvalidRequest
	}
	return s.forward(func(gateway domain.PluginAdminGateway) (domain.PluginAdminResponse, error) {
		return gateway.Surface(ctx, instance)
	})
}

func (s *PluginAdminService) Query(ctx context.Context, instance, page, digest string, body []byte) (domain.PluginAdminResponse, error) {
	if !pluginAdminIDPattern.MatchString(instance) || !pluginAdminIDPattern.MatchString(page) || !pluginAdminDigestPattern.MatchString(digest) || len(body) == 0 || len(body) > pluginAdminRequestLimit {
		return domain.PluginAdminResponse{}, domain.ErrPluginAdminInvalidRequest
	}
	return s.forward(func(gateway domain.PluginAdminGateway) (domain.PluginAdminResponse, error) {
		return gateway.Query(ctx, instance, page, digest, body)
	})
}

func (s *PluginAdminService) Action(ctx context.Context, instance, page, action, digest, idempotencyKey, confirmation string, body []byte) (domain.PluginAdminResponse, error) {
	if !pluginAdminIDPattern.MatchString(instance) || !pluginAdminIDPattern.MatchString(page) || !pluginAdminIDPattern.MatchString(action) || !pluginAdminDigestPattern.MatchString(digest) || !pluginAdminIdempotencyPattern.MatchString(idempotencyKey) || len(body) == 0 || len(body) > pluginAdminRequestLimit || len(confirmation) > 4096 {
		return domain.PluginAdminResponse{}, domain.ErrPluginAdminInvalidRequest
	}
	return s.forward(func(gateway domain.PluginAdminGateway) (domain.PluginAdminResponse, error) {
		return gateway.Action(ctx, instance, page, action, digest, idempotencyKey, confirmation, body)
	})
}

func (s *PluginAdminService) Health(ctx context.Context, instance, page string) (domain.PluginAdminResponse, error) {
	if !pluginAdminIDPattern.MatchString(instance) || !pluginAdminIDPattern.MatchString(page) {
		return domain.PluginAdminResponse{}, domain.ErrPluginAdminInvalidRequest
	}
	return s.forward(func(gateway domain.PluginAdminGateway) (domain.PluginAdminResponse, error) {
		return gateway.Health(ctx, instance, page)
	})
}

func (s *PluginAdminService) forward(call func(domain.PluginAdminGateway) (domain.PluginAdminResponse, error)) (domain.PluginAdminResponse, error) {
	if s.gateway == nil {
		return domain.PluginAdminResponse{}, domain.ErrPluginAdminGatewayUnavailable
	}
	return call(s.gateway)
}
