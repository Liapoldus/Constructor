package infrastructure

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Liapoldus/Constructor/internal/domain"
	sqlite3 "github.com/mattn/go-sqlite3"
)

type SQLiteDeliveryStore struct{ db *sql.DB }

func NewSQLiteDeliveryStore(path string) (*SQLiteDeliveryStore, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err = migrateSQLiteDeliveryStore(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteDeliveryStore{db: db}, nil
}
func (s *SQLiteDeliveryStore) Close() error { return s.db.Close() }
func (s *SQLiteDeliveryStore) SaveSnapshot(v domain.Snapshot) error {
	_, err := s.db.Exec(`INSERT INTO snapshots(id,project_id,repository_path,site_id,locale,git_commit,content_digest,status) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status`, v.ID, v.ProjectID, v.RepositoryPath, v.SiteID, v.Locale, v.GitCommit, v.ContentDigest, v.Status)
	return err
}
func (s *SQLiteDeliveryStore) Snapshot(id string) (domain.Snapshot, error) {
	var v domain.Snapshot
	err := s.db.QueryRow(`SELECT id,project_id,repository_path,site_id,locale,git_commit,content_digest,status FROM snapshots WHERE id=?`, id).Scan(&v.ID, &v.ProjectID, &v.RepositoryPath, &v.SiteID, &v.Locale, &v.GitCommit, &v.ContentDigest, &v.Status)
	if err == sql.ErrNoRows {
		return v, domain.ErrUnknownDelivery
	}
	return v, err
}
func (s *SQLiteDeliveryStore) Deployments() ([]domain.Deployment, error) {
	rows, err := s.db.Query(`SELECT id,site_id,environment_id,snapshot_id,build_id,status,previous_id,action,gateway_revision,expected_gateway_revision FROM deployments ORDER BY rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []domain.Deployment
	for rows.Next() {
		value, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

type deploymentRowScanner interface{ Scan(...any) error }

func scanDeployment(row deploymentRowScanner) (domain.Deployment, error) {
	var value domain.Deployment
	var expected sql.NullString
	err := row.Scan(&value.ID, &value.SiteID, &value.EnvironmentID, &value.SnapshotID, &value.BuildID, &value.Status, &value.PreviousID, &value.Action, &value.GatewayRevision, &expected)
	if expected.Valid {
		value.ExpectedGatewayRevision = &expected.String
	}
	return value, err
}
func (s *SQLiteDeliveryStore) SaveBuild(v domain.Build) error {
	_, err := s.db.Exec(`INSERT INTO builds(id,snapshot_id,status,artifact_path,artifact_checksum,error) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,artifact_path=excluded.artifact_path,artifact_checksum=excluded.artifact_checksum,error=excluded.error`, v.ID, v.SnapshotID, v.Status, v.ArtifactPath, v.ArtifactChecksum, v.Error)
	return err
}
func (s *SQLiteDeliveryStore) Build(id string) (domain.Build, error) {
	var v domain.Build
	err := s.db.QueryRow(`SELECT id,snapshot_id,status,artifact_path,artifact_checksum,error FROM builds WHERE id=?`, id).Scan(&v.ID, &v.SnapshotID, &v.Status, &v.ArtifactPath, &v.ArtifactChecksum, &v.Error)
	if err == sql.ErrNoRows {
		return v, domain.ErrUnknownDelivery
	}
	return v, err
}
func (s *SQLiteDeliveryStore) Snapshots() ([]domain.Snapshot, error) {
	rows, err := s.db.Query(`SELECT id,project_id,repository_path,site_id,locale,git_commit,content_digest,status FROM snapshots ORDER BY rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []domain.Snapshot
	for rows.Next() {
		var value domain.Snapshot
		if err := rows.Scan(&value.ID, &value.ProjectID, &value.RepositoryPath, &value.SiteID, &value.Locale, &value.GitCommit, &value.ContentDigest, &value.Status); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func (s *SQLiteDeliveryStore) Builds() ([]domain.Build, error) {
	rows, err := s.db.Query(`SELECT id,snapshot_id,status,artifact_path,artifact_checksum,error FROM builds ORDER BY rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []domain.Build
	for rows.Next() {
		var value domain.Build
		if err := rows.Scan(&value.ID, &value.SnapshotID, &value.Status, &value.ArtifactPath, &value.ArtifactChecksum, &value.Error); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func (s *SQLiteDeliveryStore) SaveSite(v domain.Site) error {
	_, err := s.db.Exec(`INSERT INTO sites(id,project_id,name) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET project_id=excluded.project_id,name=excluded.name`, v.ID, v.ProjectID, v.Name)
	return err
}
func (s *SQLiteDeliveryStore) Site(id string) (domain.Site, error) {
	var v domain.Site
	err := s.db.QueryRow(`SELECT id,project_id,name FROM sites WHERE id=?`, id).Scan(&v.ID, &v.ProjectID, &v.Name)
	if err == sql.ErrNoRows {
		return v, domain.ErrDeploymentNotFound
	}
	return v, err
}
func (s *SQLiteDeliveryStore) SaveEnvironment(v domain.Environment) error {
	_, err := s.db.Exec(`INSERT INTO environments(id,name,kind,domain) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,kind=excluded.kind,domain=excluded.domain`, v.ID, v.Name, v.Kind, v.Domain)
	return err
}
func (s *SQLiteDeliveryStore) Environment(id string) (domain.Environment, error) {
	var v domain.Environment
	err := s.db.QueryRow(`SELECT id,name,kind,domain FROM environments WHERE id=?`, id).Scan(&v.ID, &v.Name, &v.Kind, &v.Domain)
	if err == sql.ErrNoRows {
		return v, domain.ErrDeploymentNotFound
	}
	return v, err
}
func (s *SQLiteDeliveryStore) SaveDeployment(v domain.Deployment) error {
	_, err := s.db.Exec(`INSERT INTO deployments(id,site_id,environment_id,snapshot_id,build_id,status,previous_id,action,gateway_revision,expected_gateway_revision) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,previous_id=excluded.previous_id,action=excluded.action,gateway_revision=excluded.gateway_revision,expected_gateway_revision=excluded.expected_gateway_revision`, v.ID, v.SiteID, v.EnvironmentID, v.SnapshotID, v.BuildID, v.Status, v.PreviousID, v.Action, v.GatewayRevision, v.ExpectedGatewayRevision)
	return err
}

func (s *SQLiteDeliveryStore) StartDeployment(value domain.Deployment) error {
	result, err := s.db.Exec(`INSERT INTO deployments(id,site_id,environment_id,snapshot_id,build_id,status,previous_id,action,gateway_revision,expected_gateway_revision) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,previous_id=excluded.previous_id,action=excluded.action,gateway_revision=excluded.gateway_revision,expected_gateway_revision=excluded.expected_gateway_revision WHERE deployments.status=? AND deployments.site_id=excluded.site_id AND deployments.environment_id=excluded.environment_id AND deployments.snapshot_id=excluded.snapshot_id AND deployments.build_id=excluded.build_id AND deployments.action=excluded.action AND deployments.expected_gateway_revision IS excluded.expected_gateway_revision`, value.ID, value.SiteID, value.EnvironmentID, value.SnapshotID, value.BuildID, domain.DeploymentPending, value.PreviousID, value.Action, value.GatewayRevision, value.ExpectedGatewayRevision, domain.DeploymentFailed)
	if err != nil {
		var sqliteError sqlite3.Error
		if errors.As(err, &sqliteError) && (sqliteError.Code == sqlite3.ErrConstraint || sqliteError.Code == sqlite3.ErrBusy || sqliteError.Code == sqlite3.ErrLocked) {
			return domain.ErrDeploymentInProgress
		}
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 1 {
		return nil
	}
	existing, err := s.Deployment(value.ID)
	if err != nil {
		return err
	}
	if existing.SiteID != value.SiteID || existing.EnvironmentID != value.EnvironmentID || existing.SnapshotID != value.SnapshotID || existing.BuildID != value.BuildID || existing.Action != value.Action || !sameOptionalString(existing.ExpectedGatewayRevision, value.ExpectedGatewayRevision) {
		return domain.ErrDeploymentConflict
	}
	return domain.ErrDeploymentInProgress
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
func (s *SQLiteDeliveryStore) Deployment(id string) (domain.Deployment, error) {
	var v domain.Deployment
	v, err := scanDeployment(s.db.QueryRow(`SELECT id,site_id,environment_id,snapshot_id,build_id,status,previous_id,action,gateway_revision,expected_gateway_revision FROM deployments WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return v, domain.ErrDeploymentNotFound
	}
	return v, err
}
func (s *SQLiteDeliveryStore) ActiveDeployment(siteID, environmentID string) (domain.Deployment, error) {
	v, err := scanDeployment(s.db.QueryRow(`SELECT id,site_id,environment_id,snapshot_id,build_id,status,previous_id,action,gateway_revision,expected_gateway_revision FROM deployments WHERE site_id=? AND environment_id=? AND status=? ORDER BY id DESC LIMIT 1`, siteID, environmentID, domain.DeploymentActive))
	if err == sql.ErrNoRows {
		return v, domain.ErrDeploymentNotFound
	}
	return v, err
}

func (s *SQLiteDeliveryStore) PromoteDeployment(value domain.Deployment, expectedActiveID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		if isSQLiteBusy(err) {
			return domain.ErrActiveDeployment
		}
		return err
	}
	defer tx.Rollback()
	var activeID string
	err = tx.QueryRow(`SELECT id FROM deployments WHERE site_id=? AND environment_id=? AND status=?`, value.SiteID, value.EnvironmentID, domain.DeploymentActive).Scan(&activeID)
	if err != nil && err != sql.ErrNoRows {
		if isSQLiteBusy(err) {
			return domain.ErrActiveDeployment
		}
		return err
	}
	if err == sql.ErrNoRows {
		activeID = ""
	}
	if activeID != expectedActiveID {
		return domain.ErrActiveDeployment
	}
	if expectedActiveID != "" {
		if _, err := tx.Exec(`UPDATE deployments SET status=? WHERE id=? AND status=?`, domain.DeploymentSuperseded, expectedActiveID, domain.DeploymentActive); err != nil {
			if isSQLiteBusy(err) {
				return domain.ErrActiveDeployment
			}
			return err
		}
	}
	result, err := tx.Exec(`UPDATE deployments SET status=?,gateway_revision=? WHERE id=? AND site_id=? AND environment_id=? AND status=?`, domain.DeploymentActive, value.GatewayRevision, value.ID, value.SiteID, value.EnvironmentID, domain.DeploymentApplying)
	if err != nil {
		if isSQLiteBusy(err) {
			return domain.ErrActiveDeployment
		}
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return domain.ErrDeploymentNotFound
	}
	if err := tx.Commit(); err != nil {
		if isSQLiteBusy(err) {
			return domain.ErrActiveDeployment
		}
		return err
	}
	return nil
}

func isSQLiteBusy(err error) bool {
	var sqliteError sqlite3.Error
	return errors.As(err, &sqliteError) && (sqliteError.Code == sqlite3.ErrBusy || sqliteError.Code == sqlite3.ErrLocked)
}
func (s *SQLiteDeliveryStore) Users() ([]domain.User, error) {
	rows, err := s.db.Query(`SELECT id,name,system FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []domain.User
	for rows.Next() {
		var value domain.User
		var system int
		if err := rows.Scan(&value.ID, &value.Name, &system); err != nil {
			return nil, err
		}
		value.System = system == 1
		values = append(values, value)
	}
	return values, rows.Err()
}
func (s *SQLiteDeliveryStore) Roles() ([]domain.Role, error) {
	rows, err := s.db.Query(`SELECT id,name,system FROM roles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []domain.Role
	for rows.Next() {
		var value domain.Role
		var system int
		if err := rows.Scan(&value.ID, &value.Name, &system); err != nil {
			return nil, err
		}
		value.System = system == 1
		values = append(values, value)
	}
	return values, rows.Err()
}
func (s *SQLiteDeliveryStore) SaveUser(value domain.User) error {
	_, err := s.db.Exec(`INSERT INTO users(id,name,system) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name`, value.ID, value.Name, boolInt(value.System))
	return err
}
func (s *SQLiteDeliveryStore) SaveRole(value domain.Role) error {
	_, err := s.db.Exec(`INSERT INTO roles(id,name,system) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name`, value.ID, value.Name, boolInt(value.System))
	return err
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func (s *SQLiteDeliveryStore) Principal(context.Context) (domain.Principal, error) {
	var value domain.Principal
	var system int
	err := s.db.QueryRow(`SELECT id,name,system FROM users WHERE id='local-admin'`).Scan(&value.ID, &value.Name, &system)
	if err != nil {
		return value, err
	}
	if system == 1 {
		value.Permissions = []string{"*"}
		return value, nil
	}
	rows, err := s.db.Query(`SELECT rp.permission_key FROM role_permissions rp JOIN user_roles ur ON ur.role_id=rp.role_id WHERE ur.user_id=?`, value.ID)
	if err != nil {
		return value, err
	}
	defer rows.Close()
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return value, err
		}
		value.Permissions = append(value.Permissions, permission)
	}
	return value, rows.Err()
}
func (s *SQLiteDeliveryStore) Allows(principal domain.Principal, permission string) bool {
	for _, candidate := range principal.Permissions {
		if candidate == "*" || candidate == permission {
			return true
		}
	}
	return false
}
func (s *SQLiteDeliveryStore) Permissions() ([]domain.Permission, error) {
	rows, err := s.db.Query(`SELECT key FROM permissions ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []domain.Permission
	for rows.Next() {
		var value domain.Permission
		if err := rows.Scan(&value.Key); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func (s *SQLiteDeliveryStore) SavePermission(value domain.Permission) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO permissions(key) VALUES(?)`, value.Key)
	return err
}
func (s *SQLiteDeliveryStore) AssignRole(value domain.UserRole) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO user_roles(user_id,role_id) VALUES(?,?)`, value.UserID, value.RoleID)
	return err
}
func (s *SQLiteDeliveryStore) GrantPermission(value domain.RolePermission) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO role_permissions(role_id,permission_key) VALUES(?,?)`, value.RoleID, value.PermissionKey)
	return err
}
