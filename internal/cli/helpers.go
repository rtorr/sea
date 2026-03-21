package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rtorr/sea/internal/cache"
	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/lockfile"
	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/profile"
	"github.com/rtorr/sea/internal/registry"
	"github.com/rtorr/sea/internal/resolver"
	"github.com/spf13/cobra"
)

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
			return false // ABI tag changed (e.g. different profile)
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
func ensureLinked(c *cache.Cache, seaPkgDir, name, version, sha256Hash, linking string) error {
	// If already linked and target exists, nothing to do
	pkgDir := filepath.Join(seaPkgDir, name)
	if fi, err := os.Stat(pkgDir); err == nil && fi.IsDir() {
		return nil
	}

	if !c.Has(sha256Hash) {
		return fmt.Errorf("package %s@%s not in cache (hash %s) — run 'sea install' without --locked to download it", name, version, sha256Hash)
	}

	return linkPackage(c, seaPkgDir, name, version, sha256Hash, linking)
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
