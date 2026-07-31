// Package config provides functionality for the config subsystem.
package config

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const (
	Name              = "mcp-server-magictools"
	AppName           = "mcp-server-magictools"
	LegacyDBPath      = ".mcp_magictools" // Pre-migration dot-directory in $HOME
	EnvDBPath         = "MCP_MAGIC_TOOLS_DB_PATH"
	EnvConfigPath     = "MCP_MAGIC_TOOLS_CONFIG"
	EnvConfigDir      = "MCP_MAGIC_TOOLS_CONFIG_DIR"
	SelfName          = "magictools"
	ToolConfigFile    = "config.yaml"
	ServersConfigFile = "servers.yaml"
)

// ResolveRecallURL returns the canonicalized MCP_REC_URL environment variable.
func ResolveRecallURL() string {
	val := os.Getenv("MCP_REC_URL")
	if val == "" {
		return ""
	}
	return strings.TrimSpace(val)
}

// ResolveSocraticURL returns the canonicalized MCP_SOC_URL environment variable.
func ResolveSocraticURL() string {
	val := os.Getenv("MCP_SOC_URL")
	if val == "" {
		return ""
	}
	return strings.TrimSpace(val)
}

// IsServiceMode returns true when MCP_SERVICE_MODE=true, enabling HTTP transport
// instead of the default stdio transport. This toggles the orchestrator between
// IDE-managed child process mode (stdio) and standalone background service mode (HTTP).
func IsServiceMode() bool {
	return strings.EqualFold(os.Getenv("MCP_SERVICE_MODE"), "true")
}

// isLoopbackHost returns true if the host component is a loopback address.
func isLoopbackHost(host string) bool {
	return host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// ResolveBindAddr returns the resolved bind address from MCP_ENDPOINT_IDE_PORT.
// Priority: env var > cfgAddr (from server config) > default ("localhost:48080")
// Accepts: bare port ("48080"), host:port ("localhost:48080"), ip:port ("127.0.0.1:48080")
//
// Security: If a host:port value resolves to a non-loopback address, a warning is logged
// and the address is overridden to localhost:<port> to prevent accidental public exposure.
// Set MCP_ENDPOINT_ALLOW_NONLOOPBACK=true to intentionally bind on a non-loopback interface.
func ResolveBindAddr(cfgAddr string) string {
	// 1. Env var takes highest priority
	val := os.Getenv("MCP_ENDPOINT_IDE_PORT")
	// 2. Fall back to server config value
	if val == "" {
		val = cfgAddr
	}
	// 3. Fall back to hard-coded default
	if val == "" {
		return "localhost:48080"
	}
	// Pure numeric = port only, always loopback-safe
	if _, err := strconv.Atoi(val); err == nil {
		return "localhost:" + val
	}
	// host:port form — validate the host component
	if strings.Contains(val, ":") {
		host, port, err := splitHostPort(val)
		if err != nil {
			// Malformed — fall back to default
			slog.Warn("config: MCP_ENDPOINT_IDE_PORT is malformed, using default", "value", val)
			return "localhost:48080"
		}
		if !isLoopbackHost(host) {
			if strings.EqualFold(os.Getenv("MCP_ENDPOINT_ALLOW_NONLOOPBACK"), "true") {
				slog.Warn("config: binding HTTP service on non-loopback interface (MCP_ENDPOINT_ALLOW_NONLOOPBACK=true)",
					"addr", val)
				return val
			}
			slog.Warn("config: non-loopback bind address rejected for safety; overriding to localhost. "+
				"Set MCP_ENDPOINT_ALLOW_NONLOOPBACK=true to allow.",
				"requested", val, "effective", "localhost:"+port)
			return "localhost:" + port
		}
		return val
	}
	// Fallback for invalid input
	return "localhost:48080"
}

// ResolveLLMBindAddr returns the LLM listener bind address.
// Priority: env var (MCP_ENDPOINT_LLM_PORT) > cfgAddr (from config.llm_port) > default ("127.0.0.1:48081")
// Same loopback safety as ResolveBindAddr.
func ResolveLLMBindAddr(cfgAddr string) string {
	val := os.Getenv("MCP_ENDPOINT_LLM_PORT")
	if val == "" {
		val = cfgAddr
	}
	if val == "" {
		return "127.0.0.1:48081"
	}
	// Pure numeric = port only, always loopback-safe
	if _, err := strconv.Atoi(val); err == nil {
		return "127.0.0.1:" + val
	}
	// host:port form — validate the host component
	if strings.Contains(val, ":") {
		host, port, err := splitHostPort(val)
		if err != nil {
			slog.Warn("config: MCP_ENDPOINT_LLM_PORT is malformed, using default", "value", val)
			return "127.0.0.1:48081"
		}
		if !isLoopbackHost(host) {
			if strings.EqualFold(os.Getenv("MCP_ENDPOINT_ALLOW_NONLOOPBACK"), "true") {
				slog.Warn("config: binding LLM backplane on non-loopback interface (MCP_ENDPOINT_ALLOW_NONLOOPBACK=true)",
					"addr", val)
				return val
			}
			slog.Warn("config: non-loopback LLM bind address rejected for safety; overriding to 127.0.0.1",
				"requested", val, "effective", "127.0.0.1:"+port)
			return "127.0.0.1:" + port
		}
		return val
	}
	return "127.0.0.1:48081"
}

func splitHostPort(addr string) (host, port string, err error) {
	// Use net.SplitHostPort semantics manually to avoid importing net in config pkg.
	if len(addr) == 0 {
		return "", "", fmt.Errorf("empty address")
	}
	if addr[0] == '[' {
		// IPv6 bracketed: [::1]:port
		end := strings.LastIndex(addr, "]")
		if end < 0 {
			return "", "", fmt.Errorf("missing ']' in address")
		}
		host = addr[1:end]
		rest := addr[end+1:]
		if len(rest) == 0 || rest[0] != ':' {
			return "", "", fmt.Errorf("missing port in address")
		}
		port = rest[1:]
		return host, port, nil
	}
	colon := strings.LastIndex(addr, ":")
	if colon < 0 {
		return "", "", fmt.Errorf("missing port in address")
	}
	return addr[:colon], addr[colon+1:], nil
}

// DefaultConfigDir returns the platform-native configuration directory.
//   - Linux: ~/.config/mcp-server-magictools
//   - macOS: ~/Library/Application Support/mcp-server-magictools
func DefaultConfigDir() string {
	if dir := os.Getenv(EnvConfigDir); dir != "" {
		return dir
	}
	base, err := os.UserConfigDir()
	if err != nil {
		// Fallback: behaves identically to the legacy path on Linux
		if home, herr := os.UserHomeDir(); herr == nil {
			return filepath.Join(home, ".config", AppName)
		}
		return filepath.Join(os.TempDir(), AppName)
	}
	return filepath.Join(base, AppName)
}

// DefaultCacheDir returns the platform-native cache directory.
//   - Linux: ~/.cache/mcp-server-magictools
//   - macOS: ~/Library/Caches/mcp-server-magictools
func DefaultCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		if home, herr := os.UserHomeDir(); herr == nil {
			return filepath.Join(home, ".cache", AppName)
		}
		return filepath.Join(os.TempDir(), AppName)
	}
	return filepath.Join(base, AppName)
}

// DefaultLogPath returns the platform-native log file path.
//   - Linux: ~/.cache/mcp-server-magictools/magictools_debug.log
//   - macOS: ~/Library/Caches/mcp-server-magictools/magictools_debug.log
func DefaultLogPath() string {
	return filepath.Join(DefaultCacheDir(), "magictools_debug.log")
}

// DefaultDataPath returns the platform-native data directory for BadgerDB.
//   - Linux: ~/.config/mcp-server-magictools/db
//   - macOS: ~/Library/Application Support/mcp-server-magictools/db
func DefaultDataPath() string {
	return filepath.Join(DefaultConfigDir(), "db")
}

// MigrateDataDir performs a one-time atomic migration of data from oldPath to newPath.
// It is a no-op if oldPath doesn't exist or newPath already exists.
func MigrateDataDir(oldPath, newPath string) error {
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return nil // nothing to migrate
	}
	if _, err := os.Stat(newPath); err == nil {
		return nil // new path already exists, skip
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return fmt.Errorf("migration: failed to create parent dir: %w", err)
	}
	slog.Info("migrating data directory", "from", oldPath, "to", newPath)
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("migration: failed to move data: %w", err)
	}
	slog.Info("migration complete", "new_path", newPath)
	return nil
}

