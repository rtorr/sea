package abi

import (
	"fmt"
	"sort"
	"strings"
)

// VisibilityPolicy defines rules for symbol visibility enforcement.
type VisibilityPolicy struct {
	// InternalPatterns are patterns that should NOT be exported.
	InternalPatterns []string
	// MaxExports is the maximum number of exported symbols before warning.
	MaxExports int
}

// DefaultPolicy returns the default visibility policy.
func DefaultPolicy() VisibilityPolicy {
	return VisibilityPolicy{
		InternalPatterns: []string{
			"_Internal",
			"detail::",
			"impl::",
			"__internal_",
			"_ZN.*detail",
			"_ZN.*Impl",
		},
		MaxExports: 1000,
	}
}

// VisibilityReport is the result of checking symbol visibility.
type VisibilityReport struct {
	TotalExports  int
	LeakedSymbols []Symbol
	Clean         bool
	ExceedsMax    bool
}

// CheckVisibility checks symbols against a visibility policy.
func CheckVisibility(symbols []Symbol, policy VisibilityPolicy) *VisibilityReport {
	report := &VisibilityReport{
		TotalExports: len(symbols),
	}

	for _, sym := range symbols {
		for _, pattern := range policy.InternalPatterns {
			if matchesPattern(sym.Name, pattern) {
				report.LeakedSymbols = append(report.LeakedSymbols, sym)
				break
			}
		}
	}

	if policy.MaxExports > 0 && len(symbols) > policy.MaxExports {
		report.ExceedsMax = true
	}

	report.Clean = len(report.LeakedSymbols) == 0 && !report.ExceedsMax
	return report
}

func matchesPattern(name, pattern string) bool {
	return strings.Contains(name, pattern)
}

// StaticLeakReport describes symbols from statically-linked dependencies
// that leaked into a shared library's export table.
type StaticLeakReport struct {
	// Leaks maps dependency name → list of leaked symbol names.
	Leaks map[string][]string
	// TotalLeaked is the total count of leaked symbols across all deps.
	TotalLeaked int
	// Clean is true when no dependency symbols leaked.
	Clean bool
}

// CheckStaticLeaks detects when symbols from statically-linked dependencies
// appear in a shared library's export table. This causes symbol collisions
// when multiple consumers link the same dependency.
//
// libExports: exported symbols from the shared library being published.
// depSymbols: map of dependency name → that dependency's known exported symbols.
func CheckStaticLeaks(libExports []Symbol, depSymbols map[string][]string) *StaticLeakReport {
	report := &StaticLeakReport{
		Leaks: make(map[string][]string),
	}

	// Build a set of library exports for fast lookup
	exportSet := make(map[string]bool, len(libExports))
	for _, sym := range libExports {
		exportSet[sym.Name] = true
	}

	// Check each dependency's symbols against the library's exports
	for depName, depSyms := range depSymbols {
		var leaked []string
		for _, sym := range depSyms {
			if exportSet[sym] {
				leaked = append(leaked, sym)
			}
		}
		if len(leaked) > 0 {
			sort.Strings(leaked)
			report.Leaks[depName] = leaked
			report.TotalLeaked += len(leaked)
		}
	}

	report.Clean = report.TotalLeaked == 0
	return report
}

// FormatStaticLeakReport returns a human-readable description of symbol leaks.
func FormatStaticLeakReport(report *StaticLeakReport) string {
	if report.Clean {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Static dependency symbol leak: %d symbol(s) from %d dependency(ies) are visible in your shared library's export table.\n",
		report.TotalLeaked, len(report.Leaks)))
	sb.WriteString("This will cause symbol collisions if consumers also link these dependencies.\n\n")

	for depName, syms := range report.Leaks {
		sb.WriteString(fmt.Sprintf("  From %s (%d symbols):\n", depName, len(syms)))
		shown := syms
		if len(shown) > 10 {
			shown = shown[:10]
		}
		for _, s := range shown {
			sb.WriteString(fmt.Sprintf("    ! %s\n", s))
		}
		if len(syms) > 10 {
			sb.WriteString(fmt.Sprintf("    ... and %d more\n", len(syms)-10))
		}
	}

	return sb.String()
}

// GenerateVersionScript creates a GNU ld version script that exports only
// the given symbols and hides everything else. This is the standard fix
// for static dependency leaks on Linux.
func GenerateVersionScript(exportSymbols []string) string {
	var sb strings.Builder
	sb.WriteString("{\n  global:\n")
	sort.Strings(exportSymbols)
	for _, sym := range exportSymbols {
		sb.WriteString(fmt.Sprintf("    %s;\n", sym))
	}
	sb.WriteString("  local:\n    *;\n};\n")
	return sb.String()
}

// GenerateExportList creates a macOS -exported_symbols_list file that exports
// only the given symbols (with leading underscore per macOS convention).
func GenerateExportList(exportSymbols []string) string {
	var sb strings.Builder
	sort.Strings(exportSymbols)
	for _, sym := range exportSymbols {
		// macOS symbols have a leading underscore
		if !strings.HasPrefix(sym, "_") {
			sb.WriteString("_")
		}
		sb.WriteString(sym)
		sb.WriteString("\n")
	}
	return sb.String()
}

// IdentifyOwnSymbols returns the symbols in libExports that do NOT appear
// in any dependency's symbol list. These are the library's own symbols.
func IdentifyOwnSymbols(libExports []Symbol, depSymbols map[string][]string) []string {
	depSet := make(map[string]bool)
	for _, syms := range depSymbols {
		for _, s := range syms {
			depSet[s] = true
		}
	}

	var own []string
	for _, sym := range libExports {
		if !depSet[sym.Name] {
			own = append(own, sym.Name)
		}
	}
	sort.Strings(own)
	return own
}
