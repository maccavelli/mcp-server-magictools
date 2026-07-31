package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePaths_Precedence(t *testing.T) {
	tmpDir := t.TempDir()

	flagFile := filepath.Join(tmpDir, "flag_config.yaml")
	envFile := filepath.Join(tmpDir, "env_config.yaml")
	dirOverride := filepath.Join(tmpDir, "custom_dir")

	t.Setenv(EnvConfigPath, envFile)
	t.Setenv(EnvConfigDir, dirOverride)

	// Flag takes precedence over env and default
	p1, err := ResolvePaths(flagFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p1.Config != flagFile {
		t.Errorf("expected %s, got %s", flagFile, p1.Config)
	}
	if p1.Dir != tmpDir {
		t.Errorf("expected dir %s, got %s", tmpDir, p1.Dir)
	}
	if p1.Servers != filepath.Join(tmpDir, "servers.yaml") {
		t.Errorf("unexpected servers path: %s", p1.Servers)
	}

	// Env takes precedence when flag is empty
	p2, err := ResolvePaths("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p2.Config != envFile {
		t.Errorf("expected %s, got %s", envFile, p2.Config)
	}

	// Custom dir used when flag and env path are empty
	t.Setenv(EnvConfigPath, "")
	p3, err := ResolvePaths("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedConfig := filepath.Join(dirOverride, "config.yaml")
	if p3.Config != expectedConfig {
		t.Errorf("expected %s, got %s", expectedConfig, p3.Config)
	}
}

func TestResolvePaths_DirectoryInputError(t *testing.T) {
	tmpDir := t.TempDir()
	dirPath := filepath.Join(tmpDir, "somedir")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := ResolvePaths(dirPath)
	if err == nil {
		t.Fatal("expected error when passing directory as config file, got nil")
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("unexpected error message: %v", err)
	}
}