// IntelligenceEngine holds configuration for both the generative hydrator and the embedding engine.
type IntelligenceEngine struct {
	// Generative Hydrator settings (Fast tier)
	Provider       string   `json:"provider,omitempty,omitzero" mapstructure:"provider" yaml:"provider,omitempty"`
	Model          string   `json:"model,omitempty,omitzero" mapstructure:"model" yaml:"model,omitempty"`
	APIKey         string   `json:"api_key,omitempty,omitzero" mapstructure:"api_key" yaml:"api_key,omitempty"`
	APIURL         string   `json:"api_url,omitempty,omitzero" mapstructure:"api_url" yaml:"api_url,omitempty"`
	FallbackModels []string `json:"fallback_models,omitempty,omitzero" mapstructure:"fallback_models" yaml:"fallback_models,omitempty"`
	RetryCount     int      `json:"retry_count,omitempty,omitzero" mapstructure:"retry_count" yaml:"retry_count,omitempty"`
	RetryDelay     int      `json:"retry_delay_seconds,omitempty,omitzero" mapstructure:"retry_delay_seconds" yaml:"retry_delay_seconds,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty,omitzero" mapstructure:"timeout_seconds" yaml:"timeout_seconds,omitempty"`

	// Thinking tier — INDEPENDENT provider, NO defaulting from fast tier
	ThinkingProvider string `json:"thinking_provider,omitempty,omitzero" mapstructure:"thinking_provider" yaml:"thinking_provider,omitempty"`
	ThinkingModel    string `json:"thinking_model,omitempty,omitzero" mapstructure:"thinking_model" yaml:"thinking_model,omitempty"`
	ThinkingAPIKey   string `json:"thinking_api_key,omitempty,omitzero" mapstructure:"thinking_api_key" yaml:"thinking_api_key,omitempty"`

	// Shared LLM Backplane controls
	SharedLLMEnabled  bool `json:"shared_llm_enabled,omitempty,omitzero" mapstructure:"shared_llm_enabled" yaml:"shared_llm_enabled,omitempty"`
	LLMPort           int  `json:"llm_port,omitempty,omitzero" mapstructure:"llm_port" yaml:"llm_port,omitempty"`
	MaxConcurrent     int  `json:"max_concurrent_requests,omitempty,omitzero" mapstructure:"max_concurrent_requests" yaml:"max_concurrent_requests,omitempty"`
	MaxRPM            int  `json:"max_rpm,omitempty,omitzero" mapstructure:"max_rpm" yaml:"max_rpm,omitempty"`
	MaxBurstPerSecond int  `json:"max_burst_per_second,omitempty,omitzero" mapstructure:"max_burst_per_second" yaml:"max_burst_per_second,omitempty"`
	SubServerTokenMax int  `json:"sub_server_token_thresh,omitempty,omitzero" mapstructure:"sub_server_token_thresh" yaml:"sub_server_token_thresh,omitempty"`

	// HFSC optimization
	OrphanStreamTTL int `json:"orphan_stream_ttl_minutes,omitempty,omitzero" mapstructure:"orphan_stream_ttl_minutes" yaml:"orphan_stream_ttl_minutes,omitempty"`

	// Embedding Engine settings (decoupled from generative hydrator)
	EmbeddingProvider       string `json:"embedding_provider,omitempty,omitzero" mapstructure:"embedding_provider" yaml:"embedding_provider,omitempty"`
	EmbeddingModel          string `json:"embedding_model,omitempty,omitzero" mapstructure:"embedding_model" yaml:"embedding_model,omitempty"`
	EmbeddingAPIKey         string `json:"embedding_api_key,omitempty,omitzero" mapstructure:"embedding_api_key" yaml:"embedding_api_key,omitempty"`
	EmbeddingAPIURL         string `json:"embedding_api_url,omitempty,omitzero" mapstructure:"embedding_api_url" yaml:"embedding_api_url,omitempty"`
	EmbeddingDimensionality int    `json:"embedding_dimensionality,omitempty,omitzero" mapstructure:"embedding_dimensionality" yaml:"embedding_dimensionality,omitempty"`
	VectorEnabled           bool   `json:"vector_enabled,omitempty,omitzero" mapstructure:"vector_enabled" yaml:"vector_enabled,omitempty"`
}

// ConfigurationBlock is undocumented but satisfies standard structural requirements.
type ConfigurationBlock struct {
	MaxResponseTokens     int                `json:"maxResponseTokens,omitempty,omitzero" mapstructure:"maxResponseTokens" yaml:"maxResponseTokens,omitempty"`
	SqueezeLevel          *int               `json:"squeezeLevel,omitempty,omitzero" mapstructure:"squeezeLevel" yaml:"squeezeLevel,omitempty"`
	LogFormat             string             `json:"logFormat,omitempty,omitzero" mapstructure:"logFormat" yaml:"logFormat,omitempty"`
	LogLevel              string             `json:"logLevel,omitempty,omitzero" mapstructure:"logLevel" yaml:"logLevel,omitempty"`
	MCPLogLevel           string             `json:"mcpLogLevel,omitempty,omitzero" mapstructure:"mcpLogLevel" yaml:"mcpLogLevel,omitempty"`
	ScoreThreshold        float64            `json:"scoreThreshold,omitempty,omitzero" mapstructure:"scoreThreshold" yaml:"scoreThreshold,omitempty"`
	StrictGates           bool               `json:"strictGates,omitempty,omitzero" mapstructure:"strictGates" yaml:"strictGates,omitempty"`
	VectorMinCosine       float64            `json:"vectorMinCosine,omitempty,omitzero" mapstructure:"vectorMinCosine" yaml:"vectorMinCosine,omitempty"`
	BM25MinNormalized     float64            `json:"bm25MinNormalized,omitempty,omitzero" mapstructure:"bm25MinNormalized" yaml:"bm25MinNormalized,omitempty"`
	DisableSearchFallback bool               `json:"disableSearchFallback,omitempty,omitzero" mapstructure:"disableSearchFallback" yaml:"disableSearchFallback,omitempty"`
	ValidateProxyCalls    *bool              `json:"validateProxyCalls,omitempty,omitzero" mapstructure:"validateProxyCalls" yaml:"validateProxyCalls,omitempty"`
	SqueezeBypass         []string           `json:"squeezeBypass,omitempty,omitzero" mapstructure:"squeezeBypass" yaml:"squeezeBypass,omitempty"`
	RingBufferTargets     []string           `json:"ringBufferTargets,omitempty,omitzero" mapstructure:"ringBufferTargets" yaml:"ringBufferTargets,omitempty"`
	PinnedServers         []string           `json:"pinnedServers,omitempty,omitzero" mapstructure:"pinnedServers" yaml:"pinnedServers,omitempty"`
	TrustServers          []string           `json:"trustServers,omitempty,omitzero" mapstructure:"trustServers" yaml:"trustServers,omitempty"`
	TokenSpendThresh      int                `json:"tokenSpendThresh,omitempty,omitzero" mapstructure:"tokenSpendThresh" yaml:"tokenSpendThresh,omitempty"`
	LRULimit              int                `json:"lruLimit,omitempty,omitzero" mapstructure:"lruLimit" yaml:"lruLimit,omitempty"`
	SynthesisBiasVector   float64            `json:"synthesisBiasVector,omitempty,omitzero" mapstructure:"synthesisBiasVector" yaml:"synthesisBiasVector,omitempty"`
	SynthesisBiasSynergy  float64            `json:"synthesisBiasSynergy,omitempty,omitzero" mapstructure:"synthesisBiasSynergy" yaml:"synthesisBiasSynergy,omitempty"`
	SynthesisBiasRole     float64            `json:"synthesisBiasRole,omitempty,omitzero" mapstructure:"synthesisBiasRole" yaml:"synthesisBiasRole,omitempty"`
	ScoreFusionAlpha      float64            `json:"scoreFusionAlpha,omitempty,omitzero" mapstructure:"scoreFusionAlpha" yaml:"scoreFusionAlpha,omitempty"`
	CorroborationBonus    float64            `json:"corroborationBonus,omitempty,omitzero" mapstructure:"corroborationBonus" yaml:"corroborationBonus,omitempty"`
	ReliabilityBoost      float64            `json:"reliabilityBoost,omitempty,omitzero" mapstructure:"reliabilityBoost" yaml:"reliabilityBoost,omitempty"`
	UsageBoost            float64            `json:"usageBoost,omitempty,omitzero" mapstructure:"usageBoost" yaml:"usageBoost,omitempty"`
	NativeBoost           float64            `json:"nativeBoost,omitempty,omitzero" mapstructure:"nativeBoost" yaml:"nativeBoost,omitempty"`
	ConfidenceGap         float64            `json:"confidenceGap,omitempty,omitzero" mapstructure:"confidenceGap" yaml:"confidenceGap,omitempty"`
	Intelligence          IntelligenceEngine `json:"intelligence,omitzero" mapstructure:"intelligence" yaml:"intelligence,omitempty"`
}

