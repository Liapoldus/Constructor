package application

import "github.com/Liapoldus/Constructor/internal/domain"

type MergeService struct {
	repository domain.ProjectRepository
	engine     domain.MergeEngine
}

func NewMergeService(repository domain.ProjectRepository, engine domain.MergeEngine) *MergeService {
	return &MergeService{repository: repository, engine: engine}
}
func (s *MergeService) Merge(path string, base, candidate []byte) (domain.MergeResult, error) {
	current, err := s.repository.Read(path)
	if err != nil {
		return domain.MergeResult{}, err
	}
	content, conflicted, err := s.engine.Merge(base, current.Content, candidate)
	if err != nil {
		return domain.MergeResult{}, err
	}
	return domain.MergeResult{Path: path, Revision: current.Revision, Content: content, Conflicted: conflicted}, nil
}
