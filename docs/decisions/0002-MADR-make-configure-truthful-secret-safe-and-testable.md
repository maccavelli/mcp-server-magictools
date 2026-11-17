---
status: proposed
date: 2026-08-30
decision-makers: MagicTools maintainers
consulted: Codebase audit requested by project maintainer
informed: MagicTools contributors and operators
---

# Make configure truthful, secret-safe, and testable through one application service

## Context and Problem Statement

`mcp-server-magictools configure` has improved materially since [MADR 0001](./0001-MADR-make-configure-updates-lossless.md): `init` and `configure` are separate commands, YAML paths resolve through one primary resolver, the wizard stages typed patches, saves are locked on Unix and Windows, and generative provider identity is derived from `mcplib`. Those changes close several original data-loss and provider-drift defects.

The current implementation nevertheless stops halfway between a terminal wizard and a configuration transaction. The top-level menu, Fast/Thinking shared wizard, local Embedding flow, Backplane flow, YAML store, runtime loader, and activation consumers each own part of the contract. They exchange flattened strings rather than provider capabilities, credential provenance, validated desired state, and activation state. As a result, a successful wizard can persist a secret copied from the environment, accept a configuration that the runtime cannot activate, or report success without saying that the running process still uses the old provider, vector engine, or listener.

Several concrete regressions demonstrate the architectural gap. Ollama is advertised for both generative tiers but Fast hydration requires a non-empty API key and Thinking pool construction skips every provider without one. Backplane can be enabled without a Fast provider even though startup refuses to create it in that state. `ConfigStore.Apply` calls a function described as validation, but that function only decodes a small subset of YAML and enforces no provider, range, URL, or cross-field invariant. Most prompt errors are discarded, and the only wizard UI test is skipped.

How should MagicTools organize interactive configuration, non-interactive inspection/automation, secret handling, validation, persistence, and activation reporting so that a successful command means the persisted configuration is safe, internally coherent, and accurately described to the user?

This record is a current-tree follow-up to proposed MADR 0001, not a claim that MADR 0001 was accepted or completed. If this decision is accepted, MADR 0001 remains useful historical evidence for the original lossless-update problem, while this record governs the remaining configure architecture.

## Decision Drivers

* Never persist an environment-sourced secret merely because the user chose to use that environment variable.
* Offer only provider/tier combinations that every relevant runtime consumer can actually activate.
* Reject invalid or internally inconsistent candidates before any file or in-memory runtime state changes.
* Preserve the distinction between persisted desired state, staged state, runtime-effective state, and provider discovery status.
* Make every prompt, validation, save, and activation failure observable through a non-zero exit and actionable message.
* Keep accepted edits narrow, serialized, recoverable, and platform-correct.
* Make the complete wizard deterministic under tests without a real terminal or provider network.
* Retain YAML, Cobra, Pterm, and `mcplib` provider implementations rather than replacing working public interfaces.
* Support safe inspection and validation from scripts without pretending an interactive wizard is non-interactive.

## Considered Options

* Patch the confirmed defects in the existing functions.
* Introduce one typed configure application service with interactive and non-interactive adapters.
* Move MagicTools configuration ownership into `mcplib`.
* Replace the wizard and YAML with a new configuration product.

## Decision Outcome

Chosen option: "Introduce one typed configure application service with interactive and non-interactive adapters", because the defects share one cause—configuration meaning is fragmented across UI, persistence, and runtime consumers—while the existing YAML format, provider library, and terminal renderer remain viable.

The service boundary will own the following contract:

