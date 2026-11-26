---
status: implemented
date: 2026-11-26
associated-madr: "0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md"
---
# Express handler constraints as schema keywords

Associated MADR: [0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md](0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md)

Implements the MADR's **D2** only. D1 (annotations) is implemented; D3 (titles) and D4 (`$schema`) are
separate plans sharing this number.

## Goal

The inventory's schemas carry the constraints the handlers already enforce — as JSON Schema keywords,
not only prose — and `update_config.key`'s enum advertises every key the handler accepts. A client can
check a value's range before calling, and can discover all 14 config keys from the schema. No handler
behaviour changes.

## Scope

### In scope

| Component | Planned work |
| --- | --- |
| `internal/handler/inventory.go` | `analyze_system_logs.lines`: `minimum: 1, maximum: 1000, default: 50`. `list_tools.options`: description; `options.max_tools`: `minimum: 1, default: 1000`. `update_config.key`: enum completed to 14; `value` description carries the per-key rules. `get_internal_logs.max_lines` (inventory copy): `minimum: 0, maximum: 1000, default: 25`. |
| `internal/handler/inventory_constraints_test.go` (new) | Guard: each keyword equals the measured handler value; the enum is exactly the 14-key set; `options` has a description. |

### Out of scope

* Handler behaviour: the keywords document what the handlers do; nothing clamps or validates
  differently.
* `get_internal_logs`'s mcplib-registered wire copy (`mcplib@v1.4.1/diagnostic.go:111-115` declares
  `"default": 0` while its behaviour defaults to 25, `:136-141`): dependency-owned, fixed at the
  go-mcp-lib swap (MADR S5). The inventory copy is corrected here.
* The `update_config` persistence gap (`vector_enabled` absent from the runtime-patch switch,
  `diagnostic_handlers.go:276-306`): MADR S4, a separate defect.
* Per-key `value` constraints as schema branches: a flat schema cannot branch on `key` without a
  union (the 0013 rule); the rules stay in the description and the handler.
* D1, D3, D4.

## Grounding Reference

Every fact read at `c5dca3d` (HEAD when this plan was drafted). Re-ground at execution HEAD.

| Fact | Evidence at `c5dca3d` |
| --- | --- |
| `lines`: default 50, `>0` honoured, clamp 1000 | `internal/handler/diagnostic_handlers.go:178-185` (`linesToScan := 50`; `if input.Lines > 0`; `if linesToScan > 1000`) |
| `lines` schema today: prose only | `internal/handler/inventory.go:134-137` ("Integer limit of logs to parse (default 50).") |
| `max_tools`: handler default 1000, no maximum | `internal/handler/diagnostic_handlers.go:436-439` (`MaxTools: new(1000)` twice) |
| `max_tools` schema today: no bounds | `internal/handler/inventory.go:299-302` |
| `options` has no description | `internal/handler/inventory.go:296-305` |
| `update_config.key` enum: 10 keys | `internal/handler/inventory.go:242-253` |
| `UpdateConfigValue` accepts 14 keys | `internal/config/config.go:924`, cases at `:929-1052`: logFormat, logLevel, mcpLogLevel, squeezeLevel, scoreThreshold, confidenceGap, vector_enabled, validateProxyCalls, pinnedServers, trustServers, squeezeBypass, ringBufferTargets, tokenSpendThresh, lruLimit |
| Per-key value rules | `config.go:933` (logFormat json\|text), `:945` (squeezeLevel 0–5), `:976,983` (scoreThreshold/confidenceGap 0.0–1.0), `:1042` (tokenSpendThresh ≥ 0), `:1049` (lruLimit ≥ 1); booleans accept true/1/yes/on, false/0/no/off (`:990-995,1000-1005`) |
| `max_lines` behaviour: `<=0` → 25, clamp 1000 | `mcplib@v1.4.1/diagnostic.go:19-20` (constants 25, 1000), `:136-141` (the clamp) |
| `max_lines` inventory copy today: prose only | `internal/handler/inventory.go:81-84` ("(0-1000). Defaults to 25.") |
| mcplib wire copy's contradictory default | `mcplib@v1.4.1/diagnostic.go:111-115` (`"default": 0`) |
| Sanitiser passes constraint keywords through | `internal/util/schema_util.go:39-64` reads/writes only `type`/`properties` |
| Schema formatter reads `enum` | `internal/handler/schema_formatter_helpers.go:25-34` — the completed enum renders with no formatter change |