// IDEConfig matches the IDE's mcp_config.json top-level structure
type IDEConfig struct {
	McpServers    map[string]IDEServerEntry `json:"mcpServers" mapstructure:"mcpServers"`
	Configuration ConfigurationBlock        `json:"configuration,omitzero" mapstructure:"configuration"`
}

// IDEServerEntry matches a single server entry in the IDE config
type IDEServerEntry struct {
	Command       string            `json:"command" mapstructure:"command"`
	Args          []string          `json:"args" mapstructure:"args"`
	Env           map[string]string `json:"env,omitempty,omitzero" mapstructure:"env"`
	Disabled      bool              `json:"disabled" mapstructure:"disabled"`
	DisabledTools []string          `json:"disabledTools,omitempty,omitzero" mapstructure:"disabledTools"`
	LogLevel      string            `json:"logLevel,omitempty,omitzero" mapstructure:"logLevel"`
}

// ServerConfig defines a magictools-managed sub-server (derived from IDE entries with disabled: true)
type ServerConfig struct {
	Name          string
	Command       string
	Args          []string
	Env           map[string]string
	DisabledTools []string
	MemoryLimitMB int
	GoMemLimitMB  int
	MaxCPULimit   int
	DeferredBoot  bool
	Disabled      bool
}

// Configuration options shared with SaveInternalTools
type PersistentConfig struct {
	SqueezeLevel *int
}

// Hash returns a stable hash of the server configuration
func (s ServerConfig) Hash() string {
	data, err := json.Marshal(s)
	if err != nil {
		return "invalid-config-hash"
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// ToolOverride defines a manual override for a specific tool.
type ToolOverride struct {
	Description string `mapstructure:"description" yaml:"description"`
}

// ToolOverridesConfig holds the loaded overrides from tool_overrides.yaml.
type ToolOverridesConfig struct {
	// Map of ServerName -> ToolName -> Override
	Overrides map[string]map[string]ToolOverride `mapstructure:"overrides" yaml:"overrides"`
}

// Config holds the application configuration
type Config struct {
	mu sync.RWMutex

	Name                  string
	Version               string
	DBPath                string
	ConfigPath            string
	Paths                 Paths
	ManagedServers        []ServerConfig
	ideConfig             *IDEConfig // raw parsed config for diffing
	MaxResponseTokens     int
	LogPath               string
	LogFormat             string // format style: json or text
	NoOptimize            bool   // Flag to disable SqueezeWriter and description minification
	Debug                 bool   // Flag for high-frequency trace logging and foreground mode
	LogLevel              string // Configured log level (ERROR, WARN, INFO, DEBUG, TRACE)
	MCPLogLevel           string // Configured sub-server log level
	SqueezeLevelState     *int   // Retained so SaveInternalTools doesn't wipe it
	ScoreThreshold        float64
	StrictGates           bool
	VectorMinCosine       float64
	BM25MinNormalized     float64
	DisableSearchFallback bool
	ValidateProxyCalls    bool                // Toggle JSON schema validation in call_proxy (default: true)
	SqueezeBypass         []string            // Array of server names allowed to bypass the minifier
	RingBufferTargets     []string            // Array of specific targets (server:tool or server) routed to CSSA Ring Buffer natively
	PinnedServers         []string            // Servers exempt from idle eviction and LRU eviction
	TrustServers          []string            // Servers authorized for inline auto-execution in align_tools
	TokenSpendThresh      int                 // Session LLM token circuit-breaker limit
	LRULimit              int                 // Max items for ResponseCache and RegistryCache
	SynthesisBiasVector   float64             // Engine weight in tri-factor scoring
	SynthesisBiasSynergy  float64             // Synergy (ghost index) weight in tri-factor scoring
	SynthesisBiasRole     float64             // Role boost weight in tri-factor scoring
	ScoreFusionAlpha      float64             // Vector weight in direct score fusion (0.0=pure BM25, 1.0=pure vector)
	CorroborationBonus    float64             // Dual-leg match bonus added during score fusion (default 0.05)
	ReliabilityBoost      float64             // Post-fusion multiplier on (ProxyReliability-1.0) (default 0.15)
	UsageBoost            float64             // Post-fusion multiplier on log1p(UsageCount) (default 0.02)
	NativeBoost           float64             // Post-fusion additive boost for native tools (default 0.10)
	ConfidenceGap         float64             // Minimum score gap between #1 and #2 for inline execution in align_tools
	VectorEnabled         bool                // Global toggle for HNSW Vector Engine
	SessionLRUSize        int                 // Max tools in session LRU for System Prompt injection (default: 30)
	PRFConfidenceSkip     float64             // Skip PRF when top result confidence exceeds this (default: 0.7, 0=always PRF)
	AlignMaxResults       int                 // Max align_tools results (default: 5)
	Intelligence          IntelligenceEngine  // Native HTTP configuration for the Hydrator daemon
	ToolOverrides         ToolOverridesConfig // Overrides loaded from tool_overrides.yaml
	v                     *viper.Viper
}

// GetManagedServers returns a copy of the managed servers slice thread-safely
func (c *Config) GetManagedServers() []ServerConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	res := make([]ServerConfig, len(c.ManagedServers))
	copy(res, c.ManagedServers)
	return res
}

// GetLogFormat returns the current orchestrator log format thread-safely
func (c *Config) GetLogFormat() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.LogFormat == "" {
		return "json" // Default to JSON strict constraint
	}
	return c.LogFormat
}

// GetLogLevel returns the current orchestrator log level thread-safely
func (c *Config) GetLogLevel() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.LogLevel
}

// GetMCPLogLevel returns the current sub-server log level thread-safely
func (c *Config) GetMCPLogLevel() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.MCPLogLevel == "" {
		return c.LogLevel
	}
	return c.MCPLogLevel
}

// GetSqueezeBypass returns a copy of the minifier bypass slice thread-safely
func (c *Config) GetSqueezeBypass() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	res := make([]string, len(c.SqueezeBypass))
	copy(res, c.SqueezeBypass)
	return res
}

// GetRingBufferTargets returns a copy of the ring buffer targets slice thread-safely
func (c *Config) GetRingBufferTargets() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	res := make([]string, len(c.RingBufferTargets))
	copy(res, c.RingBufferTargets)
	return res
}

// GetPinnedServers returns a copy of the pinned servers slice thread-safely
func (c *Config) GetPinnedServers() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	res := make([]string, len(c.PinnedServers))
	copy(res, c.PinnedServers)
	return res
}

// GetTrustServers returns a copy of the trust servers slice thread-safely.
// Trust servers are authorized for inline auto-execution in align_tools.
func (c *Config) GetTrustServers() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	res := make([]string, len(c.TrustServers))
	copy(res, c.TrustServers)
	return res
}

// UpdateManagedServers updates the managed servers slice thread-safely
func (c *Config) UpdateManagedServers(servers []ServerConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ManagedServers = servers
}

// GetToolOverride returns the overridden description for a tool, or an empty string if none exists.
func (c *Config) GetToolOverride(serverName, toolName string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ToolOverrides.Overrides == nil {
		return ""
	}
	serverOverrides, ok := c.ToolOverrides.Overrides[serverName]
	if !ok {
		return ""
	}
	override, ok := serverOverrides[toolName]
	if !ok {
		return ""
	}
	return override.Description
}

