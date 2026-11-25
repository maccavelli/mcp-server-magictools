---
status: implemented
date: 2026-11-25
associated-madr: "0012-MADR-make-the-per-user-launchagent-lifecycle-correct-restartable-and-verifiable.md"
---
# Implement correct, restartable, and verifiable per-user LaunchAgent lifecycle management

Associated MADR: [0012-MADR-make-the-per-user-launchagent-lifecycle-correct-restartable-and-verifiable.md](0012-MADR-make-the-per-user-launchagent-lifecycle-correct-restartable-and-verifiable.md)

## Goal

Deliver one service-lifecycle implementation whose observable contract is:

* `service stop` persistently disables and boots out the per-user LaunchAgent and does not delete its definition.
* `service start` starts an installed service without unnecessarily killing a healthy process and succeeds only when the expected instance is ready.
* `service restart` produces a new, verified process and leaves the service enabled.
* `KeepAlive=true`, disable-plus-bootout, and `ExitTimeOut=35` provide unconditional supervision with bounded graceful shutdown.
* Install and refresh produce escaped, parsed, atomically replaced definitions and preserve/restore prior state on failure.
* Self-update preserves the original running/stopped state and never treats PID existence as readiness.
* Status and doctor distinguish registration, enablement, process state, readiness, and stale diagnostics.
* Tests never target the production label, production database, or a process the test did not start.

## Scope

### In scope

| Component | Planned work |
| --- | --- |
| `cmd/mcp-server-magictools/service.go` | Replace platform-specific command bodies with calls into the shared lifecycle service; remove raw-PID uninstall escalation and substring-only status/health behavior. |
| `cmd/mcp-server-magictools/service_lifecycle.go` and focused new files | Shared identity, manager-state model, locking, command orchestration, readiness, state-file validation, error classification, and prior-state snapshots. |
| `cmd/mcp-server-magictools/service_launchd.go`, `service_launchd_test.go`, build-tagged Darwin files | launchctl adapter, typed plist model/rendering, current plist policy, isolated Darwin probes and integration tests. |
| `cmd/mcp-server-magictools/service_other.go` and platform adapters | Preserve systemd and Windows behavior behind the common interfaces; do not route them through launchd semantics. |
| `cmd/mcp-server-magictools/service_refresh.go` | Use the shared renderer, atomic file writer, identity, and state restoration receipt; remove the Darwin no-op reload assumption. |
| `cmd/mcp-server-magictools/service_launchd.go`, `service_lifecycle.go` | Thin product wrapper over the library’s launchd `Job`: persistent disable-before-stop, readiness identity, expanded status, and transactional definitions. |
| `cmd/mcp-server-magictools/update.go`, `update_service.go` | Replace `mcplib/selfupdate` with `go-selfupdate-lib/selfupdate` v1.26.3; use its managed two-phase installer and product lifecycle/reconciler adapters. |
| `go.mod`, `go.sum` | Add and lock `github.com/maccavelli/go-selfupdate-lib v1.26.3` with its required `x/mod`, `x/sys`, and `x/term` upgrades; remove `mcplib` only if no other imports remain. |
| `internal/proctable/` | Add only a narrow, read-only process-identity seam if the readiness implementation cannot safely reuse existing start-token functionality; no test may construct the real table. |
| `internal/testsafety/` | Reclassify launchctl and any process-identity call sites; add a static rule preventing live launchd tests from using the production label. |
| `cmd/mcp-server-magictools/*_test.go` | Fake executor/filesystem/clock/HTTP tests, plist parsing fixtures, update state-machine tests, and planted regressions. |
| `docs/guides/services-and-transports.md`, `cli-reference.md`, `platform-installation.md`, `testing-and-process-safety.md`, `operations-and-security.md` | Correct lifecycle, status, GUI-session, health, shutdown, log, and safe-test documentation. |
| `docs/decisions/0012-MADR-…md`, this PLAN, `docs/README.md` | Record amendments, phase results, and index status. |

### Out of scope

* SMAppService migration, app bundles, signing/notarization, `AssociatedBundleIdentifiers`, and privileged LaunchDaemons.
* launchd socket activation, XPC transactions, `EnablePressuredExit`, and a launchd dependency graph.
* Changes to the application’s 30-second shutdown budget, downstream process-reaping policy, or database lifecycle.
* New logging dependencies or live log rotation. The plan uses the existing 50 MiB on-open cap.
* No further dependencies. The owner explicitly approved `github.com/maccavelli/go-selfupdate-lib v1.26.3`; this MADR/PLAN names it and its transitive version changes. The library’s update engine and service adapters replace local machinery, not the product identity/readiness contract.
* Service installation, restart, or update on the owner’s live machine by an agent. Owner-controlled acceptance is separate from automated tests.
* Push, tag, or release operations.

## Grounding Reference

Re-read the MADR’s grounding amendment before coding. It corrects the earlier unsupported assumption that
`SuccessfulExit=false` covers signal deaths. The selected policy is the explicitly documented `KeepAlive=true`.

