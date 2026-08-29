<!-- markdownlint-disable MD013 MD060 MD033 MD041 -->

> **Mirror notice:** This repository is a one-way published export of a
> privately hosted project. History is squashed into sync snapshots, and pull
> requests cannot be merged here directly — open an issue instead. Changes
> land in the private source and are re-exported.

# 🪄 MagicTools Orchestrator

The central backbone of the Antigravity ecosystem. A robust, fault-tolerant Model Context Protocol (MCP) orchestrator,
gateway, and autonomous execution engine.

## 🚀 Overview

`mcp-server-magictools` is the **Master Architect** of the MCP swarm. It is designed to be the **primary** MCP
server configured in your IDE. Instead of managing dozens of individual servers, you point your IDE to MagicTools,
and it manages everything else.

### 📋 Core Pillars

1. **Unified Orchestration**: Manages the lifecycle of sub-servers (e.g., `go-refactor`, `brainstorm`, `recall`).
2. **Socratic DAG Engine**: Dynamically composes multi-step execution pipelines (Directed Acyclic Graphs) with
   built-in review gates.
3. **Hybrid Intelligence**: Employs **HNSW Vector Search** and **Bleve BM25 Lexical Search** for hyper-accurate
   tool alignment.
4. **Swarm Bidding**: When a tool is requested, the orchestrator polls all sub-servers to find the most capable
   handler for the specific intent.
5. **Observability**: Real-time TUI dashboard for monitoring latencies, process health, and telemetry streams.
6. **Shared LLM Backplane**: Optionally provides a centralized HTTP LLM endpoint that all sub-servers route
   through, eliminating redundant provider connections and enabling fleet-wide token accounting.

---

## 🏗️ Architecture: How it Works

MagicTools sits between your LLM and your tool ecosystem:

1. **Intent Alignment**: When you ask "Refactor this Go function," the LLM calls `align_tools`. MagicTools uses its
   hybrid search engine to find the exact tools needed across all registered sub-servers.
2. **Pipeline Generation**: If the task is complex, `execute_pipeline` is invoked. MagicTools generates a DAG of
   tasks (e.g., scan → analyze → refactor → verify → report).
3. **Secure Proxying**: Every tool execution routes through `call_proxy`. This ensures that credentials, environment
   variables, and resource limits are enforced centrally.
4. **Circuit Breaking**: If a sub-server (like `recall`) becomes unresponsive, MagicTools isolates the failure and
   prevents it from crashing your entire IDE session.

### 🔌 Transport Modes

MagicTools supports three connection modes depending on your deployment preference:

| Mode | How It Works | Best For |
| :--- | :--- | :--- |
| **Option 1: Stdio (Default)** | IDE spawns MagicTools as a child process, communicating via stdin/stdout JSON-RPC. | Single IDE window, simplest setup. |
| **Option 2: Proxy** | MagicTools runs as a background service. IDE spawns a lightweight `proxy` subprocess that bridges stdio ↔ HTTP to the running service. | Multi-window IDE setups sharing one orchestrator. |
| **Option 3: HTTP Streamable** | MagicTools runs as a background service. IDE connects directly via HTTP using `serverUrl`. | IDEs with native HTTP/SSE MCP support (no child process). |

