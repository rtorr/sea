package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rtorr/sea/internal/dirs"
	"github.com/spf13/cobra"
)

const seaPkgDirName = dirs.SeaPackages

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Output build environment variables for installed packages",
	Long: `Output build environment variables for installed packages.

Examples:
  sea env cflags          # -I flags for all installed packages
  sea env ldflags         # -L and -l flags
  sea env cmake-prefix    # CMAKE_PREFIX_PATH value
  sea env pkg-config-path # PKG_CONFIG_PATH value`,
}

var envCflagsCmd = &cobra.Command{
	Use:     "cflags",
	Aliases: []string{"--cflags"},
	Short:   "Output -I include flags",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		flags, err := getCflags(dir)
		if err != nil {
			return err
		}
		fmt.Print(flags)
		if flags != "" {
			fmt.Println()
		}
		return nil
	},
}

var envLdflagsCmd = &cobra.Command{
	Use:     "ldflags",
	Aliases: []string{"--ldflags"},
	Short:   "Output -L and -l link flags",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		flags, err := getLdflags(dir)
		if err != nil {
			return err
		}
		fmt.Print(flags)
		if flags != "" {
			fmt.Println()
		}
		return nil
	},
}

var envCmakePrefixCmd = &cobra.Command{
	Use:     "cmake-prefix",
	Aliases: []string{"--cmake-prefix"},
	Short:   "Output CMAKE_PREFIX_PATH",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		seaPkgDir := filepath.Join(dir, seaPkgDirName)
		if _, err := os.Stat(seaPkgDir); os.IsNotExist(err) {
			return fmt.Errorf("no %s directory found — run 'sea install' first", seaPkgDirName)
		}
		fmt.Println(seaPkgDir)
		return nil
	},
}

var envPkgConfigCmd = &cobra.Command{
	Use:     "pkg-config-path",
	Aliases: []string{"--pkg-config-path"},
	Short:   "Output PKG_CONFIG_PATH",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		seaPkgDir := filepath.Join(dir, seaPkgDirName)
		entries, err := os.ReadDir(seaPkgDir)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("no %s directory found — run 'sea install' first", seaPkgDirName)
			}
			return fmt.Errorf("reading %s: %w", seaPkgDirName, err)
		}
		var paths []string
		for _, e := range entries {
			pkgPath := filepath.Join(seaPkgDir, e.Name())
			fi, err := os.Stat(pkgPath)
			if err != nil || !fi.IsDir() {
				continue
			}
			pc := filepath.Join(pkgPath, "lib", "pkgconfig")
			if _, err := os.Stat(pc); err == nil {
				paths = append(paths, pc)
			}
		}
		fmt.Println(strings.Join(paths, ":"))
		return nil
	},
}

var envShellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Output all environment variables for eval",
	Long: `Output all sea environment variables as shell export statements.

Usage:
  eval "$(sea env shell)"             # bash/zsh
  eval "$(sea env shell --format fish)" # fish

Outputs SEA_CFLAGS, SEA_LDFLAGS, SEA_CMAKE_PREFIX_PATH, and SEA_PKG_CONFIG_PATH.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}

		format, _ := cmd.Flags().GetString("format")

		cflags, _ := getCflags(dir)
		ldflags, _ := getLdflags(dir)

		seaPkgDir := filepath.Join(dir, seaPkgDirName)
		cmakePrefix := ""
		if _, err := os.Stat(seaPkgDir); err == nil {
			cmakePrefix = seaPkgDir
		}

		var pkgConfigPaths []string
		if entries, err := os.ReadDir(seaPkgDir); err == nil {
			for _, e := range entries {
				pkgPath := filepath.Join(seaPkgDir, e.Name())
				fi, err := os.Stat(pkgPath)
				if err != nil || !fi.IsDir() {
					continue
				}
				pc := filepath.Join(pkgPath, "lib", "pkgconfig")
				if _, err := os.Stat(pc); err == nil {
					pkgConfigPaths = append(pkgConfigPaths, pc)
				}
			}
		}
		pkgConfigPath := strings.Join(pkgConfigPaths, ":")

		switch format {
		case "fish":
			fmt.Printf("set -gx SEA_CFLAGS %q\n", cflags)
			fmt.Printf("set -gx SEA_LDFLAGS %q\n", ldflags)
			fmt.Printf("set -gx SEA_CMAKE_PREFIX_PATH %q\n", cmakePrefix)
			fmt.Printf("set -gx SEA_PKG_CONFIG_PATH %q\n", pkgConfigPath)
		default: // bash
			fmt.Printf("export SEA_CFLAGS=%q\n", cflags)
			fmt.Printf("export SEA_LDFLAGS=%q\n", ldflags)
			fmt.Printf("export SEA_CMAKE_PREFIX_PATH=%q\n", cmakePrefix)
			fmt.Printf("export SEA_PKG_CONFIG_PATH=%q\n", pkgConfigPath)
		}
		return nil
	},
}

var envCmakeToolchainCmd = &cobra.Command{
	Use:   "cmake-toolchain",
	Short: "Output a CMake toolchain file for sea_packages",
	Long: `Generate a CMake toolchain file to stdout that configures CMAKE_PREFIX_PATH,