1. Load one immutable persisted snapshot and create a typed draft. Keep persisted, staged, and runtime-effective views distinct rather than mutating the loaded `Config` in place.
2. Represent credentials as a source (`environment reference`, `literal`, `local/no credential`, or future secret provider), never as only a resolved string. Resolve secrets only at runtime and during an explicit bounded online check.
3. Build menus from capability contracts that cover the real consumer paths: hydrator, shared LLM pool, Thinking dispatch, fallback construction, vector embedder, endpoint handling, and credential requirements. Constructor success alone is insufficient.
4. Validate the complete draft before commit. Validation includes required field groups, provider/tier compatibility, model and dimensionality rules, URL syntax and policy, port and resource ranges, Backplane prerequisites, credential-source conflicts, and documented zero-value semantics.
5. Apply a typed patch to the latest on-disk YAML under the shared lock, then return a structured result containing actual changed paths, secret-free before/after metadata, and an activation classification for each change.
6. Treat `saved`, `applied live`, `restart required`, `index rebuild required`, and `online validation not performed` as distinct outcomes. The terminal UI must show this result before exiting; machine-readable adapters expose the same result.
7. Put every interaction behind injected interfaces. The Pterm wizard becomes an adapter over the service; prompt, environment, discovery, clock, filesystem/store, output, and service-state dependencies are replaceable in tests. Command context and cancellation flow through every operation.
8. Keep plain `configure` as the guided workflow. Add script-safe inspection and offline validation over the same service, with stable structured output. A later implementation plan must select exact command spelling for declarative mutation rather than reviving the rejected `--non-interactive` boolean without a contract.
9. Make initialization/reset use the same path, locking, atomic replacement, validation, and recovery primitives as configure. Forced multi-file reset must be confirmed, reject unsafe targets, create recoverable backups, and avoid partial mixed generations.
10. Retire or quarantine broad `SaveConfiguration` persistence after all production callers use the transactional store, and remove dead patch fields or finish their end-to-end schema/runtime support.

### Consequences

* Good, because the wizard can no longer confuse an environment secret value with a persistent credential source.
* Good, because provider menus and contract tests will reflect actual activation behavior, including keyless and custom-endpoint providers.
* Good, because a save cannot succeed with a negative limit, unusable Backplane dependency, or incomplete provider/model/credential group.
* Good, because users and automation can tell what changed and what action is still required.
* Good, because the same tested transaction supports the terminal UI, validation, inspection, and future automation.
* Good, because cancellation and prompt failures become ordinary error paths instead of hidden UI behavior.
* Bad, because this introduces an application-service layer and explicit desired/effective models across several packages.
* Bad, because accurate activation reporting may require service-state inspection and will expose that many intelligence settings are restart-only today.
* Bad, because environment references require migration-compatible schema additions and service-environment documentation.
* Bad, because cross-platform reset recovery and replacement semantics require platform-specific tests and helpers.

### Confirmation

The decision is implemented only when all of the following are automated:

* A credential-provenance matrix proves environment selections persist only variable names and resolved sentinel secrets never appear in YAML, logs, diffs, or command output.
* A provider contract matrix exercises every advertised tier through its real runtime consumer, including keyless Ollama, base URLs, fallbacks, Thinking dispatch, and vector construction.
* Candidate validation rejects missing dependencies, invalid provider/model groups, malformed or unsafe URLs, invalid ports, negative limits, inconsistent dimensions, and conflicting credential sources without changing disk or memory.
* Scripted wizard tests cover every menu action, cancel/error at every prompt, re-entry after staging, same-tier and cross-tier credential choices, save, discard, and restart/rebuild messaging.
* Golden tests prove that accepted patches change only owned YAML semantics, preserve unknown keys/comments/styles to the documented level, and make no write for a semantic no-op.
* Concurrency and fault-injection tests cover lock timeout/cancellation, concurrent configure and `update_config`, interrupted atomic replacement, Windows replacement semantics, and reset/configure races.
* Alternate-path integration tests prove `config.yaml`, `servers.yaml`, `tool_overrides.yaml`, migration, watcher, configure, and reset all use the same resolved directory.
* Runtime-state tests prove each field is either applied to its consumer or reported as restart/rebuild-required; no watcher produces a silent desired/effective split.
* CLI tests assert rejected flags are absent, non-terminal rejection is side-effect-free, structured inspection/validation output is stable, and failures exit non-zero.

