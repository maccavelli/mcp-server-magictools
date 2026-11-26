---
status: in-progress
date: 2026-11-26
associated-madr: "0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md"
---
# Give every own tool a human-readable title

Associated MADR: [0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md](0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md)

Implements the MADR's **D3** only. D1 (annotations) is implemented and pushed; D2 (constraint
keywords) is executed and pushed, pending owner acceptance. D4 (`$schema`) is a separate plan
sharing this number.

## Goal

Every own tool carries a short, human-readable `title` in the inventory, and both emission paths
(registered `tools/list` and the static fallback) deliver it on the wire. `name` stays the
programmatic id; `description` stays the model-facing hint. No handler behaviour changes.

## Scope

### In scope

| Component | Planned work |
| --- | --- |
| `internal/handler/handlers.go` | `InternalTool` gains `Title string` (JSON `title,omitempty`); `addTool`'s mapping gains `Title: it.Title`. |
| `internal/handler/list_tools_handler.go` | `staticInventory`'s mapping gains `Title: it.Title`. |
| `internal/handler/inventory.go` | All 18 entries gain the D3 title from the MADR's table. |
| `internal/handler/inventory_titles_test.go` (new) | Guard: every entry's title equals the D3 table; non-empty; distinct from `name`. |
| `internal/handler/titles_wire_test.go` (new, behind `magictoolstestseam`) | Wire + fallback emission of `title`, reusing D1's `emittedToolsList` plumbing; `get_internal_logs`'s mcplib wire copy pinned title-free (S5). |

### Out of scope

