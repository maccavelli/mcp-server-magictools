---
status: accepted
date: 2026-11-25
decision-makers: repository owner
consulted: "0014-REPORT-mcp-tool-schema-and-description-adherence.md (the audit this record answers);
the MCP 2026-07-28 schema vendored in go-mcp-lib (schema/inputs/2026-07-28/schema.json);
go-sdk v1.6.1 mcp/protocol.go and tool.go; mcplib v1.4.1 diagnostic.go; this repository at 184370e;
the owner's selection of 2026-11-25: options 1, 2, 3 and 7 of the report, one MADR, separate plans"
informed: "go-mcp-lib (its generated Tool surface carries every field adopted here); the cross-repository coordinator"
---

# Carry the MCP standard's title, annotations and constraint keywords on every own tool, and stop dropping them at registration

## Context and Problem Statement

[0014-REPORT](../reports/0014-REPORT-mcp-tool-schema-and-description-adherence.md) audited the 18 own tools
against the MCP 2026-07-28 schema and found the baseline compliant (root `type: object`,
`additionalProperties: false`, descriptions on 47 of 48 properties) but the standard's optional surface
entirely absent: zero `title`, zero `annotations`, zero `$schema`, and constraints expressed as prose where
JSON Schema keywords exist. The owner selected four of the report's eight adoption options on 2026-11-25 —
annotations (1), constraint keywords (2), titles (3), explicit `$schema` (7) — as the work that can proceed
now, without go-mcp-lib, and transition smoothly when it lands. This record decides all four; each is
implemented under its own PLAN, sharing this number.

Three facts make this one decision rather than four:

1. **One drop point kills three of the four.** `addTool` (`internal/handler/handlers.go:232-236`) maps
   `InternalTool` → `mcp.Tool` with only `Name`, `Description`, `InputSchema`. The `InternalTool` struct
   (`handlers.go:29-34`) has no field for `title` or `annotations`. Even a perfect inventory could not
   reach the wire. The static fallback `staticInventory` (`internal/handler/list_tools_handler.go:85-102`)
   drops the same fields the same way. Titles and annotations are one plumbing fix with two payloads;
   `$schema` and constraint keywords ride inside `inputSchema`, which already passes through.
2. **The SDK already carries every field.** go-sdk v1.6.1's `mcp.Tool` (`mcp/protocol.go:1295-1348`)
   has `Title`, `Annotations *ToolAnnotations`, `OutputSchema`, `Icons`, `Meta`. Nothing here waits for
   go-mcp-lib; go-mcp-lib's generated surface aliases the same shapes (`protocol/aliases.go`), so the
   inventory literals and the annotation values carry over unchanged when it lands.
3. **Silence is a false advertisement.** The standard's `ToolAnnotations` defaults are
   `readOnlyHint: false`, `destructiveHint: true`, `idempotentHint: false`, `openWorldHint: true`
   (schema `#/$defs/ToolAnnotations`). An unannotated tool presents to every client as a destructive,
   non-idempotent, open-world writer. Eight of the 18 tools are read-only or additive diagnostics.
   The fleet's own permission layering (gobble's allow/ask rules) is exactly the kind of client that
   benefits from truthful hints — while the standard itself warns hints are never trust decisions.

### Evidence and defect inventory (at `184370e`)

