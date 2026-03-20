package registry

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/rtorr/sea/internal/archive"
	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/profile"
)

// Registry is the interface all remote backends implement.
type Registry interface {
	// Name returns the registry name.
	Name() string

	// ListVersions returns available versions for a package.
	ListVersions(pkg string) ([]string, error)

	// ListABITags returns available ABI tags for a package version.
	ListABITags(pkg, version string) ([]string, error)

	// FetchMeta downloads the sea-package.toml for a specific package.
	FetchMeta(pkg, version, abiTag string) (*archive.PackageMeta, error)

	// Download streams the .tar.zst archive for a package.
	Download(pkg, version, abiTag string) (io.ReadCloser, error)

	// Upload publishes a package archive.
	Upload(pkg, version, abiTag string, data io.Reader, meta *archive.PackageMeta) error

	// Search finds packages matching a query string.
	Search(query string) ([]SearchResult, error)

	// FetchVersionManifest retrieves the sea-version.toml for a package version.
	// Returns nil, nil if no manifest exists (backward compat).
	FetchVersionManifest(pkg, version string) (*archive.VersionManifest, error)

	// UploadVersionManifest writes or merges the sea-version.toml for a package version.
	UploadVersionManifest(pkg, version string, vm *archive.VersionManifest) error
}

// SearchResult represents a search hit.
type SearchResult struct {
	Name     string
	Versions []string
	Registry string
}

// Multi aggregates multiple registries, searching them in priority order.
type Multi struct {
	registries  []Registry
	compatRules []profile.CompatRule
}

// NewMulti creates a multi-registry orchestrator from config.
func NewMulti(cfg *config.Config) (*Multi, error) {
	if cfg == nil {
		return &Multi{}, nil
	}
	m := &Multi{}
	for _, remote := range cfg.Remotes {
		r, err := FromConfig(&remote)
		if err != nil {
			return nil, fmt.Errorf("initializing remote %q: %w", remote.Name, err)
		}
		m.registries = append(m.registries, r)
	}
	return m, nil
}

// SetCompatRules configures ABI compatibility rules for tag matching.
func (m *Multi) SetCompatRules(rules []profile.CompatRule) {
	m.compatRules = rules
}

// FromConfig creates a Registry from a Remote config entry.
func FromConfig(remote *config.Remote) (Registry, error) {
	if remote == nil {
		return nil, fmt.Errorf("remote config is nil")
	}
	switch remote.Type {
	case "filesystem":
		if remote.Path == "" {
			return nil, fmt.Errorf("filesystem remote %q requires a path", remote.Name)
		}
		return NewFilesystem(remote.Name, remote.Path)
	case "artifactory":
		if remote.URL == "" {
			return nil, fmt.Errorf("artifactory remote %q requires a URL", remote.Name)
		}
		if remote.Repository == "" {
			return nil, fmt.Errorf("artifactory remote %q requires a repository name", remote.Name)
		}
		return NewArtifactory(remote.Name, remote.URL, remote.Repository, remote.TokenEnv)
	case "github":
		if remote.URL == "" {
			return nil, fmt.Errorf("github remote %q requires a URL/owner", remote.Name)
		}
		return NewGitHub(remote.Name, remote.URL, remote.TokenEnv)
	case "github-releases":
		if remote.URL == "" {
			return nil, fmt.Errorf("github-releases remote %q requires a URL (owner/repo)", remote.Name)
		}
		return NewGitHubReleases(remote.Name, remote.URL, remote.TokenEnv)
	case "local":
		if remote.Path == "" {
			return nil, fmt.Errorf("local remote %q requires a path", remote.Name)
		}
		return NewLocal(remote.Name, remote.Path)
	default:
		return nil, fmt.Errorf("unknown registry type %q for remote %q (supported: filesystem, artifactory, github, github-releases, local)", remote.Type, remote.Name)
	}
}

// ListVersions returns versions merged from all registries that have the package.
func (m *Multi) ListVersions(pkg string) ([]string, error) {
	var all []string
	seen := make(map[string]bool)

	for _, r := range m.registries {
		versions, err := r.ListVersions(pkg)
		if err != nil {
			continue
		}
		for _, v := range versions {
			if !seen[v] {
				seen[v] = true
				all = append(all, v)
			}
		}
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("package %q not found in any registry", pkg)
	}
	return all, nil
}