### Source facts at `6dc74e0600ae944cf92d4595d7b3569d5ec31601`

| Fact | Evidence to re-ground at execution HEAD |
| --- | --- |
| Stop only signals the loaded service and does not persist a stopped state. | `service.go:953-984`; plist `service.go:486-500`. |
| Start uses `print`, then `kickstart -k` or bootstrap; updater always uses bootstrap after bootout. | `service.go:912-935`; `update_service.go:187-201`. |
| Update health and running checks use state-file PID existence. | `update_service.go:66-75,99-162`; `service.go:815-827`. |
| Real `/health` returns 503 while booting and 200 for ready/degraded. | `main.go:911-945`; `docs/guides/services-and-transports.md:100-109`. |
| Refresh truncates the definition and does not reload launchd. | `service_refresh.go:95-175,266-275`. |
| Plist interpolation is not XML-safe and its test does not parse XML. | `service.go:16,452-507,622-667`; `service_hardening_test.go:64-89`. |
| Uninstall can signal a raw recorded PID after only 20 seconds. | `service.go:717-760,841-863`. |
| `mcplib` managed recovery restarts after reconcile/start/health failures regardless of initial running state. | `mcplib` v1.4.1 `selfupdate/managed.go:64-132`; call sequence reproduced in P0 tests before replacement. |
| Application log cap is 50 MiB on open, not 10 MiB and not continuous rotation. | `internal/util/io.go:60-96`; `internal/logging/logging.go:31-125`. |
| Existing process start tokens are fixed for process lifetime on Darwin. | `internal/proctable/start_other.go:11-25`; `table.go:154-170`. |
| Testsafety counts launchctl command words by file. | `internal/testsafety/allowlist.go:85-93`; `guard_primitives.go:80-99`. |
| Existing native CI uses macOS 15, not the observed macOS 27.2.1 host. | `.github/workflows/ci.yml:167-200`. |

### Apple and host evidence

The MADR links the official sources. At P0, record the following again without contacting the live service:

```bash
sw_vers
launchctl version
launchctl help bootstrap
launchctl help bootout
launchctl help enable
launchctl help kickstart
man launchd.plist
man launchctl
```

Required documentary anchors are KeepAlive true/OR semantics, persistent disable, `bootout --wait` and its blocking
warning, kickstart `-p`, non-API `print` output, ExitTimeOut, ProcessType, Umask, and output paths. `launchctl error 113`
was observed as “Could not find specified service”; that is supporting evidence, not a stable output-parsing contract.
The live probe suite, not a guessed error string, determines how absence and other errors are distinguished.

## Prerequisites

1. The owner explicitly approves this PLAN and names the phase to execute. The MADR remains proposed until that decision.
2. P0 confirms HEAD is the reviewed commit or rewrites every source line reference and affected test expectation for the new HEAD.
3. The working tree contains no unowned edits. Stage and commit only by explicit owner/coordinator instruction; use `git commit --no-edit` after the required gates.
4. No agent runs `make run`, `go run`, `serve`, a built module binary, or any lifecycle command against the default service on the live-service machine.
5. Ordinary tests remain hermetic. Suite-level and race runs use the fleet `run-heavy`/`run-on-host` placement rules; Darwin-required work runs on the Darwin host without SSHing back into the session machine.
6. Live Darwin tests require all of: an explicit `-tags=launchdlive` invocation, a scratch repository/build directory, a random test label with prefix `local.magictools.test.`, an isolated temporary HOME/config/cache, dynamically allocated listeners, and a cleanup routine scoped to that exact label.
7. The test helper is built from test-only code and does not open MagicTools’ production database, load the operator’s configuration, spawn downstream servers, or import the production orchestrator startup path.
8. If a live probe needs behavior unavailable in CI (GUI domain, login-item approval), record an environment skip with the exact probe result. An environment skip is not a behavioral pass and leaves the corresponding completion item open.
9. No other dependencies are added without a MADR. No plan phase implements beyond its stated exit criterion.

## Implementation contract

### Identity and interfaces

Use small package-local interfaces so the default tests never execute launchctl:

```go
type commandRunner interface {
    Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
    ExitCode(error) (int, bool)
}

type serviceManager interface {
    DefinitionExists(context.Context) (bool, error)
    Inspect(context.Context) (managerState, error)
    Enable(context.Context) error
    Disable(context.Context) error
    Bootstrap(context.Context) error
    Bootout(context.Context) error
    Kickstart(context.Context) error
}

type readinessProbe interface {
    WaitReady(context.Context, expectedInstance) (readyState, error)
}
```

`managerState` distinguishes definition absent/present, service unloaded/loaded, process running/stopped/unknown,
enabled/disabled/unknown, and last command error. Do not encode registration and readiness in one enum. Keep `launchctl`
argv construction pure and table-tested. Use `/usr/bin/plutil` for validation on Darwin and a standard-library XML
parser in hermetic tests; no third-party plist dependency is needed.

### launchd argv and error rules