## Pros and Cons of the Options

### Patch the confirmed defects in the existing functions

This option would fix keyless provider predicates, add Backplane ranges, remove rejected flags, and check currently ignored errors in place.

* Good, because the first patch would be small and could close individual high-impact bugs quickly.
* Good, because it would not add a new service abstraction.
* Bad, because the Fast/Thinking shared wizard and local Embedding/Backplane flows would still have different provenance, validation, and error contracts.
* Bad, because UI functions would continue mutating `Config` and patch state directly, making desired/effective state and rollback difficult to reason about.
* Bad, because the next provider or configuration feature could reintroduce the same menu/runtime drift.

### Introduce one typed configure application service with interactive and non-interactive adapters

* Good, because it centralizes draft construction, validation, commit, and activation reporting without centralizing terminal rendering.
* Good, because it can reuse the existing `ConfigStore`, provider descriptors, Pterm adapter, and YAML schema.
* Good, because it provides a stable seam for deterministic tests and script-safe read/validate features.
* Neutral, because `mcplib` remains the canonical generative provider implementation but MagicTools retains its application-specific tier and persistence policy.
* Bad, because the current 600-line command must be split and several partial abstractions must be migrated or removed.

### Move MagicTools configuration ownership into `mcplib`

* Good, because generative provider collection could return credential provenance and shared validation directly.
* Bad, because Embedding, Backplane, vector dimensionality, activation, YAML paths, and service reset are MagicTools concerns.
* Bad, because library consumers do not share one configuration schema or UI, so application ownership would couple unrelated binaries.
* Bad, because it would not by itself solve MagicTools desired/effective state or multi-file transactions.

### Replace the wizard and YAML with a new configuration product

* Good, because a new schema or web UI could design provenance and validation from scratch.
* Bad, because it creates migration, compatibility, deployment, and security work disproportionate to the defects.
* Bad, because the current terminal and YAML interfaces are usable once their application contract is made coherent.
* Bad, because it delays fixes for secret persistence and false-success paths.

## More Information

### Assessment Scope and Baseline

The audit was performed on 2026-08-30 at commit `c672a72`. It traced:

* Cobra registration, terminal gating, top-level menu routing, and every configure option;
* `mcplib` v1.2.0 wizard result and environment-key behavior;
* provider catalog construction and consumer contract tests;
* typed patch emptiness, YAML AST mutation, locking, candidate decoding, and atomic replacement;
* initialization/reset, path resolution, alternate companion files, and watchers;
* hydrator, LLM pool, Thinking dispatch, fallback construction, vector boot, and activation/reload behavior;
* CLI documentation, generated template claims, and current configure tests.

Read-only verification passed:

```text
go test -count=1 ./cmd/mcp-server-magictools ./internal/config ./internal/provider ./internal/llm ./internal/vector
PASS

go vet ./cmd/mcp-server-magictools ./internal/config ./internal/provider ./internal/llm ./internal/vector
PASS

go test -race -count=1 ./cmd/mcp-server-magictools ./internal/config ./internal/provider ./internal/llm ./internal/vector
PASS

make lint
0 issues
```

Focused coverage was:

```text
cmd/mcp-server-magictools  16.4%
internal/config            61.0%
internal/provider          94.1%
internal/llm               70.5%
internal/vector            72.9%
```

Passing checks do not invalidate the findings. `TestConfigUIFunctions` is skipped; the test named for wizard exit never calls `runConfigure`; the concurrent-store test discards every `Apply` error and only asserts that the file is non-empty; and provider contracts prove constructor existence rather than end-to-end consumer activation.

### Findings Summary

