package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rtorr/sea/internal/cache"
	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/dirs"
	"github.com/rtorr/sea/internal/lockfile"
	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/registry"
	"github.com/rtorr/sea/internal/resolver"
	"github.com/spf13/cobra"
)

// errUpdatesAvailable is returned by --check when updates exist.
// The root command maps this to exit code 1 without printing "Error:".
var errUpdatesAvailable = errors.New("updates available")

var updateCmd = &cobra.Command{
	Use:   "update [name]",
	Short: "Update dependencies to latest allowed versions",
	Long: `Re-resolve dependencies to the latest versions allowed by sea.toml constraints.

Examples:
  sea update        # update all dependencies
  sea update zlib   # update only zlib`,
	RunE: runUpdate,
}

func runUpdate(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	m, err := manifest.Load(dir)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	prof := getProfile(cfg)
	abiTag := prof.ABITag()

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

	// Load existing lockfile to track what changed
	existingLock, err := lockfile.Load(dir)
	if err != nil {
		existingLock = nil
	}
	oldVersions := make(map[string]string)
	if existingLock != nil {
		for _, pkg := range existingLock.Packages {
			oldVersions[pkg.Name] = pkg.Version
		}
	}

	// If updating a specific package, validate it exists
	var updateOnly string
	if len(args) > 0 {
		updateOnly = args[0]
		_, inDeps := m.Dependencies[updateOnly]
		_, inBuildDeps := m.BuildDeps[updateOnly]
		if !inDeps && !inBuildDeps {
			return fmt.Errorf("package %q is not a dependency in sea.toml", updateOnly)
		}
	}

	cmd.Printf("Resolving dependencies for %s\n", abiTag)

	// Re-resolve from scratch (fresh resolution picks latest allowed)
	resolved, err := resolver.ResolveFromManifest(m, multi, prof, false)
	if err != nil {
		return err
	}

	if len(resolved) == 0 {
		cmd.Println("No dependencies to update.")
		return nil
	}

	// --check mode: show what would change and exit
	checkOnly, _ := cmd.Flags().GetBool("check")
	if checkOnly {
		var changed, needsRebuild int
		for _, pkg := range resolved {
			verStr := pkg.Version.String()
			oldVer := oldVersions[pkg.Name]
			linking := depLinking(m, pkg.Name)
			if oldVer != "" && oldVer != verStr {
				changed++
				var oldMaj, newMaj int
				fmt.Sscanf(oldVer, "%d.", &oldMaj)
				fmt.Sscanf(verStr, "%d.", &newMaj)

				if oldMaj != newMaj {
					cmd.Printf("  %s: %s → %s (MAJOR — rebuild required)\n", pkg.Name, oldVer, verStr)
					needsRebuild++
				} else if linking == "static" {
					cmd.Printf("  %s: %s → %s (static — rebuild required)\n", pkg.Name, oldVer, verStr)
					needsRebuild++
				} else {
					cmd.Printf("  %s: %s → %s (shared — no rebuild needed)\n", pkg.Name, oldVer, verStr)
				}
			} else if oldVer == "" {
				cmd.Printf("  %s@%s (new)\n", pkg.Name, verStr)
				changed++
			}
		}
		if changed == 0 {
			cmd.Println("All packages are already at their latest allowed versions.")
			return nil
		}
		if needsRebuild > 0 {
			cmd.Printf("\n%d update(s) available, %d require rebuild.\n", changed, needsRebuild)
		} else {
			cmd.Printf("\n%d update(s) available, none require rebuild.\n", changed)
		}
		return errUpdatesAvailable
	}

	// Set up cache
	c, err := cache.New(cfg)
	if err != nil {
		return fmt.Errorf("initializing cache: %w", err)
	}

	seaPkgDir := filepath.Join(dir, dirs.SeaPackages)
	newLock := &lockfile.LockFile{Version: 1}
	var changed int

	for _, pkg := range resolved {
		verStr := pkg.Version.String()

		// If updating only a specific package, skip others that haven't changed
		if updateOnly != "" && pkg.Name != updateOnly {
			if oldVer, ok := oldVersions[pkg.Name]; ok && oldVer == verStr {
				// Keep the old lock entry
				if existingLock != nil {
					if locked := existingLock.Find(pkg.Name); locked != nil {
						newLock.Packages = append(newLock.Packages, *locked)
						continue
					}
				}
			}
		}

		// Show what changed with rebuild guidance
		oldVer := oldVersions[pkg.Name]
		linking := depLinking(m, pkg.Name)
		if oldVer != "" && oldVer != verStr {
			changed++
			// Classify the version bump
			var oldMaj, oldMin, newMaj, newMin int
			fmt.Sscanf(oldVer, "%d.%d", &oldMaj, &oldMin)
			fmt.Sscanf(verStr, "%d.%d", &newMaj, &newMin)

			if oldMaj != newMaj {
				// Major bump — always needs rebuild
				cmd.Printf("  %s: %s → %s (MAJOR — rebuild required)\n", pkg.Name, oldVer, verStr)
			} else if linking == "static" {
				// Static linking — any version change needs rebuild
				cmd.Printf("  %s: %s → %s (static — rebuild required to pick up changes)\n", pkg.Name, oldVer, verStr)
			} else {
				// Shared linking, same major — no rebuild needed
				cmd.Printf("  %s: %s → %s (shared — no rebuild needed)\n", pkg.Name, oldVer, verStr)
			}
		} else if oldVer == "" {
			cmd.Printf("  %s@%s (new)\n", pkg.Name, verStr)
			changed++
		} else {
			if verbose {
				cmd.Printf("  %s@%s (unchanged)\n", pkg.Name, verStr)
			}
		}

		// Download and cache
		sha, _, effectiveABI, err := downloadOrBuild(cmd, multi, c, cfg, prof, pkg.Name, verStr, abiTag, dir)
		if err != nil {
			return fmt.Errorf("installing %s@%s: %w", pkg.Name, verStr, err)
		}

		newLock.Packages = append(newLock.Packages, lockfile.LockedPackage{
			Name:        pkg.Name,
			Version:     verStr,
			ABI:         effectiveABI,
			Fingerprint: prof.ABIFingerprintHash,
			SHA256:      sha,
			Deps:        formatDeps(pkg.Deps, resolved),
		})

		linkPref := depLinking(m, pkg.Name)
		if err := linkPackage(c, seaPkgDir, pkg.Name, verStr, sha, linkPref); err != nil {
			return err
		}
	}

	// Write lockfile
	newLock.Sort()
	if err := lockfile.Save(dir, newLock); err != nil {
		return fmt.Errorf("writing lockfile: %w", err)
	}

	if changed == 0 {
		cmd.Println("All packages are already at their latest allowed versions.")
	} else {
		// Count how many need rebuild
		needsRebuild := 0
		for _, pkg := range resolved {
			verStr := pkg.Version.String()
			oldVer := oldVersions[pkg.Name]
			if oldVer != "" && oldVer != verStr {
				linking := depLinking(m, pkg.Name)
				var oldMaj, newMaj int
				fmt.Sscanf(oldVer, "%d.", &oldMaj)
				fmt.Sscanf(verStr, "%d.", &newMaj)
				if oldMaj != newMaj || linking == "static" {
					needsRebuild++
				}
			}
		}

		cmd.Printf("\nUpdated %d package(s).", changed)
		if needsRebuild > 0 {
			cmd.Printf(" %d require rebuild (run 'sea build').", needsRebuild)
		} else if changed > 0 {
			cmd.Print(" No rebuild required — shared libraries updated in place.")
		}
		cmd.Println()
	}
	return nil
}

func init() {
	updateCmd.Flags().Bool("check", false, "check for available updates without applying them (exit code 1 if updates available)")
}