### Keyword semantics against handler behaviour (the honesty table)

The MADR's D2.5 rule: no keyword the handler does not enforce; where the handler clamps rather than
rejects, the description says clamped.

| Property | Keywords | Handler behaviour | Honesty note |
| --- | --- | --- | --- |
| `analyze_system_logs.lines` | `minimum: 1, maximum: 1000, default: 50` | `<=0` → 50; `>1000` → 1000 | The handler *clamps*, never rejects. `minimum: 1` documents the meaningful range (0 and negatives fall back to the default); the description states that out-of-range values are clamped, not refused. A strict validator would reject 0, which the handler accepts — the description carries that nuance, and the keyword documents the intended range per the MADR. |
| `list_tools.options.max_tools` | `minimum: 1, default: 1000` | nil → 1000; no upper bound | No `maximum`: the handler has none. |
| `get_internal_logs.max_lines` (inventory copy) | `minimum: 0, maximum: 1000, default: 25` | `<=0` → 25; `>1000` → 1000 | `minimum: 0` matches the documented "(0-1000)" range; 0 means "use the default". Clamped, not refused. |
| `update_config.key` | `enum` of 14 | `UpdateConfigValue` refuses unknown keys (`config.go:1053-1054`) | The enum is exactly the accepted set — a true rejection boundary. |

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

Re-read every grounding row at HEAD; confirm the 14-key set against `config.go`'s switch, the clamp
values, and the mcplib constants. Record results.

Exit: every row confirmed or a deviation recorded.

### P1 — tests first (red)

File: `internal/handler/inventory_constraints_test.go`.

`TestInventoryConstraints`: parse the inventory; assert —

1. `analyze_system_logs.lines`: `minimum == 1`, `maximum == 1000`, `default == 50`.
2. `list_tools.options.max_tools`: `minimum == 1`, `default == 1000`, no `maximum`.
3. `list_tools.options`: non-empty `description`.
4. `get_internal_logs.max_lines`: `minimum == 0`, `maximum == 1000`, `default == 25`.
5. `update_config.key.enum`: exactly the 14-key set (order-insensitive), no duplicates.
6. `update_config.value.description`: mentions each per-key rule (squeezeLevel 0–5,
   scoreThreshold/confidenceGap 0.0–1.0, logFormat json|text, tokenSpendThresh, lruLimit, boolean
   spellings).

Run against the unchanged tree; keep the failures. Exit: each red for the stated reason.

### P2 — the inventory edits

File: `internal/handler/inventory.go`. Apply the keywords and descriptions of the honesty table;
complete the enum; describe `options`. Descriptions keep their prose and add the clamp nuance where
the handler clamps.

Gate: P1 green; existing suite green; `go vet`; `gofmt`. Commit.

### P3 — plants and gates