* Handler behaviour: titles are wire metadata only.
* `annotations.title` (the SDK's second display-name source, `protocol.go:1134`): not adopted;
  `title` on the tool itself takes precedence anyway (SDK comment at `protocol.go:1301`).
* `get_internal_logs`'s mcplib-registered wire copy (`diagnostic_handlers.go:21`): dependency-owned,
  fixed at the go-mcp-lib swap (MADR S5). The inventory copy carries the title like the rest.
* The hydrator's LLM payload (`internal/intelligence/hydrator.go:430-435`): stays
  name/description/category/server; titles do not enter the semantic index. No routing change.
* D1, D2, D4.

## Grounding Reference

Every fact read at `e7d6178` (HEAD when this plan was drafted). Re-ground at execution HEAD.

| Fact | Evidence at `e7d6178` |
| --- | --- |
| `InternalTool` has five fields, no `Title` | `internal/handler/handlers.go:29-35` (Name, Description, Category, InputSchema, Annotations) |
| `addTool` maps four spec fields, no `Title` | `internal/handler/handlers.go:241-246` |
| `staticInventory` maps the same four | `internal/handler/list_tools_handler.go:96-100` |
| Inventory holds 18 tools, 0 titles | `internal/handler/inventory.go` — 18 `"name":` entries, no `"title"` |
| SDK `mcp.Tool` carries `Title string` with `title,omitempty` | go-sdk v1.6.1 `mcp/protocol.go:1344`; display-name precedence comment at `:1301` (title, annotations.title, name) |
| Sanitiser touches `type`/`properties` only | `internal/util/schema_util.go:39-64` (`sanitizeMap`); tool-level fields untouched (`SanitizeToolSchema`, `:12`) |
| D1's wire-test plumbing exists to reuse | `internal/handler/annotations_wire_test.go` — `emittedToolsList`, `newTestHandler`, the S5 mcplib skip pattern (`:16-30`) |
| Sync path picks up new JSON fields automatically | `internal/client/sync_manager.go:29` (`SyncNativeTools` unmarshals into `mcp.ListToolsResult`) |
| Hydrator payload excludes titles | `internal/intelligence/hydrator.go:430-435` |
| D3 title table (18 rows) | MADR D3.1–2; wording may be refined by this plan, meaning may not |

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

Re-read every grounding row at HEAD; confirm the field lists, the 18-entry count, and the D1 test
helpers this plan reuses. Record results.

Exit: every row confirmed or a deviation recorded.

### P1 — tests first (red)

Files: `internal/handler/inventory_titles_test.go`, `internal/handler/titles_wire_test.go`
(behind `magictoolstestseam`).

1. `TestInventoryTitles`: parse the inventory; assert each of the 18 entries carries exactly the
   D3 table's title, non-empty and distinct from `name`.
2. `TestTitlesOnWire`: register as the binary does, read `tools/list` via `emittedToolsList`;
   each own tool's `Title` equals the table. `get_internal_logs`'s wire copy pinned title-free
   (mcplib-owned, S5), following D1's explicit-absence pattern.
3. `TestTitlesOnStaticFallback`: call `staticInventory` directly; each returned `mcp.Tool`
   carries the same title as registration.

Run against the unchanged tree; keep the failures. Exit: each red for the stated reason.

### P2 — the plumbing and the inventory

Files: `internal/handler/handlers.go`, `internal/handler/list_tools_handler.go`,
`internal/handler/inventory.go`.

1. `InternalTool` gains `Title string` (`json:"title,omitempty"`).
2. `addTool`'s mapping gains `Title: it.Title`.
3. `staticInventory`'s mapping gains the same.
4. All 18 inventory entries gain the D3 title.

Gate: P1 green; existing suite green; `make vet`; `gofmt`. Commit.

### P3 — plants and gates

| Plant | Test that must fail |
| --- | --- |
| one tool's title changed in the inventory | `TestInventoryTitles` |
| `addTool` drops `Title` | `TestTitlesOnWire` |
| `staticInventory` drops `Title` | `TestTitlesOnStaticFallback` |
| one entry's title removed | `TestInventoryTitles` |
| a title set equal to the tool's `name` | `TestInventoryTitles` |

Gates: full suite via `make test` on a fleet host or local `run-heavy`;
`make lint` (local `run-heavy` acceptable per D1/D2's recorded experience; the pre-existing
`service_lifecycle.go:69` SA4023 is linux-only, record 0012's, out of scope); `make vet`; `gofmt`;
`make check-records`; `make markdownlint`. Validation runs through the Makefile targets, not raw
`go` commands; single-package iteration during P1/P2 may use `go test ./onepkg`.

Exit: every plant failed; gates green; execution record updated; commit.

## Verification

* P1 red before, green after, red on each plant.
* The wire test proves what clients receive; the fallback test proves both emission paths agree
  (MADR S7).
* No handler test changes: no behaviour changed.

## Rollback

Revert the commit. The inventory loses the titles, the two mappings lose the field, and clients
display `name` again (the SDK's last-precedence display source). No stored data, no migration.

## Completion Criteria

1. All 18 own tools carry the D3 title in the inventory, pinned by the guard test.
2. Registered `tools/list` and the static fallback both emit it, pinned by the wire tests.
3. Every plant failed; gates green; no handler behaviour changed.

## Execution record

### Approval — 2026-11-26

Owner: "accept d2, approval to proceed." D2 accepted; D3 and D4 approved. PLAN `in-progress`;
commit and push to `main` authorized for this session (standing from the D2 approval turn).
D3 executes first, then D4 (both serialize on `inventory.go`).

### P0 — 2026-11-26, re-grounded at `96d9439` (pass 1)

- `InternalTool` five fields, no `Title` (`handlers.go:29-35`); `addTool` maps four spec fields
  (`:241-246`); `staticInventory` the same (`list_tools_handler.go:96-100`).
- Inventory: 18 `"name":` entries, 0 `"title"` keys.
- D1 helpers confirmed reusable: `newTestHandler` (`common_test.go:13`), `emittedToolsList`
  (`pipeline_session_store_test.go:19`, behind `magictoolstestseam`), the S5 explicit-absence
  pattern (`annotations_wire_test.go`).
- No deviation.

### P1 — 2026-11-26, tests first (pass 1)

- `internal/handler/inventory_titles_test.go`: `d3Titles()` mirrors the MADR's D3 table;
  `TestInventoryTitles` pins presence, non-emptiness, distinctness from `name`, exact wording.
- `internal/handler/titles_wire_test.go` (behind `magictoolstestseam`): `TestTitlesOnWire`
  (17 titled own tools on the wire; `get_internal_logs` pinned title-free per S5),
  `TestTitlesOnStaticFallback` (all 18).
- Seen red: `tl.Title undefined (type InternalTool has no field or method Title)` — the stated
  reason.

### P2 — 2026-11-26, plumbing and inventory (pass 1)

- `InternalTool` gains `Title string` (`json:"title,omitempty"`); `addTool` and
  `staticInventory` map it.
- All 18 inventory entries carry the D3 title verbatim.
- `TestInventoryTitles`, `TestTitlesOnWire`, `TestTitlesOnStaticFallback` green; D1/D2 guards
  still green (`TestInventoryAnnotations`, `TestAnnotationsOn*`, `TestInventoryConstraints`).
- `gofmt` clean; `make vet` clean.

### P3 — 2026-11-26, plants and gates (pass 1)

**Plants (scratch copy of `git archive HEAD` plus the P1/P2 working files, each seen to fail):**

| Plant | Test that failed | First failure line |
| :--- | :--- | :--- |
| self_check title changed | `TestInventoryTitles` | `self_check: title = "Gateway self check", want "Run gateway self-diagnostics"` |
| addTool drops Title | `TestTitlesOnWire` | `align_tools: wire title = "", want "Find the right tool"` |
| staticInventory drops Title | `TestTitlesOnStaticFallback` | `sync_servers: fallback title = "", want "Refresh server tool index"` |
| wake_servers title removed | `TestInventoryTitles` | `wake_servers: no title` |
| align_tools title set to its name | `TestInventoryTitles` | `align_tools: title equals the programmatic name` |

Scratch copies cleaned up after the runs.

**Gates:**

- `make test` on `<windows-host>` via `run-on-host --need any`: exit 0, 713 top-level passes,
  0 failures; `TestInventoryTitles`, `TestTitlesOnWire`, `TestTitlesOnStaticFallback` all PASS.
- `make lint` on `<linux-host>` via `run-on-host --need linux`: exit 2, one finding — the
  pre-existing `service_lifecycle.go:69` SA4023 (record 0012's, linux-only, out of scope).
  No finding in this plan's files. A local `run-heavy` lint re-run was not forced: the local
  queue was busy with other sessions' gates, and D2's local run on the same toolchain already
  recorded 0 issues; the linux run confirms no new finding.
- `make vet`: clean. `gofmt -l internal/handler/`: clean. `make check-records`: exit 0.
  `make markdownlint`: 0 issues.

**Completion criteria:** all three met — 18 titles pinned in the inventory, both emission paths
pinned, every plant failed and gates green with no handler behaviour change.

PLAN remains `in-progress` pending owner acceptance of the execution.
