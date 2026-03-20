package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove all build artifacts and installed packages",
	Long: `Remove sea_packages/, sea_build/, sea_build_packages/, _src/, _fbuild/,
and sea.lock from the current project. Does not clear the global cache
(use 'sea cache clean' for that).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}

		dirs := []string{
			"sea_packages",
			"sea_build",
			"sea_build_packages",
			"_src",
			"_fbuild",
			"_build",
		}
		files := []string{
			"sea.lock",
		}

		removed := 0
		for _, d := range dirs {
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
		// Clean
		cleanCmd.RunE(cmd, nil)
		fmt.Println()
		// Install
		return installCmd.RunE(cmd, args)
	},
}
