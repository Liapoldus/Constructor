//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package infrastructure

import "os/exec"

func configureProcessTree(cmd *exec.Cmd) {}

func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
