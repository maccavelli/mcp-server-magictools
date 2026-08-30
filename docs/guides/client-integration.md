# Client integration

MagicTools should normally be the single MCP entry in a client. Its
`servers.yaml` registry owns downstream server processes and tool exposure.

## Choose a connection mode

| Situation | Client configuration | Required runtime |
| :--- | :--- | :--- |
| One client window | `serve` over stdio | Client-owned process |
| Multiple windows; stdio-only clients | `proxy` over stdio | Installed service |
| Client supports Streamable HTTP | `http://localhost:48080/mcp` | Installed service |

Do not run multiple direct `serve` instances against one datastore. Badger is
opened with an exclusive lock.

## Stdio configuration

Most MCP clients that use an `mcpServers` object accept this shape:

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

Windows JSON paths require escaped backslashes:

```json
{
  "mcpServers": {
    "magictools": {
      "command": "C:\\Users\\me\\AppData\\Local\\Programs\\magictools\\mcp-server-magictools.exe",
      "args": ["serve"]
    }
  }
}
```

Use the configuration location and property names documented by the client.
Those are client-owned interfaces and can change independently of MagicTools.

## Proxy configuration

First install and verify one background service. Then change only the argument:

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

The proxy waits up to 30 seconds for the service. It prefers a Unix-domain
socket or Windows named pipe and falls back to authenticated random loopback TCP
listeners described by the owner-only auth file.

## Streamable HTTP configuration

Clients with native Streamable HTTP support should use:

```text
http://localhost:48080/mcp
```

Use the client's HTTP/server-URL field rather than wrapping the URL in a stdio
command. The endpoint supports SSE event delivery and in-memory stream
resumption. Idle IDE sessions expire after two hours by default.

The IDE-facing endpoint has no bearer authentication. Keep it on loopback. Do
not expose it through port forwarding, a public reverse proxy, or a non-loopback
bind without adding a separate authenticated boundary.

## Environment variables in client configuration

Set path overrides only when the client must use non-default files:

```json
{
  "env": {
    "MCP_MAGIC_TOOLS_CONFIG": "/absolute/path/config.yaml",
    "MCP_MAGIC_TOOLS_DB_PATH": "/absolute/path/db"
  }
}
```

Provider keys may be supplied through environment variables, but embedding-key
resolution and wizard support vary by provider. Avoid checking client configs
containing secrets into source control.

Set `MCP_GO_BIN_PATH` in a managed server's `env` when that downstream tool
needs Go and the GUI process has a restricted `PATH`. MagicTools itself does not
need Go when installed from a release binary.

## Verify the service endpoint

```bash
curl -i http://localhost:48080/health
```

During boot it returns HTTP 503 with `status: "booting"`. After the fleet boot
attempt completes it returns HTTP 200 with `ok` or `degraded`, plus uptime,
server count, readiness, and failed-server fields.

## Troubleshooting

### No tools appear

Confirm the process remained running, inspect the MagicTools log, and verify the
client performed MCP initialization. Direct stdio boot work begins after the
initial protocol handshake.

### The second client fails with a datastore lock

Both clients started direct stdio instances. Install one service and configure
both clients with `proxy`, or give each direct process a separate `--db` path.

### Proxy cannot connect

Run:

```bash
mcp-server-magictools service status
mcp-server-magictools service doctor
mcp-server-magictools service logs --lines 100
```

Check that the proxy and service run as the same operating-system user; IPC and
auth artifacts are user-scoped.

### HTTP returns 503

The orchestrator has not completed its critical boot attempt. Use `/health` and
service logs to identify slow or failed downstream servers.
