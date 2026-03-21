package profile

import (
	"strings"
)

// AreCompatible checks if two ABI tags are compatible using fingerprints.
// Tags are compatible if they are equal, if either is "any", or if their
// ABI probe fingerprints match.
func AreCompatible(tag1, tag2, fingerprint1, fingerprint2 string) bool {
	return RankCompatibility(tag1, tag2, fingerprint1, fingerprint2) > 0
}

// ParsedABITag holds the decomposed parts of an ABI tag.
//
// New format (v2): {os}-{arch}-{stdlib}
// Old format (v1): {os}-{arch}-{compiler}{major}-{stdlib}
//
// ParseABITag handles both for backward compatibility with published packages.
type ParsedABITag struct {
	OS     string
	Arch   string
	Stdlib string
	Raw    string
}

// ParseABITag decomposes an ABI tag into OS, arch, and stdlib parts.
// Handles both new 3-segment tags ("linux-x86_64-libstdcxx") and old
// 4-segment tags ("linux-x86_64-gcc13-libstdcxx") by treating the
// last segment as stdlib.
func ParseABITag(tag string) ParsedABITag {
	p := ParsedABITag{Raw: tag}
	parts := strings.Split(tag, "-")

	if len(parts) >= 1 {
		p.OS = parts[0]
	}
	if len(parts) >= 2 {
		p.Arch = parts[1]
	}

	// The stdlib is always the last segment
	if len(parts) == 3 {
		// New format: os-arch-stdlib
		p.Stdlib = parts[2]
	} else if len(parts) == 4 {
		// Old format: os-arch-compiler-stdlib (ignore compiler)
		p.Stdlib = parts[3]
	}

	return p
}

// RankCompatibility returns a score for how compatible two ABI tags are.
// Higher is better. 0 means incompatible.
//
// This is the fingerprint-based compatibility model. ABI tags are just
// human-readable labels — actual compatibility is determined by the ABI
// probe fingerprint which measures real type layouts, mangling schemes,
// and exception ABIs.
//
//	100 = exact tag match (trivially compatible)
//	 95 = different tags but same ABI fingerprint (empirically compatible)
//	 90 = "any" tag (header-only package)
//	  0 = incompatible (different fingerprint or no fingerprint available)
func RankCompatibility(available, wanted string, availableFingerprint, wantedFingerprint string) int {
	if available == wanted {
		return 100
	}
	if available == "any" || wanted == "any" {
		return 90
	}

	a := ParseABITag(available)
	w := ParseABITag(wanted)

	// OS, arch, and stdlib family must match — these are never compatible across
	if a.OS != w.OS || a.Arch != w.Arch {
		return 0
	}
	if a.Stdlib != "" && w.Stdlib != "" && a.Stdlib != w.Stdlib {
		return 0
	}

	// Both fingerprints must be present — no guessing
	if availableFingerprint == "" || wantedFingerprint == "" {
		return 0
	}

	// The real check: do the toolchains produce compatible binaries?
	if availableFingerprint == wantedFingerprint {
		return 95
	}

	return 0
}

