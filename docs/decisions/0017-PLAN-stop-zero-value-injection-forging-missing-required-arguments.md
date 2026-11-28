---
status: implemented
date: 2026-11-27
associated-madr: "0017-MADR-stop-zero-value-injection-forging-missing-required-arguments.md"
---

# Implement the firewall that rejects missing required arguments

Associated MADR: [0017-MADR-stop-zero-value-injection-forging-missing-required-arguments.md](0017-MADR-stop-zero-value-injection-forging-missing-required-arguments.md)

## Goal

A missing required argument on any dispatch path (`call_proxy`, namespaced direct call, `align_tools` inline execution) produces the Tier-2 structured correction (`MISSING_REQUIRED_FIELDS`) instead of a dispatched call with a fabricated zero value. Injection applies only explicit schema `default`s to optional fields. Every decided state-table row S1–S11 of the MADR is pinned by a test.

## Scope

In scope, the MADR being `accepted`:

* `internal/handler/proxy_service.go` — the presence pre-check in `ValidateArguments`
* `internal/handler/proxy_coerce_helpers.go` — `injectZeroValues` replaced by schema-default injection
* `internal/handler/list_tools_handler.go` — remove the duplicate `CoerceAndStrip` call at `:209`
* tests in `internal/handler` pinning S1–S11, and the fixtures of `TestAutoCoerceArguments` / `TestProxyServiceMethods` that encode the old contract

Out of scope, per the MADR: the upstream payload flattening that produced `arguments: {}` in the incident; `ComputeZeroValues` and the persisted `ZeroValues` field (template builders still read them at `proxy_service.go:249-253` and `align_tools_helpers.go:212` — only the injection source changes, no data migration); the `SchemaHash == ""` early return (S9); the nil-record namespaced skip (S12); soft-failure telemetry; any new dependency; any run of a built binary beside the live service.

## Grounding Reference

Re-ground every line at execution HEAD. The lines below were read at `aaedaf7` on 2026-11-27.

| Anchor | Where | What it pins |
| :--- | :--- | :--- |
| F1 | `internal/handler/proxy_service.go:70`, `:75`, `:105` | coercion runs before validation, both cache paths |
| F2 | `internal/handler/proxy_coerce_helpers.go:13-22` | every missing `ZeroValues` key is inserted |
| F3 | `internal/db/badger.go:1388-1448` | `ZeroValues` is built only from `required` |
| F5 | `internal/handler/proxy_handlers.go:189-192` | ALIGN-4 recorded the required-only shape |
| F6 | `internal/handler/proxy_handlers.go:246-264` | validation-off still coerces |
| F7 | `internal/handler/list_tools_handler.go:208-217`; `internal/handler/align_tools_helpers.go:69-76` | the other two dispatch paths |
| F9 | `internal/handler/proxy_service.go:111-121` | mutator fail-closed policy |
| F10 | `internal/handler/proxy_service_test.go:14-36`; `internal/handler/proxy_service_extra_test.go:47-49` | no test rejects a missing required field |

## Prerequisites

* The owner accepted Option 1 and ruled S4, S5, S6 as recommended on 2026-11-27; the MADR is `accepted`.
* No built binary of this module is started on a machine that runs the live service; tests use temporary stores and fake registries only.
* The broader-suite host decision follows the `available-dev-hosts` skill before `go test ./...` runs.

## Phase 1 — the failing test, then the check

