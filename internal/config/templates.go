// Package config provides functionality for the config subsystem.
package config

import (
	"runtime"
	"strings"
)

// DefaultConfigTemplate returns the embedded default config.yaml template content.
// Used by both the init command and the auto-creation path in New().
func DefaultConfigTemplate() string {
	return defaultConfigTemplate
}

// DefaultServersTemplate returns the platform-appropriate servers.yaml template.
// On Windows it returns the Windows-specific template with Windows paths,
// executable extensions, and platform-specific comments. On macOS the Unix
// template's example home paths are rewritten to /Users form; Linux gets the
// Unix template as-is.
func DefaultServersTemplate() string {
	switch runtime.GOOS {
	case "windows":
		return defaultServersTemplateWindows
	case "darwin":
		return strings.ReplaceAll(defaultServersTemplate, "/home/your-user", "/Users/your-user")
	default:
		return defaultServersTemplate
	}
}

// DefaultToolOverridesTemplate returns the embedded default tool_overrides.yaml template.
func DefaultToolOverridesTemplate() string {
	return defaultToolOverridesYAML
}

// defaultConfigTemplate is the fully-commented config.yaml template that ships
// with the orchestrator. This serves as both documentation and the default
// configuration seed for new installations.
const defaultConfigTemplate = `configuration:
  # =========================================================================
  # MAGIC TOOLS ORCHESTRATOR CONFIGURATION (config.yaml)
  # =========================================================================
  # This file controls the core limits, logging, search weights, AI providers,
  # and shared LLM backplane settings for the magictools MCP orchestrator.
  #
  # Hot-reload: This file is monitored via fsnotify. Changes are applied at
  # runtime without restarting the orchestrator (except logFormat which
  # requires a restart). Comments are preserved — Viper never writes to this
  # file; only the update_config tool and configure wizard modify it via the
  # native YAML library.
  #
  # Line endings: This file MUST use Unix-style (LF) line endings on all
  # platforms (Windows, macOS, Linux).
  #
  # Environment variables: Use $VAR or ${VAR} syntax for expansion in
  # servers.yaml. Windows ` + "`" + `%VAR%` + "`" + ` syntax is NOT supported by Go's os.ExpandEnv.

  # -------------------------------------------------------------------------
  # Core Orchestrator Constraints & System Limits
  # -------------------------------------------------------------------------

  # squeezeLevel controls the maximum concurrent sub-server requests made
  # during Socratic DAG pipeline generation. Higher levels increase
  # parallelism but heavily consume your LLM's context window.
  # Valid: 0-5. Recommended: 3 (balance of speed and token efficiency).
  squeezeLevel: 3

  # tokenSpendThresh is the maximum allowed token budget per operation cycle.
  # If an operation exceeds this threshold, a circuit-breaker will throttle
  # the request to prevent infinite loops and runaway billing costs.
  # Default: 1500000 (1.5M tokens).
  tokenSpendThresh: 1500000

  # lruLimit sets the maximum number of items retained in the orchestrator's
  # internal Least Recently Used (LRU) memory caches (Response & Registry).
  # Higher values improve cache hit rates but consume more memory.
  # Default: 2048.
  lruLimit: 2048

  # validateProxyCalls enforces strict JSON Schema validation within the
  # proxy execution engine. When true, tools called via call_proxy MUST
  # match their discovered JSON schemas exactly, or the call is rejected.
  # Disable only for development/debugging.
  # Default: true.
  validateProxyCalls: true


  # -------------------------------------------------------------------------
  # Observability & Logging Parameters
  # -------------------------------------------------------------------------

  # logLevel defines the verbosity of the orchestrator's internal system logs.
  # Values: ERROR, WARN, INFO, DEBUG, TRACE.
  # Hot-reloadable: changes apply immediately via update_config tool.
  logLevel: DEBUG

  # mcpLogLevel defines the logging verbosity for downstream sub-servers
  # communicating over the MCP protocol (JSON-RPC). Changing this triggers
  # a fleet-wide restart of all managed sub-servers.
  # Values: ERROR, WARN, INFO, DEBUG, TRACE.
  mcpLogLevel: INFO

  # logFormat determines how logs are emitted to the console and log files.
  # Options: 'json' (structured, machine-readable) or 'text' (human-readable).
  # NOTE: Requires orchestrator restart to take effect.
  logFormat: json


  # -------------------------------------------------------------------------
  # Search & Intent Matching Weights (HNSW & Bleve)
  # -------------------------------------------------------------------------

  # scoreThreshold defines the minimum fused search score (direct BM25/vector blend)
  # required for a tool to survive align_tools culling (Range: 0.0 to 1.0).
  # Individual tail results below (scoreThreshold × 0.4) are also dropped post-fusion.
  # Lower values return more results but with lower precision.
  # Default: 0.3.
  scoreThreshold: 0.3

  # scoreFusionAlpha controls the blend between lexical (BM25) and semantic
  # (Vector) search when both engines are active.
  # 0.0 = Pure BM25 Lexical Search.
  # 1.0 = Pure Vector Semantic Search.
  # Default: 0.5 (equal weight).
  scoreFusionAlpha: 0.5

  # Fusion + ranking-boost weights (ADR-0016). Each defaults to the calibrated
  # value when unset; a non-positive value is treated as unset. Hot-reloadable.
  #   corroborationBonus: bonus added when BOTH legs (BM25 + vector) match a tool.
  #   reliabilityBoost:   post-fusion multiplier on (proxyReliability - 1.0).
  #   usageBoost:         post-fusion multiplier on log1p(usageCount).
  #   nativeBoost:        post-fusion additive boost for native tools.
  corroborationBonus: 0.05
  reliabilityBoost: 0.15
  usageBoost: 0.02
  nativeBoost: 0.10

  # Tri-factor scoring weights for advanced intent matching. These three
  # biases govern how the Bleve engine ranks tools dynamically.
  # CONSTRAINT: These three values MUST sum to exactly 1.0.
  synthesisBiasVector: 0.7   # Weight of baseline semantic/vector similarity.
  synthesisBiasSynergy: 0.3  # Weight of synergy index (structural tool mappings).
  synthesisBiasRole: 0.0     # Weight of deterministic role-based overrides.

  # confidenceGap sets the minimum score difference between the #1 and #2
  # search results required for align_tools to auto-execute a tool inline.
  # Lower values (e.g. 0.2) allow more aggressive inline execution but
  # increase the risk of executing the wrong tool. Higher values (e.g. 0.5)
  # are more conservative — requiring a dominant match before auto-executing.
  # Range: 0.0 to 1.0. Default: 0.4.
  # Hot-reloadable: changes apply immediately via update_config tool.
  confidenceGap: 0.4

  # strictGates enables per-leg Bleve/vector confidence floors before score fusion.
  # When true, weak lexical or vector legs are dropped instead of polluting fusion.
  # Default: false (backward compatible). Pair with disableSearchFallback when enabling.
  strictGates: false

  # vectorMinCosine is the minimum top-1 HNSW cosine required to keep the vector leg
  # when strictGates is enabled. Range: 0.0 to 1.0. Default: 0.72.
  vectorMinCosine: 0.72

  # bm25MinNormalized is the minimum atan-normalized Bleve BM25 score required to
  # keep the lexical leg when strictGates is enabled. Range: 0.0 to 1.0. Default: 0.15.
  bm25MinNormalized: 0.15

  # disableSearchFallback suppresses substring SearchToolsFallback when strictGates
  # is enabled and fused scores miss scoreThreshold. Default: true.
  disableSearchFallback: true


  # -------------------------------------------------------------------------
  # Advanced Ecosystem Tuning
  # -------------------------------------------------------------------------

  # squeezeBypass: A list of specific server:tool targets that are completely
  # exempt from the squeezeLevel concurrency/minification limits. Use this
  # for tools that produce large outputs that must not be truncated.
  # Format: ["server:tool", "server:tool"] or ["server"] for all tools.
  squeezeBypass: []

  # ringBufferTargets: A list of specific targets (format: 'server:tool')
  # that are tracked at high fidelity in the telemetry diagnostic ring
  # buffers. These targets get full execution trace logging.
  ringBufferTargets: []

  # pinnedServers: A list of critical sub-servers that are guaranteed to load
  # synchronously at orchestrator boot and are immune to idle eviction.
  # These servers will always remain warm in memory.
  pinnedServers: []

  # trustServers: A list of sub-servers authorized for inline auto-execution
  # in align_tools. When the agent calls align_tools with optional 'arguments'
  # and a single high-confidence match resolves to a server in this list,
  # the tool executes inline — returning the result in a single call instead
  # of requiring a follow-up call_proxy round-trip.
  #
  # This is a SECURITY BOUNDARY: only servers you explicitly trust should be
  # listed here. Servers NOT in this list will never be auto-executed; the
  # agent must confirm the URN via call_proxy.
  #
  # NOTE: This is separate from pinnedServers. pinnedServers controls process
  # lifecycle (sleep/eviction immunity). trustServers controls execution
  # authorization. A server can be pinned but not trusted, or vice versa.
  # Default: [] (inline execution disabled — all calls require explicit URN).
  trustServers: []


  # -------------------------------------------------------------------------
  # Intelligence Engines (Generative Hydrator & Vector Memory)
  # -------------------------------------------------------------------------
  intelligence:

    # === 1. Fast Tier (Primary LLM) ========================================
    # The primary LLM used for parsing tasks, composing DAG pipelines,
    # hydrating tool intelligence, and servicing sub-server requests via
    # the shared LLM backplane.
    # Supported providers: gemini, openai, claude
    provider: ""
    model: ""
    api_key: ""

    # Optional list of models to fallback to if the primary model
    # rate-limits or fails. Tried in order.
    fallback_models: []

    # Retry logic for transient API failures.
    retry_count: 2          # Number of retry attempts (0 = no retries).
    retry_delay_seconds: 5  # Delay between retries in seconds.
    timeout_seconds: 120    # Maximum time to wait for a single LLM response.

    # === 2. Thinking Tier (Independent LLM) ================================
    # A dedicated LLM for deep reasoning tasks (Socratic analysis, complex
    # code review). Can use a completely different provider and API key
    # from the fast tier. When configured, requests to /llm/generate-thinking
    # run this model with extended thinking enabled. If NOT configured, the
    # thinking endpoint returns HTTP 404 and callers degrade to the fast tier.
    # Supported providers: gemini, openai, claude
    thinking_provider: ""
    thinking_model: ""
    thinking_api_key: ""

    # === 3. Shared LLM Backplane ===========================================
    # When enabled, the orchestrator exposes the configured LLM providers
    # as a shared HTTP service on a dedicated loopback port. Sub-servers
    # running in orchestrator mode automatically route their LLM requests
    # through this backplane instead of making independent API calls.
    #
    # Benefits:
    #   - Centralized rate limiting and token tracking
    #   - Sub-servers need no API keys of their own
    #   - Responses stay out of agent context (saves tokens)
    #   - Unified audit trail for all LLM usage
    #
    # Set to true to enable. Requires provider and api_key to be configured.
    shared_llm_enabled: false

    # Fixed TCP port for the LLM backplane listener.
    # Default: 48081. The listener binds to 127.0.0.1 only (loopback).
    # Override via MCP_ENDPOINT_LLM_PORT environment variable.
    llm_port: 48081

    # Maximum number of concurrent LLM requests across all sub-servers.
    # Acts as a global semaphore to prevent API overload.
    # Default: 4.
    max_concurrent_requests: 4

    # Maximum requests per minute (RPM) to the upstream LLM API.
    # Enforced via token-bucket rate limiting. Set to 0 for unlimited.
    # Default: 30.
    max_rpm: 30

    # Maximum burst capacity for short request spikes.
    # Allows this many requests to pass instantly before rate limiting
    # kicks in. Default: 5.
    max_burst_per_second: 5

    # Per-sub-server token threshold. When a single sub-server exceeds
    # this cumulative token count, its requests are rejected with
    # HTTP 429 until the next tracking window resets.
    # Default: 500000 (500K tokens per sub-server).
    sub_server_token_thresh: 500000

    # Time-to-live (in minutes) for orphaned LLM stream connections.
    # Connections that exceed this TTL without activity are forcibly closed.
    # Default: 5 minutes.
    orphan_stream_ttl_minutes: 5

    # === 4. Vector Search Engine (HNSW Spatial Embeddings) =================
    # Enable semantic HNSW search for tool alignment and standards matching.
    # Must be true to utilize embedding-based search.
    vector_enabled: false

    # Supported embedding providers: ollama, gemini, openai, voyage
    embedding_provider: ""
    embedding_model: ""
    embedding_api_key: ""

    # Custom endpoint for local models (e.g., http://localhost:11434 for Ollama).
    embedding_api_url: ""

    # Dimensional footprint of your chosen embedding model.
    # Examples: 384 (MiniLM), 768 (Gemini Embedding), 1536 (OpenAI ada-002).
    # Critical for static HNSW graph initialization — must match your model.
    embedding_dimensionality: 0
`

