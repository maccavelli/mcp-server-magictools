---
status: accepted
date: 2026-11-27
decision-makers: repository owner
consulted: "this repository at aaedaf7, read 2026-11-27; live-service log buffer, 2026-11-27 15:22–15:24"
informed: "agents that call sub-server tools through call_proxy, align_tools inline execution, or namespaced routing"
---

# Stop zero-value injection forging missing required arguments before validation

## Context and Problem Statement

`call_proxy` advertises a pre-flight schema firewall: when `validateProxyCalls`
is on, arguments are validated against the target tool's JSON schema before
dispatch, and a missing required field is supposed to produce a Tier-2
structured correction (`MISSING_REQUIRED_FIELDS`, `missing_required`,
`call_template`) that tells the calling agent exactly what to fix. That
correction cannot fire. Zero-value injection runs first and fabricates every
missing required field, so the payload always passes the required-field check
and dispatches with zero-value garbage.

### Observed incident

On 2026-11-27 an agent called `ddg-search:search_web` through `call_proxy` with
`arguments: {}`. The tool's schema declares `query` as a required string.
Expected: a validation error naming `query`. Observed, from the live service's
log buffer:

```text
15:22:53 INFO  tool dispatch  server=ddg-search tool=search_web corr_id=3ca81d08839b
15:22:55 INFO  tool complete  latency_ms=1748 status=ok
15:22:55 WARN  gateway: soft failure detected  reason="results array is empty"
```

The sub-server received `query: ""` and returned zero results for the empty
query. No `pre-flight firewall trapped hallucination` warning, no
`Tier1 stripped hallucinated field` entry, no `argument repair successful`
entry appears for these dispatches: the gateway received an empty argument map
and validation passed it. The same pattern repeated on every retry that day
(at least five dispatches, all `status=ok` with empty results).

Two failures stacked in that incident. Where the agent's populated payload was
flattened to `{}` before reaching the gateway is upstream of this repository
and is **not decided here** (see Out of scope). This record decides the second
failure: the gateway must not turn a missing required argument into a
successful call.

### Facts, re-grounded at `aaedaf7` on 2026-11-27

| ID | Fact | Where |
| :--- | :--- | :--- |
| F1 | `ValidateArguments` calls `CoerceAndStrip(record, args)` before the compiled schema's `Validate(args)`, on both the cache-hit and cache-miss paths | `internal/handler/proxy_service.go:70`, `:75`, `:105` |
| F2 | `CoerceAndStrip` calls `AutoCoerceArguments`, which calls `injectZeroValues`: every key in `record.ZeroValues` absent from `args` is inserted | `internal/handler/proxy_service.go:193`, `:49`; `internal/handler/proxy_coerce_helpers.go:13-22` |
| F3 | `ComputeZeroValues` builds `ZeroValues` **only from the schema's `required` list**: an explicit `default` if present, else `""` for string, `0` for integer/number, `false` for boolean, `[]` for array, `{}` for object, and `""` for any other or absent type | `internal/db/badger.go:1388-1448` |
| F4 | Therefore a missing required field is always injected before validation; `sch.Validate(args)` can never fail on required-field absence, and `formatStructuredCorrectionError`'s `missing_required` / `call_template` branch is unreachable for that case | `internal/handler/proxy_service.go:70-107`, `:204-283` |
| F5 | The ALIGN-4 comment already records that `ComputeZeroValues` only populates required keys, and removed the call-template optional pre-fill as dead code on that basis. The same fact kills required-field validation; that consequence was not recorded | `internal/handler/proxy_handlers.go:189-192` |
| F6 | `validateProxyCalls` defaults to true; when false, `call_proxy` still runs `CoerceAndStrip` ("PROXY-L5"), so injection happens even with validation explicitly disabled | `internal/config/config.go:1264-1268`; `internal/handler/proxy_handlers.go:246-264` |
| F7 | Three dispatch paths share this pipeline: `call_proxy` (`proxy_handlers.go:247`), namespaced direct calls (`list_tools_handler.go:208-217`, which calls `CoerceAndStrip` and then `ValidateArguments`, injecting twice — idempotent today), and `align_tools` inline execution (`align_tools_helpers.go:69-76`, where a validation error falls back to discovery instead of executing) | as cited |
| F8 | The namespaced path skips validation entirely when `toolRecord` is nil (`list_tools_handler.go:208`); `call_proxy` with a nil record makes `ValidateArguments` do a `GetTool` lookup and return its error | `internal/handler/proxy_service.go:57-63` |
| F9 | Infrastructure-error policy already distinguishes roles: `failClosedForMutator` rejects mutators when the schema cannot be fetched/compiled, and logs-and-proceeds for read-only tools | `internal/handler/proxy_service.go:111-121` |
| F10 | No test asserts that a missing required field is rejected. `TestAutoCoerceArguments` asserts injection works, using a hand-built `ZeroValues` map; `TestProxyServiceMethods` calls `ValidateArguments` and discards the error. `go test ./internal/handler/ -run 'TestAutoCoerce\|TestProxy\|TestReserved\|TestValidate'` passes at `aaedaf7` | `internal/handler/proxy_service_test.go:14-36`; `internal/handler/proxy_service_extra_test.go:47-49` |
| F11 | The gateway logs a soft failure when a search-style response has an empty results array, but the dispatch itself is recorded `status=ok` — the forged call is indistinguishable from a legitimate empty result in telemetry | live log buffer, 2026-11-27; incident above |

