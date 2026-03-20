package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := &Config{
		Remotes: []Remote{
			{
				Name:       "local-pkgs",
				Type:       "filesystem",
				Path:       "/opt/sea-packages",
			},
			{
				Name:       "corp",
				Type:       "artifactory",
				URL:        "https://artifactory.corp.com",
				Repository: "sea-packages",
				TokenEnv:   "ARTIFACTORY_TOKEN",
			},
		},
	}

	if err := SaveFile(cfg, path); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	if len(loaded.Remotes) != 2 {
		t.Fatalf("expected 2 remotes, got %d", len(loaded.Remotes))
	}
	if loaded.Remotes[0].Name != "local-pkgs" {
		t.Errorf("expected local-pkgs, got %s", loaded.Remotes[0].Name)
	}
	if loaded.Remotes[1].TokenEnv != "ARTIFACTORY_TOKEN" {
		t.Errorf("expected ARTIFACTORY_TOKEN, got %s", loaded.Remotes[1].TokenEnv)
	}
}

func TestFindRemote(t *testing.T) {
	cfg := &Config{
		Remotes: []Remote{
			{Name: "a", Type: "filesystem"},
			{Name: "b", Type: "artifactory"},
		},
	}

	if r := cfg.FindRemote("a"); r == nil || r.Type != "filesystem" {
		t.Error("FindRemote(a) failed")
	}
	if r := cfg.FindRemote("c"); r != nil {
		t.Error("FindRemote(c) should return nil")
	}
}

func TestAddRemovRemote(t *testing.T) {
	cfg := &Config{}

	if err := cfg.AddRemote(Remote{Name: "test", Type: "filesystem"}); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	if len(cfg.Remotes) != 1 {
		t.Fatal("expected 1 remote")
	}

	// Duplicate
	if err := cfg.AddRemote(Remote{Name: "test", Type: "filesystem"}); err == nil {
		t.Error("expected duplicate error")
	}

	if err := cfg.RemoveRemote("test"); err != nil {
		t.Fatalf("RemoveRemote: %v", err)
	}
	if len(cfg.Remotes) != 0 {
		t.Error("expected 0 remotes")
	}

	if err := cfg.RemoveRemote("nonexistent"); err == nil {
		t.Error("expected not found error")
	}
}

func TestLoadNonExistent(t *testing.T) {
	cfg, err := LoadFile(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err != nil {
		t.Fatalf("LoadFile non-existent should not error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected empty config, not nil")
	}
}

func TestSeaDir(t *testing.T) {
	// Test with SEA_HOME override
	testDir := filepath.Join(t.TempDir(), "test-sea")
	t.Setenv("SEA_HOME", testDir)
	dir, err := SeaDir()
	if err != nil {
		t.Fatalf("SeaDir: %v", err)
	}
	if dir != testDir {
		t.Errorf("expected %s, got %s", testDir, dir)
	}

	// Test default
	os.Unsetenv("SEA_HOME")
	dir, err = SeaDir()
	if err != nil {
		t.Fatalf("SeaDir: %v", err)
	}
	if dir == "" {
		t.Error("SeaDir should not be empty")
	}
}
