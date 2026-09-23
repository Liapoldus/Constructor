package infrastructure

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Liapoldus/Constructor/internal/domain"
)

const nodeBuildTimeout = 15 * time.Minute
const buildCommandWaitDelay = 2 * time.Second

type NodeBuildRunner struct {
	root      string
	workspace *ProjectWorkspace
}

func NewNodeBuildRunner(root string) *NodeBuildRunner { return &NodeBuildRunner{root: root} }
func NewNodeBuildRunnerWithWorkspace(workspace *ProjectWorkspace) *NodeBuildRunner {
	return &NodeBuildRunner{workspace: workspace}
}

func (r *NodeBuildRunner) Run(parent context.Context, snapshot domain.Snapshot) (domain.BuildArtifact, error) {
	if strings.TrimSpace(snapshot.GitCommit) == "" {
		return domain.BuildArtifact{}, &BuildError{Err: errors.New("snapshot git commit is required")}
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, nodeBuildTimeout)
	defer cancel()

	root := snapshot.RepositoryPath
	if root == "" {
		root = r.root
	}
	if r.workspace != nil {
		if snapshot.RepositoryPath == "" {
			root = r.workspace.Root()
		}
	}
	workspace, err := os.MkdirTemp("", "constructor-build-")
	if err != nil {
		return domain.BuildArtifact{}, &BuildError{Err: err}
	}
	defer os.RemoveAll(workspace)

	if err := materializeBuildRevision(ctx, root, snapshot.GitCommit, workspace); err != nil {
		return domain.BuildArtifact{}, err
	}
	// Dependency installation and build share one deadline; dependencies are
	// installed from the archived revision, never from the editable checkout.
	if err := installNodeDependenciesWithContext(ctx, workspace); err != nil {
		return domain.BuildArtifact{}, &BuildError{Err: err}
	}
	command := exec.CommandContext(ctx, "npm", "run", "build")
	command.Dir = workspace
	command.Env = projectProcessEnvironment(os.Environ())
	output, err := runBuildCommand(ctx, command)
	if err != nil {
		return domain.BuildArtifact{}, &BuildError{Output: string(output), Err: err}
	}
	if ctx.Err() != nil {
		return domain.BuildArtifact{}, &BuildError{Output: string(output), Err: ctx.Err()}
	}

	dist := filepath.Join(workspace, "dist")
	artifact, err := os.MkdirTemp("", "constructor-artifact-")
	if err != nil {
		return domain.BuildArtifact{}, &BuildError{Err: err}
	}
	if err := copyDirectory(dist, artifact); err != nil {
		_ = os.RemoveAll(artifact)
		return domain.BuildArtifact{}, &BuildError{Err: err}
	}
	checksum, err := directoryChecksum(artifact)
	if err != nil {
		_ = os.RemoveAll(artifact)
		return domain.BuildArtifact{}, &BuildError{Err: err}
	}
	return domain.BuildArtifact{Path: artifact, Checksum: checksum}, nil
}

func materializeBuildRevision(ctx context.Context, repository, revision, destination string) error {
	archive := exec.CommandContext(ctx, "git", "-C", repository, "archive", revision)
	archive.WaitDelay = buildCommandWaitDelay
	configureProcessTree(archive)

	untar := exec.CommandContext(ctx, "tar", "-x", "-f", "-")
	untar.Dir = destination
	untar.WaitDelay = buildCommandWaitDelay
	configureProcessTree(untar)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return &BuildError{Err: err}
	}
	untar.Stdin = pipe
	if err := archive.Start(); err != nil {
		return &BuildError{Err: err}
	}
	output, untarErr := untar.CombinedOutput()
	if untarErr != nil {
		if ctx.Err() != nil {
			killProcessTree(archive)
		}
		_ = archive.Wait()
		return &BuildError{Output: string(output), Err: untarErr}
	}
	if archiveErr := archive.Wait(); archiveErr != nil {
		return &BuildError{Err: archiveErr}
	}
	if ctx.Err() != nil {
		return &BuildError{Err: ctx.Err()}
	}
	return nil
}

func runBuildCommand(ctx context.Context, command *exec.Cmd) ([]byte, error) {
	configureProcessTree(command)
	command.WaitDelay = buildCommandWaitDelay
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		killProcessTree(command)
		return output, ctx.Err()
	}
	return output, err
}

func directoryExists(path string) bool { info, err := os.Stat(path); return err == nil && info.IsDir() }

func copyDirectory(source, destination string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("build output contains a non-regular file")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, openErr := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if openErr != nil {
			_ = input.Close()
			return openErr
		}
		_, copyErr := io.Copy(output, input)
		closeInputErr := input.Close()
		closeOutputErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeInputErr != nil {
			return closeInputErr
		}
		return closeOutputErr
	})
}

func directoryChecksum(root string) (string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("artifact contains a non-regular file")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("build output is empty")
	}
	sort.Strings(files)
	digest := sha256.New()
	if err := hashDirectoryFiles(digest, root, files); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func hashDirectoryFiles(digest hash.Hash, root string, files []string) error {
	var size [8]byte
	for _, relative := range files {
		if _, err := io.WriteString(digest, relative); err != nil {
			return err
		}
		if _, err := digest.Write([]byte{0}); err != nil {
			return err
		}
		file, err := os.Open(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return err
		}
		if !info.Mode().IsRegular() || info.Size() < 0 {
			_ = file.Close()
			return fmt.Errorf("artifact changed while checksumming")
		}
		binary.LittleEndian.PutUint64(size[:], uint64(info.Size()))
		if _, err := digest.Write(size[:]); err != nil {
			_ = file.Close()
			return err
		}
		if _, err := io.Copy(digest, file); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
}

type BuildError struct {
	Output string
	Err    error
}

func (e *BuildError) Error() string {
	if errors.Is(e.Err, context.DeadlineExceeded) {
		return "build timed out"
	}
	if errors.Is(e.Err, context.Canceled) {
		return "build canceled"
	}
	return "build worker failed"
}

func (e *BuildError) Unwrap() error { return e.Err }
