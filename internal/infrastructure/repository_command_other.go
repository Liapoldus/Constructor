//go:build !darwin && !linux

package infrastructure

import (
	"context"
	"os/exec"
)

func gitCommand(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "git", args...)
}
