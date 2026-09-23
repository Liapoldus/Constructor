package infrastructure

import (
	"context"
	"errors"
	"github.com/Liapoldus/Constructor/internal/domain"
)

type UnavailableGatewayClient struct{}

func (UnavailableGatewayClient) CurrentRevision(context.Context, string) (*string, error) {
	return nil, errors.New("Gateway is not configured; release status is unavailable")
}
func (UnavailableGatewayClient) Publish(context.Context, domain.Deployment, domain.Build, *string) (string, error) {
	return "", errors.New("Gateway is not configured; deployment was not applied")
}
func (UnavailableGatewayClient) Rollback(context.Context, domain.Deployment, domain.Build, *string) (string, error) {
	return "", errors.New("Gateway is not configured; rollback was not applied")
}
