package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const (
	DirName  = ".sea"
	FileName = "config.toml"
)

// SeaDir returns the path to the sea configuration directory.
func SeaDir() (string, error) {
	if d := os.Getenv("SEA_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, DirName), nil
}

// CacheDir returns the package cache directory.
func CacheDir(cfg *Config) (string, error) {
	if cfg != nil && cfg.CacheDir != "" {
		return cfg.CacheDir, nil
	}
	seaDir, err := SeaDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(seaDir, "cache"), nil
}

// Load reads the global config from ~/.sea/config.toml.
func Load() (*Config, error) {
	seaDir, err := SeaDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(seaDir, FileName)
	return LoadFile(path)
}

// LoadFile reads a config from a specific path.
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// Save writes the config to ~/.sea/config.toml.
func Save(cfg *Config) error {
	seaDir, err := SeaDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(seaDir, 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	return SaveFile(cfg, filepath.Join(seaDir, FileName))
}

// SaveFile writes a config to a specific path.
func SaveFile(cfg *Config, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating config file: %w", err)
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	return nil
}

// FindRemote looks up a remote by name.
func (c *Config) FindRemote(name string) *Remote {
	for i := range c.Remotes {
		if c.Remotes[i].Name == name {
			return &c.Remotes[i]
		}
	}
	return nil
}

// AddRemote adds a remote to the config. Returns error if name exists.
func (c *Config) AddRemote(r Remote) error {
	if c.FindRemote(r.Name) != nil {
		return fmt.Errorf("remote %q already exists", r.Name)
	}
	c.Remotes = append(c.Remotes, r)
	return nil
}

// RemoveRemote removes a remote by name. Returns error if not found.
func (c *Config) RemoveRemote(name string) error {
	for i := range c.Remotes {
		if c.Remotes[i].Name == name {
			c.Remotes = append(c.Remotes[:i], c.Remotes[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("remote %q not found", name)
}
