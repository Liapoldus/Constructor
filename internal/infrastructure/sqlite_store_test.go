package infrastructure

import (
	"database/sql"
	"fmt"
	"github.com/Liapoldus/Constructor/internal/domain"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSQLiteDeliveryStorePersistsSnapshotAndBuild(t *testing.T) {
	store, err := NewSQLiteDeliveryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot := domain.Snapshot{ID: "s1", ProjectID: "project-1", RepositoryPath: "/projects/project-1", SiteID: "site", Locale: "ru-RU", GitCommit: "abc", ContentDigest: "digest", Status: domain.SnapshotReady}
	if err := store.SaveSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Snapshot("s1")
	if err != nil || loaded != snapshot {
		t.Fatalf("snapshot=%+v err=%v", loaded, err)
	}
	build := domain.Build{ID: "b1", SnapshotID: "s1", Status: domain.BuildSucceeded, ArtifactPath: "dist", ArtifactChecksum: "sha256:fixture"}
	if err := store.SaveBuild(build); err != nil {
		t.Fatal(err)
	}
	loadedBuild, err := store.Build("b1")
	if err != nil || loadedBuild != build {
		t.Fatalf("build=%+v err=%v", loadedBuild, err)
	}
}

func TestSQLiteDeliveryStoreMigratesLegacySnapshotLocale(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE snapshots (id TEXT PRIMARY KEY, site_id TEXT NOT NULL, git_commit TEXT NOT NULL, content_digest TEXT NOT NULL, status TEXT NOT NULL); CREATE TABLE builds (id TEXT PRIMARY KEY, snapshot_id TEXT NOT NULL, status TEXT NOT NULL, artifact_path TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT ''); CREATE TABLE deployments (id TEXT PRIMARY KEY,site_id TEXT NOT NULL,environment_id TEXT NOT NULL,snapshot_id TEXT NOT NULL,build_id TEXT NOT NULL,status TEXT NOT NULL,previous_id TEXT NOT NULL DEFAULT ''); INSERT INTO snapshots VALUES('legacy','site','abc','digest','ready'); INSERT INTO builds VALUES('legacy-build','legacy','succeeded','old-dist',''); INSERT INTO deployments VALUES('legacy-deploy','site','production','legacy','legacy-build','active','')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteDeliveryStore(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	legacy, err := store.Snapshot("legacy")
	if err != nil || legacy.Locale != "" {
		t.Fatalf("legacy snapshot=%#v err=%v", legacy, err)
	}
	legacyBuild, err := store.Build("legacy-build")
	if err != nil || legacyBuild.ArtifactChecksum != "" || legacyBuild.ArtifactPath != "old-dist" {
		t.Fatalf("legacy build=%#v err=%v", legacyBuild, err)
	}
	legacyDeployment, err := store.Deployment("legacy-deploy")
	if err != nil || legacyDeployment.Action != "publish" {
		t.Fatalf("legacy deployment=%#v err=%v", legacyDeployment, err)
	}
	current := domain.Snapshot{ID: "current", SiteID: "site", Locale: "en-US", GitCommit: "def", ContentDigest: "new-digest", Status: domain.SnapshotReady}
	if err := store.SaveSnapshot(current); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Snapshot(current.ID)
	if err != nil || loaded != current {
		t.Fatalf("migrated store snapshot=%#v err=%v", loaded, err)
	}
	var migrationCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM constructor_schema_migrations`).Scan(&migrationCount); err != nil || migrationCount != len(sqliteDeliveryMigrations) {
		t.Fatalf("migration history count=%d err=%v", migrationCount, err)
	}
}

func TestSQLiteDeliveryMigrationsAreIdempotentAcrossReopen(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "migrations.db")
	for attempt := 0; attempt < 2; attempt++ {
		store, err := NewSQLiteDeliveryStore(databasePath)
		if err != nil {
			t.Fatalf("open attempt %d: %v", attempt+1, err)
		}
		var maxVersion int
		if err := store.db.QueryRow(`SELECT MAX(version) FROM constructor_schema_migrations`).Scan(&maxVersion); err != nil {
			t.Fatal(err)
		}
		if want := sqliteDeliveryMigrations[len(sqliteDeliveryMigrations)-1].version; maxVersion != want {
			t.Fatalf("schema version=%d want %d", maxVersion, want)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSQLiteDeliveryStoreRejectsNewerSchemaVersion(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "newer.db")
	store, err := NewSQLiteDeliveryStore(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	futureVersion := sqliteDeliveryMigrations[len(sqliteDeliveryMigrations)-1].version + 1
	if _, err := db.Exec(`INSERT INTO constructor_schema_migrations(version) VALUES(?)`, futureVersion); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := NewSQLiteDeliveryStore(databasePath); err == nil {
		_ = reopened.Close()
		t.Fatal("opened database with an unsupported future schema version")
	} else if !strings.Contains(err.Error(), "newer than supported") {
		t.Fatalf("unsupported schema error=%v", err)
	}
	db, err = sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var adminCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE id='local-admin'`).Scan(&adminCount); err != nil || adminCount != 1 {
		t.Fatalf("existing data changed after refused open: adminCount=%d err=%v", adminCount, err)
	}
}

func TestSQLiteDeliveryMigrationRollsBackSchemaAndVersionTogether(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "migration-rollback.db")
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE deployments (
		id TEXT PRIMARY KEY, site_id TEXT NOT NULL, environment_id TEXT NOT NULL,
		snapshot_id TEXT NOT NULL, build_id TEXT NOT NULL, status TEXT NOT NULL,
		previous_id TEXT NOT NULL DEFAULT ''
	);
	INSERT INTO deployments(id,site_id,environment_id,snapshot_id,build_id,status)
	VALUES('active-a','site','prod','s1','b1','active'),('active-b','site','prod','s2','b2','active')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if store, err := NewSQLiteDeliveryStore(databasePath); err == nil {
		_ = store.Close()
		t.Fatal("migration accepted duplicate active deployment rows")
	}
	db, err = sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow(`SELECT MAX(version) FROM constructor_schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 4 {
		t.Fatalf("failed migration was recorded at version %d; want 4", version)
	}
	var indexCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='one_active_deployment_per_target'`).Scan(&indexCount); err != nil {
		t.Fatal(err)
	}
	if indexCount != 0 {
		t.Fatal("failed migration left a partial unique index")
	}
}

