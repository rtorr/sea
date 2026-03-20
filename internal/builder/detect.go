package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BuildSystem identifies how to build a project.
type BuildSystem int

const (
	BuildUnknown   BuildSystem = iota
	BuildCMake                 // CMakeLists.txt
	BuildMakefile              // Makefile or GNUmakefile
	BuildMeson                 // meson.build
	BuildAutotools             // configure or configure.ac
	BuildScript                // explicit build.sh / build script from sea.toml
)

func (b BuildSystem) String() string {
	switch b {
	case BuildCMake:
		return "CMake"
	case BuildMakefile:
		return "Makefile"
	case BuildMeson:
		return "Meson"
	case BuildAutotools:
		return "Autotools"
	case BuildScript:
		return "script"
	default:
		return "unknown"
	}
}

// DetectBuildSystem examines a project directory and returns the build system.
// If a build script is specified in sea.toml, that takes priority.
func DetectBuildSystem(projectDir, manifestScript string) BuildSystem {
	if manifestScript != "" {
		return BuildScript
	}

	// Check in priority order
	checks := []struct {
		file   string
		system BuildSystem
	}{
		{"CMakeLists.txt", BuildCMake},
		{"meson.build", BuildMeson},
		{"GNUmakefile", BuildMakefile},
		{"Makefile", BuildMakefile},
		{"makefile", BuildMakefile},
		{"configure", BuildAutotools},
		{"configure.ac", BuildAutotools},
	}

	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(projectDir, c.file)); err == nil {
			return c.system
		}
	}

	return BuildUnknown
}

// GenerateBuildCommands returns the shell commands to build and install
// for a detected build system. installDir is the target prefix.
// seaPackagesDir, if non-empty, is added to CMAKE_PREFIX_PATH so cmake
// can find dependencies installed by sea.
func GenerateBuildCommands(system BuildSystem, projectDir, installDir, cc, cxx, cflags, cxxflags string, seaPackagesDirs ...string) ([][]string, error) {
	switch system {
	case BuildCMake:
		buildDir := filepath.Join(projectDir, "_sea_build")
		cmakeArgs := []string{
			"cmake", projectDir,
			"-B", buildDir,
			"-DCMAKE_INSTALL_PREFIX=" + installDir,
			"-DCMAKE_BUILD_TYPE=Release",
			"-DBUILD_SHARED_LIBS=ON",
		}
		// Only set compiler if explicitly configured — let CMake auto-detect on Windows/MSVC
		if cc != "" {
			cmakeArgs = append(cmakeArgs, "-DCMAKE_C_COMPILER="+cc)
		}
		if cxx != "" {
			cmakeArgs = append(cmakeArgs, "-DCMAKE_CXX_COMPILER="+cxx)
		}
		// Add sea_packages directories to CMAKE_PREFIX_PATH so deps are found
		if len(seaPackagesDirs) > 0 {
			var prefixPaths []string
			for _, dir := range seaPackagesDirs {
				if dir == "" {
					continue
				}
				// Add each installed package's root as a prefix path
				entries, err := os.ReadDir(dir)
				if err == nil {
					for _, e := range entries {
						pkgPath := filepath.Join(dir, e.Name())
						if fi, err := os.Stat(pkgPath); err == nil && fi.IsDir() {
							prefixPaths = append(prefixPaths, pkgPath)
						}
					}
				}
			}
			if len(prefixPaths) > 0 {
				cmakeArgs = append(cmakeArgs, "-DCMAKE_PREFIX_PATH="+strings.Join(prefixPaths, ";"))
			}
		}
		return [][]string{
			cmakeArgs,
			{"cmake", "--build", buildDir, "--config", "Release", "-j"},
			{"cmake", "--install", buildDir, "--config", "Release"},
		}, nil

	case BuildMakefile:
		makeArgs := []string{"make", "-j"}
		if cc != "" {
			makeArgs = append(makeArgs, "CC="+cc)
		}
		if cxx != "" {
			makeArgs = append(makeArgs, "CXX="+cxx)
		}
		installArgs := []string{"make", "install", "PREFIX=" + installDir}
		if cc != "" {
			installArgs = append(installArgs, "CC="+cc)
		}
		return [][]string{makeArgs, installArgs}, nil

	case BuildMeson:
		buildDir := filepath.Join(projectDir, "_sea_build")
		setupArgs := []string{
			"meson", "setup", buildDir, projectDir,
			"--prefix=" + installDir,
			"--buildtype=release",
		}
		return [][]string{
			setupArgs,
			{"meson", "compile", "-C", buildDir},
			{"meson", "install", "-C", buildDir},
		}, nil

	case BuildAutotools:
		configureArgs := []string{"./configure", "--prefix=" + installDir}
		if cc != "" {
			configureArgs = append(configureArgs, "CC="+cc)
		}
		if cxx != "" {
			configureArgs = append(configureArgs, "CXX="+cxx)
		}
		return [][]string{
			configureArgs,
			{"make", "-j"},
			{"make", "install"},
		}, nil

	default:
		return nil, fmt.Errorf("cannot auto-detect build system — add [build].script to sea.toml")
	}
}
