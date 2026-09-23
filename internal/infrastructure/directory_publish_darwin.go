//go:build darwin

package infrastructure

import "golang.org/x/sys/unix"

func renameDirectoryNoReplaceAt(parent int, source, target string) error {
	return unix.RenameatxNp(parent, source, parent, target, unix.RENAME_EXCL)
}