> **Note:** Options 2 and 3 both require MagicTools to be installed as a system service first.
> See [Service Installation](#optional-install-as-a-system-service) below.

### 🔗 Polymorphic IPC (Service Mode)

In service mode (`MCP_SERVICE_MODE=true`), the orchestrator binds **three** listener layers simultaneously:

1. **Primary IPC** — Unix Domain Socket (Linux/macOS) or Named Pipe (Windows). OS-ACL secured; no token required.
2. **IDE HTTP listener** — Binds to `MCP_ENDPOINT_IDE_PORT` (default `localhost:48080`). IDE connects
   directly here for Streamable HTTP transport. No token auth — sessions are isolated per `Mcp-Session-Id`.
3. **Dual-Stack TCP Fallback** — Random ephemeral ports on `127.0.0.1` (IPv4) and `[::1]` (IPv6).
   Bearer token required. Used by the `proxy` subcommand when the primary IPC socket is unavailable.

Connection details (ports + tokens) are written atomically to the auth file on startup
(see [Auth Token File Location](#auth-token-file-location)).

### 🔀 Proxy Multiplexer

The `proxy` subcommand bridges stdio (IDE) ↔ HTTP service using a **Polymorphic IPC Multiplexer**:

- **ProxyLedger**: Central connection ledger mapping client session IDs to dedicated routing channels.
- **Inbound Mapping**: Rewrites JSON-RPC `id` fields using a `clientID::originalID` delimiter to allow
  concurrent multi-IDE sessions without cross-contamination.
- **Outbound Filter**: Demultiplexes HTTP responses back to the correct IDE channel by parsing the rewritten ID.
- **Atomic Purging**: On EOF/disconnect, the ledger entry is atomically purged — no residual state.
- **Auto-Healing**: If the service restarts and returns HTTP 401, the proxy automatically re-reads the auth
  file and retries with the new token (one attempt).
- **64KB Buffer Pool**: `sync.Pool`-backed `bytes.Buffer` reuse on the hot relay path eliminates per-call
  allocation overhead.

---

## ⚠️ Prerequisites

### Go Toolchain Required

> **The `execute_pipeline` tool's AST mutation and build validation stage requires the Go
> toolchain to be installed.** It calls `go build` to validate proposed code changes
> before committing them to disk.
>
> Download and install Go from the official archive: **[https://go.dev/dl/](https://go.dev/dl/)**
>
> Minimum required version: **Go 1.26+** (Go 1.26.5 recommended)

When MagicTools runs as a subprocess under your IDE, it may inherit a **stripped `PATH`**
with no `go` binary available. Set `MCP_GO_BIN_PATH` in your IDE's server config to
point directly at your Go installation (see [IDE Configuration](#6-ide-configuration) for examples).

---

## 🛠️ Getting Started: Full Installation Walkthrough

This walkthrough covers the complete path from downloading the binary to having MagicTools
running in your IDE on **Linux**, **macOS**, or **Windows**.

### One-line install

**Linux or macOS:**

```bash
curl -fsSL https://github.com/maccavelli/mcp-server-magictools/releases/latest/download/install.sh | sh
```

**Windows PowerShell:**

```powershell
irm https://github.com/maccavelli/mcp-server-magictools/releases/latest/download/install.ps1 | iex
```

The installer verifies the release checksum and only places the executable in
`~/.local/bin` on Linux/macOS or
`%LOCALAPPDATA%\Programs\magictools` on Windows. It does not run `configure`,
initialize configuration, or install/start the background service.

### 1. Download the Binary

Download the latest release for your platform from the project's release page, or build from source:

**From release:**

```bash
# Linux (amd64)
curl -L -o mcp-server-magictools https://github.com/maccavelli/mcp-server-magictools/releases/latest/download/mcp-server-magictools-linux-amd64
chmod +x mcp-server-magictools

# macOS (Apple Silicon)
curl -L -o mcp-server-magictools https://github.com/maccavelli/mcp-server-magictools/releases/latest/download/mcp-server-magictools-darwin-arm64
chmod +x mcp-server-magictools

# Windows (amd64) — download mcp-server-magictools.exe from the release page
```

**From source:**

```bash
git clone <repository-url>
cd mcp-server-magictools
make build
# Binary: ./dist/mcp-server-magictools
```

### 2. Place in PATH

Move the binary to a directory in your system `PATH`:

**Linux:**

```bash
mv mcp-server-magictools ~/.local/bin/
# Ensure ~/.local/bin is in your PATH (add to ~/.bashrc or ~/.zshrc if needed):
# export PATH="$HOME/.local/bin:$PATH"
```

**macOS:**

```bash
mv mcp-server-magictools ~/.local/bin/
# Or use Homebrew's bin directory:
# mv mcp-server-magictools /opt/homebrew/bin/
```

**Windows (PowerShell):**

```powershell
Move-Item mcp-server-magictools.exe "$env:LOCALAPPDATA\Programs\magictools\mcp-server-magictools.exe"
# Add to PATH via System Settings > Environment Variables, or:
# [Environment]::SetEnvironmentVariable("PATH", "$env:PATH;$env:LOCALAPPDATA\Programs\magictools", "User")
```

### 3. Initialize Configuration

Run the `init` command to generate the default configuration files:

```bash
mcp-server-magictools init
```

**What this does:**

- Creates the configuration directory:
  - **Linux**: `~/.config/mcp-server-magictools/`
  - **macOS**: `~/Library/Application Support/mcp-server-magictools/`
  - **Windows**: `%APPDATA%\mcp-server-magictools\`
- Writes two template files:
  - **`config.yaml`** — Orchestrator settings (logging, search weights, LLM providers, backplane config).
  - **`servers.yaml`** — Sub-server registry defining all downstream MCP servers managed by the orchestrator.
- Safe to re-run — existing files are **never overwritten** unless `--force` is passed.

> **Tip:** If you need to reset to defaults, run `mcp-server-magictools init --force`.
> You will be prompted to confirm before any files are overwritten.

### 4. Run the Configuration Wizard

Run the interactive wizard to configure your LLM providers:

```bash
mcp-server-magictools configure
```

The wizard presents a menu with five options. Each can be configured independently, and
changes are saved to `config.yaml` immediately after each selection:

```text
=== MagicTools Configuration Wizard ===

  1) Fast Tier LLM        — Primary model for hydration & intelligence
  2) Thinking Tier LLM    — Dedicated model for deep reasoning (optional)
  3) Embedding Engine     — Vector search model for semantic alignment
  4) Shared LLM Backplane — Centralized LLM service for sub-servers
  5) Show Current Config  — Display active configuration
  0) Exit
