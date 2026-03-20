package profile

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// CompatRule defines an ABI compatibility rule.
type CompatRule struct {
	Name   string   `toml:"name"`
	Tags   []string `toml:"tags"`
	Reason string   `toml:"reason"`
}

// CompatRules holds a set of ABI compatibility rules.
type CompatRules struct {
	Rules []CompatRule `toml:"rules"`
}

// AreCompatible checks if two ABI tags are compatible given the rules.
// Tags are compatible if they are equal, if either is "any", or if any rule covers both.
func AreCompatible(tag1, tag2 string, rules []CompatRule) bool {
	if tag1 == tag2 {
		return true
	}
	if tag1 == "any" || tag2 == "any" {
		return true
	}
	for _, rule := range rules {
		has1, has2 := false, false
		for _, t := range rule.Tags {
			if t == tag1 {
				has1 = true
			}
			if t == tag2 {
				has2 = true
			}
		}
		if has1 && has2 {
			return true
		}
	}
	return false
}

// ParsedABITag holds the decomposed parts of an ABI tag.
// Format: {os}-{arch}-{compiler}{major}-{stdlib}
type ParsedABITag struct {
	OS       string
	Arch     string
	Compiler string // includes ABI major, e.g. "gcc13"
	Stdlib   string
	Raw      string
}

// ParseABITag decomposes an ABI tag like "linux-x86_64-gcc13-libstdcxx" into parts.
func ParseABITag(tag string) ParsedABITag {
	p := ParsedABITag{Raw: tag}
	parts := strings.SplitN(tag, "-", 4)
	if len(parts) >= 1 {
		p.OS = parts[0]
	}
	if len(parts) >= 2 {
		p.Arch = parts[1]
	}
	if len(parts) >= 3 {
		p.Compiler = parts[2]
	}
	if len(parts) >= 4 {
		p.Stdlib = parts[3]
	}
	return p
}

// RankCompatibility returns a score for how compatible two ABI tags are.
// Higher is better. 0 means incompatible.
//
//	100 = exact match
//	 90 = "any" tag (header-only)
//	 80 = same OS/arch/stdlib, compatible compiler (covered by a compat rule)
//	  0 = incompatible
func RankCompatibility(available, wanted string, rules []CompatRule) int {
	if available == wanted {
		return 100
	}
	if available == "any" || wanted == "any" {
		return 90
	}

	a := ParseABITag(available)
	w := ParseABITag(wanted)

	// OS and arch must match — these are never compatible across
	if a.OS != w.OS || a.Arch != w.Arch {
		return 0
	}

	// Check compat rules
	for _, rule := range rules {
		has1, has2 := false, false
		for _, t := range rule.Tags {
			if t == available {
				has1 = true
			}
			if t == wanted {
				has2 = true
			}
		}
		if has1 && has2 {
			return 80
		}
	}

	return 0
}

// LoadCompatRules reads ABI compatibility rules from a TOML file.
// Returns nil (not an error) if the file doesn't exist.
func LoadCompatRules(path string) ([]CompatRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading compat rules: %w", err)
	}
	var rules CompatRules
	if err := toml.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("parsing compat rules: %w", err)
	}
	return rules.Rules, nil
}
