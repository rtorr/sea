package builder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rtorr/sea/internal/cache"
	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/profile"
)

// Builder orchestrates source package building.
type Builder struct {
	Manifest   *manifest.Manifest
	Profile    *profile.Profile
	ProjectDir string
	Verbose    bool
}

// New creates a new Builder.
func New(m *manifest.Manifest, prof *profile.Profile, projectDir string) (*Builder, error) {
	if m == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	if prof == nil {
		return nil, fmt.Errorf("profile is required")
	}
	if projectDir == "" {
		return nil, fmt.Errorf("project directory is required")
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, fmt.Errorf("resolving project directory: %w", err)
	}
	return &Builder{
		Manifest:   m,
		Profile:    prof,
		ProjectDir: abs,
	}, nil
}

// Build runs the build and produces output in the install directory.
//
// Resolution order:
//  1. If [build].script is set → run the script
//  2. If [build.source].url is set → download source, auto-detect build system, build
//  3. Auto-detect build system in the project directory
func (b *Builder) Build() (string, error) {
	installDir := filepath.Join(b.ProjectDir, "sea_build", b.Profile.ABITag())
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return "", fmt.Errorf("creating build output directory: %w", err)
	}

	// If source URL is specified, download and build from source
	if b.Manifest.Build.Source.URL != "" {
		return b.buildFromSourceURL(installDir)
	}

	system := DetectBuildSystem(b.ProjectDir, b.Manifest.Build.Script)

	if system == BuildScript {
		// Explicit script — run it with env vars
		scriptPath := b.Manifest.Build.Script
		if !filepath.IsAbs(scriptPath) {
			scriptPath = filepath.Join(b.ProjectDir, scriptPath)
		}
		if _, err := os.Stat(scriptPath); err != nil {
			return "", fmt.Errorf("build script %q not found: %w", b.Manifest.Build.Script, err)
		}

		env := BuildEnv(b.Manifest, b.Profile, b.ProjectDir, installDir)
		if err := RunScript(b.Manifest.Build.Script, env, b.ProjectDir); err != nil {
			return "", fmt.Errorf("build failed for %s@%s (%s): %w",
				b.Manifest.Package.Name, b.Manifest.Package.Version, b.Profile.ABITag(), err)
		}
	} else if system == BuildUnknown {
		return "", fmt.Errorf("cannot build: no build.script in sea.toml and no recognized build system found (CMakeLists.txt, Makefile, meson.build)")
	} else {
		// Auto-detected build system
		if b.Verbose {
			fmt.Printf("Detected build system: %s\n", system)
		}

		cc := envOrDefault(b.Profile.Env, "CC", "cc")
		cxx := envOrDefault(b.Profile.Env, "CXX", "c++")
		cflags := b.Profile.CFlags
		cxxflags := b.Profile.CXXFlags

		seaPkgDir := filepath.Join(b.ProjectDir, "sea_packages")
		seaBuildPkgDir := filepath.Join(b.ProjectDir, "sea_build_packages")
		commands, err := GenerateBuildCommands(system, b.ProjectDir, installDir, cc, cxx, cflags, cxxflags, seaPkgDir, seaBuildPkgDir)
		if err != nil {
			return "", err
		}

		// Inject extra cmake args from manifest
		if system == BuildCMake && len(b.Manifest.Build.CMakeArgs) > 0 && len(commands) > 0 {
			commands[0] = append(commands[0], b.Manifest.Build.CMakeArgs...)
		}

		env := BuildEnv(b.Manifest, b.Profile, b.ProjectDir, installDir)

		for _, argv := range commands {
			if b.Verbose {
				fmt.Printf("  $ %s\n", strings.Join(argv, " "))
			}
			ctx, cancel := context.WithTimeout(context.Background(), buildScriptTimeout)
			cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
			cmd.Dir = b.ProjectDir
			cmd.Env = env
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				cancel()
				if ctx.Err() == context.DeadlineExceeded {
					return "", fmt.Errorf("build timed out after 30 minutes")
				}
				return "", fmt.Errorf("build command failed: %s: %w", strings.Join(argv, " "), err)
			}
			cancel()
		}
	}

	return installDir, nil
}

