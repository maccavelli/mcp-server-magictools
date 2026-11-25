---
status: accepted
date: 2026-11-25
decision-makers: repository owner
consulted: "Apple's installed Darwin 7.0.0 launchd documentation and current Service Management documentation; source and dependency review at 6dc74e0600ae944cf92d4595d7b3569d5ec31601"
informed: repository maintainers
---
# Make the per-user LaunchAgent lifecycle correct, restartable, and verifiable on current macOS

## Context and Problem Statement

MagicTools installs a standalone per-user LaunchAgent rather than an app-bundled helper. At the reviewed commit, install,
start, stop, restart, uninstall, status, doctor, and self-update use separate lifecycle implementations. Their behavior
contradicts both the CLI's promises and launchd's documented semantics. An operator cannot reliably stop the service, an
ordinary configuration error does not cause recovery, and an update can report success against an old process or start a
service that was deliberately stopped.

The target is the currently observed host: macOS 27.2.1 (26B131), arm64, with Darwin Bootstrapper 7.0.0. The current
product distribution is a standalone binary, so Apple’s macOS 13-and-later `SMAppService` app-bundle model is not a
drop-in replacement. This decision retains `~/Library/LaunchAgents` while fixing the existing model and adopting the
current facilities that apply to it.

### Evidence and defect inventory

The identifiers below are used by the decision and state table. All source locations are at the reviewed commit.

| ID | Severity | Observed implementation and consequence |
| --- | --- | --- |
| L1 | High | `service stop` sends SIGTERM with `launchctl kill` (`service.go:963-973`) but does not disable or boot out the job. It returns before exit. `SuccessfulExit=false` restarts nonzero exits; `Crashed=true` restarts crash signals. Nothing in this path records a persistent operator stop, so a nonzero or crash termination can be resurrected. |
| L2 | High | The plist ORs `SuccessfulExit=false` with `Crashed=true` (`service.go:488-494`). Apple documents that multiple KeepAlive conditions are ORed, `SuccessfulExit=false` means nonzero exit, and `Crashed` covers crash signals. A normal exit status 1 therefore restarts. Apple does not document `SuccessfulExit` as covering signal deaths, so that case must be measured rather than inferred. |
| L3 | High | The updater starts with `launchctl bootstrap gui/<uid> <path>` (`update_service.go:187-201`) without first booting out an already-loaded job. Apple documents bootstrap as loading definitions; it is not an idempotent replace operation. A loaded job can make update startup and rollback fail. |
| L4 | High | `Running` and `WaitHealthy` trust any live PID in `service.state` (`update_service.go:66-75,99-162`). PID reuse can produce a false positive. A stale state file can make health pass before the new process is ready. `/health` already returns 503 before boot completes (`main.go:912-925`) but is not consulted. |
| L5 | High | `mcplib` v1.4.1 `managed.go:88-96,113-131` always restarts after reconcile/start/health failure, even when the service was initially stopped. Rollback restores the definition while the new binary can still be running, then restarts using the restored definition and a mixed binary/definition state. |
| L6 | High | Install writes the plist and ignores both bootout results before bootstrap (`service.go:645-703`). On failure it neither restores the previous definition nor reliably boots out the newly loaded one. A failed reinstall can leave the old service stopped and the new file installed but inactive. |
| L7 | High | The plist is rendered with `text/template`, not XML escaping (`service.go:16,452-507,622-667`). A synthetic path containing `&` made `plutil -lint` fail with “unknown ampersand-escape sequence at line 9.” Binary paths, bind addresses, and endpoint URLs can therefore produce malformed XML. Existing tests only search substrings (`service_hardening_test.go:64-89`). |
| L8 | Medium | Uninstall ignores bootout errors, waits only 20 seconds for a raw recorded PID, and can SIGKILL that PID (`service.go:717-760,841-863`). The application budget is 30 seconds and plist ExitTimeOut is 35. The current host offers `bootout --wait`; the code does not use it. PID identity is not checked before escalation. |
| L9 | Medium | `service start` treats every `launchctl print` error as “not loaded,” then bootstraps (`service.go:919-934`). Permission, domain, and transient errors are therefore misclassified. Apple explicitly says `print` output is not API; only its exit status is used here, which is acceptable, but its errors are not distinguished. |
| L10 | Medium | Refresh rewrites the plist but `reloadServiceManager` is a no-op on Darwin (`service_refresh.go:95-137,266-275`). Correctness depends on the updater’s separate start path. The two renderers independently derive the same values and can drift. Writes truncate the live definition instead of atomically replacing it (`service_refresh.go:161-175`). |
| L11 | Medium | Status and doctor rely on the state file and PID existence; doctor checks only that a plist exists (`service.go:233-282,1044-1140`). They do not distinguish loaded, running, ready, disabled, or stale state. Doctor findings do not produce a failing exit status, as its guide acknowledges. |
| L12 | Medium | `ProcessType=Background` (`service.go:484-485`) opts this request-serving orchestrator into background resource limits. Apple documents `Standard` as the normal default and `Adaptive` as activity-dependent. No measurement establishes that throttling is desirable for IDE-facing latency. |
| L13 | Medium | The installer resolves the launchd target and chowns files for `SUDO_UID`, but start, doctor, logs, refresh, and updater paths variously use the current UID or `os.UserHomeDir` (`service.go:509-538,912-970,1009-1021`; `service_refresh.go:37-57,198-201,235-258`). They can address different users and homes. The plist also omits an explicit HOME even though config resolution uses the process home. |
| L14 | Low | stdout and stderr share `magictools_service.log`, while the application also writes its own log. `internal/util/io.go:60-96` caps only the application log, at 50 MiB on open and by truncating it; the plist output has no cap. The plist has no Umask; launchd documents that created output-file permissions reflect the job umask. |
| L15 | Gap | There is no safe macOS lifecycle integration test. The plist test checks only selected strings; updater tests inject lifecycle functions and do not execute real `launchctl` argument behavior. No test covers crash, nonzero exit, deliberate stop, missing domain, disabled service, slow drain, stale PID, failed bootstrap, or rollback. |