// defaultServersTemplate is the fully-commented servers.yaml template that ships
// with the orchestrator. All paths use generic placeholders — no user-specific
// data, tokens, API keys, or home directory paths.
const defaultServersTemplate = `# =========================================================================
# MAGIC TOOLS SUB-SERVER REGISTRY (servers.yaml)
# =========================================================================
# This file dynamically registers and configures all downstream MCP sub-servers
# managed by the orchestrator. It is monitored via fsnotify and a 30-second
# polling fallback — changes take effect without restarting the orchestrator.
#
# Adding a server:    The orchestrator detects the addition and boots it.
# Removing a server:  The orchestrator detects the removal and shuts it down.
# Modifying a server: The orchestrator detects the config mutation, restarts
#                     the affected server, and re-indexes its tools.
#
# =========================================================================
# FIELD REFERENCE
# =========================================================================
#
# name:             (required) Unique identifier for the sub-server. Used in
#                   tool URNs (e.g., "brainstorm:discover_project"), health
#                   reports, and logging. Must be lowercase alphanumeric with
#                   hyphens only.
#
# command:          (required) Absolute path to the server executable. For
#                   Python servers, point to the virtual environment's python
#                   binary. For Node.js servers, use "node" if on PATH or the
#                   absolute node binary path.
#
# args:             (optional) Command-line arguments passed to the server on
#                   startup. For Go servers this is typically ["serve"]. For
#                   Python, use ["-u", "-m", "module_name"] or ["-u", "script.py"].
#                   For filesystem servers, list allowed paths here.
#
# env:              (optional) Environment variables securely injected into the
#                   server process. Common variables:
#                     HOME:           Required on Linux for config discovery.
#                     PATH:           Extend to include Go, Python, Node bins.
#                     MCP_REC_URL:    Internal HTTP endpoint for recall connectivity.
#                     MCP_SOC_URL:    Internal HTTP endpoint for socratic connectivity.
#                     MCP_GO_BIN_PATH: Absolute path to Go binary for AST tools.
#                     PYTHONUNBUFFERED: Set to "1" for Python MCP servers.
#
#                   The orchestrator automatically injects these variables when
#                   the shared LLM backplane is enabled (do NOT set manually):
#                     MCP_LLM_ENABLED: "true" when backplane is active.
#                     MCP_LLM_ADDR:    Backplane HTTP address (e.g., 127.0.0.1:48081).
#                     MCP_LLM_TOKEN:   Bearer token for backplane authentication.
#
# disabled_tools:   (optional) Array of specific tool names to explicitly block
#                   from discovery. Use this to hide tools that are irrelevant or
#                   problematic. Example: ["search_users", "search_code"].
#
# memory_limit_mb:  (optional) Soft memory limit in MB. On Linux with cgroups
#                   configured, this enforces a hard memory ceiling. On other
#                   platforms it serves as a documented guideline.
#
# gomemlimit_mb:    (optional) Go-specific runtime memory limit. Sets the
#                   GOMEMLIMIT environment variable for Go-based servers,
#                   enabling the Go garbage collector to optimize within this
#                   budget. Has no effect on non-Go servers.
#
# max_cpu_limit:    (optional) Maximum CPU core allocation. On Linux with
#                   cgroups, this maps to GOMAXPROCS for Go servers.
#                   Default: 2.
#
# deferred_boot:    (optional) If true, the server boots lazily on first
#                   invocation instead of at orchestrator startup. Use for
#                   rarely-used servers to reduce boot time and memory.
#                   Default: false.
#
# disabled:         (optional) If true, the server is completely ignored and
#                   will not boot under any circumstances. Set to true to
#                   temporarily remove a server without deleting its config.
#                   Default: false.

servers:

  # -----------------------------------------------------------------------
  # Go MCP Servers — Brainstorm (Creative Architecture Engine)
  # -----------------------------------------------------------------------
  # The Brainstorm server provides Socratic dialectic analysis, architectural
  # diagramming, complexity forecasting, and project discovery tools.
  - name: brainstorm
    command: /usr/local/bin/mcp-server-brainstorm
    args:
      - serve
    env:
      HOME: /home/your-user
      # MCP_REC_URL enables HTTP-stream IPC between sub-servers (optional).
      # MCP_REC_URL: http://localhost:47669/mcp
    disabled_tools: []
    memory_limit_mb: 6144
    gomemlimit_mb: 4096
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true

  # -----------------------------------------------------------------------
  # Go MCP Servers — DuckDuckGo Search (Web Search Proxy)
  # -----------------------------------------------------------------------
  # Provides privacy-respecting web search capabilities via DuckDuckGo.
  # Lightweight — no LLM, no API key required.
  - name: ddg-search
    command: /usr/local/bin/mcp-server-duckduckgo
    env:
      HOME: /home/your-user
    disabled_tools: []
    memory_limit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true

  # -----------------------------------------------------------------------
  # Go MCP Servers — Evolve Plan (MADR Plan Evolution)
  # -----------------------------------------------------------------------
  # Generates and evolves architectural decision records (MADRs) through
  # iterative refinement. Integrates with Socratic Thinker for quality gates.
  - name: evolve-plan
    command: /usr/local/bin/mcp-server-evolve-plan
    env:
      HOME: /home/your-user
      # MCP_SOC_URL: http://localhost:47779/mcp
    disabled_tools: []
    memory_limit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true

  # -----------------------------------------------------------------------
  # Node.js MCP Servers — Filesystem (Sandboxed File Access)
  # -----------------------------------------------------------------------
  # The filesystem server provides sandboxed read/write access to specified
  # directories. List allowed paths in the args array below.
  - name: filesystem
    command: /usr/local/bin/mcp-server-filesystem
    args:
      # List of allowed filesystem paths the server can read/write.
      # Only paths listed here are accessible — everything else is denied.
      - /home/your-user/projects
      - /home/your-user/.config
      - /tmp
    env:
      HOME: /home/your-user
      PATH: /usr/local/bin:/usr/bin:/bin
    disabled_tools: []
    memory_limit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: true
    disabled: true

  # -----------------------------------------------------------------------
  # Python MCP Servers — Git (Version Control Operations)
  # -----------------------------------------------------------------------
  # Wraps git operations (status, diff, log, commit) via the mcp_server_git
  # Python module. Requires a Python virtual environment.
  - name: git
    command: /home/your-user/.venv/bin/python
    args:
      - -u                    # Unbuffered stdout (required for MCP JSON-RPC).
      - -m
      - mcp_server_git        # The Python module providing the MCP server.
    env:
      HOME: /home/your-user
      PATH: /usr/local/bin:/usr/bin:/bin
      PYTHONUNBUFFERED: "1"   # Belt-and-suspenders unbuffered output.
    disabled_tools: []
    memory_limit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: true
    disabled: true

  # -----------------------------------------------------------------------
  # Node.js MCP Servers — GitHub (Repository & Issue Management)
  # -----------------------------------------------------------------------
  # Official GitHub MCP server providing repo, issue, PR, and search tools.
  # Requires a GitHub Personal Access Token (PAT) with appropriate scopes.
  - name: github
    command: node
    args:
      - /path/to/node_modules/@modelcontextprotocol/server-github/dist/index.js
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: <YOUR_GITHUB_TOKEN>
    # Disable noisy or unused tools to reduce agent context consumption.
    disabled_tools:
      - search_users
      - search_code
    memory_limit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: true
    disabled: true

  # -----------------------------------------------------------------------
  # Python MCP Servers — GitLab (Repository & CI/CD Management)
  # -----------------------------------------------------------------------
  # Custom GitLab wrapper providing repository, pipeline, and merge request
  # operations. Requires GitLab instance URL and authentication.
  - name: glab
    command: /home/your-user/.venv/bin/python
    args:
      - -u
      - /home/your-user/.local/bin/glab-wrapper.py
    env:
      HOME: /home/your-user
      GITLAB_HOST: gitlab.example.com
      GITLAB_INSTANCE_URL: https://gitlab.example.com
      GL_DISABLE_STREAMING: "true"
      PYTHONUNBUFFERED: "1"
    disabled_tools: []
    memory_limit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: true
    disabled: true

  # -----------------------------------------------------------------------
  # Go MCP Servers — Go Refactor (AST-Safe Code Mutation Engine)
  # -----------------------------------------------------------------------
  # The Go Refactor server provides AST analysis, complexity scanning,
  # dead code pruning, implementation plan generation, and vetted code edits.
  # Requires Go toolchain (MCP_GO_BIN_PATH) for build validation.
  - name: go-modernizer
    command: /usr/local/bin/mcp-server-go-modernizer
    env:
      HOME: /home/your-user
      PATH: /usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
      # Absolute path to the Go binary — required for AST mutation validation.
      MCP_GO_BIN_PATH: /usr/local/go/bin/go
      # MCP_REC_URL: http://localhost:47669/mcp
      # MCP_SOC_URL: http://localhost:47779/mcp
    disabled_tools: []
    memory_limit_mb: 6144
    gomemlimit_mb: 4096
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true

  # -----------------------------------------------------------------------
  # Go MCP Servers — MagicSkills (Skill Discovery & Retrieval)
  # -----------------------------------------------------------------------
  # Provides intent-based skill discovery and full skill content retrieval
  # for the agent's skill system.
  - name: magicskills
    command: /usr/local/bin/mcp-server-magicskills
    env:
      HOME: /home/your-user
    disabled_tools: []
    memory_limit_mb: 2048
    gomemlimit_mb: 1848
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true

  # -----------------------------------------------------------------------
  # Go MCP Servers — Recall (Persistent Memory & Knowledge Base)
  # -----------------------------------------------------------------------
  # The Recall server provides persistent session memory, dialectic history
  # archival, pattern mining, and semantic search across stored knowledge.
  - name: recall
    command: /usr/local/bin/mcp-server-recall
    args:
      - serve
    env:
      HOME: /home/your-user
      MCP_GO_BIN_PATH: /usr/local/go/bin/go
      # MCP_ENDPOINT_API_PORT: "47669"
      # MCP_REC_URL: http://localhost:47669/mcp
    disabled_tools: []
    memory_limit_mb: 6144
    gomemlimit_mb: 4096
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true

  # -----------------------------------------------------------------------
  # Go MCP Servers — Sequential Thinking (Chain-of-Thought Engine)
  # -----------------------------------------------------------------------
  # Provides structured sequential reasoning capabilities for complex
  # multi-step problem solving.
  - name: seq-thinking
    command: /usr/local/bin/mcp-server-sequential-thinking
    env:
      HOME: /home/your-user
    disabled_tools: []
    memory_limit_mb: 1024
    gomemlimit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true

  # -----------------------------------------------------------------------
  # Go MCP Servers — Socratic Thinker (Dialectic Analysis Engine)
  # -----------------------------------------------------------------------
  # Provides deep Socratic dialectic analysis through thesis/antithesis/
  # synthesis cycles. Integrates with Recall for archival of dialectic chains.
  - name: socratic-thinker
    command: /usr/local/bin/mcp-server-socratic-thinker
    args:
      - serve
    env:
      HOME: /home/your-user
      # MCP_ENDPOINT_API_PORT: "47779"
      # MCP_SOC_URL: http://localhost:47779/mcp
    disabled_tools: []
    memory_limit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true
`

