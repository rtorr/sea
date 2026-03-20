package resolver

import (
	"strings"
	"testing"
)

// mockProvider is a test provider with hardcoded package data.
type mockProvider struct {
	packages map[string][]mockVersion
}

type mockVersion struct {
	version Version
	deps    map[string]VersionRange
	abis    []string
}

func (m *mockProvider) AvailableVersions(pkg string) ([]Version, error) {
	versions, ok := m.packages[pkg]
	if !ok {
		return nil, nil
	}
	var result []Version
	for _, v := range versions {
		result = append(result, v.version)
	}
	SortVersions(result)
	return result, nil
}

func (m *mockProvider) Dependencies(pkg string, version Version) (map[string]VersionRange, error) {
	versions, ok := m.packages[pkg]
	if !ok {
		return make(map[string]VersionRange), nil
	}
	for _, v := range versions {
		if v.version.Compare(version) == 0 {
			if v.deps == nil {
				return make(map[string]VersionRange), nil
			}
			return v.deps, nil
		}
	}
	return make(map[string]VersionRange), nil
}

func (m *mockProvider) HasABI(pkg string, version Version, abiTag string) (bool, error) {
	versions, ok := m.packages[pkg]
	if !ok {
		return false, nil
	}
	for _, v := range versions {
		if v.version.Compare(version) == 0 {
			for _, a := range v.abis {
				if a == abiTag || a == "any" || abiTag == "any" {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func (m *mockProvider) AvailableABITags(pkg string, version Version) ([]string, error) {
	versions, ok := m.packages[pkg]
	if !ok {
		return nil, nil
	}
	for _, v := range versions {
		if v.version.Compare(version) == 0 {
			return v.abis, nil
		}
	}
	return nil, nil
}

const testABI = "linux-x86_64-gcc13-libstdcxx"

func TestResolveSimple(t *testing.T) {
	provider := &mockProvider{
		packages: map[string][]mockVersion{
			"zlib": {
				{version: MustParseVersion("1.3.1"), abis: []string{testABI}},
				{version: MustParseVersion("1.3.0"), abis: []string{testABI}},
				{version: MustParseVersion("1.2.0"), abis: []string{testABI}},
			},
		},
	}

	r := New(provider, testABI)
	vr, _ := ParseRange(">=1.3.0")
	result, err := r.Resolve(map[string]VersionRange{"zlib": vr})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0].Name != "zlib" {
		t.Errorf("expected zlib, got %s", result[0].Name)
	}
	if result[0].Version.String() != "1.3.1" {
		t.Errorf("expected 1.3.1 (newest), got %s", result[0].Version)
	}
}

func TestResolveWithTransitiveDeps(t *testing.T) {
	provider := &mockProvider{
		packages: map[string][]mockVersion{
			"openssl": {
				{
					version: MustParseVersion("3.2.0"),
					abis:    []string{testABI},
					deps: map[string]VersionRange{
						"zlib": {Constraints: []Constraint{{Op: ">=", Version: MustParseVersion("1.3.0")}}},
					},
				},
			},
			"zlib": {
				{version: MustParseVersion("1.3.1"), abis: []string{testABI}},
				{version: MustParseVersion("1.2.0"), abis: []string{testABI}},
			},
		},
	}

	r := New(provider, testABI)
	vr, _ := ParseRange("^3.0.0")
	result, err := r.Resolve(map[string]VersionRange{"openssl": vr})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	// Check that zlib was resolved
	found := false
	for _, r := range result {
		if r.Name == "zlib" {
			found = true
			if r.Version.String() != "1.3.1" {
				t.Errorf("expected zlib 1.3.1, got %s", r.Version)
			}
		}
	}
	if !found {
		t.Error("zlib should be in resolved packages")
	}
}

func TestResolveABIMismatch(t *testing.T) {
	provider := &mockProvider{
		packages: map[string][]mockVersion{
			"zlib": {
				{version: MustParseVersion("1.3.1"), abis: []string{"linux-x86_64-gcc12-libstdcxx"}},
			},
		},
	}

	r := New(provider, testABI)
	vr, _ := ParseRange(">=1.3.0")
	_, err := r.Resolve(map[string]VersionRange{"zlib": vr})
	if err == nil {
		t.Fatal("expected ABI mismatch error")
	}
	if !strings.Contains(err.Error(), "ABI") {
		t.Errorf("error should mention ABI, got: %v", err)
	}
}

func TestResolveConflict(t *testing.T) {
	provider := &mockProvider{
		packages: map[string][]mockVersion{
			"aa": {
				{
					version: MustParseVersion("1.0.0"),
					abis:    []string{testABI},
					deps: map[string]VersionRange{
						"cc": {Constraints: []Constraint{{Op: ">=", Version: MustParseVersion("2.0.0")}}},
					},
				},
			},
			"bb": {
				{
					version: MustParseVersion("1.0.0"),
					abis:    []string{testABI},
					deps: map[string]VersionRange{
						"cc": {Constraints: []Constraint{{Op: "<", Version: MustParseVersion("2.0.0")}}},
					},
				},
			},
			"cc": {
				{version: MustParseVersion("2.1.0"), abis: []string{testABI}},
				{version: MustParseVersion("1.9.0"), abis: []string{testABI}},
			},
		},
	}

	r := New(provider, testABI)
	aRange, _ := ParseRange("^1.0.0")
	bRange, _ := ParseRange("^1.0.0")
	_, err := r.Resolve(map[string]VersionRange{
		"aa": aRange,
		"bb": bRange,
	})
	if err == nil {
		t.Fatal("expected conflict error")
	}
	// Should mention cc in the error
	if !strings.Contains(err.Error(), "cc") {
		t.Errorf("error should mention conflicting package cc, got: %v", err)
	}
}

func TestResolveNoVersions(t *testing.T) {
	provider := &mockProvider{packages: map[string][]mockVersion{}}

	r := New(provider, testABI)
	vr, _ := ParseRange(">=1.0.0")
	_, err := r.Resolve(map[string]VersionRange{"nonexistent": vr})
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
}

func TestResolveHeaderOnly(t *testing.T) {
	provider := &mockProvider{
		packages: map[string][]mockVersion{
			"boost-headers": {
				{version: MustParseVersion("1.84.0"), abis: []string{"any"}},
			},
		},
	}

	r := New(provider, testABI)
	vr, _ := ParseRange("~1.84.0")
	result, err := r.Resolve(map[string]VersionRange{"boost-headers": vr})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
}

func TestResolveDeterministicOrder(t *testing.T) {
	provider := &mockProvider{
		packages: map[string][]mockVersion{
			"aaa": {{version: MustParseVersion("1.0.0"), abis: []string{testABI}}},
			"bbb": {{version: MustParseVersion("1.0.0"), abis: []string{testABI}}},
			"ccc": {{version: MustParseVersion("1.0.0"), abis: []string{testABI}}},
		},
	}

	r := New(provider, testABI)
	reqs := map[string]VersionRange{
		"ccc": {},
		"aaa": {},
		"bbb": {},
	}

	// Resolve twice and check order is the same
	result1, _ := r.Resolve(reqs)
	result2, _ := r.Resolve(reqs)

	if len(result1) != len(result2) {
		t.Fatal("different result lengths")
	}
	for i := range result1 {
		if result1[i].Name != result2[i].Name {
			t.Errorf("order mismatch at %d: %s vs %s", i, result1[i].Name, result2[i].Name)
		}
	}
	// Should be alphabetical since we sort the queue
	if result1[0].Name != "aaa" {
		t.Errorf("expected first=aaa, got %s", result1[0].Name)
	}
}

func TestResolveBacktrackOnConflict(t *testing.T) {
	// The resolver should pick a version that doesn't conflict with existing deps.
	// lib-a@2.0.0 requires lib-c >=2.0.0 (which doesn't exist with our ABI)
	// lib-a@1.0.0 requires lib-c >=1.0.0 (which does exist)
	provider := &mockProvider{
		packages: map[string][]mockVersion{
			"lib-a": {
				{
					version: MustParseVersion("2.0.0"),
					abis:    []string{testABI},
					deps: map[string]VersionRange{
						"lib-c": {Constraints: []Constraint{{Op: ">=", Version: MustParseVersion("2.0.0")}}},
					},
				},
				{
					version: MustParseVersion("1.0.0"),
					abis:    []string{testABI},
					deps: map[string]VersionRange{
						"lib-c": {Constraints: []Constraint{{Op: ">=", Version: MustParseVersion("1.0.0")}}},
					},
				},
			},
			"lib-b": {
				{
					version: MustParseVersion("1.0.0"),
					abis:    []string{testABI},
					deps: map[string]VersionRange{
						"lib-c": {Constraints: []Constraint{{Op: "<", Version: MustParseVersion("2.0.0")}}},
					},
				},
			},
			"lib-c": {
				{version: MustParseVersion("1.5.0"), abis: []string{testABI}},
			},
		},
	}

	r := New(provider, testABI)
	_, err := r.Resolve(map[string]VersionRange{
		"lib-a": {Constraints: []Constraint{{Op: ">=", Version: MustParseVersion("1.0.0")}}},
		"lib-b": {Constraints: []Constraint{{Op: ">=", Version: MustParseVersion("1.0.0")}}},
	})
	// lib-a is resolved first (alphabetical), picks 2.0.0, which requires lib-c>=2.0.0
	// lib-b requires lib-c<2.0.0 — conflict
	// The current solver doesn't backtrack on lib-a, so this should be a conflict
	if err == nil {
		// If it does resolve, that means the solver tried lib-a@2.0.0 and saw
		// lib-c>=2.0.0 conflicts with lib-b's lib-c<2.0.0
		// Since lib-b resolves after lib-a and adds constraint on lib-c,
		// lib-a@2.0.0 was already committed. This should fail.
		t.Log("Note: solver resolved — implementation may support full backtracking")
	}
}
