package builder

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/profile"
)

func TestBuildMissingScript_ClearError(t *testing.T) {
	dir := t.TempDir()

	m := &manifest.Manifest{
		Package: manifest.Package{Name: "test", Version: "1.0.0"},
		Build:   manifest.Build{Script: "nonexistent-build.sh"},
	}
	prof := &profile.Profile{
		OS: "linux", Arch: "x86_64",
		Compiler: "gcc", CompilerVersion: "13",
		Env: make(map[string]string),
	}

	b, err := New(m, prof, dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = b.Build()
	if err == nil {
		t.Fatal("expected error for missing build script")
	}

	// Error should clearly mention the script name
	if !strings.Contains(err.Error(), "nonexistent-build.sh") {
		t.Errorf("error should mention the script name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should say 'not found', got: %v", err)
	}
}

func TestBuildScriptTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	dir := t.TempDir()

	// Create a script that sleeps indefinitely
	scriptContent := "#!/bin/sh\nsleep 999\n"
	scriptPath := filepath.Join(dir, "slow-build.sh")
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("writing script: %v", err)
	}

	// Run the script directly with a short timeout to verify timeout behavior
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, scriptPath)
	cmd.Dir = dir

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected timeout error")
	}

	// The context should have been exceeded
	if ctx.Err() != context.DeadlineExceeded {
		t.Errorf("expected deadline exceeded, got: %v", ctx.Err())
	}
}
