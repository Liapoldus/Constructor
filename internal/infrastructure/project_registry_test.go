package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Liapoldus/Constructor/internal/domain"
)

func TestScaffoldSdkSnapshotMatchesProjectFixture(t *testing.T) {
	fixtureRoot := filepath.Join("..", "..", "project-fixture", "src", "vendor", "liapoldus", "react")
	files := []struct {
		path string
		data []byte
	}{
		{path: "index.ts", data: scaffoldReactSDK},
		{path: "package.json", data: scaffoldReactSDKPackage},
		{path: "LICENSE", data: scaffoldReactSDKLicense},
	}
	for _, file := range files {
		current, err := os.ReadFile(filepath.Join(fixtureRoot, file.path))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(current, file.data) {
			t.Fatalf("scaffold SDK snapshot differs from project fixture: %s", file.path)
		}
	}
}

func TestScaffoldSdkSnapshotMatchesReactLibraryCheckoutWhenPresent(t *testing.T) {
	sdkRoot := filepath.Join("..", "..", "..", "react-lib")
	if _, err := os.Stat(sdkRoot); os.IsNotExist(err) {
		t.Skip("sibling react-lib checkout is not present")
	} else if err != nil {
		t.Fatal(err)
	}
	files := []struct {
		path string
		data []byte
	}{
		{path: "src/index.ts", data: scaffoldReactSDK},
		{path: "package.json", data: scaffoldReactSDKPackage},
		{path: "LICENSE", data: scaffoldReactSDKLicense},
	}
	for _, file := range files {
		current, err := os.ReadFile(filepath.Join(sdkRoot, file.path))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(current, file.data) {
			t.Fatalf("scaffold SDK snapshot is stale: %s differs from react-lib", file.path)
		}
	}
}

