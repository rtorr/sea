package profile

import "fmt"

// ABITag computes the ABI tag string for a profile.
// Format: {os}-{arch}-{compiler}{abi_major}-{stdlib_short}
func (p *Profile) ABITag() string {
	return fmt.Sprintf("%s-%s-%s%s-%s",
		p.OS,
		p.Arch,
		p.Compiler,
		p.abiMajor(),
		shortStdlib(p.CppStdlib),
	)
}

func (p *Profile) abiMajor() string {
	switch p.Compiler {
	case "gcc", "clang":
		return majorVersion(p.CompilerVersion)
	case "msvc":
		return p.CompilerVersion // already "v143" etc.
	default:
		return p.CompilerVersion
	}
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
