package domain

import (
	"context"
	"time"
)

type GatewayGroup struct {
	ID               string  `json:"id"`
	Kind             string  `json:"kind"`
	Active           bool    `json:"active"`
	CurrentRevision  *string `json:"currentRevision"`
	PreviousRevision *string `json:"previousRevision"`
	State            string  `json:"state"`
}

type GatewayGroupList struct {
	Items     []GatewayGroup `json:"items"`
	RequestID string         `json:"requestId"`
}

type GatewayGroupRevisionSummary struct {
	ID              string    `json:"id"`
	GroupID         string    `json:"groupId"`
	CaddyfileDigest string    `json:"caddyfileDigest"`
	ArtifactDigest  *string   `json:"artifactDigest"`
	CreatedAt       time.Time `json:"createdAt"`
	Actor           string    `json:"actor,omitempty"`
}

type GatewayGroupRevisionList struct {
	Items      []GatewayGroupRevisionSummary `json:"items"`
	NextCursor *string                       `json:"nextCursor"`
	RequestID  string                        `json:"requestId"`
}

type GatewayGroupReader interface {
	ListGroups(context.Context) (GatewayGroupList, error)
	ListReleases(context.Context, string, string, int) (GatewayGroupRevisionList, error)
}