1. Re-ground F1–F10 at execution HEAD; record the hash and any line drift in the Execution Record. A fact that no longer holds stops the phase and amends the MADR first.
2. Write the S1 regression test on the service layer: a record whose schema requires a string field, args `{}` — assert `ValidateArguments` returns an error containing `MISSING_REQUIRED_FIELDS` and `missing_required` naming the field. Run it; it must fail at this HEAD. Record the failure output.
3. Add the presence pre-check in `ValidateArguments`, after `hydrateRecord` and the `SchemaHash == ""` early return, before `CoerceAndStrip`: read the schema's top-level `required` list; any absent key (nil args treated as empty, S11) returns `formatStructuredCorrectionError`. A required key carrying a `default` is still rejected (S5). No mutation of `args` before this check.
4. Replace `injectZeroValues` with `injectSchemaDefaults`: iterate `InputSchema.properties`; insert an absent key's explicit `default` only when the key is not in `required` (S4); never insert a key without a schema default, whatever `ZeroValues` holds (S6). Update the `AutoCoerceArguments` doc comment.
5. Remove the duplicate `ps.CoerceAndStrip(toolRecord, args)` at `list_tools_handler.go:209`; `ValidateArguments` on the next line already coerces.
6. Update `TestAutoCoerceArguments` and `TestProxyServiceMethods` fixtures to the new contract.

Exit: the Phase-1 S1 test was seen red, then green; `go test ./internal/handler/ -count=1` green; no production change beyond steps 3–5.

## Phase 2 — pin every decided row

1. Add tests, one per row: S1 on the namespaced middleware (fake registry, assert no dispatch) and on `align_tools` inline (validation failure → discovery fallback, no execution); S2 coercible wrong-typed required field still dispatches; S3 extra fields still stripped; S4 optional default injected; S5 required-with-default rejected; S6 non-required `ZeroValues` key without schema default not injected; S7 nested required field rejected by the compiled validator; S8 validation-off path does not fabricate the required key; S9 empty `SchemaHash` early return unchanged; S10 mutator fails closed / read-only proceeds on schema infrastructure error (add if absent); S11 nil args with non-empty `required` rejected.
2. Plant check: temporarily re-introduce required-key injection in a scratch edit; the S1, S5 and S8 tests must fail; revert. Record the caught-plant output.
3. Run `go test ./internal/handler/ ./internal/db/ -count=1`.

Exit: every decided row has a passing test; no plant of "inject the required key" stays green.

## Phase 3 — suite, gates, records

1. Run the broader suite per the `available-dev-hosts` skill; record the host decision (a GATES record if a remote host is used). Local runs follow [testing-and-process-safety.md](../guides/testing-and-process-safety.md).
2. Fill the Execution Record; set this PLAN's status. Run `make check-records` and `make markdownlint`. Scan the diff for the local account name and the hostname before staging.

Exit: suite green, gates green, Execution Record complete.

## Rollback

Revert the commit series. The old inject-everything behaviour returns with it; there is no config, schema-store or wire migration to unwind. The live service is not restarted, signalled or rebuilt by this plan; the fix reaches it only when the owner next rebuilds through their own process. Agents that previously "succeeded" with missing required arguments will now receive validation errors — the contract working, per the MADR's Consequences.

## Completion Criteria

* S1–S11 have a test each, mapped one-to-one; S1 covers all three dispatch paths.
* The incident's shape — a search-like schema, args `{}` — yields `MISSING_REQUIRED_FIELDS` naming the field, at test level.
* The plant check caught the re-introduced injection.
* No built binary was run beside the live service.
* `make check-records` and `make markdownlint` pass.
* The handoff says the running service keeps the old behaviour until the owner rebuilds it.

## Execution Record

Executed 2026-11-27 on branch `feature/required-arg-firewall`, from `aaedaf7`.
The owner's rulings of 2026-11-27 (Option 1; S4, S5, S6 as recommended) are
recorded in the MADR. The owner approved the plan and authorized commit and
push for the session in the same turn.

### Phase 1, pass 1

* Re-grounded F1–F10 at `aaedaf7`: every cited line matched; no drift, no
  MADR amendment needed.
* Wrote `TestS1_MissingRequiredRejected_CallProxy`
  (`internal/handler/required_fields_test.go`) and ran it before any
  production change. Observed failure:

  ```text
  --- FAIL: TestS1_MissingRequiredRejected_CallProxy (0.15s)
      required_fields_test.go:55: expected MISSING_REQUIRED_FIELDS error, got nil; args after validation: map[query:]
  ```

  The bug reproduced exactly as the MADR states: validation passed and
  `query` was fabricated as `""`.
