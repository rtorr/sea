package cli

import (
	"fmt"

	"github.com/rtorr/sea/internal/cache"
	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/profile"
	"github.com/rtorr/sea/internal/registry"
	"github.com/spf13/cobra"
)

// downloadOrBuild downloads a prebuilt package or builds from source.
// Returns (sha256, registryName, effectiveABITag, error).
// The sha256 is the content hash used as the cache key.
func downloadOrBuild(cmd *cobra.Command, multi *registry.Multi, c *cache.Cache, cfg *config.Config, prof *profile.Profile, name, version, abiTag, projectDir string) (string, string, string, error) {
	// Try 1: prebuilt for our ABI tag
	reg, matchedTag, err := multi.FindRegistry(name, version, abiTag)
	if err == nil {
		rc, dlErr := reg.Download(name, version, matchedTag)
		if dlErr == nil {
			sha, storeErr := c.Store(rc)
			rc.Close()
			if storeErr == nil {
				return sha, reg.Name(), matchedTag, nil
			}
		}
	}

	// Try 2: source package
	for _, sourceTag := range []string{"source", "any"} {
		srcReg, _, srcErr := multi.FindRegistry(name, version, sourceTag)
		if srcErr != nil {
			continue
		}

		meta, metaErr := srcReg.FetchMeta(name, version, sourceTag)
		if metaErr != nil || meta.Package.Kind != "source" {
			continue
		}

		cmd.Printf("  %s@%s — no prebuilt for %s, building from source...\n", name, version, abiTag)

		rc, dlErr := srcReg.Download(name, version, sourceTag)
		if dlErr != nil {
			return "", "", "", fmt.Errorf("downloading source package: %w", dlErr)
		}
		_, storeErr := c.Store(rc)
		rc.Close()
		if storeErr != nil {
			return "", "", "", fmt.Errorf("caching source package: %w", storeErr)
		}

		// TODO: build from source and cache the result
		// For now, source builds are handled by `sea build`, not `sea install`
		return "", "", "", fmt.Errorf("source builds during install not yet implemented for %s@%s", name, version)
	}

	return "", "", "", fmt.Errorf("no prebuilt or source package found for %s@%s (need ABI %s)", name, version, abiTag)
}
