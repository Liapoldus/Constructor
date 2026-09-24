package presentation

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/Liapoldus/Constructor/internal/application"
	"github.com/Liapoldus/Constructor/internal/domain"
)

type Handler struct {
	projects     *application.ProjectService
	preview      *application.PreviewService
	registry     *application.ProjectRegistryService
	workspace    *application.WorkspaceService
	routes       *application.RouteService
	delivery     *application.DeliveryService
	git          *application.GitService
	repositories *application.RepositoryService
	merge        *application.MergeService
	deploy       *application.DeploymentService
	auth         *application.AuthService
	rbac         *application.RBACService
	projectFiles *application.ProjectFileService
	pluginAdmin  *application.PluginAdminService
}

func NewHandler(projects *application.ProjectService, preview *application.PreviewService, registry *application.ProjectRegistryService, workspace *application.WorkspaceService, routes *application.RouteService, delivery *application.DeliveryService, git *application.GitService, repositories *application.RepositoryService, merge *application.MergeService, deploy *application.DeploymentService, auth *application.AuthService, rbac *application.RBACService, projectFiles ...*application.ProjectFileService) http.Handler {
	return newHandler(projects, preview, registry, workspace, routes, delivery, git, repositories, merge, deploy, auth, rbac, nil, projectFiles...)
}

func NewHandlerWithPluginAdmin(projects *application.ProjectService, preview *application.PreviewService, registry *application.ProjectRegistryService, workspace *application.WorkspaceService, routes *application.RouteService, delivery *application.DeliveryService, git *application.GitService, repositories *application.RepositoryService, merge *application.MergeService, deploy *application.DeploymentService, auth *application.AuthService, rbac *application.RBACService, pluginAdmin *application.PluginAdminService, projectFiles ...*application.ProjectFileService) http.Handler {
	return newHandler(projects, preview, registry, workspace, routes, delivery, git, repositories, merge, deploy, auth, rbac, pluginAdmin, projectFiles...)
}

func newHandler(projects *application.ProjectService, preview *application.PreviewService, registry *application.ProjectRegistryService, workspace *application.WorkspaceService, routes *application.RouteService, delivery *application.DeliveryService, git *application.GitService, repositories *application.RepositoryService, merge *application.MergeService, deploy *application.DeploymentService, auth *application.AuthService, rbac *application.RBACService, pluginAdmin *application.PluginAdminService, projectFiles ...*application.ProjectFileService) http.Handler {
	h := &Handler{projects: projects, preview: preview, registry: registry, workspace: workspace, routes: routes, delivery: delivery, git: git, repositories: repositories, merge: merge, deploy: deploy, auth: auth, rbac: rbac, pluginAdmin: pluginAdmin}
	if len(projectFiles) > 0 {
		h.projectFiles = projectFiles[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", h.health)
	mux.HandleFunc("/api/v1/project", h.project)
	mux.HandleFunc("/api/v1/preview", h.previewEndpoint)
	mux.HandleFunc("/api/v1/projects", h.projectsEndpoint)
	mux.HandleFunc("/api/v1/projects/", h.projectByID)
	mux.HandleFunc("/api/v1/project/file", h.file)
	mux.HandleFunc("/api/v1/project/content", h.localizedContent)
	mux.HandleFunc("/api/v1/project/sites", h.projectSites)
	mux.HandleFunc("/api/v1/project/themes", h.projectThemes)
	mux.HandleFunc("/api/v1/project/assets", h.projectAssets)
	mux.HandleFunc("/api/v1/project/pages", h.projectPages)
	mux.HandleFunc("/api/v1/project/routes", h.routeDocument)
	mux.HandleFunc("/api/v1/project/routes/generate", h.generateRoutes)
	mux.HandleFunc("/api/v1/project/validate", h.validate)
	mux.HandleFunc("/api/v1/project/generate", h.generate)
	mux.HandleFunc("/api/v1/snapshots", h.snapshotEndpoint)
	mux.HandleFunc("/api/v1/snapshots/", h.snapshotBuildPath)
	mux.HandleFunc("/api/v1/builds", h.buildEndpoint)
	mux.HandleFunc("/api/v1/builds/", h.buildDeploymentPath)
	mux.HandleFunc("/api/v1/git/status", h.gitStatus)
	mux.HandleFunc("/api/v1/git/diff", h.gitDiff)
	mux.HandleFunc("/api/v1/git/commit", h.gitCommit)
	mux.HandleFunc("/api/v1/git/branches", h.gitBranches)
	mux.HandleFunc("/api/v1/git/checkout", h.gitCheckout)
	mux.HandleFunc("/api/v1/git/history", h.gitHistory)
	mux.HandleFunc("/api/v1/repositories/clone", h.cloneRepository)
	mux.HandleFunc("/api/v1/repositories/worktree", h.createWorktree)
	mux.HandleFunc("/api/v1/project/file/merge", h.mergeFile)
	mux.HandleFunc("/api/v1/deployments", h.deploymentEndpoint)
	mux.HandleFunc("/api/v1/deployment-target", h.deploymentTarget)
	mux.HandleFunc("/api/v1/sites", h.site)
	mux.HandleFunc("/api/v1/environments", h.environment)
	mux.HandleFunc("/api/v1/deployments/rollback", h.rollback)
	mux.HandleFunc("/api/v1/deployments/", h.deploymentRollbackPath)
	mux.HandleFunc("/api/v1/auth/session", h.authSession)
	mux.HandleFunc("/api/v1/users", h.users)
	mux.HandleFunc("/api/v1/roles", h.roles)
	mux.HandleFunc("/api/v1/permissions", h.permissions)
	mux.HandleFunc("/api/v1/user-roles", h.userRoles)
	mux.HandleFunc("/api/v1/role-permissions", h.rolePermissions)
	mux.HandleFunc("/api/plugins", h.pluginAdminAPI)
	mux.HandleFunc("/api/plugins/", h.pluginAdminAPI)
	return requestIDMiddleware(browserOriginBoundary(permissionMiddleware(mux, auth)))
}

func (h *Handler) projectAssets(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		content, ok := readLimitedRequestBody(w, r, application.MaxAssetUploadBytes)
		if !ok {
			return
		}
		result, err := h.projects.UploadAsset(content)
		if err != nil {
			statusForError(w, err)
			return
		}
		status := http.StatusCreated
		if result.Reused {
			status = http.StatusOK
		}
		writeJSON(w, status, result)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	assets, err := h.projects.Assets()
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": assets})
}

func (h *Handler) projectSites(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var request application.CreateSiteRequest
		if !decodeRequestJSON(w, r, smallJSONRequestLimit, &request) {
			return
		}
		site, err := h.projects.CreateSite(request)
		if err != nil {
			statusForError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"site": site})
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	sites, err := h.projects.Sites()
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": sites})
}

func (h *Handler) projectThemes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	themes, err := h.projects.Themes()
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"themes": themes})
}

