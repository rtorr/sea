package manifest

import (
	"fmt"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tomlData := []byte(`
[package]
name = "my-lib"
version = "2.1.0"
description = "A compression library"
license = "MIT"
authors = ["Alice"]
kind = "source"

[dependencies]
zlib = { version = ">=1.3.0, <2.0.0" }
openssl = { version = "^3.1.0", registry = "corp-artifactory" }
boost-headers = { version = "~1.84.0", optional = true }

[dependencies.protobuf]
version = ">=4.25.0"
abi_override = "gcc12-gcc13-compat"

[build]
script = "build.sh"
visibility = "hidden"

[publish]
registry = "corp-artifactory"
include = ["include/**", "lib/**", "LICENSE"]
`)

	m, err := Parse(tomlData)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if m.Package.Name != "my-lib" {
		t.Errorf("expected name my-lib, got %s", m.Package.Name)
	}
	if m.Package.Version != "2.1.0" {
		t.Errorf("expected version 2.1.0, got %s", m.Package.Version)
	}
	if m.Package.Kind != "source" {
		t.Errorf("expected kind source, got %s", m.Package.Kind)
	}
	if len(m.Dependencies) != 4 {
		t.Errorf("expected 4 dependencies, got %d", len(m.Dependencies))
	}
	if m.Dependencies["zlib"].Version != ">=1.3.0, <2.0.0" {
		t.Errorf("unexpected zlib version: %s", m.Dependencies["zlib"].Version)
	}
	if m.Dependencies["openssl"].Registry != "corp-artifactory" {
		t.Errorf("unexpected openssl registry: %s", m.Dependencies["openssl"].Registry)
	}
	if !m.Dependencies["boost-headers"].Optional {
		t.Error("expected boost-headers to be optional")
	}
	if m.Dependencies["protobuf"].ABIOverride != "gcc12-gcc13-compat" {
		t.Errorf("unexpected protobuf abi_override: %s", m.Dependencies["protobuf"].ABIOverride)
	}
	if m.Build.Script != "build.sh" {
		t.Errorf("expected build script build.sh, got %s", m.Build.Script)
	}
	if m.Build.Visibility != "hidden" {
		t.Errorf("expected visibility hidden, got %s", m.Build.Visibility)
	}
}