* Stop: `disable <service-target>`, then `bootout --wait <service-target>` with an outer context deadline.
* Start absent: `enable`, then `bootstrap <domain-target> <plist-path>`.
* Start loaded/stopped: `enable`, then `kickstart -p <service-target>`.
* Start loaded/running: no `-k`; verify the expected instance and readiness.
* Restart: bootout if loaded, enable/bootstrap, then readiness; never call persistent stop internally.
* Refresh of a loaded definition: bootout then bootstrap so launchd consumes the new file.
* Absent bootout is idempotent only after the live probe identifies that case. Other errors are not “absent.”
* Never parse `launchctl print` text. Use exit status plus typed exit codes/stderr classification verified by live tests; ambiguous failure remains unknown and blocks destructive fallback.
* `print-disabled` output is not parsed into a boolean API. The controller performs bounded `enable` before start and uses the resulting behavior and typed command errors for diagnosis. Any human-facing output shown by doctor is informational, not authoritative manager state.

### Definition policy

The typed plist must emit:

* `Label=com.magictools.mcp-server-magictools` for production and the unique helper label for tests.
* Absolute `ProgramArguments[0]`, argument `serve`, explicit HOME, deterministic PATH, and existing endpoint values.
* `KeepAlive=true`, `RunAtLoad=true`, `ThrottleInterval=5`, `ExitTimeOut=35`, `ProcessType=Standard`, and `Umask` string `"077"`.
* No `AbandonProcessGroup`, XPC transaction/pressured-exit keys, or bundle association.
* Both output paths set to the application’s existing `magictools_debug.log`.

Use `encoding/xml` or explicit XML escaping for every interpolated value; do not merely switch to `html/template` and
assume its escaping matches plist text semantics. Validate absolute paths, parse the resulting XML, run `plutil -lint`
in the Darwin adapter, and compare typed values in tests. Atomic replacement uses a same-directory temporary regular
file, complete write, `Sync`, `Close`, mode 0600, rename, and directory sync where supported. Reject a non-regular or
symlink destination rather than following it. Preserve the exact old bytes for rollback.

### Readiness and state identity

Extend diagnostic state with a process start token and the resolved executable path. The running service obtains its own
start token through a production-only factory. Tests inject a fake; no test calls the real process-table constructor.

A ready observation requires all of:

1. Manager state indicates the job is loaded and running.
2. `GET` the configured loopback `/health` with a bounded HTTP client; HTTP 200 is required. Decode status and preserve `degraded` as ready. A 503 means continue until deadline.
3. The state file parses; PID is positive; executable matches the expected canonical path; recorded start token matches a fresh identity probe; and this start is the operation’s expected new start when restart/update requires one.
4. No response from another listener or a stale state file can satisfy readiness. Tests must bind an unrelated HTTP server to prove this.

Do not delete stale state merely because readiness failed. Report it as diagnostic evidence. PID existence alone never
sets `Running=true` or `Ready=true`. Status output retains existing fields and adds registration, enabled, manager process,
readiness, and stale/error fields so current consumers do not lose data.

### Update state machine

Adopt `github.com/maccavelli/go-selfupdate-lib/selfupdate` v1.26.3. Its `ManagedInstaller` captures original running
state, keeps a stopped service stopped unless an `EnabledLifecycle` says otherwise, uses public two-phase
Apply/Reconcile/Start/WaitHealthy/Commit, stops a started replacement before definition/binary recovery, and performs
recovery with a bounded non-cancelled context. Do not expose `EnabledLifecycle` on the product adapter when a stopped
service must remain stopped; the library’s launchd enablement implementation parses human-facing `print-disabled`.

Use the library’s launchd `Job` for registration/process operations and bounded wait behavior. Wrap it for persistent
`disable` before interactive stop and product readiness. Keep the product XML renderer and exact-byte refresh receipt.
Test the real v1.26.3 managed installer with fake two-phase sessions and product fakes, proving S15-S19 rather than
reimplementing binary replacement. Run the full module after its `x/mod`, `x/sys`, and `x/term` upgrades.

## Implementation Steps

### P0 — Re-ground and prove the current failures

1. Run the grounding commands and source review above; amend MADR/PLAN references if HEAD differs.
2. Add characterization tests around current pure helpers and fakeable update orchestration. Capture current argv, malformed XML, stale-state health, and initially-stopped recovery behavior without executing service management.
3. Create a temporary plist fixture containing `&`, `<`, non-ASCII, and quotes; prove the old rendering fails parsing and record the result. Do not test against a real installed plist.
4. Review `mcplib` standalone apply/rollback APIs at the exact locked version and document the local orchestration calls. If they cannot safely be reused, stop and amend this plan before implementing a replacement.
5. Produce the final fake interfaces and state snapshots as a short design appendix in the execution record.

Exit criterion: every L1-L15 item maps to code evidence or is explicitly downgraded; no unsupported crash/status claim remains; update primitives are feasible; tests run without service side effects.

### P1 — Safe plist rendering and atomic definitions