```

#### Option 1: Fast Tier LLM

The primary LLM used for tool description hydration, search intelligence, and general-purpose generation.

**Steps:**

1. **Select provider** — Choose from Gemini (recommended), Claude (Anthropic), or OpenAI.
2. **API key resolution** — The wizard checks three sources in priority order:
   - Environment variable (e.g., `GEMINI_API_KEY`, `OPENAI_API_KEY`, `CLAUDE_API_KEY`)
   - Existing key in `config.yaml` from a previous run
   - Manual entry (input is masked on supported terminals)
3. **Model selection** — The wizard fetches available models from your provider's API in real-time.
   If the API is unreachable, a curated static fallback list is presented. Select from the list or
   enter a custom model name.
4. **Fallback models** — All remaining models from the list are saved as automatic fallbacks for retry logic.
5. **Retry defaults** — Sets retry count (2), retry delay (5s), and timeout (120s) if not already configured.

#### Option 2: Thinking Tier LLM (Optional)

A dedicated model for deep reasoning tasks (Socratic analysis, complex code review). If not configured,
the Fast Tier handles all requests.

- Supports a **different provider** than the Fast Tier (e.g., Gemini for fast, Claude for thinking).
- **Cross-tier key reuse** — If both tiers use the same provider, the wizard offers to reuse the existing API key.
- Select `0) None/Clear` to disable the thinking tier entirely.

#### Option 3: Embedding Engine

Configures the vector embedding model used by the HNSW search engine for semantic tool alignment.

- **Providers**: Gemini, Voyage, OpenAI, or Ollama (local).
- **Model + Dimensions**: Each model option shows its dimensionality. The wizard auto-sets the correct value.
- **HNSW rebuild warning**: If you change the embedding dimensionality, the wizard warns that the vector
  index will be rebuilt on next server start.

#### Option 4: Shared LLM Backplane

Enables a centralized HTTP LLM endpoint that all sub-servers route through. This eliminates redundant
API connections and provides fleet-wide token accounting.

- **Requires**: The Fast Tier LLM must be configured first.
- **Configurable parameters**: LLM port (default: 48081), max concurrent requests (4), max RPM (30),
  burst rate (5/sec), sub-server token threshold (500K), orphan stream TTL (5 min).

#### Option 5: Show Current Config

Displays a formatted summary of all configured tiers, providers, models, keys (masked), and backplane settings.

### Optional: Install as a System Service

If you want to use **Proxy mode** (Option 2) or **HTTP Streamable mode** (Option 3) for IDE connectivity,
you must install MagicTools as a background service:

```bash
mcp-server-magictools service install
```

**What this does per platform:**

| OS | Mechanism | Service File Location |
| :--- | :--- | :--- |
| **Linux** | `systemd --user` | `~/.config/systemd/user/mcp-server-magictools.service` |
| **macOS** | `launchd` (LaunchAgent) | `~/Library/LaunchAgents/com.<your_username>.mcp-server-magictools.plist` |
| **Windows** | Task Scheduler (ONLOGON) | `%APPDATA%\mcp-server-magictools\magictools-service.cmd` |

The service starts automatically and is configured to restart on failure. The command is **idempotent** —
safe to re-run without duplicating entries.

**Verify the service is running:**

```bash
mcp-server-magictools service status