// buildFromSourceURL downloads source from [build.source].url and builds it.
func (b *Builder) buildFromSourceURL(installDir string) (string, error) {
	srcCacheDir := filepath.Join(b.ProjectDir, "_src")

	// Check if already downloaded
	srcDir := filepath.Join(srcCacheDir, "src")
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		if b.Verbose {
			fmt.Printf("Downloading source from %s\n", b.Manifest.Build.Source.URL)
		}
		var downloadErr error
		srcDir, downloadErr = DownloadSource(b.Manifest.Build.Source, srcCacheDir)
		if downloadErr != nil {
			return "", fmt.Errorf("downloading source: %w", downloadErr)
		}
	}

	// Header-only packages: just copy include/ from the source to install dir
	if b.Manifest.EffectiveKind() == "header-only" {
		return b.installHeaderOnly(srcDir, installDir)
	}

	// If subdir is specified, the build system files are in a subdirectory
	buildDir := srcDir
	if b.Manifest.Build.Subdir != "" {
		buildDir = filepath.Join(srcDir, b.Manifest.Build.Subdir)
	}

	// Detect build system in the source directory
	system := DetectBuildSystem(buildDir, "")
	if system == BuildUnknown {
		return "", fmt.Errorf("cannot auto-detect build system in downloaded source (looked in %s for CMakeLists.txt, Makefile, meson.build)", buildDir)
	}

	if b.Verbose {
		fmt.Printf("Detected build system: %s (in %s)\n", system, buildDir)
	}

	cc := envOrDefault(b.Profile.Env, "CC", "")
	cxx := envOrDefault(b.Profile.Env, "CXX", "")
	cflags := b.Profile.CFlags
	cxxflags := b.Profile.CXXFlags

	seaPkgDir := filepath.Join(b.ProjectDir, "sea_packages")
	seaBuildPkgDir := filepath.Join(b.ProjectDir, "sea_build_packages")
	commands, err := GenerateBuildCommands(system, buildDir, installDir, cc, cxx, cflags, cxxflags, seaPkgDir, seaBuildPkgDir)
	if err != nil {
		return "", err
	}

	// Inject extra cmake args if any
	if system == BuildCMake && len(b.Manifest.Build.CMakeArgs) > 0 {
		if len(commands) > 0 {
			commands[0] = append(commands[0], b.Manifest.Build.CMakeArgs...)
		}
	}

	// CMAKE_POLICY_VERSION_MINIMUM is already added by GenerateBuildCommands

	env := BuildEnv(b.Manifest, b.Profile, srcDir, installDir)

	for _, argv := range commands {
		if b.Verbose {
			fmt.Printf("  $ %s\n", strings.Join(argv, " "))
		}
		ctx, cancel := context.WithTimeout(context.Background(), buildScriptTimeout)
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir = buildDir
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			cancel()
			if ctx.Err() == context.DeadlineExceeded {
				return "", fmt.Errorf("build timed out after 30 minutes")
			}
			return "", fmt.Errorf("build command failed: %s: %w", strings.Join(argv, " "), err)
		}
		cancel()
	}

	return installDir, nil
}

// installHeaderOnly copies headers from a source directory to the install directory.
// It looks for common header locations: include/, src/, or the root of the source.
func (b *Builder) installHeaderOnly(srcDir, installDir string) (string, error) {
	destInclude := filepath.Join(installDir, "include")
	if err := os.MkdirAll(destInclude, 0o755); err != nil {
		return "", err
	}

	// Try common header locations in order
	for _, candidate := range []string{
		filepath.Join(srcDir, "include"),
		filepath.Join(srcDir, "single_include"), // nlohmann-json style
		filepath.Join(srcDir, "src"),
	} {
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			if err := copyDirContents(candidate, destInclude); err != nil {
				return "", fmt.Errorf("copying headers: %w", err)
			}
			return installDir, nil
		}
	}

	// Fallback: look for any .h/.hpp files in the source root and subdirs
	found := false
	filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := filepath.Ext(info.Name())
		if ext == ".h" || ext == ".hpp" || ext == ".hxx" {
			found = true
			rel, _ := filepath.Rel(srcDir, path)
			dest := filepath.Join(destInclude, rel)
			os.MkdirAll(filepath.Dir(dest), 0o755)
			data, readErr := os.ReadFile(path)
			if readErr == nil {
				os.WriteFile(dest, data, 0o644)
			}
		}
		return nil
	})

	if !found {
		return "", fmt.Errorf("no header files found in downloaded source")
	}

	return installDir, nil
}

// copyDirContents copies all files and subdirectories from src into dest.
func copyDirContents(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func envOrDefault(env map[string]string, key, def string) string {
	if env != nil {
		if v, ok := env[key]; ok {
			return v
		}
	}
	return def
}

// SourceHash computes a hash of the build inputs for cache keying.
func (b *Builder) SourceHash() (string, error) {
	// Hash key files that affect the build
	script := b.Manifest.Build.Script
	if script == "" {
		// For auto-detected builds, use the build system file as the key
		system := DetectBuildSystem(b.ProjectDir, "")
		switch system {
		case BuildCMake:
			script = "CMakeLists.txt"
		case BuildMakefile:
			if _, err := os.Stat(filepath.Join(b.ProjectDir, "GNUmakefile")); err == nil {
				script = "GNUmakefile"
			} else {
				script = "Makefile"
			}
		case BuildMeson:
			script = "meson.build"
		case BuildAutotools:
			script = "configure"
		default:
			return "", fmt.Errorf("no build inputs to hash")
		}
	}
	return cache.ComputeSourceHash(b.ProjectDir, script)
}

// BuildCacheKey returns the build cache key.
func (b *Builder) BuildCacheKey(bc *cache.BuildCache, sourceHash string) string {
	return bc.Key(b.Manifest.Package.Name, b.Manifest.Package.Version, b.Profile.ABITag(), sourceHash)
}

// CheckBuildCache checks if a cached build exists.
func (b *Builder) CheckBuildCache(bc *cache.BuildCache) (string, bool, error) {
	sourceHash, err := b.SourceHash()
	if err != nil {
		return "", false, err
	}
	key := b.BuildCacheKey(bc, sourceHash)
	return key, bc.Has(key), nil
}

// RestoreFromCache copies cached build output to the install directory.
func (b *Builder) RestoreFromCache(bc *cache.BuildCache, key string) (string, error) {
	installDir := filepath.Join(b.ProjectDir, "sea_build", b.Profile.ABITag())
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return "", fmt.Errorf("creating build output directory: %w", err)
	}
	ok, err := bc.Retrieve(key, installDir)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("build cache entry not found for key %s", key)
	}
	return installDir, nil
}

// StoreBuildCache stores the build output in the build cache.
func (b *Builder) StoreBuildCache(bc *cache.BuildCache, key, installDir string) error {
	return bc.Store(key, installDir)
}
