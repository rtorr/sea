package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/rtorr/sea/internal/dirs"
)

// BuildCache manages cached build artifacts keyed by package identity and source hash.
// Cache key format: {package_name}-{version}-{abi_tag}-{source_hash}
type BuildCache struct {
	Root string // root directory for build cache storage
}

// NewBuildCache creates a BuildCache under the given cache root.
func NewBuildCache(cacheRoot string) (*BuildCache, error) {
	dir := filepath.Join(cacheRoot, "build-cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating build cache directory: %w", err)
	}
	return &BuildCache{Root: dir}, nil
}

// Key returns the cache key for a build artifact.
func (bc *BuildCache) Key(pkgName, version, abiTag, sourceHash string) string {
	return fmt.Sprintf("%s-%s-%s-%s", pkgName, version, abiTag, sourceHash)
}

// Dir returns the directory path for a cache key.
func (bc *BuildCache) Dir(key string) string {
	return filepath.Join(bc.Root, key)
}

// Has checks whether a cached build exists for the given key.
func (bc *BuildCache) Has(key string) bool {
	dir := bc.Dir(key)
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// Retrieve copies a cached build output into destDir. Returns true if cache hit.
func (bc *BuildCache) Retrieve(key, destDir string) (bool, error) {
	srcDir := bc.Dir(key)
	entries, err := os.ReadDir(srcDir)
	if err != nil || len(entries) == 0 {
		return false, nil
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return false, fmt.Errorf("creating destination directory: %w", err)
	}

	if err := copyTree(srcDir, destDir); err != nil {
		return false, fmt.Errorf("copying cached build: %w", err)
	}
	return true, nil
}

// Store copies the build output from srcDir into the cache under the given key.
func (bc *BuildCache) Store(key, srcDir string) error {
	destDir := bc.Dir(key)
	// Remove any existing entry
	if err := os.RemoveAll(destDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cleaning build cache entry: %w", err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating build cache entry: %w", err)
	}
	if err := copyTree(srcDir, destDir); err != nil {
		return fmt.Errorf("storing build output: %w", err)
	}
	return nil
}

// Clean removes the entire build cache.
func (bc *BuildCache) Clean() error {
	if err := os.RemoveAll(bc.Root); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cleaning build cache: %w", err)
	}
	return nil
}

// ComputeSourceHash computes a SHA256 hash from the build script content and
// all source files in the project directory. The hash is deterministic: files
// are sorted by path before hashing.
func ComputeSourceHash(projectDir string, buildScript string) (string, error) {
	h := sha256.New()

	// Hash the build script content
	scriptPath := buildScript
	if !filepath.IsAbs(scriptPath) {
		scriptPath = filepath.Join(projectDir, scriptPath)
	}
	scriptData, err := os.ReadFile(scriptPath)
	if err != nil {
		return "", fmt.Errorf("reading build script: %w", err)
	}
	h.Write(scriptData)

	// Collect source files (skip build output and cache dirs)
	var files []string
	skipDirs := map[string]bool{
		dirs.SeaBuild:         true,
		dirs.SeaPackages:      true,
		dirs.SeaBuildPackages: true,
		".git":                true,
	}

	err = filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if skipDirs[base] && path != projectDir {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(projectDir, path)
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walking source directory: %w", err)
	}

	sort.Strings(files)

	for _, rel := range files {
		// Hash the relative path for determinism
		h.Write([]byte(rel))
		data, err := os.ReadFile(filepath.Join(projectDir, rel))
		if err != nil {
			continue // skip unreadable files
		}
		h.Write(data)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyTree recursively copies a directory tree.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyOneFile(path, target, info.Mode())
	})
}

// copyOneFile copies a single file.
func copyOneFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