| ID | Severity | Observed implementation and consequence |
| --- | --- | --- |
| S1 | High | `InternalTool` (`internal/handler/handlers.go:29-34`) carries `Name`, `Description`, `Category`, `InputSchema` only. `addTool` (`:232-236`) and `staticInventory` (`internal/handler/list_tools_handler.go:93-97`) map only those three spec fields. `title` and `annotations` cannot reach the wire even if the inventory held them. |
| S2 | High | No tool declares `annotations`. All 18 present with the spec defaults (destructive, non-idempotent, open-world). `get_session_stats`, `get_health_report`, `self_check`, `list_tools`, `query_compliance`, `analyze_system_logs`, `get_internal_logs`, `semantic_similarity` (without `artifact_path`) are read-only or additive; `update_config`, `reload_servers`, `sync_servers`, `execute_pipeline`, `call_proxy` mutate. None of it declared. |
| S3 | Medium | Constraints are prose, not keywords. `analyze_system_logs.lines`: "default 50" in text, clamp at 1000 in the handler (`internal/handler/diagnostic_handlers.go:179-185`), no `minimum`/`maximum`/`default`. `list_tools.options.max_tools`: no bounds; handler default 1000 (`diagnostic_handlers.go:436-440`). `list_tools.options` itself has no description (the one missing property description in the inventory). |
| S4 | Medium | `update_config.key`'s enum lists 10 keys (`inventory.go`); `Config.UpdateConfigValue` (`internal/config/config.go:924-1057`) accepts 14: the 10 plus `vector_enabled`, `ringBufferTargets`, `tokenSpendThresh`, `lruLimit`. The schema under-advertises the handler; a client reading the enum cannot set four supported keys. The handler's runtime-patch switch (`diagnostic_handlers.go:276-306`) handles 13 of the 14 — `vector_enabled` is applied in memory by `UpdateConfigValue` but absent from the patch switch, a persistence gap this record notes and does not fix (out of scope; a separate defect). |
| S5 | Medium | `get_internal_logs` is registered by `mcplib.RegisterDiagnosticTool` (`internal/handler/diagnostic_handlers.go:21`), bypassing `addTool` and the inventory. Its wire schema comes from mcplib (`mcplib@v1.4.1/diagnostic.go:104-118`): `max_lines` carries `"default": 0` while the description says "Defaults to 25" — the default keyword contradicts the prose and the behaviour (`diagnostic.go:136-141`: `<=0` → 25). The inventory's own `get_internal_logs` entry (`inventory.go:49+`) is a second, divergent copy used by the fallback and align paths. Neither copy carries `minimum`/`maximum`. |
| S6 | Low | No tool declares `$schema`. The standard defaults to JSON Schema 2020-12 when absent, so this is compliant-by-default; explicit declaration removes validator ambiguity at zero behavioural cost. |
| S7 | Low | `OnConfigReloaded` (`handlers.go:136-166`) refreshes `h.InternalTools` but does not re-register with the SDK; the base `tools/list` keeps serving startup registrations while the fallback and align paths serve the refreshed inventory. Any field added to the inventory must be added to both emission paths or the two layers disagree. |

### What exists to build on (verified at `184370e`)

* `mcp.Tool` fields: `Title string`, `Annotations *ToolAnnotations` with `ReadOnlyHint`,
  `DestructiveHint`, `IdempotentHint`, `OpenWorldHint` (all `*bool`), `Title` — go-sdk v1.6.1
  `mcp/protocol.go:1295-1360`. Pointer booleans: an unset hint is absent from the wire, and the client
  applies the spec default; a set hint is explicit. The inventory should set all four hints on every
  tool, so no default is ever inferred.
