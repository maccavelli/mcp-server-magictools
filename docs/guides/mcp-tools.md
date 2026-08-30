# MCP tools and orchestration

MagicTools registers 18 native tools. It also exposes tools, prompts, and
resources discovered from managed servers through namespaced routing.

## Tool discovery and execution

The normal two-step flow is:

1. Call `align_tools` with intent to obtain ranked tool URNs.
2. Call `call_proxy` with a selected URN and schema-conforming arguments.

`align_tools` combines Bleve BM25 results with HNSW semantic results when the
vector engine is available. If it receives arguments, a single sufficiently
dominant result may execute inline only when its server appears in
`trustServers`. Leaving `trustServers` empty keeps discovery and execution
separate.

`call_proxy` validates arguments against the discovered input schema by default.
Set `validateProxyCalls: false` only for diagnosing incompatible schemas.

## Fleet lifecycle tools

| Tool | Behavior |
| :--- | :--- |
| `sync_servers` | Connect and index all or selected managed servers |
| `wake_servers` | Start offline non-disabled servers and ping the fleet |
| `reload_servers` | Restart all or selected active managed processes |

Configuration watches also reconcile additions, removals, enable/disable
transitions, and changes in `servers.yaml`.

## Discovery and proxy tools

| Tool | Behavior |
| :--- | :--- |
| `align_tools` | Rank tools for natural-language intent; optionally inline-execute a trusted high-confidence match |
| `call_proxy` | Execute a namespaced downstream tool by URN |
| `list_tools` | List tools from one downstream server |
| `semantic_similarity` | Compare or index semantic content through the embedding/vector subsystem |
| `query_compliance` | Search indexed compliance artifacts; uses vector search or Recall fallback |

Downstream tools use the `server:tool` namespace in URNs. The exposed dynamic
tool list is generated from indexed schemas rather than a hard-coded catalog.

## Diagnostics and configuration tools

| Tool | Behavior |
| :--- | :--- |
| `get_internal_logs` | Query recent internal log material |
| `get_session_stats` | Report current session activity |
| `get_health_report` | Summarize fleet and subsystem health |
| `analyze_system_logs` | Analyze logs for operational patterns |
| `update_config` | Persist and apply an allow-listed runtime setting |
| `self_check` | Audit database, cache, and internal buffer state |

The schema-exposed `update_config` keys are documented in
[Configuration](configuration.md#runtime-changes-and-reload-behavior).

## Pipeline tools

| Tool | Behavior |
| :--- | :--- |
| `execute_pipeline` | Compose a dry-run DAG or continue/reject an existing pipeline session |
| `validate_pipeline_step` | Validate a proposed pipeline stage |
| `cross_server_quality_gate` | Aggregate cross-server review evidence |
| `generate_audit_report` | Produce a report from pipeline execution evidence |

All four tools are registered unconditionally, but calls are gated until
`recall`, `brainstorm`, and `go-modernizer` are all healthy. Without that trio,
they return a `pipeline_disabled` result.

The `execute_pipeline` safety flow is intentionally two-stage:

1. Submit `intent`, `target`, and `dry_run: true` to generate a paused plan.
2. Review the plan and either continue using its `session_id`, or reject it by
   sending the `session_id` with `reject: true`.

The `pipeline-start` MCP prompt instructs the client to preserve structured
artifact paths and obtain explicit user confirmation of intent, target, and
blast radius before continuing. Pipeline implementation is Go-oriented and can
invoke the Go toolchain for build validation. Set `MCP_GO_BIN_PATH` where the
managed process cannot discover `go` on `PATH`.

## Raw output resources

Large proxied results can be transformed or summarized to protect the client
context. MagicTools stores the original result in Badger and returns a URI with
this template:

```text
mcp://magictools/raw/{id}
```

Reading that resource returns the full stored output. The resource is local to
the datastore and disappears if the relevant data is wiped.

## Prompts and downstream namespaces

MagicTools owns one native prompt, `pipeline-start`. Prompt and resource
middleware also routes namespaced capabilities from downstream servers. Exact
downstream availability is runtime-dependent: it reflects enabled servers,
successful handshakes, disabled tool lists, and health state.

## Security considerations

- Treat `trustServers` as an execution authorization boundary.
- Keep schema validation enabled.
- Review a pipeline dry run before continuation.
- A proxied tool inherits the filesystem, network, and credential capabilities
  granted to its managed server process; MagicTools is not a sandbox.
- Keep the service endpoint loopback-only. See
  [Operations and security](operations-and-security.md).
