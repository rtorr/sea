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

	_, err := New(nil, prof, "/tmp")
	if err == nil {
		t.Error("expected error for nil manifest")
	}

	_, err = New(m, nil, "/tmp")
	if err == nil {
		t.Error("expected error for nil profile")
	}

	_, err = New(m, prof, "")
	if err == nil {
		t.Error("expected error for empty dir")
	}

	b, err := New(m, prof, "/tmp")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.ProjectDir != "/tmp" {
		t.Errorf("expected /tmp, got %s", b.ProjectDir)
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

func TestBuildScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	dir := t.TempDir()

	// Create a simple build script that writes a marker file
	scriptContent := "#!/bin/sh\nmkdir -p \"$SEA_INSTALL_DIR/lib\"\ntouch \"$SEA_INSTALL_DIR/lib/marker\"\n"
	scriptPath := filepath.Join(dir, "build.sh")
	os.WriteFile(scriptPath, []byte(scriptContent), 0o755)

	m := &manifest.Manifest{
		Package: manifest.Package{Name: "test", Version: "1.0.0"},
		Build:   manifest.Build{Script: "build.sh"},
	}
	prof := &profile.Profile{
		OS: "linux", Arch: "x86_64",
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

	// Verify the marker file was created
	markerPath := filepath.Join(installDir, "lib", "marker")
	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("build script did not create expected output: %v", err)
	}
}

func TestBuildScriptFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	dir := t.TempDir()

	scriptPath := filepath.Join(dir, "build.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 1\n"), 0o755)

	m := &manifest.Manifest{
		Package: manifest.Package{Name: "test", Version: "1.0.0"},
		Build:   manifest.Build{Script: "build.sh"},
	}
	prof := &profile.Profile{
		OS: "linux", Arch: "x86_64",
		Compiler: "gcc", CompilerVersion: "13",
		Env: make(map[string]string),
	}

	b, _ := New(m, prof, dir)
	_, err := b.Build()
	if err == nil {
		t.Fatal("expected build failure")
	}
	if !strings.Contains(err.Error(), "exit") {
		t.Errorf("error should mention exit code: %v", err)
	}
}

func TestBuildNoScript(t *testing.T) {
	m := &manifest.Manifest{
		Package: manifest.Package{Name: "test", Version: "1.0.0"},
	}
	prof := &profile.Profile{
		OS: "linux", Arch: "x86_64",
		Compiler: "gcc", CompilerVersion: "13",
		Env: make(map[string]string),
	}

	b, _ := New(m, prof, t.TempDir())
	_, err := b.Build()
	if err == nil {
		t.Fatal("expected error for missing build script")
	}
}
