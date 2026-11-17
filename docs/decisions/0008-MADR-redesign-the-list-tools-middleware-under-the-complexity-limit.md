---
status: accepted
date: 2026-11-17
decision-makers: repository owner
consulted: "the owner's ruling of 2026-11-17, relayed by the cross-repository coordinator;
0005-MADR-make-execute-pipeline-schema-and-handler-agree.md and its PLAN (committed); the staged
0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md; this repository at 310a541;
github.com/modelcontextprotocol/go-sdk v1.6.1 module source; the drafting experiments under More Information, run
in scratch copies of 310a541"
informed: "the cross-repository coordinator"
---

# Bring ListToolsMiddleware under the cognitive-complexity limit with a small type, behaviour unchanged

## Context and Problem Statement

golangci-lint v2.15.0, the linter that
0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md moves CI to, reports one finding that
CI's current v2.13.1 does not: gocognit, `internal/handler/list_tools_handler.go:19:1`,
`(*OrchestratorHandler).ListToolsMiddleware`, cognitive complexity 52 against the limit of 50 that the repository's
own `.golangci.yml:32-33` sets (E1). Every `tools/list` response a client receives passes through this function (E2,
E4). The owner's rules rule out raising the limit.

### Decided (the owner, 2026-11-17, relayed by the coordinator; the label is quoted only as a label)

1. **A fuller redesign of the middleware, under its own record.** As put to the owner: "Turn the closure into a
   small type with methods. Cleaner long term and larger; the Go move waits for it." The other options put were
   extracting the two loops into helpers inside the Go-pins record, and doing the extraction inside the 0005 plan.
   The redesign is decided in principle. How is this record's to work out, and the owner's where it is a real choice
   (Open questions).

### Decided, second ruling (the owner, 2026-11-17, relayed by the coordinator; each label quoted only as a label)

An addition of 2026-11-17: the record was `proposed` until now and this is the owner's answer to its four open
questions and to how the work proceeds. Nothing above this heading changes; the Open questions under More Information
stay as they were asked and are answered here.

* **Q1, the design.** The label was "E: a small immutable type built once, one method per layer (Recommended)". As
   put: design E, the layers type built at wiring and never written afterwards, with the layers as its methods.
* **Q2, the defects.** The label was "Preserve behaviour; fix only the data race here; separate records for the rest
   (Recommended)". As put: the redesign is a pure restructuring that the existing tests can verify, plus the read
   lock for F3 (invisible to clients) and the stale comment (F8); F1, F2, F4 and F5 each get a follow-up record,
   because fixing F1 changes what every stdio client sees. Those four records are named here as future work and are
   not drafted in this record: one for the session-id lookup (F1), one for what a panic returns (F2), one for the
   dropped cursor and metadata (F4), one for the unsanitised, reduced fallback (F5). F6, F9 and F10 stay as recorded.
* **Q3, the tests.** The label was "All 23, as their own commit before the redesign (Recommended)". As put: the
   characterisation rows L01 to L21, E-1 and E-2 land first as a tests-only commit, seen passing on the unchanged
   code. The F3 race test is not among them: it fails on the unchanged code, which is the defect, so it lands with
   the lock (PLAN, P2).
* **Q4, `CallToolMiddleware`.** The label was "Leave it until it is next edited (Recommended)". As put: it stays at
   exactly 50 and gets its own record when someone next edits it (F9).
* **How it proceeds.** The label was "Approve the whole plan, committed and pushed as each phase passes
   (Recommended)". As put: this record is marked accepted; each phase is committed and pushed to `main` on this
   approval; then the 0006 change is committed as already approved; nothing is installed or restarted on the owner's
   running server, and no tag is made. This is the owner's approval of the PLAN, and the authority for commits and
   pushes to this repository's `main` for this plan only. **Superseded in part on 2026-11-17 by the owner, after an
   account change:** no agent and not the coordinator commits or pushes anything until the owner says otherwise; work
   is staged by path and the owner makes the commits and pushes from the coordinator's staged list.

**Correction carried in, 2026-11-17.** 0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md
(its F2) says the closure's `baseRes, err := next(...)` shadows the named result `err`. It does not: it assigns the
named result (F7 above; the proof program and the plant on `err = nil` are in R4). This note is the only place the
correction is made; 0006 is not edited by this record.

### Sequence

This record's code lands first, on the current Go pins (`go.mod:3`, `go 1.26.6`) and with CI's current linter, and it
must also pass under v2.15.0. Then the Go-pins change (0006), which cannot be pushed before this. Then the code
phases of 0005-PLAN-make-execute-pipeline-schema-and-handler-agree.md, which cite this function as evidence.

### What the function does, in one paragraph

`ListToolsMiddleware` returns a closure registered as a receiving middleware (E4). For any method other than
`tools/list` it calls `next`. For `tools/list` it installs a recover block, asks `next` for the tools the SDK
registered (the base layer), falls back to the static inventory when that yields nothing usable, then, once the
backplane is synced, appends the tools held in the calling session's cache that are not already listed (the dynamic
layer), and returns a new result with no error (E2). The behaviour table below is the contract this record preserves.

### Evidence index

