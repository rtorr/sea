package profile

import "fmt"

// ABITag computes the ABI tag string for a profile.
//
// Format: {os}-{arch}-{stdlib}
//
// The tag is a storage key and pre-filter, not a compatibility check.
// Actual binary compatibility is determined by the ABI probe fingerprint.
//
// The compiler name and version are deliberately excluded — they create
// false distinctions between compatible toolchains (e.g., Apple Clang 16
// vs 17 both produce identical ABI with libc++). The stdlib is the only
// dimension beyond OS/arch that correlates with real ABI differences
// (libstdc++ vs libc++ have different type layouts).
func (p *Profile) ABITag() string {
	stdlib := shortStdlib(p.CppStdlib)
	if stdlib == "" {
		// Pure C or no stdlib — tag is just os-arch
		return fmt.Sprintf("%s-%s", p.OS, p.Arch)
	}
	return fmt.Sprintf("%s-%s-%s", p.OS, p.Arch, stdlib)
}

func shortStdlib(stdlib string) string {
	switch stdlib {
	case "libstdc++":
		return "libstdcxx"
	case "libc++":
		return "libcxx"
	case "msvc":
		return "msvc"
	default:
		return stdlib
	}
}
