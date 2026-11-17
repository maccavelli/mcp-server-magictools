---
status: proposed
date: 2026-08-30
implements: 0002-MADR-make-configure-truthful-secret-safe-and-testable.md
owners: MagicTools and mcplib maintainers
scope: mcplib wizard result metadata; MagicTools configure service, credential persistence, validation, storage, activation reporting, reset recovery, tests, and documentation
---

# Implementation plan for a truthful, secret-safe, and testable configure service

## Associated MADR

This plan implements [MADR 0002](./0002-MADR-make-configure-truthful-secret-safe-and-testable.md). The MADR remains `proposed`; no production implementation may begin until maintainers accept the MADR, approve this completed plan explicitly, and resolve the cross-repository `mcplib` release gate described below.

This is an implementation artifact, not a second decision record. If review changes an architectural choice—especially credential representation, active-state reporting, the declarative command contract, or the `mcplib` ownership boundary—update MADR 0002 first and then revise this plan to match.

## Goal

Deliver one typed MagicTools configure application service used by the Pterm wizard and script-safe commands so that:

* an environment-selected credential is persisted as an environment-variable reference, never as the resolved secret;
* every provider/tier offered by the CLI can be activated by the real MagicTools runtime path, including keyless Ollama and custom endpoints;
* the complete candidate is validated under the same lock that protects persistence;
* concurrent edits either merge safely on independent paths or fail with an explicit path conflict;
* output distinguishes persisted desired state, staged state, verified active state, discovery source, online-validation state, restart requirements, and vector-index rebuild requirements;
* prompt, cancellation, validation, lock, write, and online-check failures produce a non-zero command result;
* `init --force` is recoverable and does not silently leave a mixed three-file reset after a handled failure;
* the full workflow is deterministic under unit, integration, race, fault-injection, and cross-platform tests.

## Fixed implementation decisions

The following details close ambiguities left intentionally open by MADR 0002. Implementation must not substitute different contracts without revising the MADR and this plan.

### Repository ownership

| Concern | Owning repository/package | Rule |
|---|---|---|
| Canonical generative provider IDs, labels, conventional env vars, locality, endpoint support, key requirements, and static generative models | `../mcplib/llmprovider` | MagicTools derives these values; it does not copy them. |
| Generic generative provider/base-URL/credential/model/fallback collection | `../mcplib/wizard` | Extend its result additively with source metadata; do not change `wizard.Prompter`. |
| MagicTools Fast, Thinking, Embedding, and Backplane roles | `internal/provider`, `internal/configure` | These are application concepts and stay out of `mcplib`. |
| Embedding providers, model/dimension pairs, vector construction, and index rebuild policy | MagicTools `internal/provider`, `internal/vector`, `internal/configure` | `mcplib` has no embedding abstraction; do not create one as part of this plan. |
| YAML schema, paths, locking, reset, migration, service state, and activation policy | MagicTools `internal/config`, `internal/configure`, CLI | Persistence remains outside `mcplib`, consistent with mcplib MADR 0004. |

The existing code comments that say only `MADR 0004` or `PLAN 0004` refer to accepted records in the sibling `mcplib` repository, not files in MagicTools. Replace those comments with qualified references such as `mcplib/docs/0004-MADR-canonicalize-llm-provider-configuration.md`; do not duplicate that decision locally.

### Shared `mcplib` API addition

Release `mcplib` v1.3.0 with an additive wizard contract. The local tag inventory ends at v1.2.0; if v1.3.0 exists by execution time, stop and revise this plan rather than choosing another version implicitly.

Add these public types and a parallel detailed entry point in `../mcplib/wizard/configure.go`. Do not add fields to existing exported structs: preserving `Result`, `Options`, and `ConfigureLLM` exactly also preserves consumers that use positional composite literals.

```go
type CredentialKind string

const (
    CredentialNone        CredentialKind = "none"
    CredentialLiteral     CredentialKind = "literal"
    CredentialEnvironment CredentialKind = "environment"
    CredentialLocal       CredentialKind = "local"
)

type CredentialSource struct {
    Kind   CredentialKind
    EnvVar string
}

type ModelSource string

const (
    ModelSourceLive   ModelSource = "live"
    ModelSourceStatic ModelSource = "static"
    ModelSourceManual ModelSource = "manual"
)

type DetailedOptions struct {
    Options                  Options
    ExistingCredentialSource CredentialSource
}

type DetailedResult struct {
    Result           Result
    CredentialSource CredentialSource
    ModelSource      ModelSource
}

func ConfigureLLMWithMetadata(
    ctx context.Context,
    p Prompter,
    o DetailedOptions,
) (DetailedResult, error)
```

Behavior is exact:

* accepting a discovered environment value returns the raw value in `DetailedResult.Result.APIKey` and `{Kind: environment, EnvVar: descriptor.EnvVar}` in `CredentialSource`;
* keeping an existing credential preserves `DetailedOptions.ExistingCredentialSource`; if caller data supplies only `Options.Existing.APIKey`, infer `literal`;
* entering a key returns `literal`;
* a local provider that does not require a key returns `local`;
* a non-local provider that does not require a key returns `none`;
* live, static-fallback, and manual model selection set the corresponding `ModelSource` value;
* fallback selection preselects `Options.Existing.Fallbacks` only when the provider is unchanged, excludes the primary model, removes duplicates, and preserves displayed order;
* manual model IDs and base URLs are trimmed and must be non-empty; no prompt or notification contains a raw credential;
* existing `ConfigureLLM` delegates to the same internal flow and returns only `DetailedResult.Result`, so its signature and behavior remain compatible;
* `Result`, `Options`, and `Prompter` remain structurally unchanged, making this a compatible minor release even for positional composite-literal consumers.

MagicTools calls `ConfigureLLMWithMetadata` and maps the detailed metadata into its own persisted credential model. It must not serialize `DetailedResult.Result.APIKey` when `CredentialSource.Kind == environment`.

### MagicTools persisted and runtime credential model

Keep existing literal fields for backward compatibility and add:

```yaml
configuration:
  intelligence:
    api_key_env: GEMINI_API_KEY
    thinking_api_key_env: CLAUDE_API_KEY
    embedding_api_key_env: VOYAGE_API_KEY
```

For each tier, exactly one of literal key or env reference may be set when the provider requires a key. A provider declared keyless by the canonical descriptor must have neither. Env names must match `^[A-Za-z_][A-Za-z0-9_]*$`.

Add a runtime-only `ResolvedCredentials` value to `config.Config` with `yaml:"-"`, `json:"-"`, and no exported secret-bearing serialization. `config.New` resolves required references once through an injectable `LookupEnv(name) (string, bool)` dependency and returns a path-specific error when an enabled remote tier's referenced variable is absent or empty. Runtime consumers use accessors such as `FastAPIKey()`, `ThinkingAPIKey()`, and `EmbeddingAPIKey()`; they no longer read persisted fields directly.

Offline decode, `show`, dry-run, and offline validation never resolve an env reference. Guided configuration may inspect the current process environment to offer a reference, and `validate --online` resolves credentials explicitly. Cross-tier reuse copies a credential source only when provider IDs match; it never copies resolved environment bytes.

### Public CLI contract

The supported commands after this plan are:

