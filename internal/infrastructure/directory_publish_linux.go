//go:build linux

package infrastructure

import "golang.org/x/sys/unix"

func renameDirectoryNoReplaceAt(parent int, source, target string) error {
	return unix.Renameat2(parent, source, parent, target, unix.RENAME_NOREPLACE)
}
