package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rtorr/sea/internal/archive"
	"github.com/rtorr/sea/internal/dirs"
	"github.com/rtorr/sea/internal/registry"
)

func TestParsePackageArg(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantVer  string
	}{
		{"zlib@1.3.1", "zlib", "=1.3.1"},
		{"zlib", "zlib", "*"},
		{"my-lib@2.0.0-beta.1", "my-lib", "=2.0.0-beta.1"},
	}
	for _, tt := range tests {
		name, ver := parsePackageArg(tt.input)
		if name != tt.wantName || ver != tt.wantVer {
			t.Errorf("parsePackageArg(%q) = (%q, %q), want (%q, %q)",
				tt.input, name, ver, tt.wantName, tt.wantVer)
		}
	}
}

func TestExtractLibName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"libz.so", "z"},
		{"libz.so.1.3.1", ""}, // versioned — skipped, symlink should exist
		{"libssl.dylib", "ssl"},
		{"libcrypto.a", "crypto"},
		{"foo.lib", "foo"},
		{"libfoo.so.1", ""}, // versioned — skipped
		{"random.txt", ""},
		{"lib.so", ""},              // no lib name after "lib" prefix
		{"libfmt.11.1.4.dylib", ""}, // versioned dylib — skipped
		{"libfmt.dylib", "fmt"},     // short name — included
	}
	for _, tt := range tests {
		got := extractLibName(tt.input)
		if got != tt.want {
			t.Errorf("extractLibName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIntegrationInstallFromFilesystem(t *testing.T) {
	// Set up a local filesystem registry with a test package
	registryDir := t.TempDir()
	projectDir := t.TempDir()

	// Create a fake archive for zlib
	srcDir := t.TempDir()
	os.MkdirAll(filepath.Join(srcDir, "include"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "include", "zlib.h"), []byte("#pragma once\n"), 0o644)
	os.MkdirAll(filepath.Join(srcDir, "lib"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "lib", "libz.a"), []byte("fake lib"), 0o644)

	archiveDir := t.TempDir()
	archivePath := filepath.Join(archiveDir, "zlib-1.3.1-any.tar.zst")
	if err := archive.Pack(srcDir, nil, archivePath); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	// Upload to filesystem registry
	fs, err := registry.NewFilesystem("test-reg", registryDir)
	if err != nil {
		t.Fatalf("NewFilesystem: %v", err)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	meta := &archive.PackageMeta{
		Package: archive.MetaPackage{Name: "zlib", Version: "1.3.1", Kind: "prebuilt"},
		ABI:     archive.MetaABI{Tag: "any"},
		Contents: archive.MetaContents{
			IncludeDirs: []string{"include"},
			LibDirs:     []string{"lib"},
		},
	}
	if err := fs.Upload("zlib", "1.3.1", "any", f, meta); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	f.Close()

	// Create sea.toml in project
	seaToml := `[package]
name = "test-project"
version = "1.0.0"

[dependencies]
zlib = { version = ">=1.0.0" }
`
	os.WriteFile(filepath.Join(projectDir, "sea.toml"), []byte(seaToml), 0o644)

	// Verify the registry has the package
	versions, err := fs.ListVersions("zlib")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}

	tags, err := fs.ListABITags("zlib", "1.3.1")
	if err != nil {
		t.Fatalf("ListABITags: %v", err)
	}
	if len(tags) != 1 || tags[0] != "any" {
		t.Fatalf("expected [any], got %v", tags)
	}

	// Verify search works
	results, err := fs.Search("zlib")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}

	t.Logf("Integration test setup complete: registry at %s, project at %s", registryDir, projectDir)
}

func TestIsLibrary(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"libz.so", true},
		{"libz.so.1.2.3", true},
		{"libssl.dylib", true},
		{"libz.a", true},
		{"foo.dll", true},
		{"foo.lib", true},
		{"foo.txt", false},
		{"foo.o", false},
		{"README.md", false},
	}
	for _, tt := range tests {
		got := isLibrary(tt.name)
		if got != tt.want {
			t.Errorf("isLibrary(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestExtractLibNameEdgeCases(t *testing.T) {
	// lib.so has no library name after stripping prefix
	got := extractLibName("lib.so")
	if got != "" {
		t.Errorf("extractLibName(\"lib.so\") = %q, want empty", got)
	}

	// libfoo-bar.so should work with hyphens
	got = extractLibName("libfoo-bar.so")
	if got != "foo-bar" {
		t.Errorf("extractLibName(\"libfoo-bar.so\") = %q, want \"foo-bar\"", got)
	}

	// Windows .lib files
	got = extractLibName("zlib.lib")
	if got != "zlib" {
		t.Errorf("extractLibName(\"zlib.lib\") = %q, want \"zlib\"", got)
	}

	// Static libs always return the name
	got = extractLibName("libboost_system.a")
	if got != "boost_system" {
		t.Errorf("extractLibName(\"libboost_system.a\") = %q, want \"boost_system\"", got)
	}
}

func TestFormatDeps(t *testing.T) {
	// We can't import resolver in the test without a cycle, but we can test
	// that the function handles empty inputs correctly.
	deps := formatDeps(nil, nil)
	if len(deps) != 0 {
		t.Errorf("expected empty deps, got %v", deps)
	}

	deps = formatDeps([]string{"missing"}, nil)
	if len(deps) != 0 {
		t.Errorf("expected empty deps for missing packages, got %v", deps)
	}
}

func TestExtractLibNameVersionedSkipped(t *testing.T) {
	// Versioned names should return "" — only the short canonical name counts.
	// sea install creates symlinks for the short names.
	versioned := []string{"libz.so.1", "libz.so.1.2.3", "libfmt.11.1.4.dylib"}
	for _, name := range versioned {
		got := extractLibName(name)
		if got != "" {
			t.Errorf("extractLibName(%q) = %q, want \"\" (versioned should be skipped)", name, got)
		}
	}

	// Short names should be returned
	short := map[string]string{"libz.so": "z", "libz.dylib": "z", "libz.a": "z"}
	for name, want := range short {
		got := extractLibName(name)
		if got != want {
			t.Errorf("extractLibName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestExtractLibNameWindowsDllAndLib(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Windows .dll files return "" (not used for -l flags)
		{"zlib.dll", ""},
		{"libcrypto-3-x64.dll", ""},
		// Windows .lib files return the name for -l flags
		{"zlib.lib", "zlib"},
		{"libcrypto.lib", "libcrypto"},
		{"boost_system.lib", "boost_system"},
		// Mixed: .lib with lib prefix
		{"libz.lib", "libz"},
	}
	for _, tt := range tests {
		got := extractLibName(tt.input)
		if got != tt.want {
			t.Errorf("extractLibName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLinkingPreferenceBehavior(t *testing.T) {
	// Test that readLinkingPref correctly reads preference files
	dir := t.TempDir()

	// No preference file — returns empty
	pref := readLinkingPref(dir)
	if pref != "" {
		t.Errorf("expected empty preference, got %q", pref)
	}

	// Write "static" preference
	os.WriteFile(filepath.Join(dir, dirs.SeaLinking), []byte("static"), 0o644)
	pref = readLinkingPref(dir)
	if pref != "static" {
		t.Errorf("expected \"static\", got %q", pref)
	}

	// Write "shared" preference
	os.WriteFile(filepath.Join(dir, dirs.SeaLinking), []byte("shared"), 0o644)
	pref = readLinkingPref(dir)
	if pref != "shared" {
		t.Errorf("expected \"shared\", got %q", pref)
	}

	// Write invalid preference — returns empty
	os.WriteFile(filepath.Join(dir, dirs.SeaLinking), []byte("invalid"), 0o644)
	pref = readLinkingPref(dir)
	if pref != "" {
		t.Errorf("expected empty for invalid preference, got %q", pref)
	}

	// Write preference with trailing whitespace — should be trimmed
	os.WriteFile(filepath.Join(dir, dirs.SeaLinking), []byte("static\n"), 0o644)
	pref = readLinkingPref(dir)
	if pref != "static" {
		t.Errorf("expected \"static\" with trimmed whitespace, got %q", pref)
	}
}
