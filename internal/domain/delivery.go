package domain

import (
	"context"
	"errors"
)

var ErrSnapshotNotReady = errors.New("snapshot is not ready")
var ErrSnapshotSourceChanged = errors.New("project files changed during snapshot capture")
var ErrUnknownDelivery = errors.New("delivery record not found")

type SnapshotStatus string

const (
	SnapshotDraft      SnapshotStatus = "draft"
	SnapshotValidating SnapshotStatus = "validating"
	SnapshotReady      SnapshotStatus = "ready"
	SnapshotFailed     SnapshotStatus = "failed"
)

type BuildStatus string

const (
	BuildQueued    BuildStatus = "queued"
	BuildRunning   BuildStatus = "running"
	BuildSucceeded BuildStatus = "succeeded"
	BuildFailed    BuildStatus = "failed"
)

type Snapshot struct {
	ID             string         `json:"id"`
	ProjectID      string         `json:"projectId,omitempty"`
	RepositoryPath string         `json:"-"`
	SiteID         string         `json:"siteId"`
	Locale         string         `json:"locale"`
	GitCommit      string         `json:"gitCommit"`
	ContentDigest  string         `json:"contentDigest"`
	Status         SnapshotStatus `json:"status"`
}
type Build struct {
	ID               string      `json:"id"`
	SnapshotID       string      `json:"snapshotId"`
	Status           BuildStatus `json:"status"`
	ArtifactPath     string      `json:"artifactPath,omitempty"`
	ArtifactChecksum string      `json:"artifactChecksum,omitempty"`
	Error            string      `json:"error,omitempty"`
}

type BuildArtifact struct {
	Path     string
	Checksum string
}

type SnapshotWorkspace interface {
	Root() string
	Head() (string, error)
	Close() error
}

type SnapshotWorkspaceFactory interface {
	CreateSnapshotWorkspace() (SnapshotWorkspace, error)
}

type SnapshotWorkspaceFactoryAtRoot interface {
	CreateSnapshotWorkspaceAt(root string) (SnapshotWorkspace, error)
}
type GitBranch struct {
	Name    string `json:"name"`
	Commit  string `json:"commit"`
	Current bool   `json:"current"`
}
type GitCommit struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}
type GitRepository interface {
	Head() (string, error)
	Status() ([]string, error)
	Diff() (string, error)
	Commit(message string) (string, error)
	Branches() ([]GitBranch, error)
	History(limit int) ([]GitCommit, error)
	CreateBranch(context.Context, string) (GitBranch, error)
	CheckoutBranch(context.Context, string) (GitBranch, error)
}
type DeliveryRepository interface {
	SaveSnapshot(Snapshot) error
	Snapshot(string) (Snapshot, error)
	SaveBuild(Build) error
	Build(string) (Build, error)
	Snapshots() ([]Snapshot, error)
	Builds() ([]Build, error)
}