```text
mcp-server-magictools configure
mcp-server-magictools configure show [--format text|json]
mcp-server-magictools configure validate [--online] [--format text|json]
mcp-server-magictools configure apply --file <path|-> [--dry-run] [--format text|json]
```

Plain `configure` remains the guided TTY workflow. `show`, offline `validate`, and `apply` work without a terminal and never create missing files. Remove the rejected `configure --force` and `configure --non-interactive` flags entirely. Destructive reset remains `init --force [--yes]`.

`configure apply` accepts exactly one strict YAML document, rejects unknown fields and documents over 1 MiB, and uses this v1 schema:

```yaml
schema_version: 1
fast:                         # omitted means unchanged
  action: set                 # set | clear
  provider: gemini
  model: gemini-2.5-flash
  credential:
    kind: environment         # environment | local
    env: GEMINI_API_KEY       # required only for environment
  base_url: ""
  fallback_models: []
thinking:
  action: clear               # set | clear
embedding:
  action: set                 # set | clear
  provider: ollama
  model: nomic-embed-text
  dimensions: 768
  credential:
    kind: local
  base_url: http://localhost:11434
  vector_enabled: true
backplane:
  action: set                 # set | disable
  port: 48081
  max_concurrent_requests: 4
  max_rpm: 0
  max_burst_per_second: 5
  sub_server_token_thresh: 500000
  orphan_stream_ttl_minutes: 5
```

Rules for the declarative adapter:

* a present section requires `action`;
* `clear`/`disable` rejects configuration fields in the same section;
* `set` requires all fields needed for a complete enabled section; optional tuning fields use documented persisted defaults when omitted;
* v1 accepts only `environment` and `local` credential kinds; it rejects literal secrets so keys cannot leak through process arguments, shell history, or automation files;
* clearing Fast while Backplane remains enabled is invalid unless the same request disables Backplane;
* all requested sections form one candidate and one store transaction;
* `--dry-run` loads, merges, validates, classifies, and renders the result without acquiring a write lock or changing mtime;
* `--file -` reads stdin; text diagnostics go to stderr so JSON stdout remains one document;
* validation/input/operational failures are non-zero. This plan does not assign new process exit numbers because the root command currently has one non-zero error contract; numeric exit-code versioning is out of scope.

### Stable result model

All adapters render the same internal `configure.Result`. JSON uses `schema_version: "configure-result/v1"` and contains:

* canonical config path;
* persisted old/new SHA-256 hashes when applicable;
* validation state: `passed`, `failed`, or `not_run`;
* online validation state: `passed`, `failed`, `not_requested`, or `unsupported`;
* model discovery source per configured generative tier: `live`, `static`, `manual`, or `not_run`;
* active-state relation: `matches`, `differs`, `not_running`, or `unknown` plus a reason;
* ordered changes with fully qualified YAML path, secret-free before/after metadata, and activation class;
* ordered next actions.

Credential change metadata is limited to `not_configured`, `literal`, `env:NAME`, `local`, or `missing_reference`; it never includes masked fragments or values. Activation classes are `live`, `restart_required`, and `index_rebuild_required`. All configure-owned intelligence changes require restart in this implementation. An Embedding provider, model, base URL, dimensions, credential, or enabled-state change additionally requires index rebuild when an existing index fingerprint differs.

### Validation rules

Pure validation reports every deterministic problem in stable path order rather than stopping at the first error.

* A Fast or Thinking tier is either completely absent or has provider and non-empty model.
* A generative provider must exist in `llmprovider.Descriptors()` and in the relevant MagicTools tier catalog.
* Remote providers requiring credentials have exactly one literal or env source. Keyless providers have neither.
* Primary and fallback model IDs are trimmed and non-empty; fallbacks are unique and cannot equal the primary.
* Base URLs are absolute HTTP(S), contain a host, and contain no userinfo, fragment, or query. Plain HTTP is accepted only for a canonical local provider or loopback host; remote custom endpoints require HTTPS.
* `retry_count` is `0..10`; `retry_delay_seconds` is `0..3600`; `timeout_seconds` is `0` for the runtime default or `1..3600`.
* When `vector_enabled` is true, provider, exact model ID, and dimensions are required. A known embedding model/dimension pair must exist in structured catalog metadata. Custom dimensions are `1..65536`.
* When vector search is false, an entirely empty embedding block or a complete dormant block is accepted for backward compatibility; partial dormant blocks are rejected. The guided clear action removes the whole block and writes `vector_enabled: false`.
* Backplane enabled requires a complete valid Fast tier. Port is `0` for default 48081 or `1..65535`; concurrent requests is `0` for default 4 or `1..1024`; RPM is `0` for unlimited or `1..1000000`; burst is `0` for default 5 or `1..100000`; token threshold is `0` for default 500000 or `1..1000000000`; orphan TTL is `0` for default 5 or `1..1440` minutes.
* Negative values are always invalid. Runtime defaulting must use exactly the same constants and zero semantics as validation and generated documentation.

Offline validation does not access the network or resolve env values. `validate --online` performs serial, cancellable checks with a 20-second per-tier timeout and a 60-second overall timeout: generative tiers call `llmprovider.ListAvailableModels`; Embedding creates the real MagicTools embedder, embeds the fixed text `magictools configuration validation`, and verifies returned dimensions. Document that the Embedding check can consume one provider request. Backplane validation remains structural and never binds a port.

### Active-state truthfulness

Extend owner-only `service.state` with a schema version, canonical config path, a secret-free active intelligence snapshot, and an intelligence fingerprint captured only after runtime consumers construct successfully. The snapshot reports enabled provider/model/base URL/fallback/dimension/port metadata and credential source kind/name, never resolved keys.

`configure show` verifies that the recorded PID is alive and that the state file belongs to the same canonical config path. It reports:

* `matches` when the active fingerprint equals the desired persisted fingerprint;
* `differs` when a verified running service loaded a different intelligence generation;
* `not_running` when no live process owns a valid state file;
* `unknown` for an old state schema, unreadable state, alternate-path mismatch, direct stdio process, or unverifiable PID.

The fingerprint is SHA-256 over a canonical representation of persisted intelligence, including literal credential bytes only inside the in-memory hash input. Only the digest is written. Environment values are not part of this fingerprint; the active snapshot therefore proves which credential source and configuration generation were loaded, not that the current shell and service manager have identical secret values.

## Scope

### In scope

* additive `mcplib` wizard metadata and fallback-default behavior;
* dependency release and MagicTools upgrade to `mcplib` v1.3.0;
* credential-reference schema, pure validation, and runtime resolution;
* real-consumer provider capability fixes, including keyless Ollama and base URLs;
* transactional configure service, optimistic path preconditions, and structured results;
* guided Fast/Thinking/Embedding/Backplane set/switch/clear flows;
* `configure show`, `configure validate`, and versioned `configure apply`;
* active service snapshot/fingerprint;
* context-aware locking, path-qualified semantic changes, cross-platform replacement behavior, and fault injection;
* alternate companion paths, reset backup/journal/rollback, and unsafe-target rejection;
* retirement of broad configuration persistence and false hot-reload behavior;
* complete automated, cross-platform, and manual verification;
* CLI, template, configuration, security, and migration documentation.

### Out of scope