### Official constraints established during review

* `launchd.plist(5)` on the target host defines `ExitTimeOut` as the SIGTERM-to-SIGKILL interval, warns against zero,
  defines `AbandonProcessGroup` cleanup, and recommends SIGTERM handling. The current 35-second allowance exceeds the
  application’s 30-second drain and should be retained.
* The same manual defines KeepAlive OR semantics, `RunAtLoad`, process types, output paths, and
  `AssociatedBundleIdentifiers`. It cautions against speculative startup where demand activation is suitable; this HTTP
  service currently binds its own listeners and does not check in launchd sockets, so socket activation is out of scope.
* `launchctl(1)` documents `bootstrap`, `bootout`, `enable`, `disable`, `kickstart -kp`, `kill`, and `print`. Its output
  is not a stable automation API. The target host additionally reports `bootout --wait` for one service target, with an
  indefinite-block warning; use therefore needs an outer bounded context.
* Apple’s current “Updating helper executables from earlier versions of macOS” says `SMAppService` replaces installing
  legacy plists for helpers inside an app bundle. It provides `statusForLegacyPlist(at:)` for legacy authorization state
  and `AssociatedBundleIdentifiers` for app attribution. Those APIs require an app/bundle context and are not available
  merely by adding a plist key to this standalone binary.
* Do not enable `EnableTransactions` or `EnablePressuredExit` without implementing the required XPC transaction
  behavior. The Go service has no such integration; setting either key would be decoration, not optimization.

## Decision Drivers

* Stop must remain stopped until an explicit start; restart must always produce a new, ready process.
* launchd, not `service.state`, owns registration and process supervision; application health owns readiness.
* Updates must preserve the prior running/stopped state and restore a coherent binary and definition on failure.
* No raw-PID liveness check may authorize health, ownership, or SIGKILL escalation.
* Installation and refresh must be valid XML, durable, atomic, and reversible.
* Use current macOS behavior where it fits the standalone distribution; do not add a second packaging architecture here.
* Live verification must never signal or alter a service the test did not create.

## Considered Options

* A. Repair and unify the standalone per-user LaunchAgent, with launchd-owned state, real readiness, bounded lifecycle operations, and current plist policy.
* B. Keep the existing commands and patch only the plist’s KeepAlive dictionary.
* C. Replace the standalone service with an app-bundled `SMAppService` agent now.
* D. Replace LaunchAgent supervision with a system LaunchDaemon.

## Decision Outcome

Chosen option: "A. Repair and unify the standalone per-user LaunchAgent", because it fixes the demonstrated lifecycle and
rollback defects without requiring a new app bundle, privileged system service, or XPC socket architecture.

