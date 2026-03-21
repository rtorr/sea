package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/lockfile"
	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/registry"
	"github.com/rtorr/sea/internal/resolver"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Remove a dependency",
	Long: `Remove a package from sea_packages/ and sea.toml, then re-resolve the lockfile.

Examples:
  sea uninstall zlib`,
	Args: cobra.ExactArgs(1),
	RunE: runUninstall,
}

func runUninstall(cmd *cobra.Command, args []string) error {
	name := args[0]

	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	// Load manifest
	m, err := manifest.Load(dir)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}

	// Check the package is actually a dependency
	_, inDeps := m.Dependencies[name]
	_, inBuildDeps := m.BuildDeps[name]
	if !inDeps && !inBuildDeps {
		return fmt.Errorf("package %q is not a dependency in sea.toml", name)
	}

	// Warn if other packages depend on the removed one
	warnDependents(cmd, m, name)

	// Remove from manifest
	delete(m.Dependencies, name)
	delete(m.BuildDeps, name)

	// Write updated manifest
	data, err := manifest.Marshal(m)
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), data, 0o644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	// Remove from sea_packages/
	pkgDir := filepath.Join(dir, "sea_packages", name)
	if fi, err := os.Lstat(pkgDir); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(pkgDir); err != nil {
				return fmt.Errorf("removing symlink %s: %w", pkgDir, err)
			}
		} else {
			if err := os.RemoveAll(pkgDir); err != nil {
				return fmt.Errorf("removing directory %s: %w", pkgDir, err)
			}
		}
		cmd.Printf("Removed %s from sea_packages/\n", name)
	}

	// Also remove from sea_build_packages/ if present
	buildPkgDir := filepath.Join(dir, "sea_build_packages", name)
	if fi, err := os.Lstat(buildPkgDir); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(buildPkgDir); err != nil {
				return fmt.Errorf("removing symlink %s: %w", buildPkgDir, err)
			}
		} else {
			if err := os.RemoveAll(buildPkgDir); err != nil {
				return fmt.Errorf("removing directory %s: %w", buildPkgDir, err)
			}
		}
	}

	// Re-resolve and re-write lockfile
	if len(m.Dependencies) > 0 || len(m.BuildDeps) > 0 {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		multi, err := registry.NewMulti(cfg)
		if err != nil {
			return fmt.Errorf("initializing registries: %w", err)
		}
		if len(multi.Registries()) == 0 {
			cmd.Println("Warning: no registries configured, cannot re-resolve lockfile")
		} else {
			prof := getProfile(cfg)
			if err := prof.EnsureFingerprint(); err != nil {
				cmd.Printf("Warning: ABI probe failed: %v\n", err)
			}
			multi.SetLocalFingerprint(prof.ABIFingerprintHash)

			resolved, err := resolver.ResolveFromManifest(m, multi, prof, false)
			if err != nil {
				cmd.Printf("Warning: could not re-resolve dependencies: %v\n", err)
			} else {
				newLock := &lockfile.LockFile{Version: 1}
				abiTag := prof.ABITag()
				for _, pkg := range resolved {
					newLock.Packages = append(newLock.Packages, lockfile.LockedPackage{
						Name:    pkg.Name,
						Version: pkg.Version.String(),
						ABI:     abiTag,
						Deps:    formatDeps(pkg.Deps, resolved),
					})
				}
				newLock.Sort()
				if err := lockfile.Save(dir, newLock); err != nil {
					return fmt.Errorf("writing lockfile: %w", err)
				}
			}
		}
	} else {
		// No deps left, write empty lockfile
		if err := lockfile.Save(dir, &lockfile.LockFile{Version: 1}); err != nil {
			return fmt.Errorf("writing lockfile: %w", err)
		}
	}

	cmd.Printf("Uninstalled %s\n", name)
	return nil
}

// warnDependents checks if any other dependencies in the manifest
// might transitively depend on the package being removed.
func warnDependents(cmd *cobra.Command, m *manifest.Manifest, name string) {
	// Check the lockfile for deps that reference this package
	dir, err := os.Getwd()
	if err != nil {
		return
	}

	lf, err := lockfile.Load(dir)
	if err != nil || lf == nil {
		return
	}

	for _, pkg := range lf.Packages {
		if pkg.Name == name {
			continue
		}
		for _, dep := range pkg.Deps {
			// deps are formatted as "name@version"
			if len(dep) > len(name) && dep[:len(name)] == name && dep[len(name)] == '@' {
				cmd.Printf("Warning: %s@%s depends on %s\n", pkg.Name, pkg.Version, name)
			}
		}
	}

}
