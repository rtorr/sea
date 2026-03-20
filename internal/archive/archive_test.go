package archive

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackUnpack(t *testing.T) {
	srcDir := t.TempDir()
	os.MkdirAll(filepath.Join(srcDir, "include"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "include", "foo.h"), []byte("#pragma once\nint foo();\n"), 0o644)
	os.MkdirAll(filepath.Join(srcDir, "lib"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "lib", "libfoo.a"), []byte("fake archive"), 0o644)

	archivePath := filepath.Join(t.TempDir(), "test.tar.zst")
	err := Pack(srcDir, nil, archivePath)
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}

	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatalf("archive not created: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("archive is empty")
	}

	destDir := t.TempDir()
	err = Unpack(archivePath, destDir)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	headerPath := filepath.Join(destDir, "include", "foo.h")
	data, err := os.ReadFile(headerPath)
	if err != nil {
		t.Fatalf("header not extracted: %v", err)
	}
	if string(data) != "#pragma once\nint foo();\n" {
		t.Errorf("header content mismatch: %s", string(data))
	}

	libPath := filepath.Join(destDir, "lib", "libfoo.a")
	data, err = os.ReadFile(libPath)
	if err != nil {
		t.Fatalf("lib not extracted: %v", err)
	}
	if string(data) != "fake archive" {
		t.Errorf("lib content mismatch: %s", string(data))
	}
}

func TestPackWithIncludePatterns(t *testing.T) {
	srcDir := t.TempDir()
	os.MkdirAll(filepath.Join(srcDir, "include"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "include", "foo.h"), []byte("header"), 0o644)
	os.MkdirAll(filepath.Join(srcDir, "lib"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "lib", "libfoo.a"), []byte("lib"), 0o644)
	os.MkdirAll(filepath.Join(srcDir, "src"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "src", "foo.c"), []byte("source"), 0o644)
	os.WriteFile(filepath.Join(srcDir, "LICENSE"), []byte("MIT"), 0o644)
	os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("readme"), 0o644)

	archivePath := filepath.Join(t.TempDir(), "test.tar.zst")
	includes := []string{"include/**", "lib/**", "LICENSE"}
	err := Pack(srcDir, includes, archivePath)
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}

	destDir := t.TempDir()
	err = Unpack(archivePath, destDir)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	// Should include header and lib
	if _, err := os.Stat(filepath.Join(destDir, "include", "foo.h")); err != nil {
		t.Error("include/foo.h should be in archive")
	}
	if _, err := os.Stat(filepath.Join(destDir, "lib", "libfoo.a")); err != nil {
		t.Error("lib/libfoo.a should be in archive")
	}
	if _, err := os.Stat(filepath.Join(destDir, "LICENSE")); err != nil {
		t.Error("LICENSE should be in archive")
	}

	// Should NOT include src or README
	if _, err := os.Stat(filepath.Join(destDir, "src", "foo.c")); err == nil {
		t.Error("src/foo.c should NOT be in archive")
	}
	if _, err := os.Stat(filepath.Join(destDir, "README.md")); err == nil {
		t.Error("README.md should NOT be in archive")
	}
}

func TestPackSourceDirNotFound(t *testing.T) {
	nonexistent := filepath.Join(t.TempDir(), "does-not-exist")
	dest := filepath.Join(t.TempDir(), "test.tar.zst")
	err := Pack(nonexistent, nil, dest)
	if err == nil {
		t.Error("expected error for nonexistent source dir")
	}
}

func TestPackSourceIsFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(f, []byte("hi"), 0o644)
	err := Pack(f, nil, filepath.Join(t.TempDir(), "test.tar.zst"))
	if err == nil {
		t.Error("expected error when source is a file")
	}
}

func TestUnpackNonexistent(t *testing.T) {
	err := Unpack("/nonexistent.tar.zst", t.TempDir())
	if err == nil {
		t.Error("expected error for nonexistent archive")
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		want    bool
	}{
		{"include/foo.h", "include/**", true},
		{"include/sub/foo.h", "include/**", true},
		{"lib/libfoo.a", "lib/**", true},
		{"src/main.c", "include/**", false},
		{"LICENSE", "LICENSE", true},
		{"README.md", "LICENSE", false},
		{"lib/libfoo.so", "lib/*.so", true},
		{"lib/libfoo.a", "lib/*.so", false},
	}

	for _, tt := range tests {
		got := matchPattern(tt.path, tt.pattern)
		if got != tt.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.want)
		}
	}
}

func TestPackageMetaValidation(t *testing.T) {
	meta := &PackageMeta{}
	if err := meta.Validate(); err == nil {
		t.Error("expected validation error for empty meta")
	}

	meta.Package.Name = "foo"
	if err := meta.Validate(); err == nil {
		t.Error("expected validation error for missing version")
	}

	meta.Package.Version = "1.0.0"
	if err := meta.Validate(); err == nil {
		t.Error("expected validation error for missing ABI tag")
	}

	meta.ABI.Tag = "any"
	if err := meta.Validate(); err != nil {
		t.Errorf("expected no error for valid meta, got: %v", err)
	}
}
