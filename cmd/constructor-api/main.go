package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Liapoldus/Constructor/internal/application"
	"github.com/Liapoldus/Constructor/internal/domain"
	"github.com/Liapoldus/Constructor/internal/infrastructure"
	"github.com/Liapoldus/Constructor/internal/presentation"
)

func main() {
	address := os.Getenv("CONSTRUCTOR_API_ADDR")
	if address == "" {
		address = "127.0.0.1:8787"
	}
	if err := validateLoopbackListenAddress(address); err != nil {
		log.Fatalf("invalid CONSTRUCTOR_API_ADDR: %v", err)
	}
	root := os.Getenv("CONSTRUCTOR_PROJECT_ROOT")
	if root == "" {
		root = "."
	}
	projectWorkspace := infrastructure.NewProjectWorkspace(root)
	repository := infrastructure.NewFilesystemRepositoryWithWorkspace(projectWorkspace)
	projects := application.NewProjectService(repository)
	routes := application.NewRouteService(projects)
	databasePath := os.Getenv("CONSTRUCTOR_DB")
	if databasePath == "" {
		databasePath = "constructor.db"
	}
	store, err := infrastructure.NewSQLiteDeliveryStore(databasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	delivery := application.NewDeliveryService(infrastructure.NewGitCommandRepositoryWithWorkspace(projectWorkspace), store, projects.Validate, infrastructure.NewNodeBuildRunnerWithWorkspace(projectWorkspace))
	git := application.NewGitService(infrastructure.NewGitCommandRepositoryWithWorkspace(projectWorkspace))
	workspace := os.Getenv("CONSTRUCTOR_WORKSPACE_ROOT")
	if workspace == "" {
		workspace = os.TempDir()
	}
	projectRegistry := infrastructure.NewFilesystemProjectRegistry(workspace)
	registry := application.NewProjectRegistryService(projectRegistry)
	projectFiles := application.NewProjectFileService(projectRegistry, projects)
	workspaceService := application.NewWorkspaceService(projectWorkspace)
	previewRunner := infrastructure.NewNodePreviewRunner()
	defer previewRunner.Close()
	preview := application.NewPreviewService(projectWorkspace, previewRunner)
	repositoryManager := infrastructure.NewGitRepositoryManager(workspace)
	worktreeRecoveryContext, cancelWorktreeRecovery := context.WithTimeout(context.Background(), 30*time.Second)
	if err := repositoryManager.RecoverPendingWorktrees(worktreeRecoveryContext); err != nil {
		cancelWorktreeRecovery()
		log.Fatalf("recover pending Git worktrees: %v", err)
	}
	cancelWorktreeRecovery()
	repositories := application.NewRepositoryService(repositoryManager)
	merge := application.NewMergeService(repository, infrastructure.NewGitMergeEngine())
	var gateway domain.GatewayClient = infrastructure.UnavailableGatewayClient{}
	var pluginAdminGateway domain.PluginAdminGateway
	var gatewayGroupReader domain.GatewayGroupReader
	if gatewayURL := os.Getenv("GATEWAY_URL"); gatewayURL != "" {
		gateway = infrastructure.NewHTTPGatewayClient(gatewayURL, os.Getenv("GATEWAY_TOKEN"))
		pluginAdminGateway = infrastructure.NewHTTPPluginAdminGateway(gatewayURL, os.Getenv("GATEWAY_TOKEN"))
		gatewayGroupReader = infrastructure.NewHTTPGatewayGroupClient(gatewayURL, os.Getenv("GATEWAY_TOKEN"))
	}
	deploy := application.NewDeploymentService(store, gateway, store)
	recoveryContext, stopRecovery := context.WithCancel(context.Background())
	recoveryDone := make(chan struct{})
	defer func() {
		stopRecovery()
		<-recoveryDone
	}()
	go func() {
		defer close(recoveryDone)
		recoveryWasIncomplete := false
		for {
			if err := deploy.Recover(recoveryContext); err != nil {
				if !recoveryWasIncomplete {
					log.Printf("deployment recovery is incomplete; unresolved targets remain reserved")
				}
				recoveryWasIncomplete = true
			} else {
				recoveryWasIncomplete = false
			}
			timer := time.NewTimer(10 * time.Second)
			select {
			case <-recoveryContext.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	auth := application.NewAuthService(store)
	rbac := application.NewRBACService(store)
	handler := presentation.NewHandlerWithGatewayGroups(projects, preview, registry, workspaceService, routes, delivery, git, repositories, merge, deploy, auth, rbac, application.NewPluginAdminService(pluginAdminGateway), application.NewGatewayGroupCatalogService(gatewayGroupReader), projectFiles)
	server := &http.Server{Addr: address, Handler: presentation.LoopbackHostBoundary(handler)}
	log.Printf("Constructor API listening on %s", address)
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.ListenAndServe() }()
	stopSignals := make(chan os.Signal, 1)
	signal.Notify(stopSignals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stopSignals)
	select {
	case err := <-serveErrors:
		if err != nil && err != http.ErrServerClosed {
			log.Printf("Constructor API stopped: %v", err)
		}
	case <-stopSignals:
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			log.Printf("Constructor API shutdown: %v", err)
		}
	}
}