The 2026-11-25 adoption amendment below was accepted by the owner, who approved its execution.

### Decision

1. **One controller and one identity.** Introduce one Darwin lifecycle controller used by install, start, stop, restart,
   uninstall, refresh, doctor, and update. Resolve one target home, UID, username, label, plist path, log directory, and
   service target at entry; reject sudo invocation with an unsupported or unavailable target domain rather than silently
   mixing users. Share one typed plist renderer between install and refresh. Pass explicit HOME and a deterministic PATH.
2. **Represent manager state explicitly.** Distinguish absent, loaded/stopped, running, disabled, unknown/error, and
   application-ready. Use `launchctl print <service-target>` only as an exit-status probe; never parse its text as an API.
   A print failure is unknown unless an absent result is established by the subsequent bounded bootstrap behavior. Do
   not build a boolean enabled/disabled classifier on `print-disabled`: its output is human-facing and not stable API.
   A disabled job is diagnosed by a failed bootstrap/kickstart followed by bounded `enable` and retry, with every step
   and error reported.
3. **Make stop stable.** Interactive `service stop` disables the service, then performs a bounded bootout by service
   target and waits for manager-confirmed process exit. Disable first so supervisor conditions cannot resurrect it.
   Apple documents that disable persists across boots until enable. Stop does not delete the plist. The product wrapper
   issues `disable` before calling go-selfupdate-lib’s bounded stop, which uses `bootout` plus bounded manager/process
   polling rather than the optional `bootout --wait` flag. `service start` uses the library’s enable/bootstrap-or-kickstart
   behavior and readiness probe. `service restart` boots out without persistent disable, then uses the same start and
   readiness path. Update recovery delegates stop/start/wait ordering to go-selfupdate-lib v1.26.3; it stops a replacement
   before restoring definitions and binaries and preserves the original running state unless `EnabledLifecycle`
   deliberately says an installed stopped job should start.
4. **Use unconditional supervision with an explicit operator stop.** Set `KeepAlive=true` and retain `RunAtLoad=true`
   and `ThrottleInterval=5`. Apple explicitly defines true as unconditional keepalive. This restarts clean, nonzero,
   and signal termination without relying on undocumented interactions between exit status and signals. Deliberate
   stop remains stable because it disables and boots out the job; restart and start explicitly enable it. Document
   that `service stop`, not a clean application exit, is the supported way to leave the service down. Remove the
   `SuccessfulExit`/`Crashed` dictionary because it cannot express that policy as directly.
5. **Retain bounded graceful shutdown and add explicit resource and log policy.** Keep `ExitTimeOut=35` for the existing
   30-second drain. Use bounded bootout plus manager/process-exit polling and return timeout/cancellation distinctly;
   the scratch live probe separately proves the host’s optional `bootout --wait` behavior. Never
   follow a timed-out launchctl command with SIGKILL to a raw state-file PID. Set `ProcessType=Standard`; revisit
   `Adaptive` only with XPC activity integration and measurements. Do not set `AbandonProcessGroup`: sub-servers have
   separate process groups, so the key would not replace their existing drain/reaper. Set plist `Umask` to octal `077`.
   Redirect launchd stdout/stderr to the application’s existing `magictools_debug.log`, whose open path caps it at
   50 MiB; eliminate the second uncapped `magictools_service.log`. Document that the cap truncates on application open
   rather than rotating while the process runs.
6. **Validate and atomically install definitions.** Render typed data with XML-safe escaping, require absolute executable
   and output paths, lint with `plutil -lint`, write a same-directory temporary file, fsync, chmod 0600, and rename.
   Preserve a previous-definition backup. Bootout is best-effort only for a genuinely absent job; all other lifecycle
   errors remain visible. If bootstrap or readiness fails, restore the previous file and prior loaded/running/disabled
   state where possible, and return joined restoration errors. Existing-definition install without `--force` is a no-op,
   not a successful health claim.