func (h *Handler) projectPages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var request application.CreatePageRequest
	if !decodeRequestJSON(w, r, smallJSONRequestLimit, &request) {
		return
	}
	page, err := h.projects.CreatePage(request)
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"page": page})
}

func (h *Handler) projectsEndpoint(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil {
		writeError(w, http.StatusNotImplemented, errors.New("project registry is not configured"))
		return
	}
	if r.Method == http.MethodGet {
		values, err := h.registry.List()
		if err != nil {
			statusForError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"projects": values})
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var request struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if !decodeRequestJSON(w, r, smallJSONRequestLimit, &request) {
		return
	}
	value, err := h.registry.Create(r.Context(), request.ID, request.Name)
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (h *Handler) projectByID(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil {
		writeError(w, http.StatusNotImplemented, errors.New("project registry is not configured"))
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/projects/")
	if strings.Contains(id, "/files/") {
		h.projectFilesByID(w, r, id)
		return
	}
	if strings.HasSuffix(id, "/validate") {
		h.validateProjectByID(w, r, strings.TrimSuffix(id, "/validate"))
		return
	}
	if id == "active" && r.Method == http.MethodGet {
		if h.workspace == nil {
			writeError(w, http.StatusNotImplemented, errors.New("workspace is not configured"))
			return
		}
		manifest, _, err := h.projects.Manifest()
		if err != nil {
			statusForError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"project": domain.ProjectRecord{ID: manifest.ID, Name: manifest.Name, Path: h.workspace.Root()}})
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(id, "/activate") {
		id = strings.TrimSuffix(id, "/activate")
		value, err := h.registry.Get(id)
		if err != nil {
			statusForError(w, err)
			return
		}
		if h.workspace == nil {
			writeError(w, http.StatusNotImplemented, errors.New("workspace is not configured"))
			return
		}
		if err := h.workspace.Activate(value.Path); err != nil {
			statusForError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"project": value, "activeRoot": h.workspace.Root()})
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	value, err := h.registry.Get(id)
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h *Handler) validateProjectByID(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if h.projectFiles == nil {
		writeError(w, http.StatusNotImplemented, errors.New("project validation service is not configured"))
		return
	}
	diagnostics, err := h.projectFiles.Validate(projectID)
	if err != nil {
		statusForError(w, err)
		return
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			writeValidationProblem(w, diagnostics)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "diagnostics": diagnostics})
}

func (h *Handler) projectFilesByID(w http.ResponseWriter, r *http.Request, suffix string) {
	if h.projectFiles == nil {
		writeError(w, http.StatusNotImplemented, errors.New("project file service is not configured"))
		return
	}
	separator := strings.Index(suffix, "/files/")
	if separator <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("project id and relative file path are required"))
		return
	}
	projectID, relativePath := suffix[:separator], suffix[separator+len("/files/"):]
	if relativePath == "" {
		writeError(w, http.StatusBadRequest, errors.New("relative file path is required"))
		return
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		file, err := h.projectFiles.Read(projectID, relativePath)
		if err != nil {
			statusForError(w, err)
			return
		}
		w.Header().Set("ETag", file.Revision)
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(file.Content)
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	content, ok := readLimitedRequestBody(w, r, structuredRequestLimit)
	if !ok {
		return
	}
	file, err := h.projectFiles.Write(projectID, relativePath, strings.Trim(r.Header.Get("If-Match"), `"`), content)
	if err != nil {
		statusForError(w, err)
		return
	}
	w.Header().Set("ETag", file.Revision)
	writeJSON(w, http.StatusOK, map[string]string{"path": file.Path, "revision": file.Revision})
}