func TestSQLiteDeploymentPromotionIsCompareAndSwapAndAtomic(t *testing.T) {
	store, err := NewSQLiteDeliveryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first := domain.Deployment{ID: "deploy-first", SiteID: "site", EnvironmentID: "production", SnapshotID: "s1", BuildID: "b1", Status: domain.DeploymentApplying, Action: "publish"}
	if err := store.SaveDeployment(first); err != nil {
		t.Fatal(err)
	}
	first.Status = domain.DeploymentActive
	if err := store.PromoteDeployment(first, ""); err != nil {
		t.Fatal(err)
	}
	second := domain.Deployment{ID: "deploy-second", SiteID: "site", EnvironmentID: "production", SnapshotID: "s2", BuildID: "b2", Status: domain.DeploymentApplying, Action: "publish"}
	if err := store.SaveDeployment(second); err != nil {
		t.Fatal(err)
	}
	second.Status = domain.DeploymentActive
	if err := store.PromoteDeployment(second, "stale-active-id"); err != domain.ErrActiveDeployment {
		t.Fatalf("stale compare-and-swap error=%v", err)
	}
	active, err := store.ActiveDeployment("site", "production")
	if err != nil || active.ID != first.ID {
		t.Fatalf("failed compare-and-swap changed active deployment: %#v err=%v", active, err)
	}
	if err := store.PromoteDeployment(second, first.ID); err != nil {
		t.Fatal(err)
	}
	active, err = store.ActiveDeployment("site", "production")
	if err != nil || active.ID != second.ID {
		t.Fatalf("second deployment was not activated: %#v err=%v", active, err)
	}
	previous, err := store.Deployment(first.ID)
	if err != nil || previous.Status != domain.DeploymentSuperseded {
		t.Fatalf("previous deployment was not atomically superseded: %#v err=%v", previous, err)
	}
}