# Platform-native checks:
# Linux:   systemctl --user status mcp-server-magictools
# macOS:   launchctl print gui/$(id -u)/com.<your_username>.mcp-server-magictools
# Windows: schtasks /Query /TN MagicToolsService
```

**Uninstall the service:**

```bash
mcp-server-magictools service uninstall
```

### 6. IDE Configuration

MagicTools should be the **only** MCP server in your IDE configuration. It manages all sub-servers internally.

Choose one of the three connection modes below based on your needs.

---

#### Option 1: Stdio Mode (Default — Simplest)

The IDE spawns MagicTools as a child process. No service installation required.

**Antigravity / Gemini** — `~/.gemini/mcp_config.json` (Linux/macOS) or `%APPDATA%\Gemini\antigravity\mcp_config.json` (Windows):

```json
{
  "mcpServers": {
    "magictools": {
      "command": "/home/your-user/.local/bin/mcp-server-magictools",
      "args": ["serve"],
      "env": {
        "HOME": "/home/your-user",
        "PATH": "/usr/local/go/bin:/usr/bin:/bin",
        "MCP_GO_BIN_PATH": "/usr/local/go/bin/go"
      }
    }
  }
}
```

**Claude Desktop** — `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "magictools": {
      "command": "/absolute/path/to/mcp-server-magictools",
      "args": ["serve"],
      "env": {
        "HOME": "/Users/your-user",
        "MCP_GO_BIN_PATH": "/opt/homebrew/bin/go"
      }
    }
  }
}
```

**VSCode (Roo Code / Cline)** — `~/.config/Code/User/globalStorage/rooveterinaryinc.roo-cline/settings/cline_mcp_settings.json` (Linux) or `%APPDATA%\Code\User\globalStorage\rooveterinaryinc.roo-cline\settings\cline_mcp_settings.json` (Windows):

```json
{
  "mcpServers": {
    "magictools": {
      "command": "/absolute/path/to/mcp-server-magictools",
      "args": ["serve"]
    }
  }
}
```

**JetBrains IDEs** — `~/.config/JetBrains/AI/mcp.json` (Linux), `~/Library/Application Support/JetBrains/AI/mcp.json` (macOS), or `%APPDATA%\JetBrains\AI\mcp.json` (Windows). Also configurable via Settings > Tools > AI Assistant > MCP Servers.

```json
{
  "mcpServers": {
    "magictools": {
      "command": "/absolute/path/to/mcp-server-magictools",
      "args": ["serve"]
    }
  }
}
```

*Windows: Use double backslashes in paths, e.g., `"C:\\Users\\User\\.local\\bin\\mcp-server-magictools.exe"`.*

---

#### Option 2: Proxy Mode (Service Required)

The IDE spawns a lightweight `proxy` subprocess that bridges stdio ↔ HTTP to the running service.
This allows multiple IDE windows to share a single orchestrator instance.

**Requires:** `mcp-server-magictools service install` completed first
(see [Optional: Install as a System Service](#optional-install-as-a-system-service)).

**Antigravity / Gemini:**

```json
{
  "mcpServers": {
    "magictools": {
      "command": "/home/your-user/.local/bin/mcp-server-magictools",
      "args": ["proxy"],
      "env": {
        "HOME": "/home/your-user"
      }
    }
  }
}
```

**Claude Desktop:**

```json
{
  "mcpServers": {
    "magictools": {
      "command": "/absolute/path/to/mcp-server-magictools",
      "args": ["proxy"]
    }
  }
}
```

**VSCode / JetBrains:**

```json
{
  "mcpServers": {
    "magictools": {
      "command": "/absolute/path/to/mcp-server-magictools",
      "args": ["proxy"]
    }
  }
}
```

> **How it works:** The `proxy` subcommand automatically discovers the running service via
> Polymorphic IPC (Unix Domain Socket → TCP fallback). It waits up to 30 seconds for the
> service to become reachable. Each IDE window gets an independent session (~4KB overhead)
> while sharing the same sub-server backplane.

---

#### Option 3: HTTP Streamable Mode (Service Required — Direct Connection)

The IDE connects directly to the running service via HTTP. No child process is spawned.
This requires an IDE that supports the MCP Streamable HTTP transport with `serverUrl`.

**Requires:** `mcp-server-magictools service install` completed first
(see [Optional: Install as a System Service](#optional-install-as-a-system-service)).

**Antigravity / Gemini:**

```json
{
  "mcpServers": {
    "magictools": {
      "serverUrl": "http://localhost:48080/mcp"
    }
  }
}
```

**Claude Desktop:**

```json
{
  "mcpServers": {
    "magictools": {
      "serverUrl": "http://localhost:48080/mcp"
    }
  }
}
```

**VSCode / JetBrains:**

```json
{
  "mcpServers": {
    "magictools": {
      "serverUrl": "http://localhost:48080/mcp"
    }
  }
}
```

> **Important:** The port (`48080`) must match the `MCP_ENDPOINT_IDE_PORT` value configured
> in the service. The default is `localhost:48080`. If you changed it, update the URL accordingly.
>
> **No authentication is required** for the IDE HTTP listener — sessions are isolated per
> `Mcp-Session-Id` header. The listener only binds to `localhost` by default for security.

### 7. Verification

Verify your setup by launching the TUI dashboard:

```bash
mcp-server-magictools dash
```

You should see the status of the internal orchestrator handlers and all sub-servers as "ONLINE".

---

## 💻 CLI Command Reference

### Core Commands

| Command | Usage | Description |
| :--- | :--- | :--- |
| `serve` | `mcp-server-magictools serve` | Starts the MCP server. In stdio mode (default), communicates via stdin/stdout. In service mode (`MCP_SERVICE_MODE=true`), starts the HTTP listeners. |
| `init` | `mcp-server-magictools init [--force]` | Generates default `config.yaml` and `servers.yaml` in the config directory. Safe to re-run; existing files preserved unless `--force` is used. |
| `configure` | `mcp-server-magictools configure` | Interactive setup wizard for LLM providers, embedding engine, and shared backplane. |
| `dash` | `mcp-server-magictools dash [--find "query"]` | Real-time TUI dashboard for health, telemetry, and fleet monitoring. Use `--find` to search historical telemetry. |
| `proxy` | `mcp-server-magictools proxy` | Stdio-to-HTTP bridge for IDEs using Polymorphic IPC. Requires the service to be running. |

### Database Commands

| Command | Usage | Description |
| :--- | :--- | :--- |
| `db wipe` | `mcp-server-magictools db wipe` | Purges all local caches and indices. Requires the orchestrator to be stopped first. |
| `db sync` | `mcp-server-magictools db sync` | Forces a re-index of all registered toolsets from BadgerDB into Bleve. Requires the orchestrator to be stopped first. |

### Server Registry Commands

| Command | Usage | Description |
| :--- | :--- | :--- |
| `servers list` | `mcp-server-magictools servers list` | Lists all sub-servers managed by the orchestrator. |

### Service Lifecycle Commands

| Command | Usage | Description |
| :--- | :--- | :--- |
| `service install` | `mcp-server-magictools service install` | Installs and starts the orchestrator as a background service. Idempotent — safe to re-run. |
| `service uninstall` | `mcp-server-magictools service uninstall` | Stops, disables, and removes the service definition and state files. |
| `service status` | `mcp-server-magictools service status` | Reads `service.state` and verifies the recorded PID is still alive. |

### Global Flags

| Flag | Description |
| :--- | :--- |
| `--config <path>` | Override path to IDE `mcp_config.json`. |
| `--db <path>` | Override path to BadgerDB data directory. |
| `--log <path>` | Override path to log file. |
| `--log-level <level>` | Set log level: `ERROR`, `WARN`, `INFO`, `DEBUG`, `TRACE`. |
| `--debug` | Enable full trace logging (forces TRACE level). |
| `--no-optimize` | Disable SqueezeWriter and description minification. |
| `-v`, `--version` | Print version info and exit. |

---

## ⚡ Boot Sequence & Deferred Boot

The orchestrator uses a **partitioned concurrent boot** strategy:

1. **Critical servers** (all servers without `deferred_boot: true` in `servers.yaml`) boot in parallel with a
   concurrency limit of 10. Each has a 60-second handshake timeout.
2. **Deferred servers** (`deferred_boot: true`) start in background *after* the critical-path boot completes.
   They do not block IDE readiness — useful for heavyweight servers (e.g., `github`, `glab`) that aren't needed
   immediately.
3. **Internal tools** (the orchestrator's own MCP tools) boot concurrently alongside external servers.
4. On completion, `IsSynced` is atomically set to `true`, lifting the readiness gate (HTTP 503 → 200).

```yaml
# servers.yaml example — defer non-critical servers
servers:
  - name: github
    deferred_boot: true
