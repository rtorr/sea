package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rtorr/sea/internal/archive"
)

func TestFilesystemRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	fs, err := NewFilesystem("test", tmpDir)
	if err != nil {
		t.Fatalf("NewFilesystem: %v", err)
	}

	if fs.Name() != "test" {
		t.Errorf("expected name test, got %s", fs.Name())
	}

	meta := &archive.PackageMeta{
		Package: archive.MetaPackage{
			Name:    "zlib",
			Version: "1.3.1",
			Kind:    "prebuilt",
		},
		ABI: archive.MetaABI{
			Tag: "linux-x86_64-gcc13-libstdcxx",
		},
		Dependencies: []archive.MetaDependency{
			{Name: "bzip2", Version: ">=1.0.0"},
		},
	}

	// Upload
	data := strings.NewReader("fake archive data")
	err = fs.Upload("zlib", "1.3.1", "linux-x86_64-gcc13-libstdcxx", data, meta)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Verify files exist
	archivePath := filepath.Join(tmpDir, "zlib", "1.3.1", "linux-x86_64-gcc13-libstdcxx", "zlib-1.3.1-linux-x86_64-gcc13-libstdcxx.tar.zst")
	if _, err := os.Stat(archivePath); err != nil {
		t.Errorf("archive not found: %v", err)
	}

	metaPath := filepath.Join(tmpDir, "zlib", "1.3.1", "linux-x86_64-gcc13-libstdcxx", archive.PackageMetaFile)
	if _, err := os.Stat(metaPath); err != nil {
		t.Errorf("meta not found: %v", err)
	}

	// List versions
	versions, err := fs.ListVersions("zlib")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 1 || versions[0] != "1.3.1" {
		t.Errorf("unexpected versions: %v", versions)
	}

	// List ABI tags
	tags, err := fs.ListABITags("zlib", "1.3.1")
	if err != nil {
		t.Fatalf("ListABITags: %v", err)
	}
	if len(tags) != 1 || tags[0] != "linux-x86_64-gcc13-libstdcxx" {
		t.Errorf("unexpected tags: %v", tags)
	}

	// Fetch meta
	fetchedMeta, err := fs.FetchMeta("zlib", "1.3.1", "linux-x86_64-gcc13-libstdcxx")
	if err != nil {
		t.Fatalf("FetchMeta: %v", err)
	}
	if fetchedMeta.Package.Name != "zlib" {
		t.Errorf("unexpected meta name: %s", fetchedMeta.Package.Name)
	}
	if len(fetchedMeta.Dependencies) != 1 || fetchedMeta.Dependencies[0].Name != "bzip2" {
		t.Errorf("unexpected meta dependencies: %v", fetchedMeta.Dependencies)
	}

	// Download
	rc, err := fs.Download("zlib", "1.3.1", "linux-x86_64-gcc13-libstdcxx")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	rc.Close()

	// Search
	results, err := fs.Search("zlib")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Name != "zlib" {
		t.Errorf("unexpected search results: %v", results)
	}
}

func TestFilesystemNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	fs, _ := NewFilesystem("test", tmpDir)

	versions, err := fs.ListVersions("nonexistent")
	if err != nil {
		t.Fatalf("ListVersions should not error for missing package: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("expected empty versions, got %v", versions)
	}
}

func TestFilesystemVersionValidation(t *testing.T) {
	tmpDir := t.TempDir()
	fs, _ := NewFilesystem("test", tmpDir)

	// Create some directories that are NOT valid versions
	pkgDir := filepath.Join(tmpDir, "mylib")
	os.MkdirAll(filepath.Join(pkgDir, "1.0.0"), 0o755)
	os.MkdirAll(filepath.Join(pkgDir, "not-a-version"), 0o755)
	os.MkdirAll(filepath.Join(pkgDir, "2.0.0-beta.1"), 0o755)

	versions, err := fs.ListVersions("mylib")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	// Should include 1.0.0 and 2.0.0-beta.1, but NOT "not-a-version"
	if len(versions) != 2 {
		t.Errorf("expected 2 valid versions, got %d: %v", len(versions), versions)
	}
}

func TestFilesystemABITagValidation(t *testing.T) {
	tmpDir := t.TempDir()
	fs, _ := NewFilesystem("test", tmpDir)

	// Create ABI dirs, but only one has metadata
	abiDir := filepath.Join(tmpDir, "mylib", "1.0.0", "linux-x86_64-gcc13-libstdcxx")
	os.MkdirAll(abiDir, 0o755)
	os.WriteFile(filepath.Join(abiDir, archive.PackageMetaFile), []byte("[package]\nname=\"mylib\"\nversion=\"1.0.0\"\n[abi]\ntag=\"linux-x86_64-gcc13-libstdcxx\""), 0o644)

	// Create another ABI dir without metadata
	os.MkdirAll(filepath.Join(tmpDir, "mylib", "1.0.0", "empty-tag"), 0o755)

	tags, err := fs.ListABITags("mylib", "1.0.0")
	if err != nil {
		t.Fatalf("ListABITags: %v", err)
	}
	if len(tags) != 1 || tags[0] != "linux-x86_64-gcc13-libstdcxx" {
		t.Errorf("expected only tag with metadata, got: %v", tags)
	}
}

func TestMultiSearch(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	fs1, _ := NewFilesystem("reg1", dir1)
	fs2, _ := NewFilesystem("reg2", dir2)

	// Create a package in each
	meta := &archive.PackageMeta{
		Package: archive.MetaPackage{Name: "zlib", Version: "1.0.0"},
		ABI:     archive.MetaABI{Tag: "any"},
	}
	fs1.Upload("zlib", "1.0.0", "any", strings.NewReader("data"), meta)
	meta2 := &archive.PackageMeta{
		Package: archive.MetaPackage{Name: "zlib", Version: "2.0.0"},
		ABI:     archive.MetaABI{Tag: "any"},
	}
	fs2.Upload("zlib", "2.0.0", "any", strings.NewReader("data"), meta2)

	multi := &Multi{registries: []Registry{fs1, fs2}}
	results, err := multi.Search("zlib")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Should be deduplicated
	if len(results) != 1 {
		t.Fatalf("expected 1 deduplicated result, got %d", len(results))
	}
}
