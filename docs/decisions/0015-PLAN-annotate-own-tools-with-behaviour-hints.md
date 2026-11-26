---
status: proposed
date: 2026-11-25
associated-madr: "0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md"
---
# Annotate every own tool with behaviour hints

Associated MADR: [0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md](0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md)

Implements the MADR's **D1** only. D2 (constraint keywords), D3 (titles) and D4 (`$schema`) are separate
plans sharing this number; D3 reuses the `InternalTool` plumbing this plan lands.

## Goal

Every own tool carries `annotations` on the wire — `readOnlyHint`, `destructiveHint`, `idempotentHint`,
`openWorldHint` — with the values of the MADR's D1 table, through both emission paths (registration and
the static fallback). A read-only diagnostic stops presenting to clients as a destructive, non-idempotent,
open-world writer. No handler behaviour changes.

## Scope

### In scope

| Component | Planned work |
| --- | --- |
| `internal/handler/handlers.go` | `InternalTool` gains `Annotations *mcp.ToolAnnotations` (`json:"annotations,omitempty"`); `addTool` maps it into the `mcp.Tool` it builds. |
| `internal/handler/list_tools_handler.go` | `staticInventory` maps `Annotations` the same way, so the fallback layer agrees with registration. |
| `internal/handler/inventory.go` | All 18 entries gain an `annotations` object with the D1 table's values. |
| `internal/handler/inventory_test.go` | Guard: every entry's annotations match the D1 table exactly; read-only tools omit the two meaningless hints. |
| `internal/handler/annotations_wire_test.go` (new) | Emission: registered `tools/list` carries each tool's annotations; the static fallback carries the same. |

### Out of scope

* D2, D3, D4 of the MADR (their own plans).
* `get_internal_logs`'s mcplib-registered wire copy: `mcplib.RegisterDiagnosticTool`
  (`internal/handler/diagnostic_handlers.go:21`) builds its own `mcp.Tool` with no annotations
  (`mcplib@v1.4.1/diagnostic.go:101-118`); this plan annotates the inventory copy only. The wire copy
  awaits the go-mcp-lib swap (MADR S5).
* Proxied downstream tools' annotations; `ToolRecord` (`internal/db/badger.go:94-125`) has no
  annotations field and `buildToolRecord` (`internal/client/sync_save_helpers.go:195-215`) does not
  read them — a separate decision if ever wanted.
* Handler behaviour of any tool. The annotation values describe what the handlers already do.
* The hydrator and align routing: the LLM payload is name/description/category/server only
  (`internal/intelligence/hydrator.go:401-406`); `align_tools` reads `it.InputSchema` only
  (`internal/handler/align_tools_helpers.go:144`). Neither reads annotations; neither changes.

## Grounding Reference

Every fact below was read at `33ed2a8` (HEAD when this plan was drafted). Re-ground each at execution
HEAD; a moved line is a path update, a changed fact is a deviation.

| Fact | Evidence at `33ed2a8` |
| --- | --- |
| `InternalTool` has four fields, no annotations | `internal/handler/handlers.go:29-34` |
| `addTool` maps only Name/Description/InputSchema | `internal/handler/handlers.go:240-244` |
| `staticInventory` maps only the same three | `internal/handler/list_tools_handler.go:95-99` |
| All 18 inventory entries lack `annotations` and `title` | parsed `InternalToolsInventoryJSON` at HEAD: 18 tools, 0 annotations, 0 titles |
| SDK `ToolAnnotations` field types | go-sdk v1.6.1 `mcp/protocol.go:1357-1384`: `DestructiveHint *bool`, `IdempotentHint bool`, `OpenWorldHint *bool`, `ReadOnlyHint bool`, `Title string` |
| SDK `Tool.Annotations` field | go-sdk v1.6.1 `mcp/protocol.go:1302` (`Annotations *ToolAnnotations`) |
| Spec defaults: readOnly false, destructive true, idempotent false, openWorld true | go-mcp-lib `schema/inputs/2026-07-28/schema.json` `#/$defs/ToolAnnotations`; SDK doc comments agree |
| `sanitizeMap` touches only `type`/`properties` | `internal/util/schema_util.go:39-64` — annotations pass through untouched |
| Inventory parsed at construction and reload | `internal/handler/handlers.go:100` (`NewHandler`), `:148-156` (`OnConfigReloaded`); both `json.Unmarshal` into `[]*InternalTool`, so a new `omitempty` field round-trips with no call-site change |
| `OnConfigReloaded` does not re-register with the SDK | `internal/handler/handlers.go:169-171` comment: base layer keeps startup registrations; fallback and align serve the refreshed inventory |
| `SyncNativeTools` re-parses the inventory JSON into `mcp.ListToolsResult` | `internal/handler/sync_handlers.go:46-48`; `mcp.Tool` unmarshalling picks up `annotations` automatically |
| Emission-test helper exists | `emittedToolsList` (`internal/handler/pipeline_session_store_test.go:19`, behind `magictoolstestseam` tag) registers as the binary does and reads `tools/list` over in-memory transports |
| Inventory guard test exists, pins count 18 | `internal/handler/inventory_test.go:8-39` |