* `Server.AddTool` (low-level) does no schema validation of its own beyond marshalling
  (`mcp/server.go:219-229`, 0005's E10); the sanitiser (`util.SanitizeToolSchema`) runs before it
  (`handlers.go:243`). The sanitiser touches `type`/`properties` only (`internal/util/schema_util.go:39-64`);
  it does not strip `$schema`, `title`, `annotations`, `minimum`, `maximum`, `default` or `enum`.
  Verified: `sanitizeMap` reads and writes only `schemaKeyType` and `schemaKeyProperties`.
* The inventory is parsed twice: at construction (`handlers.go:92`) and on config reload
  (`handlers.go:141`). Both unmarshal into `[]*InternalTool`; new fields with `omitempty` JSON tags
  round-trip without touching either call site.
* `align_tools` reads `it.InputSchema` (`internal/handler/align_tools_helpers.go:144`) and the schema
  formatter reads `description`/`enum`/`type`/`required` from property maps
  (`internal/handler/schema_formatter_helpers.go:17-45`). Added keywords pass through untouched; the
  formatter's enum rendering (`(One of: …)`) starts working for `update_config.key`'s completed enum
  with no formatter change.
* `SyncNativeTools` (`internal/client/sync_manager.go:29`) re-parses `InternalToolsInventoryJSON` into
  `mcp.ListToolsResult` (`sync_handlers.go:46-48`) — `mcp.Tool` unmarshalling picks up `title` and
  `annotations` automatically once the JSON carries them.
* The hydrator's LLM payload is `Name`/`Description`/`Category`/`Server` only
  (`internal/intelligence/hydrator.go:401-406`); annotations and titles do not enter the semantic
  index. No routing behaviour changes.
* The inventory guard test (`internal/handler/inventory_test.go:8-39`) pins the count at 18 and parses
  the whole inventory; it is the natural home for the new per-tool assertions.
* go-mcp-lib's MADR 0002 decides typed registration with schema diagnostics before serving; its
  generated `Tool` aliases carry `title`/`annotations`. The values decided here are facts about
  handlers, not SDK artifacts; they survive the swap. The registration call sites (`addTool`,
  `staticInventory`) are the only code that changes at adoption.

### Per-tool behaviour, measured (the annotation table's evidence)

Read at `184370e`; "writes" means any persistent or process-state mutation, including telemetry counters.

| Tool | Handler evidence | Behaviour class |
| --- | --- | --- |
| sync_servers | `sync_handlers.go:24-60`: `SyncEcosystem`, `SyncNativeTools` — rewrites stored tool records | mutates local store; repeat sync converges (idempotent effect); closed world (configured servers) |
| wake_servers | `sync_handlers.go:167-200`: `Registry.Connect` for offline servers; skips online | additive process state; repeat call no-op; closed world |
| reload_servers | `sync_handlers.go:228-266`: `DisconnectAll` + `SyncEcosystem`, or selective restart | destructive to live sessions (kills them); repeat call converges; closed world |
| get_internal_logs | mcplib `diagnostic.go:133-141`: reads ring buffer | read-only; closed world |
| get_session_stats | `diagnostic_handlers.go:33-41`: reads telemetry | read-only; closed world |
| get_health_report | `diagnostic_handlers.go:111-165`: `PingAll` (sends MCP pings, updates latency/health state), reads status | reads + refreshes health state; additive, non-destructive; closed world |
| analyze_system_logs | `diagnostic_handlers.go:168-200`: tails log file, filters | read-only; closed world |
| align_tools | `proxy_handlers.go:97-130`: search + optional inline execution of any resolved tool; `triggerAutoHeal` may reindex | discovery is read-only; inline execution inherits the target tool's effects — open world, potentially destructive |
| call_proxy | `proxy_handlers.go:219+`: executes any downstream tool; `UpdateToolUsage` (`:242`) | inherits the target tool's effects — open world, potentially destructive |
| update_config | `diagnostic_handlers.go:253-320`: `UpdateConfigValue` + `ConfigStore.Apply` | mutates persisted config; same key+value converges (idempotent effect); closed world |
| self_check | `diagnostic_handlers.go:332-360`: reads metrics, cache, diagnostics | read-only; closed world |
| list_tools | `diagnostic_handlers.go:418-450`: reads store/inventory | read-only; closed world |
| semantic_similarity | `semantic_similarity_handler.go:17-60`: reads store; **writes an artifact file when `artifact_path` is set** (`:52-56`) | read-only without `artifact_path`; additive file write with it; closed world |
| query_compliance | `diagnostic_handlers.go:43-110`: Recall search + vector search | read-only; closed world |
| execute_pipeline | `execute_pipeline.go:35-57` + run helpers: composes and runs DAGs; MUTATOR stages write files, git checkpoints | potentially destructive; not idempotent (re-run re-executes); open world (arbitrary target) |
| validate_pipeline_step | `pipeline_tools.go:395-430`: reads standards, scans output text | read-only; closed world |
| cross_server_quality_gate | `pipeline_tools.go:460-500`: Recall reads, approval checks | read-only; closed world |
| generate_audit_report | `generate_audit_report.go:16-60`: `git diff` (read), Recall reads; `auditRecordEdgeLearning` (`audit_report_helpers.go:159-175`) writes synergy weights | reads + additive learning writes; non-destructive; closed world |

## Decision Drivers

* Clients must see truthful tool metadata: a read-only diagnostic must not present as a destructive
  writer, and a schema must not under-advertise the handler's accepted inputs.
* Every adopted field must exist in go-sdk v1.6.1 today and in go-mcp-lib's generated surface tomorrow;
  nothing here may need rework at the swap beyond the registration call site.
* The inventory is the single source of truth for own-tool metadata; both emission paths (registration
  and static fallback) must carry the same fields, or the layers disagree (S7).
* Hints are hints: the standard forbids trust decisions on `ToolAnnotations` from untrusted servers.
  The values recorded here describe this server's own behaviour, measured from handlers, and are
  maintained as facts, not aspirations.
* No behaviour change to any handler. This record changes what tools *say*, not what they *do*. The
  one exception is the enum completion (S4), which widens the advertised input set to match the
  handler's existing acceptance — a schema fix, not a handler change.
* Constraint keywords must match measured handler behaviour exactly; a keyword that contradicts the
  handler is worse than prose.

## Considered Options

* A. One MADR (this record), four PLANs sharing its number: annotations; constraint keywords + enum
  completion; titles; explicit `$schema`. Shared plumbing (`InternalTool` fields, `addTool`,
  `staticInventory`) lands in the first plan; the rest are inventory-only.
* B. Four separate MADRs, one per option.
* C. One MADR and one plan covering all four.
* D. Wait for go-mcp-lib and adopt its typed registration, skipping the interim plumbing.

## Decision Outcome

Chosen option: "A. One MADR, four PLANs sharing its number", because the four options share one drop
point and one guard-test shape, the owner asked for separate plans, and the skill's naming rule lets
several plans share a MADR's number with slugs of their own.

### Decision

**D1 — Annotations (PLAN slug: `annotate-own-tools-with-behaviour-hints`).**

1. `InternalTool` gains `Annotations *mcp.ToolAnnotations` (JSON `annotations,omitempty`); `addTool`
   and `staticInventory` map it. All four hints are set explicitly on every tool — no inferred defaults.
2. The annotation values are the table below, derived from the measured behaviour above. Where a tool's
   effects depend on arguments (`align_tools`, `call_proxy`, `semantic_similarity`), the hints describe
   the worst case the tool can reach, because a hint cannot branch on arguments.
3. A guard test pins each tool's annotation set against this table; a handler change that alters a
   tool's behaviour class must update the table and the test in the same commit.

| Tool | readOnlyHint | destructiveHint | idempotentHint | openWorldHint |
| --- | --- | --- | --- | --- |
| sync_servers | false | false | true | false |
| wake_servers | false | false | true | false |
| reload_servers | false | true | true | false |
| get_internal_logs | true | — | — | false |
| get_session_stats | true | — | — | false |
| get_health_report | false | false | true | false |
| analyze_system_logs | true | — | — | false |
| align_tools | false | true | false | true |
| call_proxy | false | true | false | true |
| update_config | false | false | true | false |
| self_check | true | — | — | false |
| list_tools | true | — | — | false |
| semantic_similarity | false | false | true | false |
| query_compliance | true | — | — | false |
| execute_pipeline | false | true | false | true |
| validate_pipeline_step | true | — | — | false |
| cross_server_quality_gate | true | — | — | false |
| generate_audit_report | false | false | true | false |

"—" means the hint is omitted: `destructiveHint` and `idempotentHint` are meaningful only when
`readOnlyHint == false` (schema text). Read-only tools set `readOnlyHint: true` and `openWorldHint: false`
and omit the other two. Rationale for the non-obvious rows:

* `reload_servers` is `destructiveHint: true` because it kills live sessions (`DisconnectAll`); the
  spec's destructive/additive distinction is about the environment, and a killed session is a destroyed
  thing. `idempotentHint: true` because a second reload converges to the same connected state.
* `get_health_report` is not read-only: `PingAll` sends MCP pings and updates health/latency state.
  Additive, non-destructive, converging.
* `semantic_similarity` is not read-only: `artifact_path` writes a file. Additive (a new artifact),
  idempotent (same content rewritten), closed world.
* `generate_audit_report` is not read-only: `auditRecordEdgeLearning` writes synergy weights. Additive,
  converging per session, closed world.
* `align_tools` and `call_proxy` take the worst case of any tool they can execute: destructive,
  non-idempotent, open world. Discovery-only calls are a subset; the hint cannot know.
* `update_config` is `destructiveHint: false`: it overwrites a config value, which the spec's
  additive/destructive line treats as an update, not a destruction; the old value is returned and the
  store transaction rolls back on failure. `idempotentHint: true`: same key+value converges.

**D2 — Constraint keywords and enum completion (PLAN slug: `express-handler-constraints-as-schema-keywords`).**

1. `analyze_system_logs.lines`: `"minimum": 1, "maximum": 1000, "default": 50` — matching
   `diagnostic_handlers.go:179-185` (`>0` honoured, clamp 1000, default 50). The description keeps its
   prose; the keywords are the machine-readable form.
2. `list_tools.options`: add a description ("Optional listing controls."); `options.max_tools`:
   `"minimum": 1, "default": 1000` — matching `diagnostic_handlers.go:436-440`. No maximum: the handler
   has none.
3. `update_config.key`: enum completed to the 14 keys `UpdateConfigValue` accepts
   (`config.go:929-1052`): the existing 10 (`logLevel`, `mcpLogLevel`, `squeezeLevel`, `logFormat`,
   `scoreThreshold`, `confidenceGap`, `validateProxyCalls`, `pinnedServers`, `trustServers`,
   `squeezeBypass`) plus `vector_enabled`, `ringBufferTargets`, `tokenSpendThresh`, `lruLimit`.
   The description states the per-key value rules that a flat schema cannot branch on (the 0013 rule):
   `squeezeLevel` 0–5, `scoreThreshold`/`confidenceGap` 0.0–1.0, `logFormat` json|text,
   `tokenSpendThresh` ≥ 0, `lruLimit` ≥ 1, booleans accept true/1/yes/on and false/0/no/off.
4. `get_internal_logs`: the inventory copy gains `"minimum": 0, "maximum": 1000, "default": 25` on
   `max_lines`, matching mcplib's measured behaviour (`diagnostic.go:136-141`: `<=0` → 25, clamp 1000).
   The mcplib-registered wire copy keeps its contradictory `"default": 0` until go-mcp-lib replaces
   mcplib; this record does not patch a dependency. The divergence is recorded (S5) and the inventory
   copy is the one the fallback and align paths serve. A follow-up against mcplib or go-mcp-lib fixes
   the wire copy; noted, not scheduled here.
5. No keyword is added that the handler does not enforce. Where the handler clamps rather than rejects
   (`lines`, `max_lines`), the keyword documents the accepted range; the description says values outside
   it are clamped, not refused.

**D3 — Titles (PLAN slug: `give-every-own-tool-a-human-readable-title`).**

1. `InternalTool` gains `Title string` (JSON `title,omitempty`); `addTool` and `staticInventory` map it.
2. Every tool gets a title: short, human-readable, no domain jargon, per the spec's `title` intent
   ("optimized to be human-readable … even by those unfamiliar with domain-specific terminology").
   The titles are UI labels; `name` stays the programmatic id; `description` stays the model-facing
   hint. Proposed titles (the PLAN may refine wording, not meaning):

| Tool | Title |
| --- | --- |
| sync_servers | Refresh server tool index |
| wake_servers | Start sleeping servers |
| reload_servers | Restart servers |
| get_internal_logs | Read gateway logs |
| get_session_stats | Show session metrics |
| get_health_report | Show server health |
| analyze_system_logs | Filter gateway logs by severity |
| align_tools | Find the right tool |
| call_proxy | Run a downstream tool |
| update_config | Change a gateway setting |
| self_check | Run gateway self-diagnostics |
| list_tools | List a server's tools |
| semantic_similarity | Find overlapping tools |
| query_compliance | Search coding standards |
| execute_pipeline | Run an analysis pipeline |
| validate_pipeline_step | Check a pipeline step's output |
| cross_server_quality_gate | Check pipeline readiness |
| generate_audit_report | Build a pipeline audit report |

**D4 — Explicit `$schema` (PLAN slug: `declare-json-schema-2020-12-on-every-own-tool`).**

1. Every own tool's `inputSchema` gains
   `"$schema": "https://json-schema.org/draft/2020-12/schema"`. The standard defaults to 2020-12 when
   absent; this makes the draft explicit for validators at zero behavioural cost.
2. The sanitiser passes `$schema` through untouched (verified: it reads/writes only `type` and
   `properties`). The SDK's low-level `AddTool` does not validate the draft; the inventory's schemas
   use only 2020-12-compatible keywords.
3. `get_internal_logs`'s mcplib-registered wire copy is out of reach (S5); the inventory copy carries
   `$schema` like the rest.

**Sequencing.** The shared plumbing (`InternalTool` fields, `addTool`, `staticInventory`, the guard
test's shape) lands in D1's plan, the first executed. D3's plan reuses the `Title` field added there.
D2 and D4 are inventory-only and may run in any order after D1. Each plan carries its own execution
record and commits.

### Consequences

* Good, because clients see truthful hints: eight tools stop presenting as destructive writers.
* Good, because the schema advertises what the handler accepts (14 config keys, not 10) and the
  constraints a client can check before calling.
* Good, because every field exists in both SDK generations; the go-mcp-lib swap changes call sites,
  not values.
* Good, because the guard test makes the metadata a maintained contract: a behaviour change that
  falsifies a hint fails the build.
* Neutral, because `category` stays internal routing metadata, dropped at registration as today; the
  spec has no category field and `_meta` is not adopted here.
* Bad, because hints are worst-case for argument-dependent tools (`align_tools`, `call_proxy`): a
  discovery-only `align_tools` call presents as potentially destructive. The spec offers no
  per-argument hints; the alternative (read-only hints) would be a lie for inline execution.
* Bad, because `get_internal_logs`'s wire copy keeps mcplib's contradictory default until the
  dependency is replaced; the inventory copy is correct, the wire copy is not, and the divergence is
  now documented rather than hidden.
* Bad, because four plans touching one inventory file serialize on it; the sequencing rule (D1 first)
  is the mitigation.

### Confirmation

1. A guard test parses the inventory and asserts, per tool: the four annotation hints match D1's table
   (or are omitted where the table says "—"); `title` is non-empty; `inputSchema.$schema` is the
   2020-12 URI; the constraint keywords match D2's values; `update_config.key`'s enum is exactly the
   14-key set.
2. An emission test registers a handler as the binary does and reads `tools/list`: every own tool
   carries its `title` and `annotations` on the wire, and the static fallback carries the same.
3. The existing suite stays green; no handler behaviour changes (the enum completion widens the
   advertised set only).
4. A planted break per assertion (each plan's own): a falsified hint, a dropped `title` at `addTool`,
   a missing `$schema`, a reverted enum — each seen to fail its test.
5. `markdownlint-cli2` and `scripts/check_records.py --check` on the records.

### State table

| State or input | Disposition | Required outcome and proof |
| --- | --- | --- |
| Inventory carries annotations; registration path | decided | `addTool` maps `Annotations`; wire `tools/list` shows all four hints per D1. Emission test. |
| Inventory carries annotations; static fallback path | decided | `staticInventory` maps `Annotations`; fallback output matches registration. Emission test. |
| Inventory carries title; both paths | decided | `Title` mapped in both; wire shows it. Emission test. |
| Read-only tool (8 tools) | decided | `readOnlyHint: true`, `openWorldHint: false`, other hints omitted. Guard test per D1 table. |
| Argument-dependent tool (align_tools, call_proxy) | decided | Worst-case hints: destructive, non-idempotent, open world. Guard test. |
| semantic_similarity with artifact_path | decided | Hints describe the write case (additive, idempotent, closed). Guard test; behaviour unchanged. |
| analyze_system_logs.lines out of range | decided | Schema says 1–1000, default 50; handler clamps as today; description says clamped. Guard test + existing handler test. |
| list_tools.options.max_tools | decided | Schema says minimum 1, default 1000; handler default unchanged. Guard test. |
| update_config with a newly-enumerated key | decided | Accepted as today (handler already accepts); schema now advertises it. Guard test on the 14-key set. |
| update_config.value per-key rules | decided | Described in prose (flat schema cannot branch on key); handler validation unchanged. Description pin. |
| get_internal_logs inventory copy | decided | minimum 0, maximum 1000, default 25; mcplib wire copy divergence documented (S5). Guard test on inventory. |
| get_internal_logs mcplib wire copy | out of scope | Dependency-owned; fixed when go-mcp-lib replaces mcplib. Divergence recorded. |
| update_config persistence gap (vector_enabled absent from patch switch) | out of scope | Separate defect (S4); noted, not fixed here. |
| $schema on every own inputSchema | decided | 2020-12 URI present; sanitiser passes it through. Guard test. |
| Config reload (OnConfigReloaded) | decided | Refreshed inventory carries the new fields; fallback/align serve them; base layer keeps startup registrations (existing S7 behaviour, unchanged). Existing reload test + emission test. |
| Sanitiser interaction | decided | `$schema`, `title`, `annotations`, constraint keywords pass through untouched. Sanitiser test. |
| Hydrator / align routing | decided | Unchanged: LLM payload is name/description/category/server only; formatter reads existing keys. Existing tests. |
| go-mcp-lib adoption | out of scope | Values carry over; call sites change at the swap. No work here. |
| Handler behaviour change falsifying a hint | decided | Guard test fails; table and test updated in the same commit as the behaviour change. Standing rule. |

## Consequences

Option-level trade-offs are above. Operationally: four inventory-and-plumbing changes, no handler
behaviour change, no migration, no new dependency. The wire metadata changes on the next restart;
clients that cache `tools/list` see the new fields on their next list.

## Pros and Cons of the Options

### A. One MADR, four plans (chosen)

* Good, because the shared plumbing is decided once and landed once; the plans cannot disagree about
  the drop point.
* Good, because each option keeps its own execution record and commit boundary, as the owner asked.
* Good, because the guard test is one table covering all four decisions.
* Bad, because the plans serialize on `inventory.go`; D1 must land first.

### B. Four separate MADRs

* Good, because each decision stands alone.
* Bad, because the plumbing decision (S1) would be repeated or cross-referenced four times, and the
  annotation table's evidence belongs to one context. Four records for one defect class is overhead
  without isolation benefit.

### C. One MADR, one plan

* Good, because simplest sequencing.
* Bad, because the owner asked for separate plans; one plan would blur four commit boundaries and
  four execution records into one.

### D. Wait for go-mcp-lib

* Good, because no interim plumbing.
* Bad, because the false hints and the under-advertised enum are live defects now; go-mcp-lib has no
  completion date; and every value adopted here survives the swap, so the interim work is not wasted.

## More Information

* The audit: [0014-REPORT-mcp-tool-schema-and-description-adherence.md](../reports/0014-REPORT-mcp-tool-schema-and-description-adherence.md),
  findings F1–F9, options 1/2/3/7.
* The standard: `go-mcp-lib/schema/inputs/2026-07-28/schema.json`, `#/$defs/Tool`,
  `#/$defs/ToolAnnotations` (defaults and the "hints are never trust decisions" rule).
* The SDK surface: go-sdk v1.6.1 `mcp/protocol.go:1295-1360`; go-mcp-lib `protocol/aliases.go`.
* Related records: [0013-MADR](0013-MADR-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md)
  (the flat-schema rule D2's prose constraints follow); 0005 (the sanitiser, untouched here);
  0008 (the list-tools middleware whose fallback S7 describes).
* The mcplib divergence (S5): `mcplib@v1.4.1/diagnostic.go:104-118` (`"default": 0`) vs `:136-141`
  (behaviour: default 25). The inventory copy is corrected by D2; the wire copy awaits the dependency
  swap.
