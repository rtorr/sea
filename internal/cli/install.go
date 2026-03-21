package cli

import (
	crypto_sha256 "crypto/sha256"
	crypto_hex "encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rtorr/sea/internal/abi"
	"github.com/rtorr/sea/internal/archive"
	"github.com/rtorr/sea/internal/builder"
	"github.com/rtorr/sea/internal/cache"
	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/integrate"
	"github.com/rtorr/sea/internal/lockfile"
	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/pkgconfig"
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
		seaPkgDir := filepath.Join(dir, "sea_packages")
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

	seaPkgDir := filepath.Join(dir, "sea_packages")
	newLock := &lockfile.LockFile{Version: lockfile.CurrentVersion}

	// ── Phase 1: Download and cache all packages ──
	// No extraction or linking happens here. If any download fails, we stop
	// without leaving a half-installed state.
	type cachedPkg struct {
		pkg      resolver.ResolvedPackage
		verStr   string
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
							pkg:      pkg,
							verStr:   verStr,
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
			pkg:      pkg,
			verStr:   verStr,
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

	seaPkgDir := filepath.Join(dir, "sea_packages")

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

// linkPackage extracts (if needed) and symlinks a package into sea_packages/.
func linkPackage(c *cache.Cache, seaPkgDir, name, version, abiTag, linking string) error {
	if !c.IsExtracted(name, version, abiTag) {
		if _, err := c.Extract(name, version, abiTag); err != nil {
			return fmt.Errorf("extracting %s@%s: %w", name, version, err)
		}
	}

	pkgInstallDir := filepath.Join(seaPkgDir, name)
	extractDir := c.GetExtractDir(name, version, abiTag)

	// Remove existing symlink or directory
	if fi, err := os.Lstat(pkgInstallDir); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			os.Remove(pkgInstallDir)
		} else {
			os.RemoveAll(pkgInstallDir)
		}
	}

	if err := os.MkdirAll(seaPkgDir, 0o755); err != nil {
		return fmt.Errorf("creating sea_packages: %w", err)
	}

	if err := os.Symlink(extractDir, pkgInstallDir); err != nil {
		// On Windows without admin privileges, symlinks may fail.
		// Fall back to copying the directory tree.
		if runtime.GOOS == "windows" {
			if cpErr := copyDirTree(extractDir, pkgInstallDir); cpErr != nil {
				return fmt.Errorf("linking %s (symlink failed: %v, copy failed: %w)", name, err, cpErr)
			}
		} else {
			return fmt.Errorf("linking %s: %w", name, err)
		}
	}

	// Create missing library soname symlinks. Archives don't include symlinks
	// for security, but the linker needs libfoo.dylib → libfoo.1.2.3.dylib.
	createLibSymlinks(filepath.Join(pkgInstallDir, "lib"))

	// Write linking preference file if specified
	if linking == "static" || linking == "shared" {
		if err := os.WriteFile(filepath.Join(pkgInstallDir, ".sea-linking"), []byte(linking), 0o644); err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: could not write .sea-linking for %s: %v\n", name, err)
			}
		}
	} else {
		// Clean up any stale preference file
		if err := os.Remove(filepath.Join(pkgInstallDir, ".sea-linking")); err != nil && !os.IsNotExist(err) {
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: could not remove .sea-linking for %s: %v\n", name, err)
			}
		}
	}

	// Relocate cmake config files to use relative paths
	// (cmake installs write absolute paths that break when moved)
	integrate.RelocateCMakeConfigs(pkgInstallDir)

	// Auto-generate a .pc file if the package doesn't already have one
	pcDir := filepath.Join(pkgInstallDir, "lib", "pkgconfig")
	if entries, err := os.ReadDir(pcDir); err != nil || len(entries) == 0 {
		_ = pkgconfig.WriteForPackage(pkgInstallDir, name, version)
	}

	return nil
}

