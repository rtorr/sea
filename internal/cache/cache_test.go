package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rtorr/sea/internal/config"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{CacheDir: dir}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestStoreAndRetrieve(t *testing.T) {
	c := newTestCache(t)

	if c.Has("zlib", "1.3.1", "linux-x86_64-gcc13") {
		t.Fatal("should not have zlib yet")
	}

	sha, err := c.Store("zlib", "1.3.1", "linux-x86_64-gcc13", strings.NewReader("fake archive data"))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if sha == "" {
		t.Error("SHA should not be empty")
	}

	if !c.Has("zlib", "1.3.1", "linux-x86_64-gcc13") {
		t.Fatal("should have zlib after store")
	}
}

func TestVerifyHash(t *testing.T) {
	c := newTestCache(t)

	sha, err := c.Store("zlib", "1.3.1", "any", strings.NewReader("test data"))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	ok, err := c.VerifyHash("zlib", "1.3.1", "any", sha)
	if err != nil {
		t.Fatalf("VerifyHash: %v", err)
	}
	if !ok {
		t.Error("hash should match")
	}

	ok, err = c.VerifyHash("zlib", "1.3.1", "any", "wrong-hash")
	if err != nil {
		t.Fatalf("VerifyHash: %v", err)
	}
	if ok {
		t.Error("hash should not match")
	}

	// Empty expected hash should pass
	ok, err = c.VerifyHash("zlib", "1.3.1", "any", "")
	if err != nil {
		t.Fatalf("VerifyHash: %v", err)
	}
	if !ok {
		t.Error("empty hash should pass")
	}
}

func TestRemove(t *testing.T) {
	c := newTestCache(t)
	c.Store("zlib", "1.3.1", "any", strings.NewReader("data"))

	if !c.Has("zlib", "1.3.1", "any") {
		t.Fatal("should have zlib")
	}

	if err := c.Remove("zlib", "1.3.1", "any"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if c.Has("zlib", "1.3.1", "any") {
		t.Error("should not have zlib after remove")
	}

	// Remove non-existent should not error
	if err := c.Remove("nonexistent", "1.0.0", "any"); err != nil {
		t.Errorf("Remove non-existent should not error: %v", err)
	}
}

func TestClean(t *testing.T) {
	c := newTestCache(t)
	c.Store("zlib", "1.3.1", "any", strings.NewReader("data1"))
	c.Store("openssl", "3.2.0", "any", strings.NewReader("data2"))

	if err := c.Clean(); err != nil {
		t.Fatalf("Clean: %v", err)
	}

	if c.Has("zlib", "1.3.1", "any") || c.Has("openssl", "3.2.0", "any") {
		t.Error("cache should be empty after clean")
	}
}

func TestList(t *testing.T) {
	c := newTestCache(t)
	c.Store("zlib", "1.3.1", "linux-x86_64-gcc13", strings.NewReader("data1"))
	c.Store("openssl", "3.2.0", "linux-x86_64-gcc13", strings.NewReader("data2"))

	packages, err := c.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(packages) != 2 {
		t.Errorf("expected 2 packages, got %d", len(packages))
	}

	found := map[string]bool{}
	for _, p := range packages {
		found[p.Name] = true
		if p.Size == 0 {
			t.Errorf("package %s size should be > 0", p.Name)
		}
	}
	if !found["zlib"] || !found["openssl"] {
		t.Error("missing expected packages")
	}
}

func TestAtomicStore(t *testing.T) {
	c := newTestCache(t)

	// Verify the archive is written atomically (no partial files)
	archivePath := c.Layout.ArchivePath("test-pkg", "1.0.0", "any")

	// Store should create the final file only on success
	_, err := c.Store("test-pkg", "1.0.0", "any", strings.NewReader("complete data"))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("reading archive: %v", err)
	}
	if string(data) != "complete data" {
		t.Errorf("unexpected data: %s", string(data))
	}

	// No temp files should remain
	dir := filepath.Dir(archivePath)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".sea-download-") {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}

func TestSize(t *testing.T) {
	c := newTestCache(t)
	c.Store("zlib", "1.0.0", "any", strings.NewReader("hello world"))

	size, err := c.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size == 0 {
		t.Error("size should be > 0")
	}
}

func TestNilConfig(t *testing.T) {
	// Should not panic with nil config
	_, err := New(nil)
	if err != nil {
		t.Fatalf("New(nil) should not error: %v", err)
	}
}
