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
			if err := ensureLinked(c, seaPkgDir, pkg.Name, pkg.Version, pkg.SHA256, depLinking(m, pkg.Name)); err != nil {
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

	// ── Phase 1: Download all packages (concurrent) ──
	// Content-addressed: if we have the sha256, we have the content.
	// Downloads are independent — no ordering constraints. We use a worker
	// pool to parallelize HTTP requests while keeping output ordered.
	type cachedPkg struct {
		pkg       resolver.ResolvedPackage
		verStr    string
		lockEntry lockfile.LockedPackage
	}

	// Separate packages into cached (already have) and need-download
	type downloadJob struct {
		idx int
		pkg resolver.ResolvedPackage
	}

	cached := make([]cachedPkg, len(resolved))
	var jobs []downloadJob

	for i, pkg := range resolved {
		verStr := pkg.Version.String()

		// Check if lockfile already has this package with a cached hash
		if existingLock != nil {
			if locked := existingLock.Find(pkg.Name); locked != nil {
				if locked.Version == verStr && locked.SHA256 != "" && c.Has(locked.SHA256) {
					if verbose {
						cmd.Printf("  %s@%s — cached (%s)\n", pkg.Name, verStr, locked.SHA256[:12])
					}
					cached[i] = cachedPkg{
						pkg:       pkg,
						verStr:    verStr,
						lockEntry: *locked,
					}
					continue
				}
			}
		}

		jobs = append(jobs, downloadJob{idx: i, pkg: pkg})
		cached[i] = cachedPkg{pkg: pkg, verStr: verStr} // placeholder
	}

	// Download uncached packages concurrently
	if len(jobs) > 0 {
		const maxWorkers = 8
		workers := maxWorkers
		if len(jobs) < workers {
			workers = len(jobs)
		}

		type downloadResult struct {
			idx   int
			entry lockfile.LockedPackage
			err   error
		}

		results := make(chan downloadResult, len(jobs))
		jobsCh := make(chan downloadJob, len(jobs))

		// Feed jobs
		for _, j := range jobs {
			jobsCh <- j
		}
		close(jobsCh)

		// Spin up workers
		for w := 0; w < workers; w++ {
			go func() {
				for job := range jobsCh {
					verStr := job.pkg.Version.String()
					sha, regName, effectiveABI, err := downloadOrBuild(cmd, multi, c, cfg, prof, job.pkg.Name, verStr, abiTag, dir)
					if err != nil {
						results <- downloadResult{idx: job.idx, err: fmt.Errorf("installing %s@%s: %w", job.pkg.Name, verStr, err)}
						continue
					}
					results <- downloadResult{
						idx: job.idx,
						entry: lockfile.LockedPackage{
							Name:        job.pkg.Name,
							Version:     verStr,
							ABI:         effectiveABI,
							Fingerprint: prof.ABIFingerprintHash,
							SHA256:      sha,
							Registry:    regName,
							Deps:        formatDeps(job.pkg.Deps, resolved),
						},
					}
				}
			}()
		}

		// Collect results
		for range jobs {
			r := <-results
			if r.err != nil {
				return r.err
			}
			cached[r.idx] = cachedPkg{
				pkg:       resolved[r.idx],
				verStr:    r.entry.Version,
				lockEntry: r.entry,
			}
			cmd.Printf("  %s@%s (%s)\n", r.entry.Name, r.entry.Version, r.entry.ABI)
		}
	}

	// ── Phase 2: Extract and link all packages ──
	for _, cp := range cached {
		if err := linkPackage(c, seaPkgDir, cp.pkg.Name, cp.verStr, cp.lockEntry.SHA256, depLinking(m, cp.pkg.Name)); err != nil {
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

	// ── Check for superseded artifacts ──
	// Quick check: for each installed package, see if the registry has a
	// newer artifact for the same version. Warns but doesn't block.
	var stale int
	for _, cp := range cached {
		if cp.lockEntry.SHA256 == "" {
			continue
		}
		for _, reg := range multi.Registries() {
			vm, err := reg.FetchVersionManifest(cp.pkg.Name, cp.verStr)
			if err != nil || vm == nil {
				continue
			}
			if replacement := vm.IsSuperseded(cp.lockEntry.SHA256); replacement != nil {
				stale++
			} else if current := vm.CurrentArtifact(cp.lockEntry.ABI); current != nil && current.SHA256 != "" && current.SHA256 != cp.lockEntry.SHA256 {
				stale++
			}
			break
		}
	}
	if stale > 0 {
		cmd.Printf("\n%d package(s) have newer artifacts available. Run 'sea audit' for details.\n", stale)
	}

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

	// Phase 1: Ensure all packages are cached
	for _, pkg := range lf.Packages {
		if pkg.SHA256 != "" && c.Has(pkg.SHA256) {
			if verbose {
				cmd.Printf("  %s@%s — cached (%s)\n", pkg.Name, pkg.Version, pkg.SHA256[:12])
			}
			continue
		}

		cmd.Printf("  %s@%s (%s)...\n", pkg.Name, pkg.Version, pkg.ABI)
		reg, matchedTag, err := multi.FindRegistry(pkg.Name, pkg.Version, pkg.ABI)
		if err != nil {
			return fmt.Errorf("finding %s@%s: %w", pkg.Name, pkg.Version, err)
		}
		rc, err := reg.Download(pkg.Name, pkg.Version, matchedTag)
		if err != nil {
			return fmt.Errorf("downloading %s@%s: %w", pkg.Name, pkg.Version, err)
		}
		_, err = c.Store(rc)
		rc.Close()
		if err != nil {
			return fmt.Errorf("caching %s@%s: %w", pkg.Name, pkg.Version, err)
		}
	}

	// Phase 2: Extract and link all packages
	for _, pkg := range lf.Packages {
		linking := depLinking(m, pkg.Name)
		if err := linkPackage(c, seaPkgDir, pkg.Name, pkg.Version, pkg.SHA256, linking); err != nil {
			return err
		}
	}

	cmd.Printf("Installed %d package(s) from lockfile.\n", len(lf.Packages))
	return nil
}