1. Introduce the typed plist data/model and shared render function for install and refresh.
2. Implement XML escaping, path validation, HOME/PATH, selected KeepAlive/process/timeout/umask policy, and single application-log destination.
3. Implement no-follow regular-file checks, exact-byte backup, same-directory temp/write/sync/close/chmod/rename, and directory sync with explicit unsupported-error handling.
4. Replace substring-only plist tests with XML-decoded assertions and golden fixtures. Include ampersand, angle brackets, quotes, non-ASCII, spaces, empty optional endpoints, relative paths, symlink destinations, short writes, sync failures, and rename failures.
5. Run `plutil -lint` on Darwin-generated fixtures in a non-live test. Prove old malformed fixtures fail and new ones pass.

Tests: S01-S05, S25, S26, S28 partially; planted removal of escaping, fsync, atomic rename, and no-follow check must each fail a named test.

Exit criterion: install/refresh share one renderer; no direct truncate-in-place definition write remains; all plist values round-trip; failure leaves old bytes intact.

### P2 — launchd adapter, identity, and live CLI probes

1. Implement the target resolver once and thread home/UID/label/plist/log/config paths through every macOS entry point. Reject mixed sudo/user targets before mutation.
2. Implement pure argv builders and `commandRunner`, preserving stdout/stderr and typed exit status. All launchctl calls use contexts; no indefinite `bootout --wait`.
3. Add per-label lifecycle locking using existing `mcplib` locking or a small standard-library file lock. Test cancellation and two contenders.
4. Add test-only `launchdlive` helper and labels. Statically forbid `com.magictools.mcp-server-magictools` in live-test files except inside a guard that asserts it is never used.
5. Probe actual `print`, bootstrap duplicate, bootout absent, bootout `--wait`, enable/disable persistence, kickstart running/stopped, and ambiguous errors using only the helper label. Pin outcomes in live-tagged tests.
6. Update `internal/testsafety/allowlist.go` for real launchctl call sites and add its production-label test guard.

Tests: S06-S10, S13, S21-S25, S27, S29; duplicate bootstrap and absent bootout probes must be observed, not inferred.

Exit criterion: every real launchctl call goes through the adapter; every launchd operation is bounded and labeled; current-host CLI behavior is pinned without parsing print output.

### P3 — Unified lifecycle and truthful readiness

1. Add start-token recording to `service.state` and a narrow production identity factory with injected test doubles.
2. Implement manager inspection and readiness independently. Add HTTP and state-identity checks described above.
3. Implement start, stable stop, restart, install, refresh, and uninstall through one controller. Preserve no-force install as an unchanged no-op.
4. Route interactive commands and refresh to the controller. Remove restart’s composition of persistent stop and remove raw-PID SIGKILL escalation.
5. Implement expanded status/doctor and nonzero doctor failure for actionable states. Preserve JSON field compatibility.
6. Implement deterministic logging path and document the existing 50 MiB on-open truncation behavior.

Tests: S06-S14 and S20-S29, including unrelated HTTP 200, stale PID, reused start token, clean exit, nonzero exit, crash, stop, timeout, absent domain, disabled job, concurrent commands, and uninstall.

Exit criterion: no lifecycle path trusts PID alone; no stop is merely SIGTERM; restart returns only for a new ready instance; uninstall cannot signal a state-file PID directly.

### P4 — State-preserving self-update and rollback

1. Import `github.com/maccavelli/go-selfupdate-lib/selfupdate` v1.26.3 in `update.go` and `update_service.go`; lock the new module and required `x/mod`, `x/sys`, and `x/term` versions. Run `go mod tidy` and inspect remaining `mcplib` imports before removing that requirement.
2. Keep `ManagedInstaller`; replace the updater lifecycle with the shared product manager/readiness implementation and retain exact-byte refresh receipts.
3. Do not expose `EnabledLifecycle` for the stopped-state-preserving update adapter. Use the library’s two-phase managed sequencing and recovery timeout.
4. Test stopped-original success/failure, running-original success/failure, apply failure, reconcile failure, start failure, readiness failure, and recovery failure with the real library and fake two-phase sessions. Assert operation order and joined errors.
5. Add launchdlive helpers for definition reload and process replacement. Do not download releases or contact GitHub in lifecycle tests.

Tests: S15-S19 and all update scenarios in the state matrix. Plants: restart a stopped original, health by stale state, restore definition while a replacement runs, skip library recovery, lose original error.

Exit criterion: running/stopped state is preserved; refreshed definitions are loaded; the library restores files only after any started replacement is stopped; all planted defects and transitive-dependency regression gates pass.

### P5 — Documentation, migration, and operator acceptance artifacts

1. Update CLI/service guides for persistent stop, start, restart, unconditional KeepAlive, GUI-domain requirement, readiness/degraded behavior, and doctor exit status.
2. Document the single log, umask, and 50 MiB on-open truncation precisely; remove claims of continuous rotation.
3. Add a controlled migration procedure: inspect current state, install new binary, refresh definition, restart only if previously running, verify manager and `/health`, and confirm stop/start manually.
4. Add rollback instructions restoring the previous binary and exact plist backup without starting an originally stopped service.
5. Add live-test setup and environment-skip semantics to the testing guide; never instruct an agent to run against the production label.
6. Produce `0012-GATES-launchd-lifecycle.md` only when execution supplies per-host results; do not invent a report now.

