---
status: complete
date: 2026-11-25
---
# Audit of magictools' tool schemas and descriptions against the MCP 2026-07-28 standard

## Question

Do the 18 own tools of mcp-server-magictools adhere to the current MCP standard for tool definitions,
descriptions and input schemas? Where do they fall short, and what can be adopted, improved or extended —
including what the in-flight go-mcp-lib (the mcplib/go-sdk replacement) will make available?

This report observes and measures. It decides nothing; adoption choices belong to a MADR.

## The standard, as pinned in the fleet

The authoritative source used here is the MCP schema vendored in go-mcp-lib:
`~/gitrepos/go/go-mcp-lib/schema/inputs/2026-07-28/schema.json` (revision `2026-07-28`, the revision
go-mcp-lib's `protocol/constants.go:10` pins). It is newer than the revision the current SDK
(`modelcontextprotocol/go-sdk v1.6.1`, `go.mod:20`) documents (`2025-06-18`), but the `Tool` surface is the
same family: the v1.6.1 `mcp.Tool` struct (`mcp/protocol.go:1295-1348`) already carries every field the
2026-07-28 schema defines.

What the standard says about a tool (`#/$defs/Tool`):

| Field | Requirement | Notes from the schema text |
| --- | --- | --- |
| `name` | required | programmatic id; display fallback |
| `inputSchema` | required | root `type: "object"` is **required**; defaults to JSON Schema 2020-12; any 2020-12 keyword allowed, including `oneOf`/`anyOf`/`allOf`, `if`/`then`/`else`, `$ref`/`$defs` |
| `description` | optional | "a hint to the model" — the LLM-facing contract |
| `title` | optional | human-readable UI name; display precedence `title` > `annotations.title` > `name` |
| `outputSchema` | optional | JSON Schema for `structuredContent` in the result |
| `annotations` | optional | `readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint`, `title` — all **hints**, never trust decisions |
| `icons` | optional | sized icons for UIs |
| `_meta` | optional | extension metadata |

Two composition rules the 2026-07-28 schema text states explicitly, both relevant to this fleet:

1. When using composition keywords, declare `properties` at the schema root and keep branches to presence
   assertions such as `required` — type constraints inside `oneOf` branches are not enforced for the
   non-matching branch. (This is the rule 0005's S2 form followed.)
2. **The standard permits root unions; providers do not.** Anthropic's Messages API refuses any top-level
   `anyOf`/`oneOf`/`allOf` in a tool's `input_schema` (measured live 2026-11-25, record
   [0013-MADR](../decisions/0013-MADR-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md)).
   Spec compliance and provider compatibility are different axes; a schema can satisfy the first and fail
   the second. The fleet's working rule is the intersection: flat root objects, constraints in descriptions
   and the handler.

`ToolAnnotations` defaults, from the schema: `readOnlyHint: false`, `destructiveHint: true`,
`idempotentHint: false`, `openWorldHint: true`. An unannotated tool is therefore assumed by clients to be
a destructive, non-idempotent, open-world writer. For a fleet whose tools are mostly read-only diagnostics,
silence is the worst possible default.

## What was measured

Source: `internal/handler/inventory.go` (`InternalToolsInventoryJSON`, 18 tools), parsed as JSON at
`184370e`; handler behaviour read at the same commit. Registration path: `addTool`
(`internal/handler/handlers.go:221-249`) maps `InternalTool` → `mcp.Tool` with **only** `Name`,
`Description`, `InputSchema` (`:232-236`), sanitises, and calls `s.AddTool`.

### Per-tool table

| Tool | root type | addProps:false | props | required | prop descs | root union | enums | numeric bounds |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| sync_servers | object | yes | 1 | 0 | 1/1 | no | 0 | 0 |
| wake_servers | object | yes | 0 | 0 | — | no | 0 | 0 |
| reload_servers | object | yes | 1 | 0 | 1/1 | no | 0 | 0 |
| get_internal_logs | object | yes | 2 | 0 | 2/2 | no | 0 | 0 |
| get_session_stats | object | yes | 0 | 0 | — | no | 0 | 0 |
| get_health_report | object | yes | 0 | 0 | — | no | 0 | 0 |
| analyze_system_logs | object | yes | 3 | 0 | 3/3 | no | 1 | 0 |
| align_tools | object | yes | 6 | 1 | 6/6 | no | 0 | 0 |
| call_proxy | object | yes | 3 | 2 | 3/3 | no | 0 | 0 |
| update_config | object | yes | 2 | 2 | 2/2 | no | 1 | 0 |
| self_check | object | yes | 0 | 0 | — | no | 0 | 0 |
| list_tools | object | yes | 2 | 0 | 1/2 | no | 0 | 0 |
| semantic_similarity | object | yes | 2 | 0 | 2/2 | no | 0 | 0 |
| query_compliance | object | yes | 1 | 1 | 1/1 | no | 0 | 0 |
| execute_pipeline | object | yes | 7 | 0 | 7/7 | **yes** | 0 | 0 |
| validate_pipeline_step | object | yes | 3 | 2 | 3/3 | no | 0 | 0 |
| cross_server_quality_gate | object | yes | 2 | 2 | 2/2 | no | 0 | 0 |
| generate_audit_report | object | yes | 2 | 2 | 2/2 | no | 0 | 0 |

### Findings

**F1 — Spec-required fields: compliant.** All 18 tools have `name`, a root `type: "object"` `inputSchema`,
and a `description`. All 18 set `additionalProperties: false`. 47 of 48 properties carry descriptions.
The baseline is good.

**F2 — One root union, provider-refused.** `execute_pipeline` (`inventory.go:323-335`) carries the root
`anyOf`. Spec-legal, Anthropic-refused; already owned by record 0013 (flat schema + continuation). Not
re-litigated here.

**F3 — Zero annotations, zero titles, zero outputSchemas, zero icons, zero `_meta`.** No tool declares
`title`, `annotations`, `outputSchema`, `icons` or `_meta`. Two consequences:

- Every tool presents to clients with the annotation defaults: destructive, non-idempotent, open-world.
  `get_health_report`, `self_check`, `get_session_stats`, `list_tools`, `query_compliance`,
  `semantic_similarity`, `analyze_system_logs`, `get_internal_logs` are read-only diagnostics;
  `wake_servers` is additive; `update_config`, `reload_servers`, `sync_servers` mutate local state;
  `execute_pipeline` and `call_proxy` can mutate arbitrary targets. None of this is declared.
- `addTool` (`handlers.go:232-236`) drops every field except name/description/inputSchema, so even if the
  inventory carried annotations they would not reach the wire. The `InternalTool` struct
  (`handlers.go:29-34`) has no field for them. The `category` field exists in the inventory (19
  occurrences) but is likewise dropped at registration — it is internal routing metadata, not a spec
  field, and that is fine, but the drop point is where spec fields would also die.

**F4 — Descriptions: consistent house style, with real costs.** All 18 descriptions follow
`[DIRECTIVE: <label>] <sentence>. Keywords: <list>` (197–407 chars). Observations:

- The `[DIRECTIVE: …]` prefix and `Keywords:` tail are a **discovery aid for magictools' own semantic
  search** (`align_tools`), not an MCP convention. They cost ~60–100 chars per tool on every client's
  context, including clients that never use `align_tools`. The spec calls `description` a hint to the
  model; keyword stuffing is a retrieval hack layered on top. It works for the fleet's own router; it is
  noise for everyone else. Whether to keep, shorten, or move keywords to `_meta` is a decision, not a
  defect.
- Several descriptions encode **behaviour the handler does not implement** — the same disease 0005/0013
  treat for `execute_pipeline`. `update_config`'s `key` enum lists 10 keys, but
  `Config.UpdateConfigValue` (`internal/config/config.go:924+`) accepts at least 13 (`logFormat`,
  `vector_enabled`, `ringBufferTargets`, `tokenSpendThresh`, `lruLimit` are handled but not enumerated;
  the handler's runtime-patch switch at `diagnostic_handlers.go:276-306` handles 13). The schema
  under-advertises the handler. `get_internal_logs`' `max_lines` description says "(0-1000). Defaults to
  25" — the clamp lives in mcplib (`mcplib@v1.4.1/diagnostic.go`), not in a schema constraint.
- `list_tools.options` has **no description** (the one missing prop description, F1's 47/48).

**F5 — Constraints expressed as prose, not keywords.** No tool uses `minimum`, `maximum`, `default`,
`pattern`, `minLength` or `format` anywhere. Bounds that exist in code are prose in descriptions:

- `get_internal_logs.max_lines`: "(0-1000)" in text; clamp in mcplib.
- `analyze_system_logs.lines`: "default 50" in text; clamp at 1000 in the handler
  (`diagnostic_handlers.go:183-185`); no `minimum`, no `default`.
- `update_config.value`: per-key validation (`squeezeLevel` 0–5, `scoreThreshold`/`confidenceGap`
  0.0–1.0, `logFormat` json|text — `config.go:929-985`) is invisible to the schema; `value` is a bare
  string. A client cannot know the legal range without calling and failing.
- `align_tools.arguments` is a bare `type: object` with no `properties` — deliberately open (it forwards
  to arbitrary downstream tools), but the sanitiser's nested-`properties: {}` rule (0005's D3) applies
  to it.

**F6 — Enum-as-text in proxied schemas.** The ddg-search tools' `format` property carries
`"description": "…'hybrid', 'json', or 'markdown'.,enum=hybrid,enum=json,enum=markdown"` — the enum is
**inside the description string**, not an `enum` keyword. Source: the downstream server's struct tags
(`mcp-server-duckduckgo/internal/handler/search/tools.go:50`, `media/tools.go:44` use
`jsonschema:"…,enum=hybrid,…"`); the go-sdk's tag parser evidently does not split `enum=` from the
description, so the whole tag lands in `description`. Magictools stores and forwards these schemas
(`internal/client/sync_save_helpers.go:196`); its own formatter then reads `enum` from the schema
(`schema_formatter_helpers.go:25-34`) and finds none. This is a **downstream defect propagated through
the gateway**, and it is exactly the class of thing go-mcp-lib's schema-driven generation is meant to
end. Magictools' own two enums (`analyze_system_logs.severity`, `update_config.key`) are proper `enum`
keywords — the house style is right; the proxied traffic is not.

**F7 — No `outputSchema` anywhere, though results are structured.** Handlers return JSON envelopes in
`TextContent` (e.g. `get_health_report` marshals a status report, `diagnostic_handlers.go:160-164`;
`execute_pipeline` builds a metadata envelope, `execute_pipeline.go:924-978`). The SDK's
`StructuredContent` is read on proxied results (`proxy_handlers.go:489-490`) but never produced by own
tools. Every own tool's output contract is implicit. The 2026-07-28 standard's `outputSchema` +
`structuredContent` pair exists precisely for this; go-mcp-lib's MADR 0002 ("explicit structured output
and text presentation") makes it a first-class registration surface.

**F8 — `$schema` absent.** No tool declares `$schema`. The standard defaults to 2020-12 when absent, so
this is compliant-by-default; declaring it explicitly is cheap and removes ambiguity for validators.
Low priority.

**F9 — The sanitiser is a compliance risk surface.** `util.SanitizeToolSchema`
(`internal/util/schema_util.go:39-64`) adds missing `type: object`, rewrites `null` properties to
`{type: object}`, and adds `properties: {}` to object maps without properties. 0005's D3 decided to
stop the retyping and pass unions through; that work is still unimplemented (0005's PLAN, in-progress).
Until it lands, the gateway mutates downstream schemas in ways the standard does not require and some
providers refuse. Owned by 0005; noted here because it is the single largest adherence gap in the
**proxied** path.

### The go-mcp-lib horizon

go-mcp-lib (`~/gitrepos/go/go-mcp-lib`) is in flight: protocol DTOs generated from the 2026-07-28 schema
(`protocol/owned.go`, `aliases.go`, manifest-pinned), aliasing `go-sdk v1.8.0` types where reviewed. Its
MADR 0002 decides typed tool registration with **schema diagnostics before serving**, explicit
structured output, and a modern-only protocol policy. What that changes for this audit:

- **Adopt now, keep later**: every field this report recommends (`title`, `annotations`, `outputSchema`,
  proper `enum`/bounds keywords) exists in go-sdk v1.6.1's `mcp.Tool` today and in go-mcp-lib's generated
  surface. Nothing recommended here is blocked on go-mcp-lib; nothing recommended here is thrown away
  by it.
- **Registration diagnostics**: go-mcp-lib will validate schemas at registration. The inventory guard
  test 0013 adds (no root unions, flat roots) is the same idea applied earlier; when go-mcp-lib lands,
  the guard can delegate to the library's diagnostics instead of hand-rolled assertions.
- **The mcplib dependency**: `get_internal_logs` is registered by `mcplib.RegisterDiagnosticTool`
  (`diagnostic_handlers.go:21`), bypassing `addTool` and the inventory entirely — its schema comes from
  mcplib's struct tags, which is why its bounds are prose. When go-mcp-lib replaces mcplib, that tool's
  schema becomes fleet-owned and can carry proper keywords. Until then it is the one own tool whose
  schema magictools does not control.
- **F6's enum-as-text** is a struct-tag parsing defect in the current SDK path; go-mcp-lib's
  schema-driven generation (schema first, code generated from it) removes the tag-parsing layer where
  the defect lives. Downstream servers adopt it on their own schedule; the gateway could normalise
  `…,enum=x` descriptions into proper `enum` keywords at sync time as an interim repair — a decision
  for a MADR, with 0005's D3.4 "pass through unchanged" ruling to revisit.

## Adoption options (for a MADR to decide)

Ordered by value/effort, each independent:

1. **Annotations on all 18 tools** (high value, low effort): add `annotations` to `InternalTool` and the
   inventory; map it in `addTool`; declare `readOnlyHint`/`destructiveHint`/`idempotentHint`/`openWorldHint`
   per the handler's actual behaviour. Read-only diagnostics stop presenting as destructive writers.
   Guard: a test pinning each tool's annotation set against a table.
2. **Schema keywords for existing prose constraints** (high value, low effort): `minimum`/`maximum`/
   `default` on `max_lines`, `lines`; `enum` completion on `update_config.key` (13 keys, matching
   `config.go`); per-key `value` constraints expressed via description + handler (a flat schema cannot
   branch on `key` without a union — the 0013 rule applies). `list_tools.options` description.
3. **`title` on all 18** (medium value, trivial effort): human-readable UI names; the spec's display
   precedence makes this the client-facing label.
4. **`outputSchema` + `structuredContent` for the JSON-envelope tools** (high value, medium effort):
   start with `get_health_report`, `get_session_stats`, `self_check`, `analyze_system_logs` — their
   envelopes are already marshalled structs; declare the schema, return `StructuredContent` alongside
   the text. go-mcp-lib makes this the default registration shape later.
5. **Description style review** (medium value, decision needed): keep `[DIRECTIVE:]`/`Keywords:` for
   `align_tools` routing, or move keywords to `_meta` and slim the model-facing description. Measure
   the token cost across the 18 tools first.
6. **Proxied-schema enum normalisation at sync** (medium value, medium effort, touches 0005's D3.4):
   parse `…,enum=x` out of descriptions into `enum` keywords. Interim until downstream servers adopt
   go-mcp-lib. Needs a MADR because D3.4 currently rules pass-through.
7. **Explicit `$schema: "https://json-schema.org/draft/2020-12/schema"`** (low value, trivial effort):
   removes default ambiguity.
8. **Delegate the union/flat-root guard to go-mcp-lib's registration diagnostics when it lands**
   (future, no work now): noted so the 0013 guard is written as a replaceable seam.

## What this report does not cover

- Downstream servers' own schemas beyond the ddg-search enum sample (F6); a fleet-wide proxied-schema
  scan is a separate measurement.
- Prompts and resources surfaces (the standard's `Prompt`/`Resource` definitions); tools only.
- Provider-by-provider compatibility beyond the measured Anthropic refusal (0013) and 0005's recorded
  Grok refusal; the intersection rule is stated, not exhaustively probed.
- Any decision. Every option above is a MADR's to take.

## Evidence index

| Id | Claim | Location | Mark |
| --- | --- | --- | --- |
| A1 | 2026-07-28 Tool definition, composition rules, annotation defaults | `go-mcp-lib/schema/inputs/2026-07-28/schema.json` `#/$defs/Tool`, `#/$defs/ToolAnnotations` | read |
| A2 | go-sdk v1.6.1 Tool carries title/annotations/outputSchema/icons | `go-sdk@v1.6.1/mcp/protocol.go:1295-1348` | read |
| A3 | 18 tools, per-tool table, description style | `internal/handler/inventory.go` parsed at `184370e` | run |
| A4 | addTool drops all but name/description/inputSchema | `internal/handler/handlers.go:232-236` | read |
| A5 | update_config enum 10 vs handler 13 keys | `inventory.go` enum; `internal/config/config.go:924+`; `internal/handler/diagnostic_handlers.go:276-306` | read |
| A6 | lines clamp 1000, default 50 | `internal/handler/diagnostic_handlers.go:179-185` | read |
| A7 | get_internal_logs registered by mcplib, bypassing inventory | `internal/handler/diagnostic_handlers.go:21`; `mcplib@v1.4.1/diagnostic.go:66` | read |
| A8 | enum-as-text in ddg-search format property | `mcp-server-duckduckgo/internal/handler/search/tools.go:50`, `media/tools.go:44`; live `align_tools` output 2026-11-25 | read + run |
| A9 | sanitiser retyping and properties:{} | `internal/util/schema_util.go:39-64`; 0005's D3 | read |
| A10 | go-mcp-lib revision, generated surface, MADR 0002 registration diagnostics | `go-mcp-lib/protocol/constants.go:10`, `aliases.go`, `docs/decisions/0002-MADR-build-schema-driven-mcp-framework.md` | read |
| A11 | Anthropic root-union refusal | record 0013; owner's gobble log 2026-11-25 | run (owner) |
