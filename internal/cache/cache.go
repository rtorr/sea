package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rtorr/sea/internal/archive"
	"github.com/rtorr/sea/internal/config"
)

// Cache is a content-addressed store for package archives.
//
// Archives are stored by their SHA256 hash. The cache has no concept of
// package names, versions, or ABI tags — those are the caller's concern.
// This design guarantees:
//
//   - If a blob exists at hash H, its content is correct (by definition)
//   - No stale state: ABI tag changes, republishing, or version aliasing
//     cannot cause the cache to serve wrong content
//   - Deduplication: identical content (even across different packages) is
//     stored once
//
// The lockfile maps (name, version) → sha256 and is the source of truth
// for what should be in the cache.
type Cache struct {
	Layout Layout
}

// New creates a new Cache using the configured cache directory.
func New(cfg *config.Config) (*Cache, error) {
	if cfg == nil {
		cfg = &config.Config{}
	}
	dir, err := config.CacheDir(cfg)
	if err != nil {
		return nil, err
	}
	for _, sub := range []string{"blobs", "extracted"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("creating cache directory: %w", err)
		}
	}
	return &Cache{Layout: Layout{Root: dir}}, nil
}

// Has checks if a blob with the given SHA256 hash exists in the cache.
func (c *Cache) Has(sha256Hash string) bool {
	if sha256Hash == "" {
		return false
	}
	fi, err := os.Stat(c.Layout.BlobPath(sha256Hash))
	return err == nil && fi.Size() > 0
}

// IsExtracted checks if a blob has been extracted.
func (c *Cache) IsExtracted(sha256Hash string) bool {
	if sha256Hash == "" {
		return false
	}
	entries, err := os.ReadDir(c.Layout.ExtractDir(sha256Hash))
	return err == nil && len(entries) > 0
}

// Store writes an archive to the cache. The content is hashed as it's
// written, and the file is stored at blobs/{sha256}.tar.zst.
//
// Returns the SHA256 hash of the stored content. If the hash is already
// in the cache, the write is skipped (content-addressing guarantees the
// existing content is identical).
func (c *Cache) Store(src io.Reader) (string, error) {
	lockPath, err := c.acquireLock()
	if err != nil {
		return "", err
	}
	defer c.releaseLock(lockPath)

	blobsDir := c.Layout.BlobsDir()

	// Write to temp file, computing hash as we go
	tmpFile, err := os.CreateTemp(blobsDir, ".sea-download-*")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	h := sha256.New()
	w := io.MultiWriter(tmpFile, h)

	_, copyErr := io.Copy(w, src)
	closeErr := tmpFile.Close()

	if copyErr != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("writing to cache: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("closing temp file: %w", closeErr)
	}

	hash := hex.EncodeToString(h.Sum(nil))
	destPath := c.Layout.BlobPath(hash)

	// If the blob already exists, discard the download (dedup)
	if _, err := os.Stat(destPath); err == nil {
		os.Remove(tmpPath)
		return hash, nil
	}

	// Atomic rename
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("finalizing cache file: %w", err)
	}

	return hash, nil
}

// Extract unpacks a cached blob to extracted/{sha256}/.
// If already extracted, returns the existing path.
func (c *Cache) Extract(sha256Hash string) (string, error) {
	if sha256Hash == "" {
		return "", fmt.Errorf("cannot extract: no hash provided")
	}

	extractDir := c.Layout.ExtractDir(sha256Hash)

	// Already extracted? Return immediately.
	if c.IsExtracted(sha256Hash) {
		return extractDir, nil
	}

	lockPath, err := c.acquireLock()
	if err != nil {
		return "", err
	}
	defer c.releaseLock(lockPath)

	// Double-check after acquiring lock (another process may have extracted)
	if c.IsExtracted(sha256Hash) {
		return extractDir, nil
	}

	archivePath := c.Layout.BlobPath(sha256Hash)
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		return "", fmt.Errorf("blob %s not in cache", sha256Hash)
	}

	// Clean any partial previous extraction
	os.RemoveAll(extractDir)
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return "", fmt.Errorf("creating extract directory: %w", err)
	}

	if err := archive.Unpack(archivePath, extractDir); err != nil {
		os.RemoveAll(extractDir)
		return "", fmt.Errorf("extracting package: %w", err)
	}

	// Verify non-empty
	entries, err := os.ReadDir(extractDir)
	if err != nil || len(entries) == 0 {
		os.RemoveAll(extractDir)
		return "", fmt.Errorf("extracted archive is empty — the archive may be corrupt")
	}

	return extractDir, nil
}

// ExtractPath returns the extraction directory path for a hash.
func (c *Cache) ExtractPath(sha256Hash string) string {
	return c.Layout.ExtractDir(sha256Hash)
}

// Remove deletes a blob and its extraction from the cache.
func (c *Cache) Remove(sha256Hash string) error {
	if sha256Hash == "" {
		return nil
	}
	os.RemoveAll(c.Layout.ExtractDir(sha256Hash))
	os.Remove(c.Layout.BlobPath(sha256Hash))
	return nil
}

// Clean removes all cached blobs and extractions.
func (c *Cache) Clean() error {
	for _, sub := range []string{"blobs", "extracted"} {
		dir := filepath.Join(c.Layout.Root, sub)
		if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", dir, err)
		}
		os.MkdirAll(dir, 0o755)
	}
	return nil
}

// Size returns the total size of the cache in bytes.
func (c *Cache) Size() (int64, error) {
	var total int64
	filepath.Walk(c.Layout.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total, nil
}

// List returns all cached blobs with their hashes and sizes.
func (c *Cache) List() ([]CachedBlob, error) {
	var blobs []CachedBlob

	entries, err := os.ReadDir(c.Layout.BlobsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tar.zst") {
			continue
		}
		hash := strings.TrimSuffix(e.Name(), ".tar.zst")
		fi, err := e.Info()
		size := int64(0)
		if err == nil {
			size = fi.Size()
		}
		blobs = append(blobs, CachedBlob{
			SHA256:    hash,
			Size:      size,
			Extracted: c.IsExtracted(hash),
		})
	}

	return blobs, nil
}

// CachedBlob describes a blob in the content-addressed cache.
type CachedBlob struct {
	SHA256    string
	Size      int64
	Extracted bool
}

// GC removes blobs that are not referenced by any of the given hashes.
// Pass the set of hashes from all lockfiles that should be retained.
func (c *Cache) GC(retain map[string]bool) (removed int, freedBytes int64, err error) {
	blobs, err := c.List()
	if err != nil {
		return 0, 0, err
	}

	for _, blob := range blobs {
		if retain[blob.SHA256] {
			continue
		}
		freedBytes += blob.Size
		if err := c.Remove(blob.SHA256); err != nil {
			return removed, freedBytes, fmt.Errorf("removing %s: %w", blob.SHA256, err)
		}
		removed++
	}

	return removed, freedBytes, nil
}
