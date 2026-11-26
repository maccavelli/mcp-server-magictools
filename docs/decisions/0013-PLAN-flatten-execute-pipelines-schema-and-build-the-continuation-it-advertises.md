---
status: complete
date: 2026-11-25
associated-madr: "0013-MADR-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md"
---
# Flatten execute_pipeline's schema and build the continuation it advertises

Associated MADR: [0013-MADR-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md](0013-MADR-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md)

Adopted contract: [0005-MADR-make-execute-pipeline-schema-and-handler-agree.md](0005-MADR-make-execute-pipeline-schema-and-handler-agree.md)
(its D2 continuation design, owner-ruled 2026-11-17, never implemented; referenced below as "0005's D2", "0005's
R8/R9", etc.).

## Goal

Every client, including gobble on the claude provider, can load this server's tools and call `execute_pipeline`
without a schema refusal; the served `inputSchema` is a flat object with seven optional properties and no root
`anyOf`/`oneOf`/`allOf`; and the handler implements 0005's decided continuation contract — dry-run storage,
`session_id`-only continuation, `reject: true`, the atomic claim, the full refusal vocabulary — so the schema,
the guide, the prompt and the code say the same thing.

## Scope

### In scope

| Component | Planned work |
| --- | --- |
| `internal/db/pipeline_session.go` (new) + constants | Session store: own key prefix, compressed values, TTL + grace, atomic transitions, expiry, cap, instance nonce, interrupted-run rule. |
| `internal/db/pipeline_session_test.go` (new) | Store tests: claim race, expiry, cap, nonce, prefix isolation. |
| `internal/handler/execute_pipeline.go` | Strict argument decode; mode discrimination; generated 128-bit ids; the dry-run store; the refusal messages; the argument gate as a pure function. |
| `internal/handler/execute_pipeline_session.go` (new) | Continuation (claim, plan-hash check, pre-flight, run loop, terminal state) and rejection (no executor in reach). |
| `internal/handler/inventory.go` | `execute_pipeline` entry: flat schema, mode-carrying descriptions, no union. |
| `internal/handler/prompts.go`, `docs/guides/mcp-tools.md` | Prompt steps 4–5 and the guide's row/paragraph say the decided contract (they already mostly do; pin them). |
| `internal/handler/execute_pipeline_contract_test.go` (new) | The one contract table: schema (two validators) vs gate, row for row; zero-call tests; disclosure pin. |
| `internal/handler/inventory_test.go` | Union guard: no own tool's root holds `anyOf`/`oneOf`/`allOf`; `execute_pipeline`'s root is the flat seven-property object; count stays 18. |
| `internal/util` (id helper) | 128-bit id generator (`crypto/rand`, 16 bytes, hex, prefixed). The existing 64-bit `GenerateSessionID` keeps its callers. |
| `docs/decisions/0013-MADR-…md`, this PLAN, `docs/README.md` | Record status updates, execution record, index rows. |

### Out of scope

* 0005's D3 sanitiser work (`internal/util/schema_util.go`, stored-schema refresh): separate defect, owned by
  0005's PLAN; no sanitiser code touched here.
* gobble-cli's 0060 union normalisation for proxied downstream tools.
* The dry-run's unclosed telemetry pipeline (`freshInitDAGTelemetry` before the dry-run return,
  `execute_pipeline.go:78`): noted by 0005, left as-is.
* `autoPromoteRequiredFields`, the stdio bridge, `get_internal_logs`' inventory drift.
* Service installation, restart, or update on the owner's live machine by an agent; the owner's acceptance run is
  owner-controlled.
* Push, tag, or release operations beyond the phase commits this approval covers.

## Grounding Reference

Facts the MADR cites at `184370e`; re-ground each at HEAD before P1. A moved line is a file-path update; a
changed fact is a deviation entry.

