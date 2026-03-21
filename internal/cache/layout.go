package cache

import (
	"path/filepath"
)

// Layout computes paths within the cache directory.
//
// Content-addressed layout:
//
//	{cache_root}/
//	  blobs/
//	    {sha256}.tar.zst         — archive keyed by content hash
//	  extracted/
//	    {sha256}/                — extracted files keyed by same hash
//	      include/
//	      lib/
//	      ...
//
// The cache stores archives by their SHA256 hash, not by name/version/ABI.
// This means:
//   - Same content is never stored twice (deduplication)
//   - If the hash matches, the content is correct (no stale state)
//   - ABI tag changes don't invalidate the cache
//   - Republishing with the same content is free
//
// The lockfile (sea.lock) maps name@version → sha256, and the cache
// maps sha256 → files. The cache has no concept of package names.
type Layout struct {
	Root string
}

// BlobPath returns the path to a cached archive by its SHA256 hash.
func (l *Layout) BlobPath(sha256 string) string {
	return filepath.Join(l.Root, "blobs", sha256+".tar.zst")
}

// ExtractDir returns the directory for extracted package files by SHA256.
func (l *Layout) ExtractDir(sha256 string) string {
	return filepath.Join(l.Root, "extracted", sha256)
}

// BlobsDir returns the blobs directory.
func (l *Layout) BlobsDir() string {
	return filepath.Join(l.Root, "blobs")
}

// ExtractedDir returns the top-level extracted directory.
func (l *Layout) ExtractedDir() string {
	return filepath.Join(l.Root, "extracted")
}
