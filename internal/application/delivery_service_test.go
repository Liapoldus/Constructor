package application

import (
	"context"
	"github.com/Liapoldus/Constructor/internal/domain"
	"testing"
)

type gitStub struct{}

func (gitStub) Head() (string, error)                   { return "abc123", nil }
func (gitStub) Status() ([]string, error)               { return nil, nil }
func (gitStub) Diff() (string, error)                   { return "", nil }
func (gitStub) Commit(string) (string, error)           { return "abc123", nil }
func (gitStub) Branches() ([]domain.GitBranch, error)   { return nil, nil }
func (gitStub) History(int) ([]domain.GitCommit, error) { return nil, nil }
func (gitStub) CreateBranch(context.Context, string) (domain.GitBranch, error) {
	return domain.GitBranch{}, nil
}
func (gitStub) CheckoutBranch(context.Context, string) (domain.GitBranch, error) {
	return domain.GitBranch{}, nil
}

type lockCheckingGit struct {
	gitStub
	projects *ProjectService
	lockHeld *bool
}

func (g lockCheckingGit) Head() (string, error) {
	if g.projects.mutation.TryLock() {
		g.projects.mutation.Unlock()
		*g.lockHeld = false
	} else {
		*g.lockHeld = true
	}
	return "snapshot-revision", nil
}

type runnerStub struct{}

func (runnerStub) Run(context.Context, domain.Snapshot) (domain.BuildArtifact, error) {
	return domain.BuildArtifact{Path: "dist/site", Checksum: "sha256:fixture"}, nil
}
func TestDeliveryRequiresValidationBeforeBuild(t *testing.T) {
	store := newMemoryStore()
	service := NewDeliveryService(gitStub{}, store, func() ([]domain.Diagnostic, error) {
		return []domain.Diagnostic{{Severity: "error", Message: "bad schema"}}, nil
	}, runnerStub{})
	if _, err := service.CreateSnapshot("site", "ru-RU"); err == nil {
		t.Fatal("expected validation error")
	}
	if len(store.snapshots) != 0 {
		t.Fatalf("invalid snapshot was persisted: %#v", store.snapshots)
	}
}
func TestDeliveryCreatesReadySnapshotAndBuild(t *testing.T) {
	store := newMemoryStore()
	service := NewDeliveryService(gitStub{}, store, func() ([]domain.Diagnostic, error) { return nil, nil }, runnerStub{})
	snapshot, err := service.CreateSnapshot("site", "ru-RU")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != domain.SnapshotReady {
		t.Fatalf("status %s", snapshot.Status)
	}
	if snapshot.Locale != "ru-RU" {
		t.Fatalf("snapshot locale = %q", snapshot.Locale)
	}
	build, err := service.BuildSnapshot(context.Background(), snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if build.Status != domain.BuildSucceeded {
		t.Fatalf("status %s", build.Status)
	}
	if build.ArtifactChecksum != "sha256:fixture" {
		t.Fatalf("artifact checksum = %q", build.ArtifactChecksum)
	}
}

func TestSnapshotIdentityIncludesLocale(t *testing.T) {
	service := NewDeliveryService(gitStub{}, newMemoryStore(), func() ([]domain.Diagnostic, error) { return nil, nil }, runnerStub{})
	first, err := service.CreateSnapshot("site", "ru-RU")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateSnapshot("site", "en-US")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.ContentDigest == second.ContentDigest {
		t.Fatalf("locale-specific snapshots collided: first=%#v second=%#v", first, second)
	}
}

func TestSnapshotIdentityIsRepeatableForSameRevisionAndScope(t *testing.T) {
	service := NewDeliveryService(gitStub{}, newMemoryStore(), func() ([]domain.Diagnostic, error) { return nil, nil }, runnerStub{})
	first, err := service.CreateSnapshot("site", "ru-RU")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateSnapshot("site", "ru-RU")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.ContentDigest != second.ContentDigest || first.GitCommit != second.GitCommit {
		t.Fatalf("identical project revision/scope produced different snapshots: first=%#v second=%#v", first, second)
	}
}

type orderedGit struct{ order *[]string }

func (g orderedGit) Head() (string, error) {
	*g.order = append(*g.order, "revision")
	return "snapshot-revision", nil
}
func (orderedGit) Status() ([]string, error)               { return nil, nil }
func (orderedGit) Diff() (string, error)                   { return "", nil }
func (orderedGit) Commit(string) (string, error)           { return "", nil }
func (orderedGit) Branches() ([]domain.GitBranch, error)   { return nil, nil }
func (orderedGit) History(int) ([]domain.GitCommit, error) { return nil, nil }
func (orderedGit) CreateBranch(context.Context, string) (domain.GitBranch, error) {
	return domain.GitBranch{}, nil
}
func (orderedGit) CheckoutBranch(context.Context, string) (domain.GitBranch, error) {
	return domain.GitBranch{}, nil
}
func TestSnapshotPreparesAndValidatesBeforeCapturingRevision(t *testing.T) {
	order := []string{}
	service := NewDeliveryService(orderedGit{&order}, newMemoryStore(), func() ([]domain.Diagnostic, error) { order = append(order, "validate"); return nil, nil }, runnerStub{})
	prepare := func() error { order = append(order, "generate"); return nil }
	if _, err := service.CreateSnapshotFor("site", "ru-RU", service.validator, prepare); err != nil {
		t.Fatal(err)
	}
	want := []string{"generate", "validate", "revision"}
	if len(order) != len(want) {
		t.Fatalf("unexpected pipeline order %#v", order)
	}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("unexpected pipeline order %#v", order)
		}
	}
}