CMAKE_INCLUDE_PATH, and CMAKE_LIBRARY_PATH for all installed sea packages.

Usage:
  sea env cmake-toolchain > sea-toolchain.cmake
  cmake -DCMAKE_TOOLCHAIN_FILE=sea-toolchain.cmake ..`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		content, err := generateCMakeToolchain(dir)
		if err != nil {
			return err
		}
		fmt.Print(content)
		return nil
	},
}

func init() {
	envCmd.AddCommand(envCflagsCmd)
	envCmd.AddCommand(envLdflagsCmd)
	envCmd.AddCommand(envCmakePrefixCmd)
	envCmd.AddCommand(envPkgConfigCmd)
	envCmd.AddCommand(envCmakeToolchainCmd)
	envCmd.AddCommand(envShellCmd)

	envShellCmd.Flags().String("format", "bash", "output format: bash or fish")
}

func getCflags(dir string) (string, error) {
	seaPkgDir := filepath.Join(dir, seaPkgDirName)
	entries, err := os.ReadDir(seaPkgDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no %s directory found — run 'sea install' first", seaPkgDirName)
		}
		return "", fmt.Errorf("reading %s: %w", seaPkgDirName, err)
	}

	var flags []string
	for _, e := range entries {
		// Use Stat (not Lstat) to follow symlinks
		pkgPath := filepath.Join(seaPkgDir, e.Name())
		fi, err := os.Stat(pkgPath)
		if err != nil || !fi.IsDir() {
			continue
		}
		includeDir := filepath.Join(pkgPath, "include")
		if _, err := os.Stat(includeDir); err == nil {
			flags = append(flags, fmt.Sprintf("-I%s", includeDir))
		}
	}
	sort.Strings(flags)
	return strings.Join(flags, " "), nil
}

func getLdflags(dir string) (string, error) {
	seaPkgDir := filepath.Join(dir, seaPkgDirName)
	entries, err := os.ReadDir(seaPkgDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no %s directory found — run 'sea install' first", seaPkgDirName)
		}
		return "", fmt.Errorf("reading %s: %w", seaPkgDirName, err)
	}

	var libDirs []string
	seen := make(map[string]bool)
	var libs []string

	for _, e := range entries {
		pkgPath := filepath.Join(seaPkgDir, e.Name())
		fi, err := os.Stat(pkgPath)
		if err != nil || !fi.IsDir() {
			continue
		}

		// Read linking preference written by install
		linkingPref := readLinkingPref(pkgPath)

		libDir := filepath.Join(pkgPath, "lib")
		libEntries, err := os.ReadDir(libDir)
		if err != nil {
			continue
		}
		libDirs = append(libDirs, fmt.Sprintf("-L%s", libDir))

		for _, le := range libEntries {
			name := le.Name()
			// If static preference, only emit .a files
			if linkingPref == "static" && !strings.HasSuffix(name, ".a") && !strings.HasSuffix(name, ".lib") {
				continue
			}
			// If shared preference, skip .a files
			if linkingPref == "shared" && (strings.HasSuffix(name, ".a") || strings.HasSuffix(name, ".lib")) {
				continue
			}
			libName := extractLibName(name)
			if libName != "" && !seen[libName] {
				seen[libName] = true
				libs = append(libs, fmt.Sprintf("-l%s", libName))
			}
		}
	}

	sort.Strings(libDirs)
	sort.Strings(libs)
	parts := append(libDirs, libs...)
	return strings.Join(parts, " "), nil
}

// readLinkingPref reads the .sea-linking preference file from a package directory.
// Returns "static", "shared", or "" (prefer shared).
func readLinkingPref(pkgDir string) string {
	data, err := os.ReadFile(filepath.Join(pkgDir, dirs.SeaLinking))
	if err != nil {
		return ""
	}
	pref := strings.TrimSpace(string(data))
	if pref == "static" || pref == "shared" {
		return pref
	}
	return ""
}

// extractLibName extracts the library name from a filename, stripping version
// numbers. Only matches the canonical short name — skips versioned duplicates
// to avoid emitting -lfoo twice.
//
// libfoo.dylib        → "foo"     (include)
// libfoo.11.1.4.dylib → ""       (skip — versioned, symlink should exist)
// libfoo.a            → "foo"     (include)
// libfoo.so           → "foo"     (include)
// libfoo.so.1.2.3     → ""       (skip — versioned)
// foo.lib             → "foo"     (Windows)
func extractLibName(name string) string {
	// macOS: only match libfoo.dylib (not libfoo.1.2.3.dylib)
	if strings.HasPrefix(name, "lib") && strings.HasSuffix(name, ".dylib") {
		base := name[3 : len(name)-6] // strip "lib" and ".dylib"
		if !strings.Contains(base, ".") {
			return base
		}
		return "" // versioned dylib, skip
	}

	// Static: libfoo.a
	if strings.HasPrefix(name, "lib") && strings.HasSuffix(name, ".a") {
		return name[3 : len(name)-2]
	}

	// Linux: only match libfoo.so (not libfoo.so.1.2.3)
	if strings.HasPrefix(name, "lib") && strings.HasSuffix(name, ".so") {
		return name[3 : len(name)-3]
	}
	// Skip versioned .so.X.Y.Z
	if strings.Contains(name, ".so.") {
		return ""
	}

	// Windows: foo.lib
	if strings.HasSuffix(name, ".lib") {
		return name[:len(name)-4]
	}

	return ""
}

// generateCMakeToolchain creates a CMake toolchain file that sets up paths
// for all installed sea packages.
func generateCMakeToolchain(dir string) (string, error) {
	seaPkgDir := filepath.Join(dir, seaPkgDirName)
	entries, err := os.ReadDir(seaPkgDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no %s directory found — run 'sea install' first", seaPkgDirName)
		}
		return "", fmt.Errorf("reading %s: %w", seaPkgDirName, err)
	}

	var b strings.Builder

	b.WriteString("# Generated by sea env cmake-toolchain\n")
	b.WriteString("# Usage: cmake -DCMAKE_TOOLCHAIN_FILE=<this file> ..\n\n")

	// Set CMAKE_PREFIX_PATH to sea_packages/
	fmt.Fprintf(&b, "list(APPEND CMAKE_PREFIX_PATH \"%s\")\n\n", seaPkgDir)

	var includePaths []string
	var libraryPaths []string

	for _, e := range entries {
		pkgPath := filepath.Join(seaPkgDir, e.Name())
		fi, err := os.Stat(pkgPath)
		if err != nil || !fi.IsDir() {
			continue
		}

		includeDir := filepath.Join(pkgPath, "include")
		if _, err := os.Stat(includeDir); err == nil {
			includePaths = append(includePaths, includeDir)
		}

		libDir := filepath.Join(pkgPath, "lib")
		if _, err := os.Stat(libDir); err == nil {
			libraryPaths = append(libraryPaths, libDir)
		}
	}

	sort.Strings(includePaths)
	sort.Strings(libraryPaths)

	for _, p := range includePaths {
		fmt.Fprintf(&b, "list(APPEND CMAKE_INCLUDE_PATH \"%s\")\n", p)
	}
	if len(includePaths) > 0 {
		b.WriteString("\n")
	}

	for _, p := range libraryPaths {
		fmt.Fprintf(&b, "list(APPEND CMAKE_LIBRARY_PATH \"%s\")\n", p)
	}
	if len(libraryPaths) > 0 {
		b.WriteString("\n")
	}

	return b.String(), nil
}
