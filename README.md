<!-- markdownlint-disable MD013 MD033 MD041 -->

> **Mirror notice:** This repository is a one-way published export of a
> privately hosted project. History is squashed into sync snapshots, and pull
> requests cannot be merged here directly. Open an issue instead. Changes land
> in the private source and are re-exported.

# mcp-server-magictools

MagicTools is a local Model Context Protocol (MCP) orchestrator and gateway. It
starts and supervises downstream stdio MCP servers, indexes their tools, aligns
natural-language intent to those tools, proxies calls, and exposes fleet health,
search, pipeline, and observability features through one MCP entry point.

Use the default stdio mode for one MCP client process. Use the background
service with either the stdio proxy or direct Streamable HTTP when multiple
client windows must share one datastore and one downstream fleet.

## Install

Linux or macOS:

```bash
curl -fsSL https://github.com/maccavelli/mcp-server-magictools/releases/latest/download/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://github.com/maccavelli/mcp-server-magictools/releases/latest/download/install.ps1 | iex
```

The installer verifies `SHA256SUMS` and places only the binary. It does not run
`init` or `configure`, and it does not install or start a service. Published
targets are Linux amd64, macOS arm64, and Windows amd64.

## Table of contents

- [What it does](#what-it-does)
- [I want to](#i-want-to)
- [Quick start](#quick-start)
- [Runtime model](#runtime-model)
- [Supported platforms](#supported-platforms)
- [Documentation library](#documentation-library)
- [Current limitations](#current-limitations)
- [Project verification](#project-verification)

## What it does

- Manages downstream MCP server processes from `servers.yaml`.
- Presents native MagicTools tools plus namespaced downstream tools to clients.
- Finds tools with Bleve lexical search and optional HNSW vector search.
- Executes downstream tools through a schema-validating proxy.
- Supports critical and deferred fleet boot, health monitoring, restart, and
  runtime configuration reload.
- Provides an optional multi-server DAG pipeline when Recall, Brainstorm, and
  Go Modernizer are online.
- Exposes a terminal dashboard backed by a persistent telemetry ring.
- Can provide a shared, authenticated LLM backplane to managed servers.

## I want to

| Goal | Start here |
| :--- | :--- |
| Install or upgrade the binary | [Platform installation](docs/guides/platform-installation.md) |
| Complete a first working setup | [Getting started](docs/guides/getting-started.md) |
| Connect an MCP client | [Client integration](docs/guides/client-integration.md) |
| Choose stdio, proxy, or HTTP | [Services and transports](docs/guides/services-and-transports.md) |
| Configure providers, search, or managed servers | [Configuration](docs/guides/configuration.md) |
| Look up a CLI command | [CLI reference](docs/guides/cli-reference.md) |
| Understand or invoke MCP tools | [MCP tools and orchestration](docs/guides/mcp-tools.md) |
| Inspect health, logs, or the dashboard | [Dashboard and observability](docs/guides/dashboard-and-observability.md) |
| Back up data or review security boundaries | [Operations and security](docs/guides/operations-and-security.md) |
| Read the code-grounded repository audit | [Repository assessment](docs/guides/repository-assessment.md) |

## Quick start

Initialize the three YAML files:

```bash
mcp-server-magictools init
```

Edit `servers.yaml` and replace the example paths for the downstream servers you
actually have. The generated registry contains examples, not a ready-to-run
fleet.

Optionally configure LLM and embedding providers in an interactive terminal:

```bash
mcp-server-magictools configure
```

Then point an MCP client at the binary:

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

`serve` is the default command, but keeping it explicit makes client
configuration and process listings clearer. Use an absolute executable path;
GUI clients commonly inherit a restricted `PATH`.

## Runtime model

```text
MCP client
    |
    +-- stdio: client starts `magictools serve`
    |
    +-- shared service
          +-- stdio client starts `magictools proxy`
          +-- HTTP client uses http://localhost:48080/mcp
                         |
                    MagicTools
                  /      |       \
             Badger    search    managed stdio MCP servers
             source   Bleve/HNSW
```

The Badger datastore is single-owner. Do not start two direct stdio instances
against the same `--db` path. For multiple client windows, install one
background service and connect each client through `proxy` or Streamable HTTP.

In service mode, local IPC uses a Unix-domain socket on Linux/macOS or a
user-scoped named pipe on Windows, with authenticated loopback TCP fallbacks.
The IDE-facing HTTP endpoint is intentionally unauthenticated and must remain
loopback-only.

## Supported platforms

| Target | CI execution | Release artifact |
| :--- | :---: | :---: |
| Linux amd64 | Native build, vet, test, and lint | Yes |
| macOS arm64 | Native build, vet, test, and tag smoke | Yes |
| Windows amd64 | Native build, vet, test, installer dry run, and tag smoke | Yes |

Release binaries are cgo-free and do not require Go at runtime. Go 1.26.5 is
required to build this repository. A managed server or pipeline tool may impose
its own runtime requirements; for example, Go-aware mutation tools can require
`MCP_GO_BIN_PATH`.

## Documentation library

- [Getting started](docs/guides/getting-started.md)
- [Platform installation](docs/guides/platform-installation.md)
- [Client integration](docs/guides/client-integration.md)
- [Services and transports](docs/guides/services-and-transports.md)
- [Configuration](docs/guides/configuration.md)
- [CLI reference](docs/guides/cli-reference.md)
- [MCP tools and orchestration](docs/guides/mcp-tools.md)
- [Dashboard and observability](docs/guides/dashboard-and-observability.md)
- [Operations and security](docs/guides/operations-and-security.md)
- [Repository assessment](docs/guides/repository-assessment.md)

The existing architectural decision and implementation-plan artifacts remain in
[`docs/`](docs/).

## Current limitations

- The generated `servers.yaml` contains disabled examples with placeholder
  executable paths. It must be edited for the local machine.
- The datastore lock prevents concurrent direct instances using the same data
  directory.
- `dash --find` initializes search but does not currently return query results.
- Pipeline tools are visible but return `pipeline_disabled` unless Recall,
  Brainstorm, and Go Modernizer are all online.
- The IDE-facing service endpoint has no bearer authentication. The runtime
  rejects non-loopback binds unless explicitly overridden; do not override that
  guard on an untrusted network.
- There is no first-class backup or restore command for the MagicTools
  datastore. Stop the service and copy the config and data directories.

See the [repository assessment](docs/guides/repository-assessment.md) for the
complete audit, including known CLI, configuration, and repository hygiene
issues.

## Project verification

The CI workflow runs on every branch push and pull request. Linux owns format,
module-tidiness, vet, cgo-free tests, lint, and installer tests; macOS arm64 and
Windows amd64 also build, vet, and execute the complete Go test suite natively.
Semver tags (`vX.Y.Z`) build, checksum, smoke-test, and publish all three release
targets.

Local verification:

```bash
make fmt
go vet ./...
CGO_ENABLED=0 go test ./...
make lint
make build-all
```
