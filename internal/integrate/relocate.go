package integrate

import (
	"os"
	"path/filepath"
	"strings"
)

// RelocateCMakeConfigs rewrites absolute paths in cmake config files
// to use relative paths based on ${CMAKE_CURRENT_LIST_DIR}. This makes
// installed packages relocatable — they work regardless of where
// sea_packages/ is on disk.
//
// The problem: cmake's install() writes config files with the original
// CMAKE_INSTALL_PREFIX baked in as absolute paths. When sea extracts
// the package to a different location (sea_packages/foo/), those paths
// are wrong.
//
// The fix: replace any absolute path that points inside the package
// with a relative path using ${CMAKE_CURRENT_LIST_DIR}.
func RelocateCMakeConfigs(pkgDir string) {
	cmakeDir := filepath.Join(pkgDir, "lib", "cmake")
	if _, err := os.Stat(cmakeDir); err != nil {
		return // no cmake configs
	}

	// Get the absolute path of the package root for replacement
	absPkgDir, err := filepath.Abs(pkgDir)
	if err != nil {
		return
	}

	// Walk all .cmake files
	filepath.Walk(cmakeDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".cmake" {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}

		content := string(data)
		modified := false

		// Calculate the relative path from this cmake file's directory to the package root
		relFromCMakeFile, relErr := filepath.Rel(filepath.Dir(path), absPkgDir)
		if relErr != nil {
			return nil
		}
		// Normalize to forward slashes for cmake
		relFromCMakeFile = filepath.ToSlash(relFromCMakeFile)

		// Replace absolute paths pointing to the package root with relative paths
		// Common patterns in cmake config files:
		//   set(_IMPORT_PREFIX "/absolute/path/to/package")
		//   IMPORTED_LOCATION "/absolute/path/to/package/lib/libfoo.so"
		//
		// We need to handle both forward and backslash paths (Windows)
		for _, prefix := range []string{absPkgDir, filepath.ToSlash(absPkgDir)} {
			if strings.Contains(content, prefix) {
				replacement := "${CMAKE_CURRENT_LIST_DIR}/" + relFromCMakeFile
				content = strings.ReplaceAll(content, prefix, replacement)
				modified = true
			}
		}

		// Also try with trailing separator variants
		for _, prefix := range []string{
			absPkgDir + string(filepath.Separator),
			filepath.ToSlash(absPkgDir) + "/",
		} {
			replacement := "${CMAKE_CURRENT_LIST_DIR}/" + relFromCMakeFile + "/"
			if strings.Contains(content, prefix) {
				content = strings.ReplaceAll(content, prefix, replacement)
				modified = true
			}
		}

		if modified {
			os.WriteFile(path, []byte(content), info.Mode())
		}

		return nil
	})
}
