---
status: accepted
date: 2026-11-26
decision-makers: repository owner
consulted: "the owner's report of 2026-11-16 and rulings of 2026-11-17, relayed by the cross-repository coordinator;
gobble-cli's 0060-MADR-normalise-root-level-unions-in-mcp-tool-schemas.md (draft, uncommitted); magic-cli-remote's
0096-MADR-kilo-model-catalog-scope-and-order.md; this repository at 368ac6a and, for the removed continuation, the
pre-import history reachable through the gitlab/main remote-tracking ref; github.com/modelcontextprotocol/go-sdk
v1.6.1 and github.com/maccavelli/mcplib v1.4.1 module source; the read-only checkouts of codex (57e64682f8),
kilocode (58072bd696) and grok-build (2bdd1d6a); go-llmprovider-sdk at b109cea (read-only); the drafting experiments
under More Information, run in a scratch copy of 368ac6a"
informed: "gobble-cli (its 0060 record names this one as its Q5); the cross-repository coordinator"
---

# Build continuation for execute_pipeline and make its contract, schema and sanitiser agree

## Context and Problem Statement

On 2026-11-16 the owner ran gobble with this server added, on a Grok model, and every prompt failed with xAI's
`invalid_client_tool_schema` refusal of `execute_pipeline` ("root schema is an anyOf/oneOf union with a non-object
branch"). gobble-cli's 0060-MADR-normalise-root-level-unions-in-mcp-tool-schemas.md decides what gobble does about
such schemas; its Q5 asks whether this server should be fixed as well. This record answers that for three defects,
all measured at commit `368ac6a` of `main`: the schema's untyped branches (item 1), the handler's refusal of what the
schema and the documentation advertise (item 2), and the empty objects this server sends (item 3). Its design
sections (D2) specify the continuation feature the owner asked for.

The body below is the decision of 2026-11-17, measured at `368ac6a`. It is not a description of HEAD. What the tree does at `34bf269` (2026-11-26) is amendment (b), at the end of this file. Read that before treating any "today" claim in the body as current.

### Decided (the owner, 2026-11-17, relayed by the coordinator; each label quoted only as a label)

1. **Both defects are fixed, by this repository's own record.** The label was "Yes, both, by that repository's own
   record (Recommended)": type the branches at the source, and make the handler agree with the schema.
2. **The mismatch is resolved by building the continuation, and the owner confirmed it twice.**
   * First answer: "Build continuation now". As put to the owner: a dry run stores a plan and returns a session id,
     and a later call continues or rejects it; a real feature with its own safety design (what may be resumed, by
     whom, for how long), far larger than a repair. The owner chose it knowing that. This is option H2 below, not the
     option this record's first draft recommended.
   * Put again because the first statement to the owner, that no continuation code exists, was incomplete: the history
     shows it was built on 2026-04-29 (`3387f06`), removed on purpose on 2026-05-01 (`aea3a9d`), and `session_id` made
     required on 2026-05-03 (`e36357c`), with no reason recorded for the removal. Second answer, which confirms the
     first: "Still build it, with the new safety design (Recommended)". As put: the ruling stands; the new design
     fixes
     the old one's flaws; if the owner remembers why it was removed, the design will address that.
3. **The schema keeps a union for the two modes, each branch repeating the root's properties.** The label was "Typed
   branches that each repeat the root's properties" (option S2): the form Grok's own client uses; needed only because
   continuation is built.
4. **The "trailing `{}`" clean-up covers proxied tools too.** The label was "Proxied tools too: fix the sanitiser for
   everything it touches (Recommended)". The owner first asked for the pros and cons of fixing the server's own tools
   only against all tools, leaning toward the wider scope. The coordinator's evaluation: both are the same code, since
   one sanitiser function runs on this server's own tools and on proxied ones; own tools only is small and enumerated
   (six empty objects, two of them sanitiser-added) with no stored data, but needs a special case in the shared
   function and leaves forwarded tools alone; proxied tools too is one fix in one function and covers where the real
   defect is, the retyping of untyped properties, but stored proxied schemas already carry the changes and need a
   re-sync or migration, and downstream schemas cannot be enumerated. The answer to "which did you mean" is therefore
   "the sanitiser's empty objects, wherever it adds them". The plan must establish how already-stored proxied schemas
   are refreshed before anything ships (D3.3).
5. **The empty `properties` objects were put twice, and the second answer governs.**
   * First answer: "Keep it on a tool's root; stop the sanitiser adding it to nested properties (Recommended)".
   * The question was put again because, after that answer, the coordinator found that Codex and Kilocode
     deliberately add `properties: {}` to object schemas on their OpenAI routes and that gobble adds it at the root;
     whether any provider refuses a nested object without it was not verified, and the sanitiser's original reason
     was, as relayed, not recorded (item 3 shows what the history does record). Removing the nested ones on a guess
     could make a provider refuse a tool for every prompt, as the original failure did.
   * Second answer, which supersedes the first: "Fix the retyping; keep or drop the nested ones only on evidence
     (Recommended)". As put: the sanitiser stops turning untyped properties into objects, which removes the empties it
     put on them; for properties that really are objects, the plan first establishes whether any provider refuses an
     object without `properties`, and removes the nested ones only if that is shown safe, otherwise keeps them and
     documents them as deliberate. The evidence is in item 3 and the rule in D3.2.
6. **The retyping is fixed in this plan, in the same function.** The label was "In this plan, in the same function
   (Recommended)": the sanitiser's pinned test is corrected to the right behaviour, with the reason recorded.
7. **The change reaches the running server only by the owner's hand.** The label was "I build, install and restart it
   myself, on my word (Recommended)". As put: the plan ends with the exact commands and what to check from Claude Code
   and from gobble with a Grok model; `make install` keeps no previous binary, so the plan also says how to roll back
   (PLAN, P9).

8. **A continuation runs the exact stored plan, refused up front if a tool no longer resolves.** The label was "The
   exact stored plan, refused up front if a named tool no longer resolves (Recommended)" (D2.1, P-a): the plan the
   owner approved is the plan that runs, and a stale one fails before any stage.
9. **Approving a plan covers the analysis plan only.** The label was "The analysis plan only; mutation decided at run
   time as today, and the dry run says so (Recommended)" (D2.2, M-a). The question as put told the owner that a dry
   run's plan cannot show the mutations (mutating stages are added at run time after the reporting stage) and that
   with
   no plan hash mutation proceeds under a placeholder approval. The dry-run result's disclosure of this is therefore
   part of the decided contract; its words are fixed in D2 and held by a test.
10. **Anyone on this server holding a server-issued random id may continue or reject.** The label was "Anyone on this
    server holding a server-issued random id (Recommended)" (D2.3, D2.4): the id is issued by the server, 128 random
    bits; a `session_id` a caller supplies on a dry run is kept as a label only.
11. **Lifetime.** The label was "On disk, 24 hours, at most 50 plans, an interrupted run refused (Recommended)"
  (D2.5):
    stored in the store under its own key prefix, 24 hours for a planned session, finished sessions kept one hour so a
    repeat gets a clear refusal, the oldest planned session evicted past 50, a run interrupted by a restart refused
    and never resumed.
12. **Unknown and mistyped arguments are refused.** The label was "Yes, the handler refuses unknown and mistyped
    arguments (Recommended)" (D2.7, G-a), so the schema's `additionalProperties: false` is true.
13. **A union from a downstream server passes through unchanged.** The label was "Pass it through unchanged
    (Recommended)" (D3.4).
14. **How the pair proceeds.** The label was "Approve the whole plan now, through the independent review". As put: the
    pair is committed and all code phases run, each committed and pushed to `main` on this approval, stopping before
    the owner's build, install and live checks. This is the owner's approval of the PLAN through P8, and the authority
    for commits and pushes to this repository's `main` for this plan only; P9 (build, install, restart, re-sync, the
    live checks) is the owner's alone, and tags and releases need a separate word.

### 1. The schema's two union branches are untyped

`execute_pipeline` is declared in the static inventory (E1). With its seven property descriptions shortened to `…`:

```json
{
  "type": "object",
  "properties": {
    "session_id": {"type": "string", "description": "… For CONTINUATION MODE: pass session_id WITHOUT target/intent …"},
    "target": {"type": "string", "description": "…"},
    "intent": {"type": "string", "description": "…"},
    "plan_hash": {"type": "string", "description": "…"},
    "dry_run": {"type": "boolean", "description": "…"},
    "target_roles": {"type": "array", "items": {"type": "string"}, "description": "…"},
    "reject": {"type": "boolean", "description": "Set to true when continuing a paused pipeline to reject the plan …"}
  },
  "additionalProperties": false,
  "anyOf": [
    {"required": ["target", "intent"]},
    {"required": ["session_id"]}
  ]
}
```