// UpdateToolOverrides updates the tool overrides configuration thread-safely.
func (c *Config) UpdateToolOverrides(overrides ToolOverridesConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ToolOverrides = overrides
}

// New initializes configuration with path discovery: flag -> env -> default
func New(version, flagPath string) (*Config, error) {
	dbPath := os.Getenv(EnvDBPath)
	if dbPath == "" {
		dbPath = DefaultDataPath()

		// 🛡️ AUTO-MIGRATION: Move legacy ~/.mcp_magictools to the new platform-native location.
		if home, err := os.UserHomeDir(); err == nil {
			oldPath := filepath.Join(home, LegacyDBPath)
			if err := MigrateDataDir(oldPath, dbPath); err != nil {
				slog.Warn("config: legacy data migration failed", "error", err)
			}
		}
	}

	resolvedPaths, err := ResolvePaths(flagPath)
	if err != nil {
		return nil, err
	}
	configPath := resolvedPaths.Config

	// 🛡️ VIPER INITIALIZATION: Set up Viper to manage the primary config file
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetEnvPrefix("MAGIC_TOOLS")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		// Only auto-create defaults when the file is genuinely absent; surface
		// real parse/permission errors on an existing config instead of silently
		// falling back to defaults.
		if !errors.As(err, &configFileNotFoundError) && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
		// Auto-create default configuration if missing
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			slog.Warn("config: failed to create config directory", "path", configPath, "error", err)
		}

		v.SetDefault("configuration.squeezeLevel", 3)
		v.SetDefault("configuration.logLevel", "DEBUG")
		v.SetDefault("configuration.mcpLogLevel", "INFO")
		v.SetDefault("configuration.logFormat", "json")
		v.SetDefault("configuration.validateProxyCalls", true)
		v.SetDefault("configuration.scoreThreshold", 0.3)
		v.SetDefault("configuration.strictGates", false)
		v.SetDefault("configuration.vectorMinCosine", 0.72)
		v.SetDefault("configuration.bm25MinNormalized", 0.15)
		v.SetDefault("configuration.disableSearchFallback", true)
		v.SetDefault("configuration.tokenSpendThresh", 1500000)
		v.SetDefault("configuration.lruLimit", 2048)
		v.SetDefault("configuration.synthesisBiasVector", 0.7)
		v.SetDefault("configuration.synthesisBiasSynergy", 0.3)
		v.SetDefault("configuration.synthesisBiasRole", 0.0)
		v.SetDefault("configuration.scoreFusionAlpha", 0.5)
		v.SetDefault("configuration.corroborationBonus", 0.05)
		v.SetDefault("configuration.reliabilityBoost", 0.15)
		v.SetDefault("configuration.usageBoost", 0.02)
		v.SetDefault("configuration.nativeBoost", 0.10)
		v.SetDefault("configuration.confidenceGap", 0.4)
		v.SetDefault("configuration.squeezeBypass", []string{})
		v.SetDefault("configuration.ringBufferTargets", []string{})
		v.SetDefault("configuration.pinnedServers", []string{})
		v.SetDefault("configuration.trustServers", []string{})

		if err := os.WriteFile(configPath, []byte(defaultConfigTemplate), 0600); err != nil {
			return nil, fmt.Errorf("failed to create default config: %w", err)
		}
		// Retry read after create
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read created config: %w", err)
		}
	}

	cfg, err := LoadFromViper(v)
	if err != nil {
		return nil, err
	}
	cfg.v = v
	cfg.ConfigPath = configPath
	cfg.Paths = resolvedPaths
	cfg.Name = Name
	cfg.Version = version
	cfg.DBPath = dbPath
	cfg.LogPath = DefaultLogPath()

	// Default minification is Level 3 (800 tokens)
	if cfg.MaxResponseTokens == 0 {
		cfg.MaxResponseTokens = 800
	}

	// Phase 6 defaults: Proxy hardening configurability
	if cfg.SessionLRUSize == 0 {
		cfg.SessionLRUSize = 30
	}
	if cfg.AlignMaxResults == 0 {
		cfg.AlignMaxResults = 5
	}
	if cfg.PRFConfidenceSkip == 0 {
		cfg.PRFConfidenceSkip = 0.7 // Skip PRF when top result already high-confidence
	}

	return cfg, nil
}

// FoldedString is undocumented but satisfies standard structural requirements.
type FoldedString string

// MarshalYAML forces the string to be encoded as a Literal Block Scalar (|)
// which explicitly preserves our manual word wrapping at 120 characters.
func (s FoldedString) MarshalYAML() (any, error) {
	words := strings.Fields(string(s))
	if len(words) == 0 {
		return &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: string(s),
			Style: yaml.LiteralStyle,
		}, nil
	}

	var sb strings.Builder
	currentLen := 0
	for i, word := range words {
		if i > 0 {
			if currentLen+1+len(word) > 120 {
				sb.WriteString("\n")
				currentLen = 0
			} else {
				sb.WriteString(" ")
				currentLen++
			}
		}
		sb.WriteString(word)
		currentLen += len(word)
	}

	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: sb.String(),
		Style: yaml.LiteralStyle,
	}, nil
}

// NativeTool represents the structure of tools defined in the static config
type NativeTool struct {
	Name        string       `yaml:"name" json:"name"`
	Description FoldedString `yaml:"description" json:"description"`
	Category    string       `yaml:"category,omitempty" json:"category,omitempty,omitzero"`
	InputSchema any          `yaml:"inputSchema" json:"inputSchema"`
	IsNative    bool         `yaml:"is_native,omitempty" json:"is_native,omitempty,omitzero"`
}

