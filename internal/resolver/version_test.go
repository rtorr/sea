package resolver

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input   string
		want    Version
		wantErr bool
	}{
		{"1.2.3", Version{1, 2, 3}, false},
		{"0.0.1", Version{0, 0, 1}, false},
		{"10.20.30", Version{10, 20, 30}, false},
		{"v1.2.3", Version{1, 2, 3}, false},
		{"  1.2.3  ", Version{1, 2, 3}, false},
		// These should all be REJECTED — strict major.minor.patch only
		{"1.2.3-beta", Version{}, true},
		{"1.2.3-alpha.1", Version{}, true},
		{"1.2.3-rc.2+build.123", Version{}, true},
		{"1.0.0+meta", Version{}, true},
		{"bad", Version{}, true},
		{"1.2", Version{}, true},
		{"", Version{}, true},
		{"-1.0.0", Version{}, true},
		{"1.2.3.4", Version{}, true},
	}
	for _, tt := range tests {
		got, err := ParseVersion(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("ParseVersion(%q) = %v, want %v", tt.input, got, tt.want)
		}
		if tt.wantErr && err != nil {
			t.Logf("ParseVersion(%q) correctly rejected: %v", tt.input, err)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		a, b Version
		want int
	}{
		{MustParseVersion("1.0.0"), MustParseVersion("1.0.0"), 0},
		{MustParseVersion("1.0.0"), MustParseVersion("2.0.0"), -1},
		{MustParseVersion("2.0.0"), MustParseVersion("1.0.0"), 1},
		{MustParseVersion("1.1.0"), MustParseVersion("1.0.0"), 1},
		{MustParseVersion("1.0.1"), MustParseVersion("1.0.0"), 1},
		{MustParseVersion("0.0.1"), MustParseVersion("0.0.0"), 1},
	}
	for _, tt := range tests {
		got := tt.a.Compare(tt.b)
		if got != tt.want {
			t.Errorf("%s.Compare(%s) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseRange(t *testing.T) {
	tests := []struct {
		input    string
		version  string
		contains bool
	}{
		// Caret — same major = ABI compatible
		{"^1.0.0", "1.0.0", true},
		{"^1.0.0", "1.9.9", true},
		{"^1.0.0", "2.0.0", false},
		{"^1.0.0", "0.9.9", false},
		// Caret with 0.x — same minor
		{"^0.2.0", "0.2.0", true},
		{"^0.2.0", "0.2.5", true},
		{"^0.2.0", "0.3.0", false},

		// Tilde — same minor
		{"~1.2.0", "1.2.0", true},
		{"~1.2.0", "1.2.9", true},
		{"~1.2.0", "1.3.0", false},

		// Range
		{">=1.0.0, <2.0.0", "1.5.0", true},
		{">=1.0.0, <2.0.0", "2.0.0", false},
		{">=1.0.0, <2.0.0", "0.9.0", false},

		// Exact
		{"=1.2.3", "1.2.3", true},
		{"=1.2.3", "1.2.4", false},

		// Wildcard
		{"*", "99.99.99", true},
		{"", "1.0.0", true},

		// Not equal
		{"!=1.0.0", "1.0.1", true},
		{"!=1.0.0", "1.0.0", false},

		// Greater
		{">1.0.0", "1.0.1", true},
		{">1.0.0", "1.0.0", false},

		// Less
		{"<2.0.0", "1.9.9", true},
		{"<2.0.0", "2.0.0", false},

		// Double equals
		{"==1.0.0", "1.0.0", true},
		{"==1.0.0", "1.0.1", false},
	}

	for _, tt := range tests {
		vr, err := ParseRange(tt.input)
		if err != nil {
			t.Errorf("ParseRange(%q) error: %v", tt.input, err)
			continue
		}
		v := MustParseVersion(tt.version)
		got := vr.Contains(v)
		if got != tt.contains {
			t.Errorf("ParseRange(%q).Contains(%s) = %v, want %v", tt.input, tt.version, got, tt.contains)
		}
	}
}

func TestParseRangeInvalid(t *testing.T) {
	tests := []string{
		"^bad",
		"~bad",
		">=bad",
		">>1.0.0",
		"^1.0.0-beta", // pre-release not allowed
	}
	for _, input := range tests {
		_, err := ParseRange(input)
		if err == nil {
			t.Errorf("ParseRange(%q) expected error", input)
		}
	}
}

func TestIntersect(t *testing.T) {
	a, _ := ParseRange(">=1.0.0")
	b, _ := ParseRange("<2.0.0")
	merged, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("Intersect: %v", err)
	}
	if !merged.Contains(MustParseVersion("1.5.0")) {
		t.Error("1.5.0 should be in intersection")
	}
	if merged.Contains(MustParseVersion("2.0.0")) {
		t.Error("2.0.0 should not be in intersection")
	}
}

func TestIntersectContradiction(t *testing.T) {
	a, _ := ParseRange(">=2.0.0")
	b, _ := ParseRange("<1.0.0")
	_, err := Intersect(a, b)
	if err == nil {
		t.Error("expected contradiction error")
	}
}

func TestSortVersions(t *testing.T) {
	versions := []Version{
		MustParseVersion("1.0.0"),
		MustParseVersion("3.0.0"),
		MustParseVersion("2.0.0"),
		MustParseVersion("1.1.0"),
	}
	SortVersions(versions)
	if versions[0].String() != "3.0.0" {
		t.Errorf("first should be 3.0.0, got %s", versions[0])
	}
	if versions[len(versions)-1].String() != "1.0.0" {
		t.Errorf("last should be 1.0.0, got %s", versions[len(versions)-1])
	}
}

func TestFilterVersions(t *testing.T) {
	versions := []Version{
		MustParseVersion("3.0.0"),
		MustParseVersion("2.0.0"),
		MustParseVersion("1.0.0"),
	}
	vr, _ := ParseRange(">=2.0.0")
	filtered := FilterVersions(versions, vr)
	if len(filtered) != 2 {
		t.Errorf("expected 2 filtered, got %d", len(filtered))
	}
}

func TestVersionString(t *testing.T) {
	v := Version{1, 2, 3}
	if v.String() != "1.2.3" {
		t.Errorf("expected 1.2.3, got %s", v.String())
	}
}

func TestVersionIsZero(t *testing.T) {
	if !(Version{}).IsZero() {
		t.Error("zero version should be zero")
	}
	if (Version{1, 0, 0}).IsZero() {
		t.Error("1.0.0 should not be zero")
	}
}

func TestRangeString(t *testing.T) {
	vr, _ := ParseRange("^1.0.0")
	s := vr.String()
	if s != "^1.0.0" {
		t.Errorf("String() = %q, want ^1.0.0", s)
	}

	vr2 := VersionRange{}
	if vr2.String() != "*" {
		t.Errorf("empty range String() = %q, want *", vr2.String())
	}
}