* **The root is an object schema; the branches are fragments** with no `type` and no `properties` (E1). Valid JSON
  Schema; refused by Grok.
* **It reaches the wire as written.** Registration takes the inventory's schema by tool name, sanitises it, then
  registers it (E2); the sanitiser never enters `anyOf` (E3). A raw `tools/list` response captured in process through
  the registration the binary performs (R1) holds the same `anyOf` of two untyped branches and nothing added to it.
* **It is the only union in the 18-tool static inventory** (R2). Proxied tools are forwarded too: after `align_tools`
  the middleware appends up to 20 recently discovered downstream tools to `tools/list` (E11), sanitised by the same
  function (E4). Whether a downstream server sends a union is not established.
* **The server validates nothing against the schema** (E10); the handler's own checks decide what a call may contain.

### 2. The handler refuses what is advertised, and the continuation was deliberately removed

Driven in process through the registered tool, with every combination checked against the emitted schema in the same
run (R4), pipeline enabled:

| Arguments | Emitted schema accepts | Handler outcome |
| :--- | :--- | :--- |
| none | no | refusal: "requires 'target', 'intent', and 'session_id' parameters" |
| `session_id` alone, with `reject`, or with `dry_run` | yes (branch 2) | the same refusal |
| `target` + `intent` (no `session_id`) | yes (branch 1) | the same refusal, although the schema says `session_id` is "Optional … Auto-generated if omitted" |
| `target` + `session_id`; `intent` + `session_id` | yes (branch 2) | the same refusal |
| `target` + `intent` + `session_id` | yes | proceeds: composes the DAG and runs it |
| the same + `dry_run: true` | yes | proceeds: returns the plan, runs nothing |
| the same + `reject: true` | yes | proceeds exactly as without `reject` |

* **Neither of the schema's two modes works; only the all-three call does.** `handleExecutePipeline` has one path
  (E5). `reject` is not in its argument struct, so it is silently ignored: a caller who sends it to abort gets the
  pipeline run, MUTATOR stages included. That is the finding with a safety consequence.
* **The continuation existed, and was removed on purpose.** This repository's own history (33 commits from HEAD)
  begins with a squashed import (`6e65cdb`), but an earlier history is still in the object store through the
  `gitlab/main` remote-tracking ref. Reading it corrects the first draft of this record, which said no continuation
  ever existed:
  * `3387f06` (2026-04-29) "implement pipeline continuation with human-in-the-loop approval": a fresh call ran the
    analysis, paused after the PLANNER stage when mutation was wanted, saved a `PipelineSession` as
    `AWAITING_APPROVAL`
    under the key `pipeline:<id>` with a 24-hour TTL (P1), and a call with `session_id` alone continued it by
    injecting the MUTATOR stages, or with `reject` rejected it (P2).
  * `aea3a9d` (2026-05-01) "convert pipeline execution to autonomous flow": "Remove Human-in-the-Loop (HITL) pause
    and continuation mode … Remove session persistence logic from BadgerDB". The schema text, the guide and the
    `pipeline-start` prompt were not updated.
  * `e36357c` (2026-05-03) "Require session_id for execute_pipeline tool": the handler stopped generating a
    `session_id` and required all three arguments; the inventory's text at HEAD still says "Optional … Auto-generated
    if omitted".
  So the all-three requirement and the missing continuation were each a deliberate change, and the documentation is
  what was left behind. The removed design is prior art, with defects of its own (D2.6): its session read-then-write
  was not atomic, its ids were `ep-<UnixNano>` (guessable), its dry run stored nothing, and it paused after
  analysis rather than at a stored dry-run plan, which is what the owner has now asked for.
* **What the tool's own documentation promises now.** The `session_id` and `reject` property descriptions (E1); the
  guide ("continue/reject an existing pipeline session", a dry run "to generate a paused plan", E8); the
  `pipeline-start`
  prompt (dry-run with `intent` and `target`, "extract the `session_id`", call again with "ONLY the `session_id`" or
  with `reject: true`, E7). They disagree with the handler (above) and with each other: the dry-run result carries no
  `session_id`, and the prompt's dry-run call lacks the `session_id` the handler requires.
* **What the tests cover.** One handler test, a dry run with all three arguments (E9). Nothing tests another
  combination, the schema, or a continuation.

### 3. The empty objects, and what is established about `properties: {}`

"Tools sent with a trailing `{}`" is not defined by any record, test or comment here or in gobble-cli (R5). A raw
`tools/list` captured in process (R1, R6): 18 tools, 11,833 bytes, no bytes after the envelope, no trailing `{}`
element, no empty tool, no `outputSchema`, `annotations`, `_meta`, `title` or `icons`, no empty union array or branch.
Six empty objects, all `"properties": {}`: four written in the inventory for no-argument tools (`wake_servers`,
`get_session_stats`, `get_health_report`, `self_check`, E1b) and two added by the sanitiser to `arguments` of
`align_tools` and `call_proxy` (E3). With a proxied tool seeded (R3) the sanitiser also adds `properties: {}` to every
nested object-typed property and **retypes every untyped property as `object`** (E3: a pinned test, E3b); `items: {}`
from a downstream schema is passed through (valid, means any item). All are valid JSON Schema. The owner's ruling
(Decided 4) makes "the sanitiser's empty objects, wherever it adds them" the finding.

**Why the sanitiser adds `properties: {}`: what is recorded here, and what is not.** HEAD's history does not say
(the sanitiser arrives in the squashed import), but the earlier history does (R7). `bb791d7` (2026-03-29) added the
sanitiser "to prevent panics when consuming schemas from sub-servers", so that a tool schema is "never nil,
defaulting to `{type: "object", properties: {}}`": a guard against the SDK's `AddTool` panic on a missing or
non-object
schema (E10), not a provider refusal; the recursion into `properties` is from the same commit. `592b6f5` (2026-03-30)
centralised it for "protocol-compliant defaults". The addition of `properties: {}` to nested object-typed properties
came with `9c81dd5` (2026-06-30, "modularize prompt generation and refactor dashboard telemetry"), whose message gives
no reason. None of these says a provider refuses an object without `properties`. What the other agents show:

| Claim | Evidence | Kind |
| :--- | :--- | :--- |
| A root object without `properties` is refused by "OpenAI models" | Codex's comment, adding `properties: {}` at the root: "OpenAI models mandate the 'properties' field", citing the OpenAI Agents SDK's fix (E15); gobble's comment, "some providers reject an object schema without properties", adding it at the root only (E17) | upstream comments; no recorded refusal; not verified |
| A nested object-typed property without `properties` is refused by some provider | Nothing found. Codex adds `properties: {}` to every object schema at every depth (E16) as "permissive defaults" for its own schema type, and Kilocode copies it for the OpenAI and Azure routes only (E18); neither says a provider refuses the nested form | not established |
| An untyped property should become an object | The opposite: Codex's commit `4dbca61e20` ("default unknown tool schemas to empty schemas") says retyping description-only nodes "invented a constraint" that "could incorrectly steer tool arguments away from the provider's actual accepted shape", and that "the Responses API accepts permissive empty schemas such as `{}` at nested property positions"; Kilocode returns `{}` for such nodes (E18) | upstream commit message and code |
| A provider's recorded refusal of an object schema without `properties` | None in go-llmprovider-sdk's fixtures at `b109cea`: the recorded tool-schema refusals are for a root `{"type":"string"}` only (E19) | read |

What removing the nested `properties: {}` would risk: if an OpenAI-class route refuses a nested object without it,
every prompt on that route fails with the tool, as the 2026-11-16 failure did. That risk is unquantified, so the
plan's
default is to keep them (D3.2).

### What each part of the system says today, and the clients

The contract as advertised, by source:

| Source | Says |
| :--- | :--- |
| Schema (E1) | two modes: `target` + `intent`, or `session_id` alone; `session_id` optional and auto-generated; `reject` aborts a paused pipeline |
| Tool `description` (E1) | silent on continuation |
| Guide (E8) | a dry run "generates a paused plan"; continue with its `session_id`, or reject with `session_id` + `reject: true` |
| `pipeline-start` prompt (E7) | dry run with `intent`, `target`, `dry_run: true`; extract `session_id` from the result; continue with ONLY `session_id`; reject with `session_id` + `reject: true` |
| Handler (E5) | all three of `target`, `intent`, `session_id` required; one pass; `reject` unread |
| Dry-run result (E5) | no `session_id`; "call execute_pipeline again with dry_run=false" |