All paths and lines are at `310a541` (`HEAD` when this record was written) unless a module path says otherwise.

| ID | Fact | Where | Kind |
| :--- | :--- | :--- | :--- |
| E1 | The limit is 50 for gocognit and 40 for gocyclo | `.golangci.yml:32-33`, `:49` | read |
| E2 | The middleware: method gate, recover, base, fallback, session merge, return | `internal/handler/list_tools_handler.go:18-102` (gate `:21-23`, recover `:26-40`, base `:44-52`, fallback `:55-68`, merge `:73-96`, return `:98-100`) | read |
| E3 | `CallToolMiddleware`, the other function in the file, scores 50 under v2.15.0 | `internal/handler/list_tools_handler.go:105` | run (R1) |
| E4 | Registered as the first receiving middleware; the SDK composes them so the first registered is outermost | `internal/handler/handlers.go:187`; `go-sdk@v1.6.1/mcp/shared.go:88-92`, `mcp/server.go:1352-1356` | read |
| E5 | Handler state the middleware reads: `InternalTools`, guarded by `toolsMu` where it is written; `ActiveSessions` (a `sync.Map`) | `internal/handler/handlers.go:46`, `:49`, `:53`, `:146-148` | read |
| E6 | `IsSynced` is an atomic bool | `internal/client/manager.go:69` | read |
| E7 | `lruCacheFromAny` returns a nil cache, with a warning, when the stored value has the wrong type | `internal/handler/io_helpers.go:77-84` | read |
| E8 | The session cache: capacity split `capacity/10` small queue; `Values()` returns the main queue then the small queue under a read lock; a key never hit leaves the small queue when the next new key arrives | `internal/util/s3fifo.go:39-47`, `:108-125`, `:128-140`, `:158-178` | read, run (R4) |
| E9 | The handshake stores a cache of 20 under the session id, `""` mapped to `active-session` | `cmd/mcp-server-magictools/main.go:397-405` | read |
| E10 | `align_tools` sanitises the schema, maps `""` to `active-session`, creates a cache of `SessionLRUSize` (default 30) when none exists, and adds the tool | `internal/handler/align_tools_helpers.go:217-229`; `internal/config/config.go:615-616` | read |
| E11 | `safeSessionID` exists because `ServerSession.ID()` can nil-deref; the middleware does not use it | `internal/handler/proxy_handlers.go:90-95` | read |
| E12 | Registration sanitises each internal tool once | `internal/handler/handlers.go:219` | read |
| E13 | The telemetry counter the recover block increments | `internal/telemetry/telemetry.go:22-25` | read |
| E14 | SDK: `GetSession()` returns the `*ServerSession` field as a `Session` interface, so a nil pointer is a non-nil interface; `ID()` dereferences the connection; stdio and in-memory transports report `""`; the base `tools/list` pages at 1000 | `go-sdk@v1.6.1/mcp/shared.go:477-481`, `:512`; `mcp/server.go:1160-1165`, `:729-741`, `:36`; `mcp/transport.go:123-125`, `:441` | read |
| E15 | The binary's stdio mode serves over `mcp.IOTransport` | `cmd/mcp-server-magictools/main.go:487-490` | read |
| E16 | The tests that touch the middleware | `internal/handler/list_tools_handler_test.go:13-73`; `internal/handler/list_tools_concurrency_test.go:17-66`; `internal/handler/handler_exhaustive_test.go:44-47` | read |
| E17 | 0005 cites this function as evidence | `0005-MADR-make-execute-pipeline-schema-and-handler-agree.md:132`, `:296` | read |
| E18 | CI installs the linter at v2.13.1 and runs `make lint`; the Makefile's lint target | `.github/workflows/ci.yml:86-90`; `Makefile:47-53` | read |
| E19 | A stale comment about the middleware | `internal/handler/handlers.go:161-163` | read |
| R1 | Cognitive complexity per function, v2.15.0, every design | `cog_*.txt`, `measure_d*.txt`, scratch | run |
| R2 | Baseline: coverage, lint under both linters, `-race -count=20` | `cover_base.out`, `lint_base_*.txt`, `baseline_race.txt`, scratch | run |
| R3 | The characterisation tests and their 24 planted breaks | `list_tools_characterization_test.go`, `list_tools_register_test.go`, `plants_current.txt`, scratch | run |
| R4 | Defect probes (session id, panic, race) | `char_exp*.txt`, `probe_race_*.txt`, `shadowproof.txt`, scratch | run |
| R5 | Designs A to E prototyped and measured | `designs/head_*.go.txt`, `measure_d*.txt`, `plants_E.txt`, `bench_results.tsv`, scratch | run |

### The behaviour table

What a client, or the code around the middleware, sees for each condition at `310a541`. A bare `:N` in a row is a
line of `internal/handler/list_tools_handler.go`. Every row is held by a
characterisation test written for this record (R3) and seen passing on the current code. "Tested today" is whether
the three existing test files assert it (E16): **yes**, **partly** (a weaker property is asserted), **executed**
(the statements run, nothing asserts the outcome) or **no**.

