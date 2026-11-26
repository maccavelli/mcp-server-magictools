---
status: proposed
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

Gate: P1 green; existing suite green; `go vet`; `gofmt`. Commit.

### P3 — plants and gates

| Plant | Test that must fail |
| --- | --- |
| one tool's title changed in the inventory | `TestInventoryTitles` |
| `addTool` drops `Title` | `TestTitlesOnWire` |
| `staticInventory` drops `Title` | `TestTitlesOnStaticFallback` |
| one entry's title removed | `TestInventoryTitles` |
| a title set equal to the tool's `name` | `TestInventoryTitles` |

Gates: full suite `-tags magictoolstestseam ./...` on a fleet host or local `run-heavy`;
`make lint` (local `run-heavy` acceptable per D1/D2's recorded experience; the pre-existing
`service_lifecycle.go:69` SA4023 is linux-only, record 0012's, out of scope); `go vet`; `gofmt`;
`check_records.py --check`; markdownlint on this plan.

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

No phase has run. Awaiting owner approval.