| ID | Priority | Finding | User Impact |
|---|---:|---|---|
| CF2-01 | P0 | Choosing an environment credential persists its resolved secret as `*_api_key`. | A value the user expected to remain environment-only is copied into plaintext YAML. |
| CF2-02 | P0 | Keyless Ollama is advertised for Fast and Thinking, but hydration and Thinking activation require a non-empty API key. | A wizard-supported configuration is silently disabled in major runtime paths. |
| CF2-03 | P0 | Backplane can be staged without Fast, while startup requires Fast; candidate validation does not reject it. | The CLI reports a saved/enabled Backplane that never starts. |
| CF2-04 | P1 | Configure discards `RestartRequired`; the watcher replaces in-memory Intelligence without reloading pools, vector state, or listeners. | “Saved successfully” does not identify stale active behavior, and runtime state becomes split. |
| CF2-05 | P1 | `DecodeConfiguration` is labeled validation but only unmarshals a subset and enforces no domain invariants. | Invalid limits, URLs, dependencies, and incomplete tier groups can commit. |
| CF2-06 | P1 | Most Pterm errors are ignored and option handlers return no error. | Terminal failures/cancellation can look like a harmless return to the menu or unchanged success. |
| CF2-07 | P1 | Non-terminal detection occurs after initialization and config loading. | `configure` can create three files and trigger load side effects before exiting with “interactive terminal required.” |
| CF2-08 | P1 | Embedding credential reuse is not provider-safe and prefers cross-tier keys over its own current key. | Switching providers can default to retaining the wrong credential. |
| CF2-09 | P1 | Base URLs are not propagated through all consumers, fallbacks, or reload paths. | A saved custom endpoint can be ignored even though the wizard displayed and persisted it. |
| CF2-10 | P1 | Forced reset is a sequential `os.WriteFile` operation with no backup, shared lock, rollback, or symlink rejection. | Failure or a configure race can leave mixed files; reset is not recoverable. |
| CF2-11 | P1 | The store calls `os.Rename` “atomic” on every platform, while Go documents non-Unix rename as non-atomic. | The Windows durability guarantee is stronger than the implementation. |
| CF2-12 | P1 | Alternate `config.yaml` resolves colocated companions, but initial override loading and one migration save still use default paths. | `--config` can show one file set while runtime consumes or writes another. |
| CF2-13 | P2 | `isThinkingEmpty` omits `ThinkingAPIURL`. | A URL-only Thinking patch is incorrectly treated as empty and skipped. |
| CF2-14 | P2 | Embedding switches retain a stale local URL; manual model text is truncated at the first space. | Provider changes leave misleading state and some custom identifiers cannot round-trip. |
| CF2-15 | P2 | Fast fallback selection never preselects existing fallbacks and always writes the returned list. | Reconfiguring Fast can clear a working fallback chain unless it is rebuilt manually. |
| CF2-16 | P2 | Backplane “Keep current” still stages tuning values when enabled; numeric input has no domain ranges; documented `max_rpm: 0` conflicts with runtime. | A no-op choice can rewrite values, and accepted values can default differently at startup. |
| CF2-17 | P2 | “Show Current Config” displays mutated staged state as current and omits path, source, activation, and pending-action metadata. | Users cannot distinguish disk, draft, or running state before saving. |
| CF2-18 | P2 | Rejected `--force` and `--non-interactive` flags remain in configure help. | Discoverability directs users toward commands guaranteed to fail. |
| CF2-19 | P2 | Code refers ambiguously to sibling `mcplib` MADR/PLAN 0004 without a qualified repository link, and generated provider/hot-reload comments contradict current behavior. | Maintainers cannot reliably follow cross-repository rationale and users receive conflicting support claims. |
| CF2-20 | P1 | Tests do not drive the complete wizard or its error paths. | Passing CI does not protect the primary CLI workflow. |
| CF2-21 | P2 | The store mutates narrow nodes but marshals the entire YAML document; preservation guarantees are not covered beyond a header comment. | An accepted small edit may cause avoidable presentation diffs not represented in `ChangedPaths`. |
| CF2-22 | P2 | Command context is replaced with `context.Background()` in tier and endpoint flows; lock acquisition is not cancelable. | Ctrl-C or caller cancellation may not promptly stop discovery, validation, or lock waits. |
| CF2-23 | P2 | Fast has no clear/disable action. | Users cannot fully undo Fast configuration through the supported UI. |