7. **Adopt go-selfupdate-lib and make update state-preserving.** Replace `mcplib/selfupdate` with
   `github.com/maccavelli/go-selfupdate-lib/selfupdate` v1.26.3. Its managed installer preserves an initially stopped
   service, stops a started replacement before restoring definition and binary, exposes two-phase replacement, and
   supplies launchd/systemd adapters. Use its managed installer as the update engine and its launchd `Job` behind the
   product wrapper for registration/process control. Keep product-specific disable-before-stop, XML rendering,
   state-token identity, and HTTP identity checks here; the library does not implement all of them. Its enabled-start
   probe parses human-facing `print-disabled`, so the product adapter must not expose `EnabledLifecycle` when an
   originally stopped service must remain stopped. Update `go.mod` and `go.sum` with the first import. Remove `mcplib`
   only if no other import remains. The required `x/mod v0.42.0`, `x/sys v0.49.0`, and `x/term v0.47.0` versions rise
   with adoption and receive the normal full-module regression run.
8. **Use readiness and process identity, not PID existence.** Poll the configured loopback `/health` endpoint until HTTP
   200, while also verifying that the state file belongs to the expected binary and new process start and that its PID
   still exists. A state file is diagnostic evidence, never proof by itself. Time out as failure without deleting the
   only diagnostic evidence. Preserve degraded HTTP 200 as ready, because the orchestrator can serve native and
   surviving tools. Status reports registration, process, readiness, disabled/authorization, binary, and stale-state
   separately. Doctor returns nonzero for actionable failures and prints remediation.
9. **Adopt applicable current-macOS integration without inventing bundle identity.** Document the per-user GUI-session
   requirement and surface launchd’s disabled/approval diagnostics. Add `AssociatedBundleIdentifiers` only when a real,
   signed app bundle identifier exists and its signing-team relationship is verified; a bare standalone binary has none
   today. Keep the legacy-plist authorization check and `SMAppService` migration as follow-on packaging work.
10. **Prove lifecycle behavior safely.** Use an isolated test label/domain and helper process owned by the test, never
    the production label, default database, or live service. Pin the actual Darwin CLI probes in live-tagged tests, as
    required for decisions resting on external CLI behavior. Keep non-live tests hermetic with a fake executor and
    parsed-plist golden tests.

### Consequences of the chosen option

* Good, because stop, start, restart, uninstall, and update acquire one consistent meaning.
* Good, because ordinary startup failures are supervised and readiness no longer depends on stale PID state.
* Good, because stopped services stay stopped across failed updates, and rollback no longer knowingly combines a new process with an old definition.
* Good, because malformed XML, partial writes, and destructive failed installs are detected before being presented as success.
* Neutral, because `KeepAlive=true` restarts even a clean application exit; the supported persistent stop is `service stop`, which disables and boots out the job.
* Neutral, because `ProcessType=Standard` removes intentional background throttling; production latency should be observed after rollout.
* Bad, because a unified controller and real Darwin integration suite are a broader change than editing the plist.
* Bad, because launchctl exposes no stable structured status API; the implementation must treat manager queries and readiness as separate, fallible observations.
* Bad, because an app-bundled SMAppService migration remains unimplemented and requires separate packaging, signing, and authorization decisions.

### Confirmation

* Every decided state-table row has a fake-executor unit test; plist fixtures are parsed, not substring-matched.
* Darwin live-tagged tests create and clean up only their own uniquely labeled helper, with an isolated HOME and no production database.
* Tests cover clean zero exit, nonzero exit, crash signal, stable stop, restart, slow drain, disabled service, absent domain, malformed values, failed bootstrap, stale/reused PID, and update rollback from both running and stopped states.
* `make check-records` and `make markdownlint` pass for the record; affected Go tests and the repository’s queued Darwin gates pass during implementation.
* No test invokes the production label or signals any process it did not start.

### State table

