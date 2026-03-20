package resolver

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Version represents a strict semantic version: major.minor.patch.
// No pre-release, no build metadata. The version number is the sole compatibility
// signal. Whether a package is beta/rc/dev is tracked separately via channels.
//
// Compatibility rules:
//   - Major bump = breaking ABI/API change
//   - Minor bump = new features, backwards-compatible
//   - Patch bump = bug fixes, fully compatible
type Version struct {
	Major int
	Minor int
	Patch int
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare returns -1, 0, or 1.
func (v Version) Compare(other Version) int {
	if v.Major != other.Major {
		return cmpInt(v.Major, other.Major)
	}
	if v.Minor != other.Minor {
		return cmpInt(v.Minor, other.Minor)
	}
	if v.Patch != other.Patch {
		return cmpInt(v.Patch, other.Patch)
	}
	return 0
}

// IsZero returns true if this is the zero version.
func (v Version) IsZero() bool {
	return v.Major == 0 && v.Minor == 0 && v.Patch == 0
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// ParseVersion parses a strict major.minor.patch version string.
// Accepts optional "v" prefix. Rejects pre-release suffixes, build metadata,
// and any non-numeric components.
func ParseVersion(s string) (Version, error) {
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimSpace(s)
	if s == "" {
		return Version{}, fmt.Errorf("empty version string")
	}

	// Reject anything with pre-release or build metadata
	if strings.ContainsAny(s, "-+") {
		return Version{}, fmt.Errorf("invalid version %q: only major.minor.patch allowed (no pre-release or build metadata — use channels for beta/rc/dev)", s)
	}

	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("invalid version %q: must be major.minor.patch (e.g. 1.2.3)", s)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return Version{}, fmt.Errorf("invalid major version in %q: must be a non-negative integer", s)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return Version{}, fmt.Errorf("invalid minor version in %q: must be a non-negative integer", s)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil || patch < 0 {
		return Version{}, fmt.Errorf("invalid patch version in %q: must be a non-negative integer", s)
	}

	return Version{Major: major, Minor: minor, Patch: patch}, nil
}

// MustParseVersion parses a version string and panics on error. For tests only.
func MustParseVersion(s string) Version {
	v, err := ParseVersion(s)
	if err != nil {
		panic(fmt.Sprintf("MustParseVersion(%q): %v", s, err))
	}
	return v
}

// VersionRange represents a constraint on versions.
type VersionRange struct {
	Constraints []Constraint
	Raw         string // original string for display
}

func (vr VersionRange) String() string {
	if vr.Raw != "" {
		return vr.Raw
	}
	if len(vr.Constraints) == 0 {
		return "*"
	}
	parts := make([]string, len(vr.Constraints))
	for i, c := range vr.Constraints {
		parts[i] = c.String()
	}
	return strings.Join(parts, ", ")
}

// IsAny returns true if this range accepts all versions.
func (vr VersionRange) IsAny() bool {
	return len(vr.Constraints) == 0
}

// Constraint is a single version constraint (operator + version).
type Constraint struct {
	Op      string // ">=", "<=", ">", "<", "=", "!="
	Version Version
}

func (c Constraint) String() string {
	return c.Op + c.Version.String()
}

// ParseRange parses a version range string.
// Supported formats:
//   - ">=1.3.0, <2.0.0"  (comma-separated)
//   - "^3.1.0"           (caret: same major, i.e. ABI-compatible)
//   - "~1.84.0"          (tilde: same minor)
//   - "=1.2.3"           (exact)
//   - ">=1.0.0"          (single constraint)
//   - "*" or ""          (any version)
func ParseRange(s string) (VersionRange, error) {
	raw := s
	s = strings.TrimSpace(s)
	if s == "" || s == "*" {
		return VersionRange{Raw: raw}, nil // any version
	}

	var vr VersionRange
	vr.Raw = raw

	// Handle caret (^) — same major version = ABI compatible
	// ^0.x.y is special: same minor (since 0.x is pre-stable, minor bumps can break)
	if strings.HasPrefix(s, "^") {
		v, err := ParseVersion(strings.TrimPrefix(s, "^"))
		if err != nil {
			return VersionRange{}, fmt.Errorf("invalid caret range %q: %w", s, err)
		}
		upper := Version{Major: v.Major + 1}
		if v.Major == 0 {
			upper = Version{Major: 0, Minor: v.Minor + 1}
		}
		vr.Constraints = []Constraint{
			{Op: ">=", Version: v},
			{Op: "<", Version: upper},
		}
		return vr, nil
	}

	// Handle tilde (~) — same minor version
	if strings.HasPrefix(s, "~") {
		v, err := ParseVersion(strings.TrimPrefix(s, "~"))
		if err != nil {
			return VersionRange{}, fmt.Errorf("invalid tilde range %q: %w", s, err)
		}
		vr.Constraints = []Constraint{
			{Op: ">=", Version: v},
			{Op: "<", Version: Version{Major: v.Major, Minor: v.Minor + 1}},
		}
		return vr, nil
	}

	// Handle comma-separated constraints
	parts := strings.Split(s, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		c, err := parseConstraint(part)
		if err != nil {
			return VersionRange{}, err
		}
		vr.Constraints = append(vr.Constraints, c)
	}

	if len(vr.Constraints) == 0 {
		return VersionRange{}, fmt.Errorf("empty version range %q", raw)
	}

	return vr, nil
}

func parseConstraint(s string) (Constraint, error) {
	s = strings.TrimSpace(s)

	for _, op := range []string{">=", "<=", "!=", ">", "<", "=="} {
		if strings.HasPrefix(s, op) {
			v, err := ParseVersion(strings.TrimSpace(s[len(op):]))
			if err != nil {
				return Constraint{}, fmt.Errorf("invalid constraint %q: %w", s, err)
			}
			actualOp := op
			if actualOp == "==" {
				actualOp = "="
			}
			return Constraint{Op: actualOp, Version: v}, nil
		}
	}

	// Single "=" prefix
	if strings.HasPrefix(s, "=") {
		v, err := ParseVersion(strings.TrimSpace(s[1:]))
		if err != nil {
			return Constraint{}, fmt.Errorf("invalid constraint %q: %w", s, err)
		}
		return Constraint{Op: "=", Version: v}, nil
	}

	// Plain version = exact match
	v, err := ParseVersion(s)
	if err != nil {
		return Constraint{}, fmt.Errorf("invalid version constraint %q: %w", s, err)
	}
	return Constraint{Op: "=", Version: v}, nil
}

// Contains checks if a version satisfies the range.
func (vr VersionRange) Contains(v Version) bool {
	if len(vr.Constraints) == 0 {
		return true // empty range = any
	}
	for _, c := range vr.Constraints {
		if !c.Satisfied(v) {
			return false
		}
	}
	return true
}

// Satisfied checks if a version satisfies a single constraint.
func (c Constraint) Satisfied(v Version) bool {
	cmp := v.Compare(c.Version)
	switch c.Op {
	case ">=":
		return cmp >= 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case "<":
		return cmp < 0
	case "=":
		return cmp == 0
	case "!=":
		return cmp != 0
	default:
		return false
	}
}

// Intersect returns the intersection of two ranges (all constraints from both).
// Returns an error if the intersection is provably empty.
func Intersect(a, b VersionRange) (VersionRange, error) {
	merged := VersionRange{
		Raw: a.String() + " ∩ " + b.String(),
	}
	merged.Constraints = append(merged.Constraints, a.Constraints...)
	merged.Constraints = append(merged.Constraints, b.Constraints...)

	if err := checkContradiction(merged); err != nil {
		return VersionRange{}, err
	}

	return merged, nil
}

// checkContradiction does a best-effort check for impossible constraint sets.
func checkContradiction(vr VersionRange) error {
	var lowerBound *Version
	var upperBound *Version
	var lowerInclusive, upperInclusive bool

	for _, c := range vr.Constraints {
		switch c.Op {
		case ">=":
			if lowerBound == nil || c.Version.Compare(*lowerBound) > 0 {
				v := c.Version
				lowerBound = &v
				lowerInclusive = true
			}
		case ">":
			if lowerBound == nil || c.Version.Compare(*lowerBound) >= 0 {
				v := c.Version
				lowerBound = &v
				lowerInclusive = false
			}
		case "<=":
			if upperBound == nil || c.Version.Compare(*upperBound) < 0 {
				v := c.Version
				upperBound = &v
				upperInclusive = true
			}
		case "<":
			if upperBound == nil || c.Version.Compare(*upperBound) <= 0 {
				v := c.Version
				upperBound = &v
				upperInclusive = false
			}
		}
	}

	if lowerBound != nil && upperBound != nil {
		cmp := lowerBound.Compare(*upperBound)
		if cmp > 0 {
			return fmt.Errorf("impossible constraint: lower bound %s > upper bound %s", lowerBound, upperBound)
		}
		if cmp == 0 && (!lowerInclusive || !upperInclusive) {
			return fmt.Errorf("impossible constraint: bounds exclude %s", lowerBound)
		}
	}

	return nil
}

// SortVersions sorts versions in descending order (newest first).
func SortVersions(versions []Version) {
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Compare(versions[j]) > 0
	})
}

// FilterVersions returns only those versions that satisfy the range.
func FilterVersions(versions []Version, vr VersionRange) []Version {
	var result []Version
	for _, v := range versions {
		if vr.Contains(v) {
			result = append(result, v)
		}
	}
	return result
}
