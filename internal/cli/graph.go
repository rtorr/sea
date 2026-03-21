package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/lockfile"
	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/registry"
	"github.com/rtorr/sea/internal/resolver"
	"github.com/spf13/cobra"
)

var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Show the dependency graph",
	Long: `Display the full dependency tree for this project.

Shows each package, its resolved version, which platforms have published
artifacts, and whether the content is cached locally.

This is a dry run — nothing is downloaded or installed.

Examples:
  sea graph              # show dependency tree
  sea graph --json       # machine-readable output`,
	RunE: runGraph,
}

func init() {
	graphCmd.Flags().Bool("json", false, "output as JSON")
}

func runGraph(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	m, err := manifest.Load(dir)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}

	if len(m.Dependencies) == 0 {
		cmd.Println("No dependencies.")
		return nil
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
		return fmt.Errorf("no registries configured")
	}

	if err := prof.EnsureFingerprint(); err != nil {
		cmd.Printf("Warning: ABI probe failed: %v\n", err)
	}
	multi.SetLocalFingerprint(prof.ABIFingerprintHash)

	// Resolve the full graph
	resolved, err := resolver.ResolveFromManifest(m, multi, prof, false)
	if err != nil {
		return err
	}

	// Load lockfile to check what's cached
	lf, _ := lockfile.Load(dir)
	c, cacheErr := cacheFromConfig(cfg)

	cmd.Printf("%s@%s\n", m.Package.Name, m.Package.Version)
	cmd.Printf("Platform: %s (fingerprint: %s)\n\n", abiTag, prof.ABIFingerprintHash[:12])

	// Build a map of direct deps for tree display
	directDeps := make(map[string]bool)
	for name := range m.Dependencies {
		directDeps[name] = true
	}

	// Display each resolved package
	for i, pkg := range resolved {
		verStr := pkg.Version.String()

		// Check cache status
		cacheStatus := "missing"
		if lf != nil && cacheErr == nil {
			if locked := lf.Find(pkg.Name); locked != nil && locked.SHA256 != "" {
				if c.Has(locked.SHA256) {
					cacheStatus = "cached"
				}
			}
		}

		// Check registry status for all platforms
		platforms := checkPlatforms(multi, pkg.Name, verStr)

		// Tree connector
		connector := "├─"
		if i == len(resolved)-1 {
			connector = "└─"
		}

		// Direct vs transitive
		depType := ""
		if !directDeps[pkg.Name] {
			depType = " (transitive)"
		}

		cmd.Printf("%s %s@%s%s\n", connector, pkg.Name, verStr, depType)
		cmd.Printf("   %s  %s\n", cacheIcon(cacheStatus), platforms)

		// Show deps of this package
		if len(pkg.Deps) > 0 {
			cmd.Printf("   requires: %s\n", strings.Join(pkg.Deps, ", "))
		}
	}

	return nil
}

func checkPlatforms(multi *registry.Multi, name, version string) string {
	tags, _ := multi.ListABITagsFromAny(name, version)

	tagSet := make(map[string]bool)
	for _, t := range tags {
		tagSet[t] = true
	}

	var parts []string
	check := func(label string, patterns ...string) {
		for _, p := range patterns {
			if tagSet[p] {
				parts = append(parts, label+": published")
				return
			}
		}
		// Check for "any" (header-only)
		if tagSet["any"] {
			parts = append(parts, label+": published")
			return
		}
		parts = append(parts, label+": missing")
	}

	check("macos", "darwin-aarch64-libcxx", "darwin-x86_64-libcxx")
	check("linux", "linux-x86_64-libstdcxx", "linux-aarch64-libstdcxx")
	check("windows", "windows-x86_64-msvc")

	return strings.Join(parts, "  ")
}

func cacheIcon(status string) string {
	switch status {
	case "cached":
		return "[cached]"
	default:
		return "[needs download]"
	}
}
