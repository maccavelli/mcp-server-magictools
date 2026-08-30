# Configuration

MagicTools uses three YAML files. `init` creates them with extensive inline
comments; this guide explains precedence, active runtime behavior, and the
places where generated comments currently diverge from implementation.

## File locations and precedence

The primary configuration path resolves in this order:

1. `--config /path/to/config.yaml`
2. `MCP_MAGIC_TOOLS_CONFIG`
3. `MCP_MAGIC_TOOLS_CONFIG_DIR`
4. The operating system's user configuration directory

For YAML, `servers.yaml` and `tool_overrides.yaml` live beside the selected
`config.yaml`. Legacy JSON is accepted by `serve` only; its companion files
remain in the default config directory.

| OS | Default configuration directory |
| :--- | :--- |
| Linux | `~/.config/mcp-server-magictools/` |
| macOS | `~/Library/Application Support/mcp-server-magictools/` |
| Windows | `%APPDATA%\mcp-server-magictools\` |

## `config.yaml`

All application settings are nested under `configuration`. A practical lexical
search configuration is:

```yaml
configuration:
  squeezeLevel: 3
  tokenSpendThresh: 1500000
  lruLimit: 2048
  validateProxyCalls: true
  logLevel: INFO
  mcpLogLevel: INFO
  logFormat: json
  scoreThreshold: 0.3
  scoreFusionAlpha: 0.5
  strictGates: false
  vectorMinCosine: 0.72
  bm25MinNormalized: 0.15
  disableSearchFallback: true
  pinnedServers: []
  trustServers: []
  squeezeBypass: []
  ringBufferTargets: []
  intelligence:
    provider: ""
    model: ""
    api_key: ""
    vector_enabled: false
    shared_llm_enabled: false
```

The generated template is the authoritative field catalog, but read the
[known drift](#known-configuration-drift) before tuning synthesis weights.

### LLM and embedding providers

The wizard derives generative-provider choices from `mcplib` and currently
supports Gemini, Claude, OpenAI, Grok, OpenCode Zen, OpenCode Go, Hugging Face,
Kilo, and Ollama for fast/thinking tiers. Embeddings support Gemini, OpenAI,
Ollama, and Voyage.

Run:

```bash
mcp-server-magictools configure
```

The wizard can fetch live model lists, falling back to a built-in catalog when
discovery fails. It stages changes in memory and atomically writes only when
**Save and Exit** is selected. API keys written by the wizard are plaintext in
the owner-readable YAML file.

## `servers.yaml`

Each entry describes one stdio MCP child:

```yaml
servers:
  - name: example
    command: /absolute/path/to/mcp-server-example
    args:
      - serve
    env:
      EXAMPLE_SETTING: value
    disabled_tools:
      - dangerous_tool
    memory_limit_mb: 2048
    gomemlimit_mb: 1536
    max_cpu_limit: 2
    deferred_boot: true
    disabled: false
```

- `name` becomes the tool namespace, such as `example:tool_name`.
- `command` should be absolute; `args` and `env` are passed to the child.
- `$VAR` and `${VAR}` are expanded in server environment values on every OS.
- `disabled_tools` removes selected downstream tools from discovery.
- `gomemlimit_mb` injects `GOMEMLIMIT` into Go servers.
- `max_cpu_limit` maps to `GOMAXPROCS` for Go servers.
- Linux can additionally enforce memory/CPU through cgroups when available;
  other platforms treat `memory_limit_mb` primarily as metadata.
- `deferred_boot` moves startup out of the readiness-critical group, but the
  current implementation still starts it after critical boot rather than
  waiting for first invocation.
- `disabled` is honored by `wake_servers` and config-transition handling. The
  current initial boot path does not filter it, which is a known defect.

The generated file contains twelve disabled examples with placeholder paths.
Delete entries you do not need and correct paths before enabling the rest.

## `tool_overrides.yaml`

Override a discovered tool description without changing its server:

```yaml
overrides:
  example:
    tool_name:
      description: "A locally curated description."
```

Overrides are applied during indexing and hot-reloaded.

## Runtime changes and reload behavior

`config.yaml`, `servers.yaml`, and `tool_overrides.yaml` use file-system watches
plus a 30-second polling fallback. Server additions, removals, and mutations
are reconciled at runtime.

Important exceptions and side effects:

- `logFormat` requires an orchestrator restart.
- Changing `mcpLogLevel` restarts the managed fleet.
- Provider/model/vector changes can require rebuilding or rehydrating the
  vector graph.
- `update_config` exposes only the keys declared in its MCP schema, even though
  the internal updater accepts a few additional names.

The MCP-exposed keys are `logLevel`, `mcpLogLevel`, `squeezeLevel`, `logFormat`,
`scoreThreshold`, `confidenceGap`, `validateProxyCalls`, `pinnedServers`,
`trustServers`, and `squeezeBypass`.

## Environment variables

### Direct runtime variables

| Variable | Purpose |
| :--- | :--- |
| `MCP_MAGIC_TOOLS_CONFIG_DIR` | Override the configuration directory |
| `MCP_MAGIC_TOOLS_CONFIG` | Override the exact primary config path |
| `MCP_MAGIC_TOOLS_DB_PATH` | Override the Badger directory |
| `MCP_SESSION_TIMEOUT_SECONDS` | Service IDE idle timeout; default 7200 seconds |
| `MCP_SERVICE_MODE` | Select service listeners; set by service definitions |
| `MCP_ENDPOINT_IDE_PORT` | IDE HTTP bind; default `localhost:48080` |
| `MCP_ENDPOINT_LLM_PORT` | LLM bind; default `127.0.0.1:48081` |
| `MCP_ENDPOINT_ALLOW_NONLOOPBACK` | Permit an otherwise rejected remote bind |
| `MCP_REC_URL` | Recall HTTP MCP endpoint |
| `MCP_SOC_URL` | Socratic HTTP MCP endpoint |
| `GEMINI_API_KEY` | Gemini credential |
| `OPENAI_API_KEY` | OpenAI credential |
| `CLAUDE_API_KEY` | Anthropic credential |
| `VOYAGE_API_KEY` | Voyage embedding credential |
| `MAGICTOOLS_BADGER_GC_INTERVAL` | Badger value-log GC interval; default `30m` |

Viper also enables `MAGIC_TOOLS_*` overrides, replacing nested dots with
underscores. Because all YAML settings sit below `configuration`, validate any
deep override against actual startup behavior before relying on it. The example
printed by `showvars` omits that nesting and is currently misleading.

## Known configuration drift

- The generated synthesis values `.7/.3/.0` are passed through normalization
  that treats non-positive values as missing, replaces the zero role weight,
  and renormalizes all three. The effective weights therefore differ from the
  comments.
- Several normalization comments and fallback defaults disagree with the
  generated template.
- Root help labels the default log under the config directory as
  `magictools.log`; runtime actually uses the cache directory and
  `magictools_debug.log`.
- `showvars` advertises an incorrectly flattened deep Viper example.

These are audit findings, not recommended configuration behavior. Until they
are fixed, prefer the wizard and verify effective behavior through logs and the
dashboard when changing advanced ranking weights.
