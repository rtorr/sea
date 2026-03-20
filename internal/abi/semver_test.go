package abi

import (
	"testing"
)

func TestRequiredBump(t *testing.T) {
	tests := []struct {
		name    string
		old     []Symbol
		new     []Symbol
		want    VersionBump
	}{
		{
			name: "no changes",
			old:  []Symbol{{Name: "foo"}, {Name: "bar"}},
			new:  []Symbol{{Name: "foo"}, {Name: "bar"}},
			want: BumpNone,
		},
		{
			name: "symbols added",
			old:  []Symbol{{Name: "foo"}},
			new:  []Symbol{{Name: "foo"}, {Name: "bar"}},
			want: BumpMinor,
		},
		{
			name: "symbols removed",
			old:  []Symbol{{Name: "foo"}, {Name: "bar"}},
			new:  []Symbol{{Name: "foo"}},
			want: BumpMajor,
		},
		{
			name: "symbols added and removed",
			old:  []Symbol{{Name: "foo"}, {Name: "bar"}},
			new:  []Symbol{{Name: "foo"}, {Name: "baz"}},
			want: BumpMajor, // removal = breaking
		},
		{
			name: "empty to populated",
			old:  nil,
			new:  []Symbol{{Name: "foo"}},
			want: BumpMinor,
		},
		{
			name: "both empty",
			old:  nil,
			new:  nil,
			want: BumpNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := RequiredBump(tt.old, tt.new)
			if got != tt.want {
				t.Errorf("RequiredBump() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestVerifyVersion(t *testing.T) {
	old := []Symbol{{Name: "foo"}, {Name: "bar"}}
	newAdded := []Symbol{{Name: "foo"}, {Name: "bar"}, {Name: "baz"}}
	newRemoved := []Symbol{{Name: "foo"}}
	same := []Symbol{{Name: "foo"}, {Name: "bar"}}

	tests := []struct {
		name      string
		oldVer    [3]int
		newVer    [3]int
		oldSyms   []Symbol
		newSyms   []Symbol
		wantError bool
	}{
		// No changes — patch is fine
		{"no change, patch bump", [3]int{1, 2, 3}, [3]int{1, 2, 4}, old, same, false},

		// Added symbols — minor is required
		{"added, minor bump", [3]int{1, 2, 3}, [3]int{1, 3, 0}, old, newAdded, false},
		{"added, major bump", [3]int{1, 2, 3}, [3]int{2, 0, 0}, old, newAdded, false},
		{"added, only patch bump", [3]int{1, 2, 3}, [3]int{1, 2, 4}, old, newAdded, true}, // ERROR

		// Removed symbols — major is required
		{"removed, major bump", [3]int{1, 2, 3}, [3]int{2, 0, 0}, old, newRemoved, false},
		{"removed, minor bump only", [3]int{1, 2, 3}, [3]int{1, 3, 0}, old, newRemoved, true}, // ERROR
		{"removed, patch bump only", [3]int{1, 2, 3}, [3]int{1, 2, 4}, old, newRemoved, true}, // ERROR
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyVersion(
				tt.oldVer[0], tt.oldVer[1], tt.oldVer[2],
				tt.newVer[0], tt.newVer[1], tt.newVer[2],
				tt.oldSyms, tt.newSyms,
			)
			if (err != nil) != tt.wantError {
				t.Errorf("VerifyVersion() error = %v, wantError %v", err, tt.wantError)
			}
			if err != nil {
				t.Logf("  Error: %v", err)
			}
		})
	}
}

func TestVersionBumpString(t *testing.T) {
	tests := []struct {
		bump VersionBump
		want string
	}{
		{BumpNone, "none"},
		{BumpPatch, "patch"},
		{BumpMinor, "minor"},
		{BumpMajor, "major"},
	}
	for _, tt := range tests {
		if got := tt.bump.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.bump, got, tt.want)
		}
	}
}
