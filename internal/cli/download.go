package cli

import (
	crypto_sha256 "crypto/sha256"
	crypto_hex "encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rtorr/sea/internal/archive"
	"github.com/rtorr/sea/internal/builder"
	"github.com/rtorr/sea/internal/cache"
	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/profile"
	"github.com/rtorr/sea/internal/registry"
	"github.com/spf13/cobra"
)

// downloadOrBuild tries to download a prebuilt package for the host ABI tag.
// If no prebuilt exists, it looks for a source package, downloads it, builds
// it locally, and caches the result.
// Returns (sha256, registryName, effectiveABITag, error).
// effectiveABITag may differ from the input abiTag (e.g. "any" for header-only).
func downloadOrBuild(cmd *cobra.Command, multi *registry.Multi, c *cache.Cache, cfg *config.Config, prof *profile.Profile, name, version, abiTag, projectDir string) (string, string, string, error) {
	// Try 1: prebuilt for our ABI tag (also matches "any" for header-only)
	reg, matchedTag, err := multi.FindRegistry(name, version, abiTag)
	if err == nil {
		rc, dlErr := reg.Download(name, version, matchedTag)
		if dlErr == nil {
			// Store under the matched tag (e.g. "any" for header-only, not the host tag)
			sha, storeErr := c.Store(name, version, matchedTag, rc)
			rc.Close()
			if storeErr == nil {
				return sha, reg.Name(), matchedTag, nil
			}
		}
	}

	// Try 2: source package (stored under "source" or "any" ABI tag)
	for _, sourceTag := range []string{"source", "any"} {
		srcReg, _, srcErr := multi.FindRegistry(name, version, sourceTag)
		if srcErr != nil {
			continue
		}

		// Verify it's actually a source package
		meta, metaErr := srcReg.FetchMeta(name, version, sourceTag)
		if metaErr != nil || meta.Package.Kind != "source" {
			continue
		}

		cmd.Printf("  %s@%s — no prebuilt for %s, building from source...\n", name, version, abiTag)

		// Download source archive
		rc, dlErr := srcReg.Download(name, version, sourceTag)
		if dlErr != nil {
			return "", "", "", fmt.Errorf("downloading source package: %w", dlErr)
		}
		srcSha, storeErr := c.Store(name, version, sourceTag, rc)
		rc.Close()
		if storeErr != nil {
			return "", "", "", fmt.Errorf("caching source package: %w", storeErr)
		}
		_ = srcSha

		// Extract source to a temp build directory
		srcDir, extractErr := c.Extract(name, version, sourceTag)
		if extractErr != nil {
			return "", "", "", fmt.Errorf("extracting source package: %w", extractErr)
		}

		// Build it
		sha, buildErr := buildSourcePackage(cmd, c, cfg, prof, name, version, abiTag, srcDir)
		if buildErr != nil {
			return "", "", "", fmt.Errorf("building from source: %w", buildErr)
		}

		return sha, srcReg.Name() + " (built from source)", abiTag, nil
	}

	return "", "", "", fmt.Errorf("no prebuilt or source package found for %s@%s (need ABI %s)", name, version, abiTag)
}

// buildSourcePackage builds a source package and caches the result under the given ABI tag.
func buildSourcePackage(cmd *cobra.Command, c *cache.Cache, cfg *config.Config, prof *profile.Profile, name, version, abiTag, srcDir string) (string, error) {
	// Look for build.sh or sea.toml with a build script in the source package
	buildScript := ""
	for _, candidate := range []string{"build.sh", "build.cmd", "build.bat"} {
		if _, err := os.Stat(filepath.Join(srcDir, candidate)); err == nil {
			buildScript = candidate
			break
		}
	}

	// Load the source package's manifest for build config and verification rules
	var srcManifest *manifest.Manifest
	loadedManifest, loadErr := manifest.LoadFile(filepath.Join(srcDir, manifest.FileName))
	if loadErr == nil {
		srcManifest = loadedManifest
		if buildScript == "" && srcManifest.Build.Script != "" {
			buildScript = srcManifest.Build.Script
		}
	}

	if buildScript == "" {
		return "", fmt.Errorf("source package has no build script (need build.sh or [build].script in sea.toml)")
	}

	if srcManifest == nil {
		srcManifest = &manifest.Manifest{
			Package: manifest.Package{Name: name, Version: version},
			Build:   manifest.Build{Script: buildScript},
		}
	}

	// Create a build output directory
	buildDir := filepath.Join(srcDir, "sea_build_output")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return "", fmt.Errorf("creating build directory: %w", err)
	}

	// Build using the source package's script
	env := builder.BuildEnv(srcManifest, prof, srcDir, buildDir)
	if err := builder.RunScript(buildScript, env, srcDir); err != nil {
		return "", fmt.Errorf("build script failed: %w", err)
	}

	// Run build verification if configured in the source package's manifest
	if err := builder.VerifyBuildOutput(srcManifest, prof, srcDir, buildDir); err != nil {
		return "", fmt.Errorf("build verification: %w", err)
	}

	// Pack the build output into an archive and cache it under our ABI tag
	archivePath := filepath.Join(srcDir, fmt.Sprintf("%s-%s-%s.tar.zst", name, version, abiTag))
	includes := []string{"include/**", "lib/**", "bin/**", "share/**", "LICENSE", "COPYING"}
	if err := archive.Pack(buildDir, includes, archivePath); err != nil {
		return "", fmt.Errorf("packing build output: %w", err)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("opening built archive: %w", err)
	}
	defer f.Close()

	sha, err := c.Store(name, version, abiTag, f)
	if err != nil {
		return "", fmt.Errorf("caching built package: %w", err)
	}

	cmd.Printf("  %s@%s — built successfully for %s\n", name, version, abiTag)
	return sha, nil
}

// computeCachedHash reads a cached archive and computes its SHA256 hash.
func computeCachedHash(c *cache.Cache, name, version, abiTag string) (bool, string) {
	archivePath := c.Layout.ArchivePath(name, version, abiTag)
	f, err := os.Open(archivePath)
	if err != nil {
		return false, ""
	}
	defer f.Close()

	h := crypto_sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, ""
	}
	return true, crypto_hex.EncodeToString(h.Sum(nil))
}