func TestFilesystemProjectRegistryCreateListGet(t *testing.T) {
	root := t.TempDir()
	registry := NewFilesystemProjectRegistry(root)
	created, err := registry.Create(context.Background(), "demo", "Demo project")
	if err != nil {
		t.Fatal(err)
	}
	strayRoot := filepath.Join(root, "test-worktree")
	if err := os.MkdirAll(filepath.Join(strayRoot, "liapoldus"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(strayRoot, "liapoldus", "project.json"), []byte(`{"schemaVersion":1,"id":"demo","name":"duplicate test fixture"}`), 0644); err != nil {
		t.Fatal(err)
	}
	project, err := os.ReadFile(filepath.Join(created.Path, "liapoldus", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Pages []string `json:"pages"`
	}
	if err := json.Unmarshal(project, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Pages) != 1 || manifest.Pages[0] != "home" {
		t.Fatalf("manifest page IDs must match the Site document: %#v", manifest.Pages)
	}
	siteSource, err := os.ReadFile(filepath.Join(created.Path, "liapoldus", "sites", "local-site.json"))
	if err != nil {
		t.Fatal(err)
	}
	var site struct {
		Pages []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(siteSource, &site); err != nil {
		t.Fatal(err)
	}
	if len(site.Pages) != 1 || site.Pages[0].ID != manifest.Pages[0] || site.Pages[0].Name != "Home" {
		t.Fatalf("scaffold Site page must separate stable ID and display name: %#v", site.Pages)
	}
	if created.ID != "demo" || created.Name != "Demo project" {
		t.Fatalf("unexpected project: %#v", created)
	}
	if _, err := os.Stat(filepath.Join(root, "demo", "liapoldus", "project.json")); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"package.json", "index.html", "vite.config.ts", "tsconfig.json", "src/app.tsx", "src/pages/home.page.tsx", "src/components/hero/component.tsx", "src/components/hero/schema.json", "src/generated/routes.tsx", "src/generated/content/ru-RU.json", "src/vendor/liapoldus/react/index.ts", "src/vendor/liapoldus/react/package.json", "src/vendor/liapoldus/react/LICENSE", "liapoldus/sites/local-site.json", "liapoldus/content/local-site/ru-RU.json"} {
		if _, err := os.Stat(filepath.Join(root, "demo", relative)); err != nil {
			t.Fatalf("missing scaffold %s: %v", relative, err)
		}
	}
	packageSource, err := os.ReadFile(filepath.Join(created.Path, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(packageSource), `"dev": "vite"`) || !strings.Contains(string(packageSource), `"build": "tsc --noEmit && vite build"`) {
		t.Fatalf("scaffold scripts are missing dev/build commands: %s", packageSource)
	}
	appSource, err := os.ReadFile(filepath.Join(created.Path, "src", "app.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(appSource), "PreviewRuntimeProvider") || !strings.Contains(string(appSource), "initialContent") {
		t.Fatalf("scaffold entrypoint does not install the preview content provider: %s", appSource)
	}
	pageSource, err := os.ReadFile(filepath.Join(created.Path, "src", "pages", "home.page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pageSource), "usePreviewContent") || !strings.Contains(string(pageSource), "PreviewInstance") || !strings.Contains(string(pageSource), "instanceId='hero-main'") {
		t.Fatalf("scaffold page must bind its runtime content to the selection overlay: %s", pageSource)
	}
	opened, err := registry.Get("demo")
	if err != nil {
		t.Fatal(err)
	}
	if opened != created {
		t.Fatalf("opened %#v differs from created %#v", opened, created)
	}
	if revision, err := exec.Command("git", "-C", created.Path, "rev-parse", "HEAD").Output(); err != nil || len(strings.TrimSpace(string(revision))) != 40 {
		t.Fatalf("new project missing initial git commit: %q %v", revision, err)
	}
	projects, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0] != created {
		t.Fatalf("unexpected projects: %#v", projects)
	}
	if _, err := registry.Create(context.Background(), "../escape", "bad"); err == nil {
		t.Fatal("expected path validation error")
	}
}

func TestFilesystemProjectRegistryRejectsSymlinkedProjectMetadata(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	manifest := []byte(`{"schemaVersion":1,"id":"linked","name":"Linked","pages":["home"],"components":[],"react":{"entry":"src/main.tsx"}}`)
	if err := os.MkdirAll(filepath.Join(outside, "liapoldus"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "liapoldus", "project.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	registry := NewFilesystemProjectRegistry(root)
	if _, err := registry.Get("linked"); !errors.Is(err, domain.ErrInvalidPath) {
		t.Fatalf("symlink project root error=%v, want invalid path", err)
	}
	projects, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("registry listed symlinked project: %#v", projects)
	}

	metadataRoot := filepath.Join(root, "metadata-link")
	if err := os.Mkdir(metadataRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "liapoldus"), filepath.Join(metadataRoot, "liapoldus")); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Get("metadata-link"); !errors.Is(err, domain.ErrInvalidPath) {
		t.Fatalf("symlink metadata error=%v, want invalid path", err)
	}
	if projects, err := registry.List(); err != nil || len(projects) != 0 {
		t.Fatalf("registry listed symlink metadata: projects=%#v err=%v", projects, err)
	}
}

func TestFilesystemProjectRegistryCreateFailureLeavesNoPartialProject(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	registry := NewFilesystemProjectRegistry(filepath.Join(root, "projects"))
	if _, err := registry.Create(context.Background(), "failed", "Incomplete"); err == nil {
		t.Fatal("project creation unexpectedly succeeded")
	}
	projects, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("failed create became discoverable: %#v", projects)
	}
	entries, err := os.ReadDir(filepath.Join(root, "projects"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed create left workspace entries behind: %#v", entries)
	}
}

func TestFilesystemProjectRegistryCreateCancellationCleansStaging(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "projects")
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	started := filepath.Join(root, "git-started")
	childEnv := filepath.Join(root, "git-env")
	script := "#!/bin/sh\nprintf started > '" + started + "'\nprintf '%s\\n' \"${CONSTRUCTOR_SECRET_SENTINEL-unset}\" > '" + childEnv + "'\n/bin/sleep 30 &\nwait\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("CONSTRUCTOR_SECRET_SENTINEL", "must-not-leak")
	registry := NewFilesystemProjectRegistry(workspace)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := registry.Create(ctx, "cancelled", "Cancelled"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Create error=%v, want context deadline", err)
	}
	if _, err := os.Stat(started); err != nil {
		t.Fatalf("fake Git did not start: %v", err)
	}
	if data, err := os.ReadFile(childEnv); err != nil || strings.TrimSpace(string(data)) != "unset" {
		t.Fatalf("project-init Git environment was not allowlisted: value=%q err=%v", data, err)
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cancelled create left workspace entries: %#v", entries)
	}
}