| State or input | Disposition | Required outcome and proof |
| --- | --- | --- |
| Definition absent on install | decided | Validate and atomically create; bootstrap; require readiness. Fake argv/filesystem plus isolated Darwin helper. |
| Definition present without force | decided | Return unchanged/no-op, do not stop it or claim a new install. Unit test. |
| Definition present with force | decided | Preserve backup and prior state; replace safely; rollback on failure. Unit and Darwin tests. |
| XML metacharacters or non-ASCII in supported values | decided | Escaped plist passes `plutil -lint`; original values round-trip. Parsed fixture, including the reproduced ampersand case. |
| Relative executable path | decided | Reject before changing existing state. Unit test. |
| Loaded and running; `service stop` | decided | Disable then bounded bootout; remains stopped. Fake argv and live helper. |
| Loaded and stopped; `service start` | decided | Enable and start existing definition; wait for HTTP 200. Fake argv and live helper. |
| Running; `service start` | decided | Do not kill a healthy process; report already running/ready. Unit test. |
| Any loaded state; `service restart` | decided | Replace process, retain enabled state, wait for new readiness. Live helper checks changed PID/start token. |
| Clean status-zero application exit | decided | launchd restarts under unconditional KeepAlive; report the new process, not an operator stop. Live helper. |
| Ordinary nonzero application exit | decided | launchd restarts with throttle; eventual readiness. Live helper. |
| Crash signal | decided | launchd restarts; no raw-PID application killer involved. Live helper using its own child. |
| Deliberate SIGTERM during stop | decided | No resurrection after disable/bootout; bounded graceful drain. Live helper. |
| Drain exceeds launchctl bound | decided | Return timeout with service state/diagnostics intact; never SIGKILL a raw recorded PID. Fake clock/executor and bounded live helper. |
| Loaded job during updater start | decided | Bootout then bootstrap consumes refreshed definition. Exact fake argv and Darwin test. |
| Update while originally running | decided | Replace, reconcile, start, require readiness. Integration test. |
| Update while originally stopped | decided | Preserve stopped state; never start merely to prove health. Unit test of lifecycle call sequence. |
| Reconcile/start/readiness failure after running update | decided | Restore matching binary/definition, attempt one prior-state restart, join all errors. Unit test. |
| Reconcile failure after stopped update | decided | Restore disk state and remain stopped. Unit test. |
| Stale or reused PID in state file | decided | Does not prove running/healthy; readiness and identity checks reject it. Unit test. |
| Booting `/health` returns 503 | decided | Continue bounded readiness polling, then fail without claiming success. HTTP fixture. |
| Degraded `/health` returns 200 | decided | Ready with degraded status preserved. HTTP fixture. |
| `launchctl print` denied/transient failure | decided | Unknown/error, not automatically absent; no destructive fallback. Fake executor. |
| Disabled or login-item approval required | decided | Diagnose distinctly and provide remediation; do not report ready. Probe/fixture on supported macOS. |
| No GUI domain or mismatched sudo target | decided | Fail before mutation with explicit session/user guidance. Fake resolver/executor. |
| Concurrent lifecycle invocations | decided | Per-label lock serializes operations; cancellation does not release another owner’s lock. Unit test. |
| Uninstall absent service | decided | Idempotent; no SIGKILL; remove only owned definition/state. Unit test. |
| Uninstall running service | decided | Bounded bootout/wait before deleting state; errors prevent false success. Darwin helper. |
| Log files and permissions | decided | Both launchd streams use the application log; Umask is 077 and the plist is not group/world-writable. Parsed plist and filesystem tests pin the existing 50 MiB on-open cap. |
| App-bundle SMAppService and AssociatedBundleIdentifiers | out of scope | Requires app packaging/signing and a separate decision. |
| launchd socket activation, XPC transactions, pressured exit | out of scope | Current server owns listeners and has no XPC check-in/transaction integration. |
| System LaunchDaemon or running before login | out of scope | Product remains a per-user service; privilege and security implications need a separate decision. |

## Consequences

The option-level trade-offs are recorded above. The operational effects are:

* The service remains per-user and app-bundle migration remains future work.
* Existing installations need a controlled definition refresh and restart before the corrected policy takes effect.
* Operators gain stable stop and verifiable readiness; automation must consume the expanded status/doctor results rather than infer health from a PID.

## Pros and Cons of the Options

### A. Repair and unify the standalone per-user LaunchAgent

* Good, because it addresses every demonstrated defect while preserving current installation and privilege boundaries.
* Good, because current bootstrap/bootout/enable/kickstart behavior and readiness can be tested without an app bundle.
* Neutral, because legacy plist authorization still depends on the macOS login-item environment.
* Bad, because launchctl remains an external CLI with limited stable machine-readable status.

### B. Patch only KeepAlive

* Good, because `KeepAlive=true` is small and directly supplies the intended always-supervised policy.
* Bad, because it cannot fix stale health, updater bootstrap, rollback, XML, uninstall races, or split identity.
* Bad, because stable stop requires disable/bootout as well as a plist change; editing KeepAlive alone leaves the CLI semantics broken.

### C. Adopt app-bundled SMAppService now