func permissionMiddleware(next http.Handler, auth *application.AuthService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		permission := requiredPermission(r.Method, r.URL.Path)
		if permission != "" {
			if err := auth.Authorize(r.Context(), permission); err != nil {
				statusForError(w, err)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func requiredPermission(method, path string) string {
	if (method == http.MethodGet || method == http.MethodHead) && strings.HasSuffix(path, "/files/Caddyfile") && strings.HasPrefix(path, "/api/v1/projects/") {
		return "content.read"
	}
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return ""
	}
	if method == http.MethodPost && (path == "/api/v1/project/validate" || strings.HasPrefix(path, "/api/v1/projects/") && strings.HasSuffix(path, "/validate")) {
		return ""
	}
	switch {
	case method == http.MethodPut && strings.Contains(path, "/files/") && strings.HasPrefix(path, "/api/v1/projects/"):
		return "content.write"
	case path == "/api/v1/projects" || strings.HasPrefix(path, "/api/v1/projects/") && strings.HasSuffix(path, "/activate"):
		return "code.project"
	case path == "/api/v1/git/commit":
		return "code.commit"
	case path == "/api/v1/git/branches", path == "/api/v1/git/checkout":
		return "code.project"
	case path == "/api/v1/project/file":
		return "content.write"
	case path == "/api/v1/project/content":
		return "content.write"
	case path == "/api/v1/project/assets" && method == http.MethodPost:
		return "assets.write"
	case path == "/api/v1/project/themes" && method != http.MethodGet:
		return "content.write"
	case path == "/api/v1/project/pages":
		return "content.write"
	case method == http.MethodPost && path == "/api/v1/project/sites":
		return "content.write"
	case path == "/api/v1/preview":
		return "build.execute"
	case path == "/api/v1/project/routes":
		return "content.write"
	case path == "/api/v1/project/routes/generate":
		return "content.write"
	case path == "/api/v1/project/generate", path == "/api/v1/project/file/merge":
		return "content.write"
	case path == "/api/v1/snapshots":
		return "snapshots.create"
	case path == "/api/v1/builds":
		return "build.execute"
	case path == "/api/v1/deployments", path == "/api/v1/deployments/rollback", strings.HasPrefix(path, "/api/v1/builds/") && strings.HasSuffix(path, "/deployments"), strings.HasPrefix(path, "/api/v1/deployments/") && strings.HasSuffix(path, "/rollback"):
		return "deploy.execute"
	case strings.HasPrefix(path, "/api/v1/snapshots/") && strings.HasSuffix(path, "/builds"):
		return "build.execute"
	case path == "/api/v1/repositories/clone", path == "/api/v1/repositories/worktree":
		return "code.project"
	case path == "/api/v1/sites", path == "/api/v1/environments":
		return "environment.write"
	case path == "/api/v1/users", path == "/api/v1/roles", path == "/api/v1/permissions", path == "/api/v1/user-roles", path == "/api/v1/role-permissions":
		return "admin.roles"
	default:
		// New or accidentally unclassified mutation routes must fail closed.
		return "admin.roles"
	}
}

func (h *Handler) previewEndpoint(w http.ResponseWriter, r *http.Request) {
	if h.preview == nil {
		writeError(w, http.StatusNotImplemented, errors.New("preview runner is not configured"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, h.preview.Status())
	case http.MethodPost:
		session, err := h.preview.Start()
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeJSON(w, http.StatusOK, session)
	case http.MethodDelete:
		if err := h.preview.Stop(); err != nil {
			statusForError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"stopped": true})
	default:
		methodNotAllowed(w)
	}
}

func isLoopbackBrowserOrigin(origin string) bool {
	if origin == "" {
		return true // Native clients and same-host command-line tools have no Origin header.
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.Zone() == "" && address.IsLoopback()
}
func (h *Handler) generateRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	environment := r.URL.Query().Get("environment")
	if environment == "" {
		environment = "development"
	}
	file, diagnostics, err := h.routes.Generate(environment)
	if err != nil {
		if len(diagnostics) > 0 {
			writeValidationProblem(w, diagnostics)
			return
		}
		statusForError(w, err)
		return
	}
	w.Header().Set("ETag", file.Revision)
	writeJSON(w, http.StatusOK, map[string]any{"path": file.Path, "revision": file.Revision, "diagnostics": diagnostics})
}
func (h *Handler) routeDocument(w http.ResponseWriter, r *http.Request) {
	environment := r.URL.Query().Get("environment")
	if environment == "" {
		environment = "development"
	}
	if r.Method == http.MethodGet {
		file, err := h.routes.Read(environment)
		if err != nil {
			statusForError(w, err)
			return
		}
		w.Header().Set("ETag", file.Revision)
		_, _ = w.Write(file.Content)
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	raw, ok := readLimitedRequestBody(w, r, structuredRequestLimit)
	if !ok {
		return
	}
	file, diagnostics, err := h.routes.Save(environment, strings.Trim(r.Header.Get("If-Match"), `"`), raw)
	if err != nil {
		if len(diagnostics) > 0 {
			writeValidationProblem(w, diagnostics)
			return
		}
		statusForError(w, err)
		return
	}
	w.Header().Set("ETag", file.Revision)
	writeJSON(w, http.StatusOK, map[string]any{"path": file.Path, "revision": file.Revision, "diagnostics": diagnostics})
}
func (h *Handler) snapshotEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.snapshots(w, r)
		return
	}
	h.snapshot(w, r)
}
func (h *Handler) buildEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.builds(w, r)
		return
	}
	h.build(w, r)
}
func (h *Handler) snapshotBuildPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	const prefix, suffix = "/api/v1/snapshots/", "/builds"
	value := strings.TrimPrefix(r.URL.Path, prefix)
	if !strings.HasSuffix(value, suffix) {
		writeError(w, http.StatusNotFound, domain.ErrNotFound)
		return
	}
	snapshotID := strings.TrimSuffix(value, suffix)
	if snapshotID == "" || strings.Contains(snapshotID, "/") {
		writeError(w, http.StatusNotFound, domain.ErrNotFound)
		return
	}
	h.buildSnapshot(w, r, snapshotID)
}