| Plant | Test that must fail |
| :--- | :--- |
| `lines.maximum` changed to 500 | `TestInventoryConstraints` |
| enum reduced to the old 10 | `TestInventoryConstraints` |
| `max_lines.default` changed to 0 (mcplib's wire value) | `TestInventoryConstraints` |
| `options` description removed | `TestInventoryConstraints` |
| `max_tools.maximum` added (handler has none) | `TestInventoryConstraints` |

Gates: full suite on a fleet host or local `run-heavy`; `make lint` (local `run-heavy` acceptable per
the D1 plan's recorded experience); `go vet`; `gofmt`; `check_records.py --check`; markdownlint on
this plan.

Exit: every plant failed; gates green; execution record updated; commit.

## Verification

* P1 red before, green after, red on each plant.
* The enum guard is the machine-checkable form of "the schema advertises what the handler accepts".
* No handler test changes: no behaviour changed.

## Rollback

Revert the commit. The schemas lose the keywords and the four enum entries; clients lose the
pre-call range information and the discoverability of four config keys. No stored data, no migration.

## Completion Criteria

1. Every keyword of the honesty table present and equal to the measured handler value.
2. The enum is exactly the 14-key set.
3. `options` described; per-key value rules in the `value` description.
4. Every plant failed; gates green; no handler behaviour changed.

## Execution record

### Approval — 2026-11-26

Owner: "accept and approved to proceed, commit and push to main for the session". PLAN `in-progress`;
commit and push to `main` authorized for this session's phases.

### P0 — 2026-11-26, re-grounded at `09c5ded` (pass 1)

- `lines` clamp confirmed (`diagnostic_handlers.go:178-185`); `max_tools` default 1000 (`:436-439`).
- `config.go:929-1052`: 18 `case` lines = 14 top-level keys + 4 boolean-spelling sub-cases; the
  14-key set holds.
- mcplib constants 25/1000 confirmed (`diagnostic.go:19-20`).
- No deviation.

### P1 — 2026-11-26, tests first (pass 1)

- `internal/handler/inventory_constraints_test.go`: `TestInventoryConstraints` with five subtests
  (`analyze_system_logs.lines`, `list_tools.options.max_tools`, `get_internal_logs.max_lines`,
  `update_config.key_enum`, `update_config.value_per-key_rules`) pinning the honesty table, the
  14-key enum set, the `options` description, and the per-key value rules.
- Seen red against the unchanged inventory (keywords absent, enum at 10 keys, `options`
  undescribed), then green after P2.

### P2 — 2026-11-26, inventory edits (pass 1, commit `85579a7`)

- `internal/handler/inventory.go`: keywords applied per the honesty table — `lines`
  `minimum: 1, maximum: 1000, default: 50`; `max_lines` `minimum: 0, maximum: 1000, default: 25`
  with the clamp nuance in the description; `max_tools` `minimum: 1, default: 1000`, no `maximum`;
  `options` described.
- `update_config.key` enum completed to the 14-key set (`vector_enabled`, `ringBufferTargets`,
  `tokenSpendThresh`, `lruLimit` added); per-key value rules written into the `value` description.
- `TestInventoryConstraints` green; no handler behaviour changed (2 files, +146 −5).

### P3 — 2026-11-26, plants and gates (pass 1)

**Plants (scratch copies, each seen to fail `TestInventoryConstraints`, cleaned up):**

| Plant | Result |
| :--- | :--- |
| `lines.maximum` changed to 500 | failed as planned |
| enum reduced to the old 10 | failed as planned |
| `max_lines.default` changed to 0 | failed as planned |
| `options` description removed | failed as planned |
| `max_tools.maximum` added | failed as planned |

**Gates:**

- Full suite `-tags magictoolstestseam ./...` on `<windows-host>` via `run-on-host`: exit 0.
  Re-run as `make test` locally via `run-heavy`: exit 0, 723 top-level passes, 0 failures
  (gates run through the Makefile targets, not raw `go` commands).
- `make lint` on `<linux-host>` (WSL Ubuntu-24.04) via `run-on-host --need linux`: exit 2, one
  finding — `service_lifecycle.go:69` SA4023, the pre-existing linux-only issue that belongs to
  record 0012, out of this plan's scope. No finding in this plan's files. (The session note that
  this distro lacks golangci-lint is stale: the run found and used
  `$HOME/go/bin/golangci-lint`.)
- `make lint` locally via `run-heavy`: 0 issues (the SA4023 does not reproduce on darwin),
  matching D1's recorded gate evidence.
- `TestInventoryConstraints` re-run at HEAD: PASS (5/5 subtests). `make vet`: clean.
  `gofmt -l` on both changed files: clean. `make check-records` (`--check-all`): exit 0.
  `make markdownlint`: 0 issues.

**Completion criteria:** all four met — keywords equal the measured handler values, the enum is
exactly the 14-key set, `options` described and per-key rules documented, every plant failed and
gates green with no handler behaviour change.

PLAN status: `implemented` — owner accepted the execution 2026-11-26 ("accept d2, approval to
proceed").