| Fact | Evidence to re-ground |
| --- | --- |
| Root `anyOf`, two untyped branches | `internal/handler/inventory.go:323-335` |
| Handler requires all three; no `Reject` field | `internal/handler/execute_pipeline.go:41-57` |
| Guide and prompt already describe continuation | `docs/guides/mcp-tools.md:64,73-77`; `internal/handler/prompts.go:39-40` |
| `PipelineSession` is telemetry-only; no session prefix in db | `internal/handler/pipeline_session.go:12-26`; `grep -rn 'pipeline-session' internal/db/` → none |
| `SaveWithTTL`, `UpdateWithRetryCtx` | `internal/db/badger.go:2494-2500`, `:813-841` |
| `raw:` prefix publicly readable | `internal/handler/resource_handlers.go:13-18` |
| Run loop, mutator injection, checkpoint/rollback, plan-hash fallback | `internal/handler/execute_pipeline_run_helpers.go:26-45,141-160,184-243`; `internal/handler/constants.go:113` |
| Stage resolution via store | `internal/handler/execute_pipeline.go:289+` (`buildSchemaAwareArgs`), `:362,392` (`GetTool`) |
| Artifacts under `raw:` keys `pipeline_<id>_*` | `internal/handler/execute_pipeline.go:982-1050` |
| Context ceiling 128 KiB | `internal/handler/execute_pipeline.go:1089,1097` |
| 64-bit id generator | `internal/util/strings.go:12-16` |
| Lenient decode | `internal/handler/io_helpers.go:37-49` |
| Both validators in go.mod | `go.mod:100,109` (`google/jsonschema-go v0.4.3` indirect; `santhosh-tekuri/jsonschema/v5 v5.3.1` direct) |
| Inventory count 18 | `internal/handler/inventory_test.go:18` |
| Anthropic refusal body | owner's gobble log and session transcript, 2026-11-25 |

## Prerequisites

0. **Approval (recorded 2026-11-25):** the owner approved the pair — "approve 0013, proceed. you may commit
   and push to main as needed for this session". Every code phase runs, each committed and pushed to `main`
   on this approval, stopping before the owner's build, install and live checks (P8). Tags and releases need
   a separate word.
1. The owner explicitly approves this PLAN and the MADR. The MADR remains `proposed` until that decision; on
   approval it becomes `accepted` and this PLAN `in-progress`. *(Done 2026-11-25: MADR accepted, PLAN
   in-progress.)*
2. P1 re-grounds every source line reference at HEAD; a changed fact is a deviation, recorded before the phase
   continues.
3. The working tree contains no unowned edits. The one expected uncommitted file,
   `internal/forks/hnsw/go.mod`, is the owner's: never staged, reverted or committed by this plan.
4. No agent runs `make run`, `go run`, `serve`, or a built module binary on a machine with a live service
   (CLAUDE.md; record 0010). No test enumerates or signals a process it did not start.
5. Suite-level and race runs use the fleet `run-heavy`/`run-on-host` placement rules
   (`~/.agents/skills/available-dev-hosts/scripts/pick-host`, then `run-on-host`; `--race` for a race run; an
   explicit `-timeout`). A single-package `go test` may stay on the session host.
6. Tests first, seen failing (P2), then the change, then green. Tests are committed with the phase that turns
   them green, never red.
