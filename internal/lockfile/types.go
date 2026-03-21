package lockfile

// LockFile represents a sea.lock file.
type LockFile struct {
	Version  int              `toml:"version"`
	Packages []LockedPackage  `toml:"package"`
}

// LockedPackage represents a resolved and locked dependency.
type LockedPackage struct {
	Name        string   `toml:"name"`
	Version     string   `toml:"version"`
	ABI         string   `toml:"abi"`
	Fingerprint string   `toml:"fingerprint,omitempty"` // ABI probe fingerprint
	SHA256      string   `toml:"sha256"`
	Registry    string   `toml:"registry"`
	Deps        []string `toml:"deps"`
	Channel     string   `toml:"channel,omitempty"`
}
