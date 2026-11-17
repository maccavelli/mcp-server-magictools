---
status: proposed
date: 2026-07-31
implements: 0001-MADR-make-configure-updates-lossless.md
owners: MagicTools maintainers
scope: configure CLI, configuration persistence, provider capability wiring, activation contract, tests, and documentation
---

# Implementation plan for lossless, provider-capability-driven configure updates

## Purpose

This plan implements [MADR 0001](./0001-MADR-make-configure-updates-lossless.md). It converts the architectural outcome into ordered code changes, test gates, compatibility rules, and release criteria for `mcp-server-magictools configure` and the configuration paths it shares with the running orchestrator.

The plan is intentionally broader than fixing the Thinking Tier menu parser. The code currently couples initialization, runtime-effective configuration, whole-document persistence, provider selection, secret resolution, and hot reload. Fixing only the parser would leave silent non-persistence, stale values, destructive reset behavior, provider/runtime mismatches, and false success reporting intact.

## MADR Assessment

### Status and structural assessment

MADR 0001 is suitable to remain `proposed` until the maintainers accept the compatibility decisions in this plan. It follows MADR 4 conventions and contains the required context, considered options, decision outcome, consequences, and confirmation strategy.

The central decision is implementable without replacing YAML or Cobra/Pterm. Its findings remain accurate against the current tree:

* `configureThinkingTier` passes the complete Pterm label to `choiceToProvider`, while that parser accepts only `"1"` through `"4"` (`cmd/mcp-server-magictools/config.go:194-221`, `607-622`).
* `Config.SaveConfiguration` updates all intelligence keys only inside `if c.Intelligence.Provider != ""`, so a fresh Thinking or Embedding configuration can report success without persisting (`internal/config/config.go:742-815`).
* Thinking clear assigns empty strings, but save skips empty optional values rather than removing their YAML keys (`cmd/mcp-server-magictools/config.go:207-215`; `internal/config/config.go:767-775`).
* `ensureInitialized(..., force=true)` writes all three templates without confirmation (`cmd/mcp-server-magictools/config.go:771-807`), contradicting `README.md:179-182`.
* the generative runtime constructor in `mcplib` v0.2.0 supports Gemini, Claude, and OpenAI, while the Fast and Thinking menus also advertise Ollama; the pool additionally requires a Thinking API key (`internal/llm/pool.go:105-115`, `472-474`).
* the watcher copies the new `Intelligence` block into live configuration but `OrchestratorHandler.OnConfigReloaded` does not call `Pool.Reload`; vector and listener construction are boot-time operations (`internal/config/watcher.go:190-231`; `internal/handler/handlers.go:136-166`; `cmd/mcp-server-magictools/main.go:533-610`).
* existing tests do not drive the wizard: `TestConfigUIFunctions` is skipped, `TestEnsureInitialized` checks only forced file creation, and `TestSaveConfiguration` checks only the presence of two output values.

### Clarifications adopted by this plan

The MADR intentionally leaves several choices open. This plan resolves them as follows so implementation does not stall:

1. **YAML remains the public format.** Use `yaml.Node` patches against the latest file, preserve node styles/comments, remove the global `forceBlockStyle` rewrite from configuration saves, and skip the write entirely when no mutation is accepted.
2. **Writes are atomic and cross-process serialized.** Use a same-directory temporary file plus atomic replacement under an advisory lock. The running `update_config` tool and standalone CLI must use the same store.
3. **Unsupported generative providers are hidden, not partially implemented.** Fast and Thinking expose Gemini, Claude, and OpenAI. Ollama remains available for embeddings. Voyage remains embedding-only. Existing unsupported YAML values are preserved and diagnosed; the plan does not silently delete them.
4. **Environment credentials are explicit references.** Add `api_key_env`, `thinking_api_key_env`, and `embedding_api_key_env`. Existing literal key fields remain backward-compatible.
5. **Wizard-managed intelligence is restart-required for this implementation.** Fast, Thinking, Embedding, and Backplane changes are persisted but not claimed as live. Do not wire the current partial `Pool.Reload` until it can atomically reload providers, fallbacks, limits, listener state, and vector state with acknowledgement.
6. **`init` and `configure` become separate commands.** Destructive reset remains on `init --force`, requires confirmation, and requires `--yes` when stdin is non-interactive.
7. **`--config` means the MagicTools primary config.** `serve` may temporarily accept a legacy IDE JSON path with a deprecation warning; `configure` and `init` reject JSON targets and point users to `config.yaml`.
8. **No new automation interface is invented.** The misleading configure `--non-interactive` flag is deprecated and changed to return an actionable error. A future flag/subcommand design requires a separate decision.

### Required MADR state transition

After maintainers approve the eight clarifications above, change MADR 0001 from `proposed` to `accepted` before merging production changes. If any clarification is rejected, update the MADR decision outcome or consequences first, then revise this plan so the two artifacts do not conflict.

