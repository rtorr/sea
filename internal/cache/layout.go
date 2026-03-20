package cache

import (
	"path/filepath"
)

// Layout computes paths within the cache directory.
// Cache layout: {cache_root}/{name}/{version}/{abi_tag}/
type Layout struct {
	Root string
}

// PackageDir returns the cache directory for a specific package version+ABI.
func (l *Layout) PackageDir(name, version, abiTag string) string {
	return filepath.Join(l.Root, name, version, abiTag)
}

// ArchivePath returns the path to the cached .tar.zst archive.
func (l *Layout) ArchivePath(name, version, abiTag string) string {
	filename := name + "-" + version + "-" + abiTag + ".tar.zst"
	return filepath.Join(l.PackageDir(name, version, abiTag), filename)
}

// ExtractDir returns the directory where the package is extracted.
func (l *Layout) ExtractDir(name, version, abiTag string) string {
	return filepath.Join(l.PackageDir(name, version, abiTag), "extracted")
}