// defaultServersTemplateWindows is the fully-commented servers.yaml template
// tailored for Windows environments. All paths use Windows conventions
// (backslashes, C:\, .exe extensions, %USERPROFILE% references).
const defaultServersTemplateWindows = `# =========================================================================
# MAGIC TOOLS SUB-SERVER REGISTRY (servers.yaml) — Windows Edition
# =========================================================================
# This file dynamically registers and configures all downstream MCP sub-servers
# managed by the orchestrator. It is monitored via fsnotify and a 30-second
# polling fallback — changes take effect without restarting the orchestrator.
#
# Adding a server:    The orchestrator detects the addition and boots it.
# Removing a server:  The orchestrator detects the removal and shuts it down.
# Modifying a server: The orchestrator detects the config mutation, restarts
#                     the affected server, and re-indexes its tools.
#
# =========================================================================
# WINDOWS-SPECIFIC NOTES
# =========================================================================
#
# Paths:       Use forward slashes (/) or escaped backslashes (\\) in YAML.
#              Forward slashes are recommended — Go handles them natively.
#
# Env Vars:    Use $VAR or ${VAR} syntax (POSIX-style). Windows %VAR% syntax
#              is NOT supported by Go's os.ExpandEnv. Use $USERPROFILE instead
#              of %USERPROFILE%, $APPDATA instead of %APPDATA%, etc.
#
# Line Endings: This file MUST use Unix-style (LF) line endings. Most modern
#               editors (VS Code, Notepad++) handle this automatically.
#
# Executables: All Go MCP servers must use .exe extension on Windows.
#
# Memory:      memory_limit_mb has no enforcement on Windows (no cgroups).
#              It serves as a documented guideline only. gomemlimit_mb still
#              functions correctly for Go-based servers.
#
# =========================================================================
# FIELD REFERENCE
# =========================================================================
#
# name:             (required) Unique identifier for the sub-server. Used in
#                   tool URNs (e.g., "brainstorm:discover_project"), health
#                   reports, and logging. Must be lowercase alphanumeric with
#                   hyphens only.
#
# command:          (required) Absolute path to the server executable. On
#                   Windows, include the .exe extension for Go binaries.
#                   For Python servers, point to the venv's python.exe.
#                   For Node.js servers, use "node" if on PATH or the
#                   absolute path to node.exe.
#
# args:             (optional) Command-line arguments passed to the server on
#                   startup. For Go servers this is typically ["serve"]. For
#                   Python, use ["-u", "-m", "module_name"] or ["-u", "script.py"].
#                   For filesystem servers, list allowed paths here.
#
# env:              (optional) Environment variables securely injected into the
#                   server process. Common variables:
#                     USERPROFILE:     Windows home directory for config discovery.
#                     PATH:            Extend to include Go, Python, Node bins.
#                     MCP_REC_URL:     Internal HTTP endpoint for recall connectivity.
#                     MCP_SOC_URL:     Internal HTTP endpoint for socratic connectivity.
#                     MCP_GO_BIN_PATH: Absolute path to Go binary for AST tools.
#                     PYTHONUNBUFFERED: Set to "1" for Python MCP servers.
#
#                   The orchestrator automatically injects these variables when
#                   the shared LLM backplane is enabled (do NOT set manually):
#                     MCP_LLM_ENABLED: "true" when backplane is active.
#                     MCP_LLM_ADDR:    Backplane HTTP address (e.g., 127.0.0.1:48081).
#                     MCP_LLM_TOKEN:   Bearer token for backplane authentication.
#
# disabled_tools:   (optional) Array of specific tool names to explicitly block
#                   from discovery. Use this to hide tools that are irrelevant or
#                   problematic. Example: ["search_users", "search_code"].
#
# memory_limit_mb:  (optional) Soft memory limit in MB. Has NO enforcement on
#                   Windows (no cgroup support). Serves as documentation only.
#
# gomemlimit_mb:    (optional) Go-specific runtime memory limit. Sets the
#                   GOMEMLIMIT environment variable for Go-based servers,
#                   enabling the Go garbage collector to optimize within this
#                   budget. Works on Windows. Has no effect on non-Go servers.
#
# max_cpu_limit:    (optional) Maximum CPU core allocation. Maps to GOMAXPROCS
#                   for Go servers. Default: 2.
#
# deferred_boot:    (optional) If true, the server boots lazily on first
#                   invocation instead of at orchestrator startup. Use for
#                   rarely-used servers to reduce boot time and memory.
#                   Default: false.
#
# disabled:         (optional) If true, the server is completely ignored and
#                   will not boot under any circumstances. Set to true to
#                   temporarily remove a server without deleting its config.
#                   Default: false.

servers:

  # -----------------------------------------------------------------------
  # Go MCP Servers — Brainstorm (Creative Architecture Engine)
  # -----------------------------------------------------------------------
  # The Brainstorm server provides Socratic dialectic analysis, architectural
  # diagramming, complexity forecasting, and project discovery tools.
  - name: brainstorm
    command: C:/Program Files/magictools/mcp-server-brainstorm.exe
    args:
      - serve
    env:
      USERPROFILE: C:/Users/your-user
      # MCP_REC_URL enables HTTP-stream IPC between sub-servers (optional).
      # MCP_REC_URL: http://localhost:47669/mcp
    disabled_tools: []
    # memory_limit_mb has no enforcement on Windows (no cgroups).
    memory_limit_mb: 6144
    gomemlimit_mb: 4096
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true

  # -----------------------------------------------------------------------
  # Go MCP Servers — DuckDuckGo Search (Web Search Proxy)
  # -----------------------------------------------------------------------
  # Provides privacy-respecting web search capabilities via DuckDuckGo.
  # Lightweight — no LLM, no API key required.
  - name: ddg-search
    command: C:/Program Files/magictools/mcp-server-duckduckgo.exe
    env:
      USERPROFILE: C:/Users/your-user
    disabled_tools: []
    memory_limit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true

  # -----------------------------------------------------------------------
  # Go MCP Servers — Evolve Plan (MADR Plan Evolution)
  # -----------------------------------------------------------------------
  # Generates and evolves architectural decision records (MADRs) through
  # iterative refinement. Integrates with Socratic Thinker for quality gates.
  - name: evolve-plan
    command: C:/Program Files/magictools/mcp-server-evolve-plan.exe
    env:
      USERPROFILE: C:/Users/your-user
      # MCP_SOC_URL: http://localhost:47779/mcp
    disabled_tools: []
    memory_limit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true

  # -----------------------------------------------------------------------
  # Node.js MCP Servers — Filesystem (Sandboxed File Access)
  # -----------------------------------------------------------------------
  # The filesystem server provides sandboxed read/write access to specified
  # directories. List allowed paths in the args array below.
  # Note: Use forward slashes in paths — Go handles them natively on Windows.
  - name: filesystem
    command: C:/Program Files/magictools/mcp-server-filesystem.exe
    args:
      # List of allowed filesystem paths the server can read/write.
      # Only paths listed here are accessible — everything else is denied.
      - C:/Users/your-user/projects
      - C:/Users/your-user/AppData/Local/mcp-server-magictools
    env:
      USERPROFILE: C:/Users/your-user
      PATH: C:/Program Files/nodejs;C:/Program Files/magictools;C:/Windows/System32
    disabled_tools: []
    memory_limit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: true
    disabled: true

  # -----------------------------------------------------------------------
  # Python MCP Servers — Git (Version Control Operations)
  # -----------------------------------------------------------------------
  # Wraps git operations (status, diff, log, commit) via the mcp_server_git
  # Python module. Requires a Python virtual environment.
  # On Windows, point to the venv's python.exe (not python3).
  - name: git
    command: C:/Users/your-user/.venv/Scripts/python.exe
    args:
      - -u                    # Unbuffered stdout (required for MCP JSON-RPC).
      - -m
      - mcp_server_git        # The Python module providing the MCP server.
    env:
      USERPROFILE: C:/Users/your-user
      PATH: C:/Users/your-user/.venv/Scripts;C:/Program Files/Git/bin;C:/Windows/System32
      PYTHONUNBUFFERED: "1"   # Belt-and-suspenders unbuffered output.
    disabled_tools: []
    memory_limit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: true
    disabled: true

  # -----------------------------------------------------------------------
  # Node.js MCP Servers — GitHub (Repository & Issue Management)
  # -----------------------------------------------------------------------
  # Official GitHub MCP server providing repo, issue, PR, and search tools.
  # Requires a GitHub Personal Access Token (PAT) with appropriate scopes.
  - name: github
    command: node
    args:
      - C:/Users/your-user/node_modules/@modelcontextprotocol/server-github/dist/index.js
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: <YOUR_GITHUB_TOKEN>
    # Disable noisy or unused tools to reduce agent context consumption.
    disabled_tools:
      - search_users
      - search_code
    memory_limit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: true
    disabled: true

  # -----------------------------------------------------------------------
  # Python MCP Servers — GitLab (Repository & CI/CD Management)
  # -----------------------------------------------------------------------
  # Custom GitLab wrapper providing repository, pipeline, and merge request
  # operations. Requires GitLab instance URL and authentication.
  - name: glab
    command: C:/Users/your-user/.venv/Scripts/python.exe
    args:
      - -u
      - C:/Users/your-user/.local/bin/glab-wrapper.py
    env:
      USERPROFILE: C:/Users/your-user
      GITLAB_HOST: gitlab.example.com
      GITLAB_INSTANCE_URL: https://gitlab.example.com
      GL_DISABLE_STREAMING: "true"
      PYTHONUNBUFFERED: "1"
    disabled_tools: []
    memory_limit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: true
    disabled: true

  # -----------------------------------------------------------------------
  # Go MCP Servers — Go Refactor (AST-Safe Code Mutation Engine)
  # -----------------------------------------------------------------------
  # The Go Refactor server provides AST analysis, complexity scanning,
  # dead code pruning, implementation plan generation, and vetted code edits.
  # Requires Go toolchain (MCP_GO_BIN_PATH) for build validation.
  - name: go-modernizer
    command: C:/Program Files/magictools/mcp-server-go-modernizer.exe
    env:
      USERPROFILE: C:/Users/your-user
      PATH: C:/Program Files/Go/bin;C:/Program Files/magictools;C:/Windows/System32
      # Absolute path to the Go binary — required for AST mutation validation.
      MCP_GO_BIN_PATH: C:/Program Files/Go/bin/go.exe
      # MCP_REC_URL: http://localhost:47669/mcp
      # MCP_SOC_URL: http://localhost:47779/mcp
    disabled_tools: []
    memory_limit_mb: 6144
    gomemlimit_mb: 4096
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true

  # -----------------------------------------------------------------------
  # Go MCP Servers — MagicSkills (Skill Discovery & Retrieval)
  # -----------------------------------------------------------------------
  # Provides intent-based skill discovery and full skill content retrieval
  # for the agent's skill system.
  - name: magicskills
    command: C:/Program Files/magictools/mcp-server-magicskills.exe
    env:
      USERPROFILE: C:/Users/your-user
    disabled_tools: []
    memory_limit_mb: 2048
    gomemlimit_mb: 1848
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true

  # -----------------------------------------------------------------------
  # Go MCP Servers — Recall (Persistent Memory & Knowledge Base)
  # -----------------------------------------------------------------------
  # The Recall server provides persistent session memory, dialectic history
  # archival, pattern mining, and semantic search across stored knowledge.
  - name: recall
    command: C:/Program Files/magictools/mcp-server-recall.exe
    args:
      - serve
    env:
      USERPROFILE: C:/Users/your-user
      MCP_GO_BIN_PATH: C:/Program Files/Go/bin/go.exe
      # MCP_ENDPOINT_API_PORT: "47669"
      # MCP_REC_URL: http://localhost:47669/mcp
    disabled_tools: []
    memory_limit_mb: 6144
    gomemlimit_mb: 4096
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true

  # -----------------------------------------------------------------------
  # Go MCP Servers — Sequential Thinking (Chain-of-Thought Engine)
  # -----------------------------------------------------------------------
  # Provides structured sequential reasoning capabilities for complex
  # multi-step problem solving.
  - name: seq-thinking
    command: C:/Program Files/magictools/mcp-server-sequential-thinking.exe
    env:
      USERPROFILE: C:/Users/your-user
    disabled_tools: []
    memory_limit_mb: 1024
    gomemlimit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true

  # -----------------------------------------------------------------------
  # Go MCP Servers — Socratic Thinker (Dialectic Analysis Engine)
  # -----------------------------------------------------------------------
  # Provides deep Socratic dialectic analysis through thesis/antithesis/
  # synthesis cycles. Integrates with Recall for archival of dialectic chains.
  - name: socratic-thinker
    command: C:/Program Files/magictools/mcp-server-socratic-thinker.exe
    args:
      - serve
    env:
      USERPROFILE: C:/Users/your-user
      # MCP_ENDPOINT_API_PORT: "47779"
      # MCP_SOC_URL: http://localhost:47779/mcp
    disabled_tools: []
    memory_limit_mb: 1024
    max_cpu_limit: 2
    deferred_boot: false
    disabled: true
`

