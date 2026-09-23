package infrastructure

import (
	"os"
	"os/exec"
)

type GitMergeEngine struct{}

func NewGitMergeEngine() *GitMergeEngine { return &GitMergeEngine{} }
func (GitMergeEngine) Merge(base, current, candidate []byte) ([]byte, bool, error) {
	dir, err := os.MkdirTemp("", "constructor-merge-")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(dir)
	basePath, err := writeMergeInput(dir, "base", base)
	if err != nil {
		return nil, false, err
	}
	currentPath, err := writeMergeInput(dir, "current", current)
	if err != nil {
		return nil, false, err
	}
	candidatePath, err := writeMergeInput(dir, "candidate", candidate)
	if err != nil {
		return nil, false, err
	}
	output, runErr := exec.Command("git", "merge-file", "-p", currentPath, basePath, candidatePath).Output()
	if runErr == nil {
		return output, false, nil
	}
	if exit, ok := runErr.(*exec.ExitError); ok && exit.ExitCode() == 1 {
		return output, true, nil
	}
	return nil, false, runErr
}
func writeMergeInput(dir, name string, content []byte) (string, error) {
	path := dir + "/" + name
	return path, os.WriteFile(path, content, 0600)
}