## Outcomes and Non-Goals

### Required outcomes

* Selecting any Thinking provider works and never relies on parsing presentation text.
* Fast, Thinking, Embedding, and Backplane changes persist independently.
* Clear/disable operations remove stale provider, model, credential, URL, fallback, and enablement keys deliberately.
* An accepted wizard action changes only its owned YAML paths; an exit/cancel makes no file write.
* Existing unknown YAML keys and comments survive an accepted change.
* Reset cannot overwrite existing files without an explicit confirmation and recoverable backup.
* Every provider displayed for a tier has a matching runtime capability contract.
* Environment-selected credentials stay environment references on disk.
* Validation or persistence failures propagate to Cobra and the process exits non-zero.
* CLI output distinguishes `saved` from `active` and states when restart is required.
* Path resolution is identical for initialization, loading, saving, companion registries, and watchers.

### Non-goals

* Adding generative Ollama or Voyage to `mcplib`.
* Replacing YAML, Viper, Cobra, or Pterm.
* Building a remote secret manager integration.
* Making Backplane listener ports or the vector engine hot-swappable.
* Designing a complete non-interactive configure API.
* Changing model IDs solely to track current provider marketing; provider-catalog maintenance is separate from fixing capability wiring.
* Migrating existing literal API keys automatically. Existing files must continue to load.

## Target Architecture

### Command boundary

```text
root
├── init       create missing files; confirmed reset when --force
└── configure  load desired YAML, collect one typed change, validate, persist, report activation
```

`configure` must not call template generation with `force=true`. It may create genuinely missing files through the same non-destructive initializer used by `init`, but it never resets an existing file.

### Configuration write path

```text
Wizard / update_config
        │
        ▼
typed ConfigurationPatch (unchanged | set | remove)
        │
        ▼
ConfigStore.Apply
  acquire <config>.lock
  read latest bytes
  parse YAML AST
  apply owned paths only
  decode + validate candidate
  marshal without global style conversion
  write/fsync same-directory temp
  atomically replace target
  release lock
        │
        ▼
ApplyResult {changed paths, old hash, new hash, restart required}
```

The store works on the latest on-disk file after acquiring the lock. It must never serialize a `Config` object containing runtime defaults or resolved environment secrets back to disk.

### Provider capability path

```text
internal/provider catalog
├── provider identity and display label
├── supported tiers
├── credential environment variable
├── local/remote and base-URL capability
└── per-tier curated model metadata
        │
        ├── configure menus and validation
        ├── llm constructor contract tests
        ├── vector constructor contract tests
        └── README/template support tables
```

Initial advertised support matrix:

| Provider | Fast | Thinking | Embedding | Credential source |
|---|---:|---:|---:|---|
| Gemini | Yes | Yes | Yes | `GEMINI_API_KEY` or literal |
| Claude | Yes | Yes | No | `CLAUDE_API_KEY` or literal |
| OpenAI | Yes | Yes | Yes | `OPENAI_API_KEY` or literal |
| Voyage | No | No | Yes | `VOYAGE_API_KEY` or literal |
| Ollama | No | No | Yes | Local URL; API key optional |

## Data Model and Public Configuration Changes

### Resolved paths

Add `internal/config/paths.go`:

```go
type Paths struct {
    Dir       string
    Config    string
    Servers   string
    Overrides string
}

func ResolvePaths(flagPath string) (Paths, error)
```

Resolution precedence is `--config` > `MCP_MAGIC_TOOLS_CONFIG` > `MCP_MAGIC_TOOLS_CONFIG_DIR`/OS default. For a YAML config override, `Servers` and `Overrides` are siblings of the resolved primary file. All returned paths are absolute and cleaned. A directory passed where a file is required is an error.

Add `Paths` to `config.Config` while retaining `ConfigPath` as a compatibility alias during the refactor. New code uses `cfg.Paths.Config`.

Legacy JSON rule:

* `configure` and `init` reject a `.json` primary target before any write.
* `serve` can load a legacy IDE JSON path for one compatibility release and logs a deprecation warning.
* companion files for a legacy JSON source remain under the resolved MagicTools config directory; do not place `servers.yaml` beside an IDE-owned JSON file.

### Explicit patch state

Add `internal/config/patch.go` with an explicit three-state field:

```go
type PatchState uint8

const (
    PatchUnchanged PatchState = iota
    PatchSet
    PatchRemove
)

type Field[T any] struct {
    State PatchState
    Value T
}
```

Provide constructors `Unchanged[T]()`, `Set(value)`, and `Remove[T]()`. Reject a `PatchSet` whose value cannot be represented by the target schema.

Define typed patches for the wizard-owned blocks rather than accepting arbitrary YAML paths:

```go
type ConfigurationPatch struct {
    Fast      FastTierPatch
    Thinking  ThinkingTierPatch
    Embedding EmbeddingPatch
    Backplane BackplanePatch
    Runtime   RuntimeConfigPatch
}
```

Each patch owns a documented path allowlist. Examples:

* Fast: `provider`, `model`, `api_key`, `api_key_env`, `api_url`, `fallback_models`, retry fields.
* Thinking: `thinking_provider`, `thinking_model`, `thinking_api_key`, `thinking_api_key_env`.
* Embedding: `embedding_provider`, `embedding_model`, `embedding_api_key`, `embedding_api_key_env`, `embedding_api_url`, `embedding_dimensionality`, `vector_enabled`.
* Backplane: `shared_llm_enabled`, port and limiter fields.
* Runtime: only keys already authorized by the `update_config` tool.

Patch builders enforce cross-field cleanup. For example, changing Thinking from Claude to OpenAI sets provider/model and a new credential source; clearing Thinking removes all three existing fields and its env-reference field in one patch.

### Credential references

Extend `IntelligenceEngine` in `internal/config/config.go`:

```yaml
intelligence:
  api_key_env: GEMINI_API_KEY
  thinking_api_key_env: CLAUDE_API_KEY
  embedding_api_key_env: VOYAGE_API_KEY
```

Rules:

* A tier may set either its literal `*_api_key` or `*_api_key_env`, never both.
* Env names must match `[A-Za-z_][A-Za-z0-9_]*`.
* Loading resolves the referenced variable into the runtime-effective key without changing the persisted representation.
* A missing referenced variable is a validation error for commands that need the provider. `show current` reports it as missing without printing its value.
* Selecting a literal removes the corresponding env field. Selecting env removes the literal field.
* Cross-tier reuse copies the source reference when possible, not the resolved secret bytes.
* Same-tier reuse is offered only when the old provider ID equals the newly selected provider ID.

Introduce a runtime-only credential source descriptor so display and validation do not infer source from the resolved secret:

```go
type CredentialSource struct {
    Kind CredentialKind // none, literal, environment, local
    Env  string
}
```

Never pass runtime-resolved `APIKey` fields to the configuration writer.

## Work Packages

### WP0: Baseline characterization and safety net

**Purpose:** Lock down current failures before structural edits.

**Files:**

* modify `cmd/mcp-server-magictools/config_extra_test.go`
* modify `internal/config/config_test.go`
* add `internal/config/testdata/config-preservation/*.yaml`
* add `cmd/mcp-server-magictools/config_wizard_test.go`

**Tasks:**

1. Add a focused parser regression showing that the current full Thinking label returns no provider. Mark it as the test fixed by WP4; do not rely on a live Pterm terminal.
2. Add a reproduction where a fresh config with no Fast provider receives a Thinking-only or Embedding-only in-memory update and `SaveConfiguration`; assert that the desired fields are absent to document CFG-02 before replacement.
3. Add a reproduction proving Thinking clear leaves existing YAML keys.
4. Add a golden reproduction showing default tri-factor weights change after load plus unrelated save.
5. Add a reproduction showing `ensureInitialized(..., true)` overwrites existing sentinel content without confirmation.
6. Record the current focused test and coverage commands in the test output or plan checklist.

**Exit gate:** Every P0 defect has a deterministic red/characterization test whose failure message names the corresponding MADR finding.

### WP1: Canonical paths and separate commands

**Purpose:** Ensure every component agrees on file ownership before introducing new writes.

**Files:**

* add `internal/config/paths.go`
* add `internal/config/paths_test.go`
* modify `internal/config/config.go`
* modify `internal/config/watcher.go`
* modify `cmd/mcp-server-magictools/root.go`
* split `cmd/mcp-server-magictools/config.go`
* add `cmd/mcp-server-magictools/init.go`
* add `cmd/mcp-server-magictools/init_test.go`

**Tasks:**

1. Implement `ResolvePaths` and table tests for flag, config-path env, config-dir env, default, relative path, alternate directory, conflicting inputs, directory input, and legacy JSON.
2. Thread `Paths` through `config.New`, managed-server load/save, override loading, and watcher construction.
3. Add path-taking primitives `LoadManagedServersAt`, `SaveManagedServersAt`, and override equivalents. Keep default-path wrappers temporarily for callers not yet migrated; mark them for removal.
4. Create a real `initCmd`. Remove `Aliases: []string{"init"}` from `configureCmd`.
5. Make initialization create only missing files by default.
6. Define `init --force` and `init --yes`; require an interactive confirmation showing exact paths, or `--yes` for non-interactive use.
7. Reject `configure --force`; reset belongs only to `init`.
8. Reject/deprecate `configure --non-interactive` with a non-zero actionable error instead of silently bypassing work.
9. Update root `--config` help to identify the MagicTools primary configuration and legacy serve-only JSON behavior.