// SaveConfiguration writes ONLY the configuration block to the config file,
// preserving all existing YAML comments and structure. It decodes the file
// into a yaml.Node AST, surgically updates only the changed values, and
// re-encodes. This guarantees comment preservation across all write paths
// (configure wizard, update_config tool).
func (c *Config) SaveConfiguration() error {
	path := c.ConfigPath

	// Read the existing file into a yaml.Node tree to preserve comments.
	var doc yaml.Node
	existingData, readErr := os.ReadFile(path)
	if readErr == nil {
		if err := yaml.Unmarshal(existingData, &doc); err != nil {
			// If the existing file is corrupt, start fresh
			doc = yaml.Node{}
		}
	}

	// Ensure we have a valid document node wrapping a mapping node.
	// yaml.Unmarshal produces Kind==DocumentNode with Content[0]==MappingNode.
	var root *yaml.Node
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root = doc.Content[0]
	} else {
		// No existing file or empty — create a fresh mapping
		root = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	}

	// Find or create the "configuration" mapping node
	cfgNode := findOrCreateMapping(root, "configuration")

	// Build flat key→value pairs from current runtime state
	c.mu.RLock()

	setNodeValue(cfgNode, "logFormat", c.LogFormat)
	setNodeValue(cfgNode, "logLevel", c.LogLevel)
	setNodeValue(cfgNode, "mcpLogLevel", c.MCPLogLevel)
	if c.SqueezeLevelState != nil {
		setNodeValue(cfgNode, "squeezeLevel", *c.SqueezeLevelState)
	} else {
		removeNodeKey(cfgNode, "squeezeLevel")
	}
	setNodeValue(cfgNode, "validateProxyCalls", c.ValidateProxyCalls)
	setNodeValue(cfgNode, "tokenSpendThresh", c.TokenSpendThresh)
	setNodeValue(cfgNode, "lruLimit", c.LRULimit)
	setNodeValue(cfgNode, "synthesisBiasVector", c.SynthesisBiasVector)
	setNodeValue(cfgNode, "synthesisBiasSynergy", c.SynthesisBiasSynergy)
	setNodeValue(cfgNode, "synthesisBiasRole", c.SynthesisBiasRole)
	setNodeValue(cfgNode, "scoreFusionAlpha", c.ScoreFusionAlpha)
	if c.ConfidenceGap != 0 {
		setNodeValue(cfgNode, "confidenceGap", c.ConfidenceGap)
	}
	if c.ScoreThreshold != 0 {
		setNodeValue(cfgNode, "scoreThreshold", c.ScoreThreshold)
	}

	// Sequence fields
	setNodeSequence(cfgNode, "squeezeBypass", c.SqueezeBypass)
	setNodeSequence(cfgNode, "ringBufferTargets", c.RingBufferTargets)
	setNodeSequence(cfgNode, "pinnedServers", c.PinnedServers)
	setNodeSequence(cfgNode, "trustServers", c.TrustServers)

	// Intelligence sub-mapping
	if c.Intelligence.Provider != "" {
		intelNode := findOrCreateMapping(cfgNode, "intelligence")

		setNodeValue(intelNode, "provider", c.Intelligence.Provider)
		setNodeValue(intelNode, "model", c.Intelligence.Model)
		setNodeValue(intelNode, "api_key", c.Intelligence.APIKey)
		if c.Intelligence.APIURL != "" {
			setNodeValue(intelNode, "api_url", c.Intelligence.APIURL)
		}

		if len(c.Intelligence.FallbackModels) > 0 {
			setNodeSequence(intelNode, "fallback_models", c.Intelligence.FallbackModels)
		}
		if c.Intelligence.RetryCount > 0 {
			setNodeValue(intelNode, "retry_count", c.Intelligence.RetryCount)
		}
		if c.Intelligence.RetryDelay > 0 {
			setNodeValue(intelNode, "retry_delay_seconds", c.Intelligence.RetryDelay)
		}
		if c.Intelligence.TimeoutSeconds > 0 {
			setNodeValue(intelNode, "timeout_seconds", c.Intelligence.TimeoutSeconds)
		}

		// Thinking tier
		if c.Intelligence.ThinkingProvider != "" {
			setNodeValue(intelNode, "thinking_provider", c.Intelligence.ThinkingProvider)
		}
		if c.Intelligence.ThinkingModel != "" {
			setNodeValue(intelNode, "thinking_model", c.Intelligence.ThinkingModel)
		}
		if c.Intelligence.ThinkingAPIKey != "" {
			setNodeValue(intelNode, "thinking_api_key", c.Intelligence.ThinkingAPIKey)
		}

		// Shared LLM Backplane
		setNodeValue(intelNode, "shared_llm_enabled", c.Intelligence.SharedLLMEnabled)
		if c.Intelligence.LLMPort > 0 {
			setNodeValue(intelNode, "llm_port", c.Intelligence.LLMPort)
		}
		if c.Intelligence.MaxConcurrent > 0 {
			setNodeValue(intelNode, "max_concurrent_requests", c.Intelligence.MaxConcurrent)
		}
		if c.Intelligence.MaxRPM > 0 {
			setNodeValue(intelNode, "max_rpm", c.Intelligence.MaxRPM)
		}
		if c.Intelligence.MaxBurstPerSecond > 0 {
			setNodeValue(intelNode, "max_burst_per_second", c.Intelligence.MaxBurstPerSecond)
		}
		if c.Intelligence.SubServerTokenMax > 0 {
			setNodeValue(intelNode, "sub_server_token_thresh", c.Intelligence.SubServerTokenMax)
		}
		if c.Intelligence.OrphanStreamTTL > 0 {
			setNodeValue(intelNode, "orphan_stream_ttl_minutes", c.Intelligence.OrphanStreamTTL)
		}

		// Embedding Engine
		if c.Intelligence.EmbeddingProvider != "" {
			setNodeValue(intelNode, "embedding_provider", c.Intelligence.EmbeddingProvider)
		}
		if c.Intelligence.EmbeddingModel != "" {
			setNodeValue(intelNode, "embedding_model", c.Intelligence.EmbeddingModel)
		}
		if c.Intelligence.EmbeddingAPIKey != "" {
			setNodeValue(intelNode, "embedding_api_key", c.Intelligence.EmbeddingAPIKey)
		}
		if c.Intelligence.EmbeddingDimensionality > 0 {
			setNodeValue(intelNode, "embedding_dimensionality", c.Intelligence.EmbeddingDimensionality)
		}
		if c.Intelligence.EmbeddingAPIURL != "" {
			setNodeValue(intelNode, "embedding_api_url", c.Intelligence.EmbeddingAPIURL)
		}
		setNodeValue(intelNode, "vector_enabled", c.Intelligence.VectorEnabled)
	}
	c.mu.RUnlock()

	forceBlockStyle(&doc)
	data, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("failed to marshal config to yaml: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}

	// Post-write Viper sync
	c.mu.RLock()
	v := c.v
	c.mu.RUnlock()
	if v != nil {
		_ = v.ReadInConfig()
	}

	return nil
}

// findOrCreateMapping locates a child mapping node by key within a parent
// mapping node. If the key does not exist, it appends a new key-value pair
// with an empty mapping as the value. Returns the child mapping node.
func findOrCreateMapping(parent *yaml.Node, key string) *yaml.Node {
	// MappingNode Content is [key, value, key, value, ...]
	for i := 0; i < len(parent.Content)-1; i += 2 {
		if parent.Content[i].Value == key {
			child := parent.Content[i+1]
			if child.Kind == yaml.MappingNode {
				return child
			}
			// Key exists but isn't a mapping — replace it
			child.Kind = yaml.MappingNode
			child.Tag = "!!map"
			child.Value = ""
			child.Content = nil
			return child
		}
	}
	// Key not found — append
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, keyNode, valNode)
	return valNode
}

// setNodeValue sets a scalar value for a key in a mapping node. If the key
// already exists, only the value is updated (preserving the key node's
// comments). If the key does not exist, a new key-value pair is appended.
func setNodeValue(mapping *yaml.Node, key string, value any) {
	valStr := fmt.Sprintf("%v", value)

	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			vn := mapping.Content[i+1]
			vn.Value = valStr
			vn.Kind = yaml.ScalarNode
			vn.Tag = "" // Let yaml.v3 infer the tag
			vn.Content = nil
			return
		}
	}
	// Key not found — append new pair
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valNode := &yaml.Node{Kind: yaml.ScalarNode, Value: valStr}
	mapping.Content = append(mapping.Content, keyNode, valNode)
}

// removeNodeKey removes a key-value pair from a mapping node by key if it exists.
func removeNodeKey(mapping *yaml.Node, key string) {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}

// setNodeSequence sets a sequence (list) value for a key in a mapping node.
// Preserves the key node's comments. Creates the key if it doesn't exist.
func setNodeSequence(mapping *yaml.Node, key string, values []string) {
	seqNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: 0}
	for _, v := range values {
		seqNode.Content = append(seqNode.Content, &yaml.Node{
			Kind: yaml.ScalarNode, Tag: "!!str", Value: v,
		})
	}

	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = seqNode
			return
		}
	}
	// Key not found — append
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	mapping.Content = append(mapping.Content, keyNode, seqNode)
}