func (h *Handler) buildDeploymentPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	const prefix, suffix = "/api/v1/builds/", "/deployments"
	value := strings.TrimPrefix(r.URL.Path, prefix)
	if !strings.HasSuffix(value, suffix) {
		writeError(w, http.StatusNotFound, domain.ErrNotFound)
		return
	}
	buildID := strings.TrimSuffix(value, suffix)
	if buildID == "" || strings.Contains(buildID, "/") {
		writeError(w, http.StatusNotFound, domain.ErrNotFound)
		return
	}
	var request application.CreateDeploymentRequest
	if !decodeRequestJSON(w, r, smallJSONRequestLimit, &request) {
		return
	}
	if request.BuildID != "" && request.BuildID != buildID {
		writeProblem(w, http.StatusConflict, "deployment_build_mismatch", "The build ID in the request does not match the endpoint resource.", nil)
		return
	}
	request.BuildID = buildID
	h.applyDeployment(w, r, request)
}

func (h *Handler) deploymentEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.deployments(w, r)
		return
	}
	h.createDeployment(w, r)
}
func (h *Handler) permissions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		values, err := h.rbac.Permissions()
		if err != nil {
			statusForError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"permissions": values})
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var value domain.Permission
	if !decodeRequestJSON(w, r, smallJSONRequestLimit, &value) {
		return
	}
	if err := h.rbac.SavePermission(value); err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (h *Handler) userRoles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var value domain.UserRole
	if !decodeRequestJSON(w, r, smallJSONRequestLimit, &value) {
		return
	}
	if err := h.rbac.AssignRole(value); err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (h *Handler) rolePermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var value domain.RolePermission
	if !decodeRequestJSON(w, r, smallJSONRequestLimit, &value) {
		return
	}
	if err := h.rbac.GrantPermission(value); err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (h *Handler) users(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		values, err := h.rbac.Users()
		if err != nil {
			statusForError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": values})
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var value domain.User
	if !decodeRequestJSON(w, r, smallJSONRequestLimit, &value) {
		return
	}
	if err := h.rbac.SaveUser(value); err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (h *Handler) roles(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		values, err := h.rbac.Roles()
		if err != nil {
			statusForError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"roles": values})
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var value domain.Role
	if !decodeRequestJSON(w, r, smallJSONRequestLimit, &value) {
		return
	}
	if err := h.rbac.SaveRole(value); err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (h *Handler) rollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("deploymentId"))
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("deploymentId is required"))
		return
	}
	h.rollbackDeployment(w, r, id)
}