| Row | Condition | Result | Tested today |
| :--- | :--- | :--- | :--- |
| L01 | method is not `tools/list` | `next`'s result and error returned unchanged; no recover block is installed (`:21-23`) | partly: `list_tools_handler_test.go:54-59` asserts a nil error only |
| L02 | method is not `tools/list`, `next` panics | the panic propagates to the caller | no |
| L03 | `tools/list` | `next` receives the same `ctx`, method and request | no |
| L04 | base ok, backplane not synced | base tools only, same pointers, the session cache is not read (`:73-75`) | executed: `list_tools_concurrency_test.go` lists an empty base and asserts no error |
| L05 | base ok, synced, a cache exists for the session | base tools first, then the cache's `Values()` order (main queue, then small queue), the cached pointers as they are | partly: `list_tools_handler_test.go:31-52` asserts one session tool is present |
| L06 | synced, no cache for the session id | base tools only | no |
| L07 | synced | session tools whose name is already listed, or already added, are dropped; nil cache entries are skipped (`:83-94`) | no |
| L08 | a nil entry in the base result | kept in place; only the dedup set ignores it (`:84-88`) | no |
| L09 | synced, cache holds N entries | all N are appended; the middleware has no cap of its own (`:89-94`) | no |
| L10 | request has a non-nil session | the id is `req.GetSession().ID()`; `""` is looked up as `""`, not as `active-session` (`:77-78`) | no: `:78` is uncovered |
| L11 | request nil, or its session interface nil | the id is `active-session` (`:76`) | partly: nil request only, `list_tools_handler_test.go:38` |
| L12 | `next` returns an error | the error is dropped; the static inventory is served; one warning is logged (`:55-56`) | no |
| L13 | fallback | one tool per non-nil inventory entry with `Name`, `Description`, `InputSchema` only; the schema value is shared, not copied, and not sanitised (`:57-67`) | executed: `handler_exhaustive_test.go:44-47`; `list_tools_handler.go:60` (the nil skip) is uncovered |
| L14 | what triggers the fallback | a nil result, a result that is not a `*mcp.ListToolsResult`, or a result with `Tools == nil`; an empty non-nil slice does not (`:47-55`) | no |
| L15 | fallback and synced | the inventory tools, then the session tools | no |
| L16 | any success | a new `*mcp.ListToolsResult` holding only `Tools`: the base result's `NextCursor` and `Meta` are dropped (`:98-100`) | no |
| L17 | `next` panics | recovered: counter incremented, an error log with the panic value and stack, an empty non-nil list, nil error (`:26-40`) | partly: `list_tools_handler_test.go:61-72` asserts nil error and a non-nil result |
| L18 | a panic in the session merge (a wrong-typed cache makes `Values()` run on a nil cache, E7) | the same recovery: the base tools already gathered are discarded, the client sees an empty list | no |
| L19 | a panic after `next` returned an error | the error is cleared, an empty list is returned (`:38`) | no |
| L20 | request with a nil `*ServerSession` inside a non-nil interface, synced | `ID()` panics (E14); recovered; empty list | no: the concurrency test passes only because the backplane is never synced there |
| L21 | any call | the session cache is not written | no |
| E-1 | a real session over an in-memory transport (as stdio: `ID()` is `""`, E14) with the tool held under `active-session` | the tool is not listed; held under `""` it is listed | no |
| E-2 | `Register`, `RegisterPipelineTools`, `RegisterPrompts` as the binary calls them, then a session tool | the 18 registered tools are unchanged when a session tool is added; the session tool is emitted byte for byte as cached; a value the cache replaces is what the next call emits | no |

The existing tests catch 6 of the 24 planted breaks of R3 (a changed default session id, an inverted sync gate, a
wrong cache, a removed recover, a recover that returns an error, a wrong session key); they miss 18, among them the
dedup, the nil skips, the fallback's triggers, the base order, the dropped error, the re-sanitising of emitted tools
and writing to the session cache. Statement coverage of the function is 95.0%; the uncovered statements are `:60`
and `:78` (R2). Coverage counts statements run, not outcomes asserted.

### State and concurrency

* **Shared across calls and guarded:** `Registry.IsSynced` (an atomic, E6); `ActiveSessions` (a `sync.Map`, E5); each
  session cache (a mutex of its own, E8); `telemetry.PanicsRecovered` (an atomic, E13).
* **Shared across calls and not guarded by the middleware:** `h.InternalTools`. `OnConfigReloaded` replaces it under
  `toolsMu.Lock` (`handlers.go:146-148`); the fallback reads it at `list_tools_handler.go:57-58` without
  `toolsMu.RLock`, as does registration (`handlers.go:205`). The three readers in `align_tools` take the read lock
  (E5). A race probe is in R4 and under Findings, F3.
* **Per call:** the base result, the gathered slice, the dedup map, the request and its session.
* **What `list_tools_concurrency_test.go` proves:** 100 concurrent `tools/list` calls with an empty base result,
  while another 100 goroutines churn the registry's state (E16). The backplane is never synced there and the
  request's session is a nil pointer, so the calls take the not-synced path of L04 only: the test shows only that
  this path does not race. It does not exercise the fallback, `ActiveSessions`, a cache, or the
  merge.
