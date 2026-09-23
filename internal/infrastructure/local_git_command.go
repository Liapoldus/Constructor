package infrastructure

import (
	"context"
	"fmt"
	"time"
)

const localGitCommandTimeout = 2 * time.Minute

func runLocalGit(root string, extraEnv []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), localGitCommandTimeout)
	defer cancel()
	return runLocalGitContext(ctx, root, extraEnv, args...)
}

func runLocalGitContext(ctx context.Context, root string, extraEnv []string, args ...string) ([]byte, error) {
	commandContext, cancel := context.WithTimeout(ctx, localGitCommandTimeout)
	defer cancel()
	operation := "command"
	if len(args) > 0 {
		operation = args[0]
	}
	command := gitCommand(commandContext, append([]string{"-C", root}, args...)...)
	command.Env = append(gitEnvironment(), extraEnv...)
	output, err := command.CombinedOutput()
	if err != nil {
		if commandContext.Err() != nil {
			return nil, commandContext.Err()
		}
		return nil, fmt.Errorf("git %s failed: %w", operation, err)
	}
	return output, nil
}