* moving MagicTools YAML or tier semantics into `mcplib`;
* adding an embedding abstraction to `mcplib`;
* remote secret-manager integrations;
* accepting literal secrets in declarative input;
* hot-swapping providers, vector engines, rate limiters, or listener ports;
* automatically migrating existing literal keys to env references;
* promising byte-for-byte formatting preservation after a semantic YAML change;
* publishing, pushing, or opening a PR without explicit authorization in the same execution turn.

## Implementation steps

Each phase ends with a green verification gate and a commit. Do not commit a deliberately failing regression test. Write the named regression first, observe it fail locally, implement the phase, rerun it green, then commit. MagicTools and `mcplib` commits are separate because they are separate repositories.

### Phase 0 — Approval, clean-room baseline, and release readiness

**Writes:** none.

1. Confirm MADR 0002 is `accepted`, this plan is explicitly approved, both repositories are on the intended branches, and unrelated user changes are identified and preserved.
2. Record `git rev-parse HEAD`, `git status --short`, Go version, OS/architecture, and `golangci-lint` version in the execution log.
3. Confirm MagicTools still requires `github.com/maccavelli/mcplib v1.2.0`, sibling `../mcplib` is the intended module, and v1.3.0 is unused locally and remotely.
4. Run the baseline commands without changing files:

   ```bash
   go test -count=1 ./cmd/mcp-server-magictools ./internal/config ./internal/provider ./internal/llm ./internal/vector ./internal/intelligence ./internal/handler
   go test -race -count=1 ./cmd/mcp-server-magictools ./internal/config ./internal/provider ./internal/llm ./internal/vector
   go vet ./cmd/mcp-server-magictools ./internal/config ./internal/provider ./internal/llm ./internal/vector ./internal/intelligence ./internal/handler
   make lint
   make -C ../mcplib test
   make -C ../mcplib vet
   make -C ../mcplib lint
   ```

5. Record any baseline failure and stop rather than normalizing it into this work.

**Exit gate:** clean or understood worktrees, green baselines, exact mcplib release target confirmed, and explicit approval recorded.

### Phase 1 — Add credential and discovery provenance to `mcplib`

**Repository:** `../mcplib`

**Files:**

* modify `wizard/configure.go`;
* modify `wizard/configure_test.go`;
* modify `wizard/fake_prompter_test.go` to record `MultiSelect` preselection for assertions.

**Actions:**

1. Add the exact `CredentialKind`, `CredentialSource`, `ModelSource`, `DetailedOptions`, `DetailedResult`, and `ConfigureLLMWithMetadata` API specified above without changing existing `Result`, `Options`, `Prompter`, or `ConfigureLLM` declarations.
2. Refactor the internal credential flow to return `(string, CredentialSource, error)` and cover env, existing explicit source, old existing literal, prompt literal, local, and no-key remote paths.
3. Make discovery return models plus `ModelSource`; propagate manual selection in both empty-catalog and `Other` paths.
4. Pass `Options.Existing.Fallbacks` to fallback selection and compute preselected indices by model ID, excluding primary/unknown/duplicate entries.
5. Trim and validate manual IDs and base URLs. Keep secret masking at the prompt boundary.
6. Add compile-time/API tests with positional `Result` and `Options` literals proving the old entry point remains source-compatible and reads the same `Result.APIKey`.

**Named tests:**

* `TestConfigureLLM_EnvCredentialReportsReference`
* `TestConfigureLLM_ExistingCredentialSourcePreserved`
* `TestConfigureLLM_LegacyExistingKeyInfersLiteral`
* `TestConfigureLLM_LocalCredentialSource`
* `TestConfigureLLM_ModelSourceLiveStaticManual`
* `TestConfigureLLM_ExistingFallbacksPreselected`
* `TestConfigureLLM_FallbackPreselectionDropsPrimaryUnknownAndDuplicates`
* `TestConfigureLLM_RawCredentialNeverRendered`

**Verification:**

```bash
gofmt -w wizard/configure.go wizard/configure_test.go wizard/fake_prompter_test.go
golint wizard/configure.go
golint wizard/configure_test.go
golint wizard/fake_prompter_test.go
go test -count=1 ./wizard ./llmprovider ./logging
go test -race -count=1 ./wizard ./llmprovider
make test
make vet
make lint
git diff --check
```

**Commit:** `feat(wizard): report credential and model provenance`

**Exit gate:** API is additive, old result consumers compile, all provenance/fallback tests pass, and no raw sentinel secret appears in captured UI text.

### Release gate R1 — Publish `mcplib` v1.3.0 before MagicTools source migration

1. Review the Phase 1 commit and release notes in `mcplib`.
2. Obtain explicit authorization in the same turn before any push or published tag.
3. Tag exactly v1.3.0 and publish it through the repository's normal release process.
4. From a clean temporary module or MagicTools, verify `go list -m github.com/maccavelli/mcplib@v1.3.0` resolves to the intended commit and its checksum is available.
5. If publication is not authorized or the version cannot resolve, stop here. Do not commit a `replace ../mcplib`, vendor a private snapshot, or begin MagicTools commits that require an unreleased API.

**Exit gate:** immutable v1.3.0 resolves to the reviewed Phase 1 commit. This gate itself never grants permission to push.

### Phase 2 — Add persisted credential references and fix real runtime consumers

**Repository:** MagicTools.

**Files:**

* modify `go.mod`, `go.sum`;
* modify `internal/config/config.go`;
* add `internal/config/credentials.go`, `internal/config/credentials_test.go`;
* modify `internal/intelligence/hydrator.go`; add `internal/intelligence/config_contract_test.go`;
* modify `internal/llm/pool.go`; add `internal/llm/config_contract_test.go`;
* modify `internal/vector/client.go`, `internal/vector/init_helpers.go`; add `internal/vector/config_contract_test.go`;
* modify `internal/client/sync_save_helpers.go`; add `internal/client/sync_save_helpers_test.go`;
* modify Fast-provider availability checks in `cmd/mcp-server-magictools/main.go`; modify `main_extra_test.go`;
* expand `internal/provider/contract_test.go`.

**Actions:**

1. Upgrade with `go get github.com/maccavelli/mcplib@v1.3.0`; run `go mod tidy`; verify no local `replace` remains.
2. Add the three env-reference fields to `IntelligenceEngine` with matching JSON/mapstructure/YAML tags.
3. Implement pure source classification and injected runtime resolution. Store env-resolved values only in runtime-only state and expose read-only accessors.
4. Replace every production direct credential read found by `rg 'Intelligence\.(APIKey|ThinkingAPIKey|EmbeddingAPIKey)' --glob '*.go'` with the correct accessor, except persistence/source-classification code and tests deliberately exercising raw schema fields.
5. Replace provider-plus-nonempty-key availability predicates with canonical descriptor capability checks. A configured provider/model is usable when either it is keyless or its resolved credential is non-empty.
6. Pass Fast and Thinking base URLs to initial providers, fallback providers, probes, and every remaining reload/construction path.
7. Make Embedding constructors use the resolved credential and exact configured endpoint.
8. Expand provider contracts to traverse hydrator availability, pool Fast/Thinking creation, fallback creation, and vector construction. Use fake HTTP endpoints for deterministic local/provider requests; do not call real networks.

**Named tests:**