* **What a method-receiver type may hold:** the handler pointer and `next`, set once at wiring and never changed
  (design E). A type that holds per-call state (design A) must be created per call. Nothing may be memoised across
  calls: 0005 relies on every call reading the cache afresh (below).
* **Baseline:** `go test -race -count=20 -timeout 20m ./internal/handler/...` on a pristine copy of `310a541`, run
  through the heavy-run queue: exit 0, 313 s, no data race reported (R2).

### What 0005 relies on, and how this record preserves it

0005 cites `list_tools_handler.go:70-96` as evidence that, after `align_tools`, "the middleware appends up to 20
recently discovered downstream tools to `tools/list`, sanitised by the same function" (E17). Its code phases depend on
the following, and each is a row above held by a test that this record's redesign must pass unchanged:

1. **The middleware sanitises, retypes and copies nothing.** The emitted tool is the pointer the cache holds (L05,
   E-2). Sanitising happens in two other places: at registration for the 18 inventory tools (E12) and in
   `align_tools` before the tool is cached (E10). A change to the sanitiser (0005, D3.1) therefore reaches
   `tools/list` through those two paths, and the middleware passes the result through. A planted break that
   re-sanitises session tools on the way out fails E-2 (base tools are compared with themselves there, so a re-sanitise
   of those that leaves them unchanged is not detectable and is not claimed).
2. **The middleware keeps no copy.** A cache value replaced by a later `align_tools` call (what 0005's D3.3 refresh
   leads to) is what the next call emits (E-2). No design here holds state across calls; design E's type is immutable.
3. **Order and dedup:** base tools first in registration order, session tools after in cache order, names unique
   (L05, L07).
4. **Which session the tools belong to.** Over the SDK's line-delimited transports, which is what the binary's stdio
   mode uses (E15) and what 0005's emitted-`tools/list` helper uses (the in-process pipes of its P2 step 1), the
   session id is `""`, and the middleware looks the cache up under `""` (L10, E-1). `align_tools` and the handshake
   store theirs under `active-session` (E9, E10). So a tool seeded under `active-session` is not listed over those
   transports (F1). This record preserves that as it is; if the owner decides otherwise (Q2), 0005's seeding key
   changes with it.
5. **The fallback path emits the inventory schema unsanitised** (L13, F5). 0005's schema change to `execute_pipeline`
   reaches a client sanitised on the base path and raw on the fallback path. Preserved; named so 0005's tests do not
   assume otherwise.
6. **Citations.** The redesign moves the lines 0005 cites. This record's docs step amends E11 of the 0005 MADR (its
   row at line 296 and the sentence at line 132, `0005-MADR-make-execute-pipeline-schema-and-handler-agree.md`) to the
   new locations, as a dated annotation, in the same commit as the code
   (PLAN, P2). The ordering above makes the amendment before 0005's code phases.

### Documents that describe the middleware, and what the redesign makes false

Searched at `310a541`: the README, `docs/guides/`, `GEMINI.md`, and every doc comment and comment in `internal/` and
`cmd/` that names `ListToolsMiddleware` or `tools/list`.

* No guide and no README describe the middleware. The only guide sentence about middleware is about prompts and
  namespaced capabilities, `docs/guides/mcp-tools.md:100-103`; it stays true.
* The doc comment at `internal/handler/list_tools_handler.go:18` ("provide robust recovery and aggregation") stays
  true; the redesign adds doc comments to the new type and methods (PLAN, P2).
* `internal/handler/inventory.go:5` says the static inventory "is used to guarantee tool integrity during tools/list
  responses". That is the fallback layer; it stays true.
* `internal/handler/handlers.go:161-163` is already false (F8); the redesign rewrites it only if Q2 is answered 1.
* **Made false by the redesign:** the line range 0005 cites for E11 (`list_tools_handler.go:70-96`) and the sentence
  that cites it (E17). Corrected by a dated annotation in P2, not rewritten.
* **Already false, in a record this one does not edit:** 0006's F2 says `err` is shadowed (F7).

### Findings beyond the complexity

Each is an open question for the owner where fixing it changes behaviour (Q2).

