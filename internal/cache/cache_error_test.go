package cache

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// errorReader is an io.Reader that always returns an error.
type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, errors.New("simulated read error")
}

func TestStoreReadError(t *testing.T) {
	c := testCache(t)

	_, err := c.Store(&errorReader{})
	if err == nil {
		t.Fatal("Store should fail with read error")
	}
}

func TestStoreWriteError(t *testing.T) {
	c := testCache(t)

	// Make blobs dir read-only
	blobsDir := c.Layout.BlobsDir()
	os.Chmod(blobsDir, 0o444)
	defer os.Chmod(blobsDir, 0o755)

	_, err := c.Store(strings.NewReader("data"))
	if err == nil {
		t.Fatal("Store should fail when blobs dir is read-only")
	}
}

func TestExtractMissingBlob(t *testing.T) {
	c := testCache(t)

	_, err := c.Extract("nonexistent")
	if err == nil {
		t.Fatal("Extract should fail for missing blob")
	}
}

func TestExtractBadArchive(t *testing.T) {
	c := testCache(t)

	sha, err := c.Store(strings.NewReader("not an archive"))
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	_, err = c.Extract(sha)
	if err == nil {
		t.Fatal("Extract should fail for invalid archive")
	}

	// Extraction dir should be cleaned up
	if c.IsExtracted(sha) {
		t.Error("Failed extraction should not leave extracted dir")
	}
}

func TestHasEmptyHash(t *testing.T) {
	c := testCache(t)

	if c.Has("") {
		t.Error("Has should return false for empty hash")
	}
}

func TestIsExtractedEmptyHash(t *testing.T) {
	c := testCache(t)

	if c.IsExtracted("") {
		t.Error("IsExtracted should return false for empty hash")
	}
}

func TestExtractEmptyHash(t *testing.T) {
	c := testCache(t)

	_, err := c.Extract("")
	if err == nil {
		t.Error("Extract should fail for empty hash")
	}
}

func TestConcurrentStore(t *testing.T) {
	c := testCache(t)

	// Store the same content from multiple goroutines
	const n = 10
	results := make([]string, n)
	errs := make([]error, n)
	done := make(chan struct{})

	for i := 0; i < n; i++ {
		go func(idx int) {
			results[idx], errs[idx] = c.Store(strings.NewReader("concurrent content"))
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < n; i++ {
		<-done
	}

	// All should succeed with the same hash
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Errorf("goroutine %d failed: %v", i, errs[i])
		}
		if results[i] != results[0] {
			t.Errorf("goroutine %d got different hash: %s != %s", i, results[i], results[0])
		}
	}
}

func TestSizeEmpty(t *testing.T) {
	c := testCache(t)

	size, err := c.Size()
	if err != nil {
		t.Fatalf("Size failed: %v", err)
	}
	if size != 0 {
		t.Errorf("empty cache size = %d, want 0", size)
	}
}

func TestSizeWithContent(t *testing.T) {
	c := testCache(t)

	c.Store(strings.NewReader("some data"))

	size, err := c.Size()
	if err != nil {
		t.Fatalf("Size failed: %v", err)
	}
	if size <= 0 {
		t.Error("cache with content should have size > 0")
	}
}

// Suppress unused import warnings
var _ = io.EOF
var _ = os.Remove
var _ = filepath.Join
