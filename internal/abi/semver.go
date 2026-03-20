package abi

import (
	"fmt"
	"strings"
)

// VersionBump represents the type of version bump required.
type VersionBump int

const (
	BumpNone  VersionBump = iota // no ABI changes
	BumpPatch                    // internal changes only
	BumpMinor                    // new symbols added, none removed, no type changes
	BumpMajor                    // symbols removed, signatures changed, struct layouts changed
)

func (b VersionBump) String() string {
	switch b {
	case BumpNone:
		return "none"
	case BumpPatch:
		return "patch"
	case BumpMinor:
		return "minor"
	case BumpMajor:
		return "major"
	default:
		return "unknown"
	}
}

// ABIReport is the complete result of comparing two library versions.
type ABIReport struct {
	SymbolDiff  *ABIDiff
	TypeChanges []TypeChange
	Bump        VersionBump
	Reasons     []string // human-readable explanations for the bump level
}

// RequiredBump analyzes the diff between old and new symbols and determines
// the minimum semver bump required. Symbol-name-only check — no type info.
func RequiredBump(oldSyms, newSyms []Symbol) (VersionBump, *ABIDiff) {
	diff := DiffSymbols(oldSyms, newSyms)
	if len(diff.Removed) > 0 {
		return BumpMajor, diff
	}
	if len(diff.Added) > 0 {
		return BumpMinor, diff
	}
	return BumpNone, diff
}

// FullReport performs symbol-level and (when DWARF is available) type-level
// ABI analysis between two library binaries. This is the primary entry point
// for the publish gate and `sea abi check`.
func FullReport(oldPath, newPath string) (*ABIReport, error) {
	oldSyms, err := ExtractSymbols(oldPath)
	if err != nil {
		return nil, fmt.Errorf("extracting symbols from old library: %w", err)
	}
	newSyms, err := ExtractSymbols(newPath)
	if err != nil {
		return nil, fmt.Errorf("extracting symbols from new library: %w", err)
	}

	report := &ABIReport{
		SymbolDiff: DiffSymbols(oldSyms, newSyms),
		Bump:       BumpNone,
	}

	// Level 1: Symbol presence
	if len(report.SymbolDiff.Removed) > 0 {
		report.Bump = BumpMajor
		report.Reasons = append(report.Reasons,
			fmt.Sprintf("%d exported symbol(s) removed — consumers linking these will fail", len(report.SymbolDiff.Removed)))
	}
	if len(report.SymbolDiff.Added) > 0 && report.Bump < BumpMinor {
		report.Bump = BumpMinor
		report.Reasons = append(report.Reasons,
			fmt.Sprintf("%d new exported symbol(s) added", len(report.SymbolDiff.Added)))
	}

	// Level 2: DWARF type-level analysis (best effort — requires debug info)
	oldTypes := ExtractTypeInfoBestEffort(oldPath)
	newTypes := ExtractTypeInfoBestEffort(newPath)
	if oldTypes != nil && newTypes != nil {
		report.TypeChanges = DiffTypes(oldTypes, newTypes)
		if HasBreakingTypeChanges(report.TypeChanges) && report.Bump < BumpMajor {
			report.Bump = BumpMajor
			for _, tc := range report.TypeChanges {
				report.Reasons = append(report.Reasons,
					fmt.Sprintf("%s: %s (was %s, now %s)", tc.Symbol, tc.Kind, tc.Old, tc.New))
			}
		}
	} else {
		report.Reasons = append(report.Reasons,
			"DWARF debug info not available — type-level analysis skipped (build with -g for full ABI checking)")
	}

	return report, nil
}

