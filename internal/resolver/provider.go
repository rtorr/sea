package resolver

// PackageProvider is the interface the resolver uses to fetch package metadata.
type PackageProvider interface {
	// AvailableVersions returns all available versions of a package, sorted descending.
	AvailableVersions(pkg string) ([]Version, error)

	// Dependencies returns the dependencies of a specific package version.
	Dependencies(pkg string, version Version) (map[string]VersionRange, error)

	// HasABI checks if a package version is available for a given ABI tag.
	HasABI(pkg string, version Version, abiTag string) (bool, error)

	// AvailableABITags returns all ABI tags for a specific package version.
	AvailableABITags(pkg string, version Version) ([]string, error)
}

// CachingProvider wraps a provider with a version/dependency cache to avoid
// repeated network calls during resolution.
type CachingProvider struct {
	inner    PackageProvider
	versions map[string][]Version
	deps     map[string]map[string]VersionRange
	abiCache map[string][]string
}

// NewCachingProvider wraps a provider with caching.
func NewCachingProvider(inner PackageProvider) *CachingProvider {
	return &CachingProvider{
		inner:    inner,
		versions: make(map[string][]Version),
		deps:     make(map[string]map[string]VersionRange),
		abiCache: make(map[string][]string),
	}
}

func (c *CachingProvider) AvailableVersions(pkg string) ([]Version, error) {
	if v, ok := c.versions[pkg]; ok {
		return v, nil
	}
	v, err := c.inner.AvailableVersions(pkg)
	if err != nil {
		return nil, err
	}
	c.versions[pkg] = v
	return v, nil
}

func (c *CachingProvider) Dependencies(pkg string, version Version) (map[string]VersionRange, error) {
	key := pkg + "@" + version.String()
	if d, ok := c.deps[key]; ok {
		return d, nil
	}
	d, err := c.inner.Dependencies(pkg, version)
	if err != nil {
		return nil, err
	}
	c.deps[key] = d
	return d, nil
}

func (c *CachingProvider) HasABI(pkg string, version Version, abiTag string) (bool, error) {
	tags, err := c.AvailableABITags(pkg, version)
	if err != nil {
		return false, err
	}
	for _, t := range tags {
		if t == abiTag || t == "any" || abiTag == "any" {
			return true, nil
		}
	}
	return false, nil
}

func (c *CachingProvider) AvailableABITags(pkg string, version Version) ([]string, error) {
	key := pkg + "@" + version.String()
	if tags, ok := c.abiCache[key]; ok {
		return tags, nil
	}
	tags, err := c.inner.AvailableABITags(pkg, version)
	if err != nil {
		return nil, err
	}
	c.abiCache[key] = tags
	return tags, nil
}
