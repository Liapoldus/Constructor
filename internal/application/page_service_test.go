package application

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type pageRepository struct {
	files map[string][]byte
}

func (r *pageRepository) Manifest() (domain.Project, string, error) {
	file, err := r.Read("liapoldus/project.json")
	if err != nil {
		return domain.Project{}, "", err
	}
	project, diagnostics := domain.DecodeProjectDocument(file.Content, file.Path)
	if hasErrors(diagnostics) {
		return domain.Project{}, "", domain.StructuredValidationError{Diagnostics: diagnostics}
	}
	return project, file.Revision, nil
}

func (r *pageRepository) Read(path string) (domain.File, error) {
	content, ok := r.files[path]
	if !ok {
		return domain.File{}, domain.ErrNotFound
	}
	sum := sha256.Sum256(content)
	return domain.File{Path: path, Revision: hex.EncodeToString(sum[:]), Content: content}, nil
}

func (r *pageRepository) Write(path, expectedRevision string, content []byte) (domain.File, error) {
	file, err := r.Read(path)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.File{}, err
	}
	if err == nil && file.Revision != expectedRevision {
		return domain.File{}, &domain.ConflictError{Path: path, ExpectedRevision: expectedRevision, CurrentRevision: file.Revision, Current: file.Content, Candidate: content}
	}
	r.files[path] = append([]byte(nil), content...)
	return r.Read(path)
}

func (r *pageRepository) ApplyBatch(changes []domain.ProjectFileChange) error {
	for _, change := range changes {
		file, err := r.Read(change.Path)
		if err == nil && file.Revision != change.ExpectedRevision || errors.Is(err, domain.ErrNotFound) && change.ExpectedRevision != "" {
			currentRevision, current := "", []byte(nil)
			if err == nil {
				currentRevision, current = file.Revision, file.Content
			}
			return &domain.ConflictError{Path: change.Path, ExpectedRevision: change.ExpectedRevision, CurrentRevision: currentRevision, Current: current, Candidate: change.Content}
		}
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	for _, change := range changes {
		if change.Delete {
			delete(r.files, change.Path)
		} else {
			r.files[change.Path] = append([]byte(nil), change.Content...)
		}
	}
	return nil
}

func pageTestRevision(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func TestCreatePageUpdatesManifestSiteRouteAndSourceAsOneBatch(t *testing.T) {
	project := []byte(`{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":[],"react":{"entry":"src/app.tsx"}}`)
	site := []byte(`{"schemaVersion":1,"id":"site-a","projectId":"demo","name":"Site A","pages":[{"id":"home","name":"Home"}],"locales":["ru-RU"]}`)
	routes := []byte(`{"schemaVersion":1,"id":"development-routes","routes":[{"id":"home","path":"/","page":"home"}]}`)
	repository := &pageRepository{files: map[string][]byte{
		"liapoldus/project.json":            project,
		"liapoldus/sites/site-a.json":       site,
		"liapoldus/routes/development.json": routes,
	}}
	service := NewProjectService(repository)
	page, err := service.CreatePage(CreatePageRequest{
		SiteID: "site-a", ID: "about", Name: "About us", RoutePath: "/about",
		Revisions: PageRevisions{Manifest: pageTestRevision(string(project)), Site: pageTestRevision(string(site)), Routes: pageTestRevision(string(routes))},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.ID != "about" || page.Name != "About us" {
		t.Fatalf("created page = %#v", page)
	}
	updatedProject, _ := repository.Read("liapoldus/project.json")
	updatedSite, _ := repository.Read("liapoldus/sites/site-a.json")
	updatedRoutes, _ := repository.Read("liapoldus/routes/development.json")
	if !containsBytes(updatedProject.Content, `"about"`) || !containsBytes(updatedSite.Content, `"name": "About us"`) || !containsBytes(updatedRoutes.Content, `"path": "/about"`) {
		t.Fatalf("page creation did not update all canonical documents: manifest=%s site=%s routes=%s", updatedProject.Content, updatedSite.Content, updatedRoutes.Content)
	}
	if source := string(repository.files["src/pages/about.page.tsx"]); source == "" || !containsBytes([]byte(source), "About us") {
		t.Fatalf("page source was not created: %s", source)
	}
}

func TestCreatePageRejectsStaleRevisionWithoutPartialChanges(t *testing.T) {
	project := []byte(`{"schemaVersion":1,"id":"demo","name":"Demo","pages":["home"],"components":[],"react":{"entry":"src/app.tsx"}}`)
	site := []byte(`{"schemaVersion":1,"id":"site-a","projectId":"demo","name":"Site A","pages":[{"id":"home","name":"Home"}],"locales":["ru-RU"]}`)
	routes := []byte(`{"schemaVersion":1,"id":"development-routes","routes":[{"id":"home","path":"/","page":"home"}]}`)
	repository := &pageRepository{files: map[string][]byte{"liapoldus/project.json": project, "liapoldus/sites/site-a.json": site, "liapoldus/routes/development.json": routes}}
	_, err := NewProjectService(repository).CreatePage(CreatePageRequest{SiteID: "site-a", ID: "about", Name: "About", RoutePath: "/about", Revisions: PageRevisions{Manifest: "stale", Site: pageTestRevision(string(site)), Routes: pageTestRevision(string(routes))}})
	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale page create error = %v, want conflict", err)
	}
	if len(repository.files) != 3 || string(repository.files["liapoldus/project.json"]) != string(project) {
		t.Fatalf("stale page creation partially changed files: %#v", repository.files)
	}
}

func containsBytes(content []byte, expected string) bool {
	return bytes.Contains(content, []byte(expected))
}