func (h *Handler) deploymentRollbackPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	const prefix, suffix = "/api/v1/deployments/", "/rollback"
	value := strings.TrimPrefix(r.URL.Path, prefix)
	if !strings.HasSuffix(value, suffix) {
		writeError(w, http.StatusNotFound, domain.ErrNotFound)
		return
	}
	id := strings.TrimSuffix(value, suffix)
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, domain.ErrNotFound)
		return
	}
	h.rollbackDeployment(w, r, id)
}

func (h *Handler) rollbackDeployment(w http.ResponseWriter, r *http.Request, id string) {
	if h.deploy == nil {
		writeError(w, http.StatusNotImplemented, errors.New("deployment is not configured"))
		return
	}
	value, err := h.deploy.Rollback(r.Context(), id, strings.TrimSpace(r.URL.Query().Get("confirmedTarget")))
	if err != nil {
		var uncertain *application.DeploymentOutcomeUncertain
		if errors.As(err, &uncertain) {
			writeJSON(w, http.StatusAccepted, uncertain.Deployment)
			return
		}
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h *Handler) deployments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	values, err := h.deploy.Deployments()
	if err != nil {
		statusForError(w, err)
		return
	}
	if values == nil {
		values = []domain.Deployment{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployments": values})
}

func (h *Handler) deploymentTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if h.deploy == nil {
		writeError(w, http.StatusNotImplemented, errors.New("deployment is not configured"))
		return
	}
	state, err := h.deploy.TargetState(r.Context(), strings.TrimSpace(r.URL.Query().Get("siteId")), strings.TrimSpace(r.URL.Query().Get("environmentId")))
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}
func (h *Handler) snapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	values, err := h.delivery.Snapshots()
	if err != nil {
		statusForError(w, err)
		return
	}
	if values == nil {
		values = []domain.Snapshot{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": values})
}
func (h *Handler) builds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	values, err := h.delivery.Builds()
	if err != nil {
		statusForError(w, err)
		return
	}
	if values == nil {
		values = []domain.Build{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"builds": values})
}
func (h *Handler) site(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		value, err := h.deploy.Site(strings.TrimSpace(r.URL.Query().Get("id")))
		if err != nil {
			statusForError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var value domain.Site
	if !decodeRequestJSON(w, r, smallJSONRequestLimit, &value) {
		return
	}
	if err := h.deploy.SaveSite(value); err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (h *Handler) environment(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		value, err := h.deploy.Environment(strings.TrimSpace(r.URL.Query().Get("id")))
		if err != nil {
			statusForError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var value domain.Environment
	if !decodeRequestJSON(w, r, smallJSONRequestLimit, &value) {
		return
	}
	if err := h.deploy.SaveEnvironment(value); err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (h *Handler) authSession(w http.ResponseWriter, r *http.Request) {
	principal, err := h.auth.Session(r.Context())
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, principal)
}
func (h *Handler) createDeployment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var request application.CreateDeploymentRequest
	if !decodeRequestJSON(w, r, smallJSONRequestLimit, &request) {
		return
	}
	h.applyDeployment(w, r, request)
}

func (h *Handler) applyDeployment(w http.ResponseWriter, r *http.Request, request application.CreateDeploymentRequest) {
	if h.deploy == nil {
		writeError(w, http.StatusNotImplemented, errors.New("deployment is not configured"))
		return
	}
	result, err := h.deploy.Create(r.Context(), request)
	if err != nil {
		var uncertain *application.DeploymentOutcomeUncertain
		if errors.As(err, &uncertain) {
			writeJSON(w, http.StatusAccepted, uncertain.Deployment)
			return
		}
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
func (h *Handler) mergeFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var request struct {
		Path      string `json:"path"`
		Base      string `json:"base"`
		Candidate string `json:"candidate"`
	}
	if !decodeRequestJSON(w, r, structuredRequestLimit, &request) {
		return
	}
	if strings.TrimSpace(request.Path) == "" {
		writeError(w, http.StatusBadRequest, errors.New("path is required"))
		return
	}
	result, err := h.merge.Merge(request.Path, []byte(request.Base), []byte(request.Candidate))
	if err != nil {
		statusForError(w, err)
		return
	}
	status := http.StatusOK
	if result.Conflicted {
		status = http.StatusConflict
	}
	w.Header().Set("ETag", result.Revision)
	writeJSON(w, status, map[string]any{"path": result.Path, "revision": result.Revision, "content": string(result.Content), "conflicted": result.Conflicted})
}
func (h *Handler) cloneRepository(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if h.repositories == nil {
		writeError(w, http.StatusNotImplemented, errors.New("repository manager is not configured"))
		return
	}
	url := strings.TrimSpace(r.URL.Query().Get("url"))
	target := strings.TrimSpace(r.URL.Query().Get("target"))
	path, err := h.repositories.Clone(r.Context(), url, target)
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"path": path})
}
func (h *Handler) createWorktree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if h.repositories == nil {
		writeError(w, http.StatusNotImplemented, errors.New("repository manager is not configured"))
		return
	}
	repository := strings.TrimSpace(r.URL.Query().Get("repository"))
	commit := strings.TrimSpace(r.URL.Query().Get("commit"))
	target := strings.TrimSpace(r.URL.Query().Get("target"))
	path, err := h.repositories.Worktree(r.Context(), repository, commit, target)
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"path": path, "commit": commit})
}
func (h *Handler) gitStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	status, err := h.git.Status()
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": status})
}
func (h *Handler) gitDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	diff, err := h.git.Diff()
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"diff": diff})
}
func (h *Handler) gitCommit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if err := h.auth.Authorize(r.Context(), "code.commit"); err != nil {
		statusForError(w, err)
		return
	}
	var request struct {
		Message string `json:"message"`
	}
	if !decodeRequestJSON(w, r, smallJSONRequestLimit, &request) {
		return
	}
	revision, err := h.git.Commit(request.Message)
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"revision": revision})
}
func (h *Handler) gitBranches(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var request struct {
			Name string `json:"name"`
		}
		if !decodeRequestJSON(w, r, smallJSONRequestLimit, &request) {
			return
		}
		branch, err := h.git.CreateBranch(r.Context(), request.Name)
		if err != nil {
			statusForError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"branch": branch})
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	branches, err := h.git.Branches()
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"branches": branches})
}
func (h *Handler) gitCheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var request struct {
		Name string `json:"name"`
	}
	if !decodeRequestJSON(w, r, smallJSONRequestLimit, &request) {
		return
	}
	branch, err := h.git.CheckoutBranch(r.Context(), request.Name)
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"branch": branch})
}
func (h *Handler) gitHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	commits, err := h.git.History(20)
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"commits": commits})
}
func (h *Handler) snapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	siteID := strings.TrimSpace(r.URL.Query().Get("siteId"))
	if siteID == "" {
		writeError(w, http.StatusBadRequest, errors.New("siteId is required"))
		return
	}
	locale := strings.TrimSpace(r.URL.Query().Get("locale"))
	if locale == "" {
		writeError(w, http.StatusBadRequest, errors.New("locale is required"))
		return
	}
	if h.delivery == nil {
		writeError(w, http.StatusNotImplemented, errors.New("delivery is not configured"))
		return
	}
	snapshot, err := h.delivery.CreateProjectSnapshot(h.projects, siteID, locale, "development")
	if err != nil {
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, snapshot)
}
func (h *Handler) build(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if h.delivery == nil {
		writeError(w, http.StatusNotImplemented, errors.New("delivery is not configured"))
		return
	}
	snapshotID := strings.TrimSpace(r.URL.Query().Get("snapshotId"))
	if snapshotID == "" {
		writeError(w, http.StatusBadRequest, errors.New("snapshotId is required"))
		return
	}
	h.buildSnapshot(w, r, snapshotID)
}