### Per-tool behaviour evidence (the D1 table's proof, re-verified at `33ed2a8`)

| Tool | Handler | What it does (read from source) | D1 hints (ro/destructive/idempotent/openWorld) |
| --- | --- | --- | --- |
| sync_servers | `sync_handlers.go:24` | `SyncEcosystem` + `SyncNativeTools` (`:39,47`): rewrites stored tool records; repeat sync converges | false / false / true / false |
| wake_servers | `sync_handlers.go:167` | `Registry.Connect` for offline servers, skips online (`:184`); additive, repeat no-op | false / false / true / false |
| reload_servers | `sync_handlers.go:228` | `DisconnectAll` (`:240`) kills live sessions, then re-syncs; selective restart (`:265`); destructive to sessions, converges | false / true / true / false |
| get_internal_logs | mcplib `diagnostic.go:133-141` | reads the ring buffer | true / — / — / false |
| get_session_stats | `diagnostic_handlers.go:33` | `Telemetry.GetSessionStats` read, marshals | true / — / — / false |
| get_health_report | `diagnostic_handlers.go:111` | `PingAll` (`:112`) → `executePingWithSession` (`health_monitor.go:129`) writes `LastPing`, `PingLatency`, `ConsecutivePingFailures` under lock; 3 failures trigger `orchestrateRestart` (`lifecycle.go:614`). Not read-only: pings mutate health state and can restart a server | false / false / true / false |
| analyze_system_logs | `diagnostic_handlers.go:168` | `logutil.TailFile` (`:193`), filters | true / — / — / false |
| align_tools | `proxy_handlers.go:97` | search + optional inline execution of any resolved tool; `triggerAutoHeal` (`:72`) may `ReindexAllTools` (`:78`). Worst case: any downstream tool's effects | false / true / false / true |
| call_proxy | `proxy_handlers.go:219` | executes any downstream tool; `UpdateToolUsage` (`:242`). Worst case: any downstream tool's effects | false / true / false / true |
| update_config | `diagnostic_handlers.go:253` | `UpdateConfigValue` (`:269`) + `store.Apply` (`:311`), rollback on failure (`:313`); same key+value converges | false / false / true / false |
| self_check | `diagnostic_handlers.go:332` | reads metrics, cache, diagnostics (`:339-346`); no writes | true / — / — / false |
| list_tools | `diagnostic_handlers.go:418` | reads store/inventory | true / — / — / false |
| semantic_similarity | `semantic_similarity_handler.go:17` | `SearchTools` read (`:22`); writes an artifact file when `artifact_path` set (`:53-57`). Additive, same content rewritten | false / false / true / false |
| query_compliance | `diagnostic_handlers.go:43` | Recall search + vector search; no writes | true / — / — / false |
| execute_pipeline | `execute_pipeline.go:39` + session/run helpers | fresh runs mutate via MUTATOR stages (`createGitCheckpoint` `run_helpers.go:194`, rollback `:241`); dry run stores a session (`execute_pipeline_session.go:43`); continue claims and runs (`:170`); reject transitions (`:238`). Potentially destructive, re-run re-executes, arbitrary target | false / true / false / true |
| validate_pipeline_step | `pipeline_tools.go:395` | reads standards, scans output text; no writes | true / — / — / false |
| cross_server_quality_gate | `pipeline_tools.go:460` | Recall reads, approval checks; no writes | true / — / — / false |
| generate_audit_report | `generate_audit_report.go:16` | `git diff` read (`:26`), Recall reads; `auditRecordEdgeLearning` (`:57` → `audit_report_helpers.go:159`) writes synergy weights (`:174`). Additive learning writes | false / false / true / false |

"—" = hint omitted: `destructiveHint`/`idempotentHint` are meaningful only when `readOnlyHint == false`
(spec text). Read-only tools set `readOnlyHint: true` and `openWorldHint: false` only.

**SDK type note (verified):** `ReadOnlyHint` and `IdempotentHint` are plain `bool` with `omitempty`
(go-sdk v1.6.1 `protocol.go:1371,1381`); `DestructiveHint` and `OpenWorldHint` are `*bool`. A `false`
plain bool is omitted from the wire by `omitempty` — which is correct for the read-only rows (idempotent
omitted) but means a writer's `idempotentHint: false` would also vanish. The D1 table's writer rows all
declare `idempotentHint: true` except `align_tools`, `call_proxy`, `execute_pipeline` (false). For those
three the wire omits the field and the client applies the spec default `false` — the same value. The
guard test asserts the *inventory* values exactly; the wire test asserts what the SDK actually emits,
including the omitempty behaviour, so the two layers are pinned separately and honestly.

## Prerequisites

1. The owner approves this PLAN. The MADR is `accepted` (2026-11-25); this plan is `proposed` until
   approval, then `in-progress`.
2. Re-ground the tables above at HEAD before the first edit; a changed fact is a deviation recorded
   here and, if it changes a decision, in the MADR.