### Why this is architecturally significant

* The firewall is this gateway's stated contract with calling agents
  (`call_proxy`'s own description mandates schema-verified arguments). The
  required-field half of that contract is silently unenforced.
* The blast radius is every proxied tool with a required parameter, on all
  three dispatch paths (F7). For a read-only search tool the forged value
  produces empty results. For a mutator with a required `path` or `target`,
  the forged value is `""` — a call the sub-server may interpret in ways this
  gateway cannot predict.
* The self-correction loop the Tier-2 template exists to drive (agent receives
  `missing_required`, retries with the field) never starts. Agents instead
  receive plausible-looking success payloads and retry blindly, as the
  incident shows.

## Decision Drivers

* A missing required argument must produce the structured correction, not a
  dispatched call with a fabricated value.
* Tier-1 conveniences that repair what the agent *did* send (type coercion,
  enum snapping, extra-field stripping) keep working; they are not the bug.
* Schema `default` values for **optional** fields remain a legitimate
  convenience; JSON Schema semantics apply defaults only where the schema
  declares them.
* All three dispatch paths (F7) must behave identically; a fix in one path
  that leaves the others injecting is not a fix.
* The role split F9 already established (mutators fail closed, read-only
  tools best-effort on infrastructure errors) is preserved, not relitigated.
* No new dependency. No change to the wire protocol or tool inventories.

## Considered Options

* **Option 1 — Presence check before coercion.** `ValidateArguments` checks
  the schema's `required` list against the incoming `args` map before any
  mutation, and returns `formatStructuredCorrectionError` on absence.
  `injectZeroValues` stops injecting required keys (it may still apply
   explicit schema defaults to optional keys — see ruled rows
   S4–S6). Type coercion, enum snapping and stripping are unchanged and still run
  before full schema validation.
* **Option 2 — Restrict `ComputeZeroValues` to optional fields with explicit
  defaults.** Keep the pipeline order; change what `ZeroValues` contains so
  required keys are never fabricated. Missing required fields then fail
  `sch.Validate` naturally.
* **Option 3 — Validate first, coerce only on failure, revalidate.** Run
  `sch.Validate` on the raw args; on failure run `CoerceAndStrip` and
  validate again; return the correction only if the second validation fails.
* **Option 4 — Remove zero-value injection entirely.** Delete
  `injectZeroValues`; keep type coercion and stripping; let the schema
  validator reject anything missing, required or defaulted.

## Decision Outcome

Chosen option: **Option 1 — Presence check
before coercion**, because it makes the required-field contract explicit and
order-independent: the check reads the schema's `required` list directly, so
it holds regardless of what any coercion stage injects now or later, on all
three dispatch paths, and it routes into the Tier-2 correction machinery that
already exists. Option 2 fixes today's symptom by data hygiene but leaves the
ordering trap (F1) armed for any future injector. Option 3 gives forged
values a second chance to pass and doubles validation cost on every malformed
call. Option 4 is the simplest correct behaviour but discards the
schema-default convenience for optional fields without the owner having ruled
on it.

The owner accepted Option 1 and ruled S4, S5 and S6 as recommended on
2026-11-27.

### Concrete rule

* `ValidateArguments`, after hydrating the record and before `CoerceAndStrip`,
  compares the schema's top-level `required` list with the keys present in
  `args`. Any absent key produces `formatStructuredCorrectionError` (which
  already emits `MISSING_REQUIRED_FIELDS`, `missing_required`, the compact
  schema and the call template). No dispatch occurs.
* `injectZeroValues` no longer inserts keys named in the schema's `required`
  list. A key that is required **and** carries an explicit schema `default`
  is still rejected when absent (S5, recommended): a required field with a
  default is a contradictory schema, and the gateway surfaces the
  contradiction rather than guessing.
* Nested required fields (inside object-typed properties) are left to the
  compiled schema validator after coercion, as today (S7, recommended); the
  pre-check is top-level only, matching what `ComputeZeroValues` fabricates.
* The `validateProxyCalls=false` path keeps running `CoerceAndStrip` for
  coercion and stripping (F6) but, with required keys excluded from
  injection, no longer fabricates them. Validation stays off by choice; the
  gateway simply stops inventing data.
* The namespaced path's duplicate `CoerceAndStrip` call
  (`list_tools_handler.go:209`) becomes harmless rather than load-bearing;
  whether to remove it is a PLAN-scope cleanup, not a decision.
* `align_tools` inline execution needs no new branch: a validation error
  already falls back to discovery (F7), which is the correct outcome for an
  inline attempt with missing arguments.

## Consequences

* Good, because a missing required argument produces the structured
  correction on every dispatch path, and the Tier-2 retry loop can start.
* Good, because mutators can no longer receive fabricated `""` paths or
  targets from this gateway; the incident class ends at the firewall.
* Good, because the empty-query dispatch of 2026-11-27 becomes a
  `MISSING_REQUIRED_FIELDS` error the agent can act on, instead of five
  `status=ok` searches for nothing.
* Bad, because agents that unknowingly relied on injection (sending `{}` and
  getting a "successful" call) now receive errors. That is the contract
  working, but it will look like a regression to anyone reading only success
  rates.
* Bad, because `TestAutoCoerceArguments` and any caller-side fixtures that
  hand-build `ZeroValues` with required keys must be updated to the new
  contract; the PLAN owns that list.
* Neutral, because type coercion, enum snapping, extra-field stripping,
  minification, caching and telemetry are untouched.
* Neutral, because F9's infrastructure-error policy is unchanged; this record
  decides argument content, not schema availability.

## State table

| ID | Input or state | Status and outcome |
| :--- | :--- | :--- |
| S1 | Required field absent, `validateProxyCalls=true`, any dispatch path | decided — structured correction, no dispatch; pinned by a new test per path |
| S2 | Required field present but wrong type (coercible) | decided — coercion applies as today, then full validation; existing behaviour preserved |
| S3 | Required field present, extra hallucinated fields also present | decided — stripping applies as today; unchanged |
| S4 | Optional field absent, schema declares a `default` | decided — default applied by injection, sourced from the schema's `properties`, not from `ZeroValues`. Owner, 2026-11-27, as recommended |
| S5 | Required field absent, schema declares a `default` for it | decided — reject with correction; the contradictory schema is surfaced, not guessed. Owner, 2026-11-27, as recommended |
| S6 | `ZeroValues` contains keys not in `required` (legacy records, hand-built fixtures) | decided — injection ignores any key without an explicit schema `default`; `ZeroValues` is no longer an injection source. Owner, 2026-11-27, as recommended |
| S7 | Nested required field absent inside an object-typed property | decided — handled by the compiled validator after coercion, as today; pre-check is top-level only |
| S8 | `validateProxyCalls=false`, required field absent | decided — no injection of required keys; call dispatches without the field and the sub-server decides, matching the operator's choice to disable validation |
| S9 | Schema hash empty (`record.SchemaHash == ""`) | decided — existing early return at `proxy_service.go:66-68` stands; no schema, no check. Out of this record's scope to change |
| S10 | Schema fetch/compile infrastructure error | decided — F9 policy unchanged: mutator fails closed, read-only logs and proceeds |
| S11 | `args` map nil (no arguments at all) with a non-empty `required` list | decided — same as S1; the presence check treats nil as empty |
| S12 | Tool record nil on the namespaced path | out of scope — existing skip (F8) is a separate routing question, not an injection question |

No row is open. The record is `accepted`.

## Confirmation

* A test per dispatch path (`call_proxy`, namespaced, `align_tools` inline)
  sends arguments missing a required string field and asserts: no dispatch,
  `MISSING_REQUIRED_FIELDS` in the error (or discovery fallback for inline),
  and `missing_required` naming the field. Maps to S1, S11.
* A test asserts a coercible wrong-typed required field still dispatches
  after coercion. Maps to S2.
* A test asserts extra fields are still stripped. Maps to S3.
* A test per ruled row pinning the default/injection semantics. Maps to
  S4, S5, S6.
* A test asserts the compiled validator still rejects a missing nested
  required field. Maps to S7.
* A test with `validateProxyCalls=false` asserts the dispatched payload does
  not contain the fabricated required key. Maps to S8.
* The PLAN's first step re-grounds F1–F10 at the execution HEAD; line numbers
  here are pinned to `aaedaf7`.
* `make check-records` and `make markdownlint` pass before any commit.

## Pros and Cons of the Options

### Option 1 — Presence check before coercion

* Good, because the contract is enforced by reading the schema, not by
  hoping no earlier stage mutated the payload; order-independent by
  construction.
* Good, because it reuses `formatStructuredCorrectionError` unchanged — the
  correction format agents already receive for other validation failures.
* Good, because one check site (`ValidateArguments`) covers all three
  dispatch paths, which all funnel through it when validation is on.
* Bad, because required-field logic now exists in two places (the pre-check
  and the compiled validator); the pre-check must stay top-level-only to
  avoid duplicating full JSON Schema semantics.

### Option 2 — Restrict `ComputeZeroValues` to optional defaulted fields

* Good, because it deletes the fabrication at the source and requires no
  ordering change.
* Neutral, because `ZeroValues` is also consumed by template builders
  (`proxy_service.go:249-253`, `align_tools_helpers.go:212`); those consumers
  would need review in the same change.
* Bad, because the ordering trap (F1) remains: any future coercion stage that
  inserts a required key silently re-disables the firewall, and no test would
  catch it unless Option 1's presence tests are added anyway.
* Bad, because persisted `ToolRecord`s carry `ZeroValues` (`db/badger.go:121`);
  old records keep required-key zero values until re-hydration, so the fix
  has a data-migration tail the presence check does not.

### Option 3 — Validate, coerce on failure, revalidate

* Good, because well-formed calls skip coercion entirely — a latency win on
  the happy path.
* Bad, because a fabricated value that satisfies the schema still passes the
  second validation; the bug survives for exactly the incident's shape
  (`""` is a valid string).
* Bad, because two validation passes on every malformed call, and the
  correction error must be built from the pre-coercion payload to be truthful
  about what the agent sent.

### Option 4 — Remove zero-value injection entirely

* Good, because it is the smallest diff with correct required-field
  behaviour, and it makes the gateway's mutation of payloads strictly
  repair-only.
* Neutral, because it is Option 1 with S4 ruled "no defaults injected"; the
  presence check would still be worth adding as a tripwire.
* Bad, because optional fields with schema-declared defaults stop being
  filled, changing behaviour for tools whose sub-servers do not apply their
  own defaults — a compatibility question the owner has not ruled on.

## More Information

* The incident's first failure — the agent-side payload arriving as `{}` —
  is upstream of this repository (client serialization or transport). This
  record neither explains nor fixes it; with Option 1 in place, that failure
  becomes a visible correction error instead of a silent empty search, which
  is the diagnosability the gateway owes its callers either way.
* `python3 scripts/check_records.py --next` printed `0017` before this file
  existed.
* The soft-failure telemetry (F11) stays as it is; distinguishing "forged
  empty argument" from "legitimate empty result" becomes unnecessary once
  forging stops.
* Related history: the ALIGN-4 change (F5) removed the template pre-fill as
  dead code but did not record that the same `ZeroValues` shape disarms
  required-field validation. This record supersedes nothing; it decides what
  ALIGN-4 left undecided.
