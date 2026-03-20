package cache

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rtorr/sea/internal/config"
)

// errorReader returns some data then errors partway through.
type errorReader struct {
	data    string
	pos     int
	failAt  int
	errOnce sync.Once
}

func (r *errorReader) Read(p []byte) (int, error) {
	if r.pos >= r.failAt {
		return 0, errors.New("simulated read error")
	}
	remaining := r.data[r.pos:]
	n := copy(p, remaining)
	if r.pos+n > r.failAt {
		n = r.failAt - r.pos
	}
	r.pos += n
	if r.pos >= r.failAt {
		return n, errors.New("simulated read error")
	}
	if r.pos >= len(r.data) {
		return n, io.EOF
	}
	return n, nil
}

func TestStoreReaderError_NoPartialFile(t *testing.T) {
	c := newTestCache(t)

	reader := &errorReader{
		data:   "partial data that should not persist on disk after an error occurs during writing",
		failAt: 10,
	}

	_, err := c.Store("broken-pkg", "1.0.0", "any", reader)
	if err == nil {
		t.Fatal("expected error from Store with failing reader")
	}

	// Verify no archive file left behind
	archivePath := c.Layout.ArchivePath("broken-pkg", "1.0.0", "any")
	if _, statErr := os.Stat(archivePath); statErr == nil {
		t.Error("partial archive file should not exist after Store error")
	}

	// Verify no temp files left behind
	dir := filepath.Dir(archivePath)
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".sea-download-") {
				t.Errorf("temp file leaked: %s", e.Name())
			}
		}
	}
}

func TestExtractCorruptedArchive_DirectoryCleaned(t *testing.T) {
	c := newTestCache(t)

	// Store invalid data as if it were an archive
	_, err := c.Store("corrupt-pkg", "1.0.0", "any", strings.NewReader("this is not a valid tar.zst archive"))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Attempt to extract — should fail because data is not a valid archive
	_, err = c.Extract("corrupt-pkg", "1.0.0", "any")
	if err == nil {
		t.Fatal("expected error when extracting corrupted archive")
	}

	// Verify the extract directory was cleaned up
	extractDir := c.Layout.ExtractDir("corrupt-pkg", "1.0.0", "any")
	if _, statErr := os.Stat(extractDir); statErr == nil {
		t.Error("extract directory should be cleaned up after extraction failure")
	}

	// IsExtracted should return false
	if c.IsExtracted("corrupt-pkg", "1.0.0", "any") {
		t.Error("IsExtracted should return false for failed extraction")
	}
}

func TestConcurrentStore_NoCorruption(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{CacheDir: dir}

	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const goroutines = 10
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			data := strings.Repeat("data from goroutine ", 100)
			_, err := c.Store("concurrent-pkg", "1.0.0", "any", strings.NewReader(data))
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	// At least one should succeed; the rest should either succeed or error (not corrupt)
	var successCount int
	for _, err := range errs {
		if err == nil {
			successCount++
		}
	}

	if successCount == 0 {
		t.Error("expected at least one concurrent Store to succeed")
	}

	// Verify the final file is not corrupted (is complete and readable)
	archivePath := c.Layout.ArchivePath("concurrent-pkg", "1.0.0", "any")
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("reading final archive: %v", err)
	}

	expectedData := strings.Repeat("data from goroutine ", 100)
	if string(data) != expectedData {
		t.Error("archived file contents are corrupted")
	}

	// No temp files should remain
	dir2 := filepath.Dir(archivePath)
	entries, _ := os.ReadDir(dir2)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".sea-download-") {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}
