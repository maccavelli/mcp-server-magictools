package main

import (
	"testing"
)

func TestRootCommand(t *testing.T) {
	rootCmd.SetArgs([]string{"--help"})

	// HijackStdout tests
	HijackStdout()
	if err := rootCmd.Execute(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestServeCommandFlags(t *testing.T) {
	serveCmd.SetArgs([]string{"--help"})
	if err := serveCmd.Execute(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestConfigureCommandFlags(t *testing.T) {
	configureCmd.SetArgs([]string{"--help"})
	if err := configureCmd.Execute(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDbCommandFlags(t *testing.T) {
	dbCmd.SetArgs([]string{"--help"})
	if err := dbCmd.Execute(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestExecuteRoot(t *testing.T) {
	rootCmd.SetArgs([]string{"--help"})
	Execute()
}
