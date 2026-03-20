package profile

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
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
