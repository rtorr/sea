package pkgconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PCFile represents a pkg-config .pc file.
type PCFile struct {
	Name        string
	Description string
	Version     string
	Prefix      string   // installation prefix
	IncludeDirs []string // additional include dirs relative to prefix
	LibDirs     []string // additional lib dirs relative to prefix
	Libs        []string // -l flags
	CFlags      []string // extra cflags
}

// Generate creates the .pc file content.
func Generate(pc *PCFile) string {
	var b strings.Builder

	fmt.Fprintf(&b, "prefix=%s\n", pc.Prefix)
	fmt.Fprintln(&b, "includedir=${prefix}/include")
	fmt.Fprintln(&b, "libdir=${prefix}/lib")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Name: %s\n", pc.Name)
	fmt.Fprintf(&b, "Description: %s\n", pc.Description)
	fmt.Fprintf(&b, "Version: %s\n", pc.Version)

	// Build Cflags line
	var cflags []string
	cflags = append(cflags, "-I${includedir}")
	for _, d := range pc.IncludeDirs {
		cflags = append(cflags, fmt.Sprintf("-I${prefix}/%s", d))
	}
	cflags = append(cflags, pc.CFlags...)
	fmt.Fprintf(&b, "Cflags: %s\n", strings.Join(cflags, " "))

	// Build Libs line
	var libs []string
	libs = append(libs, "-L${libdir}")
	for _, d := range pc.LibDirs {
		libs = append(libs, fmt.Sprintf("-L${prefix}/%s", d))
	}
	libs = append(libs, pc.Libs...)
	fmt.Fprintf(&b, "Libs: %s\n", strings.Join(libs, " "))

	return b.String()
}

// WriteForPackage generates a .pc file for an installed package in sea_packages.
// It reads the package metadata and writes a .pc file to lib/pkgconfig/.
func WriteForPackage(pkgDir, name, version string) error {
	// Only generate if the package has an include or lib directory
	hasInclude := dirExists(filepath.Join(pkgDir, "include"))
	hasLib := dirExists(filepath.Join(pkgDir, "lib"))
	if !hasInclude && !hasLib {
		return nil
	}

	// Discover -l flags from libraries in lib/
	var libs []string
	if hasLib {
		libs = discoverLibs(filepath.Join(pkgDir, "lib"))
	}

	pc := &PCFile{
		Name:        name,
		Description: "Installed by sea",
		Version:     version,
		Prefix:      pkgDir,
		Libs:        libs,
	}

	content := Generate(pc)

	pcDir := filepath.Join(pkgDir, "lib", "pkgconfig")
	if err := os.MkdirAll(pcDir, 0o755); err != nil {
		return fmt.Errorf("creating pkgconfig dir: %w", err)
	}

	pcPath := filepath.Join(pcDir, name+".pc")
	if err := os.WriteFile(pcPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", pcPath, err)
	}

	return nil
}

// dirExists returns true if path is an existing directory.
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// discoverLibs scans a lib directory for library files and returns -l flags.
func discoverLibs(libDir string) []string {
	entries, err := os.ReadDir(libDir)
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var libs []string

	for _, e := range entries {
		name := e.Name()
		libName := extractLibNameForPC(name)
		if libName != "" && !seen[libName] {
			seen[libName] = true
			libs = append(libs, fmt.Sprintf("-l%s", libName))
		}
	}

	return libs
}

// extractLibNameForPC extracts the library name from a filename for .pc generation.
// Only returns names for canonical (non-versioned) library files.
func extractLibNameForPC(name string) string {
	// macOS: libfoo.dylib (skip versioned libfoo.1.2.3.dylib)
	if strings.HasPrefix(name, "lib") && strings.HasSuffix(name, ".dylib") {
		base := name[3 : len(name)-6]
		if !strings.Contains(base, ".") {
			return base
		}
		return ""
	}

	// Static: libfoo.a
	if strings.HasPrefix(name, "lib") && strings.HasSuffix(name, ".a") {
		return name[3 : len(name)-2]
	}

	// Linux: libfoo.so (skip versioned libfoo.so.1.2.3)
	if strings.HasPrefix(name, "lib") && strings.HasSuffix(name, ".so") {
		return name[3 : len(name)-3]
	}
	if strings.Contains(name, ".so.") {
		return ""
	}

	// Windows: foo.lib
	if strings.HasSuffix(name, ".lib") {
		return name[:len(name)-4]
	}

	return ""
}
