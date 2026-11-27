---
status: implemented
date: 2026-11-27
associated-madr: "0005-MADR-make-execute-pipeline-schema-and-handler-agree.md"
---

# Implement continuation for execute_pipeline and the contract, schema and sanitiser changes that go with it

Associated MADR:
[0005-MADR-make-execute-pipeline-schema-and-handler-agree.md](0005-MADR-make-execute-pipeline-schema-and-handler-agree.md)

## Goal

`execute_pipeline` does what its schema, descriptions, guide and `pipeline-start` prompt say, because all five state
one
contract (the MADR's D2): a dry run stores the plan and returns an issued session id; a later call with that id
continues it or rejects it; a call carrying `reject: true` never runs a stage; the schema is the two typed branches
that
each repeat their properties (S2). The sanitiser stops retyping untyped properties and holds the table in the MADR's
D3.1; stored proxied schemas are refreshed; nested `properties: {}` are kept or removed on the evidence step's
outcome.
Nothing reaches the owner's running server except by the owner's own build, install and restart (P9).

That goal is the goal as approved on 2026-11-17. It is not the remaining work. The current state and the forward path are the next section. Do not execute P3, P4, P5 or P6 of this plan: those phases landed under [0013-PLAN-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md](0013-PLAN-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md). Do not restore the S2 union: [0013-MADR-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md](0013-MADR-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md) superseded D1 for the served schema.

## Current state and forward path (2026-11-26, `34bf269`)

Read against the tree. Not a new decision. The MADR's amendment (b) holds the same facts.

**P3, the session store, is done.** It was built by 0013, not by an execution record of this plan. At `34bf269`:

| This phase required | Where it is |
| :--- | :--- |
| key prefix `pipeline-session:` | `internal/db/pipeline_session.go:28` |
| values compressed as `SaveRawResource` does | `util.Compress` at `:116` |
| Badger TTL of the 24-hour lifetime plus a one-hour grace | `:30-34`, `:125` |
| `expires_at` in the value | `:77`, `:109-111` |
| one transaction that reads the state and writes the next | `UpdateWithRetryCtx` at `:121` and `:224`. This plan said `UpdateWithRetry`. Both exist (`internal/db/badger.go:813`, `:856`). 0013 adopted `UpdateWithRetryCtx`. Not a gap. |
| cap of 50 `PLANNED`, oldest first | `PipelineSessionCap` at `:25`; `evictOverCap` at `:132-171` |
| instance nonce recorded at `RUNNING` and compared on read | `ClaimRunning` at `:232-233`; `GetForInstance` at `:213-214` |
| no exported way to write a state without its transition rule | production methods follow the transitions. `OverwriteForTest` exists only under `//go:build magictoolstestseam` and only for a `PLANNED` record (`internal/db/pipeline_session_testseam.go:1-22`). 0013 recorded that seam. |

P4's argument gate, P5's continue and reject, and the continuation half of P6 are also in the tree, under 0013. The served schema is flat: `internal/handler/inventory.go` `execute_pipeline` entry (`:399-445`) has seven properties, `additionalProperties: false`, `minProperties: 1`, and no union keyword anywhere in that file. `docs/guides/mcp-tools.md:73-84` states the two-stage contract. The dry-run path appends 0005's disclosure (`internal/handler/execute_pipeline_session.go:88-107`). It still also emits the old line "call execute_pipeline again with dry_run=false" (`internal/handler/execute_pipeline.go:144`), because `freshPipelineDryRun` calls `buildDryRunResult` and then appends the disclosure. That leftover sentence is not a P3 defect and is not part of the remaining phase. Noted, not scheduled.

**What this plan still owned at the re-grounding:**

1. **D3, the sanitiser (P1's evidence rule, then P7).** At the re-grounding, `SanitizeToolSchema` still retyped. That is no longer true after the P7 execution below. P1's owner probe still has no recorded result, so the D3.2 default stands: keep nested `properties: {}`.
2. **Amendment (a), the Go-binary lookup.** At the re-grounding it had not landed. It landed in the execution below. The stale names are gone. The glob is sorted newest-first.

**Forward path, as written at the re-grounding.** Do not re-open P3, P4, P5 or P6, and do not restore S2. P9 as written must not be run: its "two branches" check is the superseded schema. A live install stays the owner's.

### P7 execution — 2026-11-26

The owner approved this phase ("approved to proceed"). Tests first, seen red, then the change.

Red: `TestSanitizeToolSchema_SetsDefaultType`, `TestSanitizeToolSchema_Raw`, five rows of `TestSanitizeToolSchema_D31Table`, and `TestSyncSchemaRefresh_RetypingChangeDoesNotSkip`. The failures were the retyping: untyped properties gained `type: object`, a null property became `{type: object}`, a union gained a root type. First failure line: `schema_util_test.go:50: untyped property gained type object; want it left unchanged`.

Change: `internal/util/schema_util.go` implements the D3.1 table. Nested `properties: {}` are kept. `internal/client/sync_schema_refresh_test.go` holds D3.3's hash claim: an old-form stored hash does not skip, and the saved schema is the new form; an unchanged tool still skips. Deviation: `internal/handler/handlers.go`'s `addTool` comment was updated so it no longer describes the old retyping. No guide had a sanitiser note.

Green, push gate, 2026-11-26, after the owner required the make targets and the CI job before anything was called ready to push:

| Gate | Where | Result |
| :--- | :--- | :--- |
| `gofmt -l cmd internal` | session host | empty, exit 0 |
| `go mod tidy` diff | session host | `go.mod` and `go.sum` unchanged; files restored |
| `make check-records` | session host | exit 0 |
| `make markdownlint` | session host | 0 issues |
| `make test` (`go test -v -tags magictoolstestseam ./...`) | `<linux-host>`, linux/amd64 | exit 0, 21 packages `ok`, 0 `FAIL`, 60s |
| `make vet`, then `CGO_ENABLED=0 go test -count=1 -timeout 20m ./...`, then `make lint` with golangci-lint v2.15.0 | `Ubuntu-24.04` via the Windows laptop | exit 0, 21 packages `ok`, lint `0 issues.` |
| `go test -race -count=1 -timeout 20m` of `internal/handler`, `internal/db`, `internal/util`, `internal/client` | session host, darwin/arm64, `--race` | exit 0 |
| `go vet`, `go build`, `CGO_ENABLED=0 go test -count=1 -timeout 20m ./...` | `<windows-host>`, windows/amd64 | exit 0 |
| the same three | session host, darwin/arm64 | exit 0 |
| `sh -n`, `shellcheck`, `scripts/install_test.sh` | session host | 34 passed, 0 failed |
| `scripts/install.ps1 -WhatIf` | windows/amd64 | exit 0; place-binary only; path contains `Programs\magictools` |

That table is the gate of the sanitiser tree, before amendment (a). It does not cover the lookup.

### Amendment (a) execution — 2026-11-26

The owner said to finish the plan. P9 was not run: the plan forbids an agent from building, installing, or restarting the owner's server.

Red: `go test -run TestResolveGoBinary ./internal/handler` failed to compile. First line: `go_binary_test.go:25:11: undefined: resolveGoBinary`. The seam did not exist, so the old function could not be called with a temporary home.

Change: `mcpGoBinary` keeps its name and signature and calls `resolveGoBinary`. `fixedGoFallbacks` is `~/.local/go`, `~/go`, `/usr/local/go`. The SDK glob is sorted by the MADR's N-c rule. `docs/guides/mcp-tools.md` says that without `MCP_GO_BIN_PATH` the newest `go*` directory under the home directory's `sdk` folder is used first. The two no-assertion calls in `execute_pipeline_extra_test.go` were left as they are.

Green, package: `go test -run 'TestResolveGoBinary|TestFixedGoFallbacksOmitStaleNames' ./internal/handler` exit 0.

Plants, each in a scratch copy, then the scratch deleted. The working tree was not the plant.

| Plant | Test that failed | First failure line |
| :--- | :--- | :--- |
| glob taken in ascending path order | `TestResolveGoBinary/newest_wins_when_text_order_is_oldest_first` | `go1.26.11 with go1.27.4: got .../go1.26.11/bin/go, want .../go1.27.4/bin/go` |
| `go1.27rc1` parsed as `1.27.0` | `TestResolveGoBinary/release_candidate_does_not_outrank_an_older_release` | `got .../go1.27rc1/bin/go, want .../go1.26.11/bin/go` |
| untyped property given `type: object` | `TestSanitizeToolSchema_D31Table/untyped_property_stays_untyped` | `got ... "type":"object" ... want` the description-only node |
| root union given `type` and `properties` | `TestSanitizeToolSchema_D31Table/root_union_is_unchanged` | `got` a root with `type` and `properties`, `want` the union alone |

The continuation plants were already recorded by [0013-PLAN-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md](0013-PLAN-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md). They were not repeated. P9 was not run by an agent; the owner confirmed it on 2026-11-27.

### Independent review — 2026-11-27

A new gobble instance, started with `--no-session` and not this session, read `34bf269..f539892` and the MADR's D3 table and amendment (a). It was not given the implementation reasoning. It edited nothing. It did not run the tests.

| Item | Result |
| :--- | :--- |
| 1–6 reject paths, claim atomicity, session id, stored-plan contents, pre-flight, refusals | not in this diff. The reviewer did not open the continuation code. |
| 7 all-three call | not in this diff. Absence of a dispatch change is not a verified pass. |
| 8 served schema | not in this diff. |
| 9 sanitiser table | pass, from `internal/util/schema_util.go` |
| 10 Go-binary lookup | pass, from `internal/handler/go_binary.go`. Windows behaviour was not run. |

Closed: 9 and 10, subject to the tests, which this reviewer did not run. The push gate had already run those tests.

Put to the owner:

1. Items 1–8 are [0013-PLAN-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md](0013-PLAN-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md), not `f539892`. That plan records its own review. This review does not re-verify them.
2. D3.2 is still the default: nested `properties: {}` kept, because no provider probe was run.
3. D3.3's live refresh was not verified against a live store. The test is `internal/client/sync_schema_refresh_test.go`.

The plan status is `implemented`, on the owner's P9 confirmation the same day. This review does not reopen it. It closes items 9 and 10 only.

### P9 execution — 2026-11-27

The owner confirmed P9 complete. No agent ran a build, install, restart, re-sync, or call against the owner's server. The individual live checks in the list above were not reported, so this record does not claim which of them were seen.

The plan's close-out word is `complete`. This repository's record check does not accept that word. The status is `implemented`, which is the checker's word for a plan whose owner has confirmed the live step.

Still named, and not closed by this confirmation: the independent review in P8 step 3 had not been dispatched yet. It was dispatched later the same day; its result is the section above. The continuation plants remain 0013's. A tag or release was not asked for.

### D3.2 probe and D3.3 live read — 2026-11-27

The owner ordered the probe and the live read after the review above. The probe used the login shell's environment keys and printed only route, case, HTTP status, and the error `code`. No header, body, or key was printed. Models are the ones record 0084 already used, except Kilo, which used `anthropic/claude-haiku-5.5`.

Evidence (1), re-checked at go-llmprovider-sdk `223bb8d`: the recorded schema refusals in `testdata/refusals-0061` and `testdata/refusals-0062` are a root `type: string`, not a missing `properties` field. Evidence (2), the upstream comments, was not re-read this pass.

| Route | nested without `properties` | nested with `properties: {}` | root without `properties` |
| :--- | :--- | :--- | :--- |
| grok responses `grok-4.6` | 200 | 200 | 200 |
| openai responses `gpt-6-luna` | 200 | 200 | 200 |
| claude messages `claude-sonnet-5` | 200 | 200 | 200 |
| opencode zen responses `gpt-5.5` | 200 | 200 | 200 |
| gemini interactions `gemini-3.8-flash` | 200 | 200 | 200 |
| kilo chat `anthropic/claude-haiku-5.5` | 401 `INVALID_TOKEN` | 401 `INVALID_TOKEN` | 401 `INVALID_TOKEN` |

Together and Hugging Face had keys set and were not sent. OpenCode's other routes were not sent. Kilo's 401 is an auth failure, not a schema refusal and not an acceptance. Case (i) was therefore not accepted on every route the owner has a key for. D3.2's rule does not remove nested `properties: {}`. They stay.

The live read did not start a binary and did not call `sync_servers`. It called `list_tools` on the already running service at `127.0.0.1:48080`, `max_tools` 49. Result: 29 tool headings, 0 old-form properties (type object, empty `properties`, and a description, and nothing else), 0 empty `properties: {}` on an object. A filesystem copy of the live database was torn and is not the evidence; after truncating that copy's memtable only, it agreed: 47 tools, 29 proxied, the same two zeros, newest `LastSyncedAt` `2026-11-27T14:00:40Z`, which is after `f539892` (`2026-11-26T22:17:58-06:00`). This shows the current store does not hold the old fingerprint. It does not show a before-and-after sync.

## Approval

On 2026-11-17 the owner approved the whole plan "now, through the independent review" (the MADR's Decided 14). As
put: the
pair is committed and every code phase runs, each committed and pushed to `main` on this approval, stopping before the
owner's build, install and live checks. That is the owner's approval of this plan through P8, and the authority for
commits and pushes to this repository's `main` for **this plan only**. P9 (build, install, restart, re-sync, the live
checks) is the owner's alone. Tags and releases need a separate word. The plan is written for the choices the owner
made (the MADR's Decided 1 to 13); a deviation is recorded in this plan and, where it changes a decision or a fact, in
the MADR, before the phase continues.

## Scope

**In scope:**

* `internal/handler/execute_pipeline.go` and a new file for the continuation (argument gate, strict decode, generated
  ids, the dry-run store, continue, reject, refusals);
* a session store: a new file in `internal/db` (own key prefix, atomic transitions, expiry, cap, interrupted-run
  rule);
* `internal/handler/inventory.go` (the `execute_pipeline` entry: S2 schema, descriptions), `prompts.go`,
  `docs/guides/mcp-tools.md`;
* `internal/util/schema_util.go` and its tests (D3.1), and the sync path's tests for the stored-schema refresh (D3.3);
* tests in `internal/handler`, `internal/db`, `internal/util` and `internal/client`.

**Out of scope:** a union from a downstream server (passed through, D3.4); `autoPromoteRequiredFields`; the dry-run's
unclosed telemetry pipeline (noted in the MADR); the stdio bridge; `get_internal_logs`' inventory drift; any release,
tag or push; any build, install, restart, reload or configuration of the owner's server; gobble-cli and its records.

## Conventions

* **K1. Start state.** Each phase records `git status --short`, `HEAD`, the branch, and `go version`. The one expected
  uncommitted file at drafting, `internal/forks/hnsw/go.mod`, is the owner's: never staged, reverted or committed by
  this plan. Any other difference is a deviation.
* **K2. Tests first, seen failing** (P2), then the change, then green. Tests are committed with the phase that turns
  them green, never red.
* **K3. A planted break per assertion** (P8), in a scratch copy (`git archive HEAD` plus the phase's diff), never in
  the
  working tree; each plant is `gofmt` clean; the failure's first lines are kept.
* **K4. The docs step is in the same commit as the code** it describes: descriptions, prompt, guide, code comments.
* **K5. Heavy runs.** A full-module `go test ./...`, any `-race` run and `make lint` over the whole module go through
  `~/.agents/skills/available-dev-hosts/scripts/pick-host`, then
  `~/.agents/skills/available-dev-hosts/scripts/run-on-host`
  on the host it prints (`--race` for a race run), one at a time per host; on the session host,
  `~/.agents/skills/available-dev-hosts/scripts/run-heavy -- <command>`. A race run carries an explicit `-timeout`. A
  single-package `go test` may stay on the session host.
* **K6. Evidence.** Full output to a scratch file, the exit status taken before any filter.
* **K7. Deviations stop the phase,** are recorded here with a date, and in the MADR when a decision or a fact changes.
* **K8. Commits and pushes.** The coordinator commits each phase with `git commit --no-edit`, after the phase's checks
  pass and after the disclosure guard has run on what is staged, and pushes to `main`: the owner's approval above is
  the authority, for this plan only. The global hook owns the message. The agent doing a phase stages only that
  phase's files by path and never `internal/forks/hnsw/go.mod`. No tag and no release without a separate explicit
  word. `gofmt` and, where installed, `make lint` on staged Go.
* **K9. No real credential reaches any agent.** The evidence probe (P1) is run by the owner.
* **K10. Identifiers.** No hostname, account name or real-machine path in a committed file.

## Implementation Steps

Prices: model class (lightweight, mainstream, strongest) and effort (low, medium, high), cheapest first. The strongest
tier is warranted where judgment dominates and an error is expensive: the session store (P3), the continuation handler
(P5) and the independent review (P8).

### P0 — place the records

Lightweight, low.

1. The owner's answers are recorded: the MADR is `accepted`, with the rulings in its Decided list, and this plan is
   `in-progress` (done 2026-11-17).
2. The coordinator commits the two records (`docs/0005-MADR-…`, `docs/0005-PLAN-…`) on their own, and pushes.

Checks: `markdownlint-cli2` on both files (the repository's `.markdownlint.json` wants dash markers; the MADR format
and the existing records use asterisks, so both carry an `MD004` disable); the citation check; a disclosure scan.

### P1 — establish the evidence (no repository change)

Mainstream, low; the probe is the owner's.

1. **Is a provider known to refuse an object schema without `properties`? (MADR D3.2.)** Evidence that counts, in
   order: (1) a recorded refusal in go-llmprovider-sdk's fixtures (`git grep` of `testdata/refusals-*` and its
   reports at
   the commit current when the step runs; at `b109cea` none: the recorded tool-schema refusals are for a root
   `{"type":"string"}`); (2) an upstream agent's test or comment naming the provider and the refusal (Codex and gobble
   name "OpenAI models" and "some providers" for the root only; nothing for nested); (3) an owner-run live probe,
   below.
2. **The probe** (the plan supplies a script in the scratch folder; the owner runs it in the owner's shell with the
   owner's own keys; an agent never holds or uses a key). For each route the owner uses (at least Grok, and the OpenAI
   Responses route if used), it sends one minimal request per case: (i) a tool whose parameters are a root object with
   `properties` containing one nested object-typed property **without** `properties`; (ii) the same with
   `properties: {}` on it; (iii) a root object without `properties`. It prints **only** the route, the case, the HTTP
   status and the error `code` field, never a header or a body. The owner pastes the table back. The code phases do
   not wait for it: only P7's rule for the nested `properties: {}`
   depends on it, and that rule's default, with no probe or with mixed results, is to keep them.
3. **The rule applied to the outcome.** Remove the nested `properties: {}` (case i accepted) only if (1) and (2)
  show no
   refusal **and** case (i) is accepted on every probed route. Otherwise keep them. **Stop condition: mixed evidence**
   (one route refuses, or a route's status is not clearly success or refusal, or the probe is not run) means keep,
   record
   the per-route result in the MADR's D3.2 and in this plan's execution record, and do not remove. The retyping fix
   (P7) does not wait for this step.
4. **How stored proxied schemas are refreshed** (MADR D3.3), re-confirmed from the code before P7: the sync's skip
  hash
   is over the sanitised schema's hash, so a changed sanitiser changes the hash of exactly the tools it changes.

Exit: the evidence table and the applied rule recorded; the MADR's D3.2 amended if the outcome is removal.

### P2 — tests first (red)

Mainstream, medium. Files: `internal/handler/execute_pipeline_contract_test.go`,
`internal/handler/tools_list_test.go`,
`internal/handler/pipeline_session_store_test.go`, `internal/db/pipeline_session_test.go`,
`internal/util/schema_util_test.go`, `internal/client/sync_schema_refresh_test.go`.

1. **The emitted `tools/list` helper**: registers a handler as `main.go` does (`Register`, `RegisterPipelineTools`,
   `RegisterPrompts`), connects it over the SDK's line-delimited transport on in-process pipes, returns the raw
   response bytes and the decoded tools (the drafting experiment R1 is the starting point).
2. **The one table** `executePipelineContract` (the MADR's 26-row table is its start): each row is an argument set and
   the expected gate outcome (fresh run, fresh dry run, continue, reject, refuse with a reason). Two assertions per
   row,
   driven from the table: the emitted schema (compiled with the validator already in `go.mod`, and a second validator
   as a cross-check where one is already in the module graph) accepts the row exactly when the outcome is not a
   refusal;
   the handler's gate returns the table's outcome.
3. **No call carrying `reject: true` ever executes a stage.** The executor the handler uses is injected; the stub
  counts
   `executeDAGStep`, `executeMutatorAST` and `ExecuteProxy` calls. Every `reject: true` row of the table, a
   rejection of
   a stored session, a repeated rejection, a rejection of an unknown id: zero calls. A dry run: zero calls.
4. **The session store**: create, get, atomic `PLANNED` to `RUNNING` and to `REJECTED`; 32 concurrent continuations of
   one id over 200 rounds (one winner each); a continuation racing a reject over 200 rounds (never both, never
   neither); a repeated continuation refused with the prior state; expiry (`expires_at` past, entry still stored:
   `expired_session`); unknown id; the cap (the 51st `PLANNED` evicts the oldest); a `RUNNING` session with another
   server instance nonce is `session_interrupted` and never run; the key prefix is not `raw:` and a `raw:` read of a
   session id returns nothing.
5. **The continuation**: a stored plan with a missing tool is refused before any stage (`plan_stale`, zero stages run,
   the stage named); a stored `plan_hash` mismatch is refused; a plan containing a MUTATOR-role or `apply_vetted_edit`
   stage is shown but not stored; every refusal in the MADR has its own test and message; a repeated continuation
   never re-runs; the all-three call (with and without `dry_run: true`) still works and its `session_id` is a label
   on a dry run; `target` + `intent` runs and returns a generated id.
6. **The schema**: no root union other than this tool's two typed branches; each branch is a typed object with its own
   `properties`, `required` and `additionalProperties: false`; the emitted bytes hold no empty object except the root
   `properties` of the no-argument tools (and, after P1, the nested ones if kept), no `{}` branch, no bytes after the
   envelope.
7. **The sanitiser table** (MADR D3.1), one test per row: an untyped property stays untyped and gains no `properties`;
   `null` becomes `{}`; a nested object property with no `properties` per P1's outcome; `items: {}`, a union at the
   root and nested, `$ref`, `enum` pass through byte for byte; the root gains `type` and `properties` as before. The
   old test that pinned the retyping (`schema_util_test.go:27-48`) is rewritten to the correct behaviour, and the
   reason is recorded in the test and the MADR (the owner's ruling: Decided 6).
8. **The stored-schema refresh**: a stored record and schema as the old sanitiser left them (a fixture built from the
   old function's output for a tool with an untyped property); a sync of the same downstream tool computes a different
   composite hash, `shouldSkipUnchangedSync` is false, and the saved schema is the new one; a downstream tool the new
   sanitiser leaves unchanged keeps its hash and is skipped.
9. **The dry-run disclosure** (the MADR's Decided 9 and its fixed words): the dry-run result holds the exact block of
   the MADR's D2, with the issued id and the expiry filled in, after the plan; a result for a plan that is not
   stored says that it is not stored and why; no other call's result holds the block.
10. **The docs pin**: the guide, the prompt and the schema descriptions agree with the decided contract (the same
   terms; no stale "paused plan" wording).

Run every new test against the unchanged tree and keep the failures. Exit: each red for the stated reason; the
existing
suite unchanged; no source change.

### P3 — the session store

**Not the next step. Implemented by 0013, not by an execution of this plan.** Assessed at `34bf269` on 2026-11-26: the store in `internal/db/pipeline_session.go` matches this phase's contract. Do not build it again. The original step is kept below so the approval still has a text. The comparison is in the current-state section.

Strongest tier, high. Files: `internal/db/pipeline_session.go` (and its constants).

A store type with its own key prefix (`pipeline-session:`), values compressed as `SaveRawResource` does, a `Badger`
TTL of the lifetime plus a one-hour grace, `expires_at` in the value, transitions as one `UpdateWithRetry`
transaction that reads the state and writes the next (the drafted prototype R9 is the shape), a cap of 50 `PLANNED`
with oldest-first eviction, a server-instance nonce recorded at `RUNNING` and compared on read, and no exported way to
write a state without its transition rule.

Gate: P2's store tests green with `-race -timeout 10m` for the `internal/db` package; `go vet`; `gofmt`. Commit
(K8): the store and its tests.

### P4 — the argument gate and the fresh-mode fixes

**Not the next step. Implemented by 0013** (`internal/handler/execute_pipeline_gate.go`, `handleExecutePipeline` at `internal/handler/execute_pipeline.go:35-62`). The original step is kept below. The Go-binary lookup in amendment (a), which this plan later placed beside P4, did **not** land. That lookup is still open. See the current-state section.

Mainstream, medium. Files: `execute_pipeline.go`.

Strict decode of the arguments (unknown key, wrong type: refused with `bad_arguments`); the decision order of the
MADR (`reject` first, no executor in reach); `session_id` generated when a fresh call omits it; refusals with their
own
messages; the argument gate as a pure function the table test calls.

Gate: P2's contract-table handler assertions, the `reject` zero-call tests and the strictness tests green; the
existing handler test (the all-three dry run) unchanged and green. Commit and push (K8).

### P5 — dry-run store, continue, reject

**Not the next step. Implemented by 0013** in `internal/handler/execute_pipeline_session.go`. The original step is kept below.

Strongest tier, high. Files: a new `internal/handler/execute_pipeline_session.go`, `execute_pipeline.go`.

The dry run stores the exact plan (stages, warnings, the recall context capped at the existing context ceiling,
target, intent, plan hash, roles) through P3 and returns the issued id, the expiry and the disclosure block of the
MADR's D2, word for word, which says that approval covers the analysis stages, that
mutating stages are added at run time after the reporting stage, and that with no `plan_hash` mutation proceeds under
the placeholder approval `auto-approved-orchestrator-override`. The continuation: claim, plan-hash check, pre-flight
over every stage, then the existing run loop
with the stored stages (so the mutator gate, the git checkpoint and the rollback are unchanged), then `COMPLETED` or
`FAILED`. The rejection: the transition and nothing else, in a function that is given no proxy service. A stage list
holding a mutating stage is not stored.

Gate: P2's continuation, claim-race, expiry, interrupted-run and reject zero-call tests green; the whole
`internal/handler` package green with `-race -timeout 20m`. Commit and push (K8).

### P6 — the schema, the descriptions, the prompt, the guide

**Not the next step, and not this plan's S2.** 0013 served a flat schema and superseded D1. `internal/handler/inventory.go` has no `anyOf`, `oneOf` or `allOf` at `34bf269`. Restoring two typed branches would contradict 0013. The original step is kept below as the text that was approved, not as work to do.

Mainstream, medium. Files: `inventory.go`, `prompts.go`, `docs/guides/mcp-tools.md`, the dry-run result text.

The `execute_pipeline` entry becomes the S2 schema of the MADR (root with all seven properties and descriptions,
`additionalProperties: false`, `anyOf` of the two typed branches, descriptions not repeated); the `session_id` and
`reject` descriptions and the tool description state the decided contract; the prompt's steps 4 and 5 and the guide's
row and "two-stage" paragraph say the same (dry run, review, continue or reject with the issued id; the mutation
notice).
The inventory-size test (18) is unchanged.

Gate: P2's schema, table and docs-pin tests green. Commit and push (K8): schema, prompt and guide together.

### P7 — the sanitiser, and the stored-schema refresh

Mainstream, medium. Files: `schema_util.go`, its tests.

Implement the MADR's D3.1 table: no retyping; `null` to `{}`; nested `properties: {}` as P1's rule decided (kept,
documented in a code comment with the evidence, or removed); everything else byte for byte. Docs step: the comment
states why each row is what it is and cites the evidence; the guide's note on schema sanitising, if one exists.

Gate: P2's sanitiser and refresh tests green; the packages `internal/util`, `internal/client`, `internal/handler`
green.
Commit and push (K8).

### P8 — planted breaks, the repository's gates, the independent review

Plants and gates: mainstream, medium. The review: strongest tier, high.

**Step 1. A planted break per assertion** (K3), each in its own scratch copy, each seen to fail the named test:

| Plant | Test that must fail |
| :--- | :--- |
| the gate runs the fresh path when `reject: true` comes with `target` and `intent` | the table's `reject` rows and the zero-call test |
| the rejection function is handed the proxy service and calls it | the zero-call test |
| the dry run calls the execution loop | the dry-run zero-call test |
| the claim is a read then a write | the 32-way claim test |
| the continuation re-plans instead of using the stored stages | the stored-plan test |
| the pre-flight is removed | the `plan_stale` test |
| expiry is not checked | the expiry test |
| the cap is not enforced | the cap test |
| a `RUNNING` session from another instance is run | the interrupted-run test |
| the key prefix becomes `raw:` | the prefix test |
| an unknown argument is ignored | the strictness row |
| the schema's second branch loses `additionalProperties: false` | the agreement table |
| a `{}` is appended as a third union branch | the empties test (an accepting branch defeats a union) |
| the sanitiser retypes an untyped property again | the sanitiser table |
| the sanitiser stops passing a union through unchanged | the sanitiser table |
| the sync's hash is taken over the raw schema | the stored-schema refresh test |
| "paused plan" wording returns to the guide | the docs pin |

**Step 2. The repository's gates**, as `.github/workflows/ci.yml` runs them: `gofmt -l cmd internal` empty; `go mod
tidy`
   leaves `go.mod` and `go.sum` unchanged; `go vet ./...`; `CGO_ENABLED=0 go test ./...` (heavy, K5); `make lint`
   (golangci-lint, `.golangci.yml`); and `go test -race -timeout 20m ./internal/handler ./internal/db ./internal/util
   ./internal/client` through `run-on-host --race`. The macOS and Windows native jobs run on a push and are the
   owner's
   to read; if a push is asked for, wait for them.
**Step 3. Independent review before anything reaches the owner's running server.** A fresh reviewer who has not seen
this
   plan's reasoning reads the diff for: every path on which a stage could run from a `reject: true` call; the claim's
   atomicity; the session id's source; what a stored plan holds and whether any of it is secret; the pre-flight; the
   refusals; that the all-three call is unchanged; the schema and the sanitiser table. Findings are closed or put to
   the owner.

Exit: every plant failed as named; every gate green with output kept; the review's findings closed.

### P9 — the owner's live step and close-out

**Done, by the owner's confirmation on 2026-11-27 ("p9 complete").** The steps below were not observed by an agent. The confirmation is the close-out this phase names. The checklist results were not pasted back, so they are not invented here.

Mainstream, low (the owner's steps; the record update is the agent's). **P9 is the owner's alone**, outside the
approval above. Nothing here is run by an agent: no build, no install, no restart, no re-sync, no call to the owner's
server; a tag or release is a separate word.

The owner, when the owner chooses:

1. **Keep the current binary** (`make install` keeps none; it moves the installed binary aside and then removes it):
   `cp "$HOME/.local/bin/mcp-server-magictools" "$HOME/.local/bin/mcp-server-magictools.before-0005"`.
2. **Build and install** from the repository at the commit the owner chooses: `make build`, then `make install`
   (it copies `dist/mcp-server-magictools-<os>-<arch>` to `$HOME/.local/bin/mcp-server-magictools`); check
   `mcp-server-magictools --version`.
3. **Restart** the server: quit and reopen Claude Code (a stdio client starts the new binary), start a new gobble
   session; for an installed service, `mcp-server-magictools service restart` (or `service reinstall` after a binary
   update).
4. **Refresh stored proxied schemas.** The boot syncs every configured server, so the restart does it; to force it,
   call the `sync_servers` tool from the client. Both need the downstream servers to start, which is why it is the
   owner's step.
5. **Check from Claude Code**, on a scratch directory holding a small Go module, not a real project:
    * `/mcp` lists `execute_pipeline` and its schema has two branches. **Stale as of 2026-11-26.** The served schema is flat (0013). Do not treat a missing union as a failure of this plan.
   * `execute_pipeline` with `target`, `intent`, `dry_run: true`: a plan, an issued `session_id`, an expiry, and the
     notice that mutation is decided at run time.
   * the same `session_id` with `reject: true`: status `REJECTED`, no steps; repeating it returns the same; continuing
     that id returns `session_rejected`; an invented id returns `unknown_session`.
   * `reject: true` with `target` and `intent`: refused, nothing runs.
   * a second dry run, then a continuation with its `session_id` alone: the analysis runs; a second continuation of
     the
     same id returns `session_already_run`.
   * the all-three call still works.
   * `align_tools` for a proxied tool known to have an untyped property: the property no longer shows `type: object`.
6. **Check from gobble with a Grok model** and this server added: a prompt completes (the 2026-11-16 failure does not
   recur) and `execute_pipeline` can be called. If Grok refuses the S2 form, stop: that is a deviation to put to the
   owner, with the refusal text.
7. **Roll back, if needed.** Stop the client or service; `cp "$HOME/.local/bin/mcp-server-magictools.before-0005"
   "$HOME/.local/bin/mcp-server-magictools"`; restart. Stored `pipeline-session:` entries then sit unused until their
   TTL (older binaries are not known to read them: not verified); stored proxied schemas stay in the new form until
   the
   old sanitiser's next hash change would re-add the retyping; a re-sync with the old binary restores the old form
   exactly. A source rollback is `git revert` of the phase commits, on the owner's word, then a rebuild.

On the owner's confirmation, this plan's status becomes `complete` with an execution record: what each phase did, the
output of each gate, every plant's failure, the P1 evidence and the rule's outcome, every deviation, and what was
not done and why. A tag or release is the owner's separate word.

## Verification

* P2's tests: red before, green after, and red on each P8 plant.
* The gates of P8 with their output kept.
* A citation check over both records (every `path:line` resolves in the commit the record names, and each cited range
  says what the record claims), seen to fail on a planted false citation in a temporary copy.
* `markdownlint-cli2` on both records; the repository has no record-numbering or link tooling, so the number was taken
  by a whole-repository scan for `NNNN-{MADR,PLAN,REPORT,GATES}-*.md`.
* A disclosure scan of both records against the owner's deny list.

### Acceptance criteria

1. The table: for every argument combination the emitted schema and the handler agree, asserted from one table.
2. No call carrying `reject: true` executes a stage; a dry run executes none; asserted by call counts.
3. A session is claimed once; a repeated continuation never re-runs; every refusal is distinct.
4. The all-three call (with and without `dry_run`) still works; `target` + `intent` works.
5. The emitted `tools/list` holds no empty object other than the allowed paths, no `{}` branch, no trailing bytes.
6. The sanitiser table holds; a stored old-form schema is replaced by a sync.
7. Every plant failed its test; every gate green; the review closed.
8. The owner's live step passes, from Claude Code and from gobble with a Grok model.

## Rollout and Rollback

**Rollout.** Source, tests and docs in phase commits pushed to `main` on the owner's approval (P3 to P7), plus the
records (P0). No
migration; the new store keys are additive; stored proxied schemas refresh on a sync (P9). Nothing reaches the owner's
running server until the owner builds, installs and restarts it.

**Compatibility.** The all-three call keeps working. `target` + `intent` starts to work. A call with `session_id`
alone starts to do what the schema says. A call with `reject` that used to run the pipeline is refused. A stray or
mistyped argument that was ignored is refused (Decided 12). The owner is the only caller; the prompt and the guide are
corrected in the same commits as the code.

**Rollback.** P9 step 7. The previous binary is the owner's copy from P9 step 1, because `make install` keeps none.

## Amendment 2026-11-17 (a): the Go toolchain lookup (the MADR's Decided 15)

Nothing above is rewritten or removed.

**Which phase carries it: P4, as its first step and its own commit.** The function is at the end of
`internal/handler/execute_pipeline.go` (`:895-918`). P4 changes the top of that file (the argument gate, `:35-57`), P5
changes the dry-run path (`freshPipeline`, `:61-86`) and adds a new file, and P6 to P8 do not change it. The hunks are
disjoint, but landing this one first, before P5's large change to the same file, means P5 is written against the final
text and cannot conflict with it. It is also independent of every other P4 change, so it is committed on its own.
(P7 would conflict least by file, since it does not touch `execute_pipeline.go`, but it would add a handler change to
a sanitiser phase, and it would land after P5.) Price: mainstream, low.

* **Tests first (P2, then red):** a new `internal/handler/go_binary_test.go` against the seam
  `resolveGoBinary(override, home string, fallbacks []string) string`, with a temporary home holding fake executables
  (skipped on Windows, where the lookup does not apply; see the MADR's amendment). Cases: the pairs that sort wrongly
  as text, `go1.26.11` with `go1.27.4`, `go1.26.1` with `go1.27.4` and `go1.1.3` with `go1.10.5` (each must pick the newest;
  today each picks the older, R10); the pairs that sort rightly by luck, `go1.26.9` with `go1.26.11` and `go1.9.0` with
  `go1.27.4`; a release beside a release candidate (the release wins); names that do not parse (`gotip`, `go-old`,
  `go1.27rc1`) alone (they are chosen, in descending text order) and beside a parsable one (the parsable one wins);
  `go1.27` against `go1.27.0` (tie, ascending path); a newest directory whose `bin/go` is not executable (the next
  newest wins); no SDK at all (the first executable fallback, then `go`); the override set (it wins, unchecked); the
  two stale names are absent from the fallbacks `mcpGoBinary()` passes. Seen red against the unchanged tree.
* **The change (P4.1):** `mcpGoBinary()` keeps its name and signature and calls
  `resolveGoBinary(os.Getenv("MCP_GO_BIN_PATH"),
  homeDirOrDot(), fixedGoFallbacks)`, where `fixedGoFallbacks` is the three fixed paths of the MADR's table in their
  order; the glob result is parsed and sorted by the rule in the MADR; nothing exported changes.
* **Docs, same commit:** the function's comment ("Priority: …") states the real order and the rule; one sentence is
  added to `docs/guides/mcp-tools.md` after the one that names `MCP_GO_BIN_PATH` (`:82-83`), saying that without it the
  newest `go*` directory under the home directory's `sdk` folder is used before the other fixed locations and `PATH`.
  The existing two no-assertion calls in `execute_pipeline_extra_test.go` are left as they are.
* **Planted break (P8):** restore the text ordering of the glob (return its matches in `filepath.Glob`'s order): the
  `go1.26.11` with `go1.27.4` case fails; and make the parse accept `go1.27rc1` as `1.27.0`: the release-candidate case
  fails.
* **Gate:** the new tests green; `go vet`; `gofmt`; the package green. Commit and push (K8).

## Sequencing note 2026-11-17 (the coordinator's, not the owner's)

This repository now has two other records ahead of this plan's code phases, each touching different files:

* a Go-pins record (`docs/0006-…`): the Go pins, a dependency bump, the CI lint tool moved to golangci-lint v2.15.0,
  one fix in `internal/handler/pipeline_tools.go`, and the vendored fork's `go` line;
* by the owner's ruling "A fuller redesign of the middleware under its own record", a redesign of `ListToolsMiddleware`
  in `internal/handler/list_tools_handler.go`, a file the MADR cites as evidence (E11) and this plan does not change.

This plan's code phases start after both have landed, so that each plan's gates see a tree no other plan is changing.
**P2 begins by re-deriving every `path:line` the MADR and this plan rely on** (the citation script kept in the scratch
folder, run against the tree as it then is), correcting any that moved by amendment, and every lint run in this plan uses
golangci-lint v2.15.0, as CI will. A note for the other record: the 0006 record's pin sweep names
`execute_pipeline.go` in its allowlist for the two stale Go names; after P4.1 removes them that allowlist entry no
longer matters, and the coordinator may tell that record's author.

**What this plan needs the middleware redesign to preserve** (found by reading `list_tools_handler.go` and what the MADR
and tests rely on; the redesign is another record's, so this is a request, not a decision):

1. **The inventory's schema reaches the wire unchanged in the base layer.** The registered tools come from the SDK's
   own list (`addTool` sanitises each once, `handlers.go:219`); the redesign must not rewrite unions, `properties` or
   branches of those tools: this plan's S2 schema and the emitted-bytes tests depend on it.
2. **The fallback path emits the same schema.** When the base list fails the middleware builds tools straight from the
   inventory without sanitising (`list_tools_handler.go:55-68`); the redesign must keep that output identical to the
   base layer's for `execute_pipeline`, or this plan's `tools/list` test must cover both.
3. **Proxied tools are sanitised exactly once, by `util.SanitizeToolSchema`, before they are listed.** Today that
   happens when `align_tools` caches the tool (`align_tools_helpers.go:217`), and the middleware appends the cached
   tools as they are (`list_tools_handler.go:82-95`). Wherever the redesign moves it, every proxied tool on the wire must
   carry the sanitiser's output, and nothing else may add, retype or remove anything: this plan's sanitiser table
   (the MADR's D3.1) and the D3.3 refresh test are held against that output.
4. **A union or an empty `{}` that arrives from a downstream server is passed through byte for byte** (the MADR's
   D3.4); the redesign must not normalise them.
5. **The `tools/list` emission stays drivable in process.** This plan's test helper registers a handler as `main.go`
   does (`Register`, `RegisterPipelineTools`, `RegisterPrompts`) and reads `tools/list` over the SDK's line-delimited
   transport; if the redesign changes those entry points or the session-cache key the dynamic layer looks up (today an
   empty string on stdio against the `active-session` the initialise handler stores, `main.go:398`), this plan's helper
   follows it in P2.
6. **No change to what the middleware does for `tools/call`** that this plan relies on: the continuation is an ordinary
   `execute_pipeline` call and is not namespaced.
