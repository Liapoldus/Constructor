package application

import "github.com/Liapoldus/Constructor/internal/domain"

// ProjectFileService opens a project-specific repository without changing the
// active workspace. Writes to the active project reuse its shared mutation
// lock so snapshot capture and file updates remain serialized.
type ProjectFileService struct {
	registry domain.ProjectRegistry
	active   *ProjectService
}

func NewProjectFileService(registry domain.ProjectRegistry, active *ProjectService) *ProjectFileService {
	return &ProjectFileService{registry: registry, active: active}
}

func (s *ProjectFileService) Read(projectID, path string) (domain.File, error) {
	record, err := s.record(projectID)
	if err != nil {
		return domain.File{}, err
	}
	return s.active.ReadAtRoot(record.Path, record.ID, path)
}

func (s *ProjectFileService) Write(projectID, path, revision string, content []byte) (domain.File, error) {
	record, err := s.record(projectID)
	if err != nil {
		return domain.File{}, err
	}
	return s.active.WriteAtRoot(record.Path, record.ID, path, revision, content)
}

func (s *ProjectFileService) Validate(projectID string) ([]domain.Diagnostic, error) {
	record, err := s.record(projectID)
	if err != nil {
		return nil, err
	}
	return s.active.ValidateAtRoot(record.Path, record.ID)
}

func (s *ProjectFileService) record(projectID string) (domain.ProjectRecord, error) {
	if s.registry == nil || s.active == nil {
		return domain.ProjectRecord{}, domain.ErrUnsupported
	}
	return s.registry.Get(projectID)
}