func TestSQLiteDeploymentStartReservesOneOperationPerTargetAndAllowsFailedRetry(t *testing.T) {
	store, err := NewSQLiteDeliveryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	expectedRevision := strings.Repeat("b", 64)
	first := domain.Deployment{ID: "deploy-first", SiteID: "site", EnvironmentID: "production", SnapshotID: "s1", BuildID: "b1", Status: domain.DeploymentPending, Action: "publish", GatewayRevision: strings.Repeat("a", 64), ExpectedGatewayRevision: &expectedRevision}
	if err := store.StartDeployment(first); err != nil {
		t.Fatal(err)
	}
	concurrent := domain.Deployment{ID: "deploy-second", SiteID: "site", EnvironmentID: "production", SnapshotID: "s2", BuildID: "b2", Status: domain.DeploymentPending, Action: "publish"}
	if err := store.StartDeployment(concurrent); err != domain.ErrDeploymentInProgress {
		t.Fatalf("concurrent target reservation error=%v", err)
	}
	first.Status = domain.DeploymentFailed
	if err := store.SaveDeployment(first); err != nil {
		t.Fatal(err)
	}
	if err := store.StartDeployment(first); err != nil {
		t.Fatalf("failed idempotent retry was not reservable: %v", err)
	}
	if err := store.StartDeployment(concurrent); err != domain.ErrDeploymentInProgress {
		t.Fatalf("target lock was lost during retry: %v", err)
	}
	first.Status = domain.DeploymentApplying
	if err := store.SaveDeployment(first); err != nil {
		t.Fatal(err)
	}
	if err := store.PromoteDeployment(domain.Deployment{ID: first.ID, SiteID: first.SiteID, EnvironmentID: first.EnvironmentID, Status: domain.DeploymentActive, GatewayRevision: first.GatewayRevision}, ""); err != nil {
		t.Fatal(err)
	}
	active, err := store.ActiveDeployment(first.SiteID, first.EnvironmentID)
	if err != nil || active.GatewayRevision != first.GatewayRevision || !sameOptionalString(active.ExpectedGatewayRevision, first.ExpectedGatewayRevision) {
		t.Fatalf("persisted Gateway revisions=%q/%v err=%v", active.GatewayRevision, active.ExpectedGatewayRevision, err)
	}
	if err := store.StartDeployment(concurrent); err != nil {
		t.Fatalf("completed target reservation was not released: %v", err)
	}
}

func TestSQLiteDeploymentStartSerializesConcurrentTargetReservations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.db")
	first, err := NewSQLiteDeliveryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewSQLiteDeliveryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	stores := []*SQLiteDeliveryStore{first, second}
	const callers = 16
	var wait sync.WaitGroup
	errorsFound := make(chan error, callers)
	started := make(chan int, callers)
	for index := 0; index < callers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			value := domain.Deployment{ID: fmt.Sprintf("deployment-%08d", index), SiteID: "site", EnvironmentID: "production", SnapshotID: "snapshot", BuildID: "build", Status: domain.DeploymentPending, Action: "publish"}
			err := stores[index%len(stores)].StartDeployment(value)
			if err == nil {
				started <- index
			} else if err != domain.ErrDeploymentInProgress {
				errorsFound <- err
			}
		}(index)
	}
	wait.Wait()
	close(started)
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("unexpected reservation error: %v", err)
	}
	var successes []int
	for index := range started {
		successes = append(successes, index)
	}
	if len(successes) != 1 {
		t.Fatalf("concurrent requests reserved %d operations for one target: %v", len(successes), successes)
	}
	values, err := first.Deployments()
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Status != domain.DeploymentPending {
		t.Fatalf("unexpected persisted reservations: %#v", values)
	}
}

func TestSQLitePreservesInFlightDeploymentCASRevisionAcrossStoreRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.db")
	store, err := NewSQLiteDeliveryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.Repeat("e", 64)
	value := domain.Deployment{ID: "deployment-recovery-0001", SiteID: "site", EnvironmentID: "production", SnapshotID: "snapshot", BuildID: "build", Status: domain.DeploymentApplying, Action: "publish", ExpectedGatewayRevision: &expected}
	if err := store.StartDeployment(value); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDeployment(value); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewSQLiteDeliveryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := reopened.Deployment(value.ID)
	if err != nil || recovered.Status != domain.DeploymentApplying || !sameOptionalString(recovered.ExpectedGatewayRevision, &expected) {
		t.Fatalf("restart lost in-flight CAS state: %#v err=%v", recovered, err)
	}
}