* **F1. The middleware looks the session cache up under `""` where everything else uses `active-session`.** Over
  stdio, `ID()` is `""` (E14, E15), so the cache the handshake and `align_tools` fill under `active-session` (E9, E10)
  is never read: proxied tools never appear in `tools/list` over stdio. Shown with a real in-memory session (E-1); the
  handshake and `align_tools` map `""` and the middleware does not, and `safeSessionID` (E11) exists for the same
  reason. Over the streamable HTTP handler the connection returns its own session id (`mcp/streamable.go:706-708`; the
  binary's two handlers, `cmd/mcp-server-magictools/main.go:656-674`, do not set `Stateless`; not exercised here). Not
  observed against the owner's running server (not touched). Fixing it adds up to 20 to 30 tools to every stdio
  client's list: a behaviour change.
* **F2. A panic anywhere discards the base tools too.** `result` is only assigned by the final return, so the recover
  block always returns an empty list (L17 to L20); a nil session pointer (L20) or a wrong-typed cache (L18) therefore
  empties the whole list, including the 18 registered tools, where returning the tools gathered so far would not. The
  `if result == nil` guard at `list_tools_handler.go:33` is always true: unreachable otherwise. Fixing it changes what
  a client sees after a panic.
* **F3. The fallback reads `h.InternalTools` without `toolsMu`**, which `OnConfigReloaded` writes under the lock
  (E5). Probe: one goroutine calling `OnConfigReloaded` and four listing with the fallback taken; result in R4
  (below). Fixing it (a read lock and a copy of the slice header) changes nothing a client sees; the probe fails on
  the HEAD closure and passes on the fixed prototype (R4).
* **F4. The result drops the base result's `NextCursor` and `Meta`** (L16). The base handler pages at 1000 tools
  (E14); the server registers 18, so this is latent. Fixing it changes the result only if a page ever overflows.
* **F5. The fallback emits raw inventory schemas** while registration emits sanitised ones (L13, E12). It also drops
  every field of an `mcp.Tool` except name, description and schema. Fixing it changes the fallback's output.
* **F6. "Max 20" is not the middleware's.** It has no cap (L09). The bound is the cache's capacity: 20 from the
  handshake (E9), `SessionLRUSize`, default 30, for a cache `align_tools` creates (E10), and the cache admits only
  `capacity/10` never-hit entries at a time (E8: three single adds of a new key leave two held, R4). The `+20` in
  `make([]*mcp.Tool, 0, len(h.InternalTools)+20)` at `list_tools_handler.go:57` is a capacity hint. Out of scope;
  recorded because the comment at `list_tools_handler.go:71-72` and 0005's sentence say 20.
* **F7. The recover block's `err = nil` is load-bearing, and `baseRes, err := next(...)` does not shadow.** The brief
  this record was drafted from and 0006's F2 say it shadows the named result. It does not: Go's short variable
  declaration re-uses a variable declared earlier in the same block, and a function's parameters and results are in
  the function body's block, so `list_tools_handler.go:45` assigns the named result (R4, `shadowproof.txt`: same
  address, same value at the recover). The consequence holds: a panic after `next` failed would otherwise return that
  error (L19, plant P01). Under design E `err` is never assigned before the recover, so the line becomes an explicit
  nil, not a repair.
* **F8. A stale comment** at `handlers.go:161-163` names `h.mgr.UpdateInternalTools`, which does not exist (E19).
* **F9. `CallToolMiddleware`, in the same file, scores exactly 50** (E3, R1): any edit to it trips the limit. Not in
  this record's scope (Q4).
* **F10. v2.15.0 reports one more finding in the package**, `internal/handler/pipeline_tools.go:161:33` (gocritic),
  which is 0006's F1 and not this record's. Nothing else in `internal/handler` (R2).

## Decision Drivers

* **Behaviour preserved exactly.** `tools/list` is the path every client takes first; the table above is the
  contract, held by tests that pass on the current code before anything is changed.
* **Complexity with headroom.** The limit is 50. A redesign that lands at 49 is back over it at the next edit; the
  gocognit score of every function should stay well below it.
* **Concurrency safety.** Concurrent calls must share nothing mutable that is not already guarded.
* **0005's reliance.** The six points above must hold, held by a test.
* **Smallest surface.** `ListToolsMiddleware`'s signature, its registration (`handlers.go:187`) and the exported
  surface of the package stay as they are.
* **A change the owner can read in one sitting.** One file's function replaced; tests added; no behaviour decision
  hidden in a refactor.

## Considered Options

* **A.** An unexported type holding the state of one call, one method per layer; the exported function only wires it.
* **B.** A pipeline of small `func(ctx, *state) error` stages run in order.
* **C.** Three middlewares composed once inside `ListToolsMiddleware`: recover outside, session merge inside it, base
  with fallback innermost. (Registering them separately in `Register` is possible, E4, but changes `Register`.)
* **D.** Extract the two loops into helpers only (the option the owner did not choose; the baseline to compare
  against).
* **E.** An unexported type built once at wiring, holding only the handler and `next`, one method per layer; the
  gathered tools travel as arguments and results. (Found while prototyping A.)

## Decision Outcome

Recommended option, pending the owner's answer to Q1: **E**, because it meets the ruling's words (a small type with
methods, the exported function only wiring it), scores at most 9 per function (headroom 41), keeps every row of the
behaviour table, adds no allocation per call, and has a type that is immutable after wiring, so it cannot hold
per-call state by mistake; its layers are plain methods a test can call directly.

Sketch (the prototype, R5, is in the scratch folder):

```go
func (h *OrchestratorHandler) ListToolsMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
    return (&listToolsLayers{h: h, next: next}).handle
}

type listToolsLayers struct { h *OrchestratorHandler; next mcp.MethodHandler }

func (l *listToolsLayers) handle(ctx context.Context, method string, req mcp.Request) (result mcp.Result, err error)
func (l *listToolsLayers) base(ctx context.Context, method string, req mcp.Request) []*mcp.Tool
func (l *listToolsLayers) staticInventory() []*mcp.Tool
func (l *listToolsLayers) mergeSession(ctx context.Context, req mcp.Request, tools []*mcp.Tool) []*mcp.Tool
func listToolsSessionID(req mcp.Request) string
func recoveredListTools(r any) mcp.Result
```