The decided contract (D2) makes every row true. The clients:

| Client | What it does with the owner's schema | Source | Kind |
| :--- | :--- | :--- | :--- |
| Grok (xAI) | Refuses the tool; its own client sends root unions whose branches are typed objects with their own `properties` and `required` | gobble-cli 0060; E20 | the owner's paste; source |
| Anthropic Messages (through Kilo) | Said to reject any top-level `anyOf`/`oneOf`/`allOf` in a tool's `input_schema` | magic-cli-remote 0096, Context item 1 | a record's claim, not verified here |
| Codex | Types such fragments itself | gobble-cli 0060 (C1) | source, as read by that record |
| OpenCode | Sends a typed root as written | gobble-cli 0060 (O1) | source, as read by that record |
| Claude Code | Rewrites root unions in an undocumented form; the owner uses this tool from it | gobble-cli 0060 (CC3) | documentation |
| gobble | Will rewrite such schemas at load when its 0060 lands | gobble-cli 0060 | draft |

Because the owner chose to keep two modes, the union stays; the Anthropic claim, if true, means a second form (no
union) would be needed for that route, which is not in this record's scope and is not verified.

### What exists to build on, from the code

1. **`PipelineSession`** (E6) holds `SessionID`, `Target`, `Intent`, `PlanHash`, `Status` (commented `ANALYZING,
   MUTATING, COMPLETED, FAILED`), `Stages`, `CompletedSteps`, `Warnings`, `ChainedOutput`, timestamps. It is unused:
   it
   is what is left of the removed continuation's session (P3), with `AWAITING_APPROVAL`, `REJECTED` and
   `SocraticPassed` removed. It has no plan-versus-run split, no expiry and no claim state.
2. **The store** is BadgerDB on disk, opened with `SyncWrites(false)` (E21): it persists across restarts, a crash can
   lose the last writes, it supports per-entry TTL (`SaveWithTTL`, E22) and transactions with conflict detection
   (`UpdateWithRetryCtx`, E22), and only one server process opens it (a lock). `SaveRawResource` keys are `raw:<id>`
   with a one-hour TTL, and are readable by anyone through the `mcp://magictools/raw/{id}` resource (E23): sessions
   must **not** use that prefix. `WipeAll` (the CLI's wipe) drops everything, sessions included (E24).
3. **A plan** is `[]PipelineStep` (name, role, phase, purpose, args, contracts: JSON-serialisable), plus `warnings`
  and
   a `recallContext` string, composed from store searches, synergy and failure-anchor scores, and Recall reads (E25).
   `freshAnalysisRoles` removes the MUTATOR role from the roles asked for (E25), but nothing guarantees the composed
   list holds no mutating stage (R8). The plan therefore **cannot show mutations**: MUTATOR stages are injected at run
   time after the REPORTING stage, from the Socratic verdict and the analysis output (E26), and a step's output can
   inject another stage (`DYNAMIC_INJECT`, E26). With no `plan_hash`, mutation proceeds with the placeholder
   `auto-approved-orchestrator-override` (E26, E27); whether go-modernizer's `apply_vetted_edit` honours it is not
   verified.
4. **A dry run executes no stage.** `freshPipeline` returns the plan before the execution loop (E5); the loop is the
  only
   place `executeDAGStep` and `executeMutatorAST` are called (E6). What it does do: Recall reads (E25), a telemetry
   `InitializePipeline` that is never closed on the dry-run path (E5; a small leak, not decided here).
5. **A plan built earlier may not be valid later.** Stages re-resolve at run time: arguments come from the store's
   current schema for the tool (`buildSchemaAwareArgs`, falling back to injecting everything when the record is gone),
   the server is booted lazily, and the call goes straight to the downstream server (E5): a tool removed by a sync
   fails at its stage, after earlier stages have run. `sync_servers`, `reload_servers` and `align_tools` can all
   change
   the tool set between a dry run and a continuation; the stored sessions themselves are untouched by them.
6. **Session ids over the transport.** The SDK's `ServerSession.ID()` is empty on stdio and set on HTTP (E28); the
   server stores `"active-session"` for an empty id (E29). In stdio mode one process is one client; in service mode
   there are several MCP sessions on one process.

### Evidence index

Marks: **read** (a line at the commit named), **run** (an experiment for this record, in a scratch copy of `368ac6a`).

| Id | Claim | Location | Mark |
| :--- | :--- | :--- | :--- |
| E1 | The tool: description, properties, `additionalProperties: false`, root `anyOf`; `session_id` and `reject` text | `internal/handler/inventory.go:286-336`; `:287`, `:290`, `:292-294`, `:317-319`, `:322-336` | read |
| E1b | The no-argument tools' `"properties": {}` | `internal/handler/inventory.go:29`, `:73`, `:83`, `:219` | read |
| E2 | `addTool` takes the inventory schema by name, sanitises, registers | `internal/handler/handlers.go:201-226` (sanitise `:219`, `AddTool` `:225`) | read |
| E3 | The sanitiser: adds `type: object` when missing; recurses into `properties` only; adds `properties: {}` to an object map that has none | `internal/util/schema_util.go:47-50`, `:52-58`, `:60-62` | read |
| E3b | The test that pins the retyping | `internal/util/schema_util_test.go:27-48` | read |
| E4 | Other callers: proxied schemas on sync and on `align_tools` | `internal/client/sync_save_helpers.go:196`; `internal/handler/align_tools_helpers.go:217` | read |
| E5 | The handler: argument struct without `reject`; the three-way refusal; one path; the dry-run note; telemetry before the dry-run return; run-time step resolution | `internal/handler/execute_pipeline.go:40-47`, `:50-54`, `:56`, `:78`, `:136`, `:179`, `:212-218` | read |
| E6 | One pass over the DAG; `PipelineSession` unused; `freshPipelineExecute` | `internal/handler/execute_pipeline.go:29-32`; `internal/handler/execute_pipeline_run_helpers.go:316-337`; `internal/handler/pipeline_session.go:12` | read |
| E7 | The prompt's dry-run and continuation steps | `internal/handler/prompts.go:39-40` | read |
| E8 | The guide's behaviour row and "two-stage" flow | `docs/guides/mcp-tools.md:64`, `:73-77` | read |
| E9 | The only handler test | `internal/handler/execute_pipeline_test.go:349-399` | read |
| E10 | `Server.AddTool` leaves validation to the caller | `github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/server.go:219-229` | read |
| E11 | The middleware appends session-cached proxied tools to `tools/list` | `internal/handler/list_tools_handler.go:70-96` | read |
| E12 | The registration order the binary uses; the inventory-size test | `cmd/mcp-server-magictools/main.go:446-448`; `internal/handler/inventory_test.go:18` | read |
| E15 | Codex: root `properties: {}` for "OpenAI models" | `codex@57e64682f8:codex-rs/tools/src/mcp_tool.rs:45-53` | read |
| E16 | Codex: default `properties` on every object schema, nested | `codex@57e64682f8:codex-rs/tools/src/json_schema.rs:165`, `:194-201` | read |
| E17 | gobble: root `type` and `properties`, "some providers reject" | `gobble@2004980:mcpclient/tool.go:63-65`, `:74-76` | read |
| E18 | Kilocode: "Mirrors Codex's … lowering"; `{}` for unknown nodes; `properties = {}` on objects; applied to the OpenAI and Azure packages | `kilocode@58072bd696:packages/opencode/src/provider/transform.ts:1818`, `:1894`, `:1897`, `:1922` | read |
| E19 | The SDK's recorded refusals are for a root string schema | `sdk@b109cea:docs/reports/0062-REPORT-thinking-refusals.md:206`, `:208` | read |
| E20 | Grok's own client: a root `oneOf` of typed-object branches with their own `properties`, and the comment | `grok-build@2bdd1d6a:crates/codegen/xai-grok-tools/src/implementations/use_tool/input.rs:203-212` | read |
| E21 | The store opens on disk with `SyncWrites(false)` | `internal/db/badger.go:485` | read |
| E22 | TTL entries and conflict-retrying transactions | `internal/db/badger.go:2250`, `:586` | read |
| E23 | `raw:` keys, one-hour TTL, read through a resource | `internal/db/badger.go:1442-1453`; `internal/handler/resource_handlers.go:13-18` | read |
| E24 | `WipeAll` and the wipe command | `internal/db/badger.go:2472`; `cmd/mcp-server-magictools/main.go:148` | read |
| E25 | Plan composition: Recall reads, store search, MUTATOR role filtered from the roles asked for | `internal/handler/execute_pipeline_compose_helpers.go:10`, `:67` | read |
| E26 | Run-time injection of MUTATOR stages and of `DYNAMIC_INJECT` stages; the plan-hash fallback | `internal/handler/execute_pipeline_run_helpers.go:124`, `:143`, `:205`, `:246` | read |
| E27 | The placeholder plan hash | `internal/handler/constants.go:113` | read |
| E28 | `ServerSession.ID()` is empty unless the transport has one | `github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/server.go:1160` | read |
| E29 | `"active-session"` for an empty id; the HTTP handlers | `cmd/mcp-server-magictools/main.go:398`, `:656`, `:668` | read |
| E30 | Stored tool schemas are the sanitised ones; the composite sync hash is over the sanitised hash; unchanged hashes skip the save; boot syncs each server | `internal/client/sync_save_helpers.go:196`, `:211`, `:269`, `:280`; `internal/client/sync_helpers.go:25`; `cmd/mcp-server-magictools/main.go:1436`, `:1514` | read |
| E31 | `align_tools` re-sanitises the stored schema at emission | `internal/handler/align_tools_helpers.go:206`, `:217` | read |
| P1 | The removed session persistence (`pipeline:<id>`, 24-hour TTL) and its statuses | `2232ebf:internal/db/badger.go:1371`, `:1384`; `2232ebf:internal/handler/pipeline_session.go:19`, `:26` | read |
| P2 | The removed continuation: mode discrimination, id `ep-<UnixNano>`, the pause, status check, rejection, plan-hash check, delayed cleanup | `2232ebf:internal/handler/execute_pipeline.go:54`, `:66`, `:334`, `:422`, `:432`, `:440`, `:449`, `:463` | read |
| P3 | The commits: `3387f06`, `aea3a9d`, `e36357c` | `git log gitlab/main` | read |
| R1 | The raw `tools/list` bytes, static and with a proxied tool seeded | `tools_list_raw.json`, `dynamic/tools_list_raw.json`, scratch | run |
| R2 | 18 tools, one union keyword at any depth | `inventory_scan.py`, scratch | run |
| R3 | The sanitiser on a proxied schema | `dynamic/tools_list_raw.json`, scratch | run |
| R4 | The first argument-combination table | `combos.txt`, scratch | run |
| R5 | A search of this repository and gobble-cli for a trailing-`{}` finding | none found | read |
| R6 | Every empty `{}`, `[]`, null in R1; inventory-versus-wire diff | `empties.py`, `inv_vs_wire.py`, scratch | run |
| R7 | The sanitiser's history: a panic guard in `bb791d7`, centralised in `592b6f5`, nested `properties: {}` from `9c81dd5`; no provider refusal recorded | `git log --all --follow -- internal/util/schema_util.go` | read |
| R8 | The designed schema and gate, 26 rows through two validators; a planted break | `agreement_table.txt`, `s2_schema.json`, scratch | run |
| R9 | The atomic claim on a scratch Badger store; a planted break | `claim_prototype.txt`, scratch | run |

**Annotation, 2026-11-17, added by 0008-PLAN-redesign-the-list-tools-middleware-under-the-complexity-limit.md.**
The redesign of `ListToolsMiddleware` moved the code that row E11 and the sentence in the section on the schema's two
union branches cite as `internal/handler/list_tools_handler.go:70-96`; those lines no longer hold the session merge.
The same code is now in `internal/handler/list_tools_handler.go`: the session merge, `mergeSession`, at `:104-132`,
with the session-id lookup, `listToolsSessionID`, at `:134-142`, and the static-inventory fallback, `staticInventory`,
at `:80-102`; each range runs from the function's doc comment to its closing brace. What the rows say is unchanged: the
middleware appends the session cache's tools to `tools/list` as the cache holds them, without sanitising, copying or
capping them. The original cell and sentence are left as written.

## Decision Drivers

* **A caller must be able to rely on the schema**: every schema-valid call does what the schema says, and no
  schema-invalid call runs anything.
* **A human gate that can be skipped is not a gate.** `reject` must never run a stage; a session is claimed once.
* **The only call that works today (all three arguments) keeps working**, with and without `dry_run`.
* **Every client must accept the schema as written.** Grok refuses untyped branches (item 1).
* **One source of truth.** Schema, handler, prompt, guide and tests are statements of one contract; one table drives
  them.
* **Evidence before removing what another agent deliberately adds** (Decided 5).
* **The owner is the only user**; no compatibility shim for other callers.
* **Nothing about the owner's running server changes by drafting.**

## Considered Options

### D1. The schema's form: decided (S2); the others listed for the record

* **S1.** Typed branches under the root, each only `type` and `required`.
* **S2.** Typed branches that each repeat the properties they use, with `additionalProperties: false` as the root
  has it. **Decided** (Decided 3).
* **S3.** No union (one mode).
* **S4.** Two tools.

### D2. Continuation

Decided to build (H2). The alternatives, for the record: **H1** correct the contract to the code (one mode);
**H3** split into two tools; **H4** accept `session_id` alone and answer "not resumable" (a workaround). The design
choices inside H2, each with its options:

* **D2.1 What is stored and what a continuation runs.** P-a: the exact plan, with a pre-flight that refuses the whole
  run, before any stage, if a stage's tool no longer resolves. P-b: the exact plan, no pre-flight (a missing tool
  fails at its stage). P-c: a re-plan from the stored target and intent (the approved plan and the run plan can
  differ: scores and the tool set change).
