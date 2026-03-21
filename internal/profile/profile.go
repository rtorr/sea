package profile

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/rtorr/sea/internal/abi"
)

// Profile represents a build profile (profiles/*.toml).
type Profile struct {
	Name             string            `toml:"name"`
	OS               string            `toml:"os"`
	Arch             string            `toml:"arch"`
	Compiler         string            `toml:"compiler"`
	CompilerVersion  string            `toml:"compiler_version"`
	CppStdlib        string            `toml:"cpp_stdlib"`
	BuildType        string            `toml:"build_type"`
	Sysroot          string            `toml:"sysroot,omitempty"`
	ToolchainPrefix  string            `toml:"toolchain_prefix,omitempty"`
	CFlags           string            `toml:"cflags,omitempty"`
	CXXFlags         string            `toml:"cxxflags,omitempty"`
	LDFlags          string            `toml:"ldflags,omitempty"`
	Env              map[string]string `toml:"env"`

	// ABIFingerprintHash is the cached ABI probe fingerprint for this profile.
	// Computed once by ProbeABI() and cached for the session.
	ABIFingerprintHash string `toml:"abi_fingerprint,omitempty"`
}

// LoadFile loads a profile from a TOML file.
func LoadFile(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading profile: %w", err)
	}
	return Parse(data)
}

// Parse parses a profile from TOML bytes.
func Parse(data []byte) (*Profile, error) {
	var p Profile
	if err := toml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing profile: %w", err)
	}
	if err := validate(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func validate(p *Profile) error {
	if p.OS == "" {
		return fmt.Errorf("profile: os is required")
	}
	if p.Arch == "" {
		return fmt.Errorf("profile: arch is required")
	}
	if p.Compiler == "" {
		return fmt.Errorf("profile: compiler is required")
	}
	if p.CompilerVersion == "" {
		return fmt.Errorf("profile: compiler_version is required")
	}
	return nil
}

// EnsureFingerprint computes the ABI fingerprint if not already set.
// This runs the ABI probe — a small C++ program that measures actual type
// layouts, name mangling, and exception ABI of the toolchain. The result
// is a hash that two toolchains share if and only if they produce
// link-compatible binaries.
//
// If no C++ stdlib is configured (pure C packages), the fingerprint is
// derived from OS + arch alone, since the C ABI is stable per platform.
func (p *Profile) EnsureFingerprint() error {
	if p.ABIFingerprintHash != "" {
		return nil
	}

	// Pure C or no stdlib — C ABI is stable per platform
	if p.CppStdlib == "" {
		p.ABIFingerprintHash = fmt.Sprintf("c-%s-%s", p.OS, p.Arch)
		return nil
	}

	// Determine the C++ compiler to probe
	cxx := "g++"
	if p.Env != nil {
		if v, ok := p.Env["CXX"]; ok && v != "" {
			cxx = v
		}
	}

	fp, err := abi.ProbeToolchain(cxx, p.OS, p.Arch)
	if err != nil {
		return fmt.Errorf("ABI probe failed: %w", err)
	}

	p.ABIFingerprintHash = fp.Hash
	return nil
}
