package lockfile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

const FileName = "sea.lock"

// CurrentVersion is the latest lockfile schema version.
const CurrentVersion = 2

// Load reads a sea.lock from the given directory.
func Load(dir string) (*LockFile, error) {
	path := filepath.Join(dir, FileName)
	return LoadFile(path)
}

// LoadFile reads a sea.lock from the given path.
func LoadFile(path string) (*LockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading lockfile: %w", err)
	}
	var lf LockFile
	if err := toml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing lockfile: %w", err)
	}
	return &lf, nil
}

// Save writes a sea.lock to the given directory.
func Save(dir string, lf *LockFile) error {
	path := filepath.Join(dir, FileName)
	return SaveFile(path, lf)
}

// SaveFile writes a sea.lock to the given path.
func SaveFile(path string, lf *LockFile) error {
	if lf.Version == 0 {
		lf.Version = 1
	}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(lf); err != nil {
		return fmt.Errorf("encoding lockfile: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing lockfile: %w", err)
	}
	return nil
}

// Sort sorts the packages alphabetically by name for deterministic output.
func (lf *LockFile) Sort() {
	sort.Slice(lf.Packages, func(i, j int) bool {
		return lf.Packages[i].Name < lf.Packages[j].Name
	})
}

// Find looks up a locked package by name.
func (lf *LockFile) Find(name string) *LockedPackage {
	if lf == nil {
		return nil
	}
	for i := range lf.Packages {
		if lf.Packages[i].Name == name {
			return &lf.Packages[i]
		}
	}
	return nil
}

// Migrate upgrades a lockfile from an older schema version to CurrentVersion.
// Returns true if any migration was applied.
func Migrate(lf *LockFile) bool {
	if lf == nil {
		return false
	}
	migrated := false

	// Version 1 → 2: add Channel field (default "stable")
	if lf.Version < 2 {
		for i := range lf.Packages {
			if lf.Packages[i].Channel == "" {
				lf.Packages[i].Channel = "stable"
			}
		}
		lf.Version = 2
		migrated = true
	}

	return migrated
}
