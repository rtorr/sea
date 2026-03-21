package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rtorr/sea/internal/builder"
	"github.com/rtorr/sea/internal/cache"
	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/integrate"
	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/profile"
	"github.com/rtorr/sea/internal/registry"
	"github.com/rtorr/sea/internal/resolver"
	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build a source package using the build script",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		m, err := manifest.Load(dir)
		if err != nil {
			return fmt.Errorf("loading manifest: %w", err)
		}

		if m.EffectiveKind() == "header-only" && m.Build.Script == "" && m.Build.Source.URL == "" {
			cmd.Println("Package is header-only with no build script or source URL — nothing to build.")
			return nil
		}

		if m.Build.Script == "" && m.Build.Source.URL == "" {
			// Check if the project directory has a recognizable build system
			system := builder.DetectBuildSystem(dir, "")
			if system == builder.BuildUnknown {
				return fmt.Errorf("cannot build: no [build].script, no [build.source].url, and no recognized build system (CMakeLists.txt, Makefile, meson.build)")
			}
		}

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		prof := getProfile(cfg)
		cmd.Printf("Building %s@%s for %s\n", m.Package.Name, m.Package.Version, prof.ABITag())

		// Install all dependencies (runtime + build) before building.
		// Runtime deps are needed at build time too (headers + link libs).
		if len(m.Dependencies) > 0 || len(m.BuildDeps) > 0 {
			if err := installBuildDeps(cmd, dir, m, cfg, prof); err != nil {
				return fmt.Errorf("installing dependencies: %w", err)
			}
		}

		b, err := builder.New(m, prof, dir)
		if err != nil {
			return err
		}
		b.Verbose = verbose

		// Set up build cache
		c, cacheErr := cache.New(cfg)
		var bc *cache.BuildCache
		if cacheErr == nil {
			bc, _ = cache.NewBuildCache(c.Layout.Root)
		}

		// Check build cache before building
		if bc != nil {
			cacheKey, hit, err := b.CheckBuildCache(bc)
			if err == nil && hit {
				installDir, err := b.RestoreFromCache(bc, cacheKey)
				if err == nil {
					cmd.Printf("Build restored from cache: %s\n", installDir)
					return nil
				}
				if verbose {
					cmd.Printf("Warning: cache restore failed: %v\n", err)
				}
			}
		}

		// Set SEA_BUILD_PACKAGES_DIR env var
		buildPkgDir := filepath.Join(dir, "sea_build_packages")
		if _, statErr := os.Stat(buildPkgDir); statErr == nil {
			os.Setenv("SEA_BUILD_PACKAGES_DIR", buildPkgDir)
		}

		installDir, err := b.Build()
		if err != nil {
			return err
		}

		// Verify the build produced output
		entries, readErr := os.ReadDir(installDir)
		if readErr != nil {
			return fmt.Errorf("reading build output directory: %w", readErr)
		}
		if len(entries) == 0 {
			return fmt.Errorf("build script produced no output in %s", installDir)
		}

		// Run build verification checks (expected libs, headers, symbols, test program)
		if err := builder.VerifyBuildOutput(m, prof, dir, installDir); err != nil {
			return err
		}

		// Store in build cache after successful build
		if bc != nil {
			sourceHash, err := b.SourceHash()
			if err == nil {
				key := b.BuildCacheKey(bc, sourceHash)
				if storeErr := b.StoreBuildCache(bc, key, installDir); storeErr != nil {
					if verbose {
						cmd.Printf("Warning: could not store build in cache: %v\n", storeErr)
					}
				}
			}
		}

		cmd.Printf("Build complete: %s\n", installDir)
		return nil
	},
}

// installBuildDeps resolves and installs build dependencies into sea_build_packages/.
func installBuildDeps(cmd *cobra.Command, dir string, m *manifest.Manifest, cfg *config.Config, prof *profile.Profile) error {
	multi, err := registry.NewMulti(cfg)
	if err != nil {
		return fmt.Errorf("initializing registries: %w", err)
	}
	if len(multi.Registries()) == 0 {
		return fmt.Errorf("no registries configured — use 'sea remote add' to add one")
	}

	if err := prof.EnsureFingerprint(); err != nil {
		cmd.Printf("Warning: ABI probe failed: %v\n", err)
	}
	multi.SetLocalFingerprint(prof.ABIFingerprintHash)
	abiTag := prof.ABITag()

	// Resolve all deps (runtime + build) together for coherent resolution
	resolved, err := resolver.ResolveFromManifest(m, multi, prof, true)
	if err != nil {
		return err
	}

	// Determine which resolved packages are build-only
	buildOnlyNames := make(map[string]bool)
	for name := range m.BuildDeps {
		if _, isRuntime := m.Dependencies[name]; !isRuntime {
			buildOnlyNames[name] = true
		}
	}

	c, err := cache.New(cfg)
	if err != nil {
		return fmt.Errorf("initializing cache: %w", err)
	}

	seaPkgDir := filepath.Join(dir, "sea_packages")
	buildPkgDir := filepath.Join(dir, "sea_build_packages")

	for _, pkg := range resolved {
		verStr := pkg.Version.String()
		isBuildOnly := buildOnlyNames[pkg.Name]

		label := ""
		targetDir := seaPkgDir
		if isBuildOnly {
			label = "[build] "
			targetDir = buildPkgDir
		}

		// Check cache, download if needed
		effectiveABI := abiTag
		if !c.Has(pkg.Name, verStr, abiTag) && !c.Has(pkg.Name, verStr, "any") {
			cmd.Printf("  %s%s@%s (%s)...\n", label, pkg.Name, verStr, abiTag)
			reg, matchedTag, err := multi.FindRegistry(pkg.Name, verStr, abiTag)
			if err != nil {
				return fmt.Errorf("finding %s@%s: %w", pkg.Name, verStr, err)
			}
			rc, err := reg.Download(pkg.Name, verStr, matchedTag)
			if err != nil {
				return fmt.Errorf("downloading %s@%s: %w", pkg.Name, verStr, err)
			}
			_, err = c.Store(pkg.Name, verStr, matchedTag, rc)
			rc.Close()
			if err != nil {
				return fmt.Errorf("caching %s@%s: %w", pkg.Name, verStr, err)
			}
			effectiveABI = matchedTag
		} else if c.Has(pkg.Name, verStr, "any") {
			effectiveABI = "any"
		}

		linking := depLinking(m, pkg.Name)
		if err := linkPackage(c, targetDir, pkg.Name, verStr, effectiveABI, linking); err != nil {
			return err
		}
	}

	// Generate cmake integration for runtime deps
	integrate.GenerateCMakeIntegration(seaPkgDir)

	return nil
}
