# Services and transports

## Runtime modes

MagicTools has one server command with two runtime forms:

- Normal `serve` uses stdio and is owned by the MCP client.
- `serve` with `MCP_SERVICE_MODE=true` opens service listeners. The generated
  service definition sets this variable.

The `proxy` command remains a stdio MCP process but relays its traffic to the
service. Do not set `MCP_SERVICE_MODE=true` manually in a normal client entry.

## Install the background service

Initialize and review configuration first, then run:

```bash
mcp-server-magictools service install
```

The installer prompts for a binary path unless `--bin-path` is supplied. Use
`--force` to replace an existing definition during an upgrade.

Platform implementations are:

| OS | Service manager | Installed identity |
| :--- | :--- | :--- |
| Linux | systemd user service | `mcp-server-magictools.service` |
| macOS | launchd LaunchAgent | `com.magictools.mcp-server-magictools` |
| Windows | Service Control Manager | `MagicToolsService` |

Linux needs an active user D-Bus/systemd session. Enabling user linger is an
administrator or local policy decision. Windows service installation requires
Administrator privileges.

## Service lifecycle

```bash
mcp-server-magictools service status
mcp-server-magictools service start
mcp-server-magictools service stop
mcp-server-magictools service restart
mcp-server-magictools service logs --lines 100
mcp-server-magictools service logs --follow
mcp-server-magictools service doctor
mcp-server-magictools service reinstall
mcp-server-magictools service uninstall
```

Use `service status --json` for automation. `service doctor` reports diagnostic
issues but currently does not return a failing exit status solely because it
found them.

## Listener layout

Service mode binds several local interfaces:

1. Primary IPC is `/tmp/magictools.sock` on Unix or a user-scoped named pipe on
   Windows. File/pipe permissions provide the access boundary.
2. Internal fallback listeners use random IPv4 and IPv6 loopback ports. MCP
   requests require a bearer token and are rate-limited to 100 requests/second
   with a burst of 200.
3. The IDE listener defaults to `localhost:48080`, serving `/mcp` and `/health`.
   It has a readiness gate but no authentication.
4. If enabled, the shared LLM backplane defaults to `127.0.0.1:48081`.

Connection metadata and scoped tokens are atomically written to
`magictools_auth.json` with owner-only permissions. The proxy reads this file;
clients should not need to.

## IDE Streamable HTTP

The `/mcp` endpoint uses SSE responses, an in-memory 10 MiB event store, and a
two-hour idle session timeout. Set `MCP_SESSION_TIMEOUT_SECONDS` to a positive
integer to override the timeout.

The listener accepts a bare port or host-and-port through
`MCP_ENDPOINT_IDE_PORT`. Non-loopback hosts are rewritten to loopback unless
`MCP_ENDPOINT_ALLOW_NONLOOPBACK=true`.

That override removes only the bind safeguard; it does not add authentication.
Treat enabling it as unsafe unless an authenticated, encrypted, access-controlled
boundary is added outside MagicTools.

## Shared LLM backplane

When enabled in `config.yaml`, service mode exposes:

| Endpoint | Authentication |
| :--- | :--- |
| `POST /llm/generate` | Scoped bearer token |
| `POST /llm/generate-thinking` | Scoped bearer token |
| `GET /llm/status` | None |

The token is distinct from the internal MCP token. MagicTools injects connection
details into managed servers. `MCP_ENDPOINT_LLM_PORT` overrides the bind address
using the same loopback guard as the IDE listener.

## Readiness and shutdown

`/health` returns 503 until critical fleet boot completes. Failed downstream
servers then produce a 200 `degraded` state because MagicTools can still serve
native and surviving tools.

Shutdown drains the IDE listener before stopping managed servers and removing
service state. The application budget is 30 seconds; generated systemd,
launchd, and Windows service definitions allow 35 seconds before forced
termination.

On Unix, the service definition uses `SIGHUP` as a reload signal. Configuration
files also have file-system watches and a polling fallback, so ordinary edits do
not require sending a signal.
