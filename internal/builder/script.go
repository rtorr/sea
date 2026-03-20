package builder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const buildScriptTimeout = 30 * time.Minute

// RunScript executes a build script with the given environment.
func RunScript(scriptPath string, env []string, workDir string) error {
	absScript := scriptPath
	if !filepath.IsAbs(scriptPath) {
		absScript = filepath.Join(workDir, scriptPath)
	}

	info, err := os.Stat(absScript)
	if err != nil {
		return fmt.Errorf("build script %q not found: %w", scriptPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("build script %q is a directory, not a file", scriptPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), buildScriptTimeout)
	defer cancel()

	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		// On Windows, use cmd.exe for .bat/.cmd, otherwise try running directly
		ext := filepath.Ext(absScript)
		if ext == ".bat" || ext == ".cmd" {
			cmd = exec.CommandContext(ctx, "cmd", "/c", absScript)
		} else {
			cmd = exec.CommandContext(ctx, absScript)
		}
	} else {
		// On Unix, make executable and run via shell for shebang support
		if err := os.Chmod(absScript, info.Mode()|0o111); err != nil {
			return fmt.Errorf("making script executable: %w", err)
		}
		cmd = exec.CommandContext(ctx, absScript)
	}

	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("build script timed out after 30 minutes")
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("build script exited with code %d", exitErr.ExitCode())
		}
		return fmt.Errorf("build script execution failed: %w", err)
	}

	return nil
}