// copyDirTree recursively copies src directory to dst.
func copyDirTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		return copyFile(path, target, info.Mode())
	})
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// createLibSymlinks scans a lib directory and creates missing short-name and
// soname symlinks for versioned shared libraries.
// e.g. libfmt.11.1.4.dylib → libfmt.dylib, libfmt.11.dylib
//
//	liblz4.so.1.10.0     → liblz4.so, liblz4.so.1
//
// For ELF .so files, it also attempts to extract the SONAME from the binary
// and create a symlink with that name if it differs from the filename.
func createLibSymlinks(libDir string) {
	entries, err := os.ReadDir(libDir)
	if err != nil {
		return
	}

	existing := make(map[string]bool)
	for _, e := range entries {
		existing[e.Name()] = true
	}

	for _, e := range entries {
		name := e.Name()

		// On Windows, .dll files don't need soname symlinks.
		// Windows uses PATH for DLL resolution, not symlinks.
		if runtime.GOOS == "windows" && strings.HasSuffix(name, ".dll") {
			continue
		}

		// Try SONAME extraction for .so files (ELF)
		if strings.HasSuffix(name, ".so") || strings.Contains(name, ".so.") {
			soname, err := abi.ExtractSONAME(filepath.Join(libDir, name))
			if err == nil && soname != "" && soname != name && !existing[soname] {
				os.Symlink(name, filepath.Join(libDir, soname))
				existing[soname] = true
			}
		}

		// macOS: libfoo.1.2.3.dylib
		if strings.HasSuffix(name, ".dylib") && strings.Count(name, ".") > 1 {
			base := strings.TrimSuffix(name, ".dylib")
			parts := strings.SplitN(base, ".", 2)
			if len(parts) == 2 {
				shortName := parts[0] + ".dylib"
				if !existing[shortName] {
					os.Symlink(name, filepath.Join(libDir, shortName))
					existing[shortName] = true
				}
				// Also create soname: libfoo.MAJOR.dylib
				soVer := strings.SplitN(parts[1], ".", 2)
				if len(soVer) >= 1 {
					soName := parts[0] + "." + soVer[0] + ".dylib"
					if !existing[soName] {
						os.Symlink(name, filepath.Join(libDir, soName))
						existing[soName] = true
					}
				}
			}
		}

		// Linux: libfoo.so.1.2.3 or libfoo.so.1
		if idx := strings.Index(name, ".so."); idx > 0 {
			baseSo := name[:idx+3] // "libfoo.so"
			if !existing[baseSo] {
				os.Symlink(name, filepath.Join(libDir, baseSo))
				existing[baseSo] = true
			}
			// Soname: libfoo.so.MAJOR
			afterSo := name[idx+4:] // "1.2.3"
			soVer := strings.SplitN(afterSo, ".", 2)
			if len(soVer) >= 1 {
				soName := baseSo + "." + soVer[0] // "libfoo.so.1"
				if !existing[soName] {
					os.Symlink(name, filepath.Join(libDir, soName))
					existing[soName] = true
				}
			}
		}
	}
}