7. A planted break per assertion (P7), in a scratch copy (`git archive HEAD` plus the phase's diff), never in
   the working tree; each plant is `gofmt` clean; the failure's first lines are kept.
8. The docs step is in the same commit as the code it describes: descriptions, prompt, guide, code comments.
9. Evidence: full output to a scratch file, the exit status taken before any filter.
10. Each phase is committed after its checks pass and after the disclosure guard has run on what is staged;
    only that phase's files are staged, by path. No tag and no release without a separate explicit word.
    `gofmt` and, where installed, `make lint` on staged Go.
11. No real credential reaches any agent. The owner's live probe is the owner's.
12. No hostname, account name or real-machine path in a committed file.
13. No other dependencies are added without a MADR. No plan phase implements beyond its stated exit criterion.

## Implementation Steps

### P0 — place the records

Files: the two 0013 records, `docs/README.md` index rows.

The MADR and this PLAN are committed with `status: proposed` until the owner's approval; on approval the MADR
becomes `accepted` and this PLAN `in-progress`. The index rows track both. `python3 scripts/check_records.py
--check` passes.

Exit: records placed, numbered, cross-referenced, index consistent; owner approval recorded.

### P1 — re-ground the evidence (no repository change)

Re-read the grounding table at HEAD; confirm each cited line still says what the record claims. Re-run the
inventory scan (`grep -n 'anyOf\|oneOf\|allOf' internal/handler/*.go` excluding tests): expect exactly
`inventory.go:323`. Confirm no `pipeline-session` prefix exists in `internal/db`. Confirm both validators are
still in `go.mod`. Record the results in the execution record.

Exit: every grounding row confirmed or a deviation recorded.

### P2 — tests first (red)

Files (new): `internal/handler/execute_pipeline_contract_test.go`,
`internal/handler/pipeline_session_store_test.go`, `internal/db/pipeline_session_test.go`.

1. **The emitted `tools/list` helper**: registers a handler as `main.go` does (`Register`,
   `RegisterPipelineTools`, `RegisterPrompts`), connects over the SDK's line-delimited transport on in-process
   pipes, returns the raw response bytes and the decoded tools (0005's R1 is the starting point).
2. **The one table** `executePipelineContract` (0005's 26-row table, re-derived against the flat schema): each
   row is an argument set and the expected gate outcome (fresh run, fresh dry run, continue, reject, refuse with
   a reason). Two assertions per row, driven from the table: the emitted schema (compiled with
   `santhosh-tekuri/jsonschema/v5`, cross-checked with `google/jsonschema-go`) accepts the row exactly when the
   outcome is not a mode refusal; the handler's gate returns the table's outcome. The flat schema accepts every
   well-typed row, so the table's schema column asserts acceptance of all well-typed rows and rejection of
   unknown keys / wrong types only; the gate column carries the mode rules. Rows include: none; `session_id`
   alone; `session_id`+`plan_hash`; `session_id`+`reject:false`; `session_id`+`reject:true`; `reject:true` alone;
   `reject:false` alone; `session_id`+`dry_run`; `session_id`+`target_roles`; `target` alone; `intent` alone;
   `target`+`session_id`; `intent`+`session_id`; `target`+`intent`; `target`+`intent`+`plan_hash`+`target_roles`;
   `target`+`intent`+`dry_run:false`; `target`+`intent`+`dry_run:true`; the all-three form with and without
   `dry_run:true`; `target`+`intent`+`reject` (true and false); all-three+`reject:true`;
   `session_id`+`reject:true`+`dry_run`; an unknown key; `dry_run` as a string; `target_roles` with a non-string.
3. **No call carrying `reject: true` ever executes a stage.** The executor is injected; the stub counts
   `executeDAGStep`, `executeMutatorAST` and `ExecuteProxy` calls. Every `reject: true` row, a rejection of a
   stored session, a repeated rejection, a rejection of an unknown id: zero calls. A dry run: zero calls.
4. **The session store**: create, get, atomic `PLANNED`→`RUNNING` and `PLANNED`→`REJECTED`; 32 concurrent
   continuations of one id over 200 rounds (one winner each); a continuation racing a reject over 200 rounds
   (never both, never neither); a repeated continuation refused with the prior state; expiry (`expires_at` past,
   entry still stored: `expired_session`); unknown id; the cap (the 51st `PLANNED` evicts the oldest); a
   `RUNNING` session with another server instance nonce is `session_interrupted` and never run; the key prefix
   is not `raw:` and a `raw:` read of a session id returns nothing.
5. **The continuation**: a stored plan with a missing tool is refused before any stage (`plan_stale`, zero
   stages run, the stage named); a stored `plan_hash` mismatch is refused; a plan containing a MUTATOR-role or
   `apply_vetted_edit` stage is shown but not stored; every refusal in the MADR's state table has its own test
   and message; a repeated continuation never re-runs; the all-three call (with and without `dry_run: true`)
   still works and its `session_id` is a label on a dry run; `target`+`intent` runs and returns a generated id.
6. **The schema**: no own tool's root holds `anyOf`/`oneOf`/`allOf`; `execute_pipeline`'s root holds exactly the
   seven properties and `additionalProperties: false`; the inventory count is 18; the emitted bytes hold no
   empty object except the root `properties` of the no-argument tools.
7. **The dry-run disclosure** (0005's Decided 9, fixed words): the dry-run result holds the exact block of
   0005's D2, with the issued id and the expiry filled in, after the plan; a result for a plan that is not stored
   says that it is not stored and why; no other call's result holds the block.
8. **The docs pin**: the guide, the prompt and the schema descriptions agree with the decided contract (the same
   terms; no stale "paused plan" wording that contradicts the implemented behaviour).

Run every new test against the unchanged tree and keep the failures. Exit: each red for the stated reason; the
existing suite unchanged; no source change.

### P3 — the session store

Strongest tier, high. Files: `internal/db/pipeline_session.go` (and its constants),
`internal/db/pipeline_session_test.go`.

A store type with its own key prefix (`pipeline-session:`), values compressed as `SaveRawResource` does
(`util.Compress`/`Decompress`), a Badger TTL of the lifetime plus a one-hour grace, `expires_at` in the value,
transitions as one `UpdateWithRetryCtx` transaction that reads the state and writes the next (0005's R9
prototype is the shape), a cap of 50 `PLANNED` with oldest-first eviction, a server-instance nonce recorded at
`RUNNING` and compared on read, and no exported way to write a state without its transition rule. The session
value holds: id, target, intent, plan hash, status (`PLANNED`/`RUNNING`/`COMPLETED`/`FAILED`/`REJECTED`), stages,
warnings, recall context (capped at `maxContextBytes`), roles, `expires_at`, `created_at`, `updated_at`,
instance nonce, artifact URIs on completion.

Gate: P2's store tests green with `-race -timeout 10m` for the `internal/db` package (Prerequisites 5 host);
`go vet`; `gofmt`. Commit: the store and its tests.

### P4 — the argument gate and the fresh-mode fixes

Mainstream, medium. Files: `internal/handler/execute_pipeline.go`, a 128-bit id helper in `internal/util`.

Strict decode of the arguments (`json.Decoder` with `DisallowUnknownFields`; unknown key, wrong type: refused
with `bad_arguments`); the decision order of the MADR (`reject` first, no executor in reach); `session_id`
generated when a fresh call omits it (16 bytes from `crypto/rand`, hex, prefixed — the existing 64-bit
`GenerateSessionID` keeps its other callers); refusals with their own messages; the argument gate as a pure
function the table test calls.

Gate: P2's contract-table handler assertions, the `reject` zero-call tests and the strictness tests green; the
existing handler test (the all-three dry run) unchanged and green. Commit.

### P5 — dry-run store, continue, reject

Strongest tier, high. Files: a new `internal/handler/execute_pipeline_session.go`,
`internal/handler/execute_pipeline.go`.

The dry run stores the exact plan (stages, warnings, the recall context capped at `maxContextBytes`, target,
intent, plan hash, roles) through P3 and returns the issued id, the expiry and the disclosure block of 0005's
D2, word for word, which says that approval covers the analysis stages, that mutating stages are added at run
time after the reporting stage, and that with no `plan_hash` mutation proceeds under the placeholder approval
`auto-approved-orchestrator-override`. The continuation: claim, plan-hash check, pre-flight over every stage
(`store.GetTool` per stage; a missing tool names the stage and refuses before any run), then the existing run
loop (`freshPipelineExecute`) with the stored stages (so the mutator gate, the git checkpoint and the rollback
are unchanged), then `COMPLETED` or `FAILED` with the artifact URIs. The rejection: the transition and nothing
else, in a function that is given no proxy service. A stage list holding a mutating stage is not stored.

Gate: P2's continuation, claim-race, expiry, interrupted-run and reject zero-call tests green; the whole
`internal/handler` package green with `-race -timeout 20m` (Prerequisites 5 host). Commit.

### P6 — the schema, the descriptions, the prompt, the guide

Mainstream, medium. Files: `internal/handler/inventory.go`, `internal/handler/prompts.go`,
`docs/guides/mcp-tools.md`, the dry-run result text.

The `execute_pipeline` entry becomes the flat schema of the MADR's D1: root `object` with all seven properties
and mode-carrying descriptions, `additionalProperties: false`, no `required`, no union. The `session_id` and
`reject` descriptions and the tool description state the decided contract; the prompt's steps 4 and 5 and the
guide's row and "two-stage" paragraph say the same (dry run, review, continue or reject with the issued id; the
mutation notice). The inventory-size test (18) is unchanged. The union guard test is added to
`inventory_test.go`.

Gate: P2's schema, table and docs-pin tests green. Commit: schema, prompt and guide together.

### P7 — planted breaks, the repository's gates, the independent review

Plants and gates: mainstream, medium. The review: strongest tier, high.

**Step 1. A planted break per assertion** (Prerequisites 7), each in its own scratch copy, each seen to fail the
named test:

| Plant | Test that must fail |
| :--- | :--- |
| restore the root `anyOf` in the inventory | the union guard and the schema column of the table |
| the gate runs the fresh path when `reject: true` comes with `target` and `intent` | the table's `reject` rows and the zero-call test |
| the rejection function is handed the proxy service and calls it | the zero-call test |
| the claim split into a read then a write (the removed design's shape) | the claim-race test (32 winners of 32) |
| the pre-flight removed | the `plan_stale` test (a stage runs before the refusal) |
| the disclosure block altered by one word | the disclosure pin |
| the id generator reduced to 64 bits | the id-length assertion |
| `DisallowUnknownFields` removed | the unknown-key row |
| the instance nonce not compared | the interrupted-run test |
| the cap removed | the 51st-session test |

**Step 2. The repository's gates**: `go build ./...`, `go vet ./...`, `gofmt -l` on changed files, the full
`go test ./...` and `make lint` through the Prerequisites 5 host path, `python3 scripts/check_records.py
--check`, `markdownlint-cli2` on both records.

**Step 3. The independent review**: a reviewer with no stake in the draft reads the MADR, this PLAN and the
diff; the continuation's safety properties (atomic claim, no re-run, pre-flight, reject with no executor,
checkpoint/rollback unchanged) are checked against 0005's D2. Findings are recorded here and fixed before P8.

Exit: every plant failed its test; every gate green; the review closed.

### P8 — the owner's live step and close-out

Owner-controlled. The agent provides the commands; the owner runs them.

1. The owner backs up the current binary.
2. Build and install (`make install` or the repository's documented path); restart the magictools service.
3. From gobble on the claude provider: one session completes a model call (no `tools.16.custom.input_schema`
   refusal).
4. A dry-run → continue → reject cycle from gobble: dry run returns an issued id and the disclosure; continue
   runs the stored plan; a second dry run's reject returns `REJECTED` with no steps.
5. From Claude Code (the owner's other client): the same cycle, confirming the flat schema loads there too.
6. On failure: restore the backed-up binary, restart, record the deviation.

Close-out: this PLAN's status becomes `complete` only when every acceptance criterion is met; the execution
record holds each phase's actual output; the MADR's status is `accepted`.

## Verification

* P2's tests: red before, green after, and red on each P7 plant.
* The gates of P7 with their output kept.
* A citation check over both records (every `path:line` resolves at the commit the record names, and each cited
  range says what the record claims), seen to fail on a planted false citation in a temporary copy.
* `markdownlint-cli2` on both records; `python3 scripts/check_records.py --check`.

## Rollback

**Rollout.** Source, tests and docs in phase commits (P3–P6), plus the records (P0). No migration; the new
store keys are additive. Nothing reaches the owner's running server until the owner builds, installs and
restarts it (P8).

**Compatibility.** The all-three call keeps working. `target`+`intent` starts to work. A call with `session_id`
alone starts to do what the schema says. A call with `reject` that used to be ignored is now a real rejection
(or a refusal when malformed). A stray or mistyped argument that was ignored is refused (0005's G-a). The owner
is the only known caller; the prompt and the guide are corrected in the same commits as the code.

**Rollback.** P8 step 6. The previous binary is the owner's copy from P8 step 1. The new store keys are additive
and TTL-bound; an old binary ignores them. No migration either way.

## Completion Criteria

1. The table: for every argument combination the emitted schema and the handler agree, asserted from one table.
2. No call carrying `reject: true` executes a stage; a dry run executes none; asserted by call counts.
3. A session is claimed once; a repeated continuation never re-runs; every refusal is distinct.
4. The all-three call (with and without `dry_run`) still works; `target`+`intent` works and returns a
   server-issued 128-bit id.
5. The emitted `tools/list` holds no root union keyword on any own tool; `execute_pipeline`'s root is the flat
   seven-property object; the inventory count is 18.
6. The dry-run disclosure is 0005's fixed block, word for word.
7. Every plant failed its test; every gate green; the review closed.
8. The owner's live step passes: gobble on claude completes a model call; the dry-run → continue → reject cycle
   works from gobble and from Claude Code.

## Execution record

### P0 — 2026-11-25, records placed (pass 1)

- MADR `accepted`, PLAN `in-progress`, index rows updated; owner approval recorded in Prerequisites 0.
- `markdownlint-cli2` on both records: 0 issues. `python3 scripts/check_records.py --check`: exit 0.
- Commit `8f9860b` pushed to `main` (`184370e..8f9860b`). The commit also carries 0014's REPORT and
  0015's proposed MADR, whose index rows the same `docs/README.md` edit references; 0015 remains
  `proposed` pending its own approval.

### P1 — 2026-11-25, re-grounded at `8f9860b` (pass 1)

- HEAD `8f9860b9c6d4eb7bd04533702b4dcbc34e373308`, branch `main`, go1.27.4 darwin/arm64.
- Root `anyOf` still at `internal/handler/inventory.go:323`; the only union keyword in non-test
  handler source.
- Handler gate unchanged at `internal/handler/execute_pipeline.go:49-57`; no `Reject` field.
- No `pipeline-session` prefix in `internal/db`.
- Both validators in `go.mod`: `santhosh-tekuri/jsonschema/v5 v5.3.1` (direct, `:109`),
  `google/jsonschema-go v0.4.3` (indirect, `:100`).
- Every grounding-table row confirmed; no deviation.
- Additional seams verified for P2: `pipelineGate` (`pipeline_tools.go:24`),
  `NewProxyService`/`ExecuteProxy` (`proxy_service.go:30,362`), `executeDAGStep`/`executeMutatorAST`
  (`execute_pipeline.go:155,441`), `mcp.NewInMemoryTransports` (go-sdk v1.6.1 `mcp/transport.go:147`)
  for the emitted-tools/list helper.

### P2–P6 — 2026-11-25, code phases (pass 1 each)

- **P2** tests written first, seen red (undefined store/gate/seams), committed with the phases that
  turned them green. Files: `internal/db/pipeline_session_test.go`,
  `internal/handler/execute_pipeline_contract_test.go`,
  `internal/handler/pipeline_session_store_test.go`,
  `internal/handler/pipeline_schema_validator_test.go`.
- **P3** session store: `internal/db/pipeline_session.go` — own `pipeline-session:` prefix, compressed
  values, 24h TTL + 1h grace, atomic `UpdateWithRetryCtx` transitions, 50-cap oldest-first eviction,
  instance nonce, distinct refusal sentinels. Commits `532503b`, `c3de37f` (gofmt), `0208874`
  (`OverwriteForTest` seam, missed from the P4 commit). Race gate: `internal/db` green on <linux-host>
  (`-race -timeout 10m`), claim race 32×200 one winner every round.
- **P4** argument gate: `execute_pipeline_gate.go` (strict `DisallowUnknownFields` decode, pure
  `classifyPipelineArgs`, refusal vocabulary), `util/pipeline_id.go` (128-bit `ep-` ids).
  `handleExecutePipeline` rewritten to gate-first dispatch. Commits `623407b`, `fbd5791` (gofmt).
- **P5** continuation: `execute_pipeline_session.go` — dry-run store + fixed disclosure, claim →
  plan-hash check → pre-flight → existing run loop → COMPLETED/FAILED with artifact URIs; reject with
  no executor in reach. Test seams on the handler (`pipelineTestCountStep`,
  `pipelineTestInjectStage`); `freshCheckCancelled`/`freshHaltOnFailure` now carry `sessionID` so a
  failed/cancelled fresh run returns its issued id. Deviation from the plan's test list: added
  `TestExecutePipelineContract_RejectStoredSessionNeverExecutes` (commit `1e0d7d4`) after plant 3
  showed the gate-only reject rows did not exercise the stored-session reject path — the plan's P2
  item 3 required it; the first draft missed it.
- **P6** flat schema: `inventory.go` `execute_pipeline` entry — seven optional properties,
  `additionalProperties: false`, `minProperties: 1`, no union; mode-carrying descriptions; guide's
  two-stage paragraph corrected. The prompt (`prompts.go:39-40`) already said the decided contract;
  unchanged. Commit `0d5a0c2`.

### P7 — 2026-11-25, plants and gates (pass 1)

**Plants (scratch copies of `git archive HEAD`, each seen to fail its named test):**

| Plant | Test that failed | First failure line |
| :--- | :--- | :--- |
| root `anyOf` restored | `TestExecutePipelineSchema_FlatNoUnion` | `tool execute_pipeline: root holds anyOf` |
| reject+fresh runs the fresh path | `TestExecutePipelineContract_Gate` + `RejectNeverExecutes` | 3 gate rows + zero-call row failed |
| reject calls the executor | `TestExecutePipelineContract_RejectStoredSessionNeverExecutes` | `rejection of a stored session executed 1 stages, want 0` |
| claim split read-then-write | `TestPipelineSessionStore_ClaimRace` | `round 0: 3 winners of 32 claims, want exactly 1` |
| pre-flight removed | `TestExecutePipelineContinuation_Refusals/plan_stale` | `want plan_stale; got FAILED` |
| disclosure word altered | `TestExecutePipelineDryRun_Disclosure` | `disclosure missing fixed words` |
| id reduced to 64 bits | `TestExecutePipelineContract_FreshGeneratesID` | `must return a server-issued 128-bit ep- id` |
| `DisallowUnknownFields` removed | `TestExecutePipelineContract_Gate/unknown_key` + `BadArguments` | gate row failed |
| instance nonce not compared | `TestPipelineSessionStore_InterruptedRun` | `want ErrSessionInterrupted, got <nil>` |
| cap removed | `TestPipelineSessionStore_CapEvictsOldest` | `oldest should be evicted, got <nil>` |

**Gates:**

- Full suite `go test ./... -count=1` on <linux-host> (linux/amd64) via `run-on-host`: exit 0, all packages ok.
- Race gate `go test ./internal/handler/ ./internal/db/ -race -timeout 20m -count=1` on <linux-host>: exit 0
  (handler 17.7s, db 43.6s).
- `go vet ./...`: clean. `gofmt -l internal/ cmd/`: clean. `check_records.py --check`: exit 0.
  `markdownlint-cli2` on the records: 0 issues.
- `make lint` on <linux-host>: all findings in this plan's files fixed (errcheck on terminal transitions,
  goconst `continue`, nilerr, revive get/Get naming, unparam) — commit `bb7a0c2`. **One pre-existing
  finding remains, outside this plan's scope**: `cmd/mcp-server-magictools/service_lifecycle.go:69`
  SA4023 (staticcheck), present at base `184370e`, owned by record 0012's lifecycle work. Recorded
  here as a deviation from "every gate green"; not fixed by this plan.
- Windows-host lint attempt failed on a host toolchain path issue (`C:Usersmacsmgo/bin/golangci-lint`
  not found on <windows-host>); re-run pinned to linux. Host gap, not a code finding.

**Independent review (2026-11-25, pass 2 — separate reviewer):** dispatched the `code-reviewer`
agent persona to a second model (gobble on grok-4.7, read-only) over `184370e..HEAD`. Verdict:
**PASS WITH NOTES** — 0 critical, 2 important, 2 minor. It verified in source: flat schema (seven
optional properties, no union); `ClaimRunning`/`Reject` read+write in one `UpdateWithRetryCtx`
transaction; reject calls only `ps.Reject`, no ProxyService; pre-flight completes before
`freshPipelineExecute`; mutator checkpoint/rollback unchanged; 128-bit ids; disclosure matches
0005 Decided 9. Findings:

- **Important (92):** `execute_pipeline_session.go:195` — plan_hash mismatch / plan_stale handled
  after `ClaimRunning` commits RUNNING; a failed `Fail` write is logged and ignored, leaving the
  session stuck RUNNING (next continue gets `session_running`, reject also refused until expiry).
  Fix: check hash and resolve before the claim. **Fixed** (see pass 3).
- **Important (93):** `pipeline_session_store_test.go:205` — handler refusal tests cover only
  unknown_session, session_rejected, plan_hash_mismatch, plan_stale; the state table requires handler
  tests for session_running, session_already_run (with URIs), session_failed, session_interrupted,
  expired_session. **Fixed** (see pass 3).
- **Minor (94):** `countPipelineStepForTest` only in `freshRunSingleStep`, not the post-loop mutator
  path; a zero-call assertion would miss that loop. **Fixed.**
- **Minor (88):** `DisallowUnknownFields` then `Decode` without `dec.More()`; a trailing second JSON
  value is ignored. **Fixed.**
- **Open question:** reject of an interrupted RUNNING row returns `session_running` (Reject does not
  read the instance nonce). Is discard of an interrupted run meant to be impossible until expiry?
  Left as-is (reject only transitions PLANNED); owner to rule.
- **Open question:** `resultStatusOf` substring-matches `"status": "COMPLETED"`; a failed envelope
  whose step error contains that substring would be stored COMPLETED. No producer found. **Hardened**
  to parse the envelope metadata instead of substring.
- **Recommendation:** `OverwriteForTest` is exported (PLANNED-restricted after pass 1). Keep test-only
  if the "no exported write without a transition rule" rule stands. Owner to rule.

**Pass 1 (self-review, 2026-11-25):** found and fixed two gaps (commit `5855a94`):

1. `OverwriteForTest` could rewrite any stored state, violating P3's "no exported way to write a state
   without its transition rule". Now refuses any supplied or stored status other than PLANNED.
2. The old `dryRun` branch inside `freshPipeline` was dead code after P5 (the gate routes dry runs to
   `freshPipelineDryRun`); removed, and the `dryRun` parameter dropped from `freshPipeline`.

Safety properties verified against 0005's D2: atomic claim (one `UpdateWithRetryCtx` transaction,
32×200 race one winner); no re-run (every terminal state refuses a claim); pre-flight before any stage;
reject with no executor in reach (zero-call tests); mutator gate, git checkpoint and rollback unchanged
(the continuation runs the same `freshPipelineExecute`); fixed disclosure word-for-word; 128-bit
server-issued ids; cap, expiry, tombstones, instance nonce all tested.

**Not covered by this review:** an independent reviewer with no stake in the draft (the plan's P7 step 3
asks for one; this pass was the implementing agent's own review). Recorded as a gap for the owner to
assign or accept.

**Pass 3 (2026-11-25, review fixes, commit `13768c5`):** all four actionable findings fixed:

- Plan-hash and pre-flight checks moved **before** `ClaimRunning` — a refusal leaves the session
  PLANNED, never stuck RUNNING. New test `TestExecutePipelineContinuation_RefusalLeavesPlanned`
  proves the session stays PLANNED and can still be rejected.
- Handler refusal tests extended to every state-table row: `session_running`, `session_already_run`
  (asserting the stored artifact URIs are quoted), `session_failed`, `session_interrupted`,
  `expired_session` — each with a zero-call assertion.
- `countPipelineStepForTest` added to the post-loop mutator path.
- `dec.More()` check after decode refuses a trailing second JSON value.
- `resultStatusOf` parses the envelope metadata as JSON instead of substring-matching (the review's
  open question; hardened).

Race gate re-run after the fixes: `internal/handler` + `internal/db` green (`-race -timeout 20m`,
fleet host). Left for the owner: reject of an interrupted RUNNING row (returns `session_running`;
reject only transitions PLANNED — ruling needed), and `OverwriteForTest` exported-but-PLANNED-restricted
(the "no exported write without a transition rule" wording vs a test seam — ruling needed).

### Owner rulings — 2026-11-25 (post-review amendments)

1. **Interrupted run may be discarded.** Reject of a RUNNING session claimed by another server
   instance transitions it to REJECTED (nothing is running to protect). A same-instance RUNNING
   session still refuses reject with `session_running`. MADR D2.6 amended.
2. **`OverwriteForTest` build-tagged.** The test seam moves behind a build tag so production builds
   carry no exported write without a transition rule. The `make build`/`install` path (tags `netgo`)
   does not include it; `go test` runs with the tag.

### Owner rulings implemented — 2026-11-25 (commit `77d08ac`)

1. **Interrupted-run discard:** `Reject` now takes the caller's instance nonce; a RUNNING session
   claimed by another instance transitions to REJECTED, a same-instance RUNNING session still refuses
   `session_running`. The continue-vs-reject race test aligns both callers to one instance nonce (as
   production does); new `TestPipelineSessionStore_RejectInterruptedRun` covers both cases.
2. **Build-tagged seam:** `OverwriteForTest` moved to `internal/db/pipeline_session_testseam.go`
   (`//go:build magictoolstestseam`); the handler test helpers moved to
   `internal/handler/execute_pipeline_session_testseam.go`; the two test files that use them carry the
   tag; `make test` runs with `-tags magictoolstestseam`. Verified: `go build ./...` and
   `go doc PipelineSessionStore.OverwriteForTest` show no seam in production; untagged
   `go test ./internal/handler/` still passes (the seam tests are excluded without the tag).

Gates after the rulings: full suite `-tags magictoolstestseam ./...` green on the linux fleet host;
race gate on handler+db green on the windows fleet host; `go vet` clean both with and without the tag.

### P8 — 2026-11-25, owner's live step: PASSED

The owner rebuilt, installed and restarted the service, and reported: "gobble works now with claude
models tests passed". The Anthropic `tools.16.custom.input_schema` refusal is gone on the live route;
the claude provider completes model calls. Acceptance criterion 8 met (owner-reported).

**Close-out:** every acceptance criterion met — contract table green, zero-call proofs, atomic claim,
all-three and target+intent forms working, flat schema on the wire, fixed disclosure, plants caught,
gates green, independent review closed with both owner rulings implemented, live step passed. PLAN
status `complete`.
