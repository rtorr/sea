package resolver

import (
	"fmt"

	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/profile"
	"github.com/rtorr/sea/internal/registry"
)

// RegistryProvider implements PackageProvider using a multi-registry backend.
type RegistryProvider struct {
	multi            *registry.Multi
	abiTag           string
	localFingerprint string // ABI probe fingerprint of the consumer's toolchain
}

// NewRegistryProvider creates a provider backed by registries.
func NewRegistryProvider(multi *registry.Multi, abiTag, localFingerprint string) *RegistryProvider {
	return &RegistryProvider{
		multi:            multi,
		abiTag:           abiTag,
		localFingerprint: localFingerprint,
	}
}

func (rp *RegistryProvider) AvailableVersions(pkg string) ([]Version, error) {
	var allVersions []Version
	seen := make(map[string]bool)

	// Collect versions from all registries (not just the first one that has it)
	for _, reg := range rp.multi.Registries() {
		versionStrs, err := reg.ListVersions(pkg)
		if err != nil {
			continue
		}
		for _, vs := range versionStrs {
			if seen[vs] {
				continue
			}
			v, err := ParseVersion(vs)
			if err != nil {
				continue
			}
			seen[vs] = true
			allVersions = append(allVersions, v)
		}
	}

	if len(allVersions) == 0 {
		return nil, nil
	}

	SortVersions(allVersions)
	return allVersions, nil
}

func (rp *RegistryProvider) Dependencies(pkg string, version Version) (map[string]VersionRange, error) {
	// Find the package in registries and read its metadata
	verStr := version.String()

	for _, reg := range rp.multi.Registries() {
		tags, err := reg.ListABITags(pkg, verStr)
		if err != nil || len(tags) == 0 {
			continue
		}

		// Find a compatible tag by checking fingerprints
		for _, tag := range tags {
			remoteFingerprint := rp.fetchFingerprint(reg, pkg, verStr, tag)
			if !profile.AreCompatible(tag, rp.abiTag, remoteFingerprint, rp.localFingerprint) {
				continue
			}

			meta, err := reg.FetchMeta(pkg, verStr, tag)
			if err != nil {
				continue
			}

			// Parse dependencies from metadata
			result := make(map[string]VersionRange)
			for _, dep := range meta.Dependencies {
				vr, err := ParseRange(dep.Version)
				if err != nil {
					return nil, fmt.Errorf("package %s@%s has invalid dependency version for %s: %w",
						pkg, verStr, dep.Name, err)
				}
				result[dep.Name] = vr
			}
			return result, nil
		}
	}

	// No metadata found — assume no dependencies
	return make(map[string]VersionRange), nil
}

