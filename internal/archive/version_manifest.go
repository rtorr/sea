package archive

import "time"

// VersionManifestFile is the per-version manifest that tracks all published
// variants (channel × platform) for a package version.
const VersionManifestFile = "sea-version.toml"

// VersionManifest is the top-level structure stored at {pkg}/{version}/sea-version.toml.
// It is the single source of truth for what has been published for this version.
type VersionManifest struct {
	Package   VersionManifestPackage `toml:"package"`
	Artifacts []ArtifactEntry        `toml:"artifacts,omitempty"`
}

// VersionManifestPackage identifies the package version.
type VersionManifestPackage struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
	Kind    string `toml:"kind"`
}

// ArtifactEntry represents one published cell in the version × channel × platform matrix.
type ArtifactEntry struct {
	Channel   string `toml:"channel"`             // "stable", "beta", "rc", "dev"
	ABITag    string `toml:"abi_tag"`              // platform identifier
	Status    string `toml:"status"`               // "published" | "expected" | "failed"
	SHA256    string `toml:"sha256,omitempty"`      // hash of the .tar.zst
	Timestamp string `toml:"timestamp,omitempty"`   // RFC3339
	Publisher string `toml:"publisher,omitempty"`   // who published (CI identifier, human)
	CI        string `toml:"ci,omitempty"`          // "github-actions", "jenkins", etc.
	RunID     string `toml:"run_id,omitempty"`      // CI run ID
}

// FindArtifact returns the entry matching channel + ABI tag, or nil.
func (vm *VersionManifest) FindArtifact(channel, abiTag string) *ArtifactEntry {
	for i := range vm.Artifacts {
		if vm.Artifacts[i].Channel == channel && vm.Artifacts[i].ABITag == abiTag {
			return &vm.Artifacts[i]
		}
	}
	return nil
}

// PublishedABITags returns all ABI tags with status "published" for a given channel.
func (vm *VersionManifest) PublishedABITags(channel string) []string {
	var tags []string
	for _, a := range vm.Artifacts {
		if a.Status == "published" && (channel == "" || a.Channel == channel) {
			tags = append(tags, a.ABITag)
		}
	}
	return tags
}

// AllPublishedABITags returns all ABI tags with status "published" across all channels.
func (vm *VersionManifest) AllPublishedABITags() []string {
	return vm.PublishedABITags("")
}

// Merge combines another manifest into this one. For each artifact in other,
// if this manifest has the same channel+abi_tag entry, the newer one wins
// (by timestamp). Otherwise the entry is added. This enables concurrent
// publishing from different CI runners.
func (vm *VersionManifest) Merge(other *VersionManifest) {
	if other == nil {
		return
	}
	for _, entry := range other.Artifacts {
		existing := vm.FindArtifact(entry.Channel, entry.ABITag)
		if existing == nil {
			vm.Artifacts = append(vm.Artifacts, entry)
		} else if entry.Timestamp > existing.Timestamp {
			*existing = entry
		}
	}
}

// NewArtifactEntry creates a published artifact entry with the current timestamp.
func NewArtifactEntry(channel, abiTag, sha256, publisher string) ArtifactEntry {
	return ArtifactEntry{
		Channel:   channel,
		ABITag:    abiTag,
		Status:    "published",
		SHA256:    sha256,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Publisher: publisher,
	}
}

// ExpectedArtifactEntry creates an expected (not yet published) entry.
func ExpectedArtifactEntry(channel, abiTag string) ArtifactEntry {
	return ArtifactEntry{
		Channel: channel,
		ABITag:  abiTag,
		Status:  "expected",
	}
}