Exit criterion: docs match implemented behavior and commands; no documentation says macOS 27 behavior was proven on macOS 15 CI; migration and rollback preserve prior state.

### P6 — Full verification, review, and completion

1. Re-run every state-matrix test and named plant on the final tree.
2. Run Darwin package tests and launchdlive tests; run native non-Darwin tests to detect adapter regressions.
3. Run queued full-module and race gates according to the fleet host rules. On the live-service machine, follow the existing process-safety restrictions and use an approved isolated host/scratch tree.
4. Run gofmt, vet, lint, `make check-records`, `make markdownlint`, and `git diff --check`.
5. Perform an independent source review specifically for production-label access, raw-PID signaling, skipped rollback, unbounded launchctl calls, unescaped XML, and stale-PID readiness.
6. Record each phase pass, exact commands, host OS versions, outputs, environment skips, and deviations in the execution record. Mark the PLAN complete only when no decided row lacks proof.

Exit criterion: all decided rows tested, all planted regressions caught, all gates green, no blocker, and no unapproved scope expansion.

## Amendment implementation steps

Amendment, 2026-11-25, proposed: P7 through P11 below implement the MADR's adoption amendment. They are not approved
for execution. Do not start them on the strength of the earlier execution approval.

### P7 — Re-ground the adoption gaps

1. Re-read `go-selfupdate-lib` v1.26.3 `Job.Reconcile`, `Restore`, `reload`, `Stop`, `WaitHealthy`, `Status`,
   `HandOffIfInside`, and the systemd and SCM equivalents. Amend line references if HEAD is not `31218e1`.
2. Pin A1 through A8 with characterization tests against the current tree before changing it: update refresh rewrites a
   matching plist; refresh of a non-running job disables; restart uses two jobs; `Settle` is negative; Linux start does
   not call the shared controller.

Exit criterion: each A1 through A8 row is either reproduced by a named test or withdrawn with evidence. No production
label is used.

### P8 — One job, one runner, library settle

1. Construct one platform job for an operation and pass that value to stop, start, wait, and reconcile.
2. Set `Options.Runner` to the product runner. Delete the separate launchctl path except `disable`, which must use that
   same runner and the job's target.
3. Call `disable` only from operator stop. Restart, update stop, refresh, and reload must not call it.
4. Remove `Poll.Settle: -1` on Darwin, Linux, and Windows. Do not set a settle below the library default.
5. Treat library bootout exits 0, 3, 36, and 113 as gone. Do not keep a product classifier that disagrees.

Tests: operator stop disables then stops the same job; restart and update do not disable; `WaitHealthy` rejects the
previous PID; a ready observation that dies inside the settle window fails. Plants: a second job, a private runner, a
negative settle, and a refresh that disables.

Exit criterion: one job and one runner per operation; disable is confined to operator stop; settle is the library default.

### P9 — Library plist reconcile, reload, and restore

1. Point the managed installer's reconciler at the platform job's `Reconcile` and `Restore`. Do not spawn
   `service refresh` from update.
2. Leave `RewritePath` unset unless a later approved change says the binary path may move. A mismatch returns
   `service.ErrBinaryMismatch` and does not edit the file.
3. Keep the product renderer for install and for an explicit `service refresh`. Explicit refresh writes the policy,
   then reloads through the library: bootout, wait, bootstrap. It does not kickstart and it does not disable.
4. Use the job's plist path everywhere a definition is read or written. Remove the `os.UserHomeDir` plist path.
5. On Windows, call `scm.Service.Reconcile`. Delete the success result that means the definition is absent while a
   service is installed.

Tests: matching path is unchanged; mismatch is `ErrBinaryMismatch`; rewrite, when enabled in a test double, preserves
mode and owner and refuses a running job; restore returns the previous bytes; explicit refresh reloads without disable.
Plants: a full-plist rewrite on update, a disable during reload, a second home resolver.

Exit criterion: update reconcile is the library reconciler; explicit refresh is the only product rewrite; reload cannot
persist a disable.

### P10 — Shared controller, status, and handoff

1. Route Linux and Windows `service start`, `service stop`, and `service restart` through the shared controller. Remove
   the direct `systemctl` start and stop on those commands, and the parallel Windows start and stop. Install and
   uninstall of the unit or SCM registration stay product-owned.
2. Fill Darwin status and doctor from `Job.Status` plus the product health probe. Keep the old JSON fields. Add the
   library observations without parsing `print` text in product code. Doctor stays nonzero for actionable failures.
3. At process start, call `service.LoadHandOffEnv`. Before `Updater.Run`, call `service.HandOffIfInside` with the
   platform job as the detacher. A handed-off run returns the detached result. A second handoff does not recurse.
4. Do not add `EnabledLifecycle` to the update adapter. Do not set `ReplaceBeforeStop`. Do not add a signature verifier.

