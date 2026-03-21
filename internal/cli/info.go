package cli

import (
	"fmt"
	"strings"

	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/profile"
	"github.com/rtorr/sea/internal/registry"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info [pkg[@version]]",
	Short: "Show package information and available platforms",
	Long: `Show detailed information about a package including all available
versions and platforms.

Examples:
  sea info cjson           # show all versions and platforms
  sea info cjson@1.7.0     # show platforms for a specific version
  sea info                 # show all packages in all registries`,
	RunE: runInfo,
}

func runInfo(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	multi, err := registry.NewMulti(cfg)
	if err != nil {
		return err
	}

	if len(multi.Registries()) == 0 {
		return fmt.Errorf("no registries configured — use 'sea remote add' to add one")
	}

	// No args: list all packages with platform counts
	if len(args) == 0 {
		return showAllPackages(cmd, multi)
	}

	// Parse pkg[@version]
	arg := args[0]
	pkgName := arg
	pkgVersion := ""
	if idx := strings.Index(arg, "@"); idx > 0 {
		pkgName = arg[:idx]
		pkgVersion = arg[idx+1:]
	}

	if pkgVersion != "" {
		return showPackageVersion(cmd, multi, pkgName, pkgVersion)
	}
	return showPackage(cmd, multi, pkgName)
}

func showAllPackages(cmd *cobra.Command, multi *registry.Multi) error {
	results, err := multi.Search("")
	if err != nil || len(results) == 0 {
		cmd.Println("No packages found.")
		return nil
	}

	host := profile.DetectHost()
	hostABI := host.ABITag()
	cmd.Printf("%-25s %-12s %s\n", "Package", "Versions", "Your platform")
	cmd.Printf("%-25s %-12s %s\n", "-------", "--------", "-------------")

	for _, r := range results {
		verCount := fmt.Sprintf("%d version(s)", len(r.Versions))
		// Check if any version has our ABI
		compatible := "  -"
		for _, ver := range r.Versions {
			tags, _ := multi.ListABITagsFromAny(r.Name, ver)
			for _, t := range tags {
				if t == hostABI || t == "any" {
					compatible = "  ✓"
					break
				}
			}
			if compatible == "  ✓" {
				break
			}
		}
		cmd.Printf("%-25s %-12s %s\n", r.Name, verCount, compatible)
	}
	return nil
}

func showPackage(cmd *cobra.Command, multi *registry.Multi, name string) error {
	versions, err := multi.ListVersions(name)
	if err != nil {
		return fmt.Errorf("package %q not found", name)
	}

	cmd.Printf("%s — %d version(s)\n\n", name, len(versions))
	for _, ver := range versions {
		tags, _ := multi.ListABITagsFromAny(name, ver)
		platforms := summarizePlatforms(tags)
		cmd.Printf("  %s  %s\n", ver, platforms)
	}
	return nil
}

func showPackageVersion(cmd *cobra.Command, multi *registry.Multi, name, version string) error {
	tags, err := multi.ListABITagsFromAny(name, version)
	if err != nil || len(tags) == 0 {
		return fmt.Errorf("no builds found for %s@%s", name, version)
	}

	cmd.Printf("%s@%s — %d platform(s)\n\n", name, version, len(tags))
	for _, tag := range tags {
		cmd.Printf("  %s\n", tag)
	}
	return nil
}

func summarizePlatforms(tags []string) string {
	if len(tags) == 0 {
		return "(no builds)"
	}

	hasLinux, hasDarwin, hasWindows, hasAny := false, false, false, false
	for _, t := range tags {
		switch {
		case t == "any":
			hasAny = true
		case strings.HasPrefix(t, "linux-"):
			hasLinux = true
		case strings.HasPrefix(t, "darwin-"):
			hasDarwin = true
		case strings.HasPrefix(t, "windows-"):
			hasWindows = true
		}
	}

	if hasAny {
		return "[any]"
	}

	var parts []string
	if hasLinux {
		parts = append(parts, "linux")
	}
	if hasDarwin {
		parts = append(parts, "macos")
	}
	if hasWindows {
		parts = append(parts, "windows")
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
