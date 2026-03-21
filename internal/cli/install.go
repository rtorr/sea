package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rtorr/sea/internal/cache"
	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/dirs"
	"github.com/rtorr/sea/internal/integrate"
	"github.com/rtorr/sea/internal/lockfile"
	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/profile"
	"github.com/rtorr/sea/internal/registry"
	"github.com/rtorr/sea/internal/resolver"
	"github.com/spf13/cobra"
)

var lockedFlag bool
var featuresFlag string

var installCmd = &cobra.Command{
	Use:   "install [pkg@version ...]",
	Short: "Install dependencies",
	Long: `Install dependencies from sea.toml, or install specific packages.

Examples:
  sea install              # install all dependencies from sea.toml
  sea install --locked     # install exactly what's in sea.lock
  sea install zlib@1.3.1   # install a specific package
  sea install zlib openssl # install multiple packages (latest versions)`,
	RunE: runInstall,
}

func init() {
	installCmd.Flags().BoolVar(&lockedFlag, "locked", false, "install only what is in sea.lock, fail if lockfile is missing or incomplete")
	installCmd.Flags().StringVar(&featuresFlag, "features", "", "comma-separated list of features to enable")
}

func runInstall(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	prof := getProfile(cfg)

	m, err := manifest.Load(dir)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}

	// Parse enabled features from --features flag
	var enabledFeatures []string
	if featuresFlag != "" {
		for _, f := range strings.Split(featuresFlag, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				enabledFeatures = append(enabledFeatures, f)
			}
		}
	}

	// --locked: install exactly what's in sea.lock
	if lockedFlag {
		return runLockedInstall(cmd, dir, m, cfg, prof)
	}

	// If specific packages are provided on the command line, add them as deps
	if len(args) > 0 {
		for _, arg := range args {
			name, ver := parsePackageArg(arg)
			if _, exists := m.Dependencies[name]; !exists {
				m.Dependencies[name] = manifest.Dependency{Version: ver}
			}
		}
	}

	if len(m.Dependencies) == 0 {
		cmd.Println("No dependencies to install.")
		return nil
	}

	// Set up registries
	multi, err := registry.NewMulti(cfg)
	if err != nil {
		return fmt.Errorf("initializing registries: %w", err)
	}

	if len(multi.Registries()) == 0 {
		return fmt.Errorf("no registries configured — use 'sea remote add' to add one")
	}

	// Probe the local toolchain's ABI fingerprint
	if err := prof.EnsureFingerprint(); err != nil {
		cmd.Printf("Warning: ABI probe failed (%v), falling back to exact tag matching\n", err)
	}
	multi.SetLocalFingerprint(prof.ABIFingerprintHash)

	abiTag := prof.ABITag()

	// Load existing lockfile
	existingLock, err := lockfile.Load(dir)
	if err != nil {
		cmd.Printf("Warning: could not read lockfile: %v\n", err)
		existingLock = nil
	}

	// Migrate lockfile if it's an older version
	if existingLock != nil && existingLock.Version < lockfile.CurrentVersion {
		if lockfile.Migrate(existingLock) {
			if verbose {
				cmd.Printf("Migrated lockfile from version %d to %d\n", existingLock.Version-1, existingLock.Version)
			}
		}
	}

	// Check if lockfile is in sync with sea.toml — if every dep in sea.toml
	// has a matching locked entry that satisfies the constraint, skip resolution
	// entirely. This means `sea install` is a no-op when nothing changed, and
	// compatible patches published upstream don't cause unnecessary re-downloads.
	if existingLock != nil && lockfileInSync(m, existingLock, abiTag) {
		cmd.Println("Lockfile is up to date.")
		c, err := cache.New(cfg)
		if err != nil {
			return fmt.Errorf("initializing cache: %w", err)
		}
		seaPkgDir := filepath.Join(dir, dirs.SeaPackages)
		for _, pkg := range existingLock.Packages {
			if err := ensureLinked(c, seaPkgDir, pkg.Name, pkg.Version, pkg.ABI, depLinking(m, pkg.Name)); err != nil {
				return err
			}
		}
		cmd.Printf("All %d package(s) are installed.\n", len(existingLock.Packages))
		return nil
	}

	cmd.Printf("Resolving dependencies for %s\n", abiTag)

	// Build preferences from lockfile — try locked versions first to avoid drift
	var prefs map[string]resolver.Version
	if existingLock != nil {
		prefs = make(map[string]resolver.Version)
		for _, pkg := range existingLock.Packages {
			v, err := resolver.ParseVersion(pkg.Version)
			if err == nil {
				prefs[pkg.Name] = v
			}
		}
	}

	// Resolve dependencies (runtime only), preferring locked versions
	resolved, err := resolver.ResolveFromManifestWithFeatures(m, multi, prof, false, enabledFeatures, prefs)
	if err != nil {
		return err // resolver errors are already well-formatted
	}

	if len(resolved) == 0 {
		cmd.Println("All dependencies resolved — nothing to install.")
		return nil
	}

	// Set up cache
	c, err := cache.New(cfg)
	if err != nil {
		return fmt.Errorf("initializing cache: %w", err)
	}

	seaPkgDir := filepath.Join(dir, dirs.SeaPackages)
	newLock := &lockfile.LockFile{Version: lockfile.CurrentVersion}

	// ── Phase 1: Download and cache all packages ──
	// No extraction or linking happens here. If any download fails, we stop
	// without leaving a half-installed state.
	type cachedPkg struct {
		pkg       resolver.ResolvedPackage
		verStr    string
		lockEntry lockfile.LockedPackage
	}
	var cached []cachedPkg

	for _, pkg := range resolved {
		verStr := pkg.Version.String()

		// Check if lockfile already has this exact package and it's cached
		if existingLock != nil {
			if locked := existingLock.Find(pkg.Name); locked != nil {
				lockedABI := locked.ABI
				if locked.Version == verStr && profile.AreCompatible(lockedABI, abiTag, locked.Fingerprint, prof.ABIFingerprintHash) && c.Has(pkg.Name, verStr, lockedABI) {
					// Verify hash integrity
					ok, err := c.VerifyHash(pkg.Name, verStr, lockedABI, locked.SHA256)
					if err == nil && ok {
						if verbose {
							cmd.Printf("  %s@%s — cached (verified)\n", pkg.Name, verStr)
						}
						cached = append(cached, cachedPkg{
							pkg:       pkg,
							verStr:    verStr,
							lockEntry: *locked,
						})
						continue
					}
					// Hash mismatch — re-download
					cmd.Printf("  %s@%s — cache integrity check failed, re-downloading\n", pkg.Name, verStr)
					if err := c.Remove(pkg.Name, verStr, abiTag); err != nil {
						return fmt.Errorf("cleaning corrupted cache entry: %w", err)
					}
				}
			}
		}

		cmd.Printf("  %s@%s (%s)...\n", pkg.Name, verStr, abiTag)

		// Download if not cached — try prebuilt first, then build from source
		var lockEntry lockfile.LockedPackage
		if !c.Has(pkg.Name, verStr, abiTag) && !c.Has(pkg.Name, verStr, "any") {
			sha, regName, effectiveABI, err := downloadOrBuild(cmd, multi, c, cfg, prof, pkg.Name, verStr, abiTag, dir)
			if err != nil {
				return fmt.Errorf("installing %s@%s: %w", pkg.Name, verStr, err)
			}

			lockEntry = lockfile.LockedPackage{
				Name:     pkg.Name,
				Version:  verStr,
				ABI:      effectiveABI,
				SHA256:   sha,
				Registry: regName,
				Deps:     formatDeps(pkg.Deps, resolved),
			}
		} else {
			// Package is cached — determine which ABI tag it's under
			cachedABI := abiTag
			if c.Has(pkg.Name, verStr, "any") && !c.Has(pkg.Name, verStr, abiTag) {
				cachedABI = "any"
			}
			sha := ""
			if ok, computedHash := computeCachedHash(c, pkg.Name, verStr, cachedABI); ok {
				sha = computedHash
			}
			lockEntry = lockfile.LockedPackage{
				Name:    pkg.Name,
				Version: verStr,
				ABI:     cachedABI,
				SHA256:  sha,
				Deps:    formatDeps(pkg.Deps, resolved),
			}
		}

		cached = append(cached, cachedPkg{
			pkg:       pkg,
			verStr:    verStr,
			lockEntry: lockEntry,
		})
	}

	// ── Phase 2: Extract and link all packages ──
	// All downloads succeeded, so now we can safely extract and link.
	for _, cp := range cached {
		effectiveABI := cp.lockEntry.ABI
		if effectiveABI == "" {
			effectiveABI = abiTag
		}
		if err := linkPackage(c, seaPkgDir, cp.pkg.Name, cp.verStr, effectiveABI, depLinking(m, cp.pkg.Name)); err != nil {
			return err
		}
		newLock.Packages = append(newLock.Packages, cp.lockEntry)

		// ── ABI safety check on version upgrades ──
		if existingLock != nil {
			if locked := existingLock.Find(cp.pkg.Name); locked != nil && locked.Version != cp.verStr {
				checkInstallABI(cmd, multi, cp.pkg.Name, locked.Version, cp.verStr, abiTag)
			}
		}
	}

	// Write lockfile only after all packages are linked
	newLock.Sort()
	if err := lockfile.Save(dir, newLock); err != nil {
		return fmt.Errorf("writing lockfile: %w", err)
	}

	// Generate build system integration files
	integrate.GenerateCMakeIntegration(seaPkgDir)

	cmd.Printf("Installed %d package(s).\n", len(resolved))
	return nil
}

