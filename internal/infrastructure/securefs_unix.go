//go:build darwin || linux

package infrastructure

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/Liapoldus/Constructor/internal/domain"
)

func openProjectRoot(root string) (*os.File, error) {
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, domain.ErrNotFound
		}
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return nil, domain.ErrInvalidPath
		}
		return nil, err
	}
	return os.NewFile(uintptr(fd), root), nil
}

func openProjectParent(root, clean string, create bool) (*os.File, string, error) {
	current, err := openProjectRoot(root)
	if err != nil {
		return nil, "", err
	}
	parts := strings.Split(filepath.ToSlash(clean), "/")
	for _, part := range parts[:len(parts)-1] {
		fd, openErr := unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if errors.Is(openErr, unix.ENOENT) && create {
			mode := uint32(0755)
			if parts[0] == ".constructor-state" {
				mode = 0700
			}
			mkdirErr := unix.Mkdirat(int(current.Fd()), part, mode)
			if mkdirErr == nil {
				if syncErr := current.Sync(); syncErr != nil {
					_ = current.Close()
					return nil, "", syncErr
				}
			}
			if mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				current.Close()
				return nil, "", mkdirErr
			}
			fd, openErr = unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		}
		if openErr != nil {
			current.Close()
			if errors.Is(openErr, unix.ENOENT) {
				return nil, "", domain.ErrNotFound
			}
			if errors.Is(openErr, unix.ELOOP) || errors.Is(openErr, unix.ENOTDIR) {
				return nil, "", domain.ErrInvalidPath
			}
			return nil, "", openErr
		}
		if parts[0] == ".constructor-state" {
			if chmodErr := unix.Fchmod(fd, 0700); chmodErr != nil {
				_ = unix.Close(fd)
				_ = current.Close()
				return nil, "", chmodErr
			}
		}
		next := os.NewFile(uintptr(fd), part)
		_ = current.Close()
		current = next
	}
	return current, parts[len(parts)-1], nil
}

func publishDirectoryNoReplace(root, stage, target string) error {
	if filepath.Dir(stage) != filepath.Dir(target) {
		return errors.New("staging directory is not a sibling of its target")
	}
	relativeTarget, err := filepath.Rel(root, target)
	if err != nil || relativeTarget == "." || relativeTarget == ".." || strings.HasPrefix(relativeTarget, ".."+string(filepath.Separator)) {
		return domain.ErrInvalidPath
	}
	parent, targetName, err := openProjectParent(root, filepath.ToSlash(relativeTarget), false)
	if err != nil {
		return err
	}
	defer parent.Close()
	return renameDirectoryNoReplaceAt(int(parent.Fd()), filepath.Base(stage), targetName)
}

func readProjectFile(root, clean string) ([]byte, error) {
	parent, name, err := openProjectParent(root, clean, false)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	return readProjectAt(int(parent.Fd()), name)
}

func readProjectAt(parent int, name string) ([]byte, error) {
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, domain.ErrNotFound
		}
		if errors.Is(err, unix.ELOOP) {
			return nil, domain.ErrInvalidPath
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, domain.ErrInvalidPath
	}
	return io.ReadAll(file)
}

func listProjectFiles(root, clean string) ([]string, error) {
	current, err := openProjectRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = current.Close() }()
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		fd, openErr := unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if errors.Is(openErr, unix.ENOENT) {
			return []string{}, nil
		}
		if openErr != nil {
			if errors.Is(openErr, unix.ELOOP) || errors.Is(openErr, unix.ENOTDIR) {
				return nil, domain.ErrInvalidPath
			}
			return nil, openErr
		}
		next := os.NewFile(uintptr(fd), part)
		_ = current.Close()
		current = next
	}
	entries, err := current.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		files = append(files, filepath.ToSlash(filepath.Join(clean, entry.Name())))
	}
	return files, nil
}

func writeProjectFile(root, clean string, content []byte) error {
	parent, name, err := openProjectParent(root, clean, true)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := rejectExistingNonRegular(int(parent.Fd()), name); err != nil {
		return err
	}
	tmp, file, err := createTempAt(int(parent.Fd()), ".constructor-")
	if err != nil {
		return err
	}
	defer unix.Unlinkat(int(parent.Fd()), tmp, 0)
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := unix.Renameat(int(parent.Fd()), tmp, int(parent.Fd()), name); err != nil {
		return err
	}
	return parent.Sync()
}

func removeProjectFile(root, clean string) error {
	parent, name, err := openProjectParent(root, clean, false)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := unix.Unlinkat(int(parent.Fd()), name, 0); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return domain.ErrNotFound
		}
		return err
	}
	return parent.Sync()
}

func writeProjectAt(parent int, name string, content []byte) (string, error) {
	if err := rejectExistingNonRegular(parent, name); err != nil {
		return "", err
	}
	tmp, file, err := createTempAt(parent, ".constructor-")
	if err != nil {
		return "", err
	}
	if _, err := file.Write(content); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = unix.Unlinkat(parent, tmp, 0)
		return "", err
	}
	return tmp, nil
}

func rejectExistingNonRegular(parent int, name string) error {
	var stat unix.Stat_t
	err := unix.Fstatat(parent, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return domain.ErrInvalidPath
	}
	return nil
}

func createTempAt(parent int, prefix string) (string, *os.File, error) {
	for range 8 {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", nil, err
		}
		name := prefix + hex.EncodeToString(random[:])
		fd, err := unix.Openat(parent, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		return name, os.NewFile(uintptr(fd), name), nil
	}
	return "", nil, errors.New("could not allocate a unique project staging file")
}