### Measured results (R1, R5)

gocognit as golangci-lint v2.15.0 reports it for every function in the redesigned part of the file, with a
temporary configuration that lists every function (`min-complexity` 1); baseline `ListToolsMiddleware` 52,
`CallToolMiddleware` 50. "Headroom" is 50 minus the largest score among the designed functions.

| Design | Cognitive complexity per function | Largest | Headroom | Existing tests unchanged | Race, count 20 | Exported surface | Per call, 18 base + 20 session tools |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| baseline (HEAD) | `ListToolsMiddleware` 52 | 52 | -2 | pass | pass (whole package, R2) | n/a | 2415 ns, 4536 B, 13 allocs |
| A per-call type | wiring 2, run 2, loadBase 4, staticInventory 3, mergeSession 9, sessionID 2 | 9 | 41 | pass | pass, 0 races | identical | 2489 ns, 4536 B, 13 allocs |
| B stages | wiring 10, stageBase 3, stageStaticInventory 4, stageMergeSession 11 | 11 | 39 | pass | pass, 0 races | identical | 2505 ns, 4616 B, 14 allocs |
| C composed | recoverListTools 5, baseWithFallback 9, staticInventory 3, mergeSessionTools 23 | 23 | 27 | pass | pass, 0 races | doc comment only | 2509 ns, 4536 B, 13 allocs |
| D helpers only | wiring closure 16, staticInventoryTools 3, mergeSessionTools 11 | 16 | 34 | pass | pass, 0 races | identical | 2455 ns, 4536 B, 13 allocs |
| E layers type | handle 3, base 4, staticInventory 3, mergeSession 9, listToolsSessionID 2 | 9 | 41 | pass | pass, 0 races | identical | 2587 ns, 4536 B, 13 allocs |

Notes on the table:

* "Existing tests unchanged": the whole `internal/handler` package passes with no existing test file touched, and the
  23 characterisation rows pass on every design (R5).
* "Race, count 20": `go test -race -count=20 -timeout 20m` over the tools/list tests (the three existing files, the
  characterisation rows and the two end-to-end tests), through the heavy-run queue. The whole-package race run is a
  gate of the PLAN, not repeated per design.
* Latency and allocations: a scratch benchmark, five interleaved rounds of 300 ms each, medians. The machine was
  shared with other agents' heavy runs, so the nanoseconds are indicative only: every design is within about 10% of
  the baseline, which is inside the noise seen between repeated rounds of the same tree. The allocation counts are
  exact: only B allocates more (its state struct), one allocation and 80 bytes. The middleware runs once per client
  `tools/list`, not per tool call. Not measured: a real client's end-to-end latency.
* Lint: every design passes the repository's `.golangci.yml` for `./internal/handler/...` under CI's current setup
  (go1.26.6 and v2.13.1, 0 issues) and has no finding of its own under v2.15.0 on go1.27.4 and on go1.26.6; the one
  v2.15.0 finding left in the package is `pipeline_tools.go:161:33` (F10). gocyclo (limit 40) reports none. `version`
  line of the linter used: "golangci-lint has version 2.15.0 built with go1.27.4" (and the same version built with
  go1.26.6 for the go1.26.6 runs); CI's current: "golangci-lint has version 2.13.1 built with go1.26.6".
* Coverage of the redesigned file's new functions is 100% for A, D, E, 96.2% for C (an unreachable type-assertion
  branch in `mergeSessionTools`), and 91.7% for B (its stage-error branch can never be taken: no stage returns an
  error).
* Under v2.13.1 built with go1.27.4 the baseline fails to type-check with the standard library's export-data error
  (13 errors, the problem 0006 fixes); that is the reason for the go1.26.6 runs. The designs were not run that way.

## Consequences

* Good, because the file's largest function drops from 52 to 9, and the whole `tools/list` path is 100% covered by
  tests that assert outcomes, where today 18 of 24 plausible breaks pass the suite.
* Good, because the recover, base, fallback and merge layers become separate, directly testable functions, and the
  immutable type cannot accumulate per-call state.
* Good, because the 0005 reliance is held by a test (E-2) that fails if the middleware ever re-sanitises, copies or
  memoises.
* Neutral, because `err = nil` in the recover block stays as an explicit nil (F7); the sub-expression that made it
  load-bearing is gone.
* Bad, because the new test files are larger than the code they protect (about 650 lines against about 125), and
  they pin behaviours the owner may later call defects (F1, F2, F4, F5); a later fix changes the named rows on
  purpose.
* Bad, because `CallToolMiddleware` stays at 50 (F9); the next edit to it needs its own change.
* Bad, because the dropped dead `if result == nil` guard is a change in the text, although never in behaviour: it is
  provable from the code (the named result is assigned only by the final return), and an independent review is asked
  to check the claim (PLAN, P5).

### Confirmation

1. The 23 characterisation rows (R3) pass on `310a541` before the redesign is written, each seen to fail under a
   planted break of the current code (24 plants, R3), and pass unchanged after it.
2. No existing test file is modified in the redesign's commit (`git diff --stat` names `list_tools_handler.go`, the
   docs, and nothing under an existing `*_test.go`).