```

---

## 🩺 Health Endpoint

In service mode, a `/health` endpoint is available on all listeners:

```text
GET /health
```

Returns JSON with orchestrator status:

```json
{
  "status": "ok",          // "ok" | "booting" | "degraded"
  "uptime": "2m34s",
  "servers": 5,
  "boot_ready": true,
  "degraded": false,
  "failed_servers": []     // present only when degraded
}
```

- **`booting`** (HTTP 503): Boot sequence not yet complete. The readiness middleware also returns
  503 on all `/mcp` endpoints during this phase with a `Retry-After: 3` header.
- **`degraded`** (HTTP 200): Boot complete but one or more sub-servers failed. Orchestrator remains functional.

---

## 🔒 Graceful Shutdown

The orchestrator installs `SIGTERM` / `SIGINT` handlers that:

1. Cancel the master context to stop accepting new connections.
2. Kill all tracked child sub-server processes.
3. Remove the `service.state` file.
4. **Gracefully shut down the primary HTTP server** with a **30-second hard deadline** via
   `http.Server.Shutdown()`. If sub-servers don't terminate within the deadline, the process
   exits anyway.

> **Windows note:** `TerminateProcess` does not propagate to child PIDs — step 2 explicitly
> iterates and kills all tracked processes.

**Unix signal behavior:**

| Signal | Effect |
| :--- | :--- |
| `SIGINT` | Graceful shutdown |
| `SIGTERM` | Graceful shutdown |
| `SIGHUP` | Graceful shutdown (Unix only; used by systemd for reload) |

---

## 💡 Best Practices

- **Single Point of Entry**: Never configure sub-servers like `go-refactor`
  directly in your IDE. Always route through MagicTools.
- **Warm the Cache**: After adding new sub-servers to `servers.yaml`, run
  `sync_ecosystem` via your IDE to update the search index.
- **Monitor Latency**: Use the `dash` command during heavy refactoring tasks to
  ensure sub-servers are responding efficiently.
- **Context Management**: MagicTools automatically truncates long outputs to
  stay within LLM context limits while preserving important metadata headers.
- **Service Mode for Teams**: Use `service install` + `proxy` mode for
  multi-window IDE setups. Each IDE window gets an independent session
  (~4KB overhead) while sharing the same sub-server backplane.

---

## 🌍 Environment Variable Reference

All environment variables are optional. Values shown are defaults.

### Connection & Transport

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MCP_ENDPOINT_IDE_PORT` | `localhost:48080` | Bind address for the Streamable HTTP service (service mode). Accepts bare port (`48080`), `host:port` (`localhost:48080`), or IPv6 (`[::1]:48080`). Non-loopback addresses are blocked unless `MCP_ENDPOINT_ALLOW_NONLOOPBACK=true` is also set. |
| `MCP_ENDPOINT_ALLOW_NONLOOPBACK` | `false` | Set to `true` to allow binding on a non-loopback interface (e.g., `0.0.0.0:48080`). **Security warning:** this exposes all MCP tools to the network; only use in isolated environments with firewall rules. Also applies to the LLM backplane listener. |
| `MCP_SERVICE_MODE` | `false` | Set to `true` by the service wrapper to activate HTTP service mode and suppress the parent-PID watchdog. Set automatically by `service install` — do not set manually. |
| `MCP_SESSION_TIMEOUT_SECONDS` | `86400` | Idle session timeout (seconds) for IDE-facing HTTP-stream connections (service mode only). Sessions persist for the service lifetime by default (24h). The SDK's POST ref-counting pauses the timer during active tool calls, so only idle gaps between requests are counted. |
| `MCP_API_URL` | *(not set)* | Comma-separated list of recall server HTTP endpoints (e.g., `http://127.0.0.1:3333`). When set, the orchestrator establishes a persistent HTTP streaming client to recall. If not set, MagicTools runs in standalone mode with recall features disabled. |