* **D2.2 Mutation.** M-a: as a fresh call today, mutators are injected at run time from the Socratic verdict; the
  dry-run result says plainly that approval covers the analysis plan, not writes. M-b: a continuation runs the
  analysis and pauses again before mutation, and a second call with the same `session_id` approves or rejects the
  mutation (the removed design's pause, with no schema change). M-c: a continuation never mutates; mutation stays a
  fresh-call-only path.
* **D2.3 Who may continue or reject.** W-a: any caller on this server holding the id, across reconnects, with an
  unguessable server-issued id. W-b: only the MCP session that created it. W-c: any holder of the id who also echoes
  the stored `plan_hash`.
* **D2.4 `session_id` on a call that stores a session.** I-a: always issued by the server (128 random bits from
  `crypto/rand`, hex, prefixed); a value the caller supplies on a dry run is kept as a label only. I-b: a
  caller-chosen id is accepted if unused (overwriting only a `PLANNED` session). I-c: required from the caller, as
  today.
* **D2.5 Lifetime and storage.** L-a: persisted under its own key prefix, `PLANNED` valid 24 hours (the removed
  design's figure), terminal sessions kept as tombstones for one hour, at most 50 `PLANNED`, evicting the oldest, an
  interrupted run refused. L-b: memory only. L-c: persisted, 1-hour lifetime.
* **D2.6 Concurrency and the removed design's flaws.** Claim by one atomic store transaction (R9); the loser is
  refused with the winner's state. Options: A-a that (taken: it follows from the decided persistence); A-b an
  in-process mutex over an in-memory map
  (only with L-b); A-c the removed design's read-then-write (not atomic: R9's planted break ran 32 winners of 32).
* **D2.7 Argument strictness.** G-a: unknown arguments and wrong types are refused by the handler (the schema's
  `additionalProperties: false`, enforced); G-b: ignored as today; G-c: logged only.

### D3. The sanitiser, for everything it touches (Decided 4 to 6)

* **D3.1** stops retyping untyped properties (Decided 6).
* **D3.2** nested object-typed properties: keep `properties: {}` unless the plan's evidence step shows removal safe
  (Decided 5).
* **D3.3** stored proxied schemas are refreshed before anything ships (Decided 4).
* **D3.4** a union arriving from a downstream server: pass through, rewrite as gobble's 0060 D2 does, or flag.

## Decision Outcome

ACCEPTED 2026-11-17: the owner's rulings (Decided 1 to 14) settle every choice below; the reasons are kept.

* **D2.1: P-a**, because the owner approves one plan and one plan must run, and a pre-flight turns a mid-run failure
  into a refusal before anything ran.
* **D2.2: M-a**, because it keeps a fresh call's behaviour (the repository's rule: keep existing behaviour unless the
  owner says otherwise), needs no mid-run persisted state, and is made honest by the dry-run notice. M-b, the
  stronger gate, was offered and not chosen.
