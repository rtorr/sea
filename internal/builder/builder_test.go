package builder

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/profile"
)

func TestNewValidation(t *testing.T) {
	m := &manifest.Manifest{Package: manifest.Package{Name: "test", Version: "1.0.0"}}
	prof := &profile.Profile{OS: "linux", Arch: "x86_64", Compiler: "gcc", CompilerVersion: "13"}

	tmpDir := t.TempDir()

	_, err := New(nil, prof, tmpDir)
	if err == nil {
		t.Error("expected error for nil manifest")
	}

	_, err = New(m, nil, tmpDir)
	if err == nil {
		t.Error("expected error for nil profile")
	}

	_, err = New(m, prof, "")
	if err == nil {
		t.Error("expected error for empty dir")
	}

	b, err := New(m, prof, tmpDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	abs, _ := filepath.Abs(tmpDir)
	if b.ProjectDir != abs {
		t.Errorf("expected %s, got %s", abs, b.ProjectDir)
	}
}

func TestBuildEnv(t *testing.T) {
	m := &manifest.Manifest{
		Package: manifest.Package{Name: "test-lib", Version: "1.2.3"},
		Build: manifest.Build{
			Env: map[string]string{"CUSTOM_FLAG": "ON"},
		},
	}
	prof := &profile.Profile{
		OS: "linux", Arch: "x86_64",
		Compiler: "gcc", CompilerVersion: "13",
		CppStdlib: "libstdc++", BuildType: "release",
		CFlags: "-O2", CXXFlags: "-O2 -std=c++17",
		Env: map[string]string{"CC": "gcc-13"},
	}

	env := BuildEnv(m, prof, "/project", "/install")

	envMap := make(map[string]string)
	for _, e := range env {
		if idx := strings.Index(e, "="); idx > 0 {
			envMap[e[:idx]] = e[idx+1:]
		}
	}

	checks := map[string]string{
		"SEA_PACKAGE_NAME":    "test-lib",
		"SEA_PACKAGE_VERSION": "1.2.3",
		"SEA_OS":              "linux",
		"SEA_ARCH":            "x86_64",
		"SEA_COMPILER":        "gcc",
		"SEA_BUILD_TYPE":      "release",
		"SEA_PROJECT_DIR":     "/project",
		"SEA_INSTALL_DIR":     "/install",
		"SEA_CFLAGS":          "-O2",
		"SEA_CXXFLAGS":        "-O2 -std=c++17",
		"CUSTOM_FLAG":         "ON",
		"CC":                  "gcc-13",
	}

	for k, want := range checks {
		if got, ok := envMap[k]; !ok {
			t.Errorf("missing env var %s", k)
		} else if got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

// writeTestScript creates a platform-appropriate build script.
// On Unix: shell script. On Windows: .bat file.
func writeTestScript(t *testing.T, dir, name, unixContent, winContent string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, name+".bat")
		os.WriteFile(path, []byte(winContent), 0o644)
		return name + ".bat"
	}
	path := filepath.Join(dir, name+".sh")
	os.WriteFile(path, []byte(unixContent), 0o755)
	return name + ".sh"
}

func TestBuildScript(t *testing.T) {
	dir := t.TempDir()

	scriptName := writeTestScript(t, dir, "build",
		"#!/bin/sh\nmkdir -p \"$SEA_INSTALL_DIR/lib\"\nmkdir -p \"$SEA_INSTALL_DIR/include\"\ntouch \"$SEA_INSTALL_DIR/lib/libtest.a\"\ntouch \"$SEA_INSTALL_DIR/include/test.h\"\n",
		"@echo off\r\nmkdir \"%SEA_INSTALL_DIR%\\lib\" 2>nul\r\nmkdir \"%SEA_INSTALL_DIR%\\include\" 2>nul\r\necho. > \"%SEA_INSTALL_DIR%\\lib\\libtest.a\"\r\necho. > \"%SEA_INSTALL_DIR%\\include\\test.h\"\r\n",
	)

	m := &manifest.Manifest{
		Package: manifest.Package{Name: "test", Version: "1.0.0"},
		Build:   manifest.Build{Script: scriptName},
	}
	prof := &profile.Profile{
		OS: runtime.GOOS, Arch: "x86_64",
		Compiler: "gcc", CompilerVersion: "13",
		CppStdlib: "libstdc++", BuildType: "release",
		Env: make(map[string]string),
	}

	b, err := New(m, prof, dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	installDir, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	markerPath := filepath.Join(installDir, "lib", "libtest.a")
	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("build script did not create expected output: %v", err)
	}
}

func TestBuildScriptFailure(t *testing.T) {
	dir := t.TempDir()

	scriptName := writeTestScript(t, dir, "build",
		"#!/bin/sh\nexit 1\n",
		"@echo off\r\nexit /b 1\r\n",
	)

	m := &manifest.Manifest{
		Package: manifest.Package{Name: "test", Version: "1.0.0"},
		Build:   manifest.Build{Script: scriptName},
	}
	prof := &profile.Profile{
		OS: runtime.GOOS, Arch: "x86_64",
		Compiler: "gcc", CompilerVersion: "13",
		Env: make(map[string]string),
	}

	b, _ := New(m, prof, dir)
	_, err := b.Build()
	if err == nil {
		t.Fatal("expected build failure")
	}
	errStr := strings.ToLower(err.Error())
	if !strings.Contains(errStr, "exit") && !strings.Contains(errStr, "fail") {
		t.Errorf("error should mention exit/fail: %v", err)
	}
}

func TestBuildNoScript(t *testing.T) {
	m := &manifest.Manifest{
		Package: manifest.Package{Name: "test", Version: "1.0.0"},
	}
	prof := &profile.Profile{
		OS: runtime.GOOS, Arch: "x86_64",
		Compiler: "gcc", CompilerVersion: "13",
		Env: make(map[string]string),
	}

	b, _ := New(m, prof, t.TempDir())
	_, err := b.Build()
	if err == nil {
		t.Fatal("expected error for missing build script and no build system")
	}
}