* Good, because Apple recommends it for macOS 13-and-later app helpers and it integrates registration and authorization.
* Good, because bundled helpers and plists can be updated together.
* Bad, because this repository distributes a standalone executable, not the required app bundle.
* Bad, because packaging, signing, login-item consent, migration, and rollback are a separate architectural change.

### D. Use a system LaunchDaemon

* Good, because it can run independently of a GUI login.
* Bad, because it changes user configuration, credential, filesystem, and privilege boundaries.
* Bad, because the product currently uses per-user configuration and sub-servers and has no requirement established here for system-wide execution.

## More Information

### Evidence sources

* Reviewed tree: `6dc74e0600ae944cf92d4595d7b3569d5ec31601`, clean at investigation time.
* Host probes: `sw_vers`, `uname -m`, `launchctl version`, `launchctl help bootstrap`, and `launchctl help bootout` on
  2026-11-24 local time. No running service was queried or changed.
* Apple, installed `launchd.plist(5)` and `launchctl(1)` manuals supplied by the target OS.
* [Apple: Updating helper executables from earlier versions of macOS](https://developer.apple.com/documentation/servicemanagement/updating-helper-executables-from-earlier-versions-of-macos).
* [Apple: SMAppService](https://developer.apple.com/documentation/servicemanagement/smappservice).
* [Apple: SMAppService.statusForLegacyPlist(at:)](https://developer.apple.com/documentation/servicemanagement/smappservice/statusforlegacyplist(at:)).
* [Apple: SMAppService.openSystemSettingsLoginItems()](https://developer.apple.com/documentation/servicemanagement/smappservice/opensystemsettingsloginitems()).
* `mcplib` v1.4.1 `selfupdate/managed.go` in the module cache, used to establish rollback sequencing.
* `0010-MADR-make-tests-unable-to-signal-real-processes.md` and `docs/guides/testing-and-process-safety.md` define the process-safety constraints for all live tests.

### Grounding amendment, 2026-11-25

The first proposed draft selected `KeepAlive={SuccessfulExit=false}` and asserted that it also covered abnormal signal
termination. The installed `launchd.plist(5)` proves nonzero-exit behavior for that key and signal behavior for
`Crashed`; it does not prove that `SuccessfulExit=false` covers signal deaths. The decision therefore uses the manual’s
explicit unconditional form, `KeepAlive=true`, rather than making crash coverage depend on an inference. Clean zero,
nonzero, crash, and deliberate-stop behavior remain separate required live tests. The same review established that the
existing application log already has a 50 MiB cap on open (`internal/util/io.go:60-96`), so the plan reuses that path
instead of introducing a logging dependency.

### Limits of this investigation

The findings are source-, documentation-, and fixture-grounded; no production lifecycle operation was executed. Actual
restart timing and login-item authorization behavior still require the isolated Darwin tests specified above. The MADR
proposes the decision; it does not approve implementation. A separate implementation PLAN is required before source,
tests, service definitions, or documentation are changed.

### Adoption amendment, 2026-11-25, accepted

Reviewed against `go-selfupdate-lib` v1.26.3 at commit `31218e1`. The owner directed three things: the library's plist
policy is the one to keep, adoption of that library is to be finished, and more of its service API should be used where
it improves the product. This amendment records that direction. It does not authorize execution.

The library does not ship a product LaunchAgent renderer. Its plist policy, in `selfupdate/service/launchd`, is:

* An in-place binary replacement already names the executable. `Job.Reconcile` checks `Program`, otherwise
  `ProgramArguments.0`, and changes nothing when `service.SameExecutable` matches.
* A different path is an error wrapping `service.ErrBinaryMismatch` unless `Options.RewritePath` is set. A rewrite uses
  `plutil -replace`, then `plutil -lint`, and keeps the file mode and owner. A running job is refused before the file
  changes. A loaded, not-running job is reloaded by bootout, the library's stop wait, and bootstrap, without `disable`
  and without kickstart.
* `Restore` puts the previous bytes, mode, and owner back, and reloads only a loaded, not-running job.
* `Stop` is bootout plus a wait bounded by the longer of the poll timeout and the plist's `ExitTimeOut` plus 30 seconds.
  Bootout, not `disable`, is what holds against `KeepAlive`. Exit codes 0, 3, 36, and 113 are gone. `print-disabled` is
  read only inside the library's `Enabled`.
* A negative `Poll.Settle` disables the stability window. The default is 10 seconds, and the launchd running-poll raises
  a non-negative settle to at least that.
* `Job` is one object. `Stop` records the previous PID that `WaitHealthy` must not accept. A second `Job` does not see it.
* `Status` reports installed, loaded, enabled, running, and PID from one probe. `HandOffIfInside` detaches an update
  that would die with the job. `systemd.Unit` and `scm.Service` implement the same Lifecycle, Reconciler, and Detacher
  contracts. `service.ExecReconciler` expects one JSON receipt with `schema_version`, a verdict, and a reload bit.

The tree at `31218e1` adopts the managed installer, the exact asset selector, the strict version policy, and a platform
`Job` for update start, stop, and health. It does not follow the plist policy above.

| ID | Observed wiring at `31218e1` | Required change |
| --- | --- | --- |
| A1 | Update reconcile shells out to `service refresh`, which rewrites the whole plist. | Update reconcile is the platform `Job`'s `Reconcile`. An unchanged path changes nothing. |
| A2 | Refresh of a non-running job calls interactive `Stop`, which `disable`s before bootout. A stopped update is not started again, so the disable can persist. | Reload is the library reload: bootout, wait, bootstrap. No `disable`. No kickstart. |
| A3 | Interactive restart stops a newly constructed job, then starts and waits on another. `previous` is lost. `launchctlRunner` is not `Options.Runner`. | One job per operation. The product runner is `Options.Runner`. `disable` runs only for an operator stop, on that same job's target, before `Stop`. |
| A4 | `Poll.Settle` is `-1`. The first ready observation passes. | Do not set a negative settle. Keep the library's 10-second window. |
| A5 | Status leaves `enabled` and `manager_process` unknown. Doctor asks only `Running`. | Darwin status and doctor display `Job.Status`. The product does not parse `launchctl print` or `print-disabled` itself. |
| A6 | Linux `service start` and `service stop` call `systemctl` and do not wait for the probe. Windows start and stop use the local SCM code. Windows refresh reports that there is no on-disk definition. | Interactive start, stop, and restart on every platform go through the shared controller and its library job, including `WaitHealthy`. Windows reconcile is `scm.Service.Reconcile`. |
| A7 | Refresh and the definition-target check use `os.UserHomeDir`. The job uses `resolveLaunchdTargetFn`. | One resolver. The job's plist path is the only definition path. |
| A8 | An update started inside the service can die in `Stop` (`service.ErrInsideService`) and leave the job down. | Call `service.HandOffIfInside` before `Run`. Call `service.LoadHandOffEnv` at process start. Read the detached result. Do not hand off twice. |

Install-time rendering stays in the product. The library has no renderer for this product's `KeepAlive`, `ExitTimeOut`
of 35, `Umask`, `ProcessType`, `HOME`, or log path. An explicit `service refresh` may still rewrite that policy for
migration. It must then reload through the library, not through interactive stop. An update must not call that rewrite.

`EnabledLifecycle` stays off the update adapter. A stopped service is not started because the plist says `KeepAlive`.
Signature verifiers, `ReplaceBeforeStop`, Task Scheduler, `SMAppService`, socket activation, and XPC transactions stay
out. Checksum verification remains `NewExactAssetSelector`'s required `SHA256SUMS`. No signing keys are in this decision.

#### Amendment options

* E. Finish adoption of the v1.26.3 service contract: library reconcile and reload, one shared job, default settle, status from `Job.Status`, all platforms through that job, and handoff when the update is inside the service.
* F. Keep the product renderer and refresh-on-update, and patch only the disable and split-job defects.
* G. Replace the install-time renderer with a library renderer.

#### Amendment outcome

Chosen option: "E. Finish adoption of the v1.26.3 service contract", because the library already defines reconcile,
reload, restore, settle, status, and handoff, and a second implementation of those is what produced A1 through A8.
Option G is not available: the library does not render this product's plist. Option F keeps two policies for one file.

* Good, because an update no longer rewrites a plist whose program path did not change, and a failed update restores bytes, mode, and owner through the library receipt.
* Good, because refresh and update can no longer persist a disable that the operator did not ask for.
* Good, because restart and update health share the job that saw the old PID, and a crash inside the settle window does not count as healthy.
* Good, because an update started inside the service is detached instead of stopping itself.
* Neutral, because health waits take at least the library's 10-second settle. That is slower than the current first-success probe.
* Neutral, because existing installations do not pick up a new plist policy until an explicit refresh or reinstall.
* Bad, because Linux and Windows interactive commands move onto the library job, which is a wider change than the Darwin defects alone.
* Bad, because `Job.Status`'s enabled bit is the library's `print-disabled` reading. The product may display it. It must not parse that text itself, and it must not treat the bit as a private API.

#### Amendment state table

| State or input | Disposition | Required outcome and proof |
| --- | --- | --- |
| Update, plist already names the new binary | decided | `Reconcile` changes nothing and does not disable or kickstart. Fake job. |
| Update, plist names another binary, `RewritePath` unset | decided | Error wraps `service.ErrBinaryMismatch`. The file is unchanged. Fake job. |
| Update, plist names another binary, rewrite allowed | decided | `plutil` replace and lint, mode and owner kept, running job refused, loaded stopped job reloaded without disable. Fake runner. |
| Reconcile or reload failure | decided | Library `Restore` puts the previous bytes, mode, and owner back. No interactive disable. Fake runner. |
| Explicit `service refresh` | decided | Product renderer may rewrite policy keys. Reload is library bootout, wait, bootstrap. A non-running job is not disabled. Fake job. |
| Operator `service stop` | decided | `disable`, then the same job's `Stop`. Restart and update do not disable. Fake runner. |
| Operator `service restart` and update health | decided | Stop, start, and wait share one job. The previous PID does not satisfy `WaitHealthy`. A process that exits inside the settle window does not pass. Fake clock and runner. |
| `launchctl` calls | decided | Disable and the job use one `Options.Runner`. Absent bootout follows the library's 0, 3, 36, and 113. Other exits are not absent. Fake runner. |
| Darwin status and doctor | decided | Fields come from `Job.Status` plus the product health probe. No product parser of `print` or `print-disabled`. Fake job. |
| Linux and Windows start, stop, restart | decided | Shared controller, library job, and `WaitHealthy`. No direct `systemctl` start or stop on those commands. Fake job. |
| Windows update reconcile | decided | `scm.Service.Reconcile`. No success that means "no on-disk definition" while a service is installed. Fake SCM job. |
| Update started inside the service | decided | `HandOffIfInside` detaches once. `LoadHandOffEnv` runs at start. The detached result is the command result. Fake detacher. |
| Update started outside the service | decided | No handoff. The same process runs `Updater.Run`. Fake detacher. |
| Signature verification, Task Scheduler, `ReplaceBeforeStop`, `EnabledLifecycle` on the update adapter | out of scope | Checksums stay. A stopped service stays stopped. The other facilities need their own decision. |
| Install-time plist keys | decided | Product renderer remains the install and explicit-refresh writer. It is not invoked by update reconcile. Existing round-trip tests stay. |
| Command build, plist decoder | decided | `plistXML` and `plistXMLValue` live in a non-test file. `decodePlistDict` is the only pairing. A dict whose key count differs from its value count is an error. `go build ./...` is the proof. |

### Build amendment, 2026-11-25

CI run `45669857778` on `11c4a69` failed the Darwin and Windows `go build ./...` steps. Both reported `plistXML` and `plistXMLValue` undefined in `update_service.go`. Those types are declared only in `service_definition_test.go`. `go test` and `go vet` compile the test files with the package, so the Linux job, which does not run `go build ./...` except on a tag, passed. A tag build would fail at `make build-all` for the same reason. The lint failures on the two earlier runs were already fixed on `11c4a69`; the Linux job of `45669857778` passed lint.

The library's plist reader is unexported. Calling it is not available. A second decoder would drift from the one the round-trip test already uses.

#### Build amendment options

* H. Move the existing decoder into a non-test file, share it with the test helper, and reject a dict whose key count differs from its value count.
* I. Duplicate a minimal reader beside `launchdDefinitionProgram` and leave the test types where they are.
* J. Build-tag the reader as Darwin-only and stub it elsewhere.

#### Build amendment outcome

Chosen option: "H. Move the existing decoder into a non-test file", because the command already depends on that decoder and the test already requires equal key and value counts. Option I is two parsers. Option J does not fix the Darwin build, which is the platform that reads the plist.

* Good, because `go build ./...` no longer depends on a test file.
* Good, because a mis-paired dict cannot return another key's program text.
* Neutral, because the unmarshal shape is unchanged. This does not add a new plist policy.
* Bad, because the types remain a product parser rather than a library call. The library reader is unexported, so that call is not available.
