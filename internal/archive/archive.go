package archive

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	// MaxArchiveSize is the maximum allowed archive size (2 GB).
	MaxArchiveSize = 2 << 30
	// MaxFileSize is the maximum allowed single file size in an archive (1 GB).
	MaxFileSize = 1 << 30
	// MaxFiles is the maximum number of files in an archive.
	MaxFiles = 100_000
)

// Pack creates a .tar.zst archive from the given source directory.
// If includes is non-empty, only files matching at least one pattern are included.
// Patterns use filepath.Match syntax, with "**" interpreted as "any number of directories".
func Pack(srcDir string, includes []string, destPath string) error {
	srcDir, err := filepath.Abs(srcDir)
	if err != nil {
		return fmt.Errorf("resolving source dir: %w", err)
	}

	info, err := os.Stat(srcDir)
	if err != nil {
		return fmt.Errorf("source directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source %s is not a directory", srcDir)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating archive: %w", err)
	}
	defer out.Close()

	zw, err := zstd.NewWriter(out)
	if err != nil {
		return fmt.Errorf("creating zstd writer: %w", err)
	}

	tw := tar.NewWriter(zw)
	fileCount := 0

	walkErr := filepath.Walk(srcDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		// Use forward slashes in the archive (portable)
		relPath = filepath.ToSlash(relPath)

		// Skip symlinks to prevent link bombs
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		// Apply include filters to files (always include directories so we can walk into them)
		if len(includes) > 0 && !fi.IsDir() {
			if !matchAnyPattern(relPath, includes) {
				return nil
			}
		}

		fileCount++
		if fileCount > MaxFiles {
			return fmt.Errorf("too many files (limit %d)", MaxFiles)
		}

		if !fi.IsDir() && fi.Size() > MaxFileSize {
			return fmt.Errorf("file %s exceeds maximum size (%d > %d)", relPath, fi.Size(), MaxFileSize)
		}

		header, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return fmt.Errorf("creating tar header for %s: %w", relPath, err)
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("writing tar header for %s: %w", relPath, err)
		}

		if fi.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("opening %s: %w", relPath, err)
		}
		defer f.Close()

		if _, err := io.Copy(tw, f); err != nil {
			return fmt.Errorf("writing %s to archive: %w", relPath, err)
		}

		return nil
	})

	// Close tar then zstd even on walk error, so the file is properly finalized
	twErr := tw.Close()
	zwErr := zw.Close()

	if walkErr != nil {
		os.Remove(destPath)
		return walkErr
	}
	if twErr != nil {
		os.Remove(destPath)
		return fmt.Errorf("finalizing tar: %w", twErr)
	}
	if zwErr != nil {
		os.Remove(destPath)
		return fmt.Errorf("finalizing zstd: %w", zwErr)
	}

	return nil
}

// Unpack extracts a .tar.zst archive to the given destination directory.
func Unpack(archivePath, destDir string) error {
	destDir, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("resolving dest dir: %w", err)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("opening archive: %w", err)
	}
	defer f.Close()

	// Check file size
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat archive: %w", err)
	}
	if fi.Size() > MaxArchiveSize {
		return fmt.Errorf("archive exceeds maximum size (%d > %d)", fi.Size(), MaxArchiveSize)
	}

	zr, err := zstd.NewReader(f)
	if err != nil {
		return fmt.Errorf("creating zstd reader: %w", err)
	}
	defer zr.Close()

	tr := tar.NewReader(zr)
	fileCount := 0

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		fileCount++
		if fileCount > MaxFiles {
			return fmt.Errorf("archive contains too many entries (limit %d)", MaxFiles)
		}

		// Normalize and validate path
		name := filepath.FromSlash(header.Name)
		target := filepath.Join(destDir, name)
		cleanTarget := filepath.Clean(target)

		// Prevent path traversal
		if !strings.HasPrefix(cleanTarget, destDir+string(os.PathSeparator)) && cleanTarget != destDir {
			return fmt.Errorf("path traversal detected in archive: %q resolves outside destination", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(cleanTarget, 0o755); err != nil {
				return fmt.Errorf("creating directory %s: %w", name, err)
			}

		case tar.TypeReg:
			if header.Size > MaxFileSize {
				return fmt.Errorf("file %s exceeds maximum size (%d > %d)", name, header.Size, MaxFileSize)
			}

			if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
				return fmt.Errorf("creating parent directory for %s: %w", name, err)
			}

			mode := os.FileMode(header.Mode) & 0o755 // sanitize mode bits
			out, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return fmt.Errorf("creating file %s: %w", name, err)
			}

			// Use LimitReader to enforce size limit
			if _, err := io.Copy(out, io.LimitReader(tr, MaxFileSize+1)); err != nil {
				out.Close()
				return fmt.Errorf("extracting %s: %w", name, err)
			}
			out.Close()

		case tar.TypeSymlink, tar.TypeLink:
			// Skip links in package archives for security
			continue

		default:
			// Skip unknown types
			continue
		}
	}

	return nil
}

// matchAnyPattern checks if relPath matches any of the given patterns.
// Supports filepath.Match patterns and "**" as a recursive wildcard.
func matchAnyPattern(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchPattern(relPath, pattern) {
			return true
		}
	}
	return false
}

// matchPattern matches a path against a single pattern.
// "**" matches any number of path components.
func matchPattern(path, pattern string) bool {
	// Normalize to forward slashes for consistency
	path = filepath.ToSlash(path)
	pattern = filepath.ToSlash(pattern)

	// Fast path: no double-star
	if !strings.Contains(pattern, "**") {
		matched, _ := filepath.Match(pattern, path)
		return matched
	}

	// Split on "**" and match segments
	parts := strings.Split(pattern, "**")
	if len(parts) == 2 {
		prefix := parts[0]
		suffix := strings.TrimPrefix(parts[1], "/")

		// Check if path starts with prefix
		if prefix != "" && !strings.HasPrefix(path, prefix) {
			return false
		}

		// If suffix is empty, any path under prefix matches
		if suffix == "" {
			return true
		}

		// Check if any suffix of the remaining path matches the suffix pattern
		remaining := path
		if prefix != "" {
			remaining = strings.TrimPrefix(path, prefix)
		}

		// Try matching suffix against each possible subpath
		pathParts := strings.Split(remaining, "/")
		for i := 0; i < len(pathParts); i++ {
			candidate := strings.Join(pathParts[i:], "/")
			if matched, _ := filepath.Match(suffix, candidate); matched {
				return true
			}
			// Also try matching just the filename against the suffix
			if matched, _ := filepath.Match(suffix, pathParts[len(pathParts)-1]); matched {
				return true
			}
		}
	}

	return false
}