Tests: Linux and Windows command argv goes to the fake job and waits for health; status fields come from the fake
`Status`; inside-service update detaches once; outside-service update does not. Plants: a direct `systemctl start` on
the command path, a product `print-disabled` parser, a recursive handoff.

Exit criterion: interactive control and update control share the library job on every supported platform; handoff is
used only when the process is inside the service.

### P13 — Make the command build without test files

CI run `45669857778` failed Darwin and Windows at `go build ./...` because `launchdDefinitionProgram` uses `plistXML` and `plistXMLValue`, which exist only in `service_definition_test.go`. Linux vet and test passed for that reason. Do not call the library's unexported plist reader. Do not add a second decoder.

1. Move `plistXML` and `plistXMLValue` from `cmd/mcp-server-magictools/service_definition_test.go` into `cmd/mcp-server-magictools/service_definition.go`. The file is compiled on every `GOOS`, because `update_service.go` references the decoder unconditionally.
2. Add `decodePlistDict`. It unmarshals with the moved types, returns an error when `len(keys) != len(values)`, and returns the same map `plistValues` builds today, including the element name, trimmed text, and string items.
3. Change `launchdDefinitionProgram` to call `decodePlistDict`. Keep the current read order: `Program` text, otherwise the first `ProgramArguments` item, otherwise an error. A missing file stays `os.IsNotExist`. A symlink resolution failure still returns the unresolved path.
4. Change `plistValues` to call `decodePlistDict`. Delete the copied unmarshal loop.
5. Add `TestLaunchdDefinitionProgramRejectsMismatchedDict`: two keys and one value is an error, and the error is not `os.IsNotExist`.

Exit criterion: `go build -o /dev/null ./...` passes on this Darwin host, and the binary is not executed. The existing round-trip test still passes.

### P11 — Conformance, docs, and gates

1. Run `servicetest.VerifyLifecycle` against the product wrapper over a fake lab, including absent, stopped, running,
   slow stop, and a start that never becomes healthy.
2. Update the service guide: update does not rewrite a matching plist; explicit refresh does; stop is the persistent
   disable; health waits at least 10 seconds; status fields name their source.
3. Re-run the hermetic package tests, `make check-records`, and `make markdownlint`. Full-module, race, and live-helper
   gates stay on the fleet hosts and must not target the production label.
4. Record results in this execution record. Do not mark the plan implemented while a decided amendment row lacks proof,
   or while the original live S06-S14 and S23-S28 probes remain open.

Exit criterion: conformance violations are zero or recorded as amendment deviations; docs match the amendment; no
production service was mutated.

### Amendment traceability

| IDs | MADR amendment rows | Phases |
| --- | --- | --- |
| A1, A2, A7 | Matching reconcile, reload without disable, one plist path | P7, P9 |
| A3, A4 | One job, one runner, library settle, operator-only disable | P7, P8 |
| A5, A6, A8 | Status, Linux and Windows commands, Windows reconcile, handoff | P10 |
| — | Conformance and docs | P11 |


## State-matrix traceability

S-numbers below map one-to-one to the MADR’s 29 decided rows in order. No row is satisfied solely by a unit test where the
MADR requires live behavior.

| IDs | MADR rows | Implementation/proof phases |
| --- | --- | --- |
| S01-S05 | Absent install; no-force existing; force existing; XML values; relative executable | P1 fixtures; P2/P3 install integration |
| S06-S12 | Stop running; start stopped; start running; restart; clean exit; nonzero exit; crash | P2 probes; P3 live lifecycle |
| S13-S16 | SIGTERM stop; drain timeout; loaded updater start; update originally running | P2/P3 timeout and stop; P4 updater |
| S17-S19 | Update originally stopped; running-update recovery; stopped-update recovery | P4 state-machine tests |
| S20-S22 | Stale/reused PID; booting 503; degraded 200 | P3 readiness fixtures and identity tests |
| S23-S25 | Print denied/transient; disabled/approval; no GUI/mismatched sudo | P2 resolver/adapter; P3 doctor/live diagnostics |
| S26-S28 | Concurrent invocation; uninstall absent; uninstall running | P2 lock; P3 unit and live tests |
| S29 | Log files and permissions | P1 plist; P3 filesystem/docs |

Every S01-S29 row also has at least one planted regression assigned in its implementing phase; P6 repeats all plants.

## Verification

### Hermetic gates

```bash
gofmt -l cmd internal
go vet ./cmd/mcp-server-magictools ./internal/testsafety
go test ./cmd/mcp-server-magictools ./internal/testsafety
go test -run 'TestService|TestLaunchd|TestUpdate|TestRefresh' ./cmd/mcp-server-magictools
```

Run the real test names once they exist; the filtered command is illustrative, not permission to report zero selected
tests as success. Capture package test counts and ensure each required test executed.

### Darwin live gate

Run the lifecycle harness from a scratch clone, never from the primary checkout. It builds a standalone test helper,
uses a randomized `local.magictools.test.*` label and exact-label cleanup, and records `plutil`, absent print/bootout,
duplicate/disabled bootstrap, clean/nonzero/crash supervision, bounded bootout, and persistent stop. CI’s macOS 15 run is
additional coverage, not a substitute for the observed macOS 27.2.1 host.

