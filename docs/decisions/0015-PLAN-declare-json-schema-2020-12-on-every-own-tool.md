---
status: in-progress
date: 2026-11-26
associated-madr: "0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md"
---
# Declare JSON Schema 2020-12 on every own tool

Associated MADR: [0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md](0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md)

Implements the MADR's **D4** only. D1 (annotations) is implemented and pushed; D2 (constraint
keywords) is executed and pushed, pending owner acceptance. D3 (titles) is a separate plan sharing
this number.

## Goal

Every own tool's `inputSchema` declares `"$schema": "https://json-schema.org/draft/2020-12/schema"`
explicitly. The MCP standard defaults to 2020-12 when the keyword is absent, so this changes no
validation outcome; it removes draft ambiguity for client-side validators. Inventory-only: the
keyword rides inside `inputSchema`, which both emission paths already pass through. No handler
behaviour changes.

## Scope

### In scope

| Component | Planned work |
| --- | --- |
| `internal/handler/inventory.go` | All 18 `inputSchema` objects gain the `$schema` keyword with the 2020-12 URI. |
| `internal/handler/inventory_schema_draft_test.go` (new) | Guard: every entry's `inputSchema.$schema` equals the 2020-12 URI exactly. |
| `internal/handler/schema_draft_wire_test.go` (new, behind `magictoolstestseam`) | Wire emission: registered `tools/list` carries `$schema` per tool (sanitiser passthrough proven end-to-end); `get_internal_logs`'s mcplib wire copy pinned `$schema`-free (S5). Fallback emission: `staticInventory` schemas carry it (shared map, no copy). |

### Out of scope

* Handler behaviour and validation logic: the proxy firewall's compiler
  (`internal/handler/proxy_service.go:94-103`, santhosh-tekuri/jsonschema/v5) already treats these
  schemas as 2020-12-compatible; the explicit keyword changes nothing it does.
* `get_internal_logs`'s mcplib-registered wire copy (`diagnostic_handlers.go:21`,
  `mcplib@v1.4.1/diagnostic.go:104-118`): dependency-owned, fixed at the go-mcp-lib swap (MADR S5).
  The inventory copy carries `$schema` like the rest.
* Downstream (proxied) tools' schemas: not ours to declare.
* D1, D2, D3.

## Grounding Reference

Every fact read at `e7d6178` (HEAD when this plan was drafted). Re-ground at execution HEAD.

