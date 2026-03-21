package integrate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// RelocateCMakeConfigs rewrites absolute paths in cmake config files
// to use relative paths based on ${CMAKE_CURRENT_LIST_DIR}. This makes
// installed packages relocatable — they work regardless of where
// sea_packages/ is on disk.
//
// The problem: cmake's install() writes config files with the original
// CMAKE_INSTALL_PREFIX baked in as absolute paths. These include:
//   - set(_IMPORT_PREFIX "/build/path/to/package")
//   - IMPORTED_LOCATION "/build/path/to/package/lib/libfoo.so"
//   - IMPORTED_SONAME "/build/path/to/package/lib/libfoo.1.2.3.so"
//
// The fix: find ALL absolute paths in .cmake files and replace any that
// point inside the package with ${CMAKE_CURRENT_LIST_DIR}-relative paths.
// Paths that point outside the package (shouldn't exist, but sometimes
// SONAME contains the full build path) are replaced with just the basename.
func RelocateCMakeConfigs(pkgDir string) {
	cmakeDir := filepath.Join(pkgDir, "lib", "cmake")
	if _, err := os.Stat(cmakeDir); err != nil {
		return // no cmake configs
	}

	absPkgDir, err := filepath.Abs(pkgDir)
	if err != nil {
		return
	}
	// Normalize to forward slashes for cross-platform matching
	absPkgDirSlash := filepath.ToSlash(absPkgDir)

	filepath.Walk(cmakeDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".cmake" {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}

		content := string(data)
		original := content

		// Calculate the relative path from this cmake file's directory to the package root
		relFromCMakeFile, relErr := filepath.Rel(filepath.Dir(path), absPkgDir)
		if relErr != nil {
			return nil
		}
		relFromCMakeFile = filepath.ToSlash(relFromCMakeFile)
		relPrefix := "${CMAKE_CURRENT_LIST_DIR}/" + relFromCMakeFile

		// Strategy 1: Replace the known package directory path (handles most cases)
		// Try both native and forward-slash variants
		for _, prefix := range uniqueStrings(absPkgDir, absPkgDirSlash) {
			if strings.Contains(content, prefix) {
				content = strings.ReplaceAll(content, prefix, relPrefix)
			}
		}

		// Strategy 2: Find remaining absolute paths that look like they belong
		// to a build/install directory. These are paths that weren't caught by
		// strategy 1 because they point to a different location than pkgDir
		// (e.g., SONAME with the original build path from CI).
		//
		// Pattern: quoted absolute path starting with / or drive letter
		absPathRe := regexp.MustCompile(`"(/[^"]+|[A-Za-z]:\\[^"]+)"`)
		content = absPathRe.ReplaceAllStringFunc(content, func(match string) string {
			// Strip quotes
			inner := match[1 : len(match)-1]
			innerSlash := filepath.ToSlash(inner)

			// If it's already relative (contains CMAKE_CURRENT_LIST_DIR), skip
			if strings.Contains(inner, "${") {
				return match
			}

			// If the path points inside the package dir, make it relative
			if strings.HasPrefix(innerSlash, absPkgDirSlash+"/") {
				suffix := innerSlash[len(absPkgDirSlash):]
				return `"` + relPrefix + suffix + `"`
			}

			// Path points outside the package — this is likely a SONAME or
			// build-dir artifact. Extract just the lib/ relative portion if
			// it ends with a library filename pattern.
			if isLibraryPath(innerSlash) {
				// Extract the filename and make it relative to package root
				base := filepath.Base(innerSlash)
				return `"` + relPrefix + "/lib/" + base + `"`
			}

			// Unknown external path — leave it but this shouldn't happen
			// in well-formed cmake configs
			return match
		})

		if content != original {
			os.WriteFile(path, []byte(content), info.Mode())
		}

		return nil
	})
}

// isLibraryPath returns true if the path looks like it points to a library file.
func isLibraryPath(p string) bool {
	base := filepath.Base(p)
	return strings.HasSuffix(base, ".so") ||
		strings.Contains(base, ".so.") ||
		strings.HasSuffix(base, ".dylib") ||
		strings.HasSuffix(base, ".a") ||
		strings.HasSuffix(base, ".lib") ||
		strings.HasSuffix(base, ".dll")
}

// uniqueStrings returns a deduplicated slice of non-empty strings.
func uniqueStrings(ss ...string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range ss {
		if s != "" && !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
