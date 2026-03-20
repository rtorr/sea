package resolver

import (
	"fmt"
	"strings"
)

// ResolveError represents a dependency resolution failure with a derivation chain.
type ResolveError struct {
	Package    string
	Derivation []string // chain of reasoning explaining why resolution failed
}

func (e *ResolveError) Error() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("cannot resolve dependency %q:\n", e.Package))
	for _, step := range e.Derivation {
		sb.WriteString("  - ")
		sb.WriteString(step)
		sb.WriteString("\n")
	}
	return sb.String()
}

// ConflictError represents a version conflict between two dependency paths.
type ConflictError struct {
	Package string
	// Path1/Path2 show the dependency chains that led to the conflict
	Path1 string
	Path2 string
	// What each path required
	Range1 string
	Range2 string
}

func (e *ConflictError) Error() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("version conflict for %q:\n", e.Package))
	sb.WriteString(fmt.Sprintf("  %s requires %s %s\n", e.Path1, e.Package, e.Range1))
	sb.WriteString(fmt.Sprintf("  %s requires %s %s\n", e.Path2, e.Package, e.Range2))
	sb.WriteString("  these constraints are incompatible — no version satisfies both")
	return sb.String()
}

// ABIMismatchError represents an ABI tag mismatch.
type ABIMismatchError struct {
	Package   string
	Version   string
	WantABI   string
	HaveABIs  []string
}

func (e *ABIMismatchError) Error() string {
	have := "none"
	if len(e.HaveABIs) > 0 {
		have = strings.Join(e.HaveABIs, ", ")
	}
	return fmt.Sprintf("no compatible ABI for %s@%s:\n  need: %s\n  available: %s",
		e.Package, e.Version, e.WantABI, have)
}

// NoVersionsError is returned when a package has no versions at all.
type NoVersionsError struct {
	Package string
}

func (e *NoVersionsError) Error() string {
	return fmt.Sprintf("package %q has no available versions in any configured registry", e.Package)
}
