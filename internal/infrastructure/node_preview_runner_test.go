package infrastructure

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Liapoldus/Constructor/internal/application"
	"github.com/Liapoldus/Constructor/internal/domain"
)

func TestNodePreviewRunnerStartsSwitchesAndStopsProjectServers(t *testing.T) {
	t.Setenv("GATEWAY_TOKEN", "do-not-pass-to-preview")
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is unavailable")
	}
	first := previewFixture(t, "First project")
	second := previewFixture(t, "Second project")
	runner := NewNodePreviewRunner()
	t.Cleanup(func() { _ = runner.Close() })

	session, err := runner.Start(first)
	if err != nil {
		t.Fatal(err)
	}
	if !session.Active || runner.Status(first).URL != session.URL {
		t.Fatalf("preview status does not match started session: %#v", session)
	}
	if decoded, err := hex.DecodeString(session.SessionID); err != nil || len(decoded) != 16 {
		t.Fatalf("preview session ID must be a random 128-bit hex value: %q", session.SessionID)
	}
	response, err := http.Get(session.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "First project") || !strings.Contains(string(body), "no-secret") {
		t.Fatalf("preview did not serve project output: status=%d err=%v body=%s", response.StatusCode, readErr, body)
	}

	secondSession, err := runner.Start(second)
	if err != nil {
		t.Fatal(err)
	}
	if runner.Status(first).Active || !runner.Status(second).Active {
		t.Fatal("switching projects did not replace the active preview")
	}
	if session.SessionID == secondSession.SessionID {
		t.Fatal("separate preview runs must use separate session IDs")
	}
	if err := runner.Stop(second); err != nil {
		t.Fatal(err)
	}
	if runner.Status(second).Active {
		t.Fatal("preview remains active after stop")
	}
	client := &http.Client{Timeout: 250 * time.Millisecond}
	if _, err := client.Get(secondSession.URL); err == nil {
		t.Fatal("preview server still accepts requests after stop")
	}
}

func TestNodePreviewRunnerServesTheReactProjectFixture(t *testing.T) {
	root := filepath.Join("..", "..", "project-fixture")
	runner := NewNodePreviewRunner()
	t.Cleanup(func() { _ = runner.Close() })
	session, err := runner.Start(root)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(session.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "/src/app.tsx") {
		t.Fatalf("preview did not serve the React fixture: status=%d err=%v body=%s", response.StatusCode, readErr, body)
	}
	if err := runner.Stop(root); err != nil {
		t.Fatal(err)
	}
}

