package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type sqliteMigration struct {
	version int
	apply   func(*sql.Tx) error
}

var sqliteDeliveryMigrations = []sqliteMigration{
	{version: 1, apply: createSQLiteBaseSchema},
	{version: 2, apply: addSnapshotScopeColumns},
	{version: 3, apply: addBuildChecksumColumn},
	{version: 4, apply: addDeploymentRevisionColumns},
	{version: 5, apply: addSQLiteIndexesAndSystemAdmin},
}

func migrateSQLiteDeliveryStore(db *sql.DB) error {
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS constructor_schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create migration history: %w", err)
	}

	var current int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM constructor_schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read migration history: %w", err)
	}
	if latest := sqliteDeliveryMigrations[len(sqliteDeliveryMigrations)-1].version; current > latest {
		return fmt.Errorf("database schema version %d is newer than supported version %d", current, latest)
	}

	for _, migration := range sqliteDeliveryMigrations {
		if migration.version <= current {
			continue
		}
		if err := applySQLiteMigration(ctx, db, migration); err != nil {
			return fmt.Errorf("apply database migration %d: %w", migration.version, err)
		}
	}
	return nil
}

func applySQLiteMigration(ctx context.Context, db *sql.DB, migration sqliteMigration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := migration.apply(tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO constructor_schema_migrations(version) VALUES(?)`, migration.version); err != nil {
		return err
	}
	return tx.Commit()
}

func createSQLiteBaseSchema(tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS snapshots (
			id TEXT PRIMARY KEY,
			site_id TEXT NOT NULL,
			git_commit TEXT NOT NULL,
			content_digest TEXT NOT NULL,
			status TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS builds (
			id TEXT PRIMARY KEY,
			snapshot_id TEXT NOT NULL REFERENCES snapshots(id),
			status TEXT NOT NULL,
			artifact_path TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS sites (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			name TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS environments (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			domain TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS deployments (
			id TEXT PRIMARY KEY,
			site_id TEXT NOT NULL,
			environment_id TEXT NOT NULL,
			snapshot_id TEXT NOT NULL,
			build_id TEXT NOT NULL,
			status TEXT NOT NULL,
			previous_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			system INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS roles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			system INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS permissions (key TEXT PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS user_roles (
			user_id TEXT NOT NULL REFERENCES users(id),
			role_id TEXT NOT NULL REFERENCES roles(id),
			PRIMARY KEY(user_id, role_id)
		)`,
		`CREATE TABLE IF NOT EXISTS role_permissions (
			role_id TEXT NOT NULL REFERENCES roles(id),
			permission_key TEXT NOT NULL REFERENCES permissions(key),
			PRIMARY KEY(role_id, permission_key)
		)`,
	}
	return executeSQLiteStatements(tx, statements)
}

func addSnapshotScopeColumns(tx *sql.Tx) error {
	for _, column := range []string{"locale", "project_id", "repository_path"} {
		if err := ensureSQLiteColumn(tx, "snapshots", column); err != nil {
			return err
		}
	}
	return nil
}

func addBuildChecksumColumn(tx *sql.Tx) error {
	return ensureSQLiteColumn(tx, "builds", "artifact_checksum")
}

func addDeploymentRevisionColumns(tx *sql.Tx) error {
	for _, column := range []string{"action", "gateway_revision", "expected_gateway_revision"} {
		if err := ensureSQLiteColumn(tx, "deployments", column); err != nil {
			return err
		}
	}
	return nil
}

func addSQLiteIndexesAndSystemAdmin(tx *sql.Tx) error {
	statements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS one_active_deployment_per_target
			ON deployments(site_id, environment_id) WHERE status='active'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS one_deployment_operation_per_target
			ON deployments(site_id, environment_id) WHERE status IN ('pending','applying')`,
		`INSERT OR IGNORE INTO users(id, name, system) VALUES('local-admin', 'admin', 1)`,
		`INSERT OR IGNORE INTO roles(id, name, system) VALUES('admin', 'admin', 1)`,
	}
	return executeSQLiteStatements(tx, statements)
}

func ensureSQLiteColumn(tx *sql.Tx, table, column string) error {
	definition, ok := sqliteColumnDefinitions[table+"."+column]
	if !ok {
		return errors.New("unsupported database column")
	}
	rows, err := tx.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	exists := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		if name == column {
			exists = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = tx.Exec(definition)
	return err
}

var sqliteColumnDefinitions = map[string]string{
	"snapshots.locale":                      `ALTER TABLE snapshots ADD COLUMN locale TEXT NOT NULL DEFAULT ''`,
	"snapshots.project_id":                  `ALTER TABLE snapshots ADD COLUMN project_id TEXT NOT NULL DEFAULT ''`,
	"snapshots.repository_path":             `ALTER TABLE snapshots ADD COLUMN repository_path TEXT NOT NULL DEFAULT ''`,
	"builds.artifact_checksum":              `ALTER TABLE builds ADD COLUMN artifact_checksum TEXT NOT NULL DEFAULT ''`,
	"deployments.action":                    `ALTER TABLE deployments ADD COLUMN action TEXT NOT NULL DEFAULT 'publish'`,
	"deployments.gateway_revision":          `ALTER TABLE deployments ADD COLUMN gateway_revision TEXT NOT NULL DEFAULT ''`,
	"deployments.expected_gateway_revision": `ALTER TABLE deployments ADD COLUMN expected_gateway_revision TEXT NULL`,
}

func executeSQLiteStatements(tx *sql.Tx, statements []string) error {
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