func (h *Handler) buildSnapshot(w http.ResponseWriter, r *http.Request, snapshotID string) {
	if h.delivery == nil {
		writeError(w, http.StatusNotImplemented, errors.New("delivery is not configured"))
		return
	}
	build, err := h.delivery.BuildSnapshot(r.Context(), snapshotID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, http.StatusCreated, build)
}
func (h *Handler) generate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	locale := strings.TrimSpace(r.URL.Query().Get("locale"))
	if locale == "" {
		writeError(w, http.StatusBadRequest, errors.New("locale is required"))
		return
	}
	siteID := strings.TrimSpace(r.URL.Query().Get("siteId"))
	if siteID == "" {
		writeError(w, http.StatusBadRequest, errors.New("siteId is required"))
		return
	}
	artifact, err := h.projects.GenerateLocale(siteID, locale)
	if err != nil {
		var validation domain.ContentValidationError
		if errors.As(err, &validation) {
			writeValidationProblem(w, validation.Diagnostics)
			return
		}
		statusForError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, artifact)
}
func (h *Handler) validate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	siteID := strings.TrimSpace(r.URL.Query().Get("siteId"))
	locale := strings.TrimSpace(r.URL.Query().Get("locale"))
	if (siteID == "") != (locale == "") {
		writeError(w, http.StatusBadRequest, errors.New("siteId and locale must be provided together"))
		return
	}
	var diagnostics []domain.Diagnostic
	var err error
	if siteID == "" {
		diagnostics, err = h.projects.ValidateAllSites()
	} else {
		diagnostics, err = h.projects.ValidateForSiteLocale(siteID, locale)
	}
	if err != nil {
		statusForError(w, err)
		return
	}
	status := http.StatusOK
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			status = http.StatusUnprocessableEntity
			break
		}
	}
	if status == http.StatusUnprocessableEntity {
		writeValidationProblem(w, diagnostics)
		return
	}
	writeJSON(w, status, map[string]any{"valid": true, "diagnostics": diagnostics})
}
func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "apiVersion": "v1"})
}
func (h *Handler) project(w http.ResponseWriter, _ *http.Request) {
	project, revision, err := h.projects.Manifest()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("ETag", revision)
	writeJSON(w, http.StatusOK, project)
}
func (h *Handler) file(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeError(w, http.StatusBadRequest, errors.New("path is required"))
		return
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		file, err := h.projects.Read(path)
		if err != nil {
			statusForError(w, err)
			return
		}
		w.Header().Set("ETag", file.Revision)
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(file.Content)
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	content, ok := readLimitedRequestBody(w, r, structuredRequestLimit)
	if !ok {
		return
	}
	file, err := h.projects.Write(path, strings.Trim(r.Header.Get("If-Match"), `"`), content)
	if err != nil {
		statusForError(w, err)
		return
	}
	w.Header().Set("ETag", file.Revision)
	writeJSON(w, http.StatusOK, map[string]string{"path": file.Path, "revision": file.Revision})
}

