# Dashboard and observability

MagicTools writes structured operational state for both humans and automation.
The terminal dashboard reads a persistent memory-mapped telemetry ring and the
configured logs without opening the live Badger datastore.

## Launch the dashboard

```bash
mcp-server-magictools dash
```

The dashboard can run alongside the orchestrator because it reads the telemetry
ring, not the single-owner primary database.

## Controls

| Key | Action |
| :--- | :--- |
| Up or `k` | Previous page |
| Down or `j` | Next page |
| Enter | Activate Quit when selected |
| `q` or Ctrl+C | Exit |

Snapshots refresh every 10 seconds. The program uses the terminal alternate
screen and attempts to enable virtual-terminal support on Windows.

## Dashboard pages

The navigation contains ten information pages plus Quit:

1. **Summary** — high-level process, fleet, search, and activity state.
2. **Fleet & Transport** — managed-server and IPC connection metrics.
3. **Orchestration & DAG** — pipeline and orchestration state.
4. **Tool Intelligence** — hydration, ranking, and search signals.
5. **Tool Analytics** — tool usage, latency, and collision information.
6. **System Backplane** — internal queues, cache, and runtime state.
7. **Storage & Databases** — Badger, Bleve, and vector-store information.
8. **LLM Backplane** — shared provider, rate, and token state.
9. **Distributed Tracing** — trace and session activity.
10. **Logging** — recent log records.

Empty or zero values can mean the subsystem is disabled, no events have been
recorded, or the orchestrator has not yet written a snapshot. Correlate the
dashboard with `/health` and logs before diagnosing a failure.

## Telemetry and log paths

| Artifact | Default location |
| :--- | :--- |
| Telemetry ring | OS cache directory, `telemetry.ring` |
| Application log | OS cache directory, `magictools_debug.log` |
| Service log on macOS | OS cache directory, `magictools_service.log` |
| Service state | OS config directory, `service.state` |

The telemetry ring is a fixed 128 MiB memory-mapped file. Payloads larger than
an individual ring slot are replaced with a truncation record. `ringBufferTargets`
in `config.yaml` selects targets for higher-fidelity tracing; it does not change
the total ring size.

## Health endpoint

Service mode exposes:

```bash
curl -i http://localhost:48080/health
```

The JSON fields are:

- `status`: `booting`, `ok`, or `degraded`.
- `uptime`: process uptime rounded to seconds.
- `servers`: configured managed-server count.
- `boot_ready`: whether the main boot attempt completed.
- `degraded`: whether one or more servers failed.
- `failed_servers`: present when failures are known.

Booting returns HTTP 503 with `Retry-After: 3`. Degraded returns 200 because
native and healthy downstream tools remain usable. `/health` is not
authenticated and should remain loopback-only.

## Service logs and status

```bash
mcp-server-magictools service status
mcp-server-magictools service status --json
mcp-server-magictools service logs --lines 100
mcp-server-magictools service logs --follow
mcp-server-magictools service doctor
```

On Linux, service logs are read through `journalctl --user`. Platform service
implementations use their native manager and the configured cache/log paths.

MCP clients can also call `get_internal_logs`, `get_session_stats`,
`get_health_report`, `analyze_system_logs`, and `self_check`.

## Historical search limitation

The CLI exposes:

```bash
mcp-server-magictools dash --find "text"
```

Current code only initializes a search-capability message; it does not perform
the query or print matching telemetry. Use the dashboard Logging page, service
logs, or MCP diagnostic tools instead.

## Troubleshooting

### The dashboard is blank

Confirm the orchestrator is running as the same OS user and that its telemetry
ring exists in the default cache directory. Path overrides for config and DB do
not relocate the cache directory.

### Data looks stale

Wait through the 10-second refresh interval. If `/health` and logs are also
stale, check service status and the PID in `service.state`.

### The layout is clipped

Increase terminal width. The left navigation has a fixed width and information
pages render beside it.

### Logs do not match CLI help

The help string is outdated. Unless `--log` overrides it, use
`magictools_debug.log` under the OS cache directory.
