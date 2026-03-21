package profile

import (
	"testing"
)

func TestParseABITag(t *testing.T) {
	tests := []struct {
		tag      string
		wantOS   string
		wantArch string
		wantStd  string
	}{
		// New 3-segment format
		{
			tag:      "linux-x86_64-libstdcxx",
			wantOS:   "linux",
			wantArch: "x86_64",
			wantStd:  "libstdcxx",
		},
		{
			tag:      "darwin-aarch64-libcxx",
			wantOS:   "darwin",
			wantArch: "aarch64",
			wantStd:  "libcxx",
		},
		{
			tag:      "windows-x86_64-msvc",
			wantOS:   "windows",
			wantArch: "x86_64",
			wantStd:  "msvc",
		},
		// Old 4-segment format (compiler ignored)
		{
			tag:      "linux-x86_64-gcc13-libstdcxx",
			wantOS:   "linux",
			wantArch: "x86_64",
			wantStd:  "libstdcxx",
		},
		{
			tag:      "darwin-aarch64-gcc17-libcxx",
			wantOS:   "darwin",
			wantArch: "aarch64",
			wantStd:  "libcxx",
		},
		// Special tags
		{
			tag:      "any",
			wantOS:   "any",
			wantArch: "",
			wantStd:  "",
		},
		{
			tag:      "linux-aarch64",
			wantOS:   "linux",
			wantArch: "aarch64",
			wantStd:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			p := ParseABITag(tt.tag)
			if p.Raw != tt.tag {
				t.Errorf("Raw = %q, want %q", p.Raw, tt.tag)
			}
			if p.OS != tt.wantOS {
				t.Errorf("OS = %q, want %q", p.OS, tt.wantOS)
			}
			if p.Arch != tt.wantArch {
				t.Errorf("Arch = %q, want %q", p.Arch, tt.wantArch)
			}
			if p.Stdlib != tt.wantStd {
				t.Errorf("Stdlib = %q, want %q", p.Stdlib, tt.wantStd)
			}
		})
	}
}

func TestRankCompatibility(t *testing.T) {
	fp := "abc123"

	tests := []struct {
		name          string
		available     string
		wanted        string
		availableFP   string
		wantedFP      string
		wantRank      int
	}{
		// Exact tag match
		{"exact_match", "linux-x86_64-libstdcxx", "linux-x86_64-libstdcxx", fp, fp, 100},
		// "any" tag (header-only)
		{"any_available", "any", "linux-x86_64-libstdcxx", "", fp, 90},
		{"any_wanted", "linux-x86_64-libstdcxx", "any", fp, "", 90},
		{"both_any", "any", "any", "", "", 100},
		// Same fingerprint, new format tags
		{"fingerprint_match_new", "linux-x86_64-libstdcxx", "linux-x86_64-libstdcxx", fp, fp, 100},
		// Same fingerprint, old format tags (compiler ignored)
		{"old_vs_old_same_fp", "darwin-aarch64-gcc16-libcxx", "darwin-aarch64-gcc17-libcxx", fp, fp, 95},
		// Old format vs new format, same fingerprint
		{"old_vs_new_same_fp", "darwin-aarch64-gcc17-libcxx", "darwin-aarch64-libcxx", fp, fp, 95},
		// Different OS = never compatible
		{"different_os", "darwin-x86_64-libstdcxx", "linux-x86_64-libstdcxx", fp, fp, 0},
		// Different arch = never compatible
		{"different_arch", "linux-aarch64-libstdcxx", "linux-x86_64-libstdcxx", fp, fp, 0},
		// Different stdlib = never compatible
		{"different_stdlib", "linux-x86_64-libcxx", "linux-x86_64-libstdcxx", fp, fp, 0},
		// Same tag always matches (score 100) regardless of fingerprint
		{"same_tag_diff_fp", "linux-x86_64-libstdcxx", "linux-x86_64-libstdcxx", "abc", "def", 100},
		{"same_tag_no_fp", "linux-x86_64-libstdcxx", "linux-x86_64-libstdcxx", "", "", 100},
		// Different tag, different fingerprint = incompatible
		{"diff_tag_diff_fp", "linux-x86_64-libstdcxx", "linux-x86_64-libstdcxx", "abc", "def", 100}, // same tag!
		// Different tags (old vs new format), no fingerprint = incompatible
		{"old_new_no_fp", "linux-x86_64-gcc13-libstdcxx", "linux-x86_64-libstdcxx", "", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rank := RankCompatibility(tt.available, tt.wanted, tt.availableFP, tt.wantedFP)
			if rank != tt.wantRank {
				t.Errorf("RankCompatibility(%q, %q, %q, %q) = %d, want %d",
					tt.available, tt.wanted, tt.availableFP, tt.wantedFP, rank, tt.wantRank)
			}
		})
	}
}

func TestAreCompatible(t *testing.T) {
	fp := "samefp"

	tests := []struct {
		name             string
		tag1, tag2       string
		fp1, fp2         string
		want             bool
	}{
		{"same_tag", "linux-x86_64-libstdcxx", "linux-x86_64-libstdcxx", fp, fp, true},
		{"same_fp_old_tags", "darwin-aarch64-gcc16-libcxx", "darwin-aarch64-gcc17-libcxx", fp, fp, true},
		{"any_tag", "any", "linux-x86_64-libstdcxx", "", fp, true},
		{"same_tag_diff_fp", "linux-x86_64-libstdcxx", "linux-x86_64-libstdcxx", "a", "b", true}, // same tag = always compatible
		{"cross_tag_no_fp", "linux-x86_64-gcc13-libstdcxx", "linux-x86_64-libstdcxx", "", "", false},
		{"different_stdlib", "linux-x86_64-libcxx", "linux-x86_64-libstdcxx", fp, fp, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AreCompatible(tt.tag1, tt.tag2, tt.fp1, tt.fp2)
			if got != tt.want {
				t.Errorf("AreCompatible(%q, %q, %q, %q) = %v, want %v",
					tt.tag1, tt.tag2, tt.fp1, tt.fp2, got, tt.want)
			}
		})
	}
}