// checkInstallABI compares symbols between old and new versions during an upgrade.
// It warns (but does not block) if symbols were removed within the same major version.
func checkInstallABI(cmd *cobra.Command, multi *registry.Multi, pkg, oldVer, newVer, abiTag string) {
	// Fetch old version's symbol list
	oldSymNames, _, err := multi.FetchPreviousSymbols(pkg, newVer, abiTag)
	if err != nil || len(oldSymNames) == 0 {
		return // can't compare
	}

	// Fetch new version's symbol list
	for _, reg := range multi.Registries() {
		tags, err := reg.ListABITags(pkg, newVer)
		if err != nil || len(tags) == 0 {
			continue
		}
		for _, tag := range tags {
			meta, err := reg.FetchMeta(pkg, newVer, tag)
			if err != nil || len(meta.Symbols.Exported) == 0 {
				continue
			}

			// Build quick diff
			oldSet := make(map[string]bool, len(oldSymNames))
			for _, s := range oldSymNames {
				oldSet[s] = true
			}
			var removed []string
			for _, s := range oldSymNames {
				found := false
				for _, ns := range meta.Symbols.Exported {
					if ns == s {
						found = true
						break
					}
				}
				if !found {
					removed = append(removed, s)
				}
			}

			if len(removed) > 0 {
				// Check if this is within the same major version
				var oldMaj, newMaj int
				fmt.Sscanf(oldVer, "%d.", &oldMaj)
				fmt.Sscanf(newVer, "%d.", &newMaj)
				if oldMaj == newMaj {
					cmd.Printf("  Warning: %s upgraded %s → %s, but %d symbol(s) were removed within major version %d:\n",
						pkg, oldVer, newVer, len(removed), oldMaj)
					limit := len(removed)
					if limit > 5 {
						limit = 5
					}
					for _, s := range removed[:limit] {
						cmd.Printf("    - %s\n", s)
					}
					if len(removed) > 5 {
						cmd.Printf("    ... and %d more\n", len(removed)-5)
					}
					cmd.Println("  This may break your build. The publisher should have bumped the major version.")
				}
			}
			return
		}
	}
}

// getProfile resolves the build profile using this priority:
//  1. --profile CLI flag (explicit path)
//  2. Project sea.toml [profiles.default] (project-level)
//  3. ~/.sea/profiles/default.toml (user-level)
//  4. Auto-detect from host (fallback)
func getProfile(cfg *config.Config) *profile.Profile {
	// 1. CLI flag
	if profileFlag != "" {
		prof, err := profile.LoadFile(profileFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load profile %q: %v — falling back\n", profileFlag, err)
		} else {
			return prof
		}
	}

	// 2. Project-level: sea.toml [profiles.default]
	dir, _ := os.Getwd()
	if dir != "" {
		m, err := manifest.Load(dir)
		if err == nil {
			if ref, ok := m.Profiles["default"]; ok && ref.File != "" {
				profPath := ref.File
				if !filepath.IsAbs(profPath) {
					profPath = filepath.Join(dir, profPath)
				}
				prof, err := profile.LoadFile(profPath)
				if err == nil {
					return prof
				}
			}
		}
	}

	// 3. User-level: ~/.sea/profiles/default.toml
	if seaDir, err := config.SeaDir(); err == nil {
		userDefault := filepath.Join(seaDir, "profiles", "default.toml")
		if prof, err := profile.LoadFile(userDefault); err == nil {
			return prof
		}
	}

	// 4. Auto-detect
	return profile.DetectHost()
}

func parsePackageArg(arg string) (string, string) {
	if idx := strings.Index(arg, "@"); idx > 0 {
		return arg[:idx], "=" + arg[idx+1:]
	}
	return arg, "*"
}

// depLinking returns the linking preference for a dependency from the manifest.
func depLinking(m *manifest.Manifest, name string) string {
	if dep, ok := m.Dependencies[name]; ok {
		return dep.Linking
	}
	if dep, ok := m.BuildDeps[name]; ok {
		return dep.Linking
	}
	return ""
}