// UpdateConfigValue updates a single configuration value in a thread-safe manner.
// Supported keys: logLevel, mcpLogLevel, squeezeLevel, scoreThreshold, confidenceGap, validateProxyCalls, pinnedServers, trustServers, squeezeBypass, ringBufferTargets, lruLimit.
// Returns the old value (for logging) and any validation error.
func (c *Config) UpdateConfigValue(key, value string) (oldValue string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch key {
	case "logFormat":
		oldValue = c.LogFormat
		newFormat := strings.ToLower(value)
		if newFormat != "json" && newFormat != "text" {
			return "", fmt.Errorf("logFormat must be 'json' or 'text'")
		}
		c.LogFormat = newFormat
	case "logLevel":
		oldValue = c.LogLevel
		c.LogLevel = strings.ToUpper(value)
	case "mcpLogLevel":
		oldValue = c.MCPLogLevel
		c.MCPLogLevel = strings.ToUpper(value)
	case "squeezeLevel":
		lvl, parseErr := strconv.Atoi(value)
		if parseErr != nil || lvl < 0 || lvl > 5 {
			return "", fmt.Errorf("squeezeLevel must be an integer 0-5, got: %s", value)
		}
		if c.SqueezeLevelState != nil {
			oldValue = strconv.Itoa(*c.SqueezeLevelState)
		}
		c.SqueezeLevelState = &lvl

		// Apply Preset Override System at runtime (mirrors LoadFromViper boot logic)
		switch lvl {
		case 0:
			c.NoOptimize = true
			c.MaxResponseTokens = 0
		case 1:
			c.NoOptimize = false
			c.MaxResponseTokens = 600
		case 2:
			c.NoOptimize = false
			c.MaxResponseTokens = 1000
		case 3:
			c.NoOptimize = false
			c.MaxResponseTokens = 1400
		case 4:
			c.NoOptimize = false
			c.MaxResponseTokens = 1800
		case 5:
			c.NoOptimize = false
			c.MaxResponseTokens = 2400
		}
	case "scoreThreshold":
		val, parseErr := strconv.ParseFloat(value, 64)
		if parseErr != nil || val < 0 || val > 1 {
			return "", fmt.Errorf("scoreThreshold must be a float 0.0-1.0, got: %s", value)
		}
		oldValue = strconv.FormatFloat(c.ScoreThreshold, 'f', -1, 64)
		c.ScoreThreshold = val
	case "confidenceGap":
		val, parseErr := strconv.ParseFloat(value, 64)
		if parseErr != nil || val < 0 || val > 1 {
			return "", fmt.Errorf("confidenceGap must be a float 0.0-1.0, got: %s", value)
		}
		oldValue = strconv.FormatFloat(c.ConfidenceGap, 'f', -1, 64)
		c.ConfidenceGap = val
	case "vector_enabled":
		oldValue = strconv.FormatBool(c.Intelligence.VectorEnabled)
		switch strings.ToLower(value) {
		case "true", "1", "yes", "on":
			c.Intelligence.VectorEnabled = true
		case "false", "0", "no", "off":
			c.Intelligence.VectorEnabled = false
		default:
			return "", fmt.Errorf("vector_enabled must be true/false, got: %s", value)
		}
	case "validateProxyCalls":
		oldValue = strconv.FormatBool(c.ValidateProxyCalls)
		switch strings.ToLower(value) {
		case "true", "1", "yes", "on":
			c.ValidateProxyCalls = true
		case "false", "0", "no", "off":
			c.ValidateProxyCalls = false
		default:
			return "", fmt.Errorf("validateProxyCalls must be true/false, got: %s", value)
		}
	case "pinnedServers":
		oldValue = strings.Join(c.PinnedServers, " ")
		value = strings.TrimSpace(value)
		if value == "" {
			c.PinnedServers = nil
		} else {
			c.PinnedServers = strings.Fields(value)
		}
	case "trustServers":
		oldValue = strings.Join(c.TrustServers, " ")
		value = strings.TrimSpace(value)
		if value == "" {
			c.TrustServers = nil
		} else {
			c.TrustServers = strings.Fields(value)
		}
	case "squeezeBypass":
		oldValue = strings.Join(c.SqueezeBypass, " ")
		value = strings.TrimSpace(value)
		if value == "" {
			c.SqueezeBypass = nil
		} else {
			c.SqueezeBypass = strings.Fields(value)
		}
	case "ringBufferTargets":
		oldValue = strings.Join(c.RingBufferTargets, " ")
		value = strings.TrimSpace(value)
		if value == "" {
			c.RingBufferTargets = nil
		} else {
			c.RingBufferTargets = strings.Fields(value)
		}
	case "tokenSpendThresh":
		val, parseErr := strconv.Atoi(value)
		if parseErr != nil || val < 0 {
			return "", fmt.Errorf("tokenSpendThresh must be a positive integer, got: %s", value)
		}
		oldValue = strconv.Itoa(c.TokenSpendThresh)
		c.TokenSpendThresh = val
	case "lruLimit":
		val, parseErr := strconv.Atoi(value)
		if parseErr != nil || val <= 0 {
			return "", fmt.Errorf("lruLimit must be a positive integer, got: %s", value)
		}
		oldValue = strconv.Itoa(c.LRULimit)
		c.LRULimit = val
	default:
		return "", fmt.Errorf("unsupported config key: %s (supported: logLevel, squeezeLevel, scoreThreshold, confidenceGap, vector_enabled, validateProxyCalls, pinnedServers, trustServers, squeezeBypass, ringBufferTargets, tokenSpendThresh, lruLimit)", key)
	}
	return oldValue, nil
}