type blockedBatchRepository struct {
	*generatorRepository
	entered chan struct{}
	release chan struct{}
}

func (r *blockedBatchRepository) ApplyBatch(changes []domain.ProjectFileChange) error {
	close(r.entered)
	<-r.release
	return r.generatorRepository.ApplyBatch(changes)
}

func TestProjectSnapshotHoldsMutationLockThroughRevisionCapture(t *testing.T) {
	files := generatorFiles([]byte(`{"schemaVersion":1,"id":"home-content","instances":[]}`))
	files["liapoldus/routes/development.json"] = []byte(`{"schemaVersion":1,"id":"development-routes","routes":[{"id":"home-route","path":"/","page":"home"}]}`)
	files["src/pages/home.page.tsx"] = []byte("export default function Home(){return null}")
	repository := &blockedBatchRepository{
		generatorRepository: &generatorRepository{files: files},
		entered:             make(chan struct{}),
		release:             make(chan struct{}),
	}
	projects := NewProjectService(repository)
	store := newMemoryStore()
	lockHeldAtRevision := false
	delivery := NewDeliveryService(lockCheckingGit{projects: projects, lockHeld: &lockHeldAtRevision}, store, nil, runnerStub{})
	result := make(chan error, 1)
	go func() {
		_, err := delivery.CreateProjectSnapshot(projects, "site-one", "ru-RU", "development")
		result <- err
	}()
	<-repository.entered
	if projects.mutation.TryLock() {
		projects.mutation.Unlock()
		close(repository.release)
		t.Fatal("project mutations were not serialized during snapshot generation")
	}
	close(repository.release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if len(store.snapshots) != 1 {
		t.Fatalf("snapshot was not persisted after revision capture: %#v", store.snapshots)
	}
	if !lockHeldAtRevision {
		t.Fatal("project mutation lock was released before Git revision capture")
	}
}

type memoryStore struct {
	snapshots map[string]domain.Snapshot
	builds    map[string]domain.Build
}

func newMemoryStore() *memoryStore {
	return &memoryStore{snapshots: map[string]domain.Snapshot{}, builds: map[string]domain.Build{}}
}
func (s *memoryStore) SaveSnapshot(v domain.Snapshot) error        { s.snapshots[v.ID] = v; return nil }
func (s *memoryStore) Snapshot(id string) (domain.Snapshot, error) { return s.snapshots[id], nil }
func (s *memoryStore) SaveBuild(v domain.Build) error              { s.builds[v.ID] = v; return nil }
func (s *memoryStore) Build(id string) (domain.Build, error)       { return s.builds[id], nil }
func (s *memoryStore) Snapshots() ([]domain.Snapshot, error) {
	values := make([]domain.Snapshot, 0, len(s.snapshots))
	for _, value := range s.snapshots {
		values = append(values, value)
	}
	return values, nil
}
func (s *memoryStore) Builds() ([]domain.Build, error) {
	values := make([]domain.Build, 0, len(s.builds))
	for _, value := range s.builds {
		values = append(values, value)
	}
	return values, nil
}