* **D2.3: W-a**, because clients reconnect between a dry run and a continuation (stdio restarts the process; service
  sessions expire), the id is the capability, and binding to an MCP session would break both.
* **D2.4: I-a**, because a caller-chosen id is a weak capability and the only call that works today stays valid (it
  becomes a label).
* **D2.5: L-a**, because a stdio client restart is a process restart and a memory-only plan would be lost exactly
  when the owner reviews it; persisted state needs the interrupted-run rule, which is cheap.
* **D2.6: A-a**; **D2.7: G-a**.
* **D3.4: pass through unchanged** (Decided 13). This server does not alter what a downstream server declares beyond
  repairing
  what is invalid; gobble's rule handles the clients.

### D2. The decided contract (what the schema, descriptions, guide and prompt will all say)

**Mode 1, fresh** (`target` and `intent` required; `session_id`, `plan_hash`, `dry_run`, `target_roles` optional;
`reject` not accepted):

* without `dry_run` (or `false`): composes and runs the DAG in one pass, as today (D2.2 M-a). `session_id` is
  optional: a value supplied is used as the correlation id as today; omitted, the server generates one. The result
  returns it in its metadata.
* with `dry_run: true`: composes the plan, runs nothing, stores it as `PLANNED`, and returns the plan, the issued
  `session_id` (D2.4), the expiry, and the disclosure below, in these words. A plan holding any MUTATOR-role stage
  or any `apply_vetted_edit` stage is shown but **not stored**
  and is told so (a stored plan never contains a mutator).

**The dry-run disclosure (Decided 9; the words are fixed, with `<id>` and `<expiry>` filled in, and held by a test).**
The dry-run result carries this block after the plan:

```text
Approval of this plan covers the analysis stages listed above, not file writes.
Mutating stages are not part of this plan: they are added at run time, after the
reporting stage, when the Socratic verdict and the analysis call for them. With no
plan_hash, mutation proceeds under the placeholder approval
"auto-approved-orchestrator-override".
To continue: call execute_pipeline with session_id "<id>" (valid until <expiry>).
To discard: call it with session_id "<id>" and reject: true.
```

**Mode 2, continue or reject** (`session_id` required; `plan_hash` and `reject` optional; nothing else accepted):

* `reject` absent or `false`: continues. The session is claimed (`PLANNED` to `RUNNING`) by one atomic transaction; a
  stored `plan_hash`, if any, must equal a supplied one; a pre-flight confirms every stored stage still resolves; then
  the stored stages run through the same loop a fresh call uses (so the MUTATOR gate, the git checkpoint and the
  rollback are unchanged), and the session ends `COMPLETED` or `FAILED`. The result carries the same envelope as a
  fresh run.
* `reject: true`: transitions `PLANNED` to `REJECTED`, discards the stored stages, runs nothing, and returns a result
  whose status is `REJECTED` with no steps. The reject path has no access to the executor: it is a function that is
  never given the proxy service, and the handler decides it before any other branch.
* `reject: true` on a call without `session_id`, or with `target`, `intent`, `dry_run` or `target_roles`: **refused**
  (the schema excludes it and the handler refuses), never ignored.

Refusals, each distinct and never a fresh run: `unknown_session`, `expired_session` (past `expires_at`, inside the
grace the store keeps the entry), `session_running`, `session_already_run` (with the stored artifact URIs),
`session_failed`, `session_rejected` (a repeated reject returns the same `REJECTED` result), `session_interrupted`
(the
server restarted while it ran; nothing is resumed), `plan_hash_mismatch`, `plan_stale` (a stage no longer resolves;
names it, nothing ran), `reject_needs_session`, `bad_arguments`. A repeated continuation never re-runs.

**The two branch schemas** (S2; the root keeps all seven properties with descriptions, `additionalProperties: false`
and `anyOf` of exactly these two branches; descriptions are not repeated in the branches):

```json
{
  "type": "object",
  "properties": {
    "target":       {"type": "string"},
    "intent":       {"type": "string"},
    "session_id":   {"type": "string"},
    "plan_hash":    {"type": "string"},
    "dry_run":      {"type": "boolean"},
    "target_roles": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["target", "intent"],
  "additionalProperties": false
}
```

```json
{
  "type": "object",
  "properties": {
    "session_id": {"type": "string"},
    "plan_hash":  {"type": "string"},
    "reject":     {"type": "boolean"}
  },
  "required": ["session_id"],
  "additionalProperties": false
}
```

The branches are disjoint (the first requires `target`, the second forbids it), so `anyOf` and `oneOf` accept the same
calls. Branch (a) accepts `target` + `intent`, with or without `session_id`, `plan_hash`, `dry_run`, `target_roles`.
Branch (b) accepts `session_id` alone, with or without `plan_hash` and `reject`. Everything else is refused, including
`reject` with any fresh-call argument and any unknown key.

