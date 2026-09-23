package application

import (
	"github.com/Liapoldus/Constructor/internal/domain"
	"os"
	"path/filepath"
)

type WorkspaceService struct{ workspace domain.Workspace }

func NewWorkspaceService(workspace domain.Workspace) *WorkspaceService {
	return &WorkspaceService{workspace: workspace}
}
func (s *WorkspaceService) Root() string { return s.workspace.Root() }
func (s *WorkspaceService) Activate(root string) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if info, err := os.Stat(filepath.Join(absolute, "liapoldus", "project.json")); err != nil || info.IsDir() {
		if err != nil {
			return err
		}
		return os.ErrInvalid
	}
	return s.workspace.Activate(absolute)
}