3. The working tree carries no unowned edits; `internal/forks/hnsw/go.mod` is the owner's, never staged.
4. No built binary of this module runs on a machine with a live service (CLAUDE.md; record 0010). No
   test signals a process it did not start.
5. Suite and race runs go through the fleet host scripts (`pick-host` / `run-on-host`, `--race` for
   race runs, explicit `-timeout`); single-package tests may stay local.
6. Tests first, seen failing, then the change, then green; tests commit with the phase that turns them
   green.
7. A planted break per assertion, in a scratch copy, never the working tree.
8. `gofmt` and the repository's lint on staged Go; `git commit --no-edit`; push only on the owner's
   word in the same turn.
9. No hostname, account name or real-machine path in a committed file.

## Implementation Steps

### P0 — re-ground (no repository change)

Re-read every grounding row at HEAD. Confirm: `InternalTool` still four fields; both emission paths
still map three; all 18 entries still lack annotations; the SDK field types unchanged; the per-tool
behaviour evidence still holds (spot-check the three non-obvious rows: `get_health_report`'s ping
writes, `semantic_similarity`'s artifact write, `generate_audit_report`'s synergy write). Record results
in the execution record.

Exit: every row confirmed or a deviation recorded.

### P1 — tests first (red)

Files: `internal/handler/inventory_test.go` (extend), `internal/handler/annotations_wire_test.go` (new).

1. **Inventory guard** (`TestInventoryAnnotations`): parse `InternalToolsInventoryJSON`; for each of
   the 18 tools assert the annotations object equals the D1 table exactly — the four hint values where
   the table declares them, absence where it says "—". A table literal in the test mirrors the MADR's
   D1 table; the test names the MADR as its source.
2. **Wire emission** (`TestAnnotationsOnWire`, behind `magictoolstestseam` to reuse `emittedToolsList`):
   register as the binary does, read `tools/list`, assert each tool's `Annotations` matches what the
   SDK emits for the D1 values (including the omitempty behaviour of the plain-bool fields).
3. **Fallback emission** (`TestAnnotationsOnStaticFallback`): call `staticInventory` directly (same
   package), assert each returned `mcp.Tool` carries the same annotations as registration.
4. **Sanitiser passthrough** (`TestSanitiserPreservesAnnotations` in `internal/util` or the handler
   test): `SanitizeToolSchema` on a tool with annotations leaves them byte-identical.

Run against the unchanged tree; keep the failures. Exit: each red for the stated reason; existing
suite unchanged; no source change.

### P2 — the plumbing and the inventory

Files: `internal/handler/handlers.go`, `internal/handler/list_tools_handler.go`,
`internal/handler/inventory.go`.

1. `InternalTool` gains `Annotations *mcp.ToolAnnotations \`json:"annotations,omitempty"\``.
2. `addTool`'s mapping (`handlers.go:240-244`) gains `Annotations: it.Annotations`.
3. `staticInventory`'s mapping (`list_tools_handler.go:95-99`) gains the same.
4. All 18 inventory entries gain the D1 `annotations` object. JSON booleans unmarshal into the SDK
   types correctly: `readOnlyHint`/`idempotentHint` into plain bools, `destructiveHint`/`openWorldHint`
   into `*bool`.

Gate: P1's tests green; the existing suite green (`go test ./internal/handler/ ./internal/util/`);
`go vet`; `gofmt`. Commit.

### P3 — plants and gates

Plants (scratch copies, each seen to fail its named test):

| Plant | Test that must fail |
| :--- | :--- |
| one tool's `readOnlyHint` flipped | `TestInventoryAnnotations` |
| `addTool` drops `Annotations` again | `TestAnnotationsOnWire` |
| `staticInventory` drops `Annotations` | `TestAnnotationsOnStaticFallback` |
| a read-only tool gains `destructiveHint` | `TestInventoryAnnotations` (absence assertion) |
| sanitiser strips annotations | `TestSanitiserPreservesAnnotations` |

Gates: full suite `-tags magictoolstestseam ./...` on a fleet host; `go vet ./...`; `gofmt -l`;
`make lint` (fleet host; the one pre-existing `service_lifecycle.go:69` finding is record 0012's, not
this plan's); `check_records.py --check`; `markdownlint-cli2` on this plan.

Exit: every plant failed; gates green; execution record updated; commit.

## Verification

* P1's tests red before, green after, red on each P3 plant.
* The wire test proves what clients actually receive, including the SDK's omitempty behaviour — not
  just what the inventory holds.
* The fallback test proves the two emission paths agree (MADR S7).
* No handler test changes: no behaviour changed.

## Rollback

Revert the commit. The inventory loses the annotations objects, the two mappings lose the field, and
clients see the spec defaults again (destructive, non-idempotent, open-world). No stored data, no
migration; the annotations are wire metadata only.

## Completion Criteria

1. All 18 own tools carry the D1 annotation values in the inventory, pinned by the guard test.
2. Registered `tools/list` and the static fallback both emit them, pinned by the wire tests.
3. The sanitiser passes them through, pinned.
4. Every plant failed its test; the suite and gates green.
5. No handler behaviour changed (existing tests untouched and green).

## Execution record

No phase has run yet.