**Tests:**

* existing files remain byte-identical under `init` without force;
* declined force leaves all files byte-identical;
* forced reset without `--yes` on non-terminal stdin fails before writing;
* alternate YAML paths load/save/watch matching companion files;
* configure rejects `.json` before initialization;
* `init --help` and `configure --help` show distinct flags and purposes.

**Exit gate:** There is one path-resolution truth table and no production configure/init call constructs companion paths independently.

### WP2: Locked, atomic, narrow configuration store

**Purpose:** Remove broad runtime snapshot persistence and data-loss windows.

**Files:**

* add `internal/config/store.go`
* add `internal/config/patch.go`
* add `internal/config/patch_yaml.go`
* add `internal/config/atomic_write.go`
* add `internal/config/filelock_unix.go`
* add `internal/config/filelock_windows.go`
* add corresponding unit tests and golden testdata
* modify `internal/config/config.go`
* modify `internal/handler/diagnostic_handlers.go`

**Tasks:**

1. Implement the typed patch state and owned-path builders.
2. Implement `ConfigStore.Apply(ctx, patch)` under an exclusive `<config>.lock` file using `golang.org/x/sys/unix.Flock` and `windows.LockFileEx`, following the repository's existing DB lock platform split.
3. Read the latest config only after acquiring the lock.
4. Parse exactly one YAML document whose root is a mapping. Treat unreadable, empty-existing, multi-document, or malformed files as errors; never fall back to a blank document during mutation.
5. Apply only patch-owned scalar or sequence nodes. `PatchRemove` deletes the key/value pair. Remove an empty generated sub-map only if the map contains no unknown/user keys.
6. Preserve existing node `Style`, comments, ordering, and unknown keys. Do not call `forceBlockStyle` from the primary config store. Replacing a changed sequence may replace comments inside that owned sequence, but must preserve comments and formatting outside it.
7. Decode the candidate through a new pure `DecodeConfiguration` function that has no file generation, IDE discovery, environment migration, or managed-server side effects.
8. Validate the decoded candidate before writing.
9. If the resulting bytes equal the input or the patch is all-unchanged, return `Changed=false` without touching mtime.
10. Write a temporary file in the same directory, set mode `0600`, write all bytes, `Sync`, close, atomically replace the target, and sync the parent directory where supported. Add platform-specific replacement behavior for Windows.
11. Ensure temporary and lock files are cleaned/released on every error path.
12. Convert `update_config` to build a `RuntimeConfigPatch` and apply it through the store. Only update live memory after the disk transaction succeeds, or roll back the in-memory mutation on failure.
13. Stop wizard and `update_config` callers from using `SaveConfiguration`. Deprecate or restrict that method until all callers are migrated, then delete its broad serializer.

**Tests:**

* set/remove every owned scalar and sequence;
* comments, unknown keys, quoting/flow styles, and key order outside changed paths survive golden comparisons;
* no-op apply preserves byte content and mtime;
* malformed YAML is unchanged and returns an error;
* a simulated write/rename/sync failure leaves the old target readable and unchanged;
* two concurrent store instances serialize and preserve both non-conflicting patches;
* conflicting patches are last-committed under the lock and return both hashes for audit;
* file mode is `0600` after create and replace;
* watcher observes an atomic replacement and reloads exactly once after hash gating;
* resolved environment secrets never appear in output bytes.

**Exit gate:** No configure or `update_config` path calls `os.WriteFile` on the primary config, and no caller serializes runtime-normalized `Config` back to YAML.

### WP3: Provider capability catalog and contract tests

**Purpose:** Make UI, validation, runtime, and documentation agree.

**Files:**

* add `internal/provider/catalog.go`
* add `internal/provider/catalog_test.go`
* add `internal/provider/contract_test.go`
* modify `cmd/mcp-server-magictools/config.go`
* modify `internal/llm/pool.go`
* modify `internal/vector/client.go` only as required for shared IDs
* modify `internal/config/templates.go`
* modify `README.md`

**Tasks:**

1. Define canonical provider and tier constants; eliminate duplicated string literals where feasible.
2. Define `ProviderSpec` with ID, label, tier capabilities, env variable, local flag, base-URL support, and curated per-tier model metadata.
3. Populate the initial support matrix documented above.
4. Generate wizard provider options from `Catalog.ForTier(tier)` using stable option IDs separate from labels.
5. Move embedding model/dimension tables out of the command package and into catalog metadata.
6. Keep live model discovery behind an injected interface. Pass supported base URL options when a provider accepts them.
7. Add contract tests that iterate every advertised Fast/Thinking provider and prove `llmprovider.NewProvider` constructs it; for Thinking, assert the result implements `llmprovider.ThinkingProvider`.
8. Iterate every advertised Embedding provider and prove `vector.NewEmbedderFromConfig` returns the expected provider.
9. Add negative contracts proving generative Ollama/Voyage are not displayed until runtime construction exists.
10. Update the template and README support lists from the catalog outcome. If docs generation is not introduced, add a test that compares documented marker blocks to catalog output.

