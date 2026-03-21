package cli

import (
	"fmt"

	"github.com/rtorr/sea/internal/cache"
	"github.com/rtorr/sea/internal/config"
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the local package cache",
}

var cacheCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove all cached packages",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		c, err := cache.New(cfg)
		if err != nil {
			return err
		}

		size, _ := c.Size()
		if err := c.Clean(); err != nil {
			return err
		}

		// Also clean the build cache
		bc, bcErr := cache.NewBuildCache(c.Layout.Root)
		if bcErr == nil {
			if err := bc.Clean(); err != nil {
				cmd.Printf("Warning: could not clean build cache: %v\n", err)
			}
		}

		cmd.Printf("Cleaned cache (%s freed)\n", formatBytes(size))
		return nil
	},
}

var cacheListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all cached packages",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		c, err := cache.New(cfg)
		if err != nil {
			return err
		}

		packages, err := c.List()
		if err != nil {
			return err
		}

		if len(packages) == 0 {
			cmd.Println("Cache is empty.")
			return nil
		}

		cmd.Printf("Cached blobs (%d):\n", len(packages))
		for _, p := range packages {
			extracted := ""
			if p.Extracted {
				extracted = " (extracted)"
			}
			cmd.Printf("  %s %s%s\n", p.SHA256[:16], formatBytes(p.Size), extracted)
		}

		total, _ := c.Size()
		cmd.Printf("\nTotal cache size: %s\n", formatBytes(total))
		return nil
	},
}

var cacheInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show cache location and size",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		c, err := cache.New(cfg)
		if err != nil {
			return err
		}

		packages, _ := c.List()
		size, _ := c.Size()
		cmd.Printf("Cache directory: %s\n", c.Layout.Root)
		cmd.Printf("Cache size:      %s\n", formatBytes(size))
		cmd.Printf("Blobs:           %d\n", len(packages))
		return nil
	},
}

var cachePathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print cache directory path (for scripting/CI)",
	Long: `Print the cache directory path with no other output.

Useful for CI cache integration:

  # GitHub Actions example
  - uses: actions/cache@v4
    with:
      path: $(sea cache path)
      key: sea-${{ runner.os }}-${{ hashFiles('sea.lock') }}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		c, err := cache.New(cfg)
		if err != nil {
			return err
		}

		fmt.Fprintln(cmd.OutOrStdout(), c.Layout.Root)
		return nil
	},
}

func init() {
	cacheCmd.AddCommand(cacheCleanCmd)
	cacheCmd.AddCommand(cacheListCmd)
	cacheCmd.AddCommand(cacheInfoCmd)
	cacheCmd.AddCommand(cachePathCmd)
}

func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