### Repository gates

Use the existing Makefile and host queue, not a local unqueued heavy run:

```bash
make check-records
make markdownlint
git diff --check
```

Run `go test ./...`, race, lint, and cross-platform native tests through the approved host workflow. Do not run a built
module binary as a smoke test on the live-service machine.

## Rollout and Rollback

1. Land code and tests without modifying the installed service. Build artifacts stay out of committed records unless already intentionally tracked by a separate decision.
2. The owner performs migration from a verified binary. Capture the service’s original running state and exact plist backup first.
3. Refresh the definition with the new binary; restart only a previously running service; verify manager state and HTTP readiness independently.
4. Validate operator stop persistence and subsequent explicit start during an owner-approved maintenance window. An agent does not perform these operations on the live service.
5. If migration fails, stop any newly started replacement, restore the prior binary and exact plist bytes, and restore only the original running state. Report all recovery errors and preserve diagnostics.
6. Reverting source after rollout does not rewrite an installed plist. Operational rollback must explicitly restore the old definition; otherwise the new KeepAlive/timeout/process/log policy remains active.

## Rollback

Implementation rollback is phase-scoped: revert uncommitted source edits or revert the reviewed phase commit only with
owner approval. Never roll back by running the production uninstaller, deleting the operator’s plist, database, logs, or
state, or killing a recorded PID. If a phase discovers unsafe update primitives or irreconcilable CLI behavior, stop,
preserve the tree, amend the MADR/PLAN, and await re-approval.

## Completion Criteria

* All MADR decided state rows have named tests and observed results; out-of-scope rows remain untouched.
* Current-host launchd behavior is pinned by isolated live tests, including clean exit, nonzero exit, crash, stable stop, restart, and timeout.
* All plist fixtures parse, XML values round-trip, atomic-write failure plants leave old bytes unchanged, and no symlink destination is followed.
* Update success and failure preserve initial running/stopped state and restore matching binary/definition state before restart.
* No readiness path accepts PID existence alone; no lifecycle path sends SIGKILL to a raw state-file PID.
* All real launchctl operations use the shared bounded adapter and the process-safety guard passes.
* Hermetic, Darwin-live, full-module, race, native cross-platform, vet, lint, records, Markdown, and diff gates have recorded passes.
* Any environment-dependent skip is listed as unfinished proof; it cannot be silently counted as success.
* Documentation and migration instructions match the shipped behavior, and the owner has not been bypassed for live-service acceptance.
* The execution record numbers every phase pass and records every deviation and unperformed step.

## Execution record

The owner accepted the MADR and approved execution on 2026-11-25, including adoption of go-selfupdate-lib v1.26.3.
The gates below are the completion record. The installed service was not migrated.

| Phase | Evidence and result |
| --- | --- |
| P0 | PASS. Confirmed reviewed HEAD `6dc74e0600ae944cf92d4595d7b3569d5ec31601`, macOS 27.2.1/arm64 and Darwin Bootstrapper 7.0.0. Verified the old updater always restarts after post-stop failure. Verified the new library’s two-phase managed installer preserves stopped state and stops unhealthy replacements before rollback. |
| P1 | Core implementation and focused tests PASS. XML escaping/round-trip, plist policy, absolute-path rejection, `plutil -lint`, atomic replacement, symlink refusal, and Windows directory-sync handling tested. Write/rename/directory-sync failure plants and refresh failure/restore tests pass, including exact-byte rollback. |
| P2 | Scratch-only probes PASS on macOS 27.2.1 with a unique test label and cleanup. Observed absent print=113, absent bootout=3, duplicate bootstrap=5, disabled bootstrap=5, and successful bounded bootout. Clean, nonzero, and crash helpers each restarted with a different PID, then disable plus bootout persisted. No production label or service was touched. Full timeout, permission, concurrency, and uninstall matrices remain open. |
| P3 | Core implementation PASS in focused tests. Shared Darwin/systemd/SCM jobs, persistent disable, state start tokens, exact executable binding, stale/reused PID rejection, unrelated HTTP rejection, 503 booting and degraded 200 readiness covered. Full install/status/doctor integration and all planted regressions remain open. |
| P4 | Adoption and focused tests PASS. go-selfupdate-lib v1.26.3 locked; x/mod 0.42.0, x/sys 0.49.0 and x/term 0.47.0 updated. Its direct `TestManagedStartRule`, `TestManagedStopsBeforeRollback`, and `TestManagedCommitRefusedStartRule` pass. Product job reuse across start/health is tested. Full product update/reconcile integration remains open. |
| P5 | Documentation updated for lifecycle, identity status, doctor failures, logs, migration, and scratch-only launchd probes. `make check-records`, `make markdownlint`, and `git diff --check` pass on the final tree. |
| P6 | Final-tree gates PASS: full Darwin `go test ./...`, full Darwin `go test -race ./...`, full `go vet ./...`, and `make lint` (0 issues). Native Linux and native Windows affected packages pass after portability fixes. Record/Markdown/diff gates pass. Full S01-S29 integration and mutation coverage is not complete; the plan remains in progress rather than being marked implemented. |

