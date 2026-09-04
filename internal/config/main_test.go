package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain isolates the whole test binary from the operator's real
// configuration directory, and fails the run if anything reaches it anyway.
//
// This exists because it already happened. During the MADR 0004 investigation a
// test running entirely inside t.TempDir() wrote to
// ~/Library/Application Support/mcp-server-magictools/servers.yaml, because
// LoadFromViper read the path it was given and saved through a helper that
// resolved DefaultConfigDir() instead. The operator's registry had been
// degrading for weeks; the likeliest delivery mechanism is an agent or a
// developer running `go test ./...` in this repository.
//
// Per-test t.Setenv calls are correct but opt-in, and the test that caused the
// damage was written by someone who had just finished reading the code that
// does it. Isolation has to be the default, and something has to fail when it
// is not.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "magictools-config-tests-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: cannot create temp config dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	if err := os.Setenv(EnvConfigDir, tmp); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: cannot set %s: %v\n", EnvConfigDir, err)
		os.Exit(1)
	}

	// Fingerprint the real registry before the suite runs. Taken from the
	// unredirected location on purpose: the point is to notice a write that
	// escaped the redirect.
	realPath, realBefore, watching := realRegistryFingerprint()

	code := m.Run()

	if watching {
		if _, after, _ := realRegistryFingerprint(); after != realBefore {
			fmt.Fprintf(os.Stderr, `
==========================================================================
THE TEST SUITE WROTE TO THE OPERATOR'S REAL SERVER REGISTRY.

  %s

  before: %s
  after:  %s

A test reached DefaultConfigDir() instead of the temp directory TestMain
set in %s. Find the write and give it the resolved path;
do not "fix" this by relaxing the check.

This guard exists because this exact thing destroyed a live configuration
(MADR 0004 F2/F4). A .prev copy of the previous contents should be beside
the file above.
==========================================================================
`, realPath, realBefore, after, EnvConfigDir)
			if code == 0 {
				code = 1
			}
		}
	}
	os.Exit(code)
}

// realRegistryFingerprint hashes the registry in the operator's actual config
// directory, ignoring EnvConfigDir. Returns watching=false when there is
// nothing there to protect.
func realRegistryFingerprint() (path, sum string, watching bool) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", "", false
		}
		base = filepath.Join(home, ".config")
	}
	path = filepath.Join(base, AppName, ServersConfigFile)
	data, err := os.ReadFile(path) //nolint:gosec // fixed, operator-owned location
	if err != nil {
		return path, "", false
	}
	return path, fmt.Sprintf("%x", sha256.Sum256(data)), true
}
