---
status: accepted
date: 2026-11-25
decision-makers: repository owner
consulted: "the owner's live failure of 2026-11-25, captured in gobble's session log and relayed by gobble itself;
0005-MADR-make-execute-pipeline-schema-and-handler-agree.md and its PLAN (its D2 continuation contract, owner-ruled
2026-11-17, never implemented); gobble-cli's 0060-MADR-normalise-root-level-unions-in-mcp-tool-schemas.md (draft);
go-llmprovider-sdk's toolschema refusal classification (0084); this repository at 184370e"
informed: "gobble-cli (its 0060 record names this schema); the cross-repository coordinator"
---

# Flatten execute_pipeline's schema for Anthropic's Messages API and build the continuation it advertises

## Context and Problem Statement

On 2026-11-25 the owner ran gobble with `defaultProvider: claude` (model `claude-sonnet-5-5`) and this server among
its MCP servers. Every Claude model call failed before the first token. Gobble's log recorded the refusal:

```text
provider=claude model=claude-sonnet-5-5 outcome=error status=400 code=invalid_request_error
```

and the session transcript holds the body:

```text
invalid_request_error: tools.16.custom.input_schema:
input_schema does not support oneOf, allOf, or anyOf at the top level
```

`tools.16` is `execute_pipeline`. The credential was valid and the model id was listed; the request died on the
schema alone. Grok and OpenCode routes accepted the same request, which is why only the claude provider failed.

The schema at `184370e` carries a root `anyOf` of two branches (`internal/handler/inventory.go:323-335`).
[0005-MADR-make-execute-pipeline-schema-and-handler-agree.md](0005-MADR-make-execute-pipeline-schema-and-handler-agree.md)
decided, on 2026-11-17, to replace that union with a typed-branch form (its D1, option S2) **and** to build the
continuation feature the union advertises (its D2, option H2, every sub-choice owner-ruled). **Neither landed.**
The branches in the inventory are still untyped `{"required": [...]}` objects — the pre-fix state 0005's evidence
row E1 recorded — and the handler still refuses what the schema, the guide and the prompt all advertise. The
0005 PLAN's status is `in-progress` and no execution record exists for its code phases P3–P7. The owner's
recollection that continuation was "already implemented, and strictly enforced" is what the guide and prompt say;
the code does not agree.

This record decides two things, as one unit of work:

1. **The wire form**: replace the root `anyOf` with a flat all-optional object. Anthropic's Messages API refuses
   any top-level `anyOf`/`oneOf`/`allOf` in a tool's `input_schema`, typed branches or not — the owner's failing
   run is the measurement. 0005's own client table flagged exactly this ("the Anthropic claim, if true, means a
   second form (no union) would be needed for that route, which is not in this record's scope and is not
   verified"). The claim is now verified. This supersedes 0005's D1 (S2) for the served schema; S2 was never
   built, so nothing typed is being torn down.
2. **The continuation**: implement 0005's D2 contract — dry-run storage, `session_id`-only continuation,
   `reject: true`, the atomic claim, the refusal vocabulary — unchanged in substance. The owner confirmed on
   2026-11-25: "I think we should implement session_id… I thought it was already implemented, and strictly
   enforced." The design is 0005's; this record adopts it and supplies the flat schema it needs.

### Evidence and defect inventory (at `184370e`, re-grounded from 0005's E1–E31)

