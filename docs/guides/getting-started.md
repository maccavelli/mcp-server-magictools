# Getting started

This path produces a minimal, code-grounded MagicTools setup. It assumes one
client window. For shared multi-window operation, complete the first three
steps and then switch to [service mode](services-and-transports.md).

## 1. Install the binary

Use the verified release installer:

```bash
curl -fsSL https://github.com/maccavelli/mcp-server-magictools/releases/latest/download/install.sh | sh
```

On Windows PowerShell:

```powershell
irm https://github.com/maccavelli/mcp-server-magictools/releases/latest/download/install.ps1 | iex
```

Verify without starting the server:

```bash
mcp-server-magictools --version
```

The release binary is self-contained. Go is needed only to build the repository
or by a downstream tool that invokes Go.

## 2. Initialize configuration

```bash
mcp-server-magictools init
```

This creates missing files without overwriting existing ones:

- `config.yaml` controls MagicTools.
- `servers.yaml` describes managed MCP servers.
- `tool_overrides.yaml` overrides indexed tool metadata.

Use `init --force` only to reset all three files. It prompts before overwriting;
non-interactive automation must add `--yes`.

## 3. Configure a real server

The generated `servers.yaml` is an example catalog. Its executable paths are
placeholders. Start with one server you have actually installed:

```yaml
servers:
  - name: recall
    command: /absolute/path/to/mcp-server-recall
    args:
      - serve
    env: {}
    disabled_tools: []
    memory_limit_mb: 2048
    gomemlimit_mb: 1536
    max_cpu_limit: 2
    deferred_boot: false
    disabled: false
```

Use absolute paths. `disabled: true` is intended to suppress a server, while
`deferred_boot: true` lets it start after the critical boot path. The current
initial boot path has a known defect around disabled entries; see the
[repository assessment](repository-assessment.md#configuration-findings).

## 4. Configure intelligence features

The orchestrator works with lexical search and native tools without an LLM.
Run the interactive wizard if you want description hydration, vector search, or
the shared LLM backplane:

```bash
mcp-server-magictools configure
```

Changes are staged until **Save and Exit**. Exiting without saving discards the
session. The wizard must run in a terminal.

## 5. Connect one MCP client

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

Restart or reload the MCP client. Do not also configure the same downstream
servers directly in that client; MagicTools owns their processes.

## 6. Verify operation

From the client, list tools or call `get_health_report`. A healthy installation
exposes 18 native tools, one `pipeline-start` prompt, the
`mcp://magictools/raw/{id}` resource template, and any indexed downstream tools.

For a shared service:

```bash
mcp-server-magictools service install
mcp-server-magictools service status
curl -fsS http://localhost:48080/health
```

## Common first-run failures

### The client cannot launch MagicTools

Use an absolute binary path. GUI applications frequently do not inherit the
shell's `PATH`.

### A downstream server is offline

Check its `command`, `args`, and `env` in `servers.yaml`, then inspect:

```bash
mcp-server-magictools service logs --lines 100
```

For direct stdio mode, use the configured log file instead.

### The datastore is locked

Another MagicTools instance owns the same data directory. Close duplicate
direct instances, or use one service with `proxy` for all client windows.

### Vector search is offline

Lexical Bleve search remains available. Configure a supported embedding provider
and enable vector search, or deliberately operate in lexical-only mode.

### Pipeline calls return `pipeline_disabled`

The pipeline requires three managed servers to be ready: `recall`,
`brainstorm`, and `go-modernizer`. Ordinary orchestration does not require that
trio.
