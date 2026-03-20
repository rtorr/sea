package manifest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

const FileName = "sea.toml"

// packageNameRe validates package names: lowercase letters, digits, hyphens.
// Must start with a letter, 2-64 chars.
var packageNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

// semverRe validates strict semver: major.minor.patch only. No pre-release, no build metadata.
// Compatibility is signaled purely by the version number. Use channels for beta/rc/dev.
var semverRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// Load reads and parses a sea.toml from the given directory.
func Load(dir string) (*Manifest, error) {
	path := filepath.Join(dir, FileName)
	return LoadFile(path)
}

// LoadFile reads and parses a sea.toml from the given path.
func LoadFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	return Parse(data)
}

// Parse parses a sea.toml from bytes.
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	// Initialize nil maps to empty so callers can always range over them.
	if m.Dependencies == nil {
		m.Dependencies = make(map[string]Dependency)
	}
	if m.BuildDeps == nil {
		m.BuildDeps = make(map[string]Dependency)
	}
	if m.Profiles == nil {
		m.Profiles = make(map[string]ProfileRef)
	}
	if m.Features == nil {
		m.Features = make(map[string]Feature)
	}

	if err := Validate(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks a manifest for correctness. Exported so callers (e.g. publish)
// can re-validate after programmatic changes.
func Validate(m *Manifest) error {
	var errs []string

	// Package name
	if m.Package.Name == "" {
		errs = append(errs, "package.name is required")
	} else if !packageNameRe.MatchString(m.Package.Name) {
		errs = append(errs, fmt.Sprintf("package.name %q is invalid: must be 2-64 lowercase letters, digits, or hyphens, starting with a letter", m.Package.Name))
	}

	// Package version — strict major.minor.patch only
	if m.Package.Version == "" {
		errs = append(errs, "package.version is required")
	} else if !semverRe.MatchString(m.Package.Version) {
		errs = append(errs, fmt.Sprintf("package.version %q must be strict major.minor.patch (e.g. 1.2.3) — no pre-release suffixes; use channel for beta/rc/dev", m.Package.Version))
	}

	// Channel
	switch m.Package.Channel {
	case "", "stable", "beta", "rc", "dev":
		// valid — empty defaults to "stable"
	default:
		errs = append(errs, fmt.Sprintf("package.channel %q is invalid (must be one of: stable, beta, rc, dev)", m.Package.Channel))
	}

	// Package kind
	switch m.Package.Kind {
	case "", "source", "prebuilt", "header-only":
		// valid — empty defaults to "source"
	default:
		errs = append(errs, fmt.Sprintf("package.kind %q is invalid (must be one of: source, prebuilt, header-only)", m.Package.Kind))
	}

	// Build visibility
	if m.Build.Visibility != "" {
		switch m.Build.Visibility {
		case "hidden", "default":
			// valid
		default:
			errs = append(errs, fmt.Sprintf("build.visibility %q is invalid (must be hidden or default)", m.Build.Visibility))
		}
	}

	// Dependencies
	for name, dep := range m.Dependencies {
		if dep.Version == "" {
			errs = append(errs, fmt.Sprintf("dependency %q requires a version constraint", name))
		}
		if !packageNameRe.MatchString(name) {
			errs = append(errs, fmt.Sprintf("dependency name %q is invalid: must be 2-64 lowercase letters, digits, or hyphens", name))
		}
		switch dep.Linking {
		case "", "static", "shared":
			// valid
		default:
			errs = append(errs, fmt.Sprintf("dependency %q has invalid linking %q (must be static, shared, or empty)", name, dep.Linking))
		}
	}

	// Build dependencies
	for name, dep := range m.BuildDeps {
		if dep.Version == "" {
			errs = append(errs, fmt.Sprintf("build-dependency %q requires a version constraint", name))
		}
		if !packageNameRe.MatchString(name) {
			errs = append(errs, fmt.Sprintf("build-dependency name %q is invalid", name))
		}
		switch dep.Linking {
		case "", "static", "shared":
			// valid
		default:
			errs = append(errs, fmt.Sprintf("build-dependency %q has invalid linking %q (must be static, shared, or empty)", name, dep.Linking))
		}
	}

	// Profile references
	for name, pref := range m.Profiles {
		if pref.File == "" {
			errs = append(errs, fmt.Sprintf("profile %q requires a file path", name))
		}
	}

	// Publish include patterns should not be empty strings
	for i, pat := range m.Publish.Include {
		if pat == "" {
			errs = append(errs, fmt.Sprintf("publish.include[%d] is empty", i))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("manifest validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// EffectiveChannel returns the channel, defaulting to "stable".
func (m *Manifest) EffectiveChannel() string {
	if m.Package.Channel == "" {
		return "stable"
	}
	return m.Package.Channel
}

// EffectiveKind returns the package kind, defaulting to "source".
func (m *Manifest) EffectiveKind() string {
	if m.Package.Kind == "" {
		return "source"
	}
	return m.Package.Kind
}

// AllDependencies returns dependencies merged with optional deps (if include is true).
func (m *Manifest) AllDependencies(includeOptional bool) map[string]Dependency {
	result := make(map[string]Dependency, len(m.Dependencies))
	for k, v := range m.Dependencies {
		if v.Optional && !includeOptional {
			continue
		}
		result[k] = v
	}
	return result
}

// AllDependenciesWithFeatures returns base (non-optional) dependencies merged
// with any dependencies gated by the given enabled features. If a feature-gated
// dependency has the same name as a base dependency, the feature version overrides.
func (m *Manifest) AllDependenciesWithFeatures(enabledFeatures []string) map[string]Dependency {
	result := m.AllDependencies(false)

	enabled := make(map[string]bool, len(enabledFeatures))
	for _, f := range enabledFeatures {
		enabled[f] = true
	}

	for name, feature := range m.Features {
		if !enabled[name] {
			continue
		}
		for depName, dep := range feature.Dependencies {
			result[depName] = dep
		}
	}

	return result
}

// DefaultManifest returns a starter manifest for sea init.
func DefaultManifest(name string) *Manifest {
	return &Manifest{
		Package: Package{
			Name:    name,
			Version: "0.1.0",
			Kind:    "source",
		},
		Dependencies: make(map[string]Dependency),
		BuildDeps:    make(map[string]Dependency),
		Profiles:     make(map[string]ProfileRef),
	}
}

// DefaultManifestTOML returns a starter sea.toml as formatted TOML.
func DefaultManifestTOML(name string) []byte {
	return []byte(fmt.Sprintf(`[package]
name = %q
version = "0.1.0"
# channel = "stable"  # stable | beta | rc | dev
kind = "source"

[dependencies]
# zlib = { version = ">=1.3.0, <2.0.0" }

[build]
# script = "build.sh"
# visibility = "hidden"

# [publish]
# registry = "my-registry"
# include = ["include/**", "lib/**", "LICENSE"]
`, name))
}

// Marshal serializes a Manifest to TOML bytes.
func Marshal(m *Manifest) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := toml.NewEncoder(buf)
	if err := enc.Encode(m); err != nil {
		return nil, fmt.Errorf("encoding manifest: %w", err)
	}
	return buf.Bytes(), nil
}