Deviations: the adopted library supplies managed update sequencing and bounded `bootout` plus polling, so no private
update engine was written. Its stop does not use `bootout --wait`; the optional flag was separately proven in scratch.
The product does not expose `EnabledLifecycle` on the update adapter, so an initially stopped service remains stopped.
Live helper/probe files were removed from the primary tree and run only in `/tmp/magictools-launchd-scratch.7QIhze/repo`.
The first queued scratch run timed out before execution; its ticket was removed. The Windows host transport failed on
tar extended headers, so a zip-based scratch run supplied the actual Windows result. A later combined full vet/test call
was declined and did not run; a second final full-suite request was also declined and was not retried.

Live probes, 2026-11-25: `TestLaunchdLiveProbes` passed from a scratch tree with `LAUNCHD_LIVE=1` and
`-tags=launchdlive` on macOS 27.2.1, Darwin Bootstrapper 7.0.0. The label prefix was `local.magictools.test.`. No
production label was used. Observed: absent print is not success; absent bootout is gone; disabled bootstrap fails and
is not classified absent; clean exit, nonzero exit, and `SIGABRT` each restarted with a different PID; disable plus
bootout stayed down for 2 seconds; restart changed the PID; kickstart without `-k` kept the PID; another GUI domain's
print was not classified absent; uninstall removed the helper plist and a later bootout was gone. One `bootout --wait`
of a SIGTERM-ignoring helper blocked until a 1-second deadline with the helper still alive. A later run returned in 17
ms because launchd had already reaped that helper. The test issued no signal to the helper PID. S26 remains the hermetic
lock test. Full-module and race gates are still not re-run.

Amendment, 2026-11-25: the owner accepted the amendment and approved P7 through P11, including commit and push to main between steps.

| Phase | Evidence and result |
| --- | --- |
| P7 | Gaps A1 through A8 re-read at `31218e1` against v1.26.3. Replacement tests now pin the corrected contract rather than the old defects. |
| P8 | One job is passed through stop and restart. `Options.Runner` is the product runner. `Settle` is unset. Bootout exits 3, 36, and 113 are gone. Operator stop still disables first. |
| P9 | Update reconcile and restore delegate to the job. Update does not spawn `service refresh`. Explicit Darwin reload calls that job's `Stop` and then bootstrap on the shared runner. It does not disable or kickstart. The library's `reload` is unexported, so this is the public equivalent. Windows refresh uses `scm.Service.Reconcile` when a service is installed. |
| P10 | Linux and Windows start, stop, and restart use the shared controller. Darwin status and doctor read `Job.Status` through a seam. `LoadHandOffEnv` runs at process start. `HandOffIfInside` runs before `Updater.Run` when a definition exists. |
| P11 | `servicetest.VerifyLifecycle` passed over `updateLifecycle` delegating to `servicetest.FakeService`, with 200 ms bounds. Package tests for `./cmd/mcp-server-magictools` and `./internal/testsafety` passed on this Darwin host. |
| P12 | Completion gates, 2026-11-25. Queued `go test -count=1 ./...` passed on this Darwin host (32s) and, with `-race`, passed on this Darwin host (31s). The same command passed on the fleet Linux distro (33s) and, after the Windows refresh fix, on the fleet Windows host (74s). `go vet ./...` and `make lint` passed on this Darwin host, 0 issues. Windows refresh uses the SCM reconciler only when no on-disk definition path is reported, so a stubbed path still uses the file refresh. The installed service was not migrated. |
| P13 | `plistXML` and `plistXMLValue` moved to `service_definition.go`. `decodePlistDict` rejects a mismatched key and value count. `launchdDefinitionProgram` and `plistValues` call it. `go build -o /dev/null ./...` passed on this Darwin host; the binary was not executed. `TestLaunchdDefinitionProgramRejectsMismatchedDict` and the existing `TestLaunchdDefinition` tests passed. |

Continuation, same working tree: mismatched `SUDO_UID` is refused before path use. Lifecycle install, uninstall, refresh,
and shared start/stop/restart take a per-label lock; a cancelled waiter does not release the holder. `launchctl` results
are classified by exit status and context error: only the pinned absent-bootout status is `os.ErrNotExist`; a deadline
and a nonzero permission-style status are not. Uninstall of a missing definition does not call stop. Uninstall of a
present definition stops through the lifecycle seam and leaves the file in place when stop fails. Status JSON keeps the
old fields and adds registration, enabled, manager process, readiness, and stale. The real v1.26.3 managed installer,
driven with fakes, preserves a stopped service, stops a replacement before restore, and joins the original health and
reconcile errors. The product update adapter does not expose `EnabledLifecycle`. Package tests for
`./cmd/mcp-server-magictools` and `./internal/testsafety` passed on this Darwin host. Full-module, race, and live-helper
gates were not re-run.