func (h *Handler) localizedContent(w http.ResponseWriter, r *http.Request) {
	siteID := strings.TrimSpace(r.URL.Query().Get("siteId"))
	locale := strings.TrimSpace(r.URL.Query().Get("locale"))
	if siteID == "" || locale == "" {
		writeProblem(w, http.StatusBadRequest, "content_scope_required", "siteId and locale are required.", nil)
		return
	}
	if r.Method == http.MethodGet {
		view, err := h.projects.LoadLocalizedContent(siteID, locale)
		if err != nil {
			statusForError(w, err)
			return
		}
		if view.ContentRevision != "" {
			w.Header().Set("ETag", view.ContentRevision)
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var request struct {
		Base      domain.ContentDocument `json:"base"`
		Document  domain.ContentDocument `json:"document"`
		Revisions struct {
			Locale  string `json:"locale"`
			Default string `json:"default"`
		} `json:"revisions"`
	}
	if !decodeRequestJSON(w, r, structuredRequestLimit, &request) {
		return
	}
	result, err := h.projects.SaveLocalizedContent(siteID, locale, request.Base, request.Document, request.Revisions.Locale, request.Revisions.Default)
	if err != nil {
		statusForError(w, err)
		return
	}
	w.Header().Set("ETag", result.ContentRevision)
	writeJSON(w, http.StatusOK, result)
}

func statusForError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrAssetTooLarge) {
		writeProblem(w, http.StatusRequestEntityTooLarge, "payload_too_large", "The asset exceeds the 25 MiB upload limit.", nil)
		return
	}
	if errors.Is(err, domain.ErrAssetUploadInvalid) {
		writeProblem(w, http.StatusUnprocessableEntity, "asset_upload_invalid", "Upload a valid PNG, JPEG, WebP, GIF, or sanitizable SVG image.", nil)
		return
	}
	var contentValidation domain.ContentValidationError
	if errors.As(err, &contentValidation) {
		writeValidationProblem(w, contentValidation.Diagnostics)
		return
	}
	var structuredValidation domain.StructuredValidationError
	if errors.As(err, &structuredValidation) {
		writeValidationProblem(w, structuredValidation.Diagnostics)
		return
	}
	var conflict *domain.ConflictError
	if errors.As(err, &conflict) {
		writeProblem(w, http.StatusConflict, "revision_conflict", "The document changed since the supplied revision.", map[string]any{"conflict": conflict, "error": conflict.Error()})
		return
	}
	if errors.Is(err, domain.ErrSnapshotNotReady) {
		writeProblem(w, http.StatusConflict, "snapshot_not_ready", "A build requires a ready validated snapshot.", nil)
		return
	}
	if errors.Is(err, domain.ErrSnapshotSourceChanged) {
		writeProblem(w, http.StatusConflict, "snapshot_source_changed", "Project files changed during snapshot capture. Retry the snapshot.", nil)
		return
	}
	if errors.Is(err, domain.ErrDeploymentNotReady) {
		writeProblem(w, http.StatusConflict, "deployment_not_ready", "Deployment requires a ready validated snapshot and a successful matching build.", nil)
		return
	}
	if errors.Is(err, domain.ErrDeploymentConfirmationRequired) {
		writeProblem(w, http.StatusPreconditionRequired, "deployment_confirmation_required", "Confirm the exact Site and Environment target before deployment.", nil)
		return
	}
	if errors.Is(err, domain.ErrGatewayBaselineConfirmationRequired) {
		writeProblem(w, http.StatusPreconditionRequired, "gateway_baseline_confirmation_required", "Confirm the exact current Gateway release revision before the first Constructor deployment.", nil)
		return
	}
	if errors.Is(err, domain.ErrGatewayRevisionConflict) {
		writeProblem(w, http.StatusConflict, "gateway_revision_conflict", "The Gateway release revision differs from Constructor history; reconcile the target before deploying.", nil)
		return
	}
	if errors.Is(err, domain.ErrActiveDeployment) {
		writeProblem(w, http.StatusConflict, "active_deployment_conflict", "The active deployment changed during this operation.", nil)
		return
	}
	if errors.Is(err, domain.ErrDeploymentConflict) {
		writeProblem(w, http.StatusConflict, "deployment_id_conflict", "The deployment id was already used for a different request.", nil)
		return
	}
	if errors.Is(err, domain.ErrDeploymentInProgress) {
		writeProblem(w, http.StatusConflict, "deployment_in_progress", "A deployment with this id is already in progress.", nil)
		return
	}
	if errors.Is(err, domain.ErrUnknownDelivery) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if errors.Is(err, domain.ErrDeploymentNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if errors.Is(err, domain.ErrConflict) {
		writeError(w, http.StatusPreconditionFailed, err)
		return
	}
	if errors.Is(err, domain.ErrInvalidPath) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if errors.Is(err, domain.ErrInvalidRepositoryURL) || errors.Is(err, domain.ErrInvalidCommit) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if errors.Is(err, domain.ErrInvalidGitBranch) {
		writeProblem(w, http.StatusBadRequest, "git_branch_invalid", "The branch name is not valid.", nil)
		return
	}
	if errors.Is(err, domain.ErrGitWorktreeDirty) {
		writeProblem(w, http.StatusConflict, "git_worktree_dirty", "Save and commit or otherwise resolve working-tree changes before switching branches.", nil)
		return
	}
	if errors.Is(err, domain.ErrGitBranchExists) {
		writeProblem(w, http.StatusConflict, "git_branch_exists", "A local branch with this name already exists.", nil)
		return
	}
	if errors.Is(err, domain.ErrGitBranchNotFound) {
		writeProblem(w, http.StatusNotFound, "git_branch_not_found", "The requested local branch does not exist.", nil)
		return
	}
	if errors.Is(err, domain.ErrRepositoryTargetExists) {
		writeProblem(w, http.StatusConflict, "conflict", "The repository target already exists.", nil)
		return
	}
	if errors.Is(err, domain.ErrProjectExists) {
		writeProblem(w, http.StatusConflict, "conflict", "The project id already exists.", nil)
		return
	}
	if errors.Is(err, domain.ErrForbidden) {
		writeError(w, http.StatusForbidden, err)
		return
	}
	writeError(w, http.StatusInternalServerError, err)
}
func writeError(w http.ResponseWriter, status int, err error) {
	code := problemCodeForStatus(status)
	detail := http.StatusText(status)
	legacyError := detail
	if err != nil && status < http.StatusInternalServerError {
		detail = err.Error()
		legacyError = detail
	}
	writeProblem(w, status, code, detail, map[string]any{"error": legacyError})
}

func problemCodeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusConflict:
		return "conflict"
	case http.StatusPreconditionFailed:
		return "precondition_failed"
	case http.StatusRequestEntityTooLarge:
		return "payload_too_large"
	case http.StatusUnprocessableEntity:
		return "unprocessable_entity"
	case http.StatusNotImplemented:
		return "not_implemented"
	case http.StatusBadGateway:
		return "bad_gateway"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	default:
		if status >= http.StatusInternalServerError {
			return "internal_error"
		}
		return "request_failed"
	}
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.", nil)
		return
	}
	body = addRequestID(w, body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