3. A planted break per new method and per row on a scratch copy of the redesign (design E: 24 plants, all fail a
   named row, R5), seen to fail.
4. gocognit under v2.15.0 for every function in the file, with the temporary all-functions configuration, at most 9
   in the redesigned code.
5. `make lint` with v2.13.1 on the current pins (Go 1.26.6) and with v2.15.0 (Go 1.26.6 and 1.27.4), whole module,
   through the heavy-run queue.
6. `go test -race -count=20 -timeout 20m ./internal/handler/...` through the heavy-run queue, exit 0.
7. Cross-builds for linux/amd64, darwin/arm64, windows/amd64 compile.
8. An independent review of the diff before commit.

## Pros and Cons of the Options

### A. Per-call state type

* Good, because it is the smallest reading of the ruling and scores at most 9.
* Good, because the layers read as steps over one struct.
* Neutral, because the per-call struct did not add an allocation in the benchmark (it does not escape).
* Bad, because the state lives in a struct that carries a `context.Context` field, which Go style discourages, and
  that must be created per call to be safe; a future edit that stores it on the handler would be a data race.

### B. Pipeline of stages

* Good, because adding a layer is adding a function to a list.
* Bad, because `func(ctx, *state) error` has nothing to return an error for: every stage swallows its failures by
  design (the base error becomes the fallback), so the error result is vestigial and its branch is dead code (91.7%
  coverage).
* Bad, because it is the only design that allocates more per call, and its wiring function scores 10 for the
  runner loop and the recover together.
* Bad, because the static fallback is a separate stage that must test "did the last stage leave the tools nil", an
  ordering dependency that the type system does not express.

### C. Three composed middlewares

* Good, because each layer is an `mcp.Middleware`, the SDK's own idiom, and the chain allows it (E4).
* Good, because no state is shared at all; each layer is a closure.
* Neutral, because composing inside `ListToolsMiddleware` keeps the registration at `handlers.go:187`; registering
  the pieces separately would change `Register`, and the recover layer must stay outermost.
* Bad, because the merge layer scores 23, leaving 27 headroom; the loops stay nested in one closure.
* Bad, because the layers hand each other a `mcp.Result` and re-assert `*mcp.ListToolsResult`, adding an unreachable
  branch, and each layer repeats the method check.
* Bad, because three new methods and a constant are added to the exported `OrchestratorHandler`'s method set.

### D. Helpers only (baseline for comparison, not a candidate)

* Good, because it is the smallest diff and scores 16 at worst (headroom 34).
* Bad, because the closure keeps the recover, the base call and the named-result handling, so `err = nil` stays
  load-bearing (F7) and the function stays the place a next edit lands; the owner did not choose it.

### E. Layers type built once

* Good, because at most 9, immutable after wiring, no per-call allocation beyond the baseline, layers directly
  testable, and `err` is never assigned before the recover.
* Neutral, because it adds one unexported type, four unexported methods and two functions to the package.
* Bad, because `mergeSession` takes and returns the slice, which a reader may take for a copy; the doc comment says
  the slice is extended in place.

## More Information

### Open questions for the owner

In the order to answer them. Each has three options, the recommended one first.

**Q1. Which design?**

1. **E, the layers type built once (recommended).** Cost: a new unexported type, four methods, two functions; at
   most 9 per function.
2. **A, the per-call state type.** Cost: the same size; a `context.Context` field in a per-call struct; the same
   headroom.
3. **C, three composed middlewares.** Cost: the merge layer scores 23, three methods on the handler's method set.
   Nothing here is worse in behaviour; it is the least headroom of the three.

B and D are measured and not offered: B has a vestigial error return and a dead branch, D is the option already
declined.

**Q2. What happens to the defects found (F1 to F8)?**

1. **Preserve every client-visible behaviour; take only F3 (the unlocked read, invisible to clients) and F8 (the stale
   comment) here; one follow-up record each for F1, F2, F4 and F5 on the owner's word (recommended).** Cost: the
   tests pin F1, F2, F4, F5 as they are; the F3 change is three lines in design E (prototyped as design F, R5:
   same scores, race probe in R4). Consequence of this answer: stdio clients keep not seeing proxied tools in
   `tools/list` until the follow-up lands.
2. **Preserve all of it, fix nothing, including F3.** Cost: the refactor is purely mechanical and the race at F3
   stays, with a record of it.
