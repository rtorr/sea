package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rtorr/sea/internal/cache"
	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/dirs"
	"github.com/rtorr/sea/internal/integrate"
	"github.com/rtorr/sea/internal/lockfile"
	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/registry"
	"github.com/rtorr/sea/internal/resolver"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Update packages to latest compatible versions and artifacts",
	Long: `Check all dependencies for newer versions and artifacts, then update
the lockfile and installed packages automatically.

sea audit does three things:
  1. Checks for newer artifacts of the same version (security fixes, rebuilds)
  2. Checks for newer versions within your sea.toml constraints
  3. Updates the lockfile and re-installs

If a newer version exists but is outside your constraint, sea audit tells
you what to change in sea.toml.

Examples:
  sea audit              # update everything to latest compatible`,
	RunE: runAudit,
}

// auditFixFlag is used when install delegates to audit.
// Not exposed as a CLI flag anymore — audit always applies fixes.
var auditFixFlag bool

func runAudit(cmd *cobra.Command, args []string) error {
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
	if err := prof.EnsureFingerprint(); err != nil {
		cmd.Printf("Warning: ABI probe failed: %v\n", err)
	}

	multi, err := registry.NewMulti(cfg)
	if err != nil {
		return fmt.Errorf("initializing registries: %w", err)
	}
	if len(multi.Registries()) == 0 {
		return fmt.Errorf("no registries configured")
	}
	multi.SetLocalFingerprint(prof.ABIFingerprintHash)

	abiTag := prof.ABITag()

	// Load existing lockfile
	existingLock, _ := lockfile.Load(dir)

	cmd.Printf("Auditing dependencies for %s\n", abiTag)

	// Re-resolve from scratch to get latest allowed versions
	resolved, err := resolver.ResolveFromManifest(m, multi, prof, false)
	if err != nil {
		return err
	}

	if len(resolved) == 0 {
		cmd.Println("No dependencies.")
		return nil
	}

	c, err := cache.New(cfg)
	if err != nil {
		return fmt.Errorf("initializing cache: %w", err)
	}

	// Compare resolved versions with lockfile and download updates
	var changes int
	var constraintWarnings []string
	newLock := &lockfile.LockFile{Version: lockfile.CurrentVersion}
	seaPkgDir := filepath.Join(dir, dirs.SeaPackages)

	for _, pkg := range resolved {
		verStr := pkg.Version.String()

		oldVer := ""
		oldHash := ""
		if existingLock != nil {
			if locked := existingLock.Find(pkg.Name); locked != nil {
				oldVer = locked.Version
				oldHash = locked.SHA256
			}
		}

		// Download the latest artifact
		sha, regName, effectiveABI, err := downloadOrBuild(cmd, multi, c, cfg, prof, pkg.Name, verStr, abiTag, dir)
		if err != nil {
			cmd.Printf("  Warning: could not fetch %s@%s: %v\n", pkg.Name, verStr, err)
			// Keep existing lockfile entry if available
			if existingLock != nil {
				if locked := existingLock.Find(pkg.Name); locked != nil {
					newLock.Packages = append(newLock.Packages, *locked)
				}
			}
			continue
		}

		// Determine what changed
		if oldVer != verStr {
			cmd.Printf("  %s: %s → %s\n", pkg.Name, oldVer, verStr)
			changes++
		} else if oldHash != sha {
			cmd.Printf("  %s@%s: new artifact (%s)\n", pkg.Name, verStr, sha[:12])
			changes++
		}

		newLock.Packages = append(newLock.Packages, lockfile.LockedPackage{
			Name:        pkg.Name,
			Version:     verStr,
			ABI:         effectiveABI,
			Fingerprint: prof.ABIFingerprintHash,
			SHA256:      sha,
			Registry:    regName,
			Deps:        formatDeps(pkg.Deps, resolved),
		})

		// Link
		if err := linkPackage(c, seaPkgDir, pkg.Name, verStr, sha, depLinking(m, pkg.Name)); err != nil {
			return err
		}
	}

	// Check for versions outside constraints
	for name, dep := range m.Dependencies {
		vr, err := resolver.ParseRange(dep.Version)
		if err != nil {
			continue
		}

		// Find the absolute latest version in the registry
		versions, err := multi.ListVersions(name)
		if err != nil || len(versions) == 0 {
			continue
		}

		latestStr := versions[len(versions)-1]
		latest, err := resolver.ParseVersion(latestStr)
		if err != nil {
			continue
		}

		// Check if the latest is outside our constraint
		if !vr.Contains(latest) {
			// Find what we resolved to
			resolvedVer := ""
			for _, pkg := range resolved {
				if pkg.Name == name {
					resolvedVer = pkg.Version.String()
					break
				}
			}
			constraintWarnings = append(constraintWarnings,
				fmt.Sprintf("  %s: latest is %s, but sea.toml constraint %q resolved to %s",
					name, latestStr, dep.Version, resolvedVer))
		}
	}

	// Save lockfile
	newLock.Sort()
	if err := lockfile.Save(dir, newLock); err != nil {
		return fmt.Errorf("saving lockfile: %w", err)
	}

	// Generate cmake integration
	integrate.GenerateCMakeIntegration(seaPkgDir)

	if changes == 0 {
		cmd.Println("\nAll packages are up to date.")
	} else {
		cmd.Printf("\nUpdated %d package(s).\n", changes)
	}

	if len(constraintWarnings) > 0 {
		cmd.Println("\nNewer versions exist outside your constraints:")
		for _, w := range constraintWarnings {
			cmd.Println(w)
		}
		cmd.Println("\nUpdate the version constraints in sea.toml to allow these versions.")
	}

	return nil
}

func cacheFromConfig(cfg *config.Config) (*cache.Cache, error) {
	return cache.New(cfg)
}