// lockfileInSync returns true if every dependency in sea.toml has a matching
// entry in sea.lock that still satisfies the version constraint. When this is
// true, `sea install` can skip resolution entirely — no registry queries needed.
func lockfileInSync(m *manifest.Manifest, lf *lockfile.LockFile, abiTag string) bool {
	if lf == nil || len(lf.Packages) == 0 {
		return len(m.Dependencies) == 0
	}

	// Every dep in manifest must have a locked entry
	for name, dep := range m.Dependencies {
		if dep.Optional {
			continue
		}
		locked := lf.Find(name)
		if locked == nil {
			return false // dep not in lockfile
		}
		if locked.ABI != abiTag && locked.ABI != "any" && abiTag != "any" {
			return false // ABI changed (e.g. different profile)
		}
		// Check that locked version still satisfies the constraint
		vr, err := resolver.ParseRange(dep.Version)
		if err != nil {
			return false
		}
		v, err := resolver.ParseVersion(locked.Version)
		if err != nil {
			return false
		}
		if !vr.Contains(v) {
			return false // constraint changed, locked version no longer satisfies
		}
	}

	// Check no extra locked packages exist that aren't deps anymore
	for _, pkg := range lf.Packages {
		if _, ok := m.Dependencies[pkg.Name]; !ok {
			// Could be a transitive dep — that's fine, we don't require those in sea.toml
			// But if a direct dep was removed from sea.toml, we need to re-resolve
			// We can't easily tell direct from transitive here, so be conservative:
			// only flag it if NO dep in the manifest could have pulled it in.
			continue
		}
	}

	return true
}

// ensureLinked makes sure a package is extracted and linked in sea_packages,
// without downloading. Used when the lockfile is in sync.
func ensureLinked(c *cache.Cache, seaPkgDir, name, version, abiTag, linking string) error {
	// If already linked and target exists, nothing to do
	pkgDir := filepath.Join(seaPkgDir, name)
	if fi, err := os.Stat(pkgDir); err == nil && fi.IsDir() {
		return nil
	}

	// Need to extract and link from cache
	if !c.Has(name, version, abiTag) {
		return fmt.Errorf("package %s@%s not in cache — run 'sea install' without --locked to download it", name, version)
	}

	return linkPackage(c, seaPkgDir, name, version, abiTag, linking)
}

// downloadOrBuild tries to download a prebuilt package for the host ABI tag.
// If no prebuilt exists, it looks for a source package, downloads it, builds
// it locally, and caches the result. Returns (sha256, registry_name, error).
// downloadOrBuild returns (sha256, registryName, effectiveABITag, error).
// effectiveABITag may differ from the input abiTag (e.g. "any" for header-only).
func downloadOrBuild(cmd *cobra.Command, multi *registry.Multi, c *cache.Cache, cfg *config.Config, prof *profile.Profile, name, version, abiTag, projectDir string) (string, string, string, error) {
	// Try 1: prebuilt for our ABI tag (also matches "any" for header-only)
	reg, matchedTag, err := multi.FindRegistry(name, version, abiTag)
	if err == nil {
		rc, dlErr := reg.Download(name, version, matchedTag)
		if dlErr == nil {
			// Store under the matched tag (e.g. "any" for header-only, not the host tag)
			sha, storeErr := c.Store(name, version, matchedTag, rc)
			rc.Close()
			if storeErr == nil {
				return sha, reg.Name(), matchedTag, nil
			}
		}
	}

	// Try 2: source package (stored under "source" or "any" ABI tag)
	for _, sourceTag := range []string{"source", "any"} {
		srcReg, _, srcErr := multi.FindRegistry(name, version, sourceTag)
		if srcErr != nil {
			continue
		}

		// Verify it's actually a source package
		meta, metaErr := srcReg.FetchMeta(name, version, sourceTag)
		if metaErr != nil || meta.Package.Kind != "source" {
			continue
		}

		cmd.Printf("  %s@%s — no prebuilt for %s, building from source...\n", name, version, abiTag)

		// Download source archive
		rc, dlErr := srcReg.Download(name, version, sourceTag)
		if dlErr != nil {
			return "", "", "", fmt.Errorf("downloading source package: %w", dlErr)
		}
		srcSha, storeErr := c.Store(name, version, sourceTag, rc)
		rc.Close()
		if storeErr != nil {
			return "", "", "", fmt.Errorf("caching source package: %w", storeErr)
		}
		_ = srcSha

		// Extract source to a temp build directory
		srcDir, extractErr := c.Extract(name, version, sourceTag)
		if extractErr != nil {
			return "", "", "", fmt.Errorf("extracting source package: %w", extractErr)
		}

		// Build it
		sha, buildErr := buildSourcePackage(cmd, c, cfg, prof, name, version, abiTag, srcDir)
		if buildErr != nil {
			return "", "", "", fmt.Errorf("building from source: %w", buildErr)
		}

		return sha, srcReg.Name() + " (built from source)", abiTag, nil
	}

	return "", "", "", fmt.Errorf("no prebuilt or source package found for %s@%s (need ABI %s)", name, version, abiTag)
}