**Agreement, row for row** (R8: the schema above with two independent validators, `santhosh-tekuri/jsonschema` v5 and
`google/jsonschema-go` v0.4.3, against the designed argument gate as a pure function; 26 rows, 0 disagreements; the
same run with the gate's `reject` check removed disagreed on the `reject`-alone row and failed):

| Arguments | Schema | Designed gate |
| :--- | :--- | :--- |
| none | refuse | refuse |
| `session_id`; `session_id` + `plan_hash`; `session_id` + `reject: false` | accept | continue |
| `session_id` + `reject: true` | accept | reject the session |
| `reject: true` alone; `reject: false` alone | refuse | refuse |
| `session_id` + `dry_run`; + `target_roles` | refuse | refuse |
| `target` alone; `intent` alone; `target` + `session_id`; `intent` + `session_id` | refuse | refuse |
| `target` + `intent`; + `plan_hash` + `target_roles`; + `dry_run: false` | accept | fresh run |
| `target` + `intent` + `dry_run: true` | accept | fresh dry run, stored |
| `target` + `intent` + `session_id` (today's working call), with or without `dry_run: true` | accept | fresh run, or fresh dry run (label only) |
| `target` + `intent` + `reject` (true or false); with `session_id` + `reject: true`; `session_id` + `reject: true` + `dry_run` | refuse | refuse |
| an unknown key; `dry_run` as a string; `target_roles` with a non-string | refuse | refuse |

**The form against gobble's 0060 rule (D2 of that record) and Grok's own client.** The rule applies when the root
type is
exactly `object`, the root has exactly one of `anyOf`/`oneOf`, the root's other keywords are among the allowed set,
and
every branch is an object schema with no `$ref`: all true here. It then types untyped branches (ours are typed), and
copies the root's object keywords only into a branch that has none of `properties`, `patternProperties`,
`additionalProperties`: both of ours have their own, so by the rule as written gobble sends this form unchanged. That
is read from the draft's text; the rule is not implemented. Grok's own client sends a root `oneOf` of typed-object
branches, each with its own `properties` and `required`, and excludes the other branches' arguments with `not`
(E20); ours excludes them with `additionalProperties: false` in each branch. Same family, not identical. Whether xAI
accepts this schema is not verified: it is the owner's live step.

**Existing callers.** The all-three call keeps working, with and without `dry_run: true`; the `session_id` becomes a
label on a dry run. `target` + `intent` starts to work. A call with `session_id` alone starts to do what it says. Any
call carrying `reject: true` that ran the pipeline before is now refused unless it is a continuation or rejection. An
extra or mistyped argument that was ignored is now refused (D2.7 G-a): the owner's own calls are the all-three form.
The guide and the prompt are rewritten to the decided contract in the same commit.

**What a dry run is guaranteed not to do.** It executes no stage: it returns before the only loop that calls
`executeDAGStep` or `executeMutatorAST` (E5, E6); a test asserts it with an executor that counts calls. It does read
Recall and the store. That a composed plan holds no mutating stage is **not** guaranteed by construction (R8's code
reading), so the design enforces it: a plan with one is not stored.

### D3. The sanitiser

**D3.1 Behaviour, as a table the tests hold** (for everything the sanitiser touches, own tools and proxied):

| Input | Output |
| :--- | :--- |
| root with no `type` | `type: object` added (MCP requires an object root) |
| root with no `properties` | `properties: {}` added at the root (Codex, gobble: E15, E17) |
| an untyped property (description only) | unchanged: no `type` added, no `properties` added (the fix; Codex and Kilocode keep such a node permissive, E18) |
| a `null` property | `{}` (permissive; the one empty the sanitiser may produce, from an invalid input) |
| a property typed `object` with no `properties` | `properties: {}` kept unless D3.2's evidence shows removal safe, then dropped |
| a property typed `object` with its own `properties` | recursed into, otherwise unchanged |
| `items: {}` or any `{}` the downstream schema wrote | unchanged |
| a union (`anyOf`, `oneOf`, `allOf`) at the root or nested | unchanged, byte for byte (D3.4) |
| keywords the sanitiser does not know (`$ref`, `$defs`, `enum`, `default`, `additionalProperties`, `patternProperties`) | unchanged |

**D3.2 Establishing whether a provider refuses an object without `properties`.** Evidence that counts, in order:
(1) a recorded refusal in go-llmprovider-sdk's fixtures (none at `b109cea`; re-checked when the step runs); (2) an
upstream agent's test or comment naming the provider and the refusal (found for the root only, E15; none for nested);
(3) an owner-run live probe, statuses only, with the owner's own keys, on each route the owner uses. An agent never
holds or uses a real key. The rule: nested `properties: {}` are removed only if (1) and (2) show no refusal **and**
(3)
shows acceptance, a nested object schema without `properties` answered successfully, on every probed route. If any
route refuses, if the evidence is mixed, or if (3) is not run, the nested ones are kept, documented as deliberate with
the evidence, and the MADR amended. The default is to keep. The retyping fix does not wait for this.

**D3.3 Stored proxied schemas.** They were sanitised before being stored (E30), so each stored copy of a tool with an
untyped property already says `type: object`, and **re-sanitising at emission cannot undo it**: the old output is
indistinguishable from a property that was typed `object` (E31). The refresh therefore needs the downstream server's
raw schema, i.e. a sync. The code supports it without a migration: the sync's skip hash is computed over the
sanitised schema's hash (E30), so a changed sanitiser changes the hash of exactly the tools it changes, the next
sync of
that server does not skip, and the stored schema is replaced. A boot syncs every configured server (E30), so the
owner's
restart is enough, and `sync_servers` forces it. Both need live downstream servers: an owner step with its commands
(PLAN, P9); nothing an agent runs. A test shows a stored old-form schema is replaced by a sync of the same downstream
tool. Another schema repair sits before the sanitiser (`autoPromoteRequiredFields`, E30's file); it is not changed
here.

### Consequences

* Good, because every schema-valid call does what the schema says, and the abort that used to run the pipeline cannot.
* Good, because the stored plan is the plan that runs, the id is unguessable, the claim is atomic, and every refusal
  is
  distinct (R9).
* Good, because the Grok refusal is removed at the source (to be confirmed live), in the form Grok's own client uses.
* Good, because untyped properties of proxied tools stop becoming object-only, which is a correctness fix, not
  cosmetics (Decided 6).
* Bad, because it is a feature: a persisted session store, a state machine, a larger handler, and a tool that
  mutates files behind it. The strongest tier of model is warranted for those phases and for an independent review
  (PLAN).
* Bad, because approval of a dry-run plan covers analysis only (D2.2 M-a); the notice says so; M-b is the stronger
  option.
* Bad, because the schema grows (two branches repeat property types) and keeps a union that a route outside Grok's
  (the Anthropic claim) may refuse.
* Bad, because a stored proxied schema stays wrong until its server is re-synced (D3.3), and a rollback to the old
  binary would re-add the retyping on the next hash change.
* Neutral, because `session_*` keys are new to the store: older binaries are not known to iterate them (not verified).

### Confirmation

1. One table drives every argument combination through the emitted schema (a validator) and the handler's gate; the
   two must agree row for row (R8's table is its start).
2. Tests for each refusal above; for `reject` never running a stage (an executor stub counts calls: zero, on every
   `reject: true` path, and on a dry run); for the claim (32 concurrent continuations of one id, 200 rounds: one
   winner; a continuation racing a reject: one winner), expiry, the cap, a restart (interrupted), and a stored plan
   with
   a missing tool (`plan_stale`, nothing ran).
3. The emitted `tools/list` has no empty object except the root `properties` of a no-argument tool, no `{}` branch, no
   bytes after the envelope, no root union other than this tool's two typed branches.
4. The sanitiser table above, held by tests; and the stored-schema refresh test (D3.3).
5. A planted break per assertion, on a scratch copy, each seen to fail (PLAN).
6. An independent review before anything reaches the owner's running server.
7. The owner's live step: after building, installing and restarting, from Claude Code and from gobble with a Grok
  model.

## Pros and Cons of the Options

### S1. Typed branches, `type` and `required` only

* Good, because smallest. Bad, because xAI may compile each branch with its arguments unconstrained (inferred).
  Not chosen.

### S2. Typed branches repeating their properties (decided)

* Good, because the form Grok's own client uses and gobble's rule leaves unchanged; the branches exclude each other's
  arguments, so the schema states the two modes exactly.
* Bad, because it repeats seven property types and keeps a union.

### S3. No union

* Good, because nothing for any client to refuse. Bad, because there is then one mode, which the owner has ruled
  against by choosing H2.

### S4. Two tools

* Good, because each tool's schema is plain. Bad, because it changes the inventory count (`inventory_test.go:18`),
  every
  client's allow-list and the prompt, and builds the same continuation.

### H1. Correct the contract to the code

* Good, because it is a small change and ends the mismatch. Bad, because it withdraws a feature the owner wants.
  Not chosen.

### H2. Build the continuation (decided)

* Good, because the schema, guide and prompt become true, and the owner gets the gate they describe.
* Bad, because it is the largest option: new storage, a state machine, concurrency, and a mutating tool behind it.

### H3. Two tools; H4. Accept `session_id` alone and answer "not resumable"

* H3: Good, because exact schemas. Bad, because S4's costs plus H2's. H4: Good, because it keeps the schema and
  removes
  the argument error. Bad, because it is a workaround: the contract keeps promising what always fails. Neither chosen.

### D2.1 P-a, P-b, P-c

* P-a: Good, because the approved plan is the run plan and a stale plan fails before anything runs. Bad, because the
  pre-flight is more code and a stage can still fail mid-run for other reasons.
* P-b: Good, because simplest. Bad, because a removed tool fails after earlier stages ran.
* P-c: Good, because always current. Bad, because the owner approved one plan and another runs, which defeats the
  gate.

### D2.2 M-a, M-b, M-c

* M-a: Good, because a fresh call's behaviour is unchanged and no mid-run state is stored. Bad, because approving the
  plan does not approve writes; the notice says so.
* M-b: Good, because the human approves writes, and the removed design proved the shape. Bad, because it needs mid-run
  persisted state (the second claim, its interruption rules) and a second round trip.
* M-c: Good, because a continuation can never write. Bad, because the dry-run, review, mutate flow the prompt
  describes cannot be completed through it.

### D2.3 to D2.7

* W-a: Good, because it survives reconnects; bad, because the id alone is the capability. W-b: Good, because tighter;
  bad, because on stdio the session id is empty (E28), so it binds nothing there, and it breaks reconnects in service
  mode. W-c: Good, because a second secret; bad, because the hash is not secret (it appears in the plan).
* I-a: Good, because unguessable and the working call stays valid. Bad, because a caller's own id is only a label.
  I-b: Good, because natural naming; bad, because a caller-chosen id is guessable. I-c: Good, because status quo; bad,
  because the schema says optional and the generated id is the unguessable one.
* L-a: Good, because it survives a restart and refuses an interrupted run. Bad, because `SyncWrites(false)` can lose
  the
  last write in a crash (then the id is `unknown_session`). L-b: Good, because simplest; bad, because a stdio restart
  loses the plan. L-c: Good, because less to hold; bad, because a review overnight expires.
* A-a: Good, because atomic (R9). A-b: Good, because simple; bad, because only with L-b. A-c: Bad, because not atomic
  (R9's planted break).
* G-a: Good, because the schema is enforced; bad, because an extra key that was ignored is refused. G-b: Good, because
  no behaviour change; bad, because the schema's `additionalProperties: false` is then untrue. G-c: Neutral.

## Open questions for the owner

None. Every question of the first draft is decided: the scope of the "trailing {}" clean-up and the
sanitiser questions in Decided 4 to 6, how the change reaches the server in Decided 7, the mismatch and the schema's
form in Decided 2 and 3, and the continuation's design in Decided 8 to 13. Decided 14 is the approval to proceed.

## More Information

### The drafting experiments (2026-11-17, scratch copy of `368ac6a`, nothing in the working tree)

* **R1, R3, R6 (first draft).** A Go test registers the handler as the binary does and captures the raw `tools/list`
  over the SDK's line-delimited transport on in-process pipes; with a proxied tool seeded as `align_tools` does.
  Static:
  18 tools, 11,833 bytes; seeded: 19 tools, 12,317 bytes.
* **R2.** A script parses the inventory: one union keyword in 18 tools.
* **R4.** The argument combinations of the current handler against the current schema.
* **R8.** The designed schema and a pure argument gate, 26 rows, through two validators; 0 disagreements. With the
  gate's `reject` check deleted, the `reject: true` alone row disagreed and the test failed.
* **R9.** The claim, on a scratch Badger store: 32 concurrent claims of one id, 200 rounds, one winner each time; a
  continuation racing a reject, 200 rounds, never both and never neither (continue won 13, reject won 187); a repeat
  claim refused with the prior state; an entry past its `expires_at` inside its store TTL reported as expired; an
  unknown id and an id past its store TTL both "not found". With the claim split into a read then a write (the removed
  design's shape), 32 of 32 won.

### Not verified

* Whether Grok accepts the S2 form: the owner's live step. Whether any other route accepts or refuses any root union
  (the Anthropic claim is another record's).
* Whether any provider refuses an object schema without `properties`, at the root or nested: the evidence step in the
  PLAN (D3.2). The history records the sanitiser's origin as a panic guard, not a provider refusal.
* Whether go-modernizer's `apply_vetted_edit` honours the placeholder plan hash.
* That gobble's rule leaves S2 unchanged: read from the draft's text; not implemented.
* The HTTP service transport's `tools/list` bytes, the stdio bridge's output, and the session-cache key on a real
  stdio connection.
* The MCP specification's own text on `inputSchema`.
* The owner's real call patterns in Claude Code.
* Whether older binaries ignore new key prefixes in the store.
* Whether any downstream server sends a union.
* Whether boot syncs reach every configured server in the owner's configuration.
* Why continuation was removed on 2026-05-01: the commit says what, not why. The owner was told and asked to say
  if they remember; none is recorded. The new design addresses the removed one's flaws (D2.6) but cannot address a
  reason it does not know.

### Records this bears on

* gobble-cli: `docs/decisions/0060-MADR-normalise-root-level-unions-in-mcp-tool-schemas.md`, its Q5.
* magic-cli-remote: `docs/decisions/0096-MADR-kilo-model-catalog-scope-and-order.md`, which worked around this tool
  for
  Kilo.
* This repository's code comments cite "MADR 0005" for the self-update release contract; that is mcplib's
  `0005-MADR-canonicalize-cli-self-update-in-mcplib.md`. No `0005` record existed here; this record takes the next
  free
  number by a whole-repository scan and is cited by its full filename.

### Related

*
  [0005-PLAN-make-execute-pipeline-schema-and-handler-agree.md](0005-PLAN-make-execute-pipeline-schema-and-handler-agree.md)

## Amendment 2026-11-17 (a): the Go toolchain lookup in `mcpGoBinary()` (Decided 15)

Nothing above is rewritten or removed. This amendment adds one decision item, found while reading the file this plan
already changes.

### Decided (the owner, 2026-11-17, relayed by the coordinator)

**Decided 15. The stale Go names go and the newest SDK wins.** The label was "Fix it in the 0005 plan: drop the
stale names, prefer the newest SDK (Recommended)". The question put to the owner:
`internal/handler/execute_pipeline.go:903-904` hard-codes two Go locations, `go1.26.5` and `go1.26.1` under the home
directory's `sdk` folder, as fallbacks when looking for a toolchain; they are stale and, as put, never reached
because a glob over `sdk/go*/bin/go` is tried first; but the glob is ordered as text, so with two SDKs installed
side by side the wrong one can win. As put: added to 0005 by dated amendment; the two dead names go, the glob's
result is ordered by version so the newest wins, and a test holds it.

### What the code does today (verified at `368ac6a`; the first statement of the problem was partly wrong)

`mcpGoBinary()` (E32) returns the Go binary used for the pipeline's build validation. Its candidate order:

| Order | Candidate | Where |
| :--- | :--- | :--- |
| 1 | the `MCP_GO_BIN_PATH` environment variable, if non-empty, returned without checking the file | `internal/handler/execute_pipeline.go:898-900` |
| 2 | every match of `<home>/sdk/go*/bin/go`, in the order `filepath.Glob` returns them | `:909-911` |
| 3 | `<home>/sdk/go1.26.5/bin/go`, `<home>/sdk/go1.26.1/bin/go` | `:903-904` |
| 4 | `<home>/.local/go/bin/go`, `<home>/go/bin/go`, `/usr/local/go/bin/go` | `:905-907` |
| 5 | the bare name `go`, left to `PATH` | `:917` |

The first candidate from 2 to 4 that is a regular file with an execute bit wins (`:912-916`). The glob's matches are
placed before the fixed list, so the two stale names (3) are reached only when the glob finds no executable. That much
of the first statement holds. What does not: **the glob returns names in ascending text order and the loop takes the
first executable, so the lexically smallest directory wins, which is the oldest SDK whenever text order and version order
disagree.** Measured (R10: a temporary home, fake executables, `mcpGoBinary()` called as it is):

| SDKs installed side by side | Picked today | Newest? |
| :--- | :--- | :--- |
| `go1.26.11`, `go1.27.4` | `go1.26.11` | no |
| `go1.26.1`, `go1.27.4` | `go1.26.1` | no |
| `go1.1.3`, `go1.10.5` | `go1.1.3` | no |
| `go1.26.9`, `go1.26.11` | `go1.26.11` | yes, by luck of the text order |
| `go1.9.0`, `go1.27.4` | `go1.27.4` | yes, by luck of the text order |
| `go1.27.4`, `go1.27rc1` | `go1.27.4` | yes, by luck of the text order |
| none | `go` | not applicable |

On the owner's Mac one SDK is installed, so nothing is wrong there today; the defect is latent.

* **What calls it, and what the binary does.** Only `validateShadowBuffer`, twice (E33): a pre-check
  `go build ./...` and the overlay build `go build -overlay … ./...` that validate staged mutations before any file is
  written, in `executeMutatorAST` (`:544`). No other stage and no downstream tool uses it: the proxied servers have their
  own environment (`MCP_GO_BIN_PATH` is also read by them, which is why the guides name it, E35). The Go version chosen
  matters to the outcome: `go build` reads the target module's `go` line, and a toolchain older than it either switches
  toolchain or fails; a failed validation aborts every staged mutation (the code fails closed). What the owner's
  `GOTOOLCHAIN` setting would do was not run (not verified).
* **Incidental, not decided here.** The pre-check at `:646` sets no working directory, so it builds the server's own
  working directory rather than the target; only the overlay build at `:654` sets `cmd.Dir`. Noted for the owner; this
  amendment does not change it.
* **Testability.** `homeDirOrDot()` (E34) reads `os.UserHomeDir()`, which is `$HOME` on Unix and `USERPROFILE` on
  Windows, so a test can redirect it only per operating system, and the fixed `/usr/local/go/bin/go` cannot be
  redirected at all. The two existing calls only execute the function without asserting (E36). A seam is needed: a
  function that takes the override, the home directory and the fixed fallbacks as arguments, called by `mcpGoBinary()`.
* **Windows.** Not changed here, and measured only by reading: the loop requires an execute bit, which Windows files do
  not report, and looks for `go` with no `.exe`, so on Windows the lookup always falls through to `go` on `PATH`
  (E32). Not run on Windows (not verified). The templates already set `MCP_GO_BIN_PATH` to a `go.exe` path (E35).
* **Documentation.** No guide describes the lookup order: `docs/guides/mcp-tools.md:82-83` and
  `docs/guides/client-integration.md:100-102` say only to set `MCP_GO_BIN_PATH` (E35); the function's own comment says
  "common SDK paths", which is stale.

### Options

* **N-a. Change nothing.** Good, because the owner has one SDK. Bad, because a second install makes the build
  validation pick the oldest.
* **N-b. Drop the two stale names only.** Good, because it removes dead code. Bad, because the ordering defect stays.
* **N-c. Drop the stale names and order the glob by version, newest first.** **Decided.** Good, because both findings
  are closed in one small function with a seam to test it.
* **N-d. Drop the glob and the stale names; keep the override, the three fixed paths and `PATH`.** Good, because the
  least code and no ordering rule. Bad, because it removes the only way a side-by-side SDK is found automatically.

### The rule for "newest" (N-c)

* A directory name parses when it is exactly `go` followed by `MAJOR.MINOR` or `MAJOR.MINOR.PATCH`, each a decimal
  number with no sign and no leading `+`; a missing patch is `0`. Versions compare numerically, field by field, so
  `go1.26.11` is newer than `go1.26.9` and `go1.27.4` is newer than `go1.26.11`.
* Candidates are tried newest first. A directory whose `bin/go` is not an executable regular file is skipped and the
  next newest is tried, as the loop does now.
* Names that do not parse (`go1.27rc1`, `go1.27beta1`, `gotip`, `go-old`, anything else) rank below every name that
  parses and are ordered among themselves by descending text, so the choice is deterministic and they stay reachable.
  A release candidate is therefore never preferred over a release; to use one the owner sets `MCP_GO_BIN_PATH`.
* Equal versions (`go1.27` and `go1.27.0`) tie; the tie is broken by ascending full path.
* **What does not change:** `MCP_GO_BIN_PATH` still wins and is still returned unchecked; the other fallbacks keep their
  order (`~/.local/go`, `~/go`, `/usr/local/go`) and come after the glob as now; the final fallback is still `go` on
  `PATH`; the function's name and signature stay (it is unexported); nothing exported changes; Windows behaviour is the
  same as today.

### Evidence rows added

| Id | Claim | Location | Mark |
| :--- | :--- | :--- | :--- |
| E32 | The function and its candidate order | `internal/handler/execute_pipeline.go:897`, `:898-900`, `:903-904`, `:905-907`, `:909-911`, `:912-916`, `:917` | read |
| E33 | The only callers, and the stage that uses the binary | `internal/handler/execute_pipeline.go:646`, `:654`, `:602`, `:544` | read |
| E34 | The home-directory lookup | `internal/handler/io_helpers.go:95-102` | read |
| E35 | What the guides and templates say: only the environment variable | `docs/guides/mcp-tools.md:82-83`; `docs/guides/client-integration.md:100-102`; `internal/config/templates.go:534` | read |
| E36 | The existing tests call the function without asserting | `internal/handler/execute_pipeline_extra_test.go:67-68`, `:90-91` | read |
| R10 | What the lookup picks with several SDKs installed (table above) | `gobinary_today.txt`, scratch | run |

### Question raised by this amendment

* **Q-W. Windows.** The SDK lookup never matches on Windows today (above).
  * **(a)** Leave it: Windows keeps finding `go` on `PATH` or `MCP_GO_BIN_PATH`. *Recommended:* the ruling is about
    the stale names and the ordering, and a Windows lookup would be untested here. Cost: the lookup stays dead there.
  * **(b)** Make it work: look for `go.exe` and do not require an execute bit on Windows. Cost: new behaviour on a
    platform this plan cannot run, tested only by the repository's Windows CI job.
  * **(c)** Remove the SDK glob on every platform (N-d). Cost: no automatic side-by-side lookup anywhere.

The plan proceeds with (a) unless the owner answers otherwise.

## Amendment 2026-11-26 (b): re-grounding at `34bf269`

This amendment records what the tree does now. It does not change Decided 1 to 15. It does not approve execution. The body's evidence index, except where this amendment says a citation still resolves, describes `368ac6a`.

**What 0013 took.** [0013-MADR-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md](0013-MADR-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md) adopted D2 and superseded D1 (S2) for the served schema. Its plan is `implemented`. At `34bf269` the continuation is in the handler, not a proposal:

* The argument gate decides the mode before an executor is in reach (`internal/handler/execute_pipeline.go:35-62`).
* A dry run stores a `PLANNED` session and appends the D2 disclosure (`internal/handler/execute_pipeline_session.go:36-107`).
* The served schema is flat. `internal/handler/inventory.go` contains no `anyOf`, `oneOf` or `allOf`. The `execute_pipeline` entry (`:399-445`) has seven properties, `additionalProperties: false` and `minProperties: 1`.
* `docs/guides/mcp-tools.md:73-84` states that contract.

D1's S2 form is therefore not the schema to build. Restoring the union would contradict 0013. The decision text above stays as the ruling of 2026-11-17.

**P3, assessed.** The session store this record's plan named is `internal/db/pipeline_session.go`. It matches the decided store: prefix `pipeline-session:` (`:28`), compressed values (`:116`), TTL of 24 hours plus one hour (`:30-34`, `:125`), `expires_at` in the value (`:77`), cap 50 with oldest-first eviction (`:25`, `:132-171`), nonce recorded at `RUNNING` and compared on read (`:213-214`, `:232-233`). Transitions use `UpdateWithRetryCtx` (`:121`, `:224`). This record said `UpdateWithRetry`. Both functions exist (`internal/db/badger.go:813`, `:856`). 0013 adopted the context form. That is not a gap. `OverwriteForTest` is compiled only with `magictoolstestseam` and refuses any status other than `PLANNED` (`internal/db/pipeline_session_testseam.go:1-22`). No production method writes a state outside its transition.

**What is still open, and still this record's.**

* **D3.1 had not landed at the re-grounding.** That sentence was true at the start of 2026-11-26. It is not true after the P7 execution the same day, recorded below. The old `sanitizeMap` retyping is gone. `TestSanitizeToolSchema_D31Table` holds the table. A `null` property's output is `{}`.
* **D3.2 is unchanged.** No owner probe result is recorded. Nested `properties: {}` stay until that evidence says removal is safe. The retyping fix does not wait.
* **D3.3 is unchanged.** Stored proxied schemas were sanitised before they were stored. A sync is still what refreshes them. Not verified against a live store in this amendment.
* **D3.4 is unchanged.** Pass a downstream union through. Nothing in this amendment revisits that.
* **Amendment (a) had not landed at the re-grounding.** It landed the same day, recorded in the PLAN's amendment (a) execution. `mcpGoBinary` calls `resolveGoBinary`. The stale names are not in the fallbacks. E32's old line numbers still do not describe HEAD; the function is the short call at the end of `internal/handler/execute_pipeline.go`, and the rule is in `internal/handler/go_binary.go`.

**A leftover, not a new decision.** `buildDryRunResult` still tells the caller to "call execute_pipeline again with dry_run=false" (`internal/handler/execute_pipeline.go:144`). The dry-run path calls that function and then appends the decided disclosure. The stale sentence is still in the result. It is not scheduled here.

**Forward path, as written at the re-grounding.** Do not rebuild the store, the gate, the continuation, or the S2 schema. Amendment (a) is separate and still open. P9's check for two schema branches is void. A live install remains the owner's.

### P7 execution — 2026-11-26

The owner approved execution the same day ("approved to proceed"). Tests were written against the unchanged tree and seen red, then the sanitiser was changed.

Red, before the change, `go test` of the named tests: `TestSanitizeToolSchema_SetsDefaultType` failed because an untyped property gained `type: object` and `properties: {}`; `TestSanitizeToolSchema_Raw` failed because a null property became `{type: object}`; five D3.1 rows failed the same way (untyped, null, empty object, root union, nested union); `TestSyncSchemaRefresh_RetypingChangeDoesNotSkip` failed because the saved schema still retyped. The rows the old function already matched (root type, root properties, object-typed property missing properties, `items: {}`, unknown keywords) passed.

The change: `internal/util/schema_util.go` walks the D3.1 table. A union is not written. An untyped property is not given a type. A null property becomes `{}`. Nested `properties: {}` are kept, because the D3.2 probe was not run. `internal/handler/handlers.go`'s `addTool` comment no longer says every nested property is re-assigned. That comment was outside the plan's file list; leaving it would have described the old retyping as current. No guide note existed to update.

Green, after the change, and again after the push gate of 2026-11-26: `make test` on `<linux-host>` (21 packages `ok`); `make vet`, `CGO_ENABLED=0 go test ./...`, and `make lint` v2.15.0 on `Ubuntu-24.04` (lint `0 issues.`); race of the four packages on darwin; native `go vet`, `go build`, and `CGO_ENABLED=0 go test ./...` on windows/amd64 and darwin/arm64; `gofmt`, tidy, `make check-records`, `make markdownlint`, and both installers. All exit 0. Not run: P8's plants, P9, and amendment (a). Not committed. Not pushed.