* Added the presence pre-check and `missingRequiredKeys` to
  `internal/handler/proxy_service.go`; replaced `injectZeroValues` with
  `injectSchemaDefaults` in `internal/handler/proxy_coerce_helpers.go`;
  removed the duplicate `CoerceAndStrip` in
  `internal/handler/list_tools_handler.go`.
* The S1 test then passed. Full `go test ./internal/handler/` failed only on
  `TestAutoCoerceArguments`, whose fixture encoded the old contract; updated
  it (and only it — `TestProxyServiceMethods` passed unchanged, a deviation
  from the plan's step 6 which had listed it for update; its assertions do
  not depend on injection). Handler package then green.

### Phase 2, pass 1

* Added twelve tests pinning S1–S11 (S1 on all three dispatch paths). All
  passed:

  ```text
  --- PASS: TestS1_MissingRequiredRejected_CallProxy (0.13s)
  --- PASS: TestS1_MissingRequiredRejected_Namespaced (0.16s)
  --- PASS: TestS1_MissingRequiredRejected_AlignInline (0.15s)
  --- PASS: TestS2_CoercibleRequiredFieldStillValidates (0.14s)
  --- PASS: TestS3_ExtraFieldsStillStripped (0.13s)
  --- PASS: TestS4_OptionalDefaultInjected (0.00s)
  --- PASS: TestS5_RequiredWithDefaultStillRejected (0.12s)
  --- PASS: TestS6_ZeroValuesNotAnInjectionSource (0.00s)
  --- PASS: TestS7_NestedRequiredRejectedByValidator (0.14s)
  --- PASS: TestS8_ValidationOffDoesNotFabricateRequired (0.15s)
  --- PASS: TestS9_EmptySchemaHashEarlyReturn (0.17s)
  --- PASS: TestS10_SchemaInfraErrorRoleSplit (0.10s)
  --- PASS: TestS11_NilArgsRejected (0.18s)
  ```

* Plant checks, two plants, each reverted after observation:
  * Plant A — restored the old `ZeroValues` injection loop alongside the new
    code: `TestS6` and `TestS8` failed; S1/S5 stayed green because the
    presence pre-check rejects before coercion. This shows the two guards
    are independent: the pre-check protects validation-on, the injection
    guard protects validation-off.
  * Plant B — disabled the presence pre-check while Plant A was active:
    `TestS1` (all three paths), `TestS5`, `TestS6`, `TestS8` failed. `TestS11`
    stayed green because the compiled validator rejects nil args once
    nothing fabricates them — recorded as observed, not as a gap: with both
    guards removed (the original code) S11's shape was the incident itself,
    pinned by S1.
  * Both plants reverted; `go test ./internal/handler/ ./internal/db/ -count=1`
    green (`ok ... 9.472s`, `ok ... 28.959s`).

### Phase 3, pass 1

* Host decision per `available-dev-hosts`: `pick-host --need any --dry-run`
  scored the fleet; `run-on-host --need any -- go test ./...` ran the suite
  on the fleet's windows/amd64 host under its queue (ticket
  `20261128T022439Z-<mac-host>-2f7a`), waited 0s, elapsed 93s. Result:
  21 packages `ok`, 0 failures, remote status 0. The change is pure Go
  logic with no build tags, cgo or path semantics; the Windows run covers
  the GOOS most different from the session host. No GATES record needed
  beyond this entry: one host, one suite run, no per-host checklist.
* `make check-records` and `make markdownlint` green (below).
* Identifier scan of the staged diff for the local account name and the
  hostname domain: no matches (command and result recorded at commit time).

### Not done, and why

* No live-service interaction of any kind: the running service keeps the
  old behaviour until the owner rebuilds it through their own process.
* The upstream payload flattening (client side) was not investigated; out
  of scope per the MADR.
* `ComputeZeroValues` and persisted `ZeroValues` were left unchanged; only
  the injection source changed, per the MADR's scope.