// runLockedInstall installs exactly what's in sea.lock without resolving.
func runLockedInstall(cmd *cobra.Command, dir string, m *manifest.Manifest, cfg *config.Config, prof *profile.Profile) error {
	lf, err := lockfile.Load(dir)
	if err != nil {
		return fmt.Errorf("reading lockfile: %w", err)
	}
	if lf == nil {
		return fmt.Errorf("--locked: sea.lock does not exist — run 'sea install' first to generate it")
	}

	// Verify every dependency in sea.toml has a matching entry in sea.lock
	for name := range m.Dependencies {
		if lf.Find(name) == nil {
			return fmt.Errorf("--locked: dependency %q in sea.toml has no entry in sea.lock — run 'sea install' without --locked first", name)
		}
	}

	abiTag := prof.ABITag()
	cmd.Printf("Installing from lockfile for %s\n", abiTag)

	multi, err := registry.NewMulti(cfg)
	if err != nil {
		return fmt.Errorf("initializing registries: %w", err)
	}
	if err := prof.EnsureFingerprint(); err != nil {
		cmd.Printf("Warning: ABI probe failed: %v\n", err)
	}
	multi.SetLocalFingerprint(prof.ABIFingerprintHash)

	c, err := cache.New(cfg)
	if err != nil {
		return fmt.Errorf("initializing cache: %w", err)
	}

	seaPkgDir := filepath.Join(dir, dirs.SeaPackages)

	// Phase 1: Download all packages
	type downloadResult struct {
		pkg lockfile.LockedPackage
		sha string
	}
	var downloads []downloadResult

	for _, pkg := range lf.Packages {
		cmd.Printf("  %s@%s (%s)...\n", pkg.Name, pkg.Version, pkg.ABI)

		if c.Has(pkg.Name, pkg.Version, pkg.ABI) {
			// Verify hash if available
			if pkg.SHA256 != "" {
				ok, err := c.VerifyHash(pkg.Name, pkg.Version, pkg.ABI, pkg.SHA256)
				if err != nil || !ok {
					cmd.Printf("  %s@%s — cache integrity check failed, re-downloading\n", pkg.Name, pkg.Version)
					if err := c.Remove(pkg.Name, pkg.Version, pkg.ABI); err != nil {
						return fmt.Errorf("cleaning corrupted cache: %w", err)
					}
				} else {
					downloads = append(downloads, downloadResult{pkg: pkg, sha: pkg.SHA256})
					continue
				}
			} else {
				downloads = append(downloads, downloadResult{pkg: pkg, sha: pkg.SHA256})
				continue
			}
		}

		reg, matchedTag, err := multi.FindRegistry(pkg.Name, pkg.Version, pkg.ABI)
		if err != nil {
			return fmt.Errorf("finding %s@%s: %w", pkg.Name, pkg.Version, err)
		}
		rc, err := reg.Download(pkg.Name, pkg.Version, matchedTag)
		if err != nil {
			return fmt.Errorf("downloading %s@%s: %w", pkg.Name, pkg.Version, err)
		}
		sha, err := c.Store(pkg.Name, pkg.Version, pkg.ABI, rc)
		rc.Close()
		if err != nil {
			return fmt.Errorf("caching %s@%s: %w", pkg.Name, pkg.Version, err)
		}
		downloads = append(downloads, downloadResult{pkg: pkg, sha: sha})
	}

	// Phase 2: Extract and link all packages
	for _, dl := range downloads {
		linking := depLinking(m, dl.pkg.Name)
		if err := linkPackage(c, seaPkgDir, dl.pkg.Name, dl.pkg.Version, dl.pkg.ABI, linking); err != nil {
			return err
		}
	}

	cmd.Printf("Installed %d package(s) from lockfile.\n", len(downloads))
	return nil
}
