package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitPreservesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	serversPath := filepath.Join(tmpDir, "servers.yaml")
	overridesPath := filepath.Join(tmpDir, "overrides.yaml")

	sentinel := []byte("sentinel: true\n")
	os.WriteFile(configPath, sentinel, 0600)
	os.WriteFile(serversPath, sentinel, 0600)
	os.WriteFile(overridesPath, sentinel, 0600)

	err := ensureInitialized(tmpDir, configPath, serversPath, overridesPath, false)
	if err != nil {
		t.Fatalf("ensureInitialized failed: %v", err)
	}

	for _, p := range []string{configPath, serversPath, overridesPath} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != string(sentinel) {
			t.Errorf("file %s was modified under non-forced init", p)
		}
	}
}

func TestConfigureRejectsLegacyJSON(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "mcp_config.json")
	CfgPath = jsonPath
	defer func() { CfgPath = "" }()

	err := runConfigure(configureCmd, nil)
	if err == nil {
		t.Fatal("expected runConfigure to reject .json target, got nil")
	}
	if !strings.Contains(err.Error(), ".json") {
		t.Errorf("unexpected error message: %v", err)
	}
}