**Exit gate:** Adding a menu option without runtime support fails a provider contract test.

### WP4: Injectable wizard and correct error propagation

**Purpose:** Remove brittle label parsing and make every interaction testable.

**Files:**

* add `cmd/mcp-server-magictools/prompter.go`
* add `cmd/mcp-server-magictools/prompter_pterm.go`
* add `cmd/mcp-server-magictools/prompter_test.go`
* refactor `cmd/mcp-server-magictools/config.go` into focused files if useful:
  `configure.go`, `configure_fast.go`, `configure_thinking.go`, `configure_embedding.go`, `configure_backplane.go`, `configure_show.go`

**Tasks:**

1. Define a `Prompter` interface with `Select`, `Text`, `Secret`, and `Confirm`; every `Select` option has a stable ID and separate display label.
2. Implement Pterm rendering behind `PtermPrompter`. Preserve secret masking behavior and explicit non-terminal warnings.
3. Inject `Prompter`, model discovery, environment lookup, config store, and output writer into a `ConfigureRunner`.
4. Pass `cmd.Context()` through discovery and validation rather than creating unbounded `context.Background()` values in handlers.
5. Change all option handlers to return `(ApplyResult, error)` or `error`. Treat cancel as a typed non-error outcome; treat prompt, validation, discovery, and persistence failures as errors.
6. Make the main menu switch on stable IDs, not the first character of labels.
7. Fix Thinking selection as a consequence of the stable-ID design; do not add another substring workaround.
8. Print success only after `ConfigStore.Apply` succeeds. Include changed fields, desired provider/model, and `restart required`.
9. Return errors to Cobra so failed operations exit non-zero. Never print `Configuration complete` after an error.
10. Remove the skipped `TestConfigUIFunctions`; replace it with deterministic fake-prompter tests.

**Tests:**

* every menu ID routes to the correct handler regardless of label text or localization;
* Thinking Gemini, Claude, and OpenAI selections reach credential/model steps;
* cancel at every prompt produces no store call;
* prompt and store errors reach `Execute`/Cobra;
* secret input is masked in terminal mode and warned in fallback mode;
* one PTY integration test exercises rendered selection and exit.

**Exit gate:** No configuration decision is derived from label substrings, and there are no skipped wizard tests.

### WP5: Provider-scoped credential sources

**Purpose:** Prevent wrong-provider key reuse and unintended env-secret persistence.

**Files:**

* modify `internal/config/config.go`
* add `internal/config/credentials.go`
* add `internal/config/credentials_test.go`
* modify wizard tier handlers
* modify show-current rendering
* modify template and README reference tables

**Tasks:**

1. Add the three `*_api_key_env` schema fields and mapstructure/YAML tags.
2. Implement pure credential validation and runtime resolution.
3. Preserve literal-key compatibility. Reject simultaneous literal and env source for a tier with a path-specific error.
4. Change credential selection to return `CredentialSelection{Kind, Literal, Env}`.
5. Offer current same-tier reuse only if provider IDs match.
6. For cross-tier reuse, copy a literal or env reference only when provider IDs match; show the source kind, never the secret.
7. For an environment choice, verify the variable is currently set, patch the env field, and remove the literal field.
8. For a literal choice, patch the literal field and remove the env field.
9. For Ollama embeddings, use a local credential source and retain an optional literal only if the endpoint explicitly needs one.
10. Update masking/display so output shows `env:GEMINI_API_KEY`, `literal:****1234`, `local`, `missing`, or `not configured`.

**Tests:**

* switching Gemini to OpenAI never offers the Gemini literal as an OpenAI key;
* env selection writes only the env variable name;
* missing env references fail validation without mutating disk;
* literal and env conflict fails with the exact YAML paths;
* cross-tier same-provider reuse preserves source kind;
* serialized output never contains injected sentinel environment secret bytes.

**Exit gate:** A repository-wide test search finds no path that persists a credential returned by `os.Getenv`/`os.LookupEnv`.

### WP6: Rebuild each wizard action as an explicit patch

**Purpose:** Deliver independent set, switch, clear, and disable behavior.

**Fast Tier tasks:**

1. Offer Gemini, Claude, OpenAI, and `Clear Fast Tier`.
2. Clearing Fast removes provider, model, credential fields, API URL, and fallbacks. If Backplane is enabled, require confirmation to disable it in the same patch or reject the clear.
3. On provider switch, remove an incompatible old URL and credential source.
4. Preserve retry values when valid; apply documented defaults only if the user accepts creating Fast Tier and fields are absent.
5. Make fallback selection explicit. Do not automatically save every unselected discovered model without confirmation; default to no fallbacks or a curated compatible ordered set.