### Detailed Evidence

#### CF2-01: Environment credential provenance is discarded

The Fast/Thinking wrapper passes `AllowEnv: true` to `wizard.ConfigureLLM`. In `mcplib` v1.2.0, `resolveAPIKey` reads the environment value and returns the raw secret in `wizard.Result.APIKey`; the result contains no source metadata. MagicTools then assigns that raw value to `Config.Intelligence.*APIKey` and stages `api_key` or `thinking_api_key`. Embedding repeats the same behavior locally: `promptTierAPIKey` returns `os.Getenv(spec.EnvVar)` and the caller stages `embedding_api_key`.

Patch types contain `APIKeyEnv` fields, but `IntelligenceEngine` has no corresponding schema fields and no configure flow sets them. The partial abstraction therefore does not protect secrets.

Evidence: `cmd/mcp-server-magictools/config.go:130-160`, `197-216`, `278-295`, `497-504`, `521-543`; `internal/config/patch.go:28-58`; `internal/config/config.go:238-274`; `mcplib/wizard/configure.go:17-26`, `190-225` in dependency v1.2.0.

#### CF2-02, CF2-03, and CF2-09: Menu support is broader than runtime support

The provider catalog advertises Ollama for Fast and Thinking and the constructor contract accepts it with an empty key. The hydrator separately defines availability as `provider != "" && api_key != ""`, so Ollama Fast never enters LLM hydration. `llm.NewPool` constructs Fast without a key restriction, but its optional Thinking branch requires `ThinkingAPIKey != ""`, so Ollama Thinking is skipped. The contract test passes a dummy key to every provider and does not exercise either predicate.

Backplane configuration has no Fast prerequisite. Startup creates the Backplane only when `shared_llm_enabled` and Fast provider are both set. A Backplane-only patch is explicitly asserted as valid by `TestCFG02_SaveConfigurationGatedOnFastProvider`, conflating independent persistence with operational validity.

Custom Fast endpoints are passed to initial pool construction, but hydrator probe/provider creation omit `APIURL`; fallback construction omits it; and `Pool.Reload` omits it for Fast. The same persisted field consequently means different things across consumers.

Evidence: `internal/provider/catalog.go:63-136`; `internal/provider/contract_test.go:11-33`; `internal/intelligence/hydrator.go:33-49`, `356-395`; `internal/llm/pool.go:92-119`, `147-158`, `353-385`; `cmd/mcp-server-magictools/config.go:369-427`; `cmd/mcp-server-magictools/main.go:530-568`; `cmd/mcp-server-magictools/config_wizard_test.go:32-85`.

#### CF2-04 and CF2-17: Persisted and active state are conflated

Every changed `ConfigStore.Apply` result sets `RestartRequired: true`. `runConfigure` checks only `Changed` and prints `Configuration saved successfully`; it discards changed paths and activation state. `showCurrentConfig` receives the same `Config` object mutated by option handlers, labels it “Current,” and cannot compare it with disk or a running service.

The watcher later assigns the new `Intelligence` block into the shared live config and calls `OnConfigReloaded`. That callback refreshes internal tools, log level, and search gates; it does not reload the LLM pool, recreate the vector engine, or rebind the Backplane listener. `Pool.Reload` exists but has no production caller and would still omit parts of pool state. The live `Config` can therefore describe desired providers while long-lived consumers continue using old instances.

Evidence: `internal/config/store.go:14-20`, `104-110`; `cmd/mcp-server-magictools/config.go:99-116`, `431-480`; `internal/config/watcher.go:190-237`; `internal/handler/handlers.go:136-166`; `internal/llm/pool.go:353-386`; `cmd/mcp-server-magictools/main.go:530-610`.

