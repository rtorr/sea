package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rtorr/sea/internal/archive"
	"github.com/rtorr/sea/internal/config"
)

// Cache manages the local package cache.
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache directory: %w", err)
	}
	return &Cache{Layout: Layout{Root: dir}}, nil
}

// Has checks if a package archive is already cached.
func (c *Cache) Has(name, version, abiTag string) bool {
	path := c.Layout.ArchivePath(name, version, abiTag)
	fi, err := os.Stat(path)
	return err == nil && fi.Size() > 0
}

// IsExtracted checks if a package has been extracted and the extraction looks complete
// (contains at least a marker file or is non-empty).
func (c *Cache) IsExtracted(name, version, abiTag string) bool {
	dir := c.Layout.ExtractDir(name, version, abiTag)
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// Store saves an archive file to the cache and returns its SHA256 hash.
// Uses a temporary file and atomic rename to prevent partial writes.
func (c *Cache) Store(name, version, abiTag string, src io.Reader) (string, error) {
	lockPath, err := c.acquireLock()
	if err != nil {
		return "", err
	}
	defer c.releaseLock(lockPath)

	destPath := c.Layout.ArchivePath(name, version, abiTag)
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	// Write to temp file first, then rename for atomicity
	tmpFile, err := os.CreateTemp(destDir, ".sea-download-*")
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

	// Atomic rename
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("finalizing cache file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyHash checks that a cached archive matches the expected SHA256.
func (c *Cache) VerifyHash(name, version, abiTag, expectedSHA256 string) (bool, error) {
	if expectedSHA256 == "" {
		return true, nil // no hash to verify against
	}

	archivePath := c.Layout.ArchivePath(name, version, abiTag)
	f, err := os.Open(archivePath)
	if err != nil {
		return false, fmt.Errorf("opening cached archive: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, fmt.Errorf("reading cached archive: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	return actual == expectedSHA256, nil
}

// Extract unpacks a cached archive to the extraction directory.
// If extraction already exists and is non-empty, it is removed first to ensure
// a clean extraction.
func (c *Cache) Extract(name, version, abiTag string) (string, error) {
	lockPath, err := c.acquireLock()
	if err != nil {
		return "", err
	}
	defer c.releaseLock(lockPath)

	archivePath := c.Layout.ArchivePath(name, version, abiTag)
	extractDir := c.Layout.ExtractDir(name, version, abiTag)

	// Clean any partial previous extraction
	if err := os.RemoveAll(extractDir); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("cleaning extract directory: %w", err)
	}
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return "", fmt.Errorf("creating extract directory: %w", err)
	}

	if err := archive.Unpack(archivePath, extractDir); err != nil {
		// Clean up on failure so IsExtracted returns false
		os.RemoveAll(extractDir)
		return "", fmt.Errorf("extracting package: %w", err)
	}

	// Verify the extracted directory is non-empty
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		os.RemoveAll(extractDir)
		return "", fmt.Errorf("reading extracted directory: %w", err)
	}
	if len(entries) == 0 {
		os.RemoveAll(extractDir)
		return "", fmt.Errorf("extracted archive is empty — the archive may be corrupt")
	}

	return extractDir, nil
}

// GetExtractDir returns the extract directory path (whether or not it exists).
func (c *Cache) GetExtractDir(name, version, abiTag string) string {
	return c.Layout.ExtractDir(name, version, abiTag)
}

// Remove deletes a specific package from the cache.
func (c *Cache) Remove(name, version, abiTag string) error {
	pkgDir := c.Layout.PackageDir(name, version, abiTag)
	if err := os.RemoveAll(pkgDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing cached package: %w", err)
	}
	return nil
}

// Clean removes all cached packages.
func (c *Cache) Clean() error {
	entries, err := os.ReadDir(c.Layout.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading cache directory: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(c.Layout.Root, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("removing %s: %w", path, err)
		}
	}
	return nil
}

// List returns all cached packages.
func (c *Cache) List() ([]CachedPackage, error) {
	var packages []CachedPackage

	pkgDirs, err := os.ReadDir(c.Layout.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	for _, pkgEntry := range pkgDirs {
		if !pkgEntry.IsDir() {
			continue
		}
		pkgName := pkgEntry.Name()
		verDirs, err := os.ReadDir(filepath.Join(c.Layout.Root, pkgName))
		if err != nil {
			continue
		}
		for _, verEntry := range verDirs {
			if !verEntry.IsDir() {
				continue
			}
			version := verEntry.Name()
			abiDirs, err := os.ReadDir(filepath.Join(c.Layout.Root, pkgName, version))
			if err != nil {
				continue
			}
			for _, abiEntry := range abiDirs {
				if !abiEntry.IsDir() {
					continue
				}
				abiTag := abiEntry.Name()
				archivePath := c.Layout.ArchivePath(pkgName, version, abiTag)
				fi, err := os.Stat(archivePath)
				size := int64(0)
				if err == nil {
					size = fi.Size()
				}
				packages = append(packages, CachedPackage{
					Name:      pkgName,
					Version:   version,
					ABI:       abiTag,
					Size:      size,
					Extracted: c.IsExtracted(pkgName, version, abiTag),
				})
			}
		}
	}

	return packages, nil
}

// CachedPackage describes a package in the cache.
type CachedPackage struct {
	Name      string
	Version   string
	ABI       string
	Size      int64
	Extracted bool
}