* `TestCredentialReference_ClassificationMatrix`
* `TestResolveCredentials_EnvironmentLiteralLocalAndMissing`
* `TestResolvedEnvironmentSecretCannotMarshal`
* `TestHydrator_KeylessOllamaIsAvailable`
* `TestPool_KeylessOllamaFastAndThinking`
* `TestPool_CustomBaseURLAppliedToPrimaryThinkingAndFallbacks`
* `TestAdvertisedGenerativeProvidersReachRealConsumers`
* `TestAdvertisedEmbeddersReachRealConsumers`

**Verification:**

```bash
gofmt -w internal/config/config.go internal/config/credentials.go internal/config/credentials_test.go internal/intelligence/hydrator.go internal/intelligence/config_contract_test.go internal/llm/pool.go internal/llm/config_contract_test.go internal/vector/client.go internal/vector/init_helpers.go internal/vector/config_contract_test.go internal/client/sync_save_helpers.go internal/client/sync_save_helpers_test.go cmd/mcp-server-magictools/main.go cmd/mcp-server-magictools/main_extra_test.go internal/provider/contract_test.go
for phase_file in internal/config/config.go internal/config/credentials.go internal/config/credentials_test.go internal/intelligence/hydrator.go internal/intelligence/config_contract_test.go internal/llm/pool.go internal/llm/config_contract_test.go internal/vector/client.go internal/vector/init_helpers.go internal/vector/config_contract_test.go internal/client/sync_save_helpers.go internal/client/sync_save_helpers_test.go cmd/mcp-server-magictools/main.go cmd/mcp-server-magictools/main_extra_test.go internal/provider/contract_test.go; do golint "$phase_file"; done
go test -count=1 ./internal/config ./internal/provider ./internal/intelligence ./internal/llm ./internal/vector ./internal/client ./cmd/mcp-server-magictools
go test -race -count=1 ./internal/config ./internal/intelligence ./internal/llm ./internal/vector
go vet ./internal/config ./internal/provider ./internal/intelligence ./internal/llm ./internal/vector ./internal/client ./cmd/mcp-server-magictools
make lint
git diff --check
```

**Commit:** `feat(config): resolve credential references at runtime`

**Exit gate:** v1.3.0 is the only mcplib source, keyless Ollama reaches all advertised generative consumers, endpoints reach every provider construction, and env sentinel bytes cannot be serialized.

### Phase 3 — Make decoding, validation, locking, and replacement authoritative

**Files:**

* add `internal/config/validate.go`, `internal/config/validate_test.go`;
* modify `internal/config/store.go`, `store_test.go`;
* modify `internal/config/patch.go`, `patch_yaml.go`; add `patch_yaml_test.go`;
* modify `internal/config/filelock_unix.go`, `filelock_windows.go`; add `filelock_unix_test.go`, `filelock_windows_test.go`;
* split `internal/config/atomic_write.go` into common staging plus `replace_unix.go` and `replace_windows.go`; add `atomic_write_test.go`, `replace_windows_test.go`;
* add `internal/config/testdata/preservation/*` fixtures.

**Actions:**

1. Make `DecodeConfiguration` unmarshal the complete MagicTools configuration block without initialization, migration, environment lookup, default injection, or companion-file I/O.
2. Implement the validation matrix above as pure functions returning ordered `ValidationIssue{Path, Code, Message}` values.
3. Add `ApplyValidated(ctx, patch, ApplyOptions)`; keep `Apply` temporarily as a compatibility wrapper using structural validation.
4. Add semantic preconditions containing path plus expected old value hash. Under the lock, compare each touched path in the latest YAML; return `ErrConflict` with sorted paths when a touched value changed. Ignore unrelated on-disk changes and patch the latest document.
5. Validate the post-patch candidate while the lock is held and before creating/replacing a target.
6. Make patch helpers append fully qualified paths only when semantic values actually change. Removing an absent key and setting an equal value are no-ops.
7. Preserve unknown nodes, key order, comments, anchors, and scalar style outside replaced owned nodes. Explicitly document that yaml.v3 may normalize indentation, blank lines, and CRLF after a semantic write; no-op transactions remain byte-identical and preserve mtime.
8. Replace blocking locks with a context-aware acquisition loop: immediate nonblocking attempt, then waits starting at 25 ms and capped at 250 ms until acquired or `ctx.Done()`. Use `LOCK_NB` on Unix and `LOCKFILE_FAIL_IMMEDIATELY` on Windows.
9. Keep same-directory temp write, `0600`, file sync, close, and parent sync. Use `os.Rename` on Unix. Use `windows.MoveFileEx` with `MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH` on Windows and describe it as Windows replace-existing semantics, not a universal atomicity guarantee.
10. Inject filesystem/replace operations behind unexported test seams so write, sync, close, replace, and parent-sync failures are reproducible.
11. Strengthen concurrent store tests to assert every goroutine error and both independent semantic changes.

**Named tests:**

* `TestDecodeConfiguration_CompleteAndSideEffectFree`
* `TestValidateCandidate_FastThinkingEmbeddingBackplaneMatrix`
* `TestStore_RejectsCandidateWithoutWrite`
* `TestStore_MergesIndependentConcurrentPaths`
* `TestStore_ConflictingTouchedPathReturnsConflict`
* `TestStore_ContextCancellationStopsLockWait`
* `TestPatch_ChangedPathsAreQualifiedAndSemantic`
* `TestPatch_PreservationContractGolden`
* `TestPatch_NoOpPreservesBytesAndMtime`
* `TestAtomicWrite_FaultMatrixPreservesReadableTarget`
* Windows build-tag tests for replace-existing and lock cancellation.

**Verification:**

```bash
gofmt -w internal/config/validate.go internal/config/validate_test.go internal/config/store.go internal/config/store_test.go internal/config/patch.go internal/config/patch_yaml.go internal/config/patch_yaml_test.go internal/config/filelock_unix.go internal/config/filelock_unix_test.go internal/config/filelock_windows.go internal/config/filelock_windows_test.go internal/config/atomic_write.go internal/config/atomic_write_test.go internal/config/replace_unix.go internal/config/replace_windows.go internal/config/replace_windows_test.go
golint internal/config/validate.go
golint internal/config/store.go
golint internal/config/patch.go
golint internal/config/patch_yaml.go
golint internal/config/atomic_write.go
for phase_file in internal/config/validate_test.go internal/config/store_test.go internal/config/patch_yaml_test.go internal/config/filelock_unix.go internal/config/filelock_unix_test.go internal/config/filelock_windows.go internal/config/filelock_windows_test.go internal/config/atomic_write_test.go internal/config/replace_unix.go internal/config/replace_windows.go internal/config/replace_windows_test.go; do golint "$phase_file"; done
go test -count=1 ./internal/config
go test -race -count=1 ./internal/config
GOOS=windows GOARCH=amd64 go build ./internal/config
go vet ./internal/config
make lint
git diff --check
```

**Commit:** `feat(config): validate and commit semantic patches safely`

**Exit gate:** invalid candidates and conflicts make no write, lock waits cancel, no-op mtime is stable, failure injection preserves a readable old target, and Windows code cross-compiles.

### Phase 4 — Introduce the typed configure application service

**Files:**

* add `internal/configure/types.go`;
* add `internal/configure/service.go`;
* add `internal/configure/session.go`;
* add `internal/configure/validation.go`;
* add `internal/configure/activation.go`;
* add `internal/configure/service_test.go`, `session_test.go`, `activation_test.go`.

