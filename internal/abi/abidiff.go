package abi

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// ABIDiff represents the difference between two symbol sets.
type ABIDiff struct {
	Added   []string
	Removed []string
	Changed []string // placeholder for type changes (requires abidiff)
}

// DiffSymbols computes the symbol-level diff between old and new symbol lists.
func DiffSymbols(oldSyms, newSyms []Symbol) *ABIDiff {
	oldMap := make(map[string]Symbol)
	for _, s := range oldSyms {
		oldMap[s.Name] = s
	}
	newMap := make(map[string]Symbol)
	for _, s := range newSyms {
		newMap[s.Name] = s
	}

	diff := &ABIDiff{}

	for name := range newMap {
		if _, ok := oldMap[name]; !ok {
			diff.Added = append(diff.Added, name)
		}
	}
	for name := range oldMap {
		if _, ok := newMap[name]; !ok {
			diff.Removed = append(diff.Removed, name)
		}
	}

	sort.Strings(diff.Added)
	sort.Strings(diff.Removed)
	return diff
}

// IsBreaking returns true if the diff contains breaking changes (removed symbols).
func (d *ABIDiff) IsBreaking() bool {
	return len(d.Removed) > 0
}

// String returns a human-readable summary of the diff.
func (d *ABIDiff) String() string {
	var sb strings.Builder
	if len(d.Added) > 0 {
		sb.WriteString(fmt.Sprintf("Added (%d):\n", len(d.Added)))
		for _, s := range d.Added {
			sb.WriteString("  + " + s + "\n")
		}
	}
	if len(d.Removed) > 0 {
		sb.WriteString(fmt.Sprintf("Removed (%d):\n", len(d.Removed)))
		for _, s := range d.Removed {
			sb.WriteString("  - " + s + "\n")
		}
	}
	if len(d.Added) == 0 && len(d.Removed) == 0 {
		sb.WriteString("No symbol changes.\n")
	}
	return sb.String()
}

// RunAbidiff runs libabigail's abidiff tool for deep ABI comparison.
// Returns the tool's output. This is optional and fails gracefully.
func RunAbidiff(oldLib, newLib string) (string, error) {
	if _, err := exec.LookPath("abidiff"); err != nil {
		return "", fmt.Errorf("abidiff not found: install libabigail for deep ABI comparison")
	}

	out, err := exec.Command("abidiff", oldLib, newLib).CombinedOutput()
	if err != nil {
		// abidiff returns non-zero if there are differences
		return string(out), nil
	}
	return string(out), nil
}
