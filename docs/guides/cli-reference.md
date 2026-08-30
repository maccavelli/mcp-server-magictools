# CLI reference

## Command overview

| Command | Purpose |
| :--- | :--- |
| `serve` | Run MCP over stdio, or service listeners when service mode is set |
| `proxy` | Bridge client stdio to an installed MagicTools service |
| `init` | Create or reset the three YAML configuration files |
| `configure` | Interactively configure LLM, embedding, and backplane settings |
| `dash` | Open the terminal observability dashboard |
| `db sync` | Rebuild Bleve from Badger while MagicTools is stopped |
| `db wipe` | Delete Badger data and the Bleve index |
| `service ...` | Install and manage the OS background service |
| `showvars` | Print a partial environment-variable reference |
| `completion` | Generate shell completion through Cobra |

Running with no subcommand starts `serve`. Use `--version` when you only want a
safe executable/version check.

## Global flags

```text
--config PATH      primary config.yaml; JSON accepted for serve only
--db PATH          Badger data path
--log PATH         log file path
--debug            force TRACE logging
--log-level LEVEL  ERROR, WARN, INFO, DEBUG, or TRACE
--no-optimize      disable response minification
-v, --version      print version
```

The displayed default for `--log` is stale. The actual runtime default is
`magictools_debug.log` in the operating-system cache directory.

## `init`

```bash
mcp-server-magictools init
mcp-server-magictools init --force
mcp-server-magictools init --force --yes
```

Normal initialization writes missing files and preserves existing ones.
`--force` resets `config.yaml`, `servers.yaml`, and `tool_overrides.yaml`; it
requires terminal confirmation unless `--yes` is also supplied. JSON targets
are rejected.

## `configure`

```bash
mcp-server-magictools configure
```

The wizard requires a terminal, stages all changes, and writes atomically only
on **Save and Exit**. Although help still displays `--force` and
`--non-interactive`, both flags are rejected at runtime. Use `init --force` or
plain `init` respectively.

## `serve`

```bash
mcp-server-magictools serve
```

Default mode is stdio. The generated service definition runs this command with
`MCP_SERVICE_MODE=true`, which enables IPC and HTTP listeners. Avoid launching
it casually from a shell against the live datastore: startup enforces
single-instance state and may clean up a PID recorded by an earlier instance.

## `proxy`

```bash
mcp-server-magictools proxy
```

The proxy is for MCP clients that understand stdio but must share one service.
It waits up to 30 seconds, prefers OS-native local IPC, and uses authenticated
loopback TCP fallback when necessary.

## `dash`

```bash
mcp-server-magictools dash
mcp-server-magictools dash --find "query"
```

The first command opens the live terminal dashboard. `--find` is currently a
placeholder: it prints initialization messages but does not execute or display
a historical search.

## `db`

Stop every process using the selected data directory first.

```bash
mcp-server-magictools db sync
mcp-server-magictools db wipe
```

`db sync` reconstructs the derived Bleve index from Badger. `db wipe` destroys
both stores immediately and has no confirmation prompt. Back up first and use
an explicit `--db` when there is any ambiguity.

## `service`

```bash
mcp-server-magictools service install [--bin-path PATH] [--force]
mcp-server-magictools service status [--json]
mcp-server-magictools service start
mcp-server-magictools service stop
mcp-server-magictools service restart
mcp-server-magictools service reinstall
mcp-server-magictools service uninstall
mcp-server-magictools service logs [--lines N] [--follow]
mcp-server-magictools service doctor
```

See [Services and transports](services-and-transports.md) for platform details
and security boundaries.

## `showvars`

```bash
mcp-server-magictools showvars
```

This prints several path, timeout, and provider-key variables and masks three
known API key values. It is not exhaustive: it omits service binds, Recall and
Socratic endpoints, Voyage, Go tool resolution, and Badger GC. Its sample deep
Viper variable does not reflect the top-level `configuration` nesting. Use the
[configuration guide](configuration.md#environment-variables) as the audited
reference.

## Shell completion

Cobra provides Bash, Zsh, Fish, and PowerShell completion. For example:

```bash
mcp-server-magictools completion bash
mcp-server-magictools completion zsh
mcp-server-magictools completion fish
mcp-server-magictools completion powershell
```