**Actions:**

1. Define injected interfaces for store, environment lookup, generative discovery, embedder online check, clock, active-state reader, and output-neutral result collection.
2. `Service.Load(ctx)` reads one immutable persisted snapshot, retains its canonical hash/path, and creates a deep-copy draft. It performs no writes and no environment resolution.
3. `Session` exposes typed operations `SetFast`, `ClearFast`, `SetThinking`, `ClearThinking`, `SetEmbedding`, `ClearEmbedding`, `SetBackplane`, and `DisableBackplane`. Each operation mutates only the draft and records intent; no command adapter edits `config.Config` directly.
4. Every set/switch operation removes incompatible literal/env/base-URL fields in the draft. Every clear operation removes its complete owned group. Clearing Fast while Backplane is enabled returns a dependency error unless Backplane disable is staged in the same session.
5. Build a typed patch by diffing persisted and draft semantic values. Generate store preconditions for every touched path from the original snapshot.
6. `Validate` applies pure MagicTools validation and returns all ordered issues. `Commit` calls `ApplyValidated`, maps semantic changes to secret-free metadata and activation classes, and never mutates the original persisted snapshot.
7. After a conflict, retain the draft in memory, reload a new persisted snapshot on explicit retry, show conflicting paths, and require the user/adapter to confirm or resubmit; never overwrite silently.
8. Provide fake dependencies and deterministic clocks in tests. No package test should require Pterm, a real terminal, service process, provider network, or user's environment.

**Named tests:**

* `TestServiceLoad_PersistedAndDraftAreIndependent`
* `TestSession_ProviderSwitchCleansIncompatibleFields`
* `TestSession_ClearFastRequiresBackplaneDisable`
* `TestSession_CrossTierReuseRequiresSameProviderAndPreservesSource`
* `TestCommit_ValidatesLatestCandidateUnderLock`
* `TestCommit_ConflictRetainsDraftAndReportsPaths`
* `TestResult_RedactsAllCredentialValues`
* `TestActivationClassification_AllOwnedPaths`

**Verification:**

```bash
gofmt -w internal/configure/*.go
for phase_file in internal/configure/*.go; do golint "$phase_file"; done
go test -count=1 ./internal/configure ./internal/config
go test -race -count=1 ./internal/configure ./internal/config
go vet ./internal/configure ./internal/config
make lint
git diff --check
```

**Commit:** `feat(configure): add transactional configuration service`

**Exit gate:** a deterministic non-UI test can stage all four sections, validate once, commit once, and receive only secret-free structured results.

### Phase 5 — Rebuild the guided wizard as a thin adapter

**Files:**

* reduce/split `cmd/mcp-server-magictools/config.go` into command registration/controller files;
* add `configure_fast.go`, `configure_thinking.go`, `configure_embedding.go`, `configure_backplane.go`, `configure_render.go`;
* modify `pterm_prompter.go`;
* replace `config_wizard_test.go` and `config_extra_test.go` with scripted controller tests;
* add `configure_pty_unix_test.go` for the PTY integration test.

**Actions:**

1. Check that stdin is a terminal before initialization, `config.New`, model discovery, or any other side effect for plain guided `configure`.
2. If required files are missing after the TTY check, ask once to create only missing defaults through the safe initializer; decline/cancel exits without writes.
3. Replace presentation-string parsing and first-character routing with stable internal action IDs.
4. Pass `cmd.Context()` through every service, discovery, endpoint, prompt-controller, lock, and online operation. Remove configure-path `context.Background()`.
5. Make every prompt/controller return a typed cancel or error. Cancel returns to the previous menu without staging partial input; prompt/system failures return to Cobra and exit non-zero.
6. Fast supports set/switch/clear, current fallback preselection, exact custom model IDs, and source-aware credentials from `mcplib` v1.3.0.
7. Thinking supports set/switch/clear and source-aware credentials. Do not require a key for canonical keyless providers.
8. Replace embedding label parsing with `EmbeddingModel{ID, Label, Dimensions}` catalog entries. Default provider/model/dimensions to the current compatible values. Prefer the same tier's valid current source before offering same-provider cross-tier reuse. Clear stale URLs on provider switches and preserve complete custom model text.
9. Backplane `Keep current` returns immediately without staging. Numeric prompts show accepted ranges, reject invalid values in-place, and allow documented zero semantics. Enabling without valid staged/persisted Fast is blocked before save.
10. `Show persisted/staged` renders an explicit semantic diff, config path, source metadata, discovery status, and pending restart/rebuild actions. Never label staged data `Current` or print any secret fragment.
11. Final Save validates and commits once. Print `saved` only after store success, then list changed paths and required next actions. Exit-without-save and cancel keep all files byte-identical.

**Named tests:**

* `TestConfigure_NonTTYFailsBeforeAnySideEffect`
* `TestConfigure_AllStableMenuActionsRouteCorrectly`
* `TestConfigure_CancelAndErrorAtEveryPrompt`
* `TestConfigure_FastThinkingEmbeddingBackplaneMatrix`
* `TestConfigure_ExistingFallbacksAndDefaultsPreserved`
* `TestConfigure_EmbeddingProviderSafeCredentialDefaults`
* `TestConfigure_BackplaneKeepIsNoOpAndRangesReprompt`
* `TestConfigure_ShowSeparatesPersistedStagedAndActions`
* `TestConfigure_SaveErrorExitsNonZeroWithoutSuccessText`
* `TestConfigure_ExitWithoutSaveIsByteIdentical`
* `TestConfigure_PTYHappyPath`

**Verification:**

```bash
gofmt -w cmd/mcp-server-magictools/config.go cmd/mcp-server-magictools/configure_fast.go cmd/mcp-server-magictools/configure_thinking.go cmd/mcp-server-magictools/configure_embedding.go cmd/mcp-server-magictools/configure_backplane.go cmd/mcp-server-magictools/configure_render.go cmd/mcp-server-magictools/pterm_prompter.go cmd/mcp-server-magictools/config_wizard_test.go cmd/mcp-server-magictools/config_extra_test.go cmd/mcp-server-magictools/configure_pty_unix_test.go
for phase_file in cmd/mcp-server-magictools/config.go cmd/mcp-server-magictools/configure_fast.go cmd/mcp-server-magictools/configure_thinking.go cmd/mcp-server-magictools/configure_embedding.go cmd/mcp-server-magictools/configure_backplane.go cmd/mcp-server-magictools/configure_render.go cmd/mcp-server-magictools/pterm_prompter.go cmd/mcp-server-magictools/config_wizard_test.go cmd/mcp-server-magictools/config_extra_test.go cmd/mcp-server-magictools/configure_pty_unix_test.go; do golint "$phase_file"; done
go test -count=1 ./cmd/mcp-server-magictools ./internal/configure ./internal/config ./internal/provider
go test -race -count=1 ./cmd/mcp-server-magictools ./internal/configure ./internal/config
go vet ./cmd/mcp-server-magictools ./internal/configure ./internal/config ./internal/provider
make lint
git diff --check
```

**Commit:** `feat(cli): drive configure wizard through application service`

**Exit gate:** no skipped wizard test, no ignored prompt error, no label parsing, no direct draft mutation in `cmd`, and the full guided workflow is scripted without a real provider.

### Phase 6 — Add script-safe show, validate, and declarative apply