// defaultToolOverridesYAML is the fully-commented tool_overrides.yaml template.
const defaultToolOverridesYAML = `# ============================================================================
# MagicTools Orchestrator - Tool Description Overrides
# ============================================================================
# This configuration file allows administrators to surgically override the 
# descriptions of tools provided by downstream MCP sub-servers. 
# 
# USE CASES:
# - A sub-server provides a vague or misleading tool description.
# - You need to add specific keywords to a tool so the vector index (Bleve) 
#   routes semantic intents to it more accurately.
# - You cannot edit the source code of the external MCP server to fix it.
#
# HOT RELOAD CAPABLE:
# The orchestrator watches this file. Saving changes will instantly trigger 
# a targeted synchronization for the modified servers and re-index the 
# tuned descriptions in the vector database.
# ============================================================================

overrides:
  # The top-level key must be the exact name of the registered MCP sub-server 
  # (e.g., as defined in your servers.yaml).
  # 
  # git:
  #   # The nested key must be the exact name of the tool.
  #   commit_changes:
  #     # The overriding description text. This completely replaces the original.
  #     description: |
  #       [ROLE: MUTATOR] [DIRECTIVE: Version Control] Commits all staged changes 
  #       to the active Git repository with a standardized commit message. 
  #       This tool follows conventional commit formats and automatically computes 
  #       diff deltas for the commit log. You MUST stage files via add_files 
  #       before calling this tool. 
  #       Keywords: git commit save revision history version source code control
`