// LoadFromViper extracts configuration from a Viper instance and loads
// managed servers from the native servers.yaml registry.
func LoadFromViper(v *viper.Viper) (*Config, error) {
	var ide IDEConfig

	// First, let viper load whatever it has
	if err := v.Unmarshal(&ide); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Dynamic Discovery: Look for actual IDE mcp_config.json for configuration block only.
	var ideConfigPath string
	if path := v.ConfigFileUsed(); path != "" && strings.HasSuffix(path, ".json") {
		ideConfigPath = path
	} else {
		ideConfigPath, _ = DiscoverIDEConfig()
	}

	if data, err := os.ReadFile(ideConfigPath); err == nil {
		var raw IDEConfig
		if err := json.Unmarshal(data, &raw); err == nil {
			ide.McpServers = raw.McpServers
			if raw.Configuration.MaxResponseTokens != 0 {
				ide.Configuration.MaxResponseTokens = raw.Configuration.MaxResponseTokens
			}
			if raw.Configuration.SqueezeLevel != nil {
				ide.Configuration.SqueezeLevel = raw.Configuration.SqueezeLevel
			}
			if len(raw.Configuration.SqueezeBypass) > 0 {
				ide.Configuration.SqueezeBypass = append([]string{}, raw.Configuration.SqueezeBypass...)
			}
			if len(raw.Configuration.RingBufferTargets) > 0 {
				ide.Configuration.RingBufferTargets = append([]string{}, raw.Configuration.RingBufferTargets...)
			}
		}
	}

	serversPath := filepath.Join(DefaultConfigDir(), ServersConfigFile)
	if path := v.ConfigFileUsed(); path != "" {
		if p, err := ResolvePaths(path); err == nil {
			serversPath = p.Servers
		}
	}

	managed, err := LoadManagedServersAt(serversPath)
	if err != nil {
		slog.Info("config: servers.yaml not found, generating defaults", "error", err)

		if err := os.MkdirAll(filepath.Dir(serversPath), 0700); err != nil {
			slog.Warn("config: failed to create config directory", "path", serversPath, "error", err)
		}

		if err := os.WriteFile(serversPath, []byte(defaultServersTemplate), 0600); err != nil {
			slog.Warn("config: failed to write default servers.yaml", "error", err)
		} else {
			slog.Info("config: generated default servers.yaml")
		}

		managed, _ = LoadManagedServersAt(serversPath)

		if data, err := os.ReadFile(ideConfigPath); err == nil {
			var rawIDE IDEConfig
			if err := json.Unmarshal(data, &rawIDE); err == nil {
				migrationCount := 0
				for name, entry := range rawIDE.McpServers {
					if name == SelfName {
						continue // strictly never migrate the magictools orchestrator
					}

					exists := false
					for _, m := range managed {
						if m.Name == name {
							exists = true
							break
						}
					}

					if !exists {
						managed = append(managed, ServerConfig{
							Name:          name,
							Command:       entry.Command,
							Args:          entry.Args,
							Env:           entry.Env,
							DisabledTools: entry.DisabledTools,
							Disabled:      false, // actively migrate them as enabled so they boot
						})
						migrationCount++
					}
				}
				if migrationCount > 0 {
					if saveErr := SaveManagedServers(managed); saveErr != nil {
						slog.Warn("config: failed to write servers.yaml during IDE migration", "error", saveErr)
					} else {
						slog.Info("config: migrated IDE servers to servers.yaml", "count", migrationCount)
					}
				}
			}
		}
	}

	cfg := &Config{
		ManagedServers:        managed,
		ideConfig:             &ide,
		MaxResponseTokens:     ide.Configuration.MaxResponseTokens,
		SqueezeLevelState:     ide.Configuration.SqueezeLevel,
		ScoreThreshold:        ide.Configuration.ScoreThreshold,
		StrictGates:           ide.Configuration.StrictGates,
		VectorMinCosine:       ide.Configuration.VectorMinCosine,
		BM25MinNormalized:     ide.Configuration.BM25MinNormalized,
		DisableSearchFallback: ide.Configuration.DisableSearchFallback,
		SqueezeBypass:         ide.Configuration.SqueezeBypass,
		RingBufferTargets:     ide.Configuration.RingBufferTargets,
		PinnedServers:         ide.Configuration.PinnedServers,
		TrustServers:          ide.Configuration.TrustServers,
		LogFormat:             ide.Configuration.LogFormat,
		LogLevel:              ide.Configuration.LogLevel,
		MCPLogLevel:           ide.Configuration.MCPLogLevel,
		TokenSpendThresh:      ide.Configuration.TokenSpendThresh,
		LRULimit:              ide.Configuration.LRULimit,
		SynthesisBiasVector:   ide.Configuration.SynthesisBiasVector,
		SynthesisBiasSynergy:  ide.Configuration.SynthesisBiasSynergy,
		SynthesisBiasRole:     ide.Configuration.SynthesisBiasRole,
		ScoreFusionAlpha:      ide.Configuration.ScoreFusionAlpha,
		CorroborationBonus:    ide.Configuration.CorroborationBonus,
		ReliabilityBoost:      ide.Configuration.ReliabilityBoost,
		UsageBoost:            ide.Configuration.UsageBoost,
		NativeBoost:           ide.Configuration.NativeBoost,
		ConfidenceGap:         ide.Configuration.ConfidenceGap,
		Intelligence:          ide.Configuration.Intelligence,
	}

	// 🛡️ SYNCHRONOUS OVERRIDES LOAD: Load overrides immediately on boot to prevent
	// the initial sync_manager cycle from hashing the original downstream descriptions,
	// which causes a false hash delta and forces vector DB re-hydration.
	overridesPath := filepath.Join(DefaultConfigDir(), "tool_overrides.yaml")
	if data, err := os.ReadFile(overridesPath); err == nil && len(data) > 0 {
		var newOverrides ToolOverridesConfig
		if err := yaml.Unmarshal(data, &newOverrides); err == nil {
			cfg.ToolOverrides = newOverrides
		} else {
			slog.Error("config: failed to parse tool_overrides.yaml during boot", "error", err)
		}
	}

	normalizeRRFBiases(cfg)
	normalizeSearchGates(cfg)
	normalizeFusionWeights(cfg)

	if cfg.TokenSpendThresh == 0 {
		cfg.TokenSpendThresh = 1500000
	}
	if cfg.LRULimit == 0 {
		cfg.LRULimit = 2048
	}

	// 🛡️ INTELLIGENCE DEFAULTS: Apply safe defaults for hydrator retry/timeout
	if cfg.Intelligence.RetryCount <= 0 {
		cfg.Intelligence.RetryCount = 2
	}
	if cfg.Intelligence.RetryDelay <= 0 {
		cfg.Intelligence.RetryDelay = 5
	}
	if cfg.Intelligence.TimeoutSeconds <= 0 {
		cfg.Intelligence.TimeoutSeconds = 120
	}

	// 🛡️ EMBEDDING DEFAULTS: Resolve embedding provider/model from parent if omitted
	if cfg.Intelligence.EmbeddingProvider == "" && cfg.Intelligence.Provider != "" {
		switch cfg.Intelligence.Provider {
		case "gemini", "openai":
			cfg.Intelligence.EmbeddingProvider = cfg.Intelligence.Provider
		case "claude":
			// Claude has no embedding API; auto-detect from env
			if os.Getenv("GEMINI_API_KEY") != "" {
				cfg.Intelligence.EmbeddingProvider = "gemini"
			} else if os.Getenv("OPENAI_API_KEY") != "" {
				cfg.Intelligence.EmbeddingProvider = "openai"
			} else if os.Getenv("VOYAGE_API_KEY") != "" {
				cfg.Intelligence.EmbeddingProvider = "voyage"
			}
		}
	}
	if cfg.Intelligence.EmbeddingModel == "" {
		switch cfg.Intelligence.EmbeddingProvider {
		case "gemini":
			cfg.Intelligence.EmbeddingModel = "gemini-embedding-2-preview"
		case "openai":
			cfg.Intelligence.EmbeddingModel = "text-embedding-3-small"
		case "voyage":
			cfg.Intelligence.EmbeddingModel = "voyage-code-3"
		case "ollama":
			cfg.Intelligence.EmbeddingModel = "granite-embedding:30m"
		}
	}
	if cfg.Intelligence.EmbeddingAPIKey == "" {
		if cfg.Intelligence.EmbeddingProvider == cfg.Intelligence.Provider {
			cfg.Intelligence.EmbeddingAPIKey = cfg.Intelligence.APIKey
		} else {
			// Cross-provider: check env for the embedding provider's key
			switch cfg.Intelligence.EmbeddingProvider {
			case "gemini":
				cfg.Intelligence.EmbeddingAPIKey = os.Getenv("GEMINI_API_KEY")
			case "openai":
				cfg.Intelligence.EmbeddingAPIKey = os.Getenv("OPENAI_API_KEY")
			case "voyage":
				cfg.Intelligence.EmbeddingAPIKey = os.Getenv("VOYAGE_API_KEY")
			}
		}
	}
	if cfg.Intelligence.EmbeddingDimensionality <= 0 {
		switch cfg.Intelligence.EmbeddingProvider {
		case "gemini", "openai", "voyage":
			cfg.Intelligence.EmbeddingDimensionality = 768
		default:
			// Ollama and all future local/self-hosted providers default to 384.
			// Users may override to 768 for larger models (e.g., granite-embedding:278m)
			// by setting embedding_dimensionality explicitly in config.yaml.
			cfg.Intelligence.EmbeddingDimensionality = 384
		}
	}
	if cfg.Intelligence.EmbeddingAPIURL == "" && cfg.Intelligence.EmbeddingProvider == "ollama" {
		cfg.Intelligence.EmbeddingAPIURL = "http://localhost:11434"
	}

	if len(cfg.SqueezeBypass) > 0 {
		slog.Info("config: squeeze bypass loaded", "servers", cfg.SqueezeBypass)
	}
	if len(cfg.RingBufferTargets) > 0 {
		slog.Info("config: ring buffer targets loaded", "targets", cfg.RingBufferTargets)
	}

	if ide.Configuration.ValidateProxyCalls != nil {
		cfg.ValidateProxyCalls = *ide.Configuration.ValidateProxyCalls
	} else {
		cfg.ValidateProxyCalls = true
	}

	if cfg.ScoreThreshold == 0.0 {
		cfg.ScoreThreshold = 0.3
	}

	// Preset Override System
	if ide.Configuration.SqueezeLevel != nil {
		switch *ide.Configuration.SqueezeLevel {
		case 1:
			cfg.MaxResponseTokens = 600
		case 2:
			cfg.MaxResponseTokens = 1000
		case 3:
			cfg.MaxResponseTokens = 1400
		case 4:
			cfg.MaxResponseTokens = 1800
		case 5:
			cfg.MaxResponseTokens = 2400
		case 0:
			cfg.NoOptimize = true
			cfg.MaxResponseTokens = 0
		default:
			cfg.MaxResponseTokens = 1400
		}
	} else {
		// If explicitly omitted, respect viper default level 3 (which was set in SetDefault) or default to 800
		cfg.MaxResponseTokens = 800
	}

	return cfg, nil
}

// Load parses the IDE config file and extracts managed servers (Legacy/CLI wrapper)
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}
	cfg, err := LoadFromViper(v)
	if err != nil {
		return nil, err
	}
	cfg.v = v
	cfg.ConfigPath = path
	return cfg, nil
}

// normalizeRRFBiases mathematically enforces the 3-way constraint where
// SynthesisBiasVector + SynthesisBiasSynergy + SynthesisBiasRole must equal 1.0.
// Defaults: Engine=0.5, Synergy=0.2, Role=0.3. ScoreFusionAlpha defaults to 0.6.
func normalizeRRFBiases(cfg *Config) {
	if cfg.SynthesisBiasVector <= 0 && cfg.SynthesisBiasSynergy <= 0 && cfg.SynthesisBiasRole <= 0 {
		cfg.SynthesisBiasVector = 0.5
		cfg.SynthesisBiasSynergy = 0.2
		cfg.SynthesisBiasRole = 0.3
	}
	if cfg.SynthesisBiasRole <= 0 {
		cfg.SynthesisBiasRole = 0.3
	}

	total := cfg.SynthesisBiasVector + cfg.SynthesisBiasSynergy + cfg.SynthesisBiasRole
	if total != 1.0 && total > 0 {
		cfg.SynthesisBiasVector = cfg.SynthesisBiasVector / total
		cfg.SynthesisBiasSynergy = cfg.SynthesisBiasSynergy / total
		cfg.SynthesisBiasRole = cfg.SynthesisBiasRole / total
	}

	if cfg.ScoreFusionAlpha <= 0 {
		cfg.ScoreFusionAlpha = 0.6
	}
	if cfg.ScoreFusionAlpha > 1.0 {
		cfg.ScoreFusionAlpha = 1.0
	}
}

