package manifest

// Manifest represents a sea.toml project manifest.
type Manifest struct {
	Package      Package               `toml:"package"`
	Dependencies map[string]Dependency `toml:"dependencies"`
	BuildDeps    map[string]Dependency `toml:"build-dependencies"`
	Build        Build                 `toml:"build"`
	Profiles     map[string]ProfileRef `toml:"profiles"`
	Publish      Publish               `toml:"publish"`
	Features     map[string]Feature    `toml:"features,omitempty"`
}

// Feature describes an optional feature that gates additional dependencies.
type Feature struct {
	Dependencies map[string]Dependency `toml:"dependencies,omitempty"`
	Description  string               `toml:"description,omitempty"`
}

// Package describes the package metadata.
type Package struct {
	Name        string   `toml:"name"`
	Version     string   `toml:"version"`
	Channel     string   `toml:"channel,omitempty"` // "stable" (default) | "beta" | "rc" | "dev"
	Description string   `toml:"description,omitempty"`
	License     string   `toml:"license,omitempty"`
	Authors     []string `toml:"authors,omitempty"`
	Kind        string   `toml:"kind"` // "prebuilt" | "source" | "header-only"
}

// ValidChannels lists all valid channel values.
var ValidChannels = []string{"stable", "beta", "rc", "dev"}

// Dependency describes a package dependency.
type Dependency struct {
	Version     string   `toml:"version"`
	Registry    string   `toml:"registry,omitempty"`
	Optional    bool     `toml:"optional,omitempty"`
	ABIOverride string   `toml:"abi_override,omitempty"`
	Linking     string   `toml:"linking,omitempty"`     // "static" | "shared" | "" (any)
	Features    []string `toml:"features,omitempty"`    // list of features to enable on this dep
}

// ValidLinkings lists all valid linking values.
var ValidLinkings = []string{"", "static", "shared"}

// Build describes how to build a source package.
type Build struct {
	Script     string            `toml:"script,omitempty"`
	Visibility string            `toml:"visibility,omitempty"` // "hidden" | "default"
	Env        map[string]string `toml:"env,omitempty"`
	Source     BuildSource       `toml:"source,omitempty"`

	// CMakeArgs are extra CMake arguments passed when auto-detecting CMake.
	// e.g. ["-DENABLE_TESTS=OFF", "-DCMAKE_CXX_STANDARD=17"]
	CMakeArgs []string `toml:"cmake_args,omitempty"`

	// Subdir is a subdirectory within the source archive that contains the
	// build system files. e.g. "build/cmake" for lz4.
	Subdir string `toml:"subdir,omitempty"`

	// Test is a C/C++ source file that sea compiles against the build output,
	// links, and executes after every build. If it compiles, links, and exits 0,
	// the package is valid. This is the only verification you need — symbol
	// extraction and ABI checking happen automatically at publish time.
	//
	// Example:
	//   test = "test/verify.c"
	//
	// The test file is compiled with:
	//   cc -I{install}/include test/verify.c -L{install}/lib -l{libs} -o verify && ./verify
	Test string `toml:"test,omitempty"`
}

// ProfileRef is a reference to an external profile file.
type ProfileRef struct {
	File string `toml:"file"`
}

// Publish describes publishing configuration.
type Publish struct {
	Registry string   `toml:"registry,omitempty"`
	Include  []string `toml:"include,omitempty"`
}

// BuildSource specifies where to download the upstream source code.
// When set, sea downloads and extracts the source automatically — no build.sh needed.
type BuildSource struct {
	// URL is the download URL for the source archive (.tar.gz, .tar.xz, .zip).
	//
	// If Commit is also set and URL contains a GitHub archive pattern
	// (refs/heads/<branch>), the URL is automatically rewritten to use
	// the pinned commit SHA for reproducible builds.
	URL string `toml:"url,omitempty"`

	// Commit pins the source to a specific git commit SHA. This ensures
	// reproducible builds — the same commit always produces the same source.
	//
	// When set with a GitHub URL, sea rewrites the URL from:
	//   https://github.com/org/repo/archive/refs/heads/main.tar.gz
	// to:
	//   https://github.com/org/repo/archive/<commit>.tar.gz
	//
	// If URL is not set but Commit is, sea constructs the URL from the
	// package's known repository (if any).
	Commit string `toml:"commit,omitempty"`

	// Strip is the number of leading path components to strip when extracting
	// (like tar --strip-components). Default 1 (strips the top-level directory
	// that most GitHub tarballs have).
	Strip int `toml:"strip,omitempty"`

	// SHA256 is the expected hash of the downloaded archive for integrity verification.
	SHA256 string `toml:"sha256,omitempty"`
}

// ValidKinds lists all valid package kinds.
var ValidKinds = []string{"source", "prebuilt", "header-only"}

// ValidVisibilities lists all valid build visibility settings.
var ValidVisibilities = []string{"hidden", "default"}
