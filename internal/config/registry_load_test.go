package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// A registry an operator would recognise as theirs. What matters is that none
// of it appears in the shipped template, so any replacement is obvious.
const operatorRegistry = `servers:
  - name: my-private-tool
    command: /Users/me/bin/private-tool
    args: ["--serve"]
  - name: team-scanner
    command: /opt/team/scanner
    env:
      TOKEN: hunter2
`

func writeRegistry(t *testing.T, servers string) (dir string, v *viper.Viper) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("logLevel: info\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if servers != "" {
		if err := os.WriteFile(filepath.Join(dir, ServersConfigFile), []byte(servers), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	v = viper.New()
	v.SetConfigFile(filepath.Join(dir, "config.yaml"))
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	return dir, v
}

func hashOf(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// Acceptance 1. The reported defect: a YAML typo replaced the whole registry.
func TestUnparseableRegistryFailsTheLoadAndChangesNothing(t *testing.T) {
	// A single stray tab: the classic YAML error, and what a half-finished edit
	// or a tool writing YAML by hand produces.
	dir, v := writeRegistry(t, operatorRegistry+"\tnot valid yaml\n")
	path := filepath.Join(dir, ServersConfigFile)
	before := hashOf(t, path)

	_, err := LoadFromViper(v)
	if err == nil {
		t.Fatal("an unparseable registry loaded successfully; downstream cannot tell " +
			"'no servers' from 'I could not read your servers'")
	}
	if !errors.Is(err, ErrRegistryUnparseable) {
		t.Errorf("err = %v, want ErrRegistryUnparseable", err)
	}
	// The parser's line number is the whole value of the message.
	if !strings.Contains(err.Error(), "line") {
		t.Errorf("err = %v, want the parser's line number", err)
	}
	if after := hashOf(t, path); after != before {
		body, _ := os.ReadFile(path)
		t.Fatalf("THE REGISTRY WAS MODIFIED BY A FAILED LOAD.\nnow starts: %.120s", body)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "my-private-tool") {
		t.Fatal("the operator's servers are gone")
	}
}

// Acceptance 2.
func TestUnreadableRegistryFailsTheLoadAndChangesNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores mode bits")
	}
	dir, v := writeRegistry(t, operatorRegistry)
	path := filepath.Join(dir, ServersConfigFile)
	before := hashOf(t, path)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Skip("cannot deny reads here")
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err := LoadFromViper(v)
	if err == nil {
		t.Fatal("an unreadable registry loaded successfully")
	}
	if !errors.Is(err, ErrRegistryUnreadable) {
		t.Errorf("err = %v, want ErrRegistryUnreadable", err)
	}
	_ = os.Chmod(path, 0o600)
	if after := hashOf(t, path); after != before {
		t.Fatal("THE REGISTRY WAS MODIFIED after a read failure")
	}
}

// Acceptance 3. The one outcome that legitimately means "no managed servers".
func TestAbsentRegistryLoadsEmptyAndWritesNothing(t *testing.T) {
	dir, v := writeRegistry(t, "")
	path := filepath.Join(dir, ServersConfigFile)

	cfg, err := LoadFromViper(v)
	if err != nil {
		t.Fatalf("an absent registry failed the load: %v", err)
	}
	if len(cfg.ManagedServers) != 0 {
		t.Errorf("got %d servers from an absent registry", len(cfg.ManagedServers))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		body, _ := os.ReadFile(path)
		t.Fatalf("LOADING CREATED A REGISTRY. Creating one is `init`'s job.\n%.200s", body)
	}
}

// A valid registry is untouched, and is the only case that yields servers.
func TestValidRegistryLoadsAndIsUntouched(t *testing.T) {
	dir, v := writeRegistry(t, operatorRegistry)
	path := filepath.Join(dir, ServersConfigFile)
	before := hashOf(t, path)

	cfg, err := LoadFromViper(v)
	if err != nil {
		t.Fatalf("LoadFromViper: %v", err)
	}
	if len(cfg.ManagedServers) != 2 {
		t.Fatalf("got %d servers, want 2", len(cfg.ManagedServers))
	}
	if hashOf(t, path) != before {
		t.Fatal("a successful load modified the registry")
	}
}

// Acceptance 6. A completed write leaves .prev holding exactly the old bytes.
func TestSaveKeepsOneGenerationAndIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ServersConfigFile)
	if err := os.WriteFile(path, []byte(operatorRegistry), 0o600); err != nil {
		t.Fatal(err)
	}
	original := hashOf(t, path)

	if err := SaveManagedServersAt(path, []ServerConfig{
		{Name: "my-private-tool", Command: "/Users/me/bin/private-tool"},
		{Name: "team-scanner", Command: "/opt/team/scanner"},
		{Name: "added-later", Command: "/opt/new"},
	}); err != nil {
		t.Fatalf("SaveManagedServersAt: %v", err)
	}

	if got := hashOf(t, path+".prev"); got != original {
		t.Errorf(".prev does not hold the previous bytes")
	}
	after, err := LoadManagedServersAt(path)
	if err != nil {
		t.Fatalf("the saved registry does not parse: %v", err)
	}
	if len(after) != 3 {
		t.Errorf("saved %d servers, want 3", len(after))
	}
	// No temp files left behind.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".servers.yaml-") {
			t.Errorf("a temp file survived the write: %s", e.Name())
		}
	}
}

// Acceptance 4, as a scan: the helper that resolved its own path is gone, so a
// caller cannot write a directory it did not resolve.
func TestNoUnqualifiedSaveHelperExists(t *testing.T) {
	src, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "func SaveManagedServers(") {
		t.Error("SaveManagedServers(servers) is back. It resolves DefaultConfigDir() " +
			"while its callers resolve their own path, which is how a test wrote the " +
			"operator's real registry (MADR 0004 F2).")
	}
}