#### CF2-05 and CF2-16: Candidate validation is decoding, not validation

`ConfigStore.Apply` describes `DecodeConfiguration` as candidate validation. The function uses Viper to unmarshal only a subset of `Config` and returns without checking ranges, required groups, provider IDs, model compatibility, credential requirements, URLs, or dependencies.

Backplane integer input accepts any value that `strconv.Atoi` accepts. Negative values are persisted and then replaced by runtime defaults for several fields. `llm_port` above 65535 reaches listener binding and fails there. The generated template says `max_rpm: 0` means unlimited; `llm.NewPool` maps every non-positive value to 60. “Keep current” does not return early when Backplane is already enabled, so it prompts and stages all tuning values.

Evidence: `internal/config/store.go:94-137`; `cmd/mcp-server-magictools/config.go:378-427`, `610-630`; `internal/llm/pool.go:122-169`; `internal/config/templates.go:258-276`.

#### CF2-06, CF2-07, and CF2-22: Interaction errors and cancellation are not command errors

The terminal check occurs only after `ensureInitialized` and `config.New`. A non-terminal invocation can therefore create the config directory and three templates, load companion registries, or perform loader migrations before returning an error.

The main menu, Embedding menus and inputs, Backplane menu, and integer prompts discard Pterm errors. Option handlers return `void`; shared-wizard errors are printed as warnings and swallowed. `configureTier` and endpoint validation start from `context.Background()` instead of `cmd.Context()`. The config lock uses a blocking OS lock with no context-aware timeout.

Evidence: `cmd/mcp-server-magictools/config.go:36-67`, `83-86`, `127-139`, `171-205`, `224-335`, `369-420`, `490-504`, `559-585`, `610-630`; `internal/config/store.go:34-49`; `internal/config/filelock_unix.go:17-29`; `internal/config/filelock_windows.go:16-29`.

#### CF2-08, CF2-14, and CF2-15: Reconfiguration is not safely defaulted

Embedding credential selection checks matching Fast and Thinking providers before considering the existing Embedding credential. If neither cross-tier branch matches, `promptTierAPIKey` offers the existing Embedding key without checking that it belongs to the newly selected provider. Switching Voyage to OpenAI can therefore default to “Keep the existing key.” The Embedding provider/model selects also do not default to the current selections.

When leaving Ollama embeddings, `embedding_api_url` is not cleared because the patch is set only when the newly selected provider produces a non-empty URL. Manual embedding model text is reduced with `strings.Split(..., " ")[0]`, unlike curated display labels whose suffix is intentionally stripped.

The shared Fast wizard always asks for fallbacks with no preselection; MagicTools then stages the returned list unconditionally. Reconfiguring an otherwise unchanged Fast tier can clear existing fallbacks.

Evidence: `cmd/mcp-server-magictools/config.go:227-365`, `130-163`; `mcplib/wizard/configure.go:298-319` in dependency v1.2.0.

#### CF2-10 and CF2-11: Reset and replacement do not meet one durability contract

`init --force` has a confirmation gate but calls `ensureInitialized`, which writes the three targets sequentially with `os.WriteFile`. It takes neither the primary config lock nor a directory/reset lock, creates no backups, performs no staged validation, and cannot roll back if the second or third write fails. `fileExists` uses `os.Stat`, and forced writes do not reject symlink targets.

`ConfigStore` is stronger on Unix: it writes and syncs a same-directory temporary file, renames it, and syncs the parent. The same helper is compiled on Windows and calls `os.Rename`; the Go 1.26.5 `os.Rename` documentation explicitly states that rename is not atomic on non-Unix platforms. No Windows fault/replacement test establishes the stronger comment and documentation claim.

Evidence: `cmd/mcp-server-magictools/init.go:28-64`; `cmd/mcp-server-magictools/config.go:646-693`; `internal/config/atomic_write.go:9-60`; `internal/config/store.go:33-110`; `go doc os.Rename` under the audited toolchain.

