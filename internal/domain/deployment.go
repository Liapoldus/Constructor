package domain

import (
	"context"
	"errors"
)

var ErrDeploymentNotFound = errors.New("deployment not found")
var ErrActiveDeployment = errors.New("an active deployment already exists")
var ErrDeploymentNotReady = errors.New("deployment requires a ready snapshot and successful build")
var ErrDeploymentConfirmationRequired = errors.New("deployment target confirmation is required")
var ErrDeploymentConflict = errors.New("deployment id is already used by another request")
var ErrDeploymentInProgress = errors.New("deployment operation is already in progress")
var ErrGatewayRevisionConflict = errors.New("Gateway release revision changed outside Constructor")
var ErrGatewayBaselineConfirmationRequired = errors.New("existing Gateway release revision must be explicitly confirmed")

type Environment struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Domain string `json:"domain,omitempty"`
}
type Site struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
}
type DeploymentStatus string

const (
	DeploymentPending    DeploymentStatus = "pending"
	DeploymentApplying   DeploymentStatus = "applying"
	DeploymentActive     DeploymentStatus = "active"
	DeploymentFailed     DeploymentStatus = "failed"
	DeploymentSuperseded DeploymentStatus = "superseded"
)

type Deployment struct {
	ID                      string           `json:"id"`
	SiteID                  string           `json:"siteId"`
	EnvironmentID           string           `json:"environmentId"`
	SnapshotID              string           `json:"snapshotId"`
	BuildID                 string           `json:"buildId"`
	Status                  DeploymentStatus `json:"status"`
	PreviousID              string           `json:"previousId,omitempty"`
	Action                  string           `json:"action,omitempty"`
	GatewayRevision         string           `json:"gatewayRevision,omitempty"`
	ExpectedGatewayRevision *string          `json:"-"`
}
type DeploymentRepository interface {
	SaveSite(Site) error
	Site(string) (Site, error)
	SaveEnvironment(Environment) error
	Environment(string) (Environment, error)
	SaveDeployment(Deployment) error
	StartDeployment(Deployment) error
	Deployment(string) (Deployment, error)
	ActiveDeployment(siteID, environmentID string) (Deployment, error)
	PromoteDeployment(deployment Deployment, expectedActiveID string) error
}
type GatewayClient interface {
	CurrentRevision(context.Context, string) (*string, error)
	Publish(context.Context, Deployment, Build, *string) (string, error)
	Rollback(context.Context, Deployment, Build, *string) (string, error)
}
type DeploymentLister interface{ Deployments() ([]Deployment, error) }