| ID | Severity | Observed implementation and consequence |
| --- | --- | --- |
| N1 | High | `execute_pipeline`'s `inputSchema` ends with a root `anyOf` of two untyped branches (`internal/handler/inventory.go:323-335`). Anthropic's Messages API refuses it (owner's run, 2026-11-25, `tools.16.custom.input_schema`). Every Claude call from gobble fails on this tool before any model output. |
| N2 | High | The handler requires all three of `target`, `intent`, `session_id` (`internal/handler/execute_pipeline.go:49-57`); the args struct has no `Reject` field (`:41-47`). A `session_id`-only call is refused; `reject: true` is structurally unread. The guide (`docs/guides/mcp-tools.md:64,73-77`) and the `pipeline-start` prompt (`internal/handler/prompts.go:39-40`) both instruct continuation and rejection — instructions the handler cannot fulfil. |
| N3 | High | 0005's D1 typed-branch fix never landed: the branches carry `required` only, no `type`, no `properties` (N1). 0005's D2 continuation never landed: `PipelineSession` (`internal/handler/pipeline_session.go:12-26`) is a telemetry struct with no persistence, no expiry, no claim state; no `pipeline-session` key prefix exists anywhere in `internal/db`. |
| N4 | Medium | `unmarshalArgsOrWarn` (`internal/handler/io_helpers.go:37-49`) logs and swallows decode errors; unknown keys and wrong types are ignored, not refused (0005's G-a is unmet). |
| N5 | Medium | `util.GenerateSessionID` (`internal/util/strings.go:12-16`) is 64 bits (8 random bytes). 0005's I-a requires a server-issued id of 128 random bits; the existing generator is too weak for a capability id. |
| N6 | Medium | The dry run (`internal/handler/execute_pipeline.go:79-81`) returns the plan and says "call execute_pipeline again with dry_run=false" (`:136`); it stores nothing and issues no id. `freshInitDAGTelemetry` runs before the dry-run return (`:78`) and is never closed on that path — a small telemetry leak, recorded by 0005, left as-is here. |
| N7 | Low | The store's `raw:` keys are readable by any client through the `mcp://magictools/raw/{id}` resource (`internal/handler/resource_handlers.go:13-18`); session state must not use that prefix (0005's L-a). |

### What exists to build on (verified at `184370e`)

* The store: BadgerDB on disk, `SyncWrites(false)` (`internal/db/badger.go:495`); per-entry TTL via
  `SaveWithTTL` (`:2494-2500`); conflict-retrying transactions via `UpdateWithRetryCtx` (`:813-841`, five
  retries, exponential backoff, `ctx` cancellation honoured); one process opens it (a lock).
* The execution loop: `freshPipelineExecute` (`internal/handler/execute_pipeline_run_helpers.go:316-337`) runs
  stages through `freshRunPipelineLoop` (`:26-45`); MUTATOR stages are injected at run time from the Socratic
  verdict (`:141-160`), `apply_vetted_edit` goes through `freshRunMutatorASTStep` with the git checkpoint and
  rollback (`:184-243`); the plan-hash fallback is `auto-approved-orchestrator-override`
  (`internal/handler/constants.go:113`).
* Stage resolution at run time: `buildSchemaAwareArgs` (`internal/handler/execute_pipeline.go:289+`) reads the
  store's current schema per tool; a tool removed by a sync fails at its stage, after earlier stages have run
  (0005's "what exists" item 5). A pre-flight over `store.GetTool` is therefore possible and needed.
* Artifacts: `savePipelineArtifacts` (`internal/handler/execute_pipeline.go:982-1050`) persists plan, audit and
  summary under `raw:` keys named `pipeline_<sessionID>_*`, returned as URIs in the result envelope — the
  "stored artifact URIs" a repeated continuation must quote.
* The context ceiling: `maxContextBytes = 128 * 1024` (`internal/handler/execute_pipeline.go:1089`), enforced by
  `enforceContextCap` (`:1097`) — the cap the stored recall context must respect.
* Both validators for the contract table are in `go.mod`: `github.com/santhosh-tekuri/jsonschema/v5 v5.3.1`
  (direct) and `github.com/google/jsonschema-go v0.4.3` (indirect).
* The handler test harness `newTestHandler` (`internal/handler/common_test.go:13`) builds a real store in a temp
  dir; the existing dry-run test (`internal/handler/execute_pipeline_test.go:349-399`) uses the all-three form.
* The inventory is 18 tools, pinned by `TestInventoryJSON` (`internal/handler/inventory_test.go:18`).

### 0005's design, adopted unchanged

Every D2 sub-decision of 0005 stands, owner-ruled 2026-11-17, and is adopted here by reference; the PLAN restates
each in executable form:

* **D2.1 P-a**: the continuation runs the exact stored plan; a pre-flight refuses the whole run before any
  stage if a stage's tool no longer resolves (`plan_stale`).
* **D2.2 M-a**: mutators are injected at run time as a fresh call does; the dry-run disclosure says plainly that
  approval covers the analysis stages, not writes. The disclosure's words are fixed by 0005 (its Decided 9)
  and held by a test.
* **D2.3 W-a**: any caller holding the id may continue or reject, across reconnects; the id is the capability.
* **D2.4 I-a**: the id is server-issued, 128 random bits from `crypto/rand`, hex, prefixed; a caller-supplied
  `session_id` on a dry run is a label only.
* **D2.5 L-a**: persisted under its own key prefix; `PLANNED` valid 24 hours; terminal sessions kept as
  tombstones for one hour; at most 50 `PLANNED`, evicting the oldest; an interrupted run refused.
* **D2.6 A-a**: the claim is one atomic store transaction; the loser is refused with the winner's state.
* **D2.7 G-a**: unknown arguments and wrong types are refused by the handler.

## Decision Drivers

* The owner's primary agent (gobble, `defaultProvider: claude`) must be able to call this server's tools at all;
  today every Claude call fails on one schema before any model output.
* The schema must not advertise behaviour the handler does not implement; the owner believes continuation is
  implemented and enforced, and it is neither.
* The continuation design is already decided (0005's D2, every sub-choice owner-ruled); re-opening it would
  discard a settled decision and its evidence (R8's 26-row agreement table, R9's claim prototype).
* A form accepted by Anthropic, xAI, OpenAI, Google and OpenCode routes is worth more than a form that encodes
  the mode constraint in the wire.
* The mutation gate behind continuation must keep its existing safety properties: atomic claim, no re-run,
  pre-flight, git checkpoint and rollback unchanged.

## Considered Options

* A. Flat all-optional schema + implement 0005's D2 continuation contract (the owner's choice of 2026-11-25).
* B. Flat all-optional schema + strict "not implemented" refusals for continuation calls (this record's first draft).
* C. Keep the union and add a per-client schema rewrite (serve a flattened form to Anthropic routes only).
* D. Rewrite the union as nested `if/then` conditions inside the object (no top-level `anyOf`).

## Decision Outcome

Chosen option: "A. Flat all-optional schema + implement 0005's D2 continuation contract", because the owner chose
it explicitly on 2026-11-25 ("option 2"), because the design it implements is already decided and evidenced, and
because it is the only option under which the schema, the guide, the prompt and the handler finally say the same
thing on every route.

### Decision

1. **The wire schema carries no top-level `anyOf`/`oneOf`/`allOf`, anywhere in the repository's own tools.**
   `execute_pipeline`'s `inputSchema` in `internal/handler/inventory.go` becomes one flat `object` with all seven
   properties (`session_id`, `target`, `intent`, `plan_hash`, `dry_run`, `target_roles`, `reject`) present and
   optional, `additionalProperties: false`, and no `required` list. Each property description states its mode
   rule; the tool description states the two modes. The mode constraint is enforced by the handler's argument
   gate, which the contract table pins row for row to the flat schema (0005's R8 method, re-run against the flat
   form).
2. **The continuation is 0005's D2 contract, implemented.** Dry-run storage, server-issued 128-bit ids,
   `session_id`-only continuation, `reject: true`, the atomic claim, the full refusal vocabulary
   (`unknown_session`, `expired_session`, `session_running`, `session_already_run`, `session_failed`,
   `session_rejected`, `session_interrupted`, `plan_hash_mismatch`, `plan_stale`, `reject_needs_session`,
   `bad_arguments`), the 24-hour `PLANNED` lifetime, the 50-session cap, the one-hour tombstones, the
   interrupted-run rule, and the fixed disclosure words. A fresh call without `dry_run` keeps its current
   behaviour; the all-three form keeps working, with `session_id` a label on a dry run.
3. **No per-client rewriting.** The server serves one schema to all clients. gobble-cli's 0060 union
   normalisation, when it lands, sees no union here and changes nothing.
4. **A guard keeps it broken-at-build, not broken-at-call.** The inventory test asserts every own tool's
   `inputSchema` root holds none of `anyOf`, `oneOf`, `allOf`; `execute_pipeline`'s root holds exactly the seven
   properties and `additionalProperties: false`; the inventory count stays 18.
5. **0005's D3 sanitiser work stays out of scope here.** It is a separate defect with its own decided design;
   this record touches no sanitiser code. 0005's PLAN remains the record that owns it, and its status is
   annotated, not rewritten.

### Consequences

* Good, because the owner's Claude route works again, as do all routes that already worked.
* Good, because the schema, guide, prompt and handler agree; the owner's belief about the tool becomes true.
* Good, because the continuation's safety design is the one already ruled on and prototyped (0005's R8, R9),
  not a new invention.
* Neutral, because the either-or constraint is now enforced by the handler, invisible to clients until the call
  is refused — the same place 0005's agreement table already pins it.
* Bad, because a client can no longer learn the mode rules from the schema alone; it must read the descriptions
  or receive the handler's refusal. Acceptable: the descriptions carry the rules, and the contract table proves
  the gate enforces them.
* Bad, because this is a feature: a persisted session store, a state machine, a larger handler, and a tool that
  mutates files behind it. 0005's consequences stand; the strongest tier of model is warranted for the store
  and continuation phases and for an independent review.
* Bad, because a future re-introduction of a union needs a new record superseding this one. That is the standing
  rule for contradicting a decision, not a new cost.

### Confirmation

1. One table drives every argument combination through the emitted schema (two validators) and the handler's
   gate; the two agree row for row (0005's R8 method, re-run against the flat schema).
2. No call carrying `reject: true` executes a stage; a dry run executes none; asserted by call counts on an
   injected executor stub.
3. A session is claimed once (32 concurrent continuations of one id, 200 rounds, one winner); a repeated
   continuation never re-runs; every refusal is distinct and tested.
4. The emitted `tools/list` holds no root union keyword on any own tool; `execute_pipeline`'s root is the flat
   seven-property object; the inventory count is 18.
5. The dry-run disclosure is the fixed block of 0005's D2, word for word, held by a test.
6. A planted break per assertion, each seen to fail (PLAN).
7. An independent review before anything reaches the owner's running server.
8. The owner's live step: after building, installing and restarting, one gobble session on the claude provider
   completes a model call; a dry-run → continue → reject cycle works from gobble.

### State table

| State or input | Disposition | Required outcome and proof |
| --- | --- | --- |
| Schema served to any client (tools/list) | decided | Flat object, seven optional properties, no root `anyOf`/`oneOf`/`allOf`, `additionalProperties: false`, 18 tools. Inventory guard test. |
| Fresh call: `target`+`intent`, `session_id` omitted | decided | Runs; server-issued 128-bit id returned in metadata. Contract-table row + handler test. |
| Fresh call: all three of `target`+`intent`+`session_id` (owner's working form) | decided | Runs unchanged; on a dry run the supplied id is a label only. Existing test kept green + table row. |
| Fresh dry run | decided | Stores the exact plan as `PLANNED`, returns issued id, expiry, fixed disclosure; executes zero stages. Handler test with executor call counts. |
| Dry-run plan holding a MUTATOR-role or `apply_vetted_edit` stage | decided | Shown but not stored; the result says so. Handler test. |
| Continuation: `session_id` alone, or with `plan_hash` | decided | Atomic claim `PLANNED`→`RUNNING`; pre-flight over every stage; stored stages run through the existing loop; ends `COMPLETED`/`FAILED`. Handler + store tests. |
| Continuation with `reject: true` | decided | `PLANNED`→`REJECTED`, nothing runs, zero executor calls; repeated reject returns the same `REJECTED` result. Handler test with call counts. |
| `reject: true` without `session_id`, or with fresh-call arguments | decided | Refused (`reject_needs_session` / `bad_arguments`), never a run. Table rows + zero-call test. |
| Unknown key or wrong-typed value | decided | Refused `bad_arguments` by strict decode. Table rows. |
| Unknown session id | decided | `unknown_session`. Store + handler test. |
| `expires_at` past, entry still stored (inside TTL grace) | decided | `expired_session`. Store test with injected clock. |
| Second continuation of a `RUNNING` session | decided | `session_running`, refused with the winner's state. Claim-race test (32×200). |
| Continuation of a `COMPLETED` session | decided | `session_already_run`, quoting the stored artifact URIs. Handler test. |
| Continuation of a `FAILED` session | decided | `session_failed`. Handler test. |
| Continuation of a `REJECTED` session | decided | `session_rejected`. Handler test. |
| `RUNNING` session from another server instance (restart) | decided | `session_interrupted`; nothing resumed. Store test with instance nonce. |
| Stored `plan_hash` ≠ supplied | decided | `plan_hash_mismatch`, nothing runs. Handler test. |
| A stored stage's tool no longer resolves | decided | `plan_stale`, the stage named, zero stages run. Handler test. |
| 51st `PLANNED` session | decided | Oldest evicted. Store test. |
| Session key prefix | decided | Own prefix, not `raw:`; a `raw:` read of a session id returns nothing. Store test. |
| Server crash before a write syncs (`SyncWrites(false)`) | decided | The id may be `unknown_session`; accepted loss, as 0005's L-a states. Documented, not tested against a real crash. |
| Proxied downstream tools with root unions | out of scope | gobble-cli 0060 owns that rewrite; this server passes them through (0005's D3.4). |
| 0005's D3 sanitiser changes | out of scope | Separate defect, separate record; no sanitiser code touched here. |
| Live Claude call from gobble after the fix | decided | Owner's acceptance step: one gobble run on the claude provider completes a model call. Owner-controlled, not an automated test. |
| Re-introducing a union | open | Needs a new record superseding this one's D1; no work here. |

## Consequences

Option-level trade-offs are above. Operationally: the fix adds a session store, a state machine and a larger
handler; a rebuild and service restart of the magictools server is needed for the owner's live route to see it.
The new store keys are additive; no migration.

## Pros and Cons of the Options

### A. Flat schema + implement 0005's D2 continuation (chosen)

* Good, because the flat object is the form Anthropic's own tool schemas use; the refusal names only the union
  keywords, and a flat object with optional properties carries none.
* Good, because every other route already accepts flat objects.
* Good, because the schema's advertisement becomes true; the owner's belief about the tool becomes fact.
* Good, because the design is settled (0005's D2) and prototyped (R8, R9); no new architectural choice.
* Bad, because it is the largest option: persisted session store, state machine, concurrency, a mutating tool
  behind it.
* Bad, because the schema alone no longer proves the either-or rule to clients that ignore descriptions.

### B. Flat schema + strict "not implemented" refusals (this record's first draft)

* Good, because smallest; Claude works today.
* Bad, because the owner chose A explicitly on 2026-11-25; B keeps the guide and prompt lying, and keeps a
  decided feature unbuilt.

### C. Per-client schema rewrite for Anthropic

* Good, because every client keeps its ideal form.
* Bad, because the server must detect and version per-client forms it has no reliable way to identify at
  tools/list time; gobble is not distinguishable from any other stdio client.
* Bad, because it doubles the schema surface and every future change must be made twice.

### D. `if/then` nesting

* Good, because it keeps the constraint machine-readable in the wire.
* Bad, because `if/then` at the root of an `input_schema` is the same class of keyword Anthropic documents as
  unsupported alongside the union keywords; unverified where the union was verified-refused.
* Bad, because it is harder for every other client to read than a flat object with descriptions.

## More Information

* The failing evidence: `~/.local/state/gobble/logs/gobble.log` line of 2026-11-25 14:44:14 and gobble session
  transcript `2026-11-25T20-43-41-842Z_01a21473-2b92-78fd-8de0-24c093170283.jsonl` (owner's machine).
* go-llmprovider-sdk classifies this exact Anthropic refusal form (`tools.N.` + `input_schema`) as
  `ErrToolSchemaRefused` (its `internal/toolschema` 0084 record); the SDK reported it, it did not rewrite it —
  the fix belongs to the schema's owner, here.
* Supersedes the schema form (D1/S2) of
  [0005-MADR-make-execute-pipeline-schema-and-handler-agree.md](0005-MADR-make-execute-pipeline-schema-and-handler-agree.md),
  which was never built. Adopts its D2 continuation contract unchanged. 0005's D3 sanitiser work remains owned
  by 0005's PLAN.
* 0005's evidence index (E1–E31, R1–R9, P1–P3) is the prior grounding; this record's N1–N7 re-ground the
  relevant rows at `184370e`. The removed continuation's prior art (`2232ebf`, commits `3387f06`, `aea3a9d`,
  `e36357c`) and its read-then-write flaw (R9's planted break: 32 of 32 winners) are 0005's, cited there.
