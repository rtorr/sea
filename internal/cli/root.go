package cli

import (
	"errors"
	"os"

	"github.com/spf13/cobra"
)

var (
	verbose     bool
	profileFlag string
)

// version is set by goreleaser via -ldflags at build time.
var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "sea",
	Short: "A C/C++ package manager with semver + ABI tags",
	Long:  "Sea is a C/C++ package manager that uses semver and human-readable ABI tags for package identity, replacing hash-based approaches.",
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")
	rootCmd.PersistentFlags().StringVar(&profileFlag, "profile", "", "build profile to use")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(profileCmd)
	rootCmd.AddCommand(remoteCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(envCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(lockCmd)
	rootCmd.AddCommand(cacheCmd)
	rootCmd.AddCommand(buildCmd)
	rootCmd.AddCommand(publishCmd)
	rootCmd.AddCommand(abiCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(reinstallCmd)
	rootCmd.AddCommand(auditCmd)
}

// Execute runs the root command.
func Execute() error {
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true
	err := rootCmd.Execute()
	if err != nil {
		// errUpdatesAvailable means --check found updates; exit 1 without "Error:" prefix
		if errors.Is(err, errUpdatesAvailable) {
			os.Exit(1)
		}
	}
	return err
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the sea version",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Printf("sea version %s\n", version)
	},
}
