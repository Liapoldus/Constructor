package application

import (
	"context"
	"regexp"

	"github.com/Liapoldus/Constructor/internal/domain"
)

var gatewayGroupCatalogIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

type GatewayGroupCatalogService struct {
	reader domain.GatewayGroupReader
}

func NewGatewayGroupCatalogService(reader domain.GatewayGroupReader) *GatewayGroupCatalogService {
	return &GatewayGroupCatalogService{reader: reader}
}

func (s *GatewayGroupCatalogService) ListGroups(ctx context.Context) (domain.GatewayGroupList, error) {
	if s == nil || s.reader == nil {
		return domain.GatewayGroupList{}, domain.ErrGatewayGroupsUnavailable
	}
	return s.reader.ListGroups(ctx)
}

func (s *GatewayGroupCatalogService) ListReleases(ctx context.Context, groupID, cursor string, limit int) (domain.GatewayGroupRevisionList, error) {
	if s == nil || s.reader == nil {
		return domain.GatewayGroupRevisionList{}, domain.ErrGatewayGroupsUnavailable
	}
	if !gatewayGroupCatalogIDPattern.MatchString(groupID) || limit < 1 || limit > 100 || len(cursor) > 4096 {
		return domain.GatewayGroupRevisionList{}, domain.ErrGatewayGroupQueryInvalid
	}
	return s.reader.ListReleases(ctx, groupID, cursor, limit)
}