func TestScaffoldedProjectInstallsAndServesItsPreviewRuntime(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is unavailable")
	}
	registry := NewFilesystemProjectRegistry(t.TempDir())
	project, err := registry.Create(context.Background(), "preview-site", "Preview site")
	if err != nil {
		t.Fatal(err)
	}
	repository := NewFilesystemRepository(project.Path)
	projects := application.NewProjectService(repository)
	manifestFile, err := repository.Read("liapoldus/project.json")
	if err != nil {
		t.Fatal(err)
	}
	manifest, diagnostics := domain.DecodeProjectDocument(manifestFile.Content, manifestFile.Path)
	if len(diagnostics) != 0 {
		t.Fatalf("scaffold manifest is invalid: %#v", diagnostics)
	}
	manifest.Pages = append(manifest.Pages, "about")
	manifestContent, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projects.Write(manifestFile.Path, manifestFile.Revision, append(manifestContent, '\n')); err != nil {
		t.Fatalf("adding the second manifest page failed: %v", err)
	}
	sitePath := "liapoldus/sites/local-site.json"
	siteFile, err := repository.Read(sitePath)
	if err != nil {
		t.Fatal(err)
	}
	site, diagnostics := domain.DecodeSiteDocument(siteFile.Content, sitePath)
	if len(diagnostics) != 0 {
		t.Fatalf("scaffold Site is invalid: %#v", diagnostics)
	}
	site.Pages = append(site.Pages, domain.SitePage{ID: "about", Name: "About"})
	siteContent, err := json.MarshalIndent(site, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projects.Write(sitePath, siteFile.Revision, append(siteContent, '\n')); err != nil {
		t.Fatalf("adding the second Site page failed: %v", err)
	}
	aboutSource := []byte("import {usePreviewContent} from '../vendor/liapoldus/react/index'\nexport default function AboutPage(){const title=usePreviewContent('about','hero-about','title','About');return <main><h1>{title}</h1></main>}\n")
	if err := os.WriteFile(filepath.Join(project.Path, "src", "pages", "about.page.tsx"), aboutSource, 0644); err != nil {
		t.Fatal(err)
	}
	routeService := application.NewRouteService(projects)
	routeFile, err := routeService.Read("development")
	if err != nil {
		t.Fatal(err)
	}
	var routeDocument domain.RouteDocument
	if err := json.Unmarshal(routeFile.Content, &routeDocument); err != nil {
		t.Fatal(err)
	}
	routeDocument.Routes = append(routeDocument.Routes, domain.Route{ID: "about", Path: "/about", Page: "about"})
	routeContent, err := json.MarshalIndent(routeDocument, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, routeDiagnostics, err := routeService.Save("development", routeFile.Revision, append(routeContent, '\n')); err != nil || hasDiagnosticErrors(routeDiagnostics) {
		t.Fatalf("adding the second development route failed: diagnostics=%#v err=%v", routeDiagnostics, err)
	}
	contentPath := "liapoldus/content/local-site/ru-RU.json"
	contentFile, err := repository.Read(contentPath)
	if err != nil {
		t.Fatal(err)
	}
	content, diagnostics := domain.DecodeContentDocument(contentFile.Content, contentPath)
	if len(diagnostics) != 0 {
		t.Fatalf("scaffold content is invalid: %#v", diagnostics)
	}
	content.Instances[0].Fields["title"] = "Edited title"
	content.Instances = append(content.Instances, domain.ContentInstance{ID: "hero-secondary", PageID: "home", Component: "hero", Fields: map[string]any{"title": "Second instance"}})
	content.Instances = append(content.Instances, domain.ContentInstance{ID: "hero-about", PageID: "about", Component: "hero", Fields: map[string]any{"title": "About page title"}})
	updatedContent, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projects.Write(contentPath, contentFile.Revision, append(updatedContent, '\n')); err != nil {
		t.Fatalf("editing scaffold content failed: %v", err)
	}
	diagnostics, err = projects.ValidateAllSites()
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			t.Fatalf("new scaffold is invalid: %#v", diagnostics)
		}
	}
	artifactMetadata, err := projects.GenerateLocale("local-site", "ru-RU")
	if err != nil {
		t.Fatalf("generating edited locale failed: %v", err)
	}
	generatedContent, err := os.ReadFile(filepath.Join(project.Path, filepath.FromSlash(artifactMetadata.Path)))
	if err != nil || !strings.Contains(string(generatedContent), "Edited title") || !strings.Contains(string(generatedContent), "Second instance") || !strings.Contains(string(generatedContent), "About page title") {
		t.Fatalf("generated locale does not preserve edited instances: err=%v content=%s", err, generatedContent)
	}
	if _, routeDiagnostics, routeErr := routeService.Generate("development"); routeErr != nil || hasDiagnosticErrors(routeDiagnostics) {
		t.Fatalf("route generation failed: diagnostics=%#v err=%v", routeDiagnostics, routeErr)
	}
	runner := NewNodePreviewRunner()
	t.Cleanup(func() { _ = runner.Close() })
	session, err := runner.Start(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"", "src/vendor/liapoldus/react/index.ts", "src/pages/home.page.tsx", "src/pages/about.page.tsx"} {
		targetURL := session.URL
		if route != "" {
			baseURL := strings.SplitN(session.URL, "?", 2)[0]
			targetURL = strings.TrimRight(baseURL, "/") + "/" + route
		}
		request, requestErr := http.NewRequest(http.MethodGet, targetURL, nil)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		if route != "" {
			request.Header.Set("Referer", session.URL)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("scaffold preview route %q failed: status=%d err=%v body=%s", route, response.StatusCode, readErr, body)
		}
		if strings.HasPrefix(route, "src/pages/") && !strings.Contains(string(body), "usePreviewContent") {
			t.Fatalf("scaffold page was not transformed with draft content binding: %s", body)
		}
	}
	if err := runner.Stop(project.Path); err != nil {
		t.Fatal(err)
	}
	revision, err := NewGitCommandRepository(project.Path).SnapshotRevision()
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := NewNodeBuildRunner(project.Path).Run(context.Background(), domain.Snapshot{GitCommit: revision})
	if err != nil {
		t.Fatalf("scaffold project build failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(artifact.Path) })
	if len(artifact.Checksum) != 64 {
		t.Fatalf("artifact checksum is not a SHA-256 hex digest: %q", artifact.Checksum)
	}
	if _, err := os.Stat(filepath.Join(artifact.Path, "index.html")); err != nil {
		t.Fatalf("scaffold build did not produce a static site: %v", err)
	}
	assets, err := os.ReadDir(filepath.Join(artifact.Path, "assets"))
	if err != nil {
		t.Fatalf("scaffold build did not produce assets: %v", err)
	}
	var bundle strings.Builder
	for _, asset := range assets {
		if asset.IsDir() {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(artifact.Path, "assets", asset.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		bundle.Write(content)
	}
	if !strings.Contains(bundle.String(), "Edited title") || !strings.Contains(bundle.String(), "Second instance") || !strings.Contains(bundle.String(), "About page title") || !strings.Contains(bundle.String(), "/about") {
		t.Fatalf("static build omitted edited content: %s", bundle.String())
	}
}

func TestProjectSnapshotBuildsImmutableGeneratedRevision(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is unavailable")
	}
	registry := NewFilesystemProjectRegistry(t.TempDir())
	project, err := registry.Create(context.Background(), "immutable-site", "Immutable site")
	if err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", project.Path}, args...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, output, err)
		}
		return strings.TrimSpace(string(output))
	}
	activeHead := runGit("rev-parse", "HEAD")
	activeStatus := runGit("status", "--short")
	workspace := NewProjectWorkspace(project.Path)
	projects := application.NewProjectService(NewFilesystemRepositoryWithWorkspace(workspace))
	store := NewMemoryDeliveryStore()
	delivery := application.NewDeliveryService(NewGitCommandRepositoryWithWorkspace(workspace), store, nil, NewNodeBuildRunnerWithWorkspace(workspace))
	snapshot, err := delivery.CreateProjectSnapshot(projects, "local-site", "ru-RU", "development")
	if err != nil {
		t.Fatal(err)
	}
	if got := runGit("rev-parse", "HEAD"); got != activeHead {
		t.Fatalf("snapshot changed active branch HEAD: before=%s after=%s", activeHead, got)
	}
	if got := runGit("status", "--short"); got != activeStatus {
		t.Fatalf("snapshot changed active checkout status: before=%q after=%q", activeStatus, got)
	}
	if snapshot.ProjectID != "immutable-site" || snapshot.RepositoryPath != project.Path {
		t.Fatalf("snapshot identity was not pinned: %#v", snapshot)
	}
	for _, relative := range []string{"src/generated/content/ru-RU.json", "src/generated/routes.tsx", "src/generated/theme.css", "src/generated/assets.ts"} {
		if _, err := exec.Command("git", "-C", project.Path, "cat-file", "-e", snapshot.GitCommit+":"+relative).CombinedOutput(); err != nil {
			t.Fatalf("immutable snapshot omitted %s: %v", relative, err)
		}
	}
	if err := os.WriteFile(filepath.Join(project.Path, "src", "generated", "content", "ru-RU.json"), []byte(`{"pages":{"home":{"instances":{"hero-main":{"component":"hero","fields":{"title":"changed after snapshot"}}}}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	otherProject, err := registry.Create(context.Background(), "second-site", "Second site")
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.Activate(otherProject.Path); err != nil {
		t.Fatal(err)
	}
	build, err := delivery.BuildSnapshot(context.Background(), snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if build.Status != domain.BuildSucceeded || len(build.ArtifactChecksum) != 64 {
		t.Fatalf("immutable snapshot build result=%#v", build)
	}
	t.Cleanup(func() { _ = os.RemoveAll(build.ArtifactPath) })
	if _, err := os.Stat(filepath.Join(build.ArtifactPath, "index.html")); err != nil {
		t.Fatalf("immutable snapshot did not produce a static site: %v", err)
	}
	assetFiles, err := os.ReadDir(filepath.Join(build.ArtifactPath, "assets"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle strings.Builder
	for _, asset := range assetFiles {
		content, err := os.ReadFile(filepath.Join(build.ArtifactPath, "assets", asset.Name()))
		if err != nil {
			t.Fatal(err)
		}
		bundle.Write(content)
	}
	if !strings.Contains(bundle.String(), "Immutable site") || strings.Contains(bundle.String(), "changed after snapshot") {
		t.Fatalf("build did not isolate the pinned snapshot revision: %s", bundle.String())
	}
	if got := runGit("worktree", "list", "--porcelain"); strings.Count(got, "worktree ") != 1 {
		t.Fatalf("snapshot build left a temporary worktree registered: %s", got)
	}
}

func hasDiagnosticErrors(diagnostics []domain.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			return true
		}
	}
	return false
}

func previewFixture(t *testing.T, title string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "liapoldus"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "liapoldus", "project.json"), []byte(`{"schemaVersion":1,"id":"preview-test","name":"Preview test","pages":[],"components":[],"react":{"entry":"src/app.tsx"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	packageJSON := `{"private":true,"scripts":{"dev":"node server.mjs"}}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(packageJSON), 0644); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`import {createServer} from 'node:http'; const port=Number(process.argv[process.argv.indexOf('--port')+1]); createServer((request,response)=>{response.writeHead(200,{'content-type':'text/html'});response.end('<h1>%s</h1><small>'+ (process.env.GATEWAY_TOKEN||'no-secret') +'</small>')}).listen(port,'127.0.0.1')`, title)
	if err := os.WriteFile(filepath.Join(root, "server.mjs"), []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}
