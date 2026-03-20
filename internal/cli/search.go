package cli

import (
	"fmt"
	"strings"

	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/registry"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search for packages across all registries",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		multi, err := registry.NewMulti(cfg)
		if err != nil {
			return err
		}

		results, err := multi.Search(args[0])
		if err != nil {
			return err
		}

		if len(results) == 0 {
			cmd.Println("No packages found.")
			return nil
		}

		for _, r := range results {
			versions := "none"
			if len(r.Versions) > 0 {
				// Show latest 3
				show := r.Versions
				if len(show) > 3 {
					show = show[:3]
				}
				versions = strings.Join(show, ", ")
				if len(r.Versions) > 3 {
					versions += fmt.Sprintf(" (+%d more)", len(r.Versions)-3)
				}
			}
			cmd.Printf("  %s [%s] — versions: %s\n", r.Name, r.Registry, versions)
		}
		return nil
	},
}
