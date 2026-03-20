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

	scriptName := "nonexistent-build.sh"
	if runtime.GOOS == "windows" {
		scriptName = "nonexistent-build.bat"
	}

	m := &manifest.Manifest{
		Package: manifest.Package{Name: "test", Version: "1.0.0"},
		Build:   manifest.Build{Script: scriptName},
	}
	prof := &profile.Profile{
		OS: runtime.GOOS, Arch: "x86_64",
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

	if !strings.Contains(err.Error(), scriptName) {
		t.Errorf("error should mention the script name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should say 'not found', got: %v", err)
	}
}

func TestBuildScriptTimeout(t *testing.T) {
	// Use os.MkdirTemp instead of t.TempDir() because on Windows,
	// the killed process may hold file handles, preventing cleanup.
	dir, err := os.MkdirTemp("", "sea-timeout-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir) // best-effort cleanup

	var scriptPath string
	if runtime.GOOS == "windows" {
		scriptPath = filepath.Join(dir, "slow.bat")
		os.WriteFile(scriptPath, []byte("@echo off\r\nping -n 999 127.0.0.1 >nul\r\n"), 0o644)
	} else {
		scriptPath = filepath.Join(dir, "slow.sh")
		os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 999\n"), 0o755)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, scriptPath)
	cmd.Dir = dir

	runErr := cmd.Run()
	if runErr == nil {
		t.Fatal("expected timeout error")
	}

	if ctx.Err() != context.DeadlineExceeded {
		t.Errorf("expected deadline exceeded, got: %v", ctx.Err())
	}
}