**Thinking Tier tasks:**

1. Offer Gemini, Claude, OpenAI, and `None/Clear` from the capability catalog.
2. Use Thinking-specific model metadata; do not label a truncated general list as thinking-capable.
3. Clear provider, model, literal/env credentials, and any future Thinking URL together.
4. Allow Thinking persistence without Fast Tier, but explain that the Backplane endpoint is unavailable until Fast and Backplane are configured.

**Embedding tasks:**

1. Offer Gemini, Voyage, OpenAI, Ollama, and `Disable/Clear`.
2. Move model IDs and dimensions to structured metadata instead of parsing display text with `strings.Split`.
3. Store exact custom model text. Require an explicit positive custom dimension within a sane upper bound.
4. On provider switch, remove stale embedding URL/credential fields not supported by the new provider.
5. Clear removes provider/model/credentials/URL/dimension and sets `vector_enabled: false` explicitly.
6. Preserve the index rebuild warning whenever provider, model, URL, or dimensions change—not dimensions alone.

**Backplane tasks:**

1. Keep the Fast Tier prerequisite.
2. Validate port `1..65535`, concurrency `>0`, burst `>0`, timeout/TTL `>0`, token threshold `>0`, and RPM according to the chosen zero semantics.
3. Align `max_rpm: 0` behavior with the template. This plan chooses `0 = unlimited`; update `llm.NewPool` to use `rate.Inf` rather than replacing zero with 60.
4. Disable changes only `shared_llm_enabled`; retain tuning values for later re-enable unless the user chooses an explicit reset-to-defaults action.
5. Report listener/backplane changes as restart-required.

**Shared tests:**

* fresh config and every tier independently;
* same-provider model change;
* provider switch with stale-key cleanup;
* clear and reconfigure;
* custom model/dimension;
* Backplane prerequisite, enable, keep, disable, and invalid ranges;
* each action's golden diff contains only its owned paths.

**Exit gate:** The full set/switch/clear matrix passes after reloading from disk, not merely by inspecting the mutated in-memory object.

### WP7: Candidate validation and restart contract

**Purpose:** Ensure saved configuration can be interpreted and avoid false hot-reload claims.

**Files:**

* add `internal/config/validate.go`
* add `internal/config/validate_test.go`
* modify `internal/config/watcher.go`
* modify `internal/llm/pool.go`
* modify `cmd/mcp-server-magictools/config.go` or new show/activation files
* modify template and README hot-reload language

**Tasks:**

1. Implement pure structural validation for provider IDs, tier capabilities, required model/credential pairs, dimensions, ports, retry values, and Backplane limits.
2. Validate provider construction without issuing a generation request. Model-list/health checks remain an explicit user-visible step with bounded context and clear fallback language.
3. Distinguish discovery status: live list, curated fallback, custom entry. Never imply an API key was authenticated solely because a static list is available.
4. Remove `w.liveConfig.Intelligence = cfg.Intelligence` from hot reload for now. Log that desired intelligence differs and requires restart; keep the active runtime config stable.
5. Do not call the partial `Pool.Reload`. Either remove it and its dead-code implication or mark it internal experimental until a later MADR covers acknowledged full reload.
6. Update documentation to list actual hot-reload fields. Fast, Thinking, Embedding, Backplane enablement, listener port, and limiter construction require restart.
7. Extend show-current output with resolved config path, desired provider/model, credential source, URLs with secrets stripped, and an activation note. Do not claim to know running state unless service-state comparison is implemented.
8. Update `UpdateConfig` success text so only genuinely live-applied keys say `applied at runtime`.

**Tests:**

* invalid candidate never reaches atomic replace;
* static fallback is reported distinctly from live discovery;
* watcher changes search/log fields live but leaves active Intelligence unchanged;
* configure output always includes restart-required for wizard-managed fields;
* update_config messages match each key's actual activation behavior.

**Exit gate:** No code or documentation claims general config hot reload; each supported key has an explicit activation classification.

### WP8: Reset transaction, backups, and recovery

**Purpose:** Make the only destructive initialization operation explicit and recoverable.

**Files:**

* finalize `cmd/mcp-server-magictools/init.go`
* extend `internal/config/atomic_write.go` or add `reset.go`
* add reset integration tests

**Tasks:**

1. Before force reset, display all existing target paths and state that three files may be replaced.
2. Create timestamped owner-only backups in the same config directory, preserving original names and modes. Never overwrite a prior backup.
3. Stage and validate all three default files before replacing any target.
4. Hold a config-directory reset lock across backup and replacement.
5. Replace each file atomically. If a later replacement fails, restore previously replaced targets from their backups and return a non-zero error.
6. Print backup locations and recovery instructions after success.
7. Add `--yes` only as confirmation bypass; it must not weaken exact target resolution or validation.