**Files:**

* add `cmd/mcp-server-magictools/configure_show.go`;
* add `configure_validate.go`, `configure_apply.go`;
* add `internal/configure/spec.go`, `spec_test.go`;
* add `internal/configure/render_text.go`, `render_json.go` and golden tests;
* add `internal/configure/testdata/result-text.golden` and `result-json.golden`;
* modify Cobra registration and CLI tests.

**Actions:**

1. Register the exact commands/flags from the Public CLI Contract and delete `--force`/`--non-interactive` from configure help.
2. Implement strict, size-limited, one-document spec decode with unknown-field rejection and ordered path-specific errors.
3. Map all present spec sections into one service session. Reject literal credential kinds and cross-section dependency violations before store access.
4. Implement dry-run without write locking or mtime change. Regular apply uses one atomic service commit.
5. Implement offline validate with no env lookup/network. Implement bounded online validation exactly as specified and propagate cancellation.
6. Render one stable JSON document to stdout in JSON mode. Send progress and human diagnostics to stderr. Text mode uses concise persisted/active/actions sections.
7. Add golden JSON/text fixtures and normalize only injected clock/path values.

**Named tests:**

* `TestConfigureApply_StrictSchemaAndSizeLimit`
* `TestConfigureApply_AtomicMultiSectionRequest`
* `TestConfigureApply_RejectsLiteralSecretInput`
* `TestConfigureApply_DryRunDoesNotLockOrWrite`
* `TestConfigureValidate_OfflineHasNoEnvOrNetworkCalls`
* `TestConfigureValidate_OnlineTimeoutCancellationAndEmbeddingDimensions`
* `TestConfigureJSON_OneDocumentAndNoSecretLeak`
* `TestConfigureSubcommandHelpContract`

**Verification:**

```bash
gofmt -w cmd/mcp-server-magictools/configure_show.go cmd/mcp-server-magictools/configure_validate.go cmd/mcp-server-magictools/configure_apply.go internal/configure/spec.go internal/configure/spec_test.go internal/configure/render_text.go internal/configure/render_json.go
for phase_file in cmd/mcp-server-magictools/configure_show.go cmd/mcp-server-magictools/configure_validate.go cmd/mcp-server-magictools/configure_apply.go internal/configure/spec.go internal/configure/spec_test.go internal/configure/render_text.go internal/configure/render_json.go; do golint "$phase_file"; done
go test -count=1 ./cmd/mcp-server-magictools ./internal/configure ./internal/config
go test -race -count=1 ./cmd/mcp-server-magictools ./internal/configure ./internal/config
go vet ./cmd/mcp-server-magictools ./internal/configure ./internal/config
make lint
git diff --check
```

**Commit:** `feat(cli): add script-safe configure inspection and apply`

**Exit gate:** all three non-interactive commands work on pipes, JSON is stable and secret-free, apply is atomic, and offline operations make zero network/env-resolution calls.

### Phase 7 — Make runtime activation state truthful

**Files:**

* modify `cmd/mcp-server-magictools/main.go`, `service.go`, `main_extra_test.go`, and `service_hardening_test.go`;
* add `internal/config/intelligence_fingerprint.go`, `intelligence_fingerprint_test.go`;
* modify `internal/config/watcher.go`, `watcher_test.go`;
* modify `internal/handler/handlers.go`; add `internal/handler/activation_test.go`;
* remove unused `internal/llm.Pool.Reload` from `internal/llm/pool.go`; modify `pool_test.go`.

**Actions:**

1. Compute the desired intelligence fingerprint canonically and independently of YAML formatting.
2. Populate a versioned active snapshot only after Fast, Thinking, Embedding/vector, and Backplane construction has succeeded. Write it to the resolved config directory's owner-only `service.state` using the safe writer.
3. Update service-status readers to resolve state beside the selected config and remain backward-compatible with old state files by reporting active state `unknown`.
4. Implement active relation calculation with verified PID and canonical path checks. Never infer active state solely from disk.
5. Remove `w.liveConfig.Intelligence = cfg.Intelligence`. Continue live application only for fields the handler actually reloads; log one restart-required message when desired intelligence changes.
6. Confirm `rg -n '\.Reload\(' internal cmd` has no production `Pool.Reload` caller, remove the partial method, and keep a watcher regression proving intelligence changes never attempt a pool reload.
7. Update `update_config` reporting so only actual handler-applied fields say `live`; configure-owned intelligence paths say restart/rebuild.

**Named tests:**

* `TestIntelligenceFingerprint_IsStableAndCredentialSensitive`
* `TestServiceState_ActiveSnapshotContainsNoSecrets`
* `TestActiveRelation_MatchesDiffersNotRunningUnknown`
* `TestWatcher_IntelligenceChangeDoesNotMutateActiveRuntimeConfig`
* `TestWatcher_LiveFieldsStillApply`
* `TestUpdateConfig_ActivationMessageMatchesBehavior`

**Verification:**

```bash
gofmt -w cmd/mcp-server-magictools/main.go cmd/mcp-server-magictools/service.go cmd/mcp-server-magictools/main_extra_test.go cmd/mcp-server-magictools/service_hardening_test.go internal/config/intelligence_fingerprint.go internal/config/intelligence_fingerprint_test.go internal/config/watcher.go internal/config/watcher_test.go internal/handler/handlers.go internal/handler/activation_test.go internal/llm/pool.go internal/llm/pool_test.go
for phase_file in cmd/mcp-server-magictools/main.go cmd/mcp-server-magictools/service.go cmd/mcp-server-magictools/main_extra_test.go cmd/mcp-server-magictools/service_hardening_test.go internal/config/intelligence_fingerprint.go internal/config/intelligence_fingerprint_test.go internal/config/watcher.go internal/config/watcher_test.go internal/handler/handlers.go internal/handler/activation_test.go internal/llm/pool.go internal/llm/pool_test.go; do golint "$phase_file"; done
go test -count=1 ./cmd/mcp-server-magictools ./internal/config ./internal/handler ./internal/llm
go test -race -count=1 ./cmd/mcp-server-magictools ./internal/config ./internal/handler
GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/mcp-server-magictools
go vet ./cmd/mcp-server-magictools ./internal/config ./internal/handler ./internal/llm
make lint
git diff --check
```

**Commit:** `fix(runtime): report desired and active intelligence separately`

**Exit gate:** a running service remains on its constructed intelligence generation after a disk edit, and show accurately reports `differs` plus restart/rebuild actions.

### Phase 8 — Unify paths and make reset recoverable

**Files:**

* modify `internal/config/paths.go`, `paths_test.go`, `config.go`, `config_test.go`, `watcher.go`, and `watcher_test.go`;
* add `internal/config/reset.go`, `reset_test.go`;
* modify `cmd/mcp-server-magictools/init.go`, `init_test.go`;
* modify service-state path consumers in `cmd/mcp-server-magictools/service.go` and `service_hardening_test.go`.

**Actions:**

