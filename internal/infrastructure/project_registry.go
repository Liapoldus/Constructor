package infrastructure

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Liapoldus/Constructor/internal/domain"
)

//go:embed templates/project/src/vendor/liapoldus/react/index.ts
var scaffoldReactSDK []byte

//go:embed templates/project/src/vendor/liapoldus/react/package.json
var scaffoldReactSDKPackage []byte

//go:embed templates/project/src/vendor/liapoldus/react/LICENSE
var scaffoldReactSDKLicense []byte

type FilesystemProjectRegistry struct{ root string }

func NewFilesystemProjectRegistry(root string) *FilesystemProjectRegistry {
	return &FilesystemProjectRegistry{root: root}
}

func (r *FilesystemProjectRegistry) List() ([]domain.ProjectRecord, error) {
	if err := r.validateRoot(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(r.root)
	if err != nil {
		return nil, err
	}
	projects := []domain.ProjectRecord{}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		projectRoot := filepath.Join(r.root, entry.Name())
		content, readErr := readRegistryManifest(projectRoot)
		if readErr != nil {
			continue
		}
		path := filepath.Join(projectRoot, "liapoldus", "project.json")
		project, diagnostics := domain.DecodeProjectDocument(content, filepath.Join(entry.Name(), "liapoldus", "project.json"))
		if len(diagnostics) == 0 && project.ID == entry.Name() {
			projects = append(projects, domain.ProjectRecord{ID: project.ID, Name: project.Name, Path: filepath.Dir(filepath.Dir(path))})
		}
	}
	return projects, nil
}

func (r *FilesystemProjectRegistry) Get(id string) (domain.ProjectRecord, error) {
	id = strings.TrimSpace(id)
	if !validProjectID(id) {
		return domain.ProjectRecord{}, domain.ErrInvalidPath
	}
	if err := r.validateRoot(); err != nil {
		return domain.ProjectRecord{}, err
	}
	path := filepath.Join(r.root, id)
	content, err := readRegistryManifest(path)
	if err != nil {
		return domain.ProjectRecord{}, err
	}
	project, diagnostics := domain.DecodeProjectDocument(content, filepath.Join(id, "liapoldus", "project.json"))
	if len(diagnostics) > 0 {
		return domain.ProjectRecord{}, domain.StructuredValidationError{Diagnostics: diagnostics}
	}
	if project.ID != id {
		return domain.ProjectRecord{}, domain.ErrNotFound
	}
	return domain.ProjectRecord{ID: project.ID, Name: project.Name, Path: path}, nil
}

