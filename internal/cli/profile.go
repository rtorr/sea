package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/profile"
	"github.com/spf13/cobra"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage build profiles",
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available profiles and detected host profile",
	RunE: func(cmd *cobra.Command, args []string) error {
		host := profile.DetectHost()
		cmd.Println("Detected host:")
		cmd.Printf("  OS:       %s\n", host.OS)
		cmd.Printf("  Arch:     %s\n", host.Arch)
		cmd.Printf("  Compiler: %s %s\n", host.Compiler, host.CompilerVersion)
		cmd.Printf("  Stdlib:   %s\n", host.CppStdlib)
		cmd.Printf("  ABI Tag:  %s\n", host.ABITag())

		// User-level profiles: ~/.sea/profiles/
		if seaDir, err := config.SeaDir(); err == nil {
			userDir := filepath.Join(seaDir, "profiles")
			if entries, err := os.ReadDir(userDir); err == nil && len(entries) > 0 {
				cmd.Println("\nUser profiles (~/.sea/profiles/):")
				for _, e := range entries {
					if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
						continue
					}
					p, err := profile.LoadFile(filepath.Join(userDir, e.Name()))
					if err != nil {
						cmd.Printf("  %s — error: %v\n", e.Name(), err)
						continue
					}
					cmd.Printf("  %s — %s (ABI: %s)\n", e.Name(), p.Name, p.ABITag())
				}
			}
		}

		// Project-level profiles: ./profiles/
		dir, err := os.Getwd()
		if err != nil {
			return nil
		}
		projectDir := filepath.Join(dir, "profiles")
		if entries, err := os.ReadDir(projectDir); err == nil && len(entries) > 0 {
			cmd.Println("\nProject profiles (profiles/):")
			for _, e := range entries {
				if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
					continue
				}
				p, err := profile.LoadFile(filepath.Join(projectDir, e.Name()))
				if err != nil {
					cmd.Printf("  %s — error: %v\n", e.Name(), err)
					continue
				}
				cmd.Printf("  %s — %s (ABI: %s)\n", e.Name(), p.Name, p.ABITag())
			}
		}

		// Show which profile would be used
		cfg, _ := config.Load()
		active := getProfile(cfg)
		cmd.Printf("\nActive profile: %s (ABI: %s)\n", active.Name, active.ABITag())

		return nil
	},
}

var profileCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new build profile based on current host",
	Long: `Create a new profile from the auto-detected host configuration.

By default, profiles are saved to ~/.sea/profiles/ (user-level).
Use --project to save to the project's profiles/ directory instead.

Examples:
  sea profile create default              # ~/.sea/profiles/default.toml
  sea profile create cross-arm64          # ~/.sea/profiles/cross-arm64.toml
  sea profile create release --project    # ./profiles/release.toml`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		host := profile.DetectHost()
		host.Name = name

		projectLevel, _ := cmd.Flags().GetBool("project")

		var profilesDir string
		if projectLevel {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			profilesDir = filepath.Join(dir, "profiles")
		} else {
			seaDir, err := config.SeaDir()
			if err != nil {
				return err
			}
			profilesDir = filepath.Join(seaDir, "profiles")
		}

		if err := os.MkdirAll(profilesDir, 0o755); err != nil {
			return fmt.Errorf("creating profiles directory: %w", err)
		}

		filename := name + ".toml"
		path := filepath.Join(profilesDir, filename)
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("profile %s already exists", path)
		}

		var buf bytes.Buffer
		enc := toml.NewEncoder(&buf)
		if err := enc.Encode(host); err != nil {
			return fmt.Errorf("encoding profile: %w", err)
		}

		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			return fmt.Errorf("writing profile: %w", err)
		}

		location := "user"
		if projectLevel {
			location = "project"
		}
		cmd.Printf("Created %s profile: %s (ABI: %s)\n", location, path, host.ABITag())
		return nil
	},
}

var profileShowCmd = &cobra.Command{
	Use:   "show <file>",
	Short: "Show details of a profile file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := profile.LoadFile(args[0])
		if err != nil {
			return err
		}
		cmd.Printf("Name:     %s\n", p.Name)
		cmd.Printf("OS:       %s\n", p.OS)
		cmd.Printf("Arch:     %s\n", p.Arch)
		cmd.Printf("Compiler: %s %s\n", p.Compiler, p.CompilerVersion)
		cmd.Printf("Stdlib:   %s\n", p.CppStdlib)
		cmd.Printf("Build:    %s\n", p.BuildType)
		cmd.Printf("ABI Tag:  %s\n", p.ABITag())
		if p.Sysroot != "" {
			cmd.Printf("Sysroot:  %s\n", p.Sysroot)
		}
		if p.ToolchainPrefix != "" {
			cmd.Printf("Prefix:   %s\n", p.ToolchainPrefix)
		}
		if len(p.Env) > 0 {
			cmd.Println("Env:")
			for k, v := range p.Env {
				cmd.Printf("  %s=%s\n", k, v)
			}
		}
		return nil
	},
}

func init() {
	profileCreateCmd.Flags().Bool("project", false, "save to project profiles/ directory instead of ~/.sea/profiles/")

	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileCreateCmd)
	profileCmd.AddCommand(profileShowCmd)
}