func (rp *RegistryProvider) HasABI(pkg string, version Version, abiTag string) (bool, error) {
	verStr := version.String()
	for _, reg := range rp.multi.Registries() {
		tags, err := reg.ListABITags(pkg, verStr)
		if err != nil {
			continue
		}
		for _, tag := range tags {
			remoteFingerprint := rp.fetchFingerprint(reg, pkg, verStr, tag)
			if profile.AreCompatible(tag, abiTag, remoteFingerprint, rp.localFingerprint) {
				return true, nil
			}
		}
		// If no prebuilt matches, check if a source package exists that we can build.
		for _, tag := range tags {
			if tag == "source" || tag == "any" {
				meta, err := reg.FetchMeta(pkg, verStr, tag)
				if err != nil {
					continue
				}
				if meta.Package.Kind == "source" || meta.Package.Kind == "header-only" {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// ResolveABITag determines the actual ABI tag to use for a package.
// Returns the tag and whether the package needs to be built from source.
func (rp *RegistryProvider) ResolveABITag(pkg string, version Version) (abiTag string, needsBuild bool, sourceABI string, err error) {
	verStr := version.String()
	for _, reg := range rp.multi.Registries() {
		tags, tagErr := reg.ListABITags(pkg, verStr)
		if tagErr != nil {
			continue
		}

		// First pass: look for a prebuilt that matches our ABI via fingerprint
		bestScore := 0
		for _, tag := range tags {
			remoteFingerprint := rp.fetchFingerprint(reg, pkg, verStr, tag)
			score := profile.RankCompatibility(tag, rp.abiTag, remoteFingerprint, rp.localFingerprint)
			if score > bestScore {
				bestScore = score
				abiTag = tag
			}
		}
		if bestScore > 0 {
			return abiTag, false, "", nil
		}

		// Second pass: look for a source package we can build
		for _, tag := range tags {
			if tag == "source" || tag == "any" {
				meta, metaErr := reg.FetchMeta(pkg, verStr, tag)
				if metaErr != nil {
					continue
				}
				if meta.Package.Kind == "source" {
					return rp.abiTag, true, tag, nil
				}
				if meta.Package.Kind == "header-only" {
					return "any", false, "", nil
				}
			}
		}
	}
	return "", false, "", fmt.Errorf("no compatible ABI or source package for %s@%s (need %s, fingerprint %s)", pkg, version, rp.abiTag, rp.localFingerprint)
}

// fetchFingerprint gets the ABI fingerprint from a package's metadata.
// Returns empty string if the metadata doesn't have a fingerprint (old format).
func (rp *RegistryProvider) fetchFingerprint(reg registry.Registry, pkg, version, tag string) string {
	meta, err := reg.FetchMeta(pkg, version, tag)
	if err != nil {
		return ""
	}
	return meta.ABI.Fingerprint
}

func (rp *RegistryProvider) AvailableABITags(pkg string, version Version) ([]string, error) {
	verStr := version.String()
	var allTags []string
	seen := make(map[string]bool)

	for _, reg := range rp.multi.Registries() {
		tags, err := reg.ListABITags(pkg, verStr)
		if err != nil {
			continue
		}
		for _, tag := range tags {
			if !seen[tag] {
				seen[tag] = true
				allTags = append(allTags, tag)
			}
		}
	}
	return allTags, nil
}

// ResolveOptions holds optional parameters for ResolveFromManifest.
type ResolveOptions struct {
	EnabledFeatures []string
	LockedVersions  map[string]Version
}

// ResolveFromManifest resolves all dependencies from a manifest.
func ResolveFromManifest(m *manifest.Manifest, multi *registry.Multi, prof *profile.Profile, includeBuildDeps bool, lockedVersions ...map[string]Version) ([]ResolvedPackage, error) {
	return ResolveFromManifestWithFeatures(m, multi, prof, includeBuildDeps, nil, lockedVersions...)
}

// ResolveFromManifestWithFeatures is like ResolveFromManifest but also accepts
// a list of enabled features.
func ResolveFromManifestWithFeatures(m *manifest.Manifest, multi *registry.Multi, prof *profile.Profile, includeBuildDeps bool, enabledFeatures []string, lockedVersions ...map[string]Version) ([]ResolvedPackage, error) {
	if m == nil {
		return nil, fmt.Errorf("manifest is nil")
	}
	if multi == nil {
		return nil, fmt.Errorf("registry is nil")
	}
	if prof == nil {
		return nil, fmt.Errorf("profile is nil")
	}

	abiTag := prof.ABITag()

	// For header-only packages, use "any" as the ABI tag
	if m.EffectiveKind() == "header-only" {
		abiTag = "any"
	}

	requirements := make(map[string]VersionRange)
	deps := m.AllDependenciesWithFeatures(enabledFeatures)
	for name, dep := range deps {
		vr, err := ParseRange(dep.Version)
		if err != nil {
			return nil, fmt.Errorf("invalid version range for dependency %q: %w", name, err)
		}
		requirements[name] = vr
	}

	if includeBuildDeps {
		for name, dep := range m.BuildDeps {
			vr, err := ParseRange(dep.Version)
			if err != nil {
				return nil, fmt.Errorf("invalid version range for build-dependency %q: %w", name, err)
			}
			requirements[name] = vr
		}
	}

	if len(requirements) == 0 {
		return nil, nil
	}

	provider := NewCachingProvider(NewRegistryProvider(multi, abiTag, prof.ABIFingerprintHash))
	solver := New(provider, abiTag)

	// Apply lockfile preferences if provided
	if len(lockedVersions) > 0 && lockedVersions[0] != nil {
		solver.SetPreferences(lockedVersions[0])
	}

	return solver.Resolve(requirements)
}