// FindRegistry returns the first registry that has the given package+version+ABI,
// using compatibility rules for ABI matching. Also returns the actual ABI tag
// that matched (which may differ from the requested tag, e.g. "any" for header-only).
func (m *Multi) FindRegistry(pkg, version, abiTag string) (reg Registry, matchedTag string, err error) {
	bestScore := 0
	for _, r := range m.registries {
		tags, tagErr := r.ListABITags(pkg, version)
		if tagErr != nil {
			continue
		}
		for _, t := range tags {
			score := profile.RankCompatibility(t, abiTag, m.compatRules)
			if score > bestScore {
				bestScore = score
				reg = r
				matchedTag = t
			}
		}
	}
	if reg == nil {
		return nil, "", fmt.Errorf("package %s@%s not found with compatible ABI %q in any registry", pkg, version, abiTag)
	}
	return reg, matchedTag, nil
}

// FetchPreviousSymbols finds the highest version of a package that is lower than
// the given version, fetches its metadata, and returns the exported symbol list.
// Returns nil, nil if no previous version exists (first publish).
func (m *Multi) FetchPreviousSymbols(pkg, currentVersion, abiTag string) ([]string, string, error) {
	// Gather all versions across registries
	allVersions, err := m.ListVersions(pkg)
	if err != nil {
		return nil, "", nil // package doesn't exist yet — first publish
	}

	// Find the highest version < currentVersion
	var best string
	for _, v := range allVersions {
		if v == currentVersion {
			continue
		}
		if versionLess(v, currentVersion) {
			if best == "" || versionLess(best, v) {
				best = v
			}
		}
	}

	if best == "" {
		return nil, "", nil // first version
	}

	// Fetch metadata from whichever registry has it
	for _, r := range m.registries {
		tags, err := r.ListABITags(pkg, best)
		if err != nil || len(tags) == 0 {
			continue
		}
		// Prefer exact ABI match, fall back to any
		for _, tag := range tags {
			if profile.AreCompatible(tag, abiTag, m.compatRules) {
				meta, err := r.FetchMeta(pkg, best, tag)
				if err != nil {
					continue
				}
				return meta.Symbols.Exported, best, nil
			}
		}
	}

	return nil, best, nil // version exists but we couldn't fetch symbols
}

// versionLess returns true if a < b using simple string-based semver comparison.
func versionLess(a, b string) bool {
	aParts := splitVersion(a)
	bParts := splitVersion(b)
	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return true
		}
		if aParts[i] > bParts[i] {
			return false
		}
	}
	return false
}

func splitVersion(v string) [3]int {
	var parts [3]int
	fmt.Sscanf(v, "%d.%d.%d", &parts[0], &parts[1], &parts[2])
	return parts
}

// FindRegistryByName returns a specific registry by name.
func (m *Multi) FindRegistryByName(name string) (Registry, error) {
	for _, r := range m.registries {
		if r.Name() == name {
			return r, nil
		}
	}
	return nil, fmt.Errorf("registry %q not found", name)
}

// Registries returns the list of registries.
func (m *Multi) Registries() []Registry {
	return m.registries
}

// Search searches all registries and returns deduplicated, sorted results.
func (m *Multi) Search(query string) ([]SearchResult, error) {
	seen := make(map[string]*SearchResult)
	var order []string

	for _, r := range m.registries {
		results, err := r.Search(query)
		if err != nil {
			continue
		}
		for _, res := range results {
			key := strings.ToLower(res.Name)
			if existing, ok := seen[key]; ok {
				// Merge versions from multiple registries
				vSet := make(map[string]bool)
				for _, v := range existing.Versions {
					vSet[v] = true
				}
				for _, v := range res.Versions {
					if !vSet[v] {
						existing.Versions = append(existing.Versions, v)
					}
				}
			} else {
				resCopy := res
				seen[key] = &resCopy
				order = append(order, key)
			}
		}
	}

	sort.Strings(order)
	results := make([]SearchResult, 0, len(order))
	for _, key := range order {
		results = append(results, *seen[key])
	}
	return results, nil
}
