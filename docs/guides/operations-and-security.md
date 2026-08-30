# Operations and security

MagicTools is a local orchestration boundary, not a process sandbox. Managed
servers retain whatever filesystem, network, executable, and credential access
their OS process receives.

## Storage layout

| Artifact | Location | Role |
| :--- | :--- | :--- |
| `config.yaml` | Config directory | Runtime and provider configuration |
| `servers.yaml` | Config directory | Managed process registry and environment |
| `tool_overrides.yaml` | Config directory | Curated tool descriptions |
| `db/` | Config directory | Badger source data, Bleve index, vector files, lock/PID state |
| `service.state` | Config directory | Service PID, bind, version, and binary path |
| `telemetry.ring` | Cache directory | 128 MiB persistent telemetry ring |
| `magictools_debug.log` | Cache directory | Default application log |
| `magictools_auth.json` | Runtime/cache/temp directory | IPC ports and bearer tokens |

Badger is the source of truth for indexed tool and runtime records. Bleve and
the HNSW graph are derived search structures. A legacy `~/.mcp_magictools`
directory is moved to the platform-native data path when the new path does not
already exist.

## File and secret protection

Generated YAML and the auth file are written with owner-only file modes where
the operating system supports Unix permissions. The wizard can store provider
API keys directly in `config.yaml`; these are plaintext at rest. The datastore
is also not configured for encryption at rest.

Practical controls:

- Keep the config, cache, and runtime directories readable only by the owning
  user.
- Prefer environment or service-manager secret injection when operationally
  appropriate, while remembering environment values are inherited by child
  processes.
- Do not commit populated configuration or client files.
- Limit each managed server's `env` to what it needs.
- Treat the machine account as the primary trust boundary and use full-disk
  encryption when data-at-rest confidentiality matters.

## Local transport boundary

Primary Unix sockets and Windows named pipes use OS access controls. Random TCP
fallbacks require a bearer token read from `magictools_auth.json`. The shared
LLM generation endpoints use a different scoped token.

The IDE-facing HTTP `/mcp` endpoint and `/health` endpoint have no bearer
authentication. The runtime forces non-loopback binds back to loopback unless
`MCP_ENDPOINT_ALLOW_NONLOOPBACK=true`, but enabling that variable does not add
TLS, authentication, or authorization. Never expose this listener directly to
an untrusted network.

## Process ownership and datastore locking

One MagicTools process may own a Badger directory. A second direct stdio process
will fail to open it. The supported multi-client topology is one background
service with client proxies or direct local HTTP connections.

Startup also reads PID state and attempts to terminate a stale recorded
orchestrator process before opening the datastore. Do not copy live PID/lock
state into another active environment, and do not launch ad hoc `serve`
commands against a service-owned data path.

## Backup and restore

There is no MagicTools backup/export CLI. Use a cold filesystem backup:

1. Stop the service or close the direct MCP client process.
2. Confirm no `mcp-server-magictools serve` process owns the datastore.
3. Copy the complete configuration directory.
4. Copy the cache directory only if telemetry and logs must be preserved.
5. Restart the service.

Restore while MagicTools is stopped. Restore the three YAML files and complete
`db/` directory as a matched set. If the derived Bleve index is damaged but
Badger is intact, run:

```bash
mcp-server-magictools db sync
```

That command must have exclusive access to the datastore.

## Destructive maintenance

```bash
mcp-server-magictools --db /absolute/path/to/db db wipe
```

`db wipe` removes Badger and Bleve data without an interactive confirmation
gate. Always stop the service, back up, and provide an explicit path. Vector
artifacts inside the selected data tree can also be lost.

`init --force` is separately destructive to configuration, but it prompts or
requires `--yes` in automation.

## Managed-server trust

- `disabled_tools` hides specific downstream tools.
- `validateProxyCalls` rejects arguments that do not match discovered schemas.
- `trustServers` permits high-confidence inline execution from `align_tools`.
  Keep it empty unless that behavior is specifically wanted.
- `deferred_boot` changes startup timing, not permissions.
- Memory and CPU fields are not a complete sandbox. Enforcement varies by OS
  and server runtime.

Review absolute executable paths and dependencies before enabling a generated
server example. Environment expansion uses the orchestrator's environment and
can disclose inherited values to children if configured carelessly.

## Service hardening

Generated service definitions:

- run as the current user on Linux and macOS;
- use a real auto-start Windows SCM service;
- keep network listeners on loopback by default;
- restart after failures;
- reserve 35 seconds for the application's 30-second graceful drain.

Linux user linger allows processes to survive logout and should be enabled only
when compatible with local policy. Windows service installation requires
Administrator and therefore deserves extra scrutiny of the binary path.

## Incident response

1. Stop the service.
2. Preserve the config directory, cache directory, and service-manager logs.
3. Rotate provider keys stored in YAML or exposed to managed children.
4. Remove `magictools_auth.json`; it is regenerated with new tokens on startup.
5. Audit `servers.yaml`, service definitions, and the installed binary path.
6. Reinstall from a verified release if binary integrity is uncertain.

Deleting only the auth file while the service is live can interrupt proxies;
coordinate token removal with a service restart.
