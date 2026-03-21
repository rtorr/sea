package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rtorr/sea/internal/abi"
	"github.com/rtorr/sea/internal/cache"
	"github.com/rtorr/sea/internal/dirs"
	"github.com/rtorr/sea/internal/integrate"
	"github.com/rtorr/sea/internal/pkgconfig"
)

// linkPackage extracts (if needed) and symlinks a package into sea_packages/.
// The sha256Hash identifies the content in the cache.
func linkPackage(c *cache.Cache, seaPkgDir, name, version, sha256Hash, linking string) error {
	extractDir, err := c.Extract(sha256Hash)
	if err != nil {
		return fmt.Errorf("extracting %s@%s: %w", name, version, err)
	}

	pkgInstallDir := filepath.Join(seaPkgDir, name)

	// Remove existing symlink or directory
	if fi, err := os.Lstat(pkgInstallDir); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			os.Remove(pkgInstallDir)
		} else {
			os.RemoveAll(pkgInstallDir)
		}
	}

	if err := os.MkdirAll(seaPkgDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dirs.SeaPackages, err)
	}

	if err := os.Symlink(extractDir, pkgInstallDir); err != nil {
		// On Windows without admin privileges, symlinks may fail.
		// Fall back to copying the directory tree.
		if runtime.GOOS == "windows" {
			if cpErr := copyDirTree(extractDir, pkgInstallDir); cpErr != nil {
				return fmt.Errorf("linking %s (symlink failed: %v, copy failed: %w)", name, err, cpErr)
			}
		} else {
			return fmt.Errorf("linking %s: %w", name, err)
		}
	}

	// Create missing library soname symlinks. Archives don't include symlinks
	// for security, but the linker needs libfoo.dylib → libfoo.1.2.3.dylib.
	createLibSymlinks(filepath.Join(pkgInstallDir, "lib"))

	// Write linking preference file if specified
	if linking == "static" || linking == "shared" {
		if err := os.WriteFile(filepath.Join(pkgInstallDir, dirs.SeaLinking), []byte(linking), 0o644); err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: could not write %s for %s: %v\n", dirs.SeaLinking, name, err)
			}
		}
	} else {
		// Clean up any stale preference file
		if err := os.Remove(filepath.Join(pkgInstallDir, dirs.SeaLinking)); err != nil && !os.IsNotExist(err) {
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: could not remove %s for %s: %v\n", dirs.SeaLinking, name, err)
			}
		}
	}

	// Relocate cmake config files to use relative paths
	// (cmake installs write absolute paths that break when moved)
	integrate.RelocateCMakeConfigs(pkgInstallDir)

	// Auto-generate a .pc file if the package doesn't already have one
	pcDir := filepath.Join(pkgInstallDir, "lib", "pkgconfig")
	if entries, err := os.ReadDir(pcDir); err != nil || len(entries) == 0 {
		_ = pkgconfig.WriteForPackage(pkgInstallDir, name, version)
	}

	return nil
}

// copyDirTree recursively copies src directory to dst.
func copyDirTree(src, dst string) error {
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

		return copyFile(path, target, info.Mode())
	})
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string, mode os.FileMode) error {
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

// createLibSymlinks scans a lib directory and creates missing short-name and
// soname symlinks for versioned shared libraries.
// e.g. libfmt.11.1.4.dylib → libfmt.dylib, libfmt.11.dylib
//
//	liblz4.so.1.10.0     → liblz4.so, liblz4.so.1
//
// For ELF .so files, it also attempts to extract the SONAME from the binary
// and create a symlink with that name if it differs from the filename.
func createLibSymlinks(libDir string) {
	entries, err := os.ReadDir(libDir)
	if err != nil {
		return
	}

	existing := make(map[string]bool)
	for _, e := range entries {
		existing[e.Name()] = true
	}

	for _, e := range entries {
		name := e.Name()

		// On Windows, .dll files don't need soname symlinks.
		// Windows uses PATH for DLL resolution, not symlinks.
		if runtime.GOOS == "windows" && strings.HasSuffix(name, ".dll") {
			continue
		}

		// Try SONAME extraction for .so files (ELF)
		if strings.HasSuffix(name, ".so") || strings.Contains(name, ".so.") {
			soname, err := abi.ExtractSONAME(filepath.Join(libDir, name))
			if err == nil && soname != "" && soname != name && !existing[soname] {
				os.Symlink(name, filepath.Join(libDir, soname))
				existing[soname] = true
			}
		}

		// macOS: libfoo.1.2.3.dylib
		if strings.HasSuffix(name, ".dylib") && strings.Count(name, ".") > 1 {
			base := strings.TrimSuffix(name, ".dylib")
			parts := strings.SplitN(base, ".", 2)
			if len(parts) == 2 {
				shortName := parts[0] + ".dylib"
				if !existing[shortName] {
					os.Symlink(name, filepath.Join(libDir, shortName))
					existing[shortName] = true
				}
				// Also create soname: libfoo.MAJOR.dylib
				soVer := strings.SplitN(parts[1], ".", 2)
				if len(soVer) >= 1 {
					soName := parts[0] + "." + soVer[0] + ".dylib"
					if !existing[soName] {
						os.Symlink(name, filepath.Join(libDir, soName))
						existing[soName] = true
					}
				}
			}
		}

		// Linux: libfoo.so.1.2.3 or libfoo.so.1
		if idx := strings.Index(name, ".so."); idx > 0 {
			baseSo := name[:idx+3] // "libfoo.so"
			if !existing[baseSo] {
				os.Symlink(name, filepath.Join(libDir, baseSo))
				existing[baseSo] = true
			}
			// Soname: libfoo.so.MAJOR
			afterSo := name[idx+4:] // "1.2.3"
			soVer := strings.SplitN(afterSo, ".", 2)
			if len(soVer) >= 1 {
				soName := baseSo + "." + soVer[0] // "libfoo.so.1"
				if !existing[soName] {
					os.Symlink(name, filepath.Join(libDir, soName))
					existing[soName] = true
				}
			}
		}
	}
}