| Fact | Evidence at `e7d6178` |
| --- | --- |
| Inventory holds 18 tools, 0 `$schema` keywords | `internal/handler/inventory.go` — 18 `"name":` entries, no `"$schema"` |
| Sanitiser passes `$schema` through | `internal/util/schema_util.go:39-64` — `sanitizeMap` reads/writes only `type`/`properties`; all other keys untouched |
| `inputSchema` passes through both emission paths today | `internal/handler/handlers.go:241-246` (`addTool`), `internal/handler/list_tools_handler.go:96-100` (`staticInventory`, schema shared not copied) |
| Proxy firewall compiles inventory-compatible schemas | `internal/handler/proxy_service.go:94-103` (`jsonschema.NewCompiler`, AddResource, Compile, Validate) |
| SDK low-level `AddTool` does no draft validation | go-sdk v1.6.1 `mcp/server.go` (0005's E10, cited by the MADR) |
| D1's wire-test plumbing exists to reuse | `internal/handler/annotations_wire_test.go` — `emittedToolsList`, `newTestHandler`, the S5 explicit-absence pattern |
| The 2020-12 URI | MADR D4.1: `https://json-schema.org/draft/2020-12/schema` |

## Prerequisites

1. The owner approves this PLAN; then `in-progress`.
2. Re-ground the table at HEAD; a changed fact is a deviation.
3. No unowned edits; `internal/forks/hnsw/go.mod` never staged.
4. No built binary on a live-service machine; no test signals a process it did not start.
5. Suite runs through the fleet scripts or local `run-heavy`; single-package tests may stay local.
6. Tests first, seen failing; plants in scratch copies; `gofmt` + lint on staged Go;
   `git commit --no-edit`; push only on the owner's word.
7. No hostname, account name or real-machine path in a committed file.

## Implementation Steps

### P0 — re-ground (no repository change)

Re-read every grounding row at HEAD; confirm the 18-entry count, the sanitiser's key set, and the
D1 test helpers this plan reuses. Record results.

Exit: every row confirmed or a deviation recorded.

### P1 — tests first (red)

Files: `internal/handler/inventory_schema_draft_test.go`,
`internal/handler/schema_draft_wire_test.go` (behind `magictoolstestseam`).

1. `TestInventorySchemaDraft`: parse the inventory; assert each of the 18 entries'
   `inputSchema.$schema` equals the 2020-12 URI exactly (no trailing slash, no `#`).
2. `TestSchemaDraftOnWire`: register as the binary does, read `tools/list` via `emittedToolsList`;
   each own tool's wire schema carries the URI — proving the sanitiser passes it through on the
   real path. `get_internal_logs`'s wire copy pinned `$schema`-free (mcplib-owned, S5).
3. `TestSchemaDraftOnStaticFallback`: call `staticInventory` directly; each returned schema
   carries the URI.

Run against the unchanged tree; keep the failures. Exit: each red for the stated reason.

### P2 — the inventory edits

File: `internal/handler/inventory.go`. Add the `$schema` keyword to all 18 `inputSchema` objects.
No other change.

Gate: P1 green; existing suite green (the proxy firewall's compile/validate path included);
`make vet`; `gofmt`. Commit.

### P3 — plants and gates

| Plant | Test that must fail |
| --- | --- |
| one entry's `$schema` removed | `TestInventorySchemaDraft` |
| one entry's URI changed to draft-07 | `TestInventorySchemaDraft` |
| sanitiser strips `$schema` | `TestSchemaDraftOnWire` |
| one entry's URI given a trailing `#` | `TestInventorySchemaDraft` |
| fallback schema loses `$schema` (copy-and-strip in `staticInventory`) | `TestSchemaDraftOnStaticFallback` |

Gates: full suite via `make test` on a fleet host or local `run-heavy`;
`make lint` (local `run-heavy` acceptable per D1/D2's recorded experience; the pre-existing
`service_lifecycle.go:69` SA4023 is linux-only, record 0012's, out of scope); `make vet`; `gofmt`;
`make check-records`; `make markdownlint`. Validation runs through the Makefile targets, not raw
`go` commands; single-package iteration during P1/P2 may use `go test ./onepkg`.

Exit: every plant failed; gates green; execution record updated; commit.

## Verification

* P1 red before, green after, red on each plant.
* The wire test doubles as the sanitiser-passthrough proof on the real registration path
  (MADR Confirmation 2 and the state table's sanitiser row).
* The existing proxy-firewall tests stay green: the explicit draft changes no validation outcome.
* No handler test changes: no behaviour changed.

## Rollback

Revert the commit. The schemas lose the explicit draft declaration and validators fall back to
the standard's 2020-12 default — the same semantics, less explicit. No stored data, no migration.

## Completion Criteria

1. All 18 own `inputSchema` objects declare the exact 2020-12 URI, pinned by the guard test.
2. Registered `tools/list` and the static fallback both carry it, pinned by the wire tests.
3. Every plant failed; gates green; no handler behaviour changed.

## Execution record

### Approval — 2026-11-26

Owner: "accept d2, approval to proceed." D2 accepted; D3 and D4 approved. PLAN `in-progress`;
commit and push to `main` authorized for this session (standing from the D2 approval turn).
D3 executes first; this plan follows.

### P0 — 2026-11-26, re-grounded at `be224de` (pass 1)

- Inventory: 18 `"name":` entries, 18 `"inputSchema": {` objects, 0 `"$schema"` keywords.
- Sanitiser key set confirmed (`schema_util.go:39-64`): `type`/`properties` only.
- Proxy firewall compiler confirmed (`proxy_service.go:94`).
- D1/D3 helpers confirmed reusable: `newTestHandler`, `emittedToolsList`, `schemaMap`
  (`pipeline_session_store_test.go:54`), `d3Titles` as the own-tool name set.
- No deviation.

### P1 — 2026-11-26, tests first (pass 1)

- `internal/handler/inventory_schema_draft_test.go`: `TestInventorySchemaDraft` pins the exact
  2020-12 URI on all 18 entries.
- `internal/handler/schema_draft_wire_test.go` (behind `magictoolstestseam`):
  `TestSchemaDraftOnWire` (17 own schemas on the wire; `get_internal_logs` pinned
  `$schema`-free per S5), `TestSchemaDraftOnStaticFallback` (all 18).
- Seen red: `inputSchema has no $schema` (inventory), `wire schema has no $schema` (wire) —
  the stated reasons.

### P2 — 2026-11-26, inventory edits (pass 1)

- All 18 `inputSchema` objects gain
  `"$schema": "https://json-schema.org/draft/2020-12/schema"`. No other change.
- All three P1 tests green; D1/D2/D3 guards and the 0013 flat-schema guard still green.
- `gofmt` clean; `make vet` clean.

### P3 — 2026-11-26, plants and gates (pass 1)

**Plants (scratch copy of `git archive HEAD` plus the P1/P2 working files, each seen to fail):**

| Plant | Test that failed | First failure line |
| :--- | :--- | :--- |
| first entry's `$schema` removed | `TestInventorySchemaDraft` | `sync_servers: inputSchema has no $schema` |
| first entry's URI changed to draft-07 | `TestInventorySchemaDraft` | `sync_servers: $schema = http://json-schema.org/draft-07/schema#, want "https://…"` |
| sanitiser strips `$schema` | `TestSchemaDraftOnWire` | `align_tools: wire schema has no $schema (sanitiser stripped it?)` |
| first entry's URI given trailing `#` | `TestInventorySchemaDraft` | `sync_servers: $schema = https://…/schema#, want "https://…/schema"` |
| fallback copies schema and strips `$schema` | `TestSchemaDraftOnStaticFallback` | `sync_servers: fallback $schema = <nil>, want "https://…"` |

Deviation note: the plan's plant table names specific tools ("one entry"); the runs planted on
the first entry (`sync_servers`) for plants 1/2/4 — same assertion, same test, no scope change.

**Gates:**

- `make test` on `<windows-host>` via `run-on-host --need any`: exit 0, 716 top-level passes,
  0 failures; all three D4 tests PASS.
- `make lint` on `<linux-host>` (WSL Ubuntu-24.04) via `run-on-host --need linux`: exit 2, one
  finding — the pre-existing `service_lifecycle.go:69` SA4023 (record 0012's, linux-only, out of
  scope). No finding in this plan's files. D2's local `run-heavy` lint on the same toolchain
  recorded 0 issues; the local queue was busy with other sessions' gates during this phase.
- `make vet`: clean. `gofmt -l internal/handler/`: clean. `make check-records`: exit 0.
  `make markdownlint`: 0 issues.

**Completion criteria:** all three met — the exact 2020-12 URI pinned on all 18 inventory
schemas, both emission paths pinned (wire test doubles as the sanitiser-passthrough proof),
every plant failed and gates green with no handler behaviour change.

PLAN remains `in-progress` pending owner acceptance of the execution.