### Storage & Paths

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MCP_CONFIG_DIR` | `~/.config/mcp-server-magictools` (Linux/macOS) | Override the configuration directory. |
| `XDG_RUNTIME_DIR` | *(set by systemd)* | If set, the auth token file is written here (`$XDG_RUNTIME_DIR/magictools_auth.json`) — a kernel-managed 0700 `tmpfs` on systemd Linux, providing the most secure token storage. Takes precedence over `UserCacheDir`. |
| `MCP_GO_BIN_PATH` | *(auto-detected from PATH)* | Absolute path to the Go binary. Required for AST mutation validation in `go-refactor` and `recall`. Propagated to all Go-based sub-servers. |

### Shared LLM Backplane

The LLM backplane provides a centralized HTTP LLM endpoint that all sub-servers can route through, eliminating
redundant API connections and enabling fleet-wide token accounting.

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MCP_ENDPOINT_LLM_PORT` | `48081` | Override the TCP port for the shared LLM backplane HTTP listener. The backplane binds to `127.0.0.1` only (loopback). Set in `config.yaml` via `intelligence.llm_port` or override with this env var. |

The backplane exposes three endpoints (authenticated via a scoped bearer token distinct from the IPC token):

| Endpoint | Auth Required | Description |
| :--- | :--- | :--- |
| `POST /llm/generate` | ✅ Bearer token | Submit a generation request to the shared LLM pool. |
| `POST /llm/generate-thinking` | ✅ Bearer token | Submit a thinking-mode generation request. |
| `GET /llm/status` | ❌ No auth | Health probe for sub-server connectivity checks. |

### Sub-Server Environment (Orchestrator-Injected)

These variables are **automatically injected** by the orchestrator into every managed
sub-server process. Do **not** set these manually in `servers.yaml` — they are managed
by the orchestrator lifecycle.

| Variable | Injected When | Description |
| :--- | :--- | :--- |
| `MCP_LLM_ENABLED` | Shared LLM backplane is active | Set to `"true"` to signal that the orchestrator provides shared LLM services. |
| `MCP_LLM_ADDR` | Shared LLM backplane is active | HTTP address of the LLM backplane (e.g., `127.0.0.1:48081`). |
| `MCP_LLM_TOKEN` | Shared LLM backplane is active | Bearer token for authenticating requests to the LLM backplane. |
| `MCP_SERVER_NAME` | Always | The sub-server's registered name from `servers.yaml` (e.g., `brainstorm`). |
| `MCP_LOG_LEVEL` | Always | The `mcpLogLevel` value from `config.yaml`, propagated to sub-servers. |