1. Replace remaining default-path companion reads/writes, including initial override loading and IDE migration save, with `cfg.Paths` path-taking APIs.
2. Define lock order globally: acquire `<config-dir>/.reset.lock` first when a multi-file reset is involved, then `<config>.lock`; ordinary config transactions acquire only `<config>.lock`. Add a lock-order comment and test helper.
3. Before reset, `Lstat` every target and reject directories, symlinks, devices, and targets outside the resolved directory.
4. Stage all three templates in the target directory, set owner-only modes, parse/validate each, sync, and close before replacing anything.
5. Create a unique UTC generation ID and owner-only backups using `<name>.backup.<generation>`; never overwrite a backup.
6. Write and sync `.reset-transaction.json` containing generation, exact targets/backups, completed replacements, and state. Replace files one at a time through the platform helper, updating/syncing the journal after each.
7. On a handled failure, restore completed targets from backups in reverse order and return non-zero. On the next `init`, `configure`, or store open, detect an incomplete journal and restore before proceeding; preserve the journal if recovery fails.
8. Remove the journal only after all replacements and directory sync succeed. Print backup paths and restore instructions.
9. Confirmed reset may leave a momentary multi-path transition because filesystems do not provide a three-file atomic rename. The success/error/recovery contract is: no completed command returns a mixed generation, and an interrupted transaction is detected and recovered before the next MagicTools configuration operation.

**Named tests:**

* `TestAlternateConfig_AllCompanionLoadSaveMigrateWatchPaths`
* `TestReset_DeclineAndNonTTYWithoutYesDoNotWrite`
* `TestReset_RejectsSymlinkDirectoryAndEscapingTargets`
* `TestReset_BackupsAreUniqueOwnerOnlyAndRestorable`
* `TestReset_FailureAtEachReplacementRollsBack`
* `TestReset_InterruptedJournalRecoversBeforeNextOperation`
* `TestResetAndConfigure_RespectGlobalLockOrder`

**Verification:**

```bash
gofmt -w internal/config/paths.go internal/config/paths_test.go internal/config/config.go internal/config/config_test.go internal/config/watcher.go internal/config/watcher_test.go internal/config/reset.go internal/config/reset_test.go cmd/mcp-server-magictools/init.go cmd/mcp-server-magictools/init_test.go cmd/mcp-server-magictools/service.go cmd/mcp-server-magictools/service_hardening_test.go
for phase_file in internal/config/paths.go internal/config/paths_test.go internal/config/config.go internal/config/config_test.go internal/config/watcher.go internal/config/watcher_test.go internal/config/reset.go internal/config/reset_test.go cmd/mcp-server-magictools/init.go cmd/mcp-server-magictools/init_test.go cmd/mcp-server-magictools/service.go cmd/mcp-server-magictools/service_hardening_test.go; do golint "$phase_file"; done
go test -count=1 ./internal/config ./cmd/mcp-server-magictools
go test -race -count=1 ./internal/config ./cmd/mcp-server-magictools
GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/mcp-server-magictools
go vet ./internal/config ./cmd/mcp-server-magictools
make lint
git diff --check
```

**Commit:** `fix(config): make reset and alternate paths recoverable`

**Exit gate:** alternate paths are end-to-end consistent; reset rejects unsafe targets, backs up all existing files, rolls back handled failures, and recovers an interrupted journal.

### Phase 9 — Retire unsafe persistence and align documentation

**Files:**

* remove `Config.SaveConfiguration` and obsolete tests after caller migration;
* remove dead patch fields/helpers or complete their schema wiring;
* modify `README.md`;
* modify `docs/guides/cli-reference.md`, `configuration.md`, `getting-started.md`, `operations-and-security.md`, and `repository-assessment.md`;
* modify `internal/config/templates.go`;
* qualify mcplib MADR/PLAN references in code/tests;
* update MADR/PLAN status only through the approval workflow.

**Actions:**

1. Prove with `rg` that no production caller uses broad `SaveConfiguration`, then delete it and rewrite/remove characterization tests that encoded its defects.
2. Ensure every `*_api_key_env` patch field reaches schema, validation, runtime resolution, and tests. Remove no-longer-used helpers such as label parsers and background-context wrappers.
3. Remove contradictory general hot-reload claims. Publish an exact live-vs-restart table.
4. Document credential source precedence, service-manager environment requirements, missing-reference behavior, online-check network/cost implications, and literal-key compatibility.
5. Document exact CLI syntax, v1 apply schema, strict parsing, JSON result schema, examples for env/local sources, dry-run, conflict retry, and clear/disable.
6. Document provider support from generated/shared catalog facts, Backplane prerequisite/ranges/zero semantics, active-state meanings, vector rebuild, reset backups/journal recovery, and YAML presentation-preservation limits.
7. Qualify cross-repository references to mcplib MADR/PLAN 0004 and correct the MagicTools MADR finding wording; do not claim those records are missing.
8. Keep accepted MADR 0002 accepted after implementation; completion satisfies its confirmation criteria but does not supersede the decision.

**Verification:**

```bash
rg -n 'SaveConfiguration\(|context\.Background\(\)' cmd/mcp-server-magictools internal/configure internal/config internal/intelligence internal/llm internal/vector
rg -n 'non-interactive|configure.*--force|hot.reload|MADR 0004|PLAN 0004' README.md docs cmd internal
go test -count=1 ./...
go vet ./...
make lint
git diff --check
```

Review every search result; expected test-only or qualified-reference hits must be documented in the commit message.

**Commit:** `docs(configure): document truthful configuration lifecycle`

**Exit gate:** code, help, templates, guides, and records describe one provider, credential, validation, activation, and reset contract; no broad primary-config writer remains.

### Phase 10 — Full verification and release candidate

**Writes:** test artifacts only in temporary directories; no source changes unless a failed gate is fixed and recommitted in the owning phase.

Run from MagicTools:

```bash
go test -count=1 ./cmd/mcp-server-magictools ./internal/configure ./internal/config ./internal/provider ./internal/llm ./internal/vector ./internal/intelligence ./internal/handler ./internal/client
go test -race -count=1 ./cmd/mcp-server-magictools ./internal/configure ./internal/config ./internal/llm ./internal/vector ./internal/intelligence ./internal/handler
go vet ./...
go test -count=1 ./...
make lint
git diff --check
git status --short
```

Run from `../mcplib`:

```bash
go test -count=1 ./...
go test -race -count=1 ./wizard ./llmprovider
go vet ./...
make lint
git diff --check
git status --short
```

Cross-platform CI must run Linux, macOS, and Windows. At minimum it must execute config/configure tests natively so lock and replacement behavior is not covered only by cross-compilation.

Manual smoke test in an isolated temporary config directory:

1. Initialize; confirm three files and owner-only permissions where supported.
2. Run guided configure and exit without saving; compare hashes and mtimes.
3. Configure keyless Ollama Fast and Thinking against a fake/local endpoint; verify no key prompt and runtime consumer construction.
4. Configure a remote Fast tier using an env sentinel; verify sentinel absence from YAML, JSON, terminal output, state, logs, and diffs.
5. Re-enter Fast; verify existing fallbacks are preselected and no-op save does not write.
6. Switch Embedding providers; verify same-tier default, exact model ID, stale URL cleanup, and rebuild action.
7. Attempt Backplane without Fast and invalid boundary values; verify rejection and unchanged file.
8. Save intelligence while a service runs; verify desired/active `differs`, then restart and verify `matches`.
9. Run offline validate with network disabled; verify no attempted connection. Run explicit online validate against controlled fake providers.
10. Apply a multi-section spec with `--dry-run`, then apply it; compare JSON and focused YAML diff.
11. Race `configure apply` with `update_config`; verify independent paths merge and same paths conflict explicitly.
12. Decline reset; then accept reset, verify backups, inject/reproduce recovery where supported, and restore one generation manually.
13. Repeat show/validate/apply and service-state checks with an alternate `--config` directory.

