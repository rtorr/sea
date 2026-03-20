package cli

import (
	"testing"

	"github.com/rtorr/sea/internal/manifest"
)

func TestParsePackageArgEdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantVer  string
	}{
		// Basic cases
		{"zlib@1.3.1", "zlib", "=1.3.1"},
		{"zlib", "zlib", "*"},
		// Scoped-style name with @
		{"my-lib@2.0.0-beta.1", "my-lib", "=2.0.0-beta.1"},
		// No version, just a bare name
		{"openssl", "openssl", "*"},
		// Version with pre-release and build metadata
		{"foo@1.0.0-rc.1+build.42", "foo", "=1.0.0-rc.1+build.42"},
		// Name with underscores and numbers
		{"lib_foo2@3.0.0", "lib_foo2", "=3.0.0"},
		// Single character name
		{"z@1.0.0", "z", "=1.0.0"},
		// @ at beginning should return empty name but code uses idx > 0
		{"@1.0.0", "@1.0.0", "*"},
	}
	for _, tt := range tests {
		name, ver := parsePackageArg(tt.input)
		if name != tt.wantName || ver != tt.wantVer {
			t.Errorf("parsePackageArg(%q) = (%q, %q), want (%q, %q)",
				tt.input, name, ver, tt.wantName, tt.wantVer)
		}
	}
}

func TestDepLinking(t *testing.T) {
	m := &manifest.Manifest{
		Dependencies: map[string]manifest.Dependency{
			"zlib":    {Version: ">=1.0.0", Linking: "static"},
			"openssl": {Version: ">=1.0.0", Linking: "shared"},
			"fmt":     {Version: ">=1.0.0"}, // no linking preference
		},
		BuildDeps: map[string]manifest.Dependency{
			"cmake": {Version: ">=3.20", Linking: "static"},
		},
	}

	tests := []struct {
		name string
		want string
	}{
		{"zlib", "static"},
		{"openssl", "shared"},
		{"fmt", ""},
		{"cmake", "static"},      // found in build deps
		{"nonexistent", ""},       // not in manifest at all
	}

	for _, tt := range tests {
		got := depLinking(m, tt.name)
		if got != tt.want {
			t.Errorf("depLinking(m, %q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
