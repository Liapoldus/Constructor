package application

import "github.com/Liapoldus/Constructor/internal/domain"

type PreviewService struct {
	workspace domain.Workspace
	runner    domain.PreviewRunner
}

func NewPreviewService(workspace domain.Workspace, runner domain.PreviewRunner) *PreviewService {
	return &PreviewService{workspace: workspace, runner: runner}
}

func (s *PreviewService) Start() (domain.PreviewSession, error) {
	return s.runner.Start(s.workspace.Root())
}

func (s *PreviewService) Stop() error {
	return s.runner.Stop(s.workspace.Root())
}

func (s *PreviewService) Status() domain.PreviewSession {
	return s.runner.Status(s.workspace.Root())
}
