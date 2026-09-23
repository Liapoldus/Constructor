package application

import (
	"github.com/Liapoldus/Constructor/internal/domain"
	"strings"
	"testing"
)

type routeFixtureRepository struct{ files map[string][]byte }

func (r *routeFixtureRepository) Manifest() (domain.Project, string, error) {
	var project domain.Project
	project.SchemaVersion = 1
	project.ID = "demo"
	project.Name = "Demo"
	project.Pages = []string{"home", "product", "missing"}
	project.React.Entry = "src/app.tsx"
	return project, "manifest", nil
}
func (r *routeFixtureRepository) Read(path string) (domain.File, error) {
	content, ok := r.files[path]
	if !ok {
		return domain.File{}, domain.ErrNotFound
	}
	return domain.File{Path: path, Revision: "rev", Content: content}, nil
}
func (r *routeFixtureRepository) Write(path, revision string, content []byte) (domain.File, error) {
	if current, ok := r.files[path]; ok && revision != "rev" {
		return domain.File{}, &domain.ConflictError{Path: path, ExpectedRevision: revision, CurrentRevision: "rev", Current: current, Candidate: content}
	}
	r.files[path] = content
	return domain.File{Path: path, Revision: "rev", Content: content}, nil
}

func TestRouteServiceGeneratesSortedRouterArtifact(t *testing.T) {
	repository := &routeFixtureRepository{files: map[string][]byte{
		"liapoldus/routes/development.json": []byte(`{"schemaVersion":1,"id":"main-routes","routes":[{"id":"product","path":"/product/:id","page":"product","chunk":"lazy","lazy":true,"layouts":["shell"],"access":"member","preload":["home"],"metadata":{"title":"Product"}},{"id":"home","path":"/","page":"home","chunk":"same"}]}`),
		"src/pages/home.page.tsx":           []byte("export default function Home() { return null }"),
		"src/pages/product.page.tsx":        []byte("export default function Product() { return null }"),
		"src/layouts/shell.layout.tsx":      []byte("export default function Shell({children}: {children: React.ReactNode}) { return <div>{children}</div> }"),
	}}
	service := NewRouteService(NewProjectService(repository))
	artifact, _, err := service.Generate("development")
	if err != nil {
		t.Fatal(err)
	}
	source := string(artifact.Content)
	if strings.Index(source, "path: \"/\"") > strings.Index(source, "path: \"/product/:id\"") {
		t.Fatalf("exact routes were not ordered before parameter routes:\n%s", source)
	}
	for _, expected := range []string{"createRoutes", "lazy: async", "../layouts/shell.layout", "withLayouts", "withAccess", "Access denied", "layout", "access", "preload", "metadata"} {
		if !strings.Contains(source, expected) {
			t.Fatalf("generated routes missing %q:\n%s", expected, source)
		}
	}
}

func TestLegacyLayoutWrapsOrderedLayouts(t *testing.T) {
	route := domain.Route{Layout: "legacy-shell", Layouts: []string{"section", "page"}}
	want := []string{"legacy-shell", "section", "page"}
	got := routeLayouts(route)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("routeLayouts() = %v, want outer-to-inner chain %v", got, want)
	}
}
