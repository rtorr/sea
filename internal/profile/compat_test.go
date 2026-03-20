package profile

import (
	"testing"
)

func TestParseABITag(t *testing.T) {
	tests := []struct {
		tag      string
		wantOS   string
		wantArch string
		wantComp string
		wantStd  string
	}{
		{
			tag:      "linux-x86_64-gcc13-libstdcxx",
			wantOS:   "linux",
			wantArch: "x86_64",
			wantComp: "gcc13",
			wantStd:  "libstdcxx",
		},
		{
			tag:      "darwin-aarch64-clang17-libcxx",
			wantOS:   "darwin",
			wantArch: "aarch64",
			wantComp: "clang17",
			wantStd:  "libcxx",
		},
		{
			tag:      "windows-x86_64-msvcv143-msvc",
			wantOS:   "windows",
			wantArch: "x86_64",
			wantComp: "msvcv143",
			wantStd:  "msvc",
		},
		{
			tag:      "any",
			wantOS:   "any",
			wantArch: "",
			wantComp: "",
			wantStd:  "",
		},
		{
			tag:      "linux-aarch64",
			wantOS:   "linux",
			wantArch: "aarch64",
			wantComp: "",
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
			if p.Compiler != tt.wantComp {
				t.Errorf("Compiler = %q, want %q", p.Compiler, tt.wantComp)
			}
			if p.Stdlib != tt.wantStd {
				t.Errorf("Stdlib = %q, want %q", p.Stdlib, tt.wantStd)
			}
		})
	}
}

func TestRankCompatibility(t *testing.T) {
	rules := []CompatRule{
		{
			Name: "gcc12-gcc13-compat",
			Tags: []string{
				"linux-x86_64-gcc12-libstdcxx",
				"linux-x86_64-gcc13-libstdcxx",
			},
		},
	}

	tests := []struct {
		available string
		wanted    string
		wantRank  int
	}{
		// Exact match
		{"linux-x86_64-gcc13-libstdcxx", "linux-x86_64-gcc13-libstdcxx", 100},
		// "any" tag
		{"any", "linux-x86_64-gcc13-libstdcxx", 90},
		{"linux-x86_64-gcc13-libstdcxx", "any", 90},
		{"any", "any", 100}, // both any = exact match
		// Compatible via rule
		{"linux-x86_64-gcc12-libstdcxx", "linux-x86_64-gcc13-libstdcxx", 80},
		{"linux-x86_64-gcc13-libstdcxx", "linux-x86_64-gcc12-libstdcxx", 80},
		// Incompatible: different OS
		{"darwin-x86_64-gcc13-libstdcxx", "linux-x86_64-gcc13-libstdcxx", 0},
		// Incompatible: different arch
		{"linux-aarch64-gcc13-libstdcxx", "linux-x86_64-gcc13-libstdcxx", 0},
		// Incompatible: no rule covers gcc11
		{"linux-x86_64-gcc11-libstdcxx", "linux-x86_64-gcc13-libstdcxx", 0},
	}

	for _, tt := range tests {
		t.Run(tt.available+"_vs_"+tt.wanted, func(t *testing.T) {
			rank := RankCompatibility(tt.available, tt.wanted, rules)
			if rank != tt.wantRank {
				t.Errorf("RankCompatibility(%q, %q) = %d, want %d",
					tt.available, tt.wanted, rank, tt.wantRank)
			}
		})
	}
}

func TestRankCompatibilityNoRules(t *testing.T) {
	// With no rules, only exact match and "any" should score
	rank := RankCompatibility("linux-x86_64-gcc13-libstdcxx", "linux-x86_64-gcc13-libstdcxx", nil)
	if rank != 100 {
		t.Errorf("exact match should be 100, got %d", rank)
	}

	rank = RankCompatibility("linux-x86_64-gcc12-libstdcxx", "linux-x86_64-gcc13-libstdcxx", nil)
	if rank != 0 {
		t.Errorf("different compilers without rules should be 0, got %d", rank)
	}

	rank = RankCompatibility("any", "linux-x86_64-gcc13-libstdcxx", nil)
	if rank != 90 {
		t.Errorf("any tag should be 90, got %d", rank)
	}
}
