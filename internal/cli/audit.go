package cli

import (
	"fmt"
	"os"

	"github.com/rtorr/sea/internal/cache"
	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/lockfile"
	"github.com/rtorr/sea/internal/registry"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Check for superseded packages (security fixes, rebuilds)",
	Long: `Check if any packages in your lockfile have been superseded by
newer artifacts. This happens when a publisher re-publishes the same
version with a security fix, build fix, or toolchain update.

sea audit does NOT change your lockfile. To accept updates, run:

  sea update --refresh

Examples:
  sea audit              # check all locked packages
  sea audit --fix        # equivalent to sea update --refresh`,
	RunE: runAudit,
}

var auditFixFlag bool

func init() {
	auditCmd.Flags().BoolVar(&auditFixFlag, "fix", false, "update lockfile to use current artifacts (equivalent to 'sea update --refresh')")
}

func runAudit(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	lf, err := lockfile.Load(dir)
	if err != nil {
		return fmt.Errorf("no lockfile found — run 'sea install' first")
	}

	multi, err := registry.NewMulti(cfg)
	if err != nil {
		return fmt.Errorf("initializing registries: %w", err)
	}

	var stale []auditResult
	for _, pkg := range lf.Packages {
		if pkg.SHA256 == "" {
			continue
		}

		// Fetch the version manifest from the registry
		for _, reg := range multi.Registries() {
			vm, err := reg.FetchVersionManifest(pkg.Name, pkg.Version)
			if err != nil || vm == nil {
				continue
			}

			// Check if our hash has been superseded
			if replacement := vm.IsSuperseded(pkg.SHA256); replacement != nil {
				stale = append(stale, auditResult{
					Name:       pkg.Name,
					Version:    pkg.Version,
					OldHash:    pkg.SHA256,
					NewHash:    replacement.SHA256,
					Reason:     replacement.Reason,
					Timestamp:  replacement.Timestamp,
					Supersedes: replacement.Supersedes,
				})
				break
			}

			// Also check if the current artifact for our ABI has a different hash
			// (covers the case where supersedes wasn't recorded)
			if current := vm.CurrentArtifact(pkg.ABI); current != nil {
				if current.SHA256 != "" && current.SHA256 != pkg.SHA256 {
					reason := current.Reason
					if reason == "" {
						reason = "newer artifact available"
					}
					stale = append(stale, auditResult{
						Name:      pkg.Name,
						Version:   pkg.Version,
						OldHash:   pkg.SHA256,
						NewHash:   current.SHA256,
						Reason:    reason,
						Timestamp: current.Timestamp,
					})
					break
				}
			}

			break // found the package, no need to check other registries
		}
	}

	if len(stale) == 0 {
		cmd.Println("All packages are up to date.")
		return nil
	}

	cmd.Printf("Found %d package(s) with newer artifacts:\n\n", len(stale))
	for _, s := range stale {
		reason := s.Reason
		if reason == "" {
			reason = "updated"
		}
		cmd.Printf("  %s@%s [%s]\n", s.Name, s.Version, reason)
		cmd.Printf("    locked:  %s\n", s.OldHash[:16])
		cmd.Printf("    current: %s\n", s.NewHash[:16])
		if s.Timestamp != "" {
			cmd.Printf("    published: %s\n", s.Timestamp)
		}
		cmd.Println()
	}

	if auditFixFlag {
		return runRefresh(cmd, dir, lf, stale)
	}

	cmd.Println("Run 'sea audit --fix' or 'sea update --refresh' to update your lockfile.")
	return nil
}

type auditResult struct {
	Name       string
	Version    string
	OldHash    string
	NewHash    string
	Reason     string
	Timestamp  string
	Supersedes string
}

// runRefresh updates the lockfile with the newer artifact hashes.
func runRefresh(cmd *cobra.Command, dir string, lf *lockfile.LockFile, stale []auditResult) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	c, err := cacheFromConfig(cfg)
	if err != nil {
		return err
	}

	multi, err := registry.NewMulti(cfg)
	if err != nil {
		return err
	}
	prof := getProfile(cfg)
	if err := prof.EnsureFingerprint(); err != nil {
		cmd.Printf("Warning: ABI probe failed: %v\n", err)
	}
	multi.SetLocalFingerprint(prof.ABIFingerprintHash)

	updated := 0
	for _, s := range stale {
		pkg := lf.Find(s.Name)
		if pkg == nil {
			continue
		}

		// Download the new artifact
		reg, matchedTag, err := multi.FindRegistry(s.Name, s.Version, pkg.ABI)
		if err != nil {
			cmd.Printf("  Warning: could not find %s@%s: %v\n", s.Name, s.Version, err)
			continue
		}
		rc, err := reg.Download(s.Name, s.Version, matchedTag)
		if err != nil {
			cmd.Printf("  Warning: could not download %s@%s: %v\n", s.Name, s.Version, err)
			continue
		}
		sha, err := c.Store(rc)
		rc.Close()
		if err != nil {
			cmd.Printf("  Warning: could not cache %s@%s: %v\n", s.Name, s.Version, err)
			continue
		}

		// Update lockfile entry
		pkg.SHA256 = sha
		cmd.Printf("  Updated %s@%s → %s\n", s.Name, s.Version, sha[:16])
		updated++
	}

	if updated > 0 {
		lf.Sort()
		if err := lockfile.Save(dir, lf); err != nil {
			return fmt.Errorf("saving lockfile: %w", err)
		}
		cmd.Printf("\nUpdated %d package(s). Run 'sea install' to apply.\n", updated)
	}

	return nil
}

func cacheFromConfig(cfg *config.Config) (*cache.Cache, error) {
	return cache.New(cfg)
}