func TestParseValidation(t *testing.T) {
	tests := []struct {
		name    string
		toml    string
		wantErr string
	}{
		{
			name:    "missing name",
			toml:    "[package]\nversion = \"1.0.0\"",
			wantErr: "package.name is required",
		},
		{
			name:    "missing version",
			toml:    "[package]\nname = \"foo\"",
			wantErr: "package.version is required",
		},
		{
			name:    "invalid kind",
			toml:    "[package]\nname = \"foo\"\nversion = \"1.0.0\"\nkind = \"bad\"",
			wantErr: "package.kind",
		},
		{
			name:    "dep missing version",
			toml:    "[package]\nname = \"foo\"\nversion = \"1.0.0\"\n[dependencies]\nbar = {}",
			wantErr: "requires a version",
		},
		{
			name:    "invalid package name",
			toml:    "[package]\nname = \"Bad_Name!\"\nversion = \"1.0.0\"",
			wantErr: "package.name",
		},
		{
			name:    "invalid version",
			toml:    "[package]\nname = \"foo\"\nversion = \"not-semver\"",
			wantErr: "package.version",
		},
		{
			name:    "invalid visibility",
			toml:    "[package]\nname = \"foo\"\nversion = \"1.0.0\"\n[build]\nvisibility = \"bad\"",
			wantErr: "build.visibility",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.toml))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestParseValidCases(t *testing.T) {
	tests := []struct {
		name string
		toml string
	}{
		{
			name: "minimal",
			toml: "[package]\nname = \"foo\"\nversion = \"1.0.0\"",
		},
		{
			name: "with channel",
			toml: "[package]\nname = \"foo\"\nversion = \"1.0.0\"\nchannel = \"beta\"",
		},
		{
			name: "header only",
			toml: "[package]\nname = \"foo\"\nversion = \"1.0.0\"\nkind = \"header-only\"",
		},
		{
			name: "empty kind defaults",
			toml: "[package]\nname = \"foo\"\nversion = \"1.0.0\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.toml))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDefaultManifest(t *testing.T) {
	m := DefaultManifest("test-pkg")
	if m.Package.Name != "test-pkg" {
		t.Errorf("expected name test-pkg, got %s", m.Package.Name)
	}
	if m.Package.Version != "0.1.0" {
		t.Errorf("expected version 0.1.0, got %s", m.Package.Version)
	}
	if m.Dependencies == nil {
		t.Error("dependencies should not be nil")
	}
}

func TestPreReleaseVersionRejected(t *testing.T) {
	badVersions := []string{
		"1.0.0-beta",
		"1.0.0-alpha.1",
		"1.0.0-rc.2+build",
		"1.0.0+meta",
	}
	for _, v := range badVersions {
		toml := fmt.Sprintf("[package]\nname = \"foo\"\nversion = %q", v)
		_, err := Parse([]byte(toml))
		if err == nil {
			t.Errorf("version %q should be rejected — use channels for beta/rc/dev", v)
		}
	}
}

func TestChannelValidation(t *testing.T) {
	valid := []string{"stable", "beta", "rc", "dev", ""}
	for _, ch := range valid {
		toml := fmt.Sprintf("[package]\nname = \"foo\"\nversion = \"1.0.0\"\nchannel = %q", ch)
		_, err := Parse([]byte(toml))
		if err != nil {
			t.Errorf("channel %q should be valid, got: %v", ch, err)
		}
	}

	_, err := Parse([]byte("[package]\nname = \"foo\"\nversion = \"1.0.0\"\nchannel = \"nightly\""))
	if err == nil {
		t.Error("channel 'nightly' should be rejected")
	}
}

func TestEffectiveChannel(t *testing.T) {
	m := &Manifest{}
	if m.EffectiveChannel() != "stable" {
		t.Errorf("expected default channel stable, got %s", m.EffectiveChannel())
	}
	m.Package.Channel = "beta"
	if m.EffectiveChannel() != "beta" {
		t.Errorf("expected beta, got %s", m.EffectiveChannel())
	}
}

func TestEffectiveKind(t *testing.T) {
	m := &Manifest{}
	if m.EffectiveKind() != "source" {
		t.Errorf("expected default kind source, got %s", m.EffectiveKind())
	}
	m.Package.Kind = "header-only"
	if m.EffectiveKind() != "header-only" {
		t.Errorf("expected header-only, got %s", m.EffectiveKind())
	}
}

func TestAllDependencies(t *testing.T) {
	m := &Manifest{
		Dependencies: map[string]Dependency{
			"zlib":    {Version: ">=1.0.0"},
			"openssl": {Version: "^3.0.0", Optional: true},
			"boost":   {Version: "~1.84.0"},
		},
	}

	all := m.AllDependencies(true)
	if len(all) != 3 {
		t.Errorf("expected 3 deps with optional, got %d", len(all))
	}

	required := m.AllDependencies(false)
	if len(required) != 2 {
		t.Errorf("expected 2 deps without optional, got %d", len(required))
	}
	if _, ok := required["openssl"]; ok {
		t.Error("openssl should be excluded when optional=false")
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	m := DefaultManifest("test-pkg")
	m.Dependencies["zlib"] = Dependency{Version: ">=1.0.0"}

	data, err := Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	m2, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse after Marshal: %v", err)
	}

	if m2.Package.Name != m.Package.Name {
		t.Errorf("name mismatch: %s != %s", m2.Package.Name, m.Package.Name)
	}
	if m2.Dependencies["zlib"].Version != ">=1.0.0" {
		t.Errorf("dep version mismatch: %s", m2.Dependencies["zlib"].Version)
	}
}