3. **Fix F1 and F3 here.** Cost: the redesign changes what stdio clients see (up to 20 to 30 more tools in every
   list, and 0005's seeding key and tests change with it); a bigger review; the Go-pins change waits longer.

**Q3. Which uncovered behaviours get tests in this record?**

1. **All 23 rows, as a separate commit before the redesign (recommended).** Cost: about 650 lines of test; the
   existing tests catch 6 of 24 planted breaks, these catch 24.
2. **Only the rows no current test asserts (L02 to L04, L06 to L10, L12 to L16, L18 to L21, E-1, E-2).** Cost: fewer
   lines, but L01, L05, L11 and L17 stay at their weaker current assertions, and the redesign could regress them
   unseen.
3. **None beyond the existing three files.** Cost: the redesign would be checked by tests that miss 18 of 24 breaks,
   including every behaviour 0005 relies on except the session key; not recommended.

**Q4. `CallToolMiddleware` is at exactly 50 in the same file (F9). What now?**

1. **Leave it; it gets its own record when someone next edits it (recommended).** Cost: any edit trips the limit
   until then.
2. **Extract its routing in a later record of its own, now.** Cost: another pair, a function with 46.5% coverage
   needing its own characterisation first.
3. **Extract it here, in the same commit.** Cost: widens the review of a path this record's evidence does not cover
   and drops the characterise-first rule for it.

### The drafting experiments (2026-11-17, scratch copies of 310a541, nothing in the working tree)

* **R1.** Cognitive complexity of every function, golangci-lint v2.15.0, from a configuration with `gocognit`
  `min-complexity` 1; baseline and designs A to F.
* **R2.** Baseline on a `git archive` of `310a541`: statement coverage of `ListToolsMiddleware` 95.0% (uncovered
  `list_tools_handler.go:60` and `list_tools_handler.go:78`); the repository's lint config under go1.26.6 and v2.13.1:
  0 issues; under v2.15.0 on go1.27.4 and on go1.26.6: two issues, gocognit at `list_tools_handler.go:19:1` and
  gocritic at `pipeline_tools.go:161:33`; under v2.13.1 built with go1.27.4: 13 type-check errors, from the standard
  library's export data, as 0006 reports. `go test -race -count=20 -timeout 20m ./internal/handler/...`: exit 0.
* **R3.** `list_tools_characterization_test.go` (21 rows, one function per row so no function scores above the limit)
  and `list_tools_register_test.go` (E-1 and E-2). All pass on `310a541`, and under `-race -count=20` through the
  heavy-run queue (exit 0). 24 planted breaks of the current code, each applied to a scratch copy and run: every row
  fails under at least one, and each plant fails at least one row. The existing three test files fail under 6 of the
  24.
* **R4.** Probes. The session id: a server with the middleware and an in-memory session (`ID()` is `""`); a tool held
  under `active-session` is not listed, one held under `""` is. The panic rows: L18, L19, L20 (empty list, nil error).
  `shadowproof.txt`: a function whose named result `err` is followed by `x, err := f()` in the same block reports the
  same variable (`before == after`: true) and the recover sees the error. S3-FIFO: three single adds of new keys to a
  cache of 20 leave 2 values. The `InternalTools` race probe, one writer through `OnConfigReloaded` and four listers
  on the fallback path, `-race -count=5`, HEAD closure: **5 data-race reports, exit 1**, the first a write at
  `handlers.go:147` against the read at `list_tools_handler.go:57`, the others reads of the freshly unmarshalled tool
  objects at `:58` and `:63` to `:65` that the unsynchronised read hands out early; the same probe on design F (E with
  the read lock): **0 reports, exit 0**. A first probe with many concurrent `OnConfigReloaded` calls reported races
  between those calls in `syncSearchGates` (`handlers.go:105`), which is not the question, and was replaced by the
  single-writer probe.
* **R5.** Designs A to E built in scratch copies (each only replaces `ListToolsMiddleware`; the `CallToolMiddleware`
  tail is byte-identical): gofmt, vet, per-function scores, the repository's lint under three linter/toolchain
  pairs, the list-tools tests and the whole `internal/handler` package, coverage per function, the exported
  surface (`go doc -all` identical to `310a541` for A, B, D, E; C differs in one doc comment), the benchmark, and for
  design E 24 planted breaks (all fail a named row).

### Not verified

* What a client sees from the owner's running server: it was not started, stopped or called. F1 rests on the SDK's
  source (E14, E15) and an in-memory session, which uses the same connection type as the binary's stdio mode
  (`ioConn`, E14).
* Whether the Claude Code stdio client ever shows proxied tools through another path than `tools/list`.
* Real end-to-end latency; the benchmark is one middleware call on a shared machine.
* `make lint` over the whole module for each design: the scratch runs lint `./internal/handler/...` only; the whole
  module is a PLAN gate.
* The whole-package race run per design (only the baseline and, in the PLAN, the chosen design).
* That the Windows and Linux CI jobs behave as the macOS runs here: cross-builds are a PLAN gate; the native jobs run
  on a push.
* How 0005's drafting experiment seeded its proxied tool (R3 of that record): the key it used decides whether it saw
  the tool over its in-process pipes; not re-run here.

### Relation to other records

* **0005-MADR-make-execute-pipeline-schema-and-handler-agree.md** is accepted and its PLAN in progress; this record
  does not change a decision of it. It amends one citation (E11) as a dated annotation in this plan's docs step.
* **0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md** (staged) lists this finding as its
  F2 and says the extraction should "wait for or be sequenced with 0005". This record takes the other side of that
  sequence, as the owner ruled: it lands first. Its F2 text says the closure's `err` is shadowed; it is not (F7).
  That sentence is in a record this one does not edit.
* **0007-MADR-install-the-fleet-agent-workspace.md** (accepted) moves `0001` to `0006` into `docs/decisions/`. This
  record is `0008`, because `0007` was taken when it was written, and lives in that directory with them.
