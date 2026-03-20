package cli

import (
	"fmt"

	"github.com/rtorr/sea/internal/config"
	"github.com/spf13/cobra"
)

var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Manage package registries",
}

var remoteListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured remotes",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if len(cfg.Remotes) == 0 {
			cmd.Println("No remotes configured. Use 'sea remote add' to add one.")
			return nil
		}
		for _, r := range cfg.Remotes {
			switch r.Type {
			case "filesystem":
				cmd.Printf("  %s (%s) → %s\n", r.Name, r.Type, r.Path)
			default:
				cmd.Printf("  %s (%s) → %s/%s\n", r.Name, r.Type, r.URL, r.Repository)
			}
		}
		return nil
	},
}

var remoteAddCmd = &cobra.Command{
	Use:   "add <name> <type> <url-or-path>",
	Short: "Add a remote registry",
	Long: `Add a remote registry. Types: artifactory, github, filesystem.

Examples:
  sea remote add corp artifactory https://artifactory.corp.com/artifactory --repo sea-packages --token-env ARTIFACTORY_TOKEN
  sea remote add local-pkgs filesystem /opt/sea-packages
  sea remote add oss github github.com/org`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, typ, urlOrPath := args[0], args[1], args[2]

		remote := config.Remote{
			Name: name,
			Type: typ,
		}

		switch typ {
		case "filesystem":
			remote.Path = urlOrPath
		case "artifactory":
			remote.URL = urlOrPath
			remote.Repository, _ = cmd.Flags().GetString("repo")
			remote.TokenEnv, _ = cmd.Flags().GetString("token-env")
		case "github":
			remote.URL = urlOrPath
			remote.TokenEnv, _ = cmd.Flags().GetString("token-env")
		default:
			return fmt.Errorf("unknown remote type %q (use artifactory, github, or filesystem)", typ)
		}

		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if err := cfg.AddRemote(remote); err != nil {
			return err
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
		cmd.Printf("Added remote %q (%s)\n", name, typ)
		return nil
	},
}

var remoteRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a remote registry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if err := cfg.RemoveRemote(args[0]); err != nil {
			return err
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
		cmd.Printf("Removed remote %q\n", args[0])
		return nil
	},
}

func init() {
	remoteAddCmd.Flags().String("repo", "", "repository name (for artifactory)")
	remoteAddCmd.Flags().String("token-env", "", "environment variable name for auth token")

	remoteCmd.AddCommand(remoteListCmd)
	remoteCmd.AddCommand(remoteAddCmd)
	remoteCmd.AddCommand(remoteRemoveCmd)
}