**Final commit:** only if Phase 10 requires scoped test/gate fixes; otherwise Phase 9 is the final MagicTools commit. Do not create an empty commit.

**Exit gate:** all automated and manual checks pass, CI is green on three OS families, worktrees contain only intended changes, and every MADR confirmation bullet maps to a named passing test below.

## Verification matrix

| MADR confirmation / finding | Required evidence |
|---|---|
| CF2-01 env secret persistence | mcplib provenance tests; `TestResolvedEnvironmentSecretCannotMarshal`; guided/apply JSON sentinel scans. |
| CF2-02 keyless Ollama | hydrator/pool real-consumer tests with empty key; guided no-secret-prompt test. |
| CF2-03 Backplane without Fast | pure validation matrix; guided/apply dependency tests; unchanged-disk assertion. |
| CF2-04 / CF2-17 desired vs active | active snapshot/fingerprint tests; watcher non-mutation test; running-service smoke test. |
| CF2-05 validation gap | complete side-effect-free decoder; table tests for every field group/range/conflict. |
| CF2-06 prompt errors | scripted error/cancel at every interaction; Cobra non-zero test; no success-text assertion. |
| CF2-07 pre-TTY side effects | non-TTY test with fake initializer/loader/store call counters all zero. |
| CF2-08 credential reuse | provider/source matrix for same-tier and cross-tier choices. |
| CF2-09 endpoint propagation | primary/Thinking/fallback/hydrator/vector fake-server contract tests. |
| CF2-10 reset | unsafe target, backup, each-step failure, journal recovery, race tests. |
| CF2-11 Windows replacement | native Windows replace and fault tests plus compile gate. |
| CF2-12 alternate paths | one integration test covering load, migration, store, watcher, reset, and state. |
| CF2-13 empty Thinking patch | URL-only semantic patch test and qualified changed path. |
| CF2-14 embedding stale URL/model truncation | provider-switch cleanup and exact custom ID round-trip tests. |
| CF2-15 fallback clearing | mcplib preselection tests and MagicTools no-op re-entry test. |
| CF2-16 Backplane keep/ranges/zero | no-op keep, boundary table, and runtime default consistency tests. |
| CF2-18 rejected flags | help golden contains subcommands and excludes both flags. |
| CF2-19 references/docs | qualified cross-repo link search and generated support/hot-reload table review. |
| CF2-20 wizard coverage | no skipped configure tests; full action/error matrix plus one PTY test. |
| CF2-21 YAML/change metadata | preservation golden, semantic changed paths, byte-identical no-op. |
| CF2-22 cancellation | discovery/online/lock cancellation tests using command context. |
| CF2-23 Fast clear | guided and declarative clear tests, including Backplane dependency. |

## Commit and approval discipline

1. Commit at the end of every completed phase in the repository that phase changes.
2. Before each commit, identify staged Go files, run `gofmt` and per-file `golint` (or a repository wrapper if one is added), and do not commit a failed check.
3. Do not stage unrelated user changes. Review `git diff --staged --check` and the complete staged diff.
4. Do not push either repository, publish a tag, or open/update a PR unless the user explicitly authorizes that publication in the same turn.
5. If a new finding requires files, public APIs, commands, migrations, or behavior outside this plan, stop that work, report it, and obtain a plan amendment plus approval.

## Rollout

1. Release `mcplib` v1.3.0 first; verify proxy resolution and checksum.
2. Merge MagicTools phases in dependency order. Do not ship an intermediate MagicTools build that imports unreleased shared APIs.
3. Treat the MagicTools release as backward-compatible for existing literal YAML fields but behavior-changing for validation, rejected unsafe configurations, removed configure flags, restart reporting, and reset recovery.
4. Release notes must include:
   * env references are now persisted by name;
   * existing literal keys continue to work and are not auto-migrated;
   * incomplete/conflicting configurations that previously saved may now fail validation;
   * intelligence changes require restart, and embedding identity changes require index rebuild;
   * `configure show|validate|apply` syntax and apply schema version;
   * `init --force` backup locations and recovery;
   * service-manager env variables must be configured in the service environment, not only the invoking shell.
5. After deployment, monitor structured errors by stage (`decode`, `validate`, `lock`, `conflict`, `replace`, `online_check`, `active_state`) and support reports for legacy YAML validation. Never log patch objects or resolved credentials.

## Rollback

Rollback is phase-aware and must not destroy user configuration.

* **Before a MagicTools release:** revert only the affected commits in reverse phase order. Keep v1.3.0 published; its API is additive and other consumers may already depend on it. Never retag or delete the released version.
* **After a MagicTools release:** ship a forward revert binary that still reads the three `*_api_key_env` fields. Do not roll back to a binary that ignores env references without first warning operators that credentials may become unavailable.
* **Configuration data:** do not restore an older `config.yaml` automatically. New env-reference keys are harmless unknown fields to many old paths but may not be resolved; instruct users to keep the new binary or explicitly choose a literal only if they accept plaintext persistence.
* **Reset:** use the recorded backup generation and journal recovery command/path documented by Phase 8. Preserve failed journals and backups until recovery is verified.
* **Service state:** old binaries ignore additive JSON fields; new CLI treats old state schemas as `unknown`. Deleting a stale state file is safe only after verifying the recorded PID is not the running MagicTools process.
* **Database/vector index:** configuration work does not delete index data. If an Embedding change is rolled back, restart with the prior config and allow the existing fingerprint logic to decide whether rebuild is needed.

Rollback acceptance requires a readable validated config, a booting service or an explicit actionable validation error, no credential in logs/output, and preserved reset backups.

## Acceptance criteria

Implementation is complete only when all of these statements are true:

* MagicTools uses released `mcplib` v1.3.0 with no committed local replace.
* An env-selected sentinel secret is absent from YAML, JSON, text output, logs, state, diffs, and test failure text.
* Every advertised provider/tier passes its real consumer contract; Ollama works keylessly for Fast and Thinking.
* Full candidate validation runs under lock immediately before commit and reports all issues deterministically.
* Independent concurrent edits merge; touched-path conflicts fail without overwriting either unseen value.
* No-op, cancel, dry-run, failed validation, failed prompt, canceled lock, and failed replacement preserve bytes and mtime.
* Guided configure supports set/switch/clear for every owned section and never parses presentation labels.
* `show`, `validate`, and `apply` meet their TTY, stdout/stderr, strict schema, JSON, and network contracts.
* A verified service exposes secret-free active state; disk intelligence edits do not mutate active runtime objects; restart changes `differs` to `matches`.
* All config, migration, watcher, state, configure, and reset paths honor the same resolved directory.
* Reset is confirmed, rejects unsafe targets, creates recoverable backups, rolls back handled failures, and recovers interrupted journals.
* Broad `SaveConfiguration` and false general hot-reload claims are gone.
* Every verification command and manual smoke test passes on the required platform matrix.
* MADR 0002's confirmation items and CF2-01 through CF2-23 have named passing evidence or an explicitly approved follow-up; none are silently deferred.
