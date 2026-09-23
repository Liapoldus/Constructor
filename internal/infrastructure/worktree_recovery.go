package infrastructure

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Liapoldus/Constructor/internal/domain"
)

const worktreeRecoveryDirectory = ".constructor-state/worktrees"

var worktreeRecoveryIDPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)

type worktreeRecoveryRecord struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"id"`
	Repository    string `json:"repository"`
	Stage         string `json:"stage"`
	Target        string `json:"target"`
	Phase         string `json:"phase"`
	AdminMarker   string `json:"adminMarker,omitempty"`
}

func (m *GitRepositoryManager) beginWorktreeRecovery(repository, stage, target string) (worktreeRecoveryRecord, string, error) {
	root, err := m.workspaceRoot()
	if err != nil {
		return worktreeRecoveryRecord{}, "", err
	}
	repositoryPath, err := workspaceRelative(root, repository)
	if err != nil {
		return worktreeRecoveryRecord{}, "", err
	}
	stagePath, err := workspaceRelative(root, stage)
	if err != nil {
		return worktreeRecoveryRecord{}, "", err
	}
	targetPath, err := workspaceRelative(root, target)
	if err != nil {
		return worktreeRecoveryRecord{}, "", err
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return worktreeRecoveryRecord{}, "", err
	}
	record := worktreeRecoveryRecord{
		SchemaVersion: 1,
		ID:            hex.EncodeToString(random[:]),
		Repository:    repositoryPath,
		Stage:         stagePath,
		Target:        targetPath,
		Phase:         "creating",
	}
	journal := filepath.ToSlash(filepath.Join(worktreeRecoveryDirectory, record.ID+".json"))
	if err := m.saveWorktreeRecovery(root, journal, record); err != nil {
		return worktreeRecoveryRecord{}, "", err
	}
	return record, journal, nil
}

func (m *GitRepositoryManager) saveWorktreeRecovery(root, journal string, record worktreeRecoveryRecord) error {
	content, err := json.Marshal(record)
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return writeProjectFile(root, journal, content)
}

func (m *GitRepositoryManager) clearWorktreeRecovery(journal string) error {
	root, err := m.workspaceRoot()
	if err != nil {
		return err
	}
	err = removeProjectFile(root, journal)
	if err == domain.ErrNotFound {
		return nil
	}
	return err
}

// RecoverPendingWorktrees must run before the API starts accepting requests.
// Every journal represents a worktree operation that had not been committed
// to its caller before the previous process stopped, so recovery cleans it.
func (m *GitRepositoryManager) RecoverPendingWorktrees(ctx context.Context) error {
	if err := m.acquire(ctx); err != nil {
		return err
	}
	defer m.release()
	root, err := m.workspaceRoot()
	if err != nil {
		return err
	}
	recoveryDirectory, _, err := openProjectParent(root, worktreeRecoveryDirectory+"/.recovery", true)
	if err != nil {
		return fmt.Errorf("secure worktree recovery directory: %w", err)
	}
	if err := recoveryDirectory.Close(); err != nil {
		return err
	}
	journals, err := listProjectFiles(root, worktreeRecoveryDirectory)
	if err != nil {
		return fmt.Errorf("list worktree recovery records: %w", err)
	}
	if len(journals) > 256 {
		return errors.New("too many pending worktree recovery records")
	}
	for _, journal := range journals {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !strings.HasPrefix(journal, worktreeRecoveryDirectory+"/") || !strings.HasSuffix(journal, ".json") {
			return fmt.Errorf("unexpected file in worktree recovery directory")
		}
		id := strings.TrimSuffix(filepath.Base(journal), ".json")
		if !worktreeRecoveryIDPattern.MatchString(id) {
			return errors.New("invalid worktree recovery record filename")
		}
		content, err := readProjectFile(root, journal)
		if err != nil {
			return fmt.Errorf("read worktree recovery record: %w", err)
		}
		if len(content) > 16*1024 {
			return errors.New("worktree recovery record exceeds size limit")
		}
		var record worktreeRecoveryRecord
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			return fmt.Errorf("decode worktree recovery record: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return errors.New("worktree recovery record must contain one JSON value")
		}
		if record.SchemaVersion != 1 || record.ID != id || !worktreeRecoveryIDPattern.MatchString(record.ID) {
			return errors.New("worktree recovery record identity or version is invalid")
		}
		repository, stage, target, err := m.validateWorktreeRecoveryRecord(record)
		if err != nil {
			return err
		}
		if err := m.recoverWorktreeRecord(ctx, record, journal, repository, stage, target); err != nil {
			return fmt.Errorf("recover worktree operation %s: %w", record.ID, err)
		}
	}
	return nil
}

func (m *GitRepositoryManager) validateWorktreeRecoveryRecord(record worktreeRecoveryRecord) (string, string, string, error) {
	if record.Phase != "creating" && record.Phase != "ready" {
		return "", "", "", errors.New("worktree recovery phase is invalid")
	}
	if record.Phase == "creating" && record.AdminMarker != "" || record.Phase == "ready" && (record.AdminMarker == "" || len(record.AdminMarker) > 4096) {
		return "", "", "", errors.New("worktree recovery marker is invalid")
	}
	if filepath.ToSlash(filepath.Clean(record.Stage)) != record.Stage || filepath.ToSlash(filepath.Clean(record.Target)) != record.Target || filepath.ToSlash(filepath.Clean(record.Repository)) != record.Repository {
		return "", "", "", domain.ErrInvalidPath
	}
	if filepath.Dir(filepath.FromSlash(record.Stage)) != filepath.Dir(filepath.FromSlash(record.Target)) || !strings.HasPrefix(filepath.Base(filepath.FromSlash(record.Stage)), ".constructor-worktree-") {
		return "", "", "", domain.ErrInvalidPath
	}
	repository, err := m.safeRepository(record.Repository)
	if err != nil {
		return "", "", "", err
	}
	stage, err := m.safePath(record.Stage)
	if err != nil {
		return "", "", "", err
	}
	target, err := m.safePath(record.Target)
	if err != nil {
		return "", "", "", err
	}
	return repository, stage, target, nil
}

func (m *GitRepositoryManager) recoverWorktreeRecord(ctx context.Context, record worktreeRecoveryRecord, journal, repository, stage, target string) error {
	stageMarker, stageErr := m.worktreeAdminMarker(stage)
	targetMarker, targetErr := m.worktreeAdminMarker(target)
	if record.Phase == "creating" {
		if targetErr == nil {
			return errors.New("target exists before worktree publication; refusing to remove it")
		}
		if targetErr != domain.ErrNotFound {
			return targetErr
		}
		if stageErr != nil && stageErr != domain.ErrNotFound {
			return stageErr
		}
		if err := m.removeWorktreeChecked(repository, stage); err != nil {
			return err
		}
		return m.clearWorktreeRecovery(journal)
	}

	marker := []byte(record.AdminMarker)
	switch {
	case stageErr == nil && bytes.Equal(stageMarker, marker) && targetErr == domain.ErrNotFound:
		if err := m.removeWorktreeChecked(repository, stage); err != nil {
			return err
		}
	case targetErr == nil && bytes.Equal(targetMarker, marker) && stageErr == domain.ErrNotFound:
		repairCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := m.run(repairCtx, "-C", repository, "worktree", "repair", target)
		cancel()
		if err != nil {
			return err
		}
		if err := m.removeWorktreeChecked(repository, target); err != nil {
			return err
		}
	case stageErr == domain.ErrNotFound && targetErr == domain.ErrNotFound:
		stageRegistered, err := m.worktreeRegistered(repository, stage)
		if err != nil {
			return err
		}
		targetRegistered, err := m.worktreeRegistered(repository, target)
		if err != nil {
			return err
		}
		if stageRegistered || targetRegistered {
			return errors.New("Git still registers a worktree whose directory is missing")
		}
	default:
		return errors.New("worktree paths or Git admin markers do not match the recovery record")
	}
	return m.clearWorktreeRecovery(journal)
}

func workspaceRelative(root, value string) (string, error) {
	relative, err := filepath.Rel(root, value)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", domain.ErrInvalidPath
	}
	return filepath.ToSlash(relative), nil
}