### Performance & Tuning

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MAGICTOOLS_BADGER_GC_INTERVAL` | `30m` | BadgerDB value-log GC frequency. Accepts any `time.ParseDuration` value (e.g. `5m`, `1h`). |

### Auth Token File Location

1. `$XDG_RUNTIME_DIR/magictools_auth.json` — kernel-managed tmpfs, Linux systemd only
2. `$UserCacheDir/magictools_auth.json` — `~/Library/Caches/` on macOS, `%LOCALAPPDATA%` on Windows
3. `$TMPDIR/magictools_auth.json` — fallback (non-systemd Linux, unusual environments)

The file is always written with **0600 permissions** (owner-readable only). It contains the
IPC TCP ports (IPv4 + IPv6), the IPC bearer token, and the LLM backplane port and token when
the backplane is active.

---

## 📝 Configuration Files & Tunables Reference

MagicTools utilizes two central YAML configuration files, stored in your configuration folder. Both support **hot-reloading** via `fsnotify` changes applied at runtime without restarting (except where noted).

---

### 1. `config.yaml` — Central Orchestrator Settings

Governs limits, logging, search weights, LLM providers, and shared backplane settings.

| Parameter | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| **System Constraints** | | | |
| `squeezeLevel` | `int` | `3` | Concurrency limit during Socratic DAG generation. Range: `0` (minified/linear) to `5` (highly concurrent). |
| `tokenSpendThresh` | `int` | `1500000` | Max allowed token budget per operation cycle before circuit breaker throtlling. |
| `lruLimit` | `int` | `2048` | Max items retained in internal Least Recently Used memory caches. |
| `validateProxyCalls`| `bool`| `true` | Enforces JSON Schema validation on tools executed via `call_proxy`. |
| **Observability** | | | |
| `logLevel` | `string`| `DEBUG` | Logging verbosity for the orchestrator (`ERROR`, `WARN`, `INFO`, `DEBUG`, `TRACE`). |
| `mcpLogLevel` | `string`| `INFO` | Verbosity for downstream sub-servers (triggers fleet-wide restart on change). |
| `logFormat` | `string`| `json` | Format for stdout/file logging (`json` or `text`). *Requires restart.* |
| **Search & Intents** | | | |
| `scoreThreshold` | `float`| `0.3` | Minimum score for a tool to survive `align_tools` (Range `0.0` - `1.0`). |
| `scoreFusionAlpha` | `float`| `0.5` | Blend weight. `0.0` = pure lexical (Bleve), `1.0` = pure semantic (Vector). |
| `corroborationBonus`| `float`| `0.05` | Score bonus when both search legs match a tool. |
| `reliabilityBoost` | `float`| `0.15` | Multiplier applied to historical sub-server reliability score. |
| `usageBoost` | `float`| `0.02` | Multiplier applied to tool usage counts (`log1p` scaled). |
| `nativeBoost` | `float`| `0.10` | Additive boost for native/orchestrator-owned tools. |
| `synthesisBiasVector`| `float`| `0.7` | Synthesis bias for Vector Similarity. *Must sum to 1.0 with Synergy/Role.* |
| `synthesisBiasSynergy`| `float`| `0.3` | Synthesis bias for Synergy Index (structural tool mappings). |
| `synthesisBiasRole` | `float`| `0.0` | Synthesis bias for Role-based overrides. |
| `confidenceGap` | `float`| `0.4` | Score gap between match #1 and #2 required for auto-execution inline. |
| `strictGates` | `bool` | `false` | Enable confidence floors on search legs before fusion. |
| `vectorMinCosine` | `float`| `0.72` | Minimum cosine similarity required to keep the vector leg in search. |
| `bm25MinNormalized` | `float`| `0.15` | Minimum Bleve BM25 score required to keep the lexical leg in search. |
| `disableSearchFallback`|`bool`| `true` | Suppress substring search fallback when scores miss threshold. |
| **Ecosystem Tuning** | | | |
| `squeezeBypass` | `list` | `[]` | Excludes specified tools/servers (e.g. `["server:tool"]`) from minification. |
| `ringBufferTargets` | `list` | `[]` | Targets tracked with high-fidelity execution tracing in diagnostic buffers. |
| `pinnedServers` | `list` | `[]` | Sub-servers immune to idle sleep eviction. |
| `trustServers` | `list` | `[]` | Sub-servers authorized for inline execution in `align_tools`. |
| **Intelligence (LLM)**| | | |
| `intelligence.provider`| `string`| *(none)* | Fast Tier LLM provider (`gemini`, `openai`, `claude`). |
| `intelligence.model`| `string`| *(none)* | Fast Tier model name (e.g. `gemini-2.5-flash`). |
| `intelligence.fallback_models`| `list`| `[]` | Ordered models to fall back to if the primary model fails. |
| `intelligence.retry_count`| `int` | `2` | Retry attempts for transient LLM API calls. |
| `intelligence.retry_delay_seconds`|`int`| `5` | Delay between retries in seconds. |
| `intelligence.timeout_seconds`| `int`| `120` | Timeout for a single LLM API invocation. |
| `intelligence.thinking_provider`|`string`| *(none)* | Provider for thinking tier reasoning. |
| `intelligence.thinking_model`|`string`| *(none)* | Model for thinking tier reasoning. |
| **LLM Backplane** | | | |
| `intelligence.shared_llm_enabled`|`bool`|`false`| centralizes LLM routing for sub-servers through local HTTP endpoint. |
| `intelligence.llm_port`| `int` | `48081` | TCP port for backplane loopback listener. |
| `intelligence.max_concurrent_requests`|`int`|`4` | Concurrency limit for upstream LLM API requests. |
| `intelligence.max_rpm`| `int` | `30` | Rate limit (requests per minute) to upstream provider. |
| `intelligence.max_burst_per_second`|`int`|`5` | Burst capacity for short request spikes. |
| `intelligence.sub_server_token_thresh`|`int`|`500000`| Cumulative token rate limit per sub-server before throttling. |
| `intelligence.orphan_stream_ttl_minutes`|`int`|`5` | Timeout for inactive/orphaned LLM stream connections. |
| **Vector Search** | | | |
| `intelligence.vector_enabled`| `bool`| `false` | Enable vector search logic. |
| `intelligence.embedding_provider`| `string`| *(none)* | Embedding model provider (`gemini`, `openai`, `voyage`, `ollama`). |
| `intelligence.embedding_model`| `string`| *(none)* | Model name for spatial embeddings. |
| `intelligence.embedding_dimensionality`| `int`| `0` | Dimension length of embeddings (critical for HNSW graph init). |

---

### 2. `servers.yaml` — Sub-Server Registry

Defines the downstream MCP sub-servers managed by the orchestrator.

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `name` | `string` | Yes | Unique lowercase alphanumeric identifier (used in tool URN prefixes). |
| `command` | `string` | Yes | Absolute path to the server executable (or python interpreter). |
| `args` | `list` | No | Command-line arguments passed during sub-server spawn. |
| `env` | `map` | No | Environment variables securely injected into the sub-process. |
| `disabled_tools` | `list` | No | Sub-server tools to explicitly block from discovery index. |
| `memory_limit_mb` | `int` | No | Soft memory ceiling (cgroups enforced on Linux). |
| `gomemlimit_mb` | `int` | No | Sets `GOMEMLIMIT` environment variable for Go sub-servers. |
| `max_cpu_limit` | `int` | No | CPU core allocation cap (maps to `GOMAXPROCS` on Linux). |
| `deferred_boot` | `bool` | No | If `true`, boots the sub-server lazily on its first tool call. |
| `disabled` | `bool` | No | If `true`, ignores the sub-server definition completely. |

---

### 3. `service.state` — Runtime State File

Written dynamically to the configuration directory upon service start, mapping the PID, binding addresses, config version, and execution path.

```json
{
  "pid": 12345,
  "addr": "localhost:48080",
  "started": "2026-05-20T03:00:00Z",
  "config_version": "1.5.0",
  "binary_path": "/usr/local/bin/mcp-server-magictools"
}
```

---

## 🖥️ Platform Notes

### macOS — SIP & Code Signing

On **macOS Ventura and later** with System Integrity Protection (SIP) enabled,
`launchctl` may silently refuse to load a LaunchAgent for an unsigned binary
located in a user-controlled directory (e.g. `~/.local/bin`).

If `service install` succeeds but the agent does not start:

```bash
codesign -s - ~/.local/bin/mcp-server-magictools
launchctl unload ~/Library/LaunchAgents/com.<your_username>.mcp-server-magictools.plist
launchctl load -w ~/Library/LaunchAgents/com.<your_username>.mcp-server-magictools.plist
```

An **ad-hoc signature** (`-s -`) is sufficient — no Apple Developer certificate is required.

### Windows Server — Headless / SSH Sessions

The default Task Scheduler trigger is `ONLOGON`, which fires on interactive
user logon only. On **Windows Server** where the service must start on boot
without an interactive session:

```bat
:: Run as Administrator after service install
schtasks /Change /TN MagicToolsService /RU SYSTEM /RL HIGHEST
```

Service logs are written to `%LOCALAPPDATA%\mcp-server-magictools\magictools_service.log`.

### Linux — File Descriptor Limits

On startup, the orchestrator automatically raises the open file descriptor soft limit to
a minimum of **4096** via `setrlimit(RLIMIT_NOFILE)`. This is required to handle large
numbers of concurrent sub-server connections and HTTP sessions without hitting OS defaults.
No manual `ulimit` configuration is needed for typical deployments.

---

*Built with ❤️ for the Next Generation of Agentic Coding.*
