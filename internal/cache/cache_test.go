package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreAndHas(t *testing.T) {
	c := testCache(t)

	sha, err := c.Store(strings.NewReader("hello world"))
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}
	if sha == "" {
		t.Fatal("Store returned empty hash")
	}
	if !c.Has(sha) {
		t.Error("Has returned false for stored blob")
	}
	if c.Has("nonexistent") {
		t.Error("Has returned true for nonexistent hash")
	}
}

func TestStoreDedup(t *testing.T) {
	c := testCache(t)

	sha1, _ := c.Store(strings.NewReader("same content"))
	sha2, _ := c.Store(strings.NewReader("same content"))

	if sha1 != sha2 {
		t.Errorf("same content produced different hashes: %s != %s", sha1, sha2)
	}
}

func TestClean(t *testing.T) {
	c := testCache(t)

	sha, _ := c.Store(strings.NewReader("data"))
	if !c.Has(sha) {
		t.Fatal("should have blob after store")
	}
	if err := c.Clean(); err != nil {
		t.Fatalf("Clean failed: %v", err)
	}
	if c.Has(sha) {
		t.Error("should not have blob after clean")
	}
}

func TestGC(t *testing.T) {
	c := testCache(t)

	sha1, _ := c.Store(strings.NewReader("keep this"))
	sha2, _ := c.Store(strings.NewReader("remove this"))

	retain := map[string]bool{sha1: true}
	removed, _, err := c.GC(retain)
	if err != nil {
		t.Fatalf("GC failed: %v", err)
	}
	if removed != 1 {
		t.Errorf("GC removed %d blobs, want 1", removed)
	}
	if !c.Has(sha1) {
		t.Error("GC removed retained blob")
	}
	if c.Has(sha2) {
		t.Error("GC didn't remove unreferenced blob")
	}
}

func testCache(t *testing.T) *Cache {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"blobs", "extracted"} {
		os.MkdirAll(filepath.Join(dir, sub), 0o755)
	}
	return &Cache{Layout: Layout{Root: dir}}
}
