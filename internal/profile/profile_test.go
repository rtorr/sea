package profile

import (
	"testing"
)

func TestABITag(t *testing.T) {
	tests := []struct {
		name     string
		profile  Profile
		expected string
	}{
		{
			name: "linux gcc13",
			profile: Profile{
				OS: "linux", Arch: "x86_64",
				Compiler: "gcc", CompilerVersion: "13",
				CppStdlib: "libstdc++",
			},
			expected: "linux-x86_64-gcc13-libstdcxx",
		},
		{
			name: "darwin clang17",
			profile: Profile{
				OS: "darwin", Arch: "aarch64",
				Compiler: "clang", CompilerVersion: "17",
				CppStdlib: "libc++",
			},
			expected: "darwin-aarch64-clang17-libcxx",
		},
		{
			name: "windows msvc",
			profile: Profile{
				OS: "windows", Arch: "x86_64",
				Compiler: "msvc", CompilerVersion: "v143",
				CppStdlib: "msvc",
			},
			expected: "windows-x86_64-msvcv143-msvc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.profile.ABITag()
			if got != tt.expected {
				t.Errorf("ABITag() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDetectHost(t *testing.T) {
	p := DetectHost()
	if p.OS == "" {
		t.Error("OS should not be empty")
	}
	if p.Arch == "" {
		t.Error("Arch should not be empty")
	}
	if p.Compiler == "" {
		t.Error("Compiler should not be empty")
	}
	tag := p.ABITag()
	if tag == "" {
		t.Error("ABITag should not be empty")
	}
}

func TestAreCompatible(t *testing.T) {
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
		tag1, tag2 string
		want       bool
	}{
		{"linux-x86_64-gcc13-libstdcxx", "linux-x86_64-gcc13-libstdcxx", true},
		{"linux-x86_64-gcc12-libstdcxx", "linux-x86_64-gcc13-libstdcxx", true},
		{"linux-x86_64-gcc11-libstdcxx", "linux-x86_64-gcc13-libstdcxx", false},
		{"any", "linux-x86_64-gcc13-libstdcxx", true},
		{"linux-x86_64-gcc13-libstdcxx", "any", true},
	}

	for _, tt := range tests {
		got := AreCompatible(tt.tag1, tt.tag2, rules)
		if got != tt.want {
			t.Errorf("AreCompatible(%q, %q) = %v, want %v", tt.tag1, tt.tag2, got, tt.want)
		}
	}
}

func TestParseProfile(t *testing.T) {
	data := []byte(`
name = "aarch64-linux-gcc13"
os = "linux"
arch = "aarch64"
compiler = "gcc"
compiler_version = "13"
cpp_stdlib = "libstdc++"
build_type = "release"
cxxflags = "-march=armv8-a -std=c++17"

[env]
CC = "aarch64-linux-gnu-gcc-13"
CXX = "aarch64-linux-gnu-g++-13"
`)

	p, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if p.Name != "aarch64-linux-gcc13" {
		t.Errorf("unexpected name: %s", p.Name)
	}
	if p.ABITag() != "linux-aarch64-gcc13-libstdcxx" {
		t.Errorf("unexpected ABI tag: %s", p.ABITag())
	}
	if p.Env["CC"] != "aarch64-linux-gnu-gcc-13" {
		t.Errorf("unexpected CC: %s", p.Env["CC"])
	}
}