**Tests:**

* decline/no leaves files byte-identical;
* non-interactive force without yes fails;
* successful reset creates three recoverable backups;
* injected failure on the second/third replace restores the original set;
* symlinks and directories are rejected as reset targets rather than followed destructively.

**Exit gate:** There is no unconfirmed code path that replaces an existing config, server registry, or override file.

### WP9: Documentation, migration notes, and dead-code cleanup

**Purpose:** Make public behavior match the implementation and remove misleading remnants.

**Files:**

* modify `README.md`
* modify `internal/config/templates.go`
* modify CLI help text and examples
* modify MADR status after acceptance
* remove `ConfigSyncFunc` if no caller is introduced
* remove/deprecate broad `SaveConfiguration`, default-path wrappers, and partial `Pool.Reload` as decided above

**Tasks:**

1. Document separate `init` and `configure` commands, reset confirmation, backup behavior, and `.json` rejection.
2. Document exact provider support by tier and remove generative Voyage/Ollama claims.
3. Add the new env-reference fields and precedence/conflict rules to the config reference.
4. Correct hot-reload claims and show restart examples for service and foreground modes.
5. Document `max_rpm: 0 = unlimited` after runtime alignment.
6. Add a migration note: literal keys continue working; environment choices now persist references; unsupported existing provider values are diagnosed but preserved.
7. Remove task-number comments and dead declarations that no longer describe the code.
8. Link the README architecture/configuration section to MADR 0001 and this plan.

**Exit gate:** README, template comments, CLI help, catalog, and runtime contract tests describe the same support matrix and activation behavior.

### WP10: Full verification and release gate

**Purpose:** Prove the change is safe across platforms and integration boundaries.

**Required commands:**

```bash
go test -count=1 ./cmd/mcp-server-magictools ./internal/config ./internal/provider ./internal/llm ./internal/vector ./internal/intelligence ./internal/handler
go test -race -count=1 ./internal/config ./cmd/mcp-server-magictools ./internal/handler
go vet ./cmd/mcp-server-magictools ./internal/config ./internal/provider ./internal/llm ./internal/vector ./internal/intelligence ./internal/handler
go test -count=1 ./...
git diff --check
```

**Cross-platform matrix:**

* Linux: atomic replace, flock, default XDG path, PTY test.
* macOS: atomic replace, flock, Application Support path, PTY test.
* Windows: `LockFileEx`, replace-existing semantics, `%APPDATA%` path, non-terminal secret fallback, reset rollback.

**Manual smoke tests:**

1. Copy a real, commented config to a temporary config directory.
2. Run configure and exit immediately; verify byte-identical files.
3. Configure each tier independently and inspect a focused diff.
4. Switch providers and verify stale key/URL removal.
5. Select env credentials and confirm secret bytes are absent from YAML.
6. Clear Thinking and disable Embedding; restart and verify runtime state.
7. Attempt `init --force`, decline, and verify no changes.
8. Accept reset, verify backups, and restore one manually.
9. Run the service during an intelligence edit and verify output/logs say restart required rather than hot-applied.

**Exit gate:** All automated commands pass, cross-platform CI is green, and every MADR Confirmation bullet maps to at least one named test.

## Test Inventory to Add

| Test area | Representative tests |
|---|---|
| Path resolution | `TestResolvePaths_Precedence`, `TestResolvePaths_AlternateYAML`, `TestConfigureRejectsLegacyJSON` |
| Initialization | `TestInitPreservesExisting`, `TestInitForceRequiresConfirmation`, `TestInitResetRollback` |
| Patch semantics | `TestPatch_SetRemove`, `TestPatch_UnknownKeysAndCommentsPreserved`, `TestPatch_NoOpDoesNotWrite` |
| Atomicity | `TestStore_ConcurrentNonConflictingWriters`, `TestStore_RenameFailurePreservesTarget`, platform lock tests |
| Thinking regression | `TestConfigureThinkingTier_ProviderIDs`, `TestConfigureThinkingTier_ClearRemovesAllFields` |
| Independent tiers | `TestConfigureThinkingWithoutFastPersists`, `TestConfigureEmbeddingWithoutFastPersists` |
| Provider contracts | `TestAdvertisedGenerativeProvidersConstruct`, `TestAdvertisedThinkingProvidersImplementThinking`, `TestAdvertisedEmbeddersConstruct` |
| Credential security | `TestEnvCredentialPersistsReferenceOnly`, `TestProviderSwitchDoesNotReuseWrongKey`, `TestResolvedSecretsNeverSerialized` |
| Embedding metadata | `TestEmbeddingSelectionDoesNotParseLabel`, `TestEmbeddingSwitchRemovesStaleURL`, `TestEmbeddingClearDisablesVector` |
| Backplane validation | `TestBackplaneRanges`, `TestBackplaneZeroRPMUnlimited`, `TestBackplaneRequiresFast` |
| Activation | `TestWatcherLeavesRestartRequiredIntelligenceActiveState`, `TestConfigureReportsRestartRequired` |
| CLI errors | `TestWizardPromptErrorExitsNonZero`, `TestWizardSaveErrorExitsNonZero`, PTY happy path |

