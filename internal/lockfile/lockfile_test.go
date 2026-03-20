package lockfile

import (
	"testing"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	lf := &LockFile{
		Version: 1,
		Packages: []LockedPackage{
			{
				Name:     "zlib",
				Version:  "1.3.1",
				ABI:      "linux-x86_64-gcc13-libstdcxx",
				SHA256:   "abc123def456",
				Registry: "local",
				Deps:     []string{},
			},
			{
				Name:     "openssl",
				Version:  "3.2.1",
				ABI:      "linux-x86_64-gcc13-libstdcxx",
				SHA256:   "def456abc123",
				Registry: "local",
				Deps:     []string{"zlib@1.3.1"},
			},
		},
	}

	if err := Save(dir, lf); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded lockfile is nil")
	}
	if loaded.Version != 1 {
		t.Errorf("expected version 1, got %d", loaded.Version)
	}
	if len(loaded.Packages) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(loaded.Packages))
	}
	if loaded.Packages[0].Name != "zlib" {
		t.Errorf("expected first package zlib, got %s", loaded.Packages[0].Name)
	}
}

func TestFind(t *testing.T) {
	lf := &LockFile{
		Packages: []LockedPackage{
			{Name: "zlib", Version: "1.3.1"},
			{Name: "openssl", Version: "3.2.1"},
		},
	}

	if pkg := lf.Find("zlib"); pkg == nil || pkg.Version != "1.3.1" {
		t.Error("Find(zlib) failed")
	}
	if pkg := lf.Find("nonexistent"); pkg != nil {
		t.Error("Find(nonexistent) should return nil")
	}
}

func TestLoadNonExistent(t *testing.T) {
	lf, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load non-existent should not error: %v", err)
	}
	if lf != nil {
		t.Error("expected nil for non-existent lockfile")
	}
}