#### CF2-12: Canonical paths are not threaded through all consumers

`ResolvePaths` correctly colocates YAML companions and `Config.New` records those paths. `LoadFromViper` resolves `servers.yaml` from the primary path, but initial `tool_overrides.yaml` loading still uses `DefaultConfigDir`. The IDE migration branch also calls default-path `SaveManagedServers` instead of its path-taking counterpart. Watchers use `cfg.Paths`, so boot and later reload can initially disagree about which override file is active.

Evidence: `internal/config/paths.go:18-84`; `internal/config/config.go:536-603`, `1096-1153`, `1190-1201`; `internal/config/watcher.go:64-86`.

#### CF2-13 and CF2-21: Patch metadata has correctness gaps

`ThinkingTierPatch` owns `ThinkingAPIURL`, and YAML mutation applies it, but `isThinkingEmpty` omits that field. A URL-only patch returns early as empty or never creates the Intelligence mutation path.

Each field helper appends a bare key to `ChangedPaths` even when setting the current value or removing a missing key. Only the final byte comparison determines whether anything changed, and a changed result can still report paths that did not semantically change. The complete document is marshaled after a narrow AST mutation. Existing tests preserve one header comment but do not establish the promised behavior for quotes, flow style, blank-line structure, CRLF, anchors, unknown nested nodes, or path-qualified audit output.

Evidence: `internal/config/patch.go:41-48`, `119-124`; `internal/config/patch_yaml.go:9-90`, `93-145`; `internal/config/store.go:74-110`; `internal/config/store_test.go:12-70`.

#### CF2-18 through CF2-20 and CF2-23: CLI surface and tests are incomplete

Configure registers `--force` and `--non-interactive`, then rejects both before doing useful work. Fast has no equivalent to Thinking clear or Embedding disable. Generated template comments advertise only three Fast/Thinking providers while the wizard offers every `mcplib` descriptor, and the template says nearly all config changes hot-reload. Multiple production/test comments cite “MADR 0004” or “PLAN 0004” without qualifying that they refer to the accepted records in the sibling `mcplib` repository; MagicTools' local `docs/` contains only its own MADR/PLAN 0001 and this proposed follow-up.

The only function that invokes all interactive option handlers is skipped. `TestCFG04_WizardExitWithoutSavePreservesOriginal` constructs an empty patch and rereads a file without running the wizard. `TestStore_ConcurrentWriters` ignores all write errors and does not assert preservation of independent fields. Provider contracts use dummy credentials and constructor success, missing keyless predicates, endpoints, fallbacks, and consumers.

Evidence: `cmd/mcp-server-magictools/config.go:30-43`, `73-121`, `127-220`, `640-643`; `internal/config/templates.go:39-50`, `214-241`; `cmd/mcp-server-magictools/config_extra_test.go:56-70`; `cmd/mcp-server-magictools/config_wizard_test.go:87-120`; `internal/config/store_test.go:99-140`; `internal/provider/contract_test.go:11-55`; repository `docs/` inventory.

### Recommended Remediation Order

This ordering is advisory evidence for review, not an approved implementation plan:

1. Stop secret-copy behavior and false-success activation paths: credential provenance, keyless provider predicates, Backplane prerequisite, and restart/rebuild reporting.
2. Add pure candidate validation and propagate all prompt/store errors before expanding features.
3. Introduce the configure service/draft boundary and deterministic scripted wizard coverage.
4. Finish platform/path durability: Windows replacement, reset transaction/backups, lock cancellation, and alternate companions.
5. Improve ergonomics: provider-safe defaults, clear Fast, staged diff/summary, desired-versus-active show, offline validate, structured output, and removal of rejected flags.
6. Remove stale `SaveConfiguration`, dead env patch fields, nonexistent MADR references, and contradictory template/help text after the new paths are authoritative.

No implementation plan or source change is authorized by this proposed record. Under the repository workflow, implementation must wait for a complete associated plan and explicit user approval.