## Sequencing and Change Isolation

Implement in the WP order. The critical dependency chain is:

```text
WP0 characterization
  └── WP1 paths/commands
       └── WP2 patch store
            ├── WP3 capability catalog
            ├── WP4 injectable wizard
            └── WP5 credential references
                 └── WP6 tier patches
                      └── WP7 validation/activation
                           ├── WP8 reset recovery
                           └── WP9 docs/cleanup
                                └── WP10 release gate
```

Recommended commit boundaries:

1. characterization tests only;
2. paths plus command split;
3. patch/store plus atomic writer;
4. migrate `update_config`;
5. provider catalog/contracts;
6. prompter and Thinking regression fix;
7. credential references;
8. Fast/Thinking patch flows;
9. Embedding/Backplane patch flows;
10. validation and restart contract;
11. reset transaction;
12. docs, cleanup, and final test adjustments.

Do not combine the atomic store and all wizard rewrites in one commit. Review must be able to verify persistence correctness independently from UI behavior.

## Compatibility and Migration Strategy

### Compatible behavior

* Existing YAML and literal API key fields load unchanged.
* Existing supported Gemini/Claude/OpenAI configurations continue to construct the same runtime providers.
* Existing embedding providers remain available.
* Default file locations remain platform-native.

### Intentional behavior changes

* `init` is no longer an alias that opens the configure wizard.
* `configure --force` is removed; destructive reset is `init --force` with confirmation.
* `configure --non-interactive` no longer silently does nothing.
* generative Ollama is no longer offered until runtime support is complete.
* Voyage is documented and offered only for embeddings.
* environment credential selection writes an env reference, not secret bytes.
* wizard changes explicitly require restart.
* a malformed existing file is an error and is never replaced with a fresh mapping.

### Legacy/unsupported values

Loading an existing unsupported provider value must not rewrite or remove it. `show current` displays `unsupported by this binary`, and editing another independent block preserves the value. Editing that same block requires choosing a supported provider or clearing it.

## Observability and Error Contract

Every successful apply should produce structured internal logging with:

* config path;
* changed YAML paths, excluding secret values;
* old/new content hashes;
* credential source kind, never credential content;
* restart-required boolean;
* provider/model IDs when relevant.

User-facing errors must identify the stage:

* `resolve config path`;
* `read existing config`;
* `parse existing YAML`;
* `validate candidate`;
* `acquire config lock`;
* `commit config atomically`;
* `discover provider models`.

Do not log complete patches when they can contain literal API keys.

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| YAML re-encoding still changes style | Mutate existing nodes in place, retain `Style`, remove `forceBlockStyle`, and use golden files containing flow collections, quotes, comments, and unknown keys. |
| Cross-platform atomic replace differs | Isolate platform helpers and run Windows CI; never fall back to truncate-in-place. |
| Lock files become stale | Use OS advisory locks tied to open handles; a leftover filename is harmless. |
| Env references break service environments | Validate the current process and document that systemd/launchd must receive the variable; show missing source clearly. |
| Provider catalog drifts from dependency | Constructor contract tests fail whenever advertised support and `mcplib` differ. |
| Restart-required behavior surprises users | Print it after every apply and correct template/README hot-reload claims in the same release. |
| Command split breaks scripts | Provide clear help/error messages and release notes; preserve `init` name while changing it to its documented purpose. |
| Concurrent reset/configure race | Use the directory/reset lock plus the same primary-config lock ordering everywhere. Document lock order to prevent deadlock. |
| Runtime state mutates before persistence | Persist first, then apply only supported live fields; roll back in-memory `update_config` changes on commit error. |

## Definition of Done

The implementation is complete only when:

* all P0 and P1 findings in MADR 0001 are closed by code and named tests;
* all P2 findings are either closed or explicitly moved to a tracked follow-up with rationale;
* no ordinary configure action writes template defaults over existing files;
* no accepted action changes semantic values outside its owned patch;
* clear/disable behavior survives reload from disk;
* unsupported providers cannot appear in wizard menus;
* environment secret sentinel tests prove no leakage to YAML or logs;
* reset is confirmed, backed up, atomic per file, and rollback-tested;
* the live/restart contract is accurate in code, CLI output, template comments, and README;
* focused, race, vet, full-suite, diff, and cross-platform checks pass;
* MADR 0001 is marked `accepted`, and its Confirmation section links or maps to the completed tests.