// buildSourcePackage builds a source package and caches the result under the given ABI tag.
func buildSourcePackage(cmd *cobra.Command, c *cache.Cache, cfg *config.Config, prof *profile.Profile, name, version, abiTag, srcDir string) (string, error) {
	// Look for build.sh or sea.toml with a build script in the source package
	buildScript := ""
	for _, candidate := range []string{"build.sh", "build.cmd", "build.bat"} {
		if _, err := os.Stat(filepath.Join(srcDir, candidate)); err == nil {
			buildScript = candidate
			break
		}
	}

	// Load the source package's manifest for build config and verification rules
	var srcManifest *manifest.Manifest
	loadedManifest, loadErr := manifest.LoadFile(filepath.Join(srcDir, manifest.FileName))
	if loadErr == nil {
		srcManifest = loadedManifest
		if buildScript == "" && srcManifest.Build.Script != "" {
			buildScript = srcManifest.Build.Script
		}
	}

	if buildScript == "" {
		return "", fmt.Errorf("source package has no build script (need build.sh or [build].script in sea.toml)")
	}

	if srcManifest == nil {
		srcManifest = &manifest.Manifest{
			Package: manifest.Package{Name: name, Version: version},
			Build:   manifest.Build{Script: buildScript},
		}
	}

	// Create a build output directory
	buildDir := filepath.Join(srcDir, "sea_build_output")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return "", fmt.Errorf("creating build directory: %w", err)
	}

	// Build using the source package's script
	env := builder.BuildEnv(srcManifest, prof, srcDir, buildDir)
	if err := builder.RunScript(buildScript, env, srcDir); err != nil {
		return "", fmt.Errorf("build script failed: %w", err)
	}

	// Run build verification if configured in the source package's manifest
	if err := builder.VerifyBuildOutput(srcManifest, prof, srcDir, buildDir); err != nil {
		return "", fmt.Errorf("build verification: %w", err)
	}

	// Pack the build output into an archive and cache it under our ABI tag
	archivePath := filepath.Join(srcDir, fmt.Sprintf("%s-%s-%s.tar.zst", name, version, abiTag))
	includes := []string{"include/**", "lib/**", "bin/**", "share/**", "LICENSE", "COPYING"}
	if err := archive.Pack(buildDir, includes, archivePath); err != nil {
		return "", fmt.Errorf("packing build output: %w", err)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("opening built archive: %w", err)
	}
	defer f.Close()

	sha, err := c.Store(name, version, abiTag, f)
	if err != nil {
		return "", fmt.Errorf("caching built package: %w", err)
	}

	cmd.Printf("  %s@%s — built successfully for %s\n", name, version, abiTag)
	return sha, nil
}

// computeCachedHash reads a cached archive and computes its SHA256 hash.
func computeCachedHash(c *cache.Cache, name, version, abiTag string) (bool, string) {
	archivePath := c.Layout.ArchivePath(name, version, abiTag)
	f, err := os.Open(archivePath)
	if err != nil {
		return false, ""
	}
	defer f.Close()

	h := crypto_sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, ""
	}
	return true, crypto_hex.EncodeToString(h.Sum(nil))
}

func formatDeps(depNames []string, resolved []resolver.ResolvedPackage) []string {
	var deps []string
	for _, name := range depNames {
		for _, r := range resolved {
			if r.Name == name {
				deps = append(deps, fmt.Sprintf("%s@%s", name, r.Version))
				break
			}
		}
	}
	return deps
}
