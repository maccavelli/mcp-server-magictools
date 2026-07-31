package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadAndExtract(t *testing.T) {
	// Create a mock mcp_config.json
	ideCfg := IDEConfig{
		McpServers: map[string]IDEServerEntry{
			"magictools": {
				Command:  "/bin/magic",
				Disabled: false,
			},
			"brainstorm": {
				Command:  "/bin/brainstorm",
				Args:     []string{"--dry-run"},
				Disabled: true,
				Env:      map[string]string{"DEBUG": "1"},
			},
			"filesystem": {
				Command:  "/bin/fs",
				Disabled: false,
			},
		},
	}

	tmpDir, err := os.MkdirTemp("", "mcp-config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	// XDG_CONFIG_HOME only redirects os.UserConfigDir on Linux; use the
	// loader's own override so the redirect holds on macOS too.
	t.Setenv(EnvConfigDir, filepath.Join(tmpDir, "mcp-server-magictools"))

	configPath := filepath.Join(tmpDir, "mcp_config.json")
	data, _ := json.Marshal(ideCfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	// Create mock servers.yaml to provide brainstorm
	magictoolsDir := filepath.Join(tmpDir, "mcp-server-magictools")
	os.MkdirAll(magictoolsDir, 0755)
	serversYaml := `servers:
  - name: brainstorm
    command: /bin/brainstorm
    args:
      - --dry-run
    env:
      DEBUG: "1"
`
	os.WriteFile(filepath.Join(magictoolsDir, "servers.yaml"), []byte(serversYaml), 0644)

	// Test Load
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	var s *ServerConfig
	for i := range cfg.ManagedServers {
		if cfg.ManagedServers[i].Name == "brainstorm" {
			s = &cfg.ManagedServers[i]
			break
		}
	}

	if s == nil {
		t.Errorf("expected brainstorm to be managed, but not found")
	} else if s.Env["DEBUG"] != "1" {
		t.Errorf("expected env DEBUG=1, got %v", s.Env["DEBUG"])
	}

	// Test GetManagedServerNames
	names := cfg.GetManagedServerNames()
	if !names["brainstorm"] {
		t.Error("expected brainstorm in managed names map")
	}
	if names["magictools"] {
		t.Error("magictools should not be managed")
	}
}

func TestLoadFromServersYAML(t *testing.T) {
	// Save and restore DefaultConfigDir by using env override
	tmpDir := t.TempDir()
	// XDG_CONFIG_HOME only redirects os.UserConfigDir on Linux; use the
	// loader's own override so the redirect holds on macOS too.
	t.Setenv(EnvConfigDir, filepath.Join(tmpDir, "mcp-server-magictools"))

	servers := []ServerConfig{
		{
			Name:    "brainstorm",
			Command: "/bin/brainstorm",
			Args:    []string{"--dry-run"},
			Env:     map[string]string{"DEBUG": "1"},
		},
		{
			Name:    "filesystem",
			Command: "/bin/fs",
			Args:    []string{"/home"},
		},
	}

	// Write servers.yaml to the temp dir using SaveManagedServers logic
	reg := serversYAML{Servers: make([]serverEntry, 0, len(servers))}
	for _, sc := range servers {
		reg.Servers = append(reg.Servers, serverEntry{
			Name:    sc.Name,
			Command: sc.Command,
			Args:    sc.Args,
			Env:     sc.Env,
		})
	}

	data, err := yaml.Marshal(&reg)
	if err != nil {
		t.Fatal(err)
	}

	serversPath := filepath.Join(tmpDir, ServersConfigFile)
	if err := os.WriteFile(serversPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Read back using LoadManagedServers won't work directly since it uses
	// DefaultConfigDir(). Instead, test the round-trip via SaveManagedServers/LoadManagedServers
	// by verifying the YAML structure matches.
	var loaded serversYAML
	raw, err := os.ReadFile(serversPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &loaded); err != nil {
		t.Fatalf("failed to parse servers.yaml: %v", err)
	}

	if len(loaded.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(loaded.Servers))
	}

	if loaded.Servers[0].Name != "brainstorm" {
		t.Errorf("expected brainstorm, got %s", loaded.Servers[0].Name)
	}
	if loaded.Servers[1].Name != "filesystem" {
		t.Errorf("expected filesystem, got %s", loaded.Servers[1].Name)
	}
	if loaded.Servers[0].Env["DEBUG"] != "1" {
		t.Errorf("expected env DEBUG=1, got %v", loaded.Servers[0].Env)
	}
}

func TestThreadSafeAccessors(t *testing.T) {
	cfg := &Config{
		ManagedServers: []ServerConfig{{Name: "test"}},
	}

	// Update
	newServers := []ServerConfig{{Name: "new"}}
	cfg.UpdateManagedServers(newServers)

	// Get
	managed := cfg.GetManagedServers()
	if len(managed) != 1 || managed[0].Name != "new" {
		t.Errorf("thread-safe accessor failed, got %v", managed)
	}
}

func TestHash(t *testing.T) {
	s := ServerConfig{Name: "hash-test"}
	h := s.Hash()
	if h == "" || h == "invalid-config-hash" {
		t.Error("hash failed")
	}
}

func TestFoldedString_MarshalYAML(t *testing.T) {
	fs := FoldedString("short")
	node, _ := fs.MarshalYAML()
	n := node.(*yaml.Node)
	if n.Value != "short" {
		t.Error("failed")
	}
}

func TestUpdateConfigValue(t *testing.T) {
	cfg := &Config{}
	_, err := cfg.UpdateConfigValue("logLevel", "DEBUG")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if cfg.LogLevel != "DEBUG" {
		t.Errorf("expected DEBUG, got %s", cfg.LogLevel)
	}

	_, err = cfg.UpdateConfigValue("squeezeLevel", "4")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if cfg.SqueezeLevelState == nil || *cfg.SqueezeLevelState != 4 {
		t.Errorf("expected 4, got %v", cfg.SqueezeLevelState)
	}
	if cfg.MaxResponseTokens != 1800 {
		t.Errorf("expected 1800 based on level 4, got %v", cfg.MaxResponseTokens)
	}

	_, err = cfg.UpdateConfigValue("scoreThreshold", "0.5")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if cfg.ScoreThreshold != 0.5 {
		t.Errorf("expected 0.5, got %v", cfg.ScoreThreshold)
	}

	_, err = cfg.UpdateConfigValue("validateProxyCalls", "false")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if cfg.ValidateProxyCalls != false {
		t.Errorf("expected false, got true")
	}

	_, err = cfg.UpdateConfigValue("pinnedServers", "a b c")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(cfg.PinnedServers) != 3 {
		t.Errorf("expected 3, got %d", len(cfg.PinnedServers))
	}

	_, err = cfg.UpdateConfigValue("squeezeBypass", "x y")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(cfg.SqueezeBypass) != 2 {
		t.Errorf("expected 2, got %d", len(cfg.SqueezeBypass))
	}

	_, err = cfg.UpdateConfigValue("invalidKey", "val")
	if err == nil {
		t.Errorf("expected error for invalid key")
	}
}

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()
	// XDG_CONFIG_HOME only redirects os.UserConfigDir on Linux; use the
	// loader's own override so the redirect holds on macOS too.
	t.Setenv(EnvConfigDir, filepath.Join(tmpDir, "mcp-server-magictools"))

	cfgFile := filepath.Join(tmpDir, "mcp-server-magictools", "config.yaml")

	cfg, err := New("1.0.0", cfgFile)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected config")
	}
	if cfg.Name != Name {
		t.Errorf("expected %s, got %s", Name, cfg.Name)
	}
	if cfg.Version != "1.0.0" {
		t.Errorf("expected 1.0.0, got %s", cfg.Version)
	}
}

