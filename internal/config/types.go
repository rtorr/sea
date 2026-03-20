package config

// Config represents the global sea configuration (~/.sea/config.toml).
//
// Registry resolution:
//  1. If a dependency in sea.toml has `registry = "name"`, use that specific remote.
//  2. Otherwise, use the default registry.
//  3. If no default is set, the first remote in the list is the default.
//  4. Per-package overrides in [registry.packages] route specific packages to specific registries.
type Config struct {
	Remotes        []Remote          `toml:"remotes"`
	DefaultProfile string            `toml:"default_profile,omitempty"`
	CacheDir       string            `toml:"cache_dir,omitempty"`
	Registry       RegistryConfig    `toml:"registry,omitempty"`
}

// RegistryConfig controls how packages are resolved across registries.
type RegistryConfig struct {
	// Default is the name of the default remote. If empty, the first remote is used.
	Default string `toml:"default,omitempty"`

	// Packages maps package names to specific remote names.
	// e.g. "my-internal-lib" = "corp-registry"
	Packages map[string]string `toml:"packages,omitempty"`

	// Platforms maps "package:abi-pattern" to specific remote names.
	// Patterns support * as a wildcard.
	// e.g. "mylib:android-*" = "corp"
	// e.g. "internal-*" = "corp"    (matches any package starting with "internal-")
	Platforms map[string]string `toml:"platforms,omitempty"`
}

// Remote represents a package registry endpoint.
type Remote struct {
	Name       string `toml:"name"`
	Type       string `toml:"type"`       // "artifactory" | "github" | "github-releases" | "filesystem" | "local"
	URL        string `toml:"url,omitempty"`
	Repository string `toml:"repository,omitempty"`
	TokenEnv   string `toml:"token_env,omitempty"`
	Path       string `toml:"path,omitempty"` // for filesystem type
}

// DefaultRemoteName returns the name of the default registry.
func (c *Config) DefaultRemoteName() string {
	if c.Registry.Default != "" {
		return c.Registry.Default
	}
	if len(c.Remotes) > 0 {
		return c.Remotes[0].Name
	}
	return ""
}

// RemoteForPackage returns which remote name should be used for a specific package
// on a given platform (ABI tag). Resolution order:
//  1. Platform-specific: "pkg:abi-pattern" match in [registry.platforms]
//  2. Package-level: "pkg" match in [registry.packages]
//  3. Default registry
func (c *Config) RemoteForPackage(pkg string, abiTag ...string) string {
	// 1. Platform-specific routing
	if len(abiTag) > 0 && abiTag[0] != "" && c.Registry.Platforms != nil {
		abi := abiTag[0]
		// Check "pkg:abi-pattern"
		for pattern, remote := range c.Registry.Platforms {
			if matchRoute(pattern, pkg, abi) {
				return remote
			}
		}
	}

	// 2. Package-level routing
	if c.Registry.Packages != nil {
		if name, ok := c.Registry.Packages[pkg]; ok {
			return name
		}
		// Also check wildcard patterns in packages
		for pattern, remote := range c.Registry.Packages {
			if matchWildcard(pattern, pkg) {
				return remote
			}
		}
	}

	return c.DefaultRemoteName()
}

// matchRoute checks if a "pkg:abi" or "pkg-pattern" matches.
func matchRoute(pattern, pkg, abi string) bool {
	if i := indexOf(pattern, ':'); i >= 0 {
		// "pkg:abi-pattern" format
		pkgPat := pattern[:i]
		abiPat := pattern[i+1:]
		return matchWildcard(pkgPat, pkg) && matchWildcard(abiPat, abi)
	}
	// Just a package pattern (no colon)
	return matchWildcard(pattern, pkg)
}

// matchWildcard does simple glob matching with * as wildcard.
func matchWildcard(pattern, s string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == s {
		return true
	}
	// Handle trailing *: "foo-*" matches "foo-bar"
	if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(s) >= len(prefix) && s[:len(prefix)] == prefix
	}
	// Handle leading *: "*-bar" matches "foo-bar"
	if len(pattern) > 0 && pattern[0] == '*' {
		suffix := pattern[1:]
		return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
	}
	return false
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
