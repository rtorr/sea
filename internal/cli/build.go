package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rtorr/sea/internal/builder"
	"github.com/rtorr/sea/internal/cache"
	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/dirs"
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
	Long: `Build the current project from source.

sea build will:
  1. Download the source archive (if [build.source] is set)
  2. Run the build script or auto-detect the build system
  3. Install the output to sea_build/{abi_tag}/

For packages with dependencies, sea build will install them to sea_packages/
(runtime deps) and sea_build_packages/ (build-only deps) automatically.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		m, err := manifest.Load(dir)
		if err != nil {
			return fmt.Errorf("loading manifest: %w", err)
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

		// Try to restore build cache for incremental builds
		bc, bcErr := cache.NewBuildCache("")
		if bcErr == nil {
			key := bc.Key(m.Package.Name, m.Package.Version, prof.ABITag(), "")
			if restored, err := bc.Retrieve(key, dir); err != nil {
				if verbose {
					cmd.Printf("Build cache miss: %v\n", err)
				}
			} else if restored {
				if verbose {
					cmd.Println("Restored incremental build cache")
				}
			}
		}

		// Set SEA_BUILD_PACKAGES_DIR env var
		buildPkgDir := filepath.Join(dir, dirs.SeaBuildPackages)
		if _, statErr := os.Stat(buildPkgDir); statErr == nil {
			os.Setenv("SEA_BUILD_PACKAGES_DIR", buildPkgDir)
		}

		installDir, err := b.Build()
		if err != nil {
			return err
		}

		// Save build cache for next incremental build
		if bcErr == nil {
			key := bc.Key(m.Package.Name, m.Package.Version, prof.ABITag(), "")
			if err := bc.Store(key, dir); err != nil {
				if verbose {
					cmd.Printf("Warning: could not save build cache: %v\n", err)
				}
			}
		}

		cmd.Printf("Build complete: %s\n", installDir)
		return nil
	},
}

// installBuildDeps resolves and installs all dependencies (runtime + build-only)
// before building. Runtime deps go to sea_packages/, build-only deps go to
// sea_build_packages/.
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

	seaPkgDir := filepath.Join(dir, dirs.SeaPackages)
	buildPkgDir := filepath.Join(dir, dirs.SeaBuildPackages)

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
