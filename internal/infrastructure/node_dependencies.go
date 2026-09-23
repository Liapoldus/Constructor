package infrastructure

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const dependencyInstallTimeout = 5 * time.Minute

func installNodeDependencies(root string) error {
	ctx, cancel := context.WithTimeout(context.Background(), dependencyInstallTimeout)
	defer cancel()
	return installNodeDependenciesWithContext(ctx, root)
}

func installNodeDependenciesWithContext(ctx context.Context, root string) error {
	if directoryExists(filepath.Join(root, "node_modules")) {
		return nil
	}
	args := []string{"install", "--no-audit", "--no-fund"}
	if _, err := os.Stat(filepath.Join(root, "package-lock.json")); err == nil {
		args = []string{"ci", "--no-audit", "--no-fund"}
	} else if !os.IsNotExist(err) {
		return err
	}
	command := exec.CommandContext(ctx, "npm", args...)
	configureProcessTree(command)
	command.WaitDelay = 2 * time.Second
	command.Dir = root
	command.Env = projectProcessEnvironment(os.Environ())
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		killProcessTree(command)
	}
	if err != nil {
		return fmt.Errorf("install project dependencies: %w: %s", err, output)
	}
	return nil
}

func projectProcessEnvironment(parent []string) []string {
	allowed := map[string]bool{
		"PATH": true, "HOME": true, "USER": true, "LOGNAME": true,
		"TMPDIR": true, "TMP": true, "TEMP": true, "LANG": true,
		"LC_ALL": true, "LC_CTYPE": true, "SYSTEMROOT": true,
		"WINDIR": true, "COMSPEC": true, "PATHEXT": true,
		"USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true,
	}
	result := make([]string, 0, len(allowed))
	for _, entry := range parent {
		name, _, ok := strings.Cut(entry, "=")
		if ok && allowed[strings.ToUpper(name)] {
			result = append(result, entry)
		}
	}
	return result
}
