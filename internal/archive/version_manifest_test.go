package archive

import (
	"testing"
)

func TestMergeOverlappingNewerWins(t *testing.T) {
	base := &VersionManifest{
		Package: VersionManifestPackage{Name: "foo", Version: "1.0.0", Kind: "source"},
		Artifacts: []ArtifactEntry{
			{
				Channel:   "stable",
				ABITag:    "linux-x86_64-gcc13-libstdcxx",
				Status:    "published",
				SHA256:    "aaa",
				Timestamp: "2025-01-01T00:00:00Z",
			},
			{
				Channel:   "stable",
				ABITag:    "darwin-aarch64-clang17-libcxx",
				Status:    "published",
				SHA256:    "bbb",
				Timestamp: "2025-01-01T00:00:00Z",
			},
		},
	}

	other := &VersionManifest{
		Package: VersionManifestPackage{Name: "foo", Version: "1.0.0", Kind: "source"},
		Artifacts: []ArtifactEntry{
			{
				Channel:   "stable",
				ABITag:    "linux-x86_64-gcc13-libstdcxx",
				Status:    "published",
				SHA256:    "ccc",
				Timestamp: "2025-06-01T00:00:00Z", // newer
			},
			{
				Channel:   "beta",
				ABITag:    "linux-x86_64-gcc13-libstdcxx",
				Status:    "published",
				SHA256:    "ddd",
				Timestamp: "2025-06-01T00:00:00Z",
			},
		},
	}

	base.Merge(other)

	if len(base.Artifacts) != 3 {
		t.Fatalf("expected 3 artifacts after merge, got %d", len(base.Artifacts))
	}

	// The overlapping entry (stable, linux-x86_64-gcc13-libstdcxx) should have the newer SHA
	entry := base.FindArtifact("stable", "linux-x86_64-gcc13-libstdcxx")
	if entry == nil {
		t.Fatal("expected to find stable linux entry")
	}
	if entry.SHA256 != "ccc" {
		t.Errorf("expected newer SHA256 'ccc', got %q", entry.SHA256)
	}

	// The darwin entry should be unchanged
	entry = base.FindArtifact("stable", "darwin-aarch64-clang17-libcxx")
	if entry == nil {
		t.Fatal("expected to find stable darwin entry")
	}
	if entry.SHA256 != "bbb" {
		t.Errorf("expected SHA256 'bbb', got %q", entry.SHA256)
	}

	// The new beta entry should be added
	entry = base.FindArtifact("beta", "linux-x86_64-gcc13-libstdcxx")
	if entry == nil {
		t.Fatal("expected to find beta linux entry")
	}
	if entry.SHA256 != "ddd" {
		t.Errorf("expected SHA256 'ddd', got %q", entry.SHA256)
	}
}

func TestMergeOlderTimestampDoesNotOverwrite(t *testing.T) {
	base := &VersionManifest{
		Artifacts: []ArtifactEntry{
			{
				Channel:   "stable",
				ABITag:    "linux-x86_64-gcc13-libstdcxx",
				Status:    "published",
				SHA256:    "newer-hash",
				Timestamp: "2025-06-01T00:00:00Z",
			},
		},
	}

	other := &VersionManifest{
		Artifacts: []ArtifactEntry{
			{
				Channel:   "stable",
				ABITag:    "linux-x86_64-gcc13-libstdcxx",
				Status:    "published",
				SHA256:    "older-hash",
				Timestamp: "2024-01-01T00:00:00Z", // older
			},
		},
	}

	base.Merge(other)

	entry := base.FindArtifact("stable", "linux-x86_64-gcc13-libstdcxx")
	if entry.SHA256 != "newer-hash" {
		t.Errorf("older timestamp should not overwrite: got %q", entry.SHA256)
	}
}

func TestFindArtifact(t *testing.T) {
	vm := &VersionManifest{
		Artifacts: []ArtifactEntry{
			{Channel: "stable", ABITag: "linux-x86_64-gcc13-libstdcxx", Status: "published"},
			{Channel: "beta", ABITag: "linux-x86_64-gcc13-libstdcxx", Status: "published"},
			{Channel: "stable", ABITag: "darwin-aarch64-clang17-libcxx", Status: "expected"},
		},
	}

	entry := vm.FindArtifact("stable", "linux-x86_64-gcc13-libstdcxx")
	if entry == nil {
		t.Fatal("expected to find entry")
	}
	if entry.Channel != "stable" {
		t.Errorf("expected channel stable, got %s", entry.Channel)
	}

	entry = vm.FindArtifact("beta", "linux-x86_64-gcc13-libstdcxx")
	if entry == nil {
		t.Fatal("expected to find beta entry")
	}

	entry = vm.FindArtifact("stable", "windows-x86_64-msvcv143-msvc")
	if entry != nil {
		t.Error("expected nil for non-existent entry")
	}

	entry = vm.FindArtifact("rc", "linux-x86_64-gcc13-libstdcxx")
	if entry != nil {
		t.Error("expected nil for non-existent channel")
	}
}

func TestPublishedABITags(t *testing.T) {
	vm := &VersionManifest{
		Artifacts: []ArtifactEntry{
			{Channel: "stable", ABITag: "linux-x86_64-gcc13-libstdcxx", Status: "published"},
			{Channel: "stable", ABITag: "darwin-aarch64-clang17-libcxx", Status: "published"},
			{Channel: "stable", ABITag: "windows-x86_64-msvcv143-msvc", Status: "expected"},
			{Channel: "beta", ABITag: "linux-x86_64-gcc13-libstdcxx", Status: "published"},
		},
	}

	// Filter by "stable" channel
	tags := vm.PublishedABITags("stable")
	if len(tags) != 2 {
		t.Fatalf("expected 2 published stable tags, got %d", len(tags))
	}

	// "expected" status should be excluded
	for _, tag := range tags {
		if tag == "windows-x86_64-msvcv143-msvc" {
			t.Error("expected status should not be included in published tags")
		}
	}

	// All channels (empty string)
	allTags := vm.PublishedABITags("")
	if len(allTags) != 3 {
		t.Fatalf("expected 3 published tags across all channels, got %d", len(allTags))
	}

	// AllPublishedABITags is equivalent
	allTags2 := vm.AllPublishedABITags()
	if len(allTags2) != len(allTags) {
		t.Errorf("AllPublishedABITags returned %d, PublishedABITags('') returned %d", len(allTags2), len(allTags))
	}
}
