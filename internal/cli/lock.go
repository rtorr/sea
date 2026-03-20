package cli

import (
	"os"

	"github.com/rtorr/sea/internal/lockfile"
	"github.com/spf13/cobra"
)

var lockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Display or manage the lockfile",
}

var lockShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display lockfile contents",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}

		lf, err := lockfile.Load(dir)
		if err != nil {
			return err
		}
		if lf == nil {
			cmd.Println("No sea.lock found. Run 'sea install' to generate one.")
			return nil
		}

		cmd.Printf("sea.lock (version %d) — %d package(s)\n\n", lf.Version, len(lf.Packages))
		for _, pkg := range lf.Packages {
			shaShort := pkg.SHA256
			if len(shaShort) > 12 {
				shaShort = shaShort[:12]
			}
			cmd.Printf("  %s@%s\n", pkg.Name, pkg.Version)
			cmd.Printf("    abi:      %s\n", pkg.ABI)
			cmd.Printf("    sha256:   %s...\n", shaShort)
			cmd.Printf("    registry: %s\n", pkg.Registry)
			if len(pkg.Deps) > 0 {
				cmd.Printf("    deps:     %v\n", pkg.Deps)
			}
		}
		return nil
	},
}

func init() {
	lockCmd.AddCommand(lockShowCmd)

	// Also make "sea lock" with no subcommand show the lockfile
	lockCmd.RunE = lockShowCmd.RunE
}
