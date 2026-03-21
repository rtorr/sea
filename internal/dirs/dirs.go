// Package dirs defines the canonical directory and file names used by sea.
//
// All code should reference these constants instead of hardcoding strings.
// This ensures consistency, makes renaming trivial, and allows newcomers
// to discover all sea's directory conventions in one place.
package dirs

const (
	// SeaPackages is the directory where installed packages are symlinked.
	SeaPackages = "sea_packages"

	// SeaBuild is the directory where build output is placed, keyed by ABI tag.
	SeaBuild = "sea_build"

	// SeaBuildPackages is the directory for build-time dependencies.
	SeaBuildPackages = "sea_build_packages"

	// SrcCache is the directory where downloaded source tarballs are cached.
	SrcCache = "_src"

	// SeaBuildInternal is the cmake-internal build directory used during
	// auto-detected builds (distinct from SeaBuild which holds the install output).
	SeaBuildInternal = "_sea_build"

	// SeaLinking is the per-package file that records linking preference (static/shared).
	SeaLinking = ".sea-linking"

	// CMakeModules is the directory for generated CMake Find modules.
	CMakeModules = ".cmake_modules"
)
