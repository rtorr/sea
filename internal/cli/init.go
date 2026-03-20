package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rtorr/sea/internal/manifest"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [name]",
	Short: "Initialize a new sea.toml in the current directory",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}

		path := filepath.Join(dir, manifest.FileName)
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists", manifest.FileName)
		}

		name := filepath.Base(dir)
		if len(args) > 0 {
			name = args[0]
		}

		data := manifest.DefaultManifestTOML(name)

		if err := os.WriteFile(path, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", manifest.FileName, err)
		}

		cmd.Printf("Created %s for package %q\n", manifest.FileName, name)
		return nil
	},
}
