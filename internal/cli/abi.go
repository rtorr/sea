package cli

import (
	"fmt"

	"github.com/rtorr/sea/internal/abi"
	"github.com/spf13/cobra"
)

var abiCmd = &cobra.Command{
	Use:   "abi",
	Short: "ABI analysis tools",
}

var abiSymbolsCmd = &cobra.Command{
	Use:   "symbols <library>",
	Short: "List exported symbols from a library",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]

		format, err := abi.DetectFormat(path)
		if err != nil {
			return fmt.Errorf("detecting format: %w", err)
		}
		cmd.Printf("Format: %s\n", format)

		symbols, err := abi.ExtractSymbols(path)
		if err != nil {
			return fmt.Errorf("extracting symbols: %w", err)
		}

		cmd.Printf("Exported symbols (%d):\n", len(symbols))
		for _, s := range symbols {
			cmd.Printf("  [%s] %s (%s, %s)\n", s.Type, s.Name, s.Binding, s.Visibility)
		}

		report := abi.CheckVisibility(symbols, abi.DefaultPolicy())
		if !report.Clean {
			cmd.Printf("\nVisibility warnings:\n")
			if report.ExceedsMax {
				cmd.Printf("  ! Export count (%d) exceeds threshold\n", report.TotalExports)
			}
			for _, s := range report.LeakedSymbols {
				cmd.Printf("  ! Leaked: %s\n", s.Name)
			}
		} else {
			cmd.Println("\nVisibility: clean")
		}

		return nil
	},
}

var abiCheckCmd = &cobra.Command{
	Use:   "check <old-library> <new-library>",
	Short: "Compare ABI between two library versions",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		oldPath, newPath := args[0], args[1]

		report, err := abi.FullReport(oldPath, newPath)
		if err != nil {
			return err
		}

		// Symbol-level changes
		cmd.Print(report.SymbolDiff.String())

		// Type-level changes (from DWARF, if available)
		if len(report.TypeChanges) > 0 {
			cmd.Print(abi.FormatTypeChanges(report.TypeChanges))
		}

		// Verdict
		switch report.Bump {
		case abi.BumpMajor:
			cmd.Println("\nVerdict: MAJOR bump required")
			for _, r := range report.Reasons {
				cmd.Printf("  - %s\n", r)
			}
		case abi.BumpMinor:
			cmd.Println("\nVerdict: MINOR bump required")
			for _, r := range report.Reasons {
				cmd.Printf("  - %s\n", r)
			}
		case abi.BumpNone:
			cmd.Println("\nVerdict: No ABI changes — PATCH bump is sufficient")
		}

		if report.SymbolDiff.IsBreaking() || abi.HasBreakingTypeChanges(report.TypeChanges) {
			return fmt.Errorf("ABI break detected — requires major version bump")
		}

		return nil
	},
}

var abiVerifyCmd = &cobra.Command{
	Use:   "verify <old-library> <new-library> <old-version> <new-version>",
	Short: "Verify that a version bump matches ABI changes",
	Long: `Verify that the proposed version number correctly reflects ABI changes.

This compares symbols between the old and new library and checks:
  - Symbols removed → requires major version bump
  - Symbols added   → requires at least minor version bump
  - No changes      → patch bump is sufficient

Examples:
  sea abi verify old/libfoo.so new/libfoo.so 1.2.3 1.3.0   # OK: minor bump for new symbols
  sea abi verify old/libfoo.so new/libfoo.so 1.2.3 1.2.4   # ERROR if symbols changed`,
	Args: cobra.ExactArgs(4),
	RunE: func(cmd *cobra.Command, args []string) error {
		oldPath, newPath := args[0], args[1]
		oldVerStr, newVerStr := args[2], args[3]

		oldSyms, err := abi.ExtractSymbols(oldPath)
		if err != nil {
			return fmt.Errorf("extracting symbols from %s: %w", oldPath, err)
		}

		newSyms, err := abi.ExtractSymbols(newPath)
		if err != nil {
			return fmt.Errorf("extracting symbols from %s: %w", newPath, err)
		}

		// Parse versions (importing resolver would create a cycle, so parse manually)
		var oldMaj, oldMin, oldPat int
		if _, err := fmt.Sscanf(oldVerStr, "%d.%d.%d", &oldMaj, &oldMin, &oldPat); err != nil {
			return fmt.Errorf("invalid old version %q: must be major.minor.patch", oldVerStr)
		}
		var newMaj, newMin, newPat int
		if _, err := fmt.Sscanf(newVerStr, "%d.%d.%d", &newMaj, &newMin, &newPat); err != nil {
			return fmt.Errorf("invalid new version %q: must be major.minor.patch", newVerStr)
		}

		// Show the diff
		diff := abi.DiffSymbols(oldSyms, newSyms)
		cmd.Print(diff.String())

		// Verify the version bump
		if err := abi.VerifyVersion(oldMaj, oldMin, oldPat, newMaj, newMin, newPat, oldSyms, newSyms); err != nil {
			return err
		}

		bump, _ := abi.RequiredBump(oldSyms, newSyms)
		cmd.Printf("\nVersion bump %s → %s is valid (required: %s)\n", oldVerStr, newVerStr, bump)
		return nil
	},
}

func init() {
	abiCmd.AddCommand(abiSymbolsCmd)
	abiCmd.AddCommand(abiCheckCmd)
	abiCmd.AddCommand(abiVerifyCmd)
}
