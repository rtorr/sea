package cli

import (
	"fmt"
	"os"

	"github.com/rtorr/sea/internal/archive"
	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/registry"
	"github.com/spf13/cobra"
)

var publishInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Declare expected platforms for a multi-platform release",
	Long: `Declare which platforms you intend to publish for a version.
This creates a version manifest (sea-version.toml) with "expected" entries
that will be marked "published" as each platform's build completes.

Use this in CI before a matrix build:
  sea publish init --expect linux-x86_64-gcc13-libstdcxx --expect darwin-aarch64-clang17-libcxx

Then each matrix job runs: sea publish
And a final job checks: sea publish status`,
	RunE: runPublishInit,
}

var publishStatusCmd = &cobra.Command{
	Use:   "status [pkg@version]",
	Short: "Show platform availability for a package version",
	Long: `Display the version manifest showing which platforms are published,
expected, or failed for a given version.

Examples:
  sea publish status              # show status for current project version
  sea publish status zlib@1.3.1   # show status for a specific package`,
	RunE: runPublishStatus,
}

var expectFlags []string

func init() {
	publishInitCmd.Flags().StringSliceVar(&expectFlags, "expect", nil, "ABI tag to expect (can be repeated)")
	publishInitCmd.MarkFlagRequired("expect")

	publishCmd.AddCommand(publishInitCmd)
	publishCmd.AddCommand(publishStatusCmd)
}

func runPublishInit(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	m, err := manifest.Load(dir)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	regName := m.Publish.Registry
	if r, _ := cmd.Flags().GetString("registry"); r != "" {
		regName = r
	}
	if regName == "" {
		return fmt.Errorf("no publish registry specified — set [publish].registry in sea.toml or use --registry")
	}

	remote := cfg.FindRemote(regName)
	if remote == nil {
		return fmt.Errorf("registry %q not found", regName)
	}
	reg, err := registry.FromConfig(remote)
	if err != nil {
		return err
	}

	channel := m.EffectiveChannel()

	vm := &archive.VersionManifest{
		Package: archive.VersionManifestPackage{
			Name:    m.Package.Name,
			Version: m.Package.Version,
			Kind:    m.EffectiveKind(),
		},
	}
	for _, tag := range expectFlags {
		vm.Artifacts = append(vm.Artifacts, archive.ExpectedArtifactEntry(channel, tag))
	}

	if err := reg.UploadVersionManifest(m.Package.Name, m.Package.Version, vm); err != nil {
		return fmt.Errorf("uploading version manifest: %w", err)
	}

	cmd.Printf("Initialized %s@%s [%s] with %d expected platform(s):\n", m.Package.Name, m.Package.Version, channel, len(expectFlags))
	for _, tag := range expectFlags {
		cmd.Printf("  - %s\n", tag)
	}
	return nil
}

func runPublishStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	multi, err := registry.NewMulti(cfg)
	if err != nil {
		return err
	}

	var pkgName, pkgVersion string

	if len(args) > 0 {
		// Parse pkg@version
		arg := args[0]
		for i, c := range arg {
			if c == '@' {
				pkgName = arg[:i]
				pkgVersion = arg[i+1:]
				break
			}
		}
		if pkgName == "" {
			pkgName = arg
		}
	} else {
		// Use current project
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		m, err := manifest.Load(dir)
		if err != nil {
			return fmt.Errorf("loading manifest: %w", err)
		}
		pkgName = m.Package.Name
		pkgVersion = m.Package.Version
	}

	if pkgVersion == "" {
		return fmt.Errorf("version required — use pkg@version or run from a project directory")
	}

	// Fetch version manifest from all registries
	var vm *archive.VersionManifest
	for _, reg := range multi.Registries() {
		v, err := reg.FetchVersionManifest(pkgName, pkgVersion)
		if err == nil && v != nil {
			vm = v
			break
		}
	}

	if vm == nil {
		// Fall back to listing ABI tags
		cmd.Printf("%s@%s — no version manifest found, listing ABI tags:\n", pkgName, pkgVersion)
		for _, reg := range multi.Registries() {
			tags, err := reg.ListABITags(pkgName, pkgVersion)
			if err != nil || len(tags) == 0 {
				continue
			}
			for _, tag := range tags {
				cmd.Printf("  %s (from %s)\n", tag, reg.Name())
			}
		}
		return nil
	}

	// Display manifest
	cmd.Printf("%s@%s [%s]\n", vm.Package.Name, vm.Package.Version, vm.Package.Kind)
	cmd.Println()

	published := 0
	expected := 0
	failed := 0
	for _, a := range vm.Artifacts {
		status := a.Status
		extra := ""
		switch status {
		case "published":
			published++
			if a.Timestamp != "" {
				extra = fmt.Sprintf(" (%s)", a.Timestamp)
			}
		case "expected":
			expected++
		case "failed":
			failed++
		}
		cmd.Printf("  [%s] %-10s %s%s\n", a.Channel, status, a.ABITag, extra)
		if a.Publisher != "" {
			cmd.Printf("           by %s", a.Publisher)
			if a.CI != "" {
				cmd.Printf(" (%s", a.CI)
				if a.RunID != "" {
					cmd.Printf(" #%s", a.RunID)
				}
				cmd.Print(")")
			}
			cmd.Println()
		}
	}

	total := published + expected + failed
	cmd.Printf("\n%d/%d published", published, total)
	if expected > 0 {
		cmd.Printf(", %d expected", expected)
	}
	if failed > 0 {
		cmd.Printf(", %d failed", failed)
	}
	cmd.Println()

	return nil
}