// FullReportFromSymbolNames does a symbol-name-only comparison using stored
// symbol lists (e.g. from package metadata). No type-level analysis since
// there are no binaries to inspect.
func FullReportFromSymbolNames(oldNames, newNames []string) *ABIReport {
	oldSyms := make([]Symbol, len(oldNames))
	for i, n := range oldNames {
		oldSyms[i] = Symbol{Name: n}
	}
	newSyms := make([]Symbol, len(newNames))
	for i, n := range newNames {
		newSyms[i] = Symbol{Name: n}
	}

	report := &ABIReport{
		SymbolDiff: DiffSymbols(oldSyms, newSyms),
		Bump:       BumpNone,
	}

	if len(report.SymbolDiff.Removed) > 0 {
		report.Bump = BumpMajor
		report.Reasons = append(report.Reasons,
			fmt.Sprintf("%d exported symbol(s) removed", len(report.SymbolDiff.Removed)))
	} else if len(report.SymbolDiff.Added) > 0 {
		report.Bump = BumpMinor
		report.Reasons = append(report.Reasons,
			fmt.Sprintf("%d new exported symbol(s) added", len(report.SymbolDiff.Added)))
	}

	return report
}

// VerifyVersion checks whether a proposed version bump is valid given the ABI changes.
func VerifyVersion(oldMajor, oldMinor, oldPatch, newMajor, newMinor, newPatch int, oldSyms, newSyms []Symbol) error {
	required, diff := RequiredBump(oldSyms, newSyms)
	actualBump := classifyBump(oldMajor, oldMinor, oldPatch, newMajor, newMinor, newPatch)

	switch required {
	case BumpMajor:
		if actualBump < BumpMajor {
			return &VersionBumpError{
				Required: BumpMajor,
				Actual:   actualBump,
				Diff:     diff,
				Message: fmt.Sprintf(
					"removed %d symbol(s) — this is a breaking ABI change requiring a major version bump (expected %d.0.0+, got %d.%d.%d)",
					len(diff.Removed), oldMajor+1, newMajor, newMinor, newPatch),
			}
		}
	case BumpMinor:
		if actualBump < BumpMinor {
			return &VersionBumpError{
				Required: BumpMinor,
				Actual:   actualBump,
				Diff:     diff,
				Message: fmt.Sprintf(
					"added %d new symbol(s) — this requires at least a minor version bump (expected %d.%d.0+, got %d.%d.%d)",
					len(diff.Added), oldMajor, oldMinor+1, newMajor, newMinor, newPatch),
			}
		}
	}

	return nil
}

// VerifyVersionFull checks version bump validity using the full ABI report
// (symbols + DWARF types when available).
func VerifyVersionFull(oldMajor, oldMinor, oldPatch, newMajor, newMinor, newPatch int, report *ABIReport) error {
	actualBump := classifyBump(oldMajor, oldMinor, oldPatch, newMajor, newMinor, newPatch)

	if report.Bump > actualBump {
		var detail strings.Builder
		for _, r := range report.Reasons {
			detail.WriteString("  - ")
			detail.WriteString(r)
			detail.WriteString("\n")
		}
		return &VersionBumpError{
			Required: report.Bump,
			Actual:   actualBump,
			Diff:     report.SymbolDiff,
			Message: fmt.Sprintf(
				"ABI changes require a %s version bump, but only got a %s bump (%d.%d.%d → %d.%d.%d):\n%s",
				report.Bump, actualBump,
				oldMajor, oldMinor, oldPatch, newMajor, newMinor, newPatch,
				detail.String()),
		}
	}

	return nil
}

func classifyBump(oldMaj, oldMin, oldPat, newMaj, newMin, newPat int) VersionBump {
	if newMaj > oldMaj {
		return BumpMajor
	}
	if newMin > oldMin {
		return BumpMinor
	}
	if newPat > oldPat {
		return BumpPatch
	}
	return BumpNone
}

// VersionBumpError is returned when the proposed version doesn't match
// the actual ABI changes.
type VersionBumpError struct {
	Required VersionBump
	Actual   VersionBump
	Diff     *ABIDiff
	Message  string
}

func (e *VersionBumpError) Error() string {
	return e.Message
}
