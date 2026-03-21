package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rtorr/sea/internal/dirs"
	"github.com/rtorr/sea/internal/lockfile"
	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove all build artifacts and installed packages",
	Long: `Remove sea_packages/, sea_build/, sea_build_packages/, _src/, _sea_build/,
and sea.lock from the current project. Does not clear the global cache
(use 'sea cache clean' for that).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}

		cleanDirs := []string{
			dirs.SeaPackages,
			dirs.SeaBuild,
			dirs.SeaBuildPackages,
			dirs.SrcCache,
			dirs.SeaBuildInternal,
		}
		files := []string{
			lockfile.FileName,
		}

		removed := 0
		for _, d := range cleanDirs {
			p := filepath.Join(dir, d)
			if fi, err := os.Lstat(p); err == nil {
				if fi.Mode()&os.ModeSymlink != 0 {
					os.Remove(p)
				} else {
					os.RemoveAll(p)
				}
				cmd.Printf("  Removed %s/\n", d)
				removed++
			}
		}
		for _, f := range files {
			p := filepath.Join(dir, f)
			if _, err := os.Stat(p); err == nil {
				os.Remove(p)
				cmd.Printf("  Removed %s\n", f)
				removed++
			}
		}

		if removed == 0 {
			cmd.Println("Nothing to clean.")
		} else {
			cmd.Printf("Cleaned %d item(s).\n", removed)
		}
		return nil
	},
}

var reinstallCmd = &cobra.Command{
	Use:   "reinstall",
	Short: "Clean and reinstall all dependencies",
	Long:  "Equivalent to 'sea clean && sea install'. Removes all artifacts then re-resolves and installs fresh.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cleanCmd.RunE(cmd, nil); err != nil {
			return fmt.Errorf("clean: %w", err)
		}
		fmt.Println()
		return installCmd.RunE(cmd, args)
	},
}