func (r *FilesystemProjectRegistry) validateRoot() error {
	info, err := os.Lstat(r.root)
	if err != nil {
		if os.IsNotExist(err) {
			return domain.ErrNotFound
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return domain.ErrInvalidPath
	}
	return nil
}

func readRegistryManifest(projectRoot string) ([]byte, error) {
	rootInfo, err := os.Lstat(projectRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, domain.ErrInvalidPath
	}
	metadata := filepath.Join(projectRoot, "liapoldus")
	metadataInfo, err := os.Lstat(metadata)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if metadataInfo.Mode()&os.ModeSymlink != 0 || !metadataInfo.IsDir() {
		return nil, domain.ErrInvalidPath
	}
	manifest := filepath.Join(metadata, "project.json")
	manifestInfo, err := os.Lstat(manifest)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if manifestInfo.Mode()&os.ModeSymlink != 0 || !manifestInfo.Mode().IsRegular() {
		return nil, domain.ErrInvalidPath
	}
	return os.ReadFile(manifest)
}

func (r *FilesystemProjectRegistry) Create(ctx context.Context, id, name string) (domain.ProjectRecord, error) {
	if err := ctx.Err(); err != nil {
		return domain.ProjectRecord{}, err
	}
	id, name = strings.TrimSpace(id), strings.TrimSpace(name)
	if !validProjectID(id) || name == "" {
		return domain.ProjectRecord{}, fmt.Errorf("project id and name are required")
	}
	if err := os.MkdirAll(r.root, 0755); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := r.validateRoot(); err != nil {
		return domain.ProjectRecord{}, err
	}
	target := filepath.Join(r.root, id)
	if _, err := os.Lstat(target); err == nil {
		return domain.ProjectRecord{}, domain.ErrProjectExists
	} else if !os.IsNotExist(err) {
		return domain.ProjectRecord{}, err
	}
	path, err := os.MkdirTemp(r.root, ".constructor-project-")
	if err != nil {
		return domain.ProjectRecord{}, err
	}
	defer os.RemoveAll(path)
	if err := ctx.Err(); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.MkdirAll(filepath.Join(path, "liapoldus"), 0755); err != nil {
		return domain.ProjectRecord{}, err
	}
	project := domain.Project{SchemaVersion: 1, ID: id, Name: name, Pages: []string{"home"}, Components: []string{"Hero"}}
	project.React.Entry = "src/app.tsx"
	content, err := json.MarshalIndent(project, "", "  ")
	if err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(path, "liapoldus", "project.json"), append(content, '\n'), 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	siteJSON, err := json.MarshalIndent(map[string]any{"schemaVersion": 1, "id": "local-site", "projectId": id, "name": name, "pages": []domain.SitePage{{ID: "home", Name: "Home"}}, "locales": []string{"ru-RU"}}, "", "  ")
	if err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.MkdirAll(filepath.Join(path, "liapoldus", "sites"), 0755); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(path, "liapoldus", "sites", "local-site.json"), append(siteJSON, '\n'), 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.MkdirAll(filepath.Join(path, "src"), 0755); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(path, "src", "vite-env.d.ts"), []byte("/// <reference types=\"vite/client\" />\n"), 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.MkdirAll(filepath.Join(path, "src", "pages"), 0755); err != nil {
		return domain.ProjectRecord{}, err
	}
	packageJSON := []byte("{\n  \"name\": \"" + id + "\",\n  \"private\": true,\n  \"type\": \"module\",\n  \"scripts\": {\"dev\": \"vite\", \"build\": \"tsc --noEmit && vite build\"},\n  \"dependencies\": {\"@vitejs/plugin-react\": \"latest\", \"vite\": \"latest\", \"typescript\": \"latest\", \"react\": \"latest\", \"react-dom\": \"latest\", \"react-router-dom\": \"latest\"},\n  \"devDependencies\": {\"@types/react\": \"latest\", \"@types/react-dom\": \"latest\"}\n}\n")
	if err := os.WriteFile(filepath.Join(path, "package.json"), packageJSON, 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(path, ".gitignore"), []byte("node_modules/\ndist/\n.env*\n"), 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(path, "index.html"), []byte("<!doctype html>\n<html lang=\"ru\"><head><meta charset=\"UTF-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\"><title>"+html.EscapeString(name)+"</title></head><body><div id=\"root\"></div><script type=\"module\" src=\"/src/app.tsx\"></script></body></html>\n"), 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(path, "vite.config.ts"), []byte("import {defineConfig} from 'vite'\nimport react from '@vitejs/plugin-react'\nexport default defineConfig({plugins: [react()]})\n"), 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(path, "tsconfig.json"), []byte("{\n  \"compilerOptions\": {\n    \"target\": \"ES2022\",\n    \"module\": \"ESNext\",\n    \"moduleResolution\": \"Bundler\",\n    \"jsx\": \"react-jsx\",\n    \"strict\": true,\n    \"noEmit\": true,\n    \"resolveJsonModule\": true,\n    \"skipLibCheck\": true\n  },\n  \"include\": [\"src\"]\n}\n"), 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(path, "src", "app.tsx"), []byte("import {createRoot} from 'react-dom/client'\nimport {createBrowserRouter,RouterProvider} from 'react-router-dom'\nimport {createRoutes} from './generated/routes'\nimport './generated/theme.css'\nimport {PreviewRuntimeProvider} from './vendor/liapoldus/react/index'\nimport initialContent from './generated/content/ru-RU.json'\n\nconst canAccess = (_policy:string) => false\ncreateRoot(document.getElementById('root')!).render(<PreviewRuntimeProvider initialContent={initialContent}><RouterProvider router={createBrowserRouter(createRoutes(canAccess))}/></PreviewRuntimeProvider>)\n"), 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	page := []byte("import Hero from '../components/hero/component'\nimport {PreviewInstance,usePreviewContent} from '../vendor/liapoldus/react/index'\nexport default function HomePage(){const title=usePreviewContent<string>('home','hero-main','title'," + strconv.Quote(name) + ");return <PreviewInstance instanceId='hero-main'><main><Hero title={title}/></main></PreviewInstance>}\n")
	if err := os.WriteFile(filepath.Join(path, "src", "pages", "home.page.tsx"), page, 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	heroDirectory := filepath.Join(path, "src", "components", "hero")
	if err := os.MkdirAll(heroDirectory, 0755); err != nil {
		return domain.ProjectRecord{}, err
	}
	heroSchema := []byte("{\n  \"schemaVersion\": 1,\n  \"id\": \"hero\",\n  \"kind\": \"component\",\n  \"source\": \"src/components/hero/component.tsx\",\n  \"fields\": [{\"key\":\"title\",\"type\":\"text\",\"label\":\"Title\",\"required\":true,\"localized\":true}]\n}\n")
	if err := os.WriteFile(filepath.Join(heroDirectory, "schema.json"), heroSchema, 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	heroSource := []byte("import {component, type ComponentSchema} from '../../vendor/liapoldus/react/index'\nconst schema={id:'hero',schemaVersion:1,kind:'component',source:'src/components/hero/component.tsx',fields:[{key:'title',type:'text',label:'Title',required:true,localized:true}]} as const satisfies ComponentSchema\nexport default component(schema,({title})=><section><h1>{title}</h1></section>)\n")
	if err := os.WriteFile(filepath.Join(heroDirectory, "component.tsx"), heroSource, 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	routeArtifact := []byte("import React from 'react'\nimport type {RouteObject} from 'react-router-dom'\nimport HomePage from '../pages/home.page'\nexport type AccessCheck=(policy:string)=>boolean\nexport const createRoutes=(_canAccess:AccessCheck):RouteObject[]=>[{path:'/',element:React.createElement(HomePage)}]\nexport const routes:RouteObject[]=createRoutes(()=>false)\n")
	if err := os.MkdirAll(filepath.Join(path, "src", "generated"), 0755); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(path, "src", "generated", "routes.tsx"), routeArtifact, 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(path, "src", "generated", "assets.ts"), []byte("export const assets = {} as const\nexport type AssetId = keyof typeof assets\n"), 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(path, "src", "generated", "theme.css"), []byte(":root {}\n"), 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	vendoredSDK := filepath.Join(path, "src", "vendor", "liapoldus", "react")
	if err := os.MkdirAll(vendoredSDK, 0755); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(vendoredSDK, "index.ts"), scaffoldReactSDK, 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(vendoredSDK, "package.json"), scaffoldReactSDKPackage, 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(vendoredSDK, "LICENSE"), scaffoldReactSDKLicense, 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	contentDocument := map[string]any{"schemaVersion": 1, "id": "home-content", "instances": []map[string]any{{"id": "hero-main", "pageId": "home", "component": "hero", "fields": map[string]string{"title": name}}}}
	contentJSON, err := json.MarshalIndent(contentDocument, "", "  ")
	if err != nil {
		return domain.ProjectRecord{}, err
	}
	contentJSON = append(contentJSON, '\n')
	contentDirectory := filepath.Join(path, "liapoldus", "content", "local-site")
	if err := os.MkdirAll(contentDirectory, 0755); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(contentDirectory, "ru-RU.json"), contentJSON, 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.MkdirAll(filepath.Join(path, "src", "generated", "content"), 0755); err != nil {
		return domain.ProjectRecord{}, err
	}
	runtimeContent, err := json.MarshalIndent(map[string]any{"pages": map[string]any{"home": map[string]any{"instances": map[string]any{"hero-main": map[string]any{"component": "hero", "fields": map[string]string{"title": name}}}}}}, "", "  ")
	if err != nil {
		return domain.ProjectRecord{}, err
	}
	runtimeContent = append(runtimeContent, '\n')
	if err := os.WriteFile(filepath.Join(path, "src", "generated", "content", "ru-RU.json"), runtimeContent, 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	routes := []byte("{\n  \"schemaVersion\": 1,\n  \"id\": \"development-routes\",\n  \"routes\": [{\"id\":\"home\",\"path\":\"/\",\"page\":\"home\",\"chunk\":\"same\"}]\n}\n")
	if err := os.MkdirAll(filepath.Join(path, "liapoldus", "routes"), 0755); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(path, "liapoldus", "routes", "development.json"), routes, 0644); err != nil {
		return domain.ProjectRecord{}, err
	}
	git := func(args ...string) error {
		commandArgs := append([]string{"-c", "protocol.ext.allow=never", "-c", "core.hooksPath=/dev/null", "-C", path}, args...)
		command := gitCommand(ctx, commandArgs...)
		command.Env = gitEnvironment()
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		err := command.Run()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return fmt.Errorf("initialize project git repository: %w", err)
		}
		return nil
	}
	if err := git("init", "-q"); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := git("config", "user.name", "Liapoldus Constructor"); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := git("config", "user.email", "constructor@localhost"); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := git("add", "--all"); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := git("commit", "-q", "-m", "chore: initialize Constructor project"); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.ProjectRecord{}, err
	}
	if err := publishProjectDirectory(ctx, path, target); err != nil {
		return domain.ProjectRecord{}, err
	}
	return domain.ProjectRecord{ID: id, Name: name, Path: target}, nil
}

func publishProjectDirectory(ctx context.Context, stage, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := publishDirectoryNoReplace(filepath.Dir(target), stage, target); err != nil {
		if os.IsExist(err) {
			return domain.ErrProjectExists
		}
		return fmt.Errorf("project publication failed: %w", err)
	}
	return nil
}

func validProjectID(value string) bool {
	if value == "" || filepath.Base(value) != value {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return false
		}
	}
	return true
}