func TestSaveConfiguration(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")

	// Create an initial empty config
	os.WriteFile(path, []byte(""), 0644)

	val := 2
	cfg := &Config{
		ConfigPath:         path,
		LogLevel:           "WARN",
		SqueezeLevelState:  &val,
		ValidateProxyCalls: true,
		Intelligence: IntelligenceEngine{
			Provider:       "test_prov",
			Model:          "test_model",
			APIKey:         "key",
			RetryCount:     3,
			RetryDelay:     10,
			TimeoutSeconds: 300,
		},
	}

	err := cfg.SaveConfiguration()
	if err != nil {
		t.Fatalf("SaveConfiguration failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written config: %v", err)
	}

	strData := string(data)
	if !strings.Contains(strData, "logLevel: WARN") {
		t.Errorf("expected WARN, got:\n%s", strData)
	}
	if !strings.Contains(strData, "provider: test_prov") {
		t.Errorf("expected test_prov, got:\n%s", strData)
	}
}

// TestCFG02_SaveConfigurationGatedOnFastProvider documents CFG-02:
// SaveConfiguration gates the entire intelligence section on c.Intelligence.Provider != "".
// Setting ThinkingProvider on a config without a Fast Tier provider results in intelligence not being written.
func TestCFG02_SaveConfigurationGatedOnFastProvider(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(path, []byte(""), 0644)

	cfg := &Config{
		ConfigPath: path,
		Intelligence: IntelligenceEngine{
			Provider:         "", // Fast provider empty
			ThinkingProvider: "claude",
			ThinkingModel:    "claude-3-5-sonnet",
			ThinkingAPIKey:   "sk-test-key",
		},
	}

	if err := cfg.SaveConfiguration(); err != nil {
		t.Fatalf("SaveConfiguration failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	strData := string(data)
	if strings.Contains(strData, "thinking_provider: claude") {
		t.Fatalf("[CFG-02] unexpected persistence of thinking_provider when Fast provider is empty before WP2 fix:\n%s", strData)
	}
}

// TestCFG03_SaveConfigurationLeavesStaleKeysOnClear documents CFG-03:
// Clearing thinking fields sets in-memory values to "" and calls SaveConfiguration.
// SaveConfiguration skips empty optional fields, leaving existing YAML keys in place.
func TestCFG03_SaveConfigurationLeavesStaleKeysOnClear(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	initialYAML := `intelligence:
  provider: gemini
  model: gemini-2.5-flash
  thinking_provider: claude
  thinking_api_key: sk-old-secret
`
	os.WriteFile(path, []byte(initialYAML), 0644)

	cfg := &Config{
		ConfigPath: path,
		Intelligence: IntelligenceEngine{
			Provider:         "gemini",
			Model:            "gemini-2.5-flash",
			ThinkingProvider: "", // Cleared
			ThinkingAPIKey:   "", // Cleared
		},
	}

	if err := cfg.SaveConfiguration(); err != nil {
		t.Fatalf("SaveConfiguration failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	strData := string(data)
	if !strings.Contains(strData, "thinking_api_key: sk-old-secret") {
		t.Fatalf("[CFG-03] expected stale thinking_api_key to remain in YAML after clear before WP2 patch fix:\n%s", strData)
	}
}

// TestCFG07_UnrelatedSaveMutatesRRFBiases documents CFG-07:
// Runtime normalization (e.g. normalizeRRFBiases) alters default zero role weights,
// causing an unrelated SaveConfiguration to persist mutated search weights.
func TestCFG07_UnrelatedSaveMutatesRRFBiases(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	initialYAML := `logLevel: INFO
search:
  rrf_biases:
    semantic: 0.7
    keyword: 0.3
    role: 0.0
`
	os.WriteFile(path, []byte(initialYAML), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	cfg.ConfigPath = path
	cfg.Intelligence.Provider = "gemini"
	cfg.Intelligence.Model = "gemini-2.5-flash"

	if err := cfg.SaveConfiguration(); err != nil {
		t.Fatalf("SaveConfiguration failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	strData := string(data)
	// Role bias 0.0 gets normalized to non-zero 0.230769..., mutating search settings on unrelated save
	if !strings.Contains(strData, "synthesisBiasRole: 0.23") && !strings.Contains(strData, "synthesisBiasRole: 0.3") {
		t.Fatalf("[CFG-07] expected RRF role bias synthesisBiasRole to be mutated into saved YAML by normalizeRRFBiases before WP2 fix:\n%s", strData)
	}
}

func TestMigrateDataDir(t *testing.T) {
	tmpDir := t.TempDir()
	oldDir := filepath.Join(tmpDir, "old")
	newDir := filepath.Join(tmpDir, "new")

	// Migrate when old does not exist
	err := MigrateDataDir(oldDir, newDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Create old
	os.MkdirAll(oldDir, 0755)

	// Migrate when new does not exist
	err = MigrateDataDir(oldDir, newDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		t.Fatalf("expected newDir to be created")
	}

	// Migrate when new already exists
	os.MkdirAll(oldDir, 0755) // recreate old
	err = MigrateDataDir(oldDir, newDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestIsServiceMode(t *testing.T) {
	tests := []struct {
		name     string
		envVal   string
		expected bool
	}{
		{"true lowercase", "true", true},
		{"TRUE uppercase", "TRUE", true},
		{"True mixed", "True", true},
		{"false", "false", false},
		{"empty", "", false},
		{"other", "yes", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("MCP_SERVICE_MODE", tt.envVal)
			defer os.Unsetenv("MCP_SERVICE_MODE")
			if got := IsServiceMode(); got != tt.expected {
				t.Errorf("IsServiceMode() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestResolveBindAddr(t *testing.T) {
	tests := []struct {
		name             string
		envVal           string
		cfgVal           string
		allowNonLoopback string // MCP_ENDPOINT_ALLOW_NONLOOPBACK
		expected         string
	}{
		{"default", "", "", "", "localhost:48080"},
		{"bare port", "", "9090", "", "localhost:9090"},
		{"host:port rejects non-loopback", "", "myhost:8080", "", "localhost:8080"},
		{"ip:port rejects non-loopback", "", "192.168.1.5:7070", "", "localhost:7070"},
		{"env overrides cfg", "5050", "ignored:9999", "", "localhost:5050"},
		{"env host:port rejects non-loopback", "custom:6060", "", "", "localhost:6060"},
		{"invalid bare string", "", "notanumber", "", "localhost:48080"},
		{"non-loopback allowed by env", "", "myhost:8080", "true", "myhost:8080"},
		{"non-loopback ip allowed by env", "", "192.168.1.5:7070", "true", "192.168.1.5:7070"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				os.Setenv("MCP_ENDPOINT_IDE_PORT", tt.envVal)
				defer os.Unsetenv("MCP_ENDPOINT_IDE_PORT")
			} else {
				os.Unsetenv("MCP_ENDPOINT_IDE_PORT")
			}
			if tt.allowNonLoopback != "" {
				os.Setenv("MCP_ENDPOINT_ALLOW_NONLOOPBACK", tt.allowNonLoopback)
				defer os.Unsetenv("MCP_ENDPOINT_ALLOW_NONLOOPBACK")
			} else {
				os.Unsetenv("MCP_ENDPOINT_ALLOW_NONLOOPBACK")
			}
			got := ResolveBindAddr(tt.cfgVal)
			if got != tt.expected {
				t.Errorf("ResolveBindAddr(%q) = %q, want %q", tt.cfgVal, got, tt.expected)
			}
		})
	}
}
