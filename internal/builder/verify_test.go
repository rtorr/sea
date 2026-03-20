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

func newTestProfile() *profile.Profile {
	return &profile.Profile{
		OS: runtime.GOOS, Arch: "x86_64",
		Compiler: "gcc", CompilerVersion: "13",
		Env: make(map[string]string),
	}
}

func TestVerifyPassesWithHeadersAndStaticLib(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "include"), 0o755)
	os.WriteFile(filepath.Join(dir, "include", "foo.h"), []byte("#pragma once\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "lib"), 0o755)
	os.WriteFile(filepath.Join(dir, "lib", "libfoo.a"), []byte("!<arch>\n"), 0o644)

	m := &manifest.Manifest{}
	err := VerifyBuildOutput(m, newTestProfile(), dir, dir)
	if err != nil {
		t.Fatalf("expected pass: %v", err)
	}
}

func TestVerifyFailsNoHeaders(t *testing.T) {
	dir := t.TempDir()
	// lib but no include
	os.MkdirAll(filepath.Join(dir, "lib"), 0o755)
	os.WriteFile(filepath.Join(dir, "lib", "libfoo.a"), []byte("data"), 0o644)

	m := &manifest.Manifest{}
	err := VerifyBuildOutput(m, newTestProfile(), dir, dir)
	if err == nil {
		t.Fatal("expected failure for missing headers")
	}
	if !strings.Contains(err.Error(), "headers") {
		t.Errorf("error should mention headers: %v", err)
	}
}

func TestVerifyFailsNoLibs(t *testing.T) {
	dir := t.TempDir()
	// include but no lib
	os.MkdirAll(filepath.Join(dir, "include"), 0o755)
	os.WriteFile(filepath.Join(dir, "include", "foo.h"), []byte("#pragma once\n"), 0o644)

	m := &manifest.Manifest{}
	err := VerifyBuildOutput(m, newTestProfile(), dir, dir)
	if err == nil {
		t.Fatal("expected failure for missing libraries")
	}
	if !strings.Contains(err.Error(), "libraries") {
		t.Errorf("error should mention libraries: %v", err)
	}
}

func TestVerifyOptionalTestProgram(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test program compilation not tested on Windows")
	}

	dir := t.TempDir()
	installDir := t.TempDir()

	// Create a header-only library
	os.MkdirAll(filepath.Join(installDir, "include"), 0o755)
	os.WriteFile(filepath.Join(installDir, "include", "add.h"), []byte(`
#ifndef ADD_H
#define ADD_H
static inline int add(int a, int b) { return a + b; }
#endif
`), 0o644)
	os.MkdirAll(filepath.Join(installDir, "lib"), 0o755)
	os.WriteFile(filepath.Join(installDir, "lib", "libstub.a"), []byte("!<arch>\n"), 0o644)

	// Test source
	os.WriteFile(filepath.Join(dir, "test.c"), []byte(`
#include <add.h>
int main() { return add(2,3) == 5 ? 0 : 1; }
`), 0o644)

	m := &manifest.Manifest{Build: manifest.Build{Test: "test.c"}}
	err := VerifyBuildOutput(m, newTestProfile(), dir, installDir)
	if err != nil {
		t.Fatalf("test program should pass: %v", err)
	}
}

func TestVerifyNoTestNoConfig(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "include"), 0o755)
	os.WriteFile(filepath.Join(dir, "include", "x.h"), []byte("//\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "lib"), 0o755)
	os.WriteFile(filepath.Join(dir, "lib", "libx.a"), []byte("data"), 0o644)

	// No test, no config — should pass with just automatic checks
	m := &manifest.Manifest{}
	err := VerifyBuildOutput(m, newTestProfile(), dir, dir)
	if err != nil {
		t.Fatalf("should pass with no test configured: %v", err)
	}
}

func TestDetectBuildSystem(t *testing.T) {
	// CMake
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CMakeLists.txt"), []byte("cmake_minimum_required(VERSION 3.0)"), 0o644)
	if got := DetectBuildSystem(dir, ""); got != BuildCMake {
		t.Errorf("expected CMake, got %s", got)
	}

	// Makefile
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir2, "Makefile"), []byte("all:"), 0o644)
	if got := DetectBuildSystem(dir2, ""); got != BuildMakefile {
		t.Errorf("expected Makefile, got %s", got)
	}

	// Meson
	dir3 := t.TempDir()
	os.WriteFile(filepath.Join(dir3, "meson.build"), []byte("project('x')"), 0o644)
	if got := DetectBuildSystem(dir3, ""); got != BuildMeson {
		t.Errorf("expected Meson, got %s", got)
	}

	// Explicit script overrides
	if got := DetectBuildSystem(dir, "build.sh"); got != BuildScript {
		t.Errorf("expected Script, got %s", got)
	}

	// Nothing
	dir4 := t.TempDir()
	if got := DetectBuildSystem(dir4, ""); got != BuildUnknown {
		t.Errorf("expected Unknown, got %s", got)
	}
}