// normalizeSearchGates applies calibrated defaults for per-leg retrieval floors.
func normalizeSearchGates(cfg *Config) {
	if cfg.VectorMinCosine <= 0 {
		cfg.VectorMinCosine = 0.72
	}
	if cfg.BM25MinNormalized <= 0 {
		cfg.BM25MinNormalized = 0.15
	}
}

// normalizeFusionWeights applies the calibrated (ADR-0016) defaults for the
// score-fusion and post-fusion ranking-boost weights (INT-7). A non-positive
// value means "unset" and falls back to the default — these weights are never
// meant to be zero, so this never silently disables a boost.
func normalizeFusionWeights(cfg *Config) {
	if cfg.CorroborationBonus <= 0 {
		cfg.CorroborationBonus = 0.05
	}
	if cfg.ReliabilityBoost <= 0 {
		cfg.ReliabilityBoost = 0.15
	}
	if cfg.UsageBoost <= 0 {
		cfg.UsageBoost = 0.02
	}
	if cfg.NativeBoost <= 0 {
		cfg.NativeBoost = 0.10
	}
}

// Reload re-reads the config file and returns a new Config
func (c *Config) Reload() (*Config, error) {
	if c.v != nil {
		if err := c.v.ReadInConfig(); err != nil {
			return nil, err
		}
		cfg, err := LoadFromViper(c.v)
		if err != nil {
			return nil, err
		}
		return cfg, nil
	}
	cfg, err := Load(c.ConfigPath)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// Viper returns the underlying viper instance
func (c *Config) Viper() *viper.Viper {
	return c.v
}

// GetManagedServerNames returns just the names of managed servers
func (c *Config) GetManagedServerNames() map[string]bool {
	names := make(map[string]bool, len(c.ManagedServers))
	for _, s := range c.ManagedServers {
		names[s.Name] = true
	}
	return names
}

// serversYAML is the on-disk format for the native server registry.
type serversYAML struct {
	Servers []serverEntry `yaml:"servers"`
}

type serverEntry struct {
	Name          string            `yaml:"name"`
	Command       string            `yaml:"command"`
	Args          []string          `yaml:"args"`
	Env           map[string]string `yaml:"env"`
	DisabledTools []string          `yaml:"disabled_tools"`
	MemoryLimitMB int               `yaml:"memory_limit_mb"`
	GoMemLimitMB  int               `yaml:"gomemlimit_mb,omitempty"`
	MaxCPULimit   int               `yaml:"max_cpu_limit"`
	DeferredBoot  bool              `yaml:"deferred_boot"`
	Disabled      bool              `yaml:"disabled"`
}

// LoadManagedServers reads the native server registry from default servers.yaml.
func LoadManagedServers() ([]ServerConfig, error) {
	return LoadManagedServersAt(filepath.Join(DefaultConfigDir(), ServersConfigFile))
}

// LoadManagedServersAt reads the native server registry from the specified servers.yaml path.
func LoadManagedServersAt(path string) ([]ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("servers.yaml not found: %w", err)
	}

	var reg serversYAML
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("servers.yaml parse error: %w", err)
	}

	servers := make([]ServerConfig, 0, len(reg.Servers))
	for _, e := range reg.Servers {
		servers = append(servers, ServerConfig{
			Name:          e.Name,
			Command:       e.Command,
			Args:          e.Args,
			Env:           e.Env,
			DisabledTools: e.DisabledTools,
			MemoryLimitMB: e.MemoryLimitMB,
			GoMemLimitMB:  e.GoMemLimitMB,
			MaxCPULimit:   e.MaxCPULimit,
			DeferredBoot:  e.DeferredBoot,
			Disabled:      e.Disabled,
		})
	}

	slog.Info("config: loaded servers from native registry", "count", len(servers), "path", path)
	return servers, nil
}

// SaveManagedServers writes the native server registry to default servers.yaml.
func SaveManagedServers(servers []ServerConfig) error {
	return SaveManagedServersAt(filepath.Join(DefaultConfigDir(), ServersConfigFile), servers)
}

// SaveManagedServersAt writes the native server registry to the specified path,
// preserving all existing YAML comments and structure.
func SaveManagedServersAt(path string, servers []ServerConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Read existing file into yaml.Node tree to preserve comments
	var doc yaml.Node
	existingData, readErr := os.ReadFile(path)
	if readErr == nil {
		if err := yaml.Unmarshal(existingData, &doc); err != nil {
			doc = yaml.Node{}
		}
	}

	// Ensure valid document structure
	var root *yaml.Node
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root = doc.Content[0]
	} else {
		root = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	}

	// Find or create the "servers" sequence node
	var serversSeq *yaml.Node
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == "servers" {
			serversSeq = root.Content[i+1]
			break
		}
	}
	if serversSeq == nil || serversSeq.Kind != yaml.SequenceNode {
		serversSeq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "servers"},
			serversSeq,
		)
	}

	// Build index of existing server nodes by name for in-place updates
	existingIdx := map[string]int{} // name → index in serversSeq.Content
	for i, entry := range serversSeq.Content {
		if entry.Kind == yaml.MappingNode {
			for j := 0; j < len(entry.Content)-1; j += 2 {
				if entry.Content[j].Value == "name" {
					existingIdx[entry.Content[j+1].Value] = i
					break
				}
			}
		}
	}

	// Update existing or append new server entries
	for _, sc := range servers {
		if idx, ok := existingIdx[sc.Name]; ok {
			// Update in-place — preserve comments on existing node
			updateServerNode(serversSeq.Content[idx], sc)
		} else {
			// New server — append a fresh mapping node
			node := buildServerNode(sc)
			serversSeq.Content = append(serversSeq.Content, node)
		}
	}

	forceBlockStyle(&doc)
	data, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("failed to marshal servers.yaml: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write servers.yaml: %w", err)
	}

	slog.Info("config: saved native server registry", "count", len(servers), "path", path)
	return nil
}

// updateServerNode updates an existing server mapping node in-place,
// preserving all comments on the key nodes.
func updateServerNode(node *yaml.Node, sc ServerConfig) {
	setNodeValue(node, "name", sc.Name)
	setNodeValue(node, "command", sc.Command)
	if len(sc.Args) > 0 {
		setNodeSequence(node, "args", sc.Args)
	}
	if len(sc.Env) > 0 {
		setServerEnvNode(node, sc.Env)
	}
	if sc.DisabledTools == nil {
		sc.DisabledTools = []string{}
	}
	setNodeSequence(node, "disabled_tools", sc.DisabledTools)
	if sc.MemoryLimitMB > 0 {
		setNodeValue(node, "memory_limit_mb", sc.MemoryLimitMB)
	}
	if sc.GoMemLimitMB > 0 {
		setNodeValue(node, "gomemlimit_mb", sc.GoMemLimitMB)
	}
	if sc.MaxCPULimit > 0 {
		setNodeValue(node, "max_cpu_limit", sc.MaxCPULimit)
	}
	setNodeValue(node, "deferred_boot", sc.DeferredBoot)
	setNodeValue(node, "disabled", sc.Disabled)
}

// buildServerNode creates a fresh yaml.Node mapping for a new server entry.
func buildServerNode(sc ServerConfig) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	updateServerNode(node, sc)
	return node
}

// setServerEnvNode sets the env mapping within a server node, preserving
// existing env key comments.
func setServerEnvNode(serverNode *yaml.Node, env map[string]string) {
	envNode := findOrCreateMapping(serverNode, "env")
	for k, v := range env {
		setNodeValue(envNode, k, v)
	}
}

// forceBlockStyle recursively traverses the AST and clears the yaml.FlowStyle
// bitmask from all SequenceNode and MappingNode objects to enforce pure
// block-style formatting in the output configuration file.
func forceBlockStyle(node *yaml.Node) {
	if node == nil {
		return
	}
	if node.Kind == yaml.SequenceNode || node.Kind == yaml.MappingNode {
		node.Style &= ^yaml.FlowStyle
	}
	for _, child := range node.Content {
		forceBlockStyle(child)
	}
}
