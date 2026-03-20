package manifest

import (
	"testing"
)

func TestAllDependenciesWithFeaturesNoFeatures(t *testing.T) {
	m := &Manifest{
		Dependencies: map[string]Dependency{
			"zlib":    {Version: "^1.3.0"},
			"openssl": {Version: "^3.0.0"},
		},
		Features: map[string]Feature{
			"ssl": {
				Description: "Enable TLS support",
				Dependencies: map[string]Dependency{
					"gnutls": {Version: "^3.8.0"},
				},
			},
		},
	}

	deps := m.AllDependenciesWithFeatures(nil)
	if len(deps) != 2 {
		t.Fatalf("expected 2 deps with no features, got %d", len(deps))
	}
	if _, ok := deps["zlib"]; !ok {
		t.Error("expected zlib in deps")
	}
	if _, ok := deps["openssl"]; !ok {
		t.Error("expected openssl in deps")
	}
	if _, ok := deps["gnutls"]; ok {
		t.Error("gnutls should not be included without enabling ssl feature")
	}
}

func TestAllDependenciesWithFeaturesOneEnabled(t *testing.T) {
	m := &Manifest{
		Dependencies: map[string]Dependency{
			"zlib": {Version: "^1.3.0"},
		},
		Features: map[string]Feature{
			"ssl": {
				Description: "Enable TLS support",
				Dependencies: map[string]Dependency{
					"openssl": {Version: "^3.0.0"},
				},
			},
			"zstd": {
				Description: "Enable zstd compression",
				Dependencies: map[string]Dependency{
					"zstd": {Version: "^1.5.0"},
				},
			},
		},
	}

	deps := m.AllDependenciesWithFeatures([]string{"ssl"})
	if len(deps) != 2 {
		t.Fatalf("expected 2 deps with ssl feature, got %d", len(deps))
	}
	if _, ok := deps["zlib"]; !ok {
		t.Error("expected zlib in deps")
	}
	if _, ok := deps["openssl"]; !ok {
		t.Error("expected openssl in deps from ssl feature")
	}
	if _, ok := deps["zstd"]; ok {
		t.Error("zstd should not be included without enabling zstd feature")
	}
}

func TestAllDependenciesWithFeaturesOverlapping(t *testing.T) {
	m := &Manifest{
		Dependencies: map[string]Dependency{
			"zlib":    {Version: "^1.3.0"},
			"openssl": {Version: "^3.0.0"},
		},
		Features: map[string]Feature{
			"ssl-latest": {
				Description: "Use latest OpenSSL",
				Dependencies: map[string]Dependency{
					"openssl": {Version: "^3.2.0"}, // overrides base version
				},
			},
		},
	}

	deps := m.AllDependenciesWithFeatures([]string{"ssl-latest"})
	if len(deps) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(deps))
	}

	// The feature should override the base openssl version
	if deps["openssl"].Version != "^3.2.0" {
		t.Errorf("expected openssl version ^3.2.0 from feature override, got %s", deps["openssl"].Version)
	}
	if deps["zlib"].Version != "^1.3.0" {
		t.Errorf("expected zlib version ^1.3.0, got %s", deps["zlib"].Version)
	}
}

func TestAllDependenciesWithFeaturesMultiple(t *testing.T) {
	m := &Manifest{
		Dependencies: map[string]Dependency{
			"zlib": {Version: "^1.3.0"},
		},
		Features: map[string]Feature{
			"ssl": {
				Dependencies: map[string]Dependency{
					"openssl": {Version: "^3.0.0"},
				},
			},
			"zstd": {
				Dependencies: map[string]Dependency{
					"zstd": {Version: "^1.5.0"},
				},
			},
		},
	}

	deps := m.AllDependenciesWithFeatures([]string{"ssl", "zstd"})
	if len(deps) != 3 {
		t.Fatalf("expected 3 deps with both features, got %d", len(deps))
	}
	if _, ok := deps["zlib"]; !ok {
		t.Error("expected zlib")
	}
	if _, ok := deps["openssl"]; !ok {
		t.Error("expected openssl from ssl feature")
	}
	if _, ok := deps["zstd"]; !ok {
		t.Error("expected zstd from zstd feature")
	}
}

func TestAllDependenciesWithFeaturesEmptyList(t *testing.T) {
	m := &Manifest{
		Dependencies: map[string]Dependency{
			"zlib": {Version: "^1.3.0"},
		},
		Features: map[string]Feature{
			"ssl": {
				Dependencies: map[string]Dependency{
					"openssl": {Version: "^3.0.0"},
				},
			},
		},
	}

	// Empty slice should behave like nil (no features)
	deps := m.AllDependenciesWithFeatures([]string{})
	if len(deps) != 1 {
		t.Fatalf("expected 1 dep with empty features, got %d", len(deps))
	}
}

func TestAllDependenciesWithFeaturesExcludesOptional(t *testing.T) {
	m := &Manifest{
		Dependencies: map[string]Dependency{
			"zlib":    {Version: "^1.3.0"},
			"openssl": {Version: "^3.0.0", Optional: true},
		},
		Features: map[string]Feature{
			"ssl": {
				Dependencies: map[string]Dependency{
					"gnutls": {Version: "^3.8.0"},
				},
			},
		},
	}

	deps := m.AllDependenciesWithFeatures([]string{"ssl"})
	// zlib (required) + gnutls (from feature) = 2. openssl is optional, excluded.
	if len(deps) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(deps))
	}
	if _, ok := deps["openssl"]; ok {
		t.Error("optional openssl should be excluded from AllDependenciesWithFeatures")
	}
}

func TestParseManifestWithFeatures(t *testing.T) {
	tomlData := []byte(`
[package]
name = "my-lib"
version = "1.0.0"
kind = "source"

[dependencies]
zlib = { version = "^1.3.0" }

[features.ssl]
description = "Enable TLS support"
[features.ssl.dependencies]
openssl = { version = "^3.0.0" }

[features.zstd]
description = "Enable zstd compression"
[features.zstd.dependencies]
zstd = { version = "^1.5.0" }
`)

	m, err := Parse(tomlData)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(m.Features) != 2 {
		t.Fatalf("expected 2 features, got %d", len(m.Features))
	}

	ssl, ok := m.Features["ssl"]
	if !ok {
		t.Fatal("expected ssl feature")
	}
	if ssl.Description != "Enable TLS support" {
		t.Errorf("unexpected ssl description: %s", ssl.Description)
	}
	if ssl.Dependencies["openssl"].Version != "^3.0.0" {
		t.Errorf("unexpected openssl version: %s", ssl.Dependencies["openssl"].Version)
	}

	zstd, ok := m.Features["zstd"]
	if !ok {
		t.Fatal("expected zstd feature")
	}
	if zstd.Description != "Enable zstd compression" {
		t.Errorf("unexpected zstd description: %s", zstd.Description)
	}
}

func TestDependencyWithFeaturesList(t *testing.T) {
	tomlData := []byte(`
[package]
name = "my-app"
version = "1.0.0"
kind = "source"

[dependencies]
my-lib = { version = "^1.0.0", features = ["ssl", "zstd"] }
`)

	m, err := Parse(tomlData)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	dep := m.Dependencies["my-lib"]
	if len(dep.Features) != 2 {
		t.Fatalf("expected 2 features on my-lib, got %d", len(dep.Features))
	}
	if dep.Features[0] != "ssl" || dep.Features[1] != "zstd" {
		t.Errorf("unexpected features: %v", dep.Features)
	}
}
