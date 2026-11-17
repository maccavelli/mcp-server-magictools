---
status: proposed
date: 2026-07-31
decision-makers: MagicTools maintainers
consulted: Codebase audit requested by project maintainer
informed: MagicTools contributors and operators
---

# Make configure updates lossless and provider-capability-driven

## Context and Problem Statement

`mcp-server-magictools configure` is intended to update one independently selected area of `config.yaml` and save it immediately without disturbing other settings. Two user-visible failures prompted this review: reconfiguration appears to replace the file with template/default values, and every Thinking Tier provider is rejected as an invalid choice.

The Thinking Tier failure is confirmed. The overwrite symptom is also credible, but normal `configure` without `--force` does not directly copy the default template over an existing regular file. Instead, several distinct behaviors can reset, rewrite, or fail to clear configuration: `--force` unconditionally replaces all configuration files; every save reserializes and mutates a broad runtime snapshot; normalized defaults leak into unrelated saves; empty values are not written as removals; and concurrent/non-atomic writes can lose data. The current design therefore cannot guarantee the wizard's documented independent, lossless updates.

How should initialization, interactive configuration, persistence, provider discovery, validation, and live activation be organized so that each accepted wizard action has an explicit and testable effect?

## Decision Drivers

* Preserve every setting, comment, credential reference, and formatting choice outside the field set the user explicitly changes.
* Make clear and disable actions remove stale on-disk values reliably.
* Present only provider/tier combinations that the runtime can instantiate and use.
* Keep initialization/reset semantics separate from ordinary reconfiguration.
* Resolve one canonical configuration target consistently across flags, environment variables, loading, saving, and companion files.
* Report prompt, validation, and persistence failures through a non-zero command exit.
* Define whether each change is hot-applied or requires restart, then enforce and communicate that contract.
* Make the critical paths deterministic and testable without a real terminal or provider network.

## Considered Options

* Patch only the two reported symptoms.
* Introduce typed, transactional configuration patches and a shared provider-capability registry.
* Replace YAML and the wizard with a new configuration subsystem.

## Decision Outcome

Chosen option: "Introduce typed, transactional configuration patches and a shared provider-capability registry", because it fixes the confirmed failures and their common causes while retaining the public YAML format and interactive workflow.

The implementation should follow these boundaries:

1. Split `init` from `configure`. `init` creates missing files only; reset is an explicit, confirmed operation. `configure` never applies template defaults to existing files.
2. Resolve a single `ConfigPaths` value once, honoring `--config`, `MCP_MAGIC_TOOLS_CONFIG`, and the default directory consistently. Clarify that `--config` targets MagicTools YAML, not an IDE JSON file. Companion file paths must derive from the same resolved root.
3. Replace whole-snapshot `SaveConfiguration` use in the wizard with a typed patch API that distinguishes `unchanged`, `set`, and `remove`. Apply the patch to the latest on-disk YAML AST, validate the result, and commit it atomically with restrictive permissions. Serialize writes across processes or detect conflicts.
4. Centralize provider metadata and capabilities: canonical ID, display label, credential source, generative/embedding/thinking support, local/remote status, base URL support, model discovery, and runtime constructor. The wizard, documentation, and runtime must consume the same registry.
5. Treat credentials as provider-scoped values or references. Selecting an environment variable must not silently copy its secret into YAML. Reuse an existing key only when its provider identity matches.
6. Validate the complete candidate configuration before committing. Validate types and ranges locally; validate provider/model compatibility through the same constructor used at runtime; make any network health check explicit and bounded.
7. Return errors from option handlers to Cobra. Print success only after an atomic commit and successful activation decision. State "applied live" or "restart required" for each change.
8. Wire supported live changes to their consumers. If safe reinitialization is not available, document and surface a restart requirement instead of claiming universal hot reload.

### Consequences

* Good, because changing one tier no longer rewrites unrelated configuration or retains stale values.
* Good, because menu offerings, model discovery, and runtime support cannot drift independently.
* Good, because initialization and destructive reset behavior become explicit and auditable.
* Good, because failures become visible to users and automation through the process exit status.
* Good, because provider secrets can remain in their chosen environment source.
* Bad, because the patch model, atomic writer, and provider registry add implementation work and migration tests.
* Bad, because some currently advertised combinations, notably generative Ollama, must temporarily disappear until runtime support exists.
* Bad, because accurate restart signaling may expose that fewer settings are hot-reloadable than current documentation claims.

### Confirmation

The decision is implemented when all of the following are automated:

* A golden-file matrix proves that every wizard action changes only its intended YAML keys and preserves unknown keys and comments.
* Set, switch, clear, and disable tests cover Fast Tier, Thinking Tier, Embedding Engine, and Backplane independently, including a fresh config with no Fast Tier.
* Path-resolution tests cover flag, environment, default, relative, alternate-directory, and conflicting-input cases.
* Provider contract tests prove every displayed option has working discovery and runtime construction for its advertised capabilities.
* CLI tests drive the wizard through an injectable prompt interface and assert exit codes; at least one PTY integration test covers rendered selections.
* Reset tests prove that no overwrite occurs without explicit confirmation and that a declined reset leaves all files byte-identical.
* Atomicity/conflict tests cover interrupted writes and concurrent writers.
* Live-activation tests prove that supported changes reach the LLM pool/vector engine, while unsupported changes produce an explicit restart-required result.

## Pros and Cons of the Options

### Patch only the two reported symptoms

This would slice the Thinking Tier menu label before parsing and add a guard around the suspected overwrite path.

* Good, because the immediate code change is small.
* Bad, because independent Thinking/Embedding saves, clears, provider/runtime drift, path inconsistencies, and activation gaps remain broken.
* Bad, because broad snapshot serialization would continue to cause unrelated diffs and data-loss risk.
* Bad, because duplicated provider menus would make the same class of failure likely to recur.

### Introduce typed, transactional configuration patches and a shared provider-capability registry

* Good, because it addresses the root design problems while retaining YAML and Cobra/Pterm.
* Good, because explicit `set`/`remove` semantics eliminate the empty-value ambiguity.
* Good, because one capability source can drive menus, validation, runtime construction, and docs generation.
* Neutral, because existing configuration files remain valid but save behavior becomes intentionally narrower.
* Bad, because it requires changes across the CLI, config package, runtime activation, and tests.

### Replace YAML and the wizard with a new configuration subsystem

* Good, because a new schema and storage layer could enforce transactions and secret references from the start.
* Bad, because it creates unnecessary migration and compatibility risk.
* Bad, because the reported failures do not justify replacing the user-facing format.
* Bad, because it delays critical correctness fixes.

## More Information

### Assessment Scope and Method

The audit traced `runConfigure` and every wizard option through `config.New`, `LoadFromViper`, `SaveConfiguration`, provider discovery, `llm.NewPool`, the config watcher, and runtime consumers. Focused tests and vet passed for the reviewed packages:

```text
go test -count=1 -cover ./cmd/mcp-server-magictools ./internal/config ./internal/llm
cmd/mcp-server-magictools  16.1%
internal/config            56.2%
internal/llm               70.4%

go vet ./cmd/mcp-server-magictools ./internal/config ./internal/llm ./internal/vector ./internal/intelligence
```

Passing tests do not contradict the findings: the interactive UI test is skipped, the init test checks only forced creation, and the save test asserts only that two values appear. No test asserts independent persistence, removal, byte/semantic preservation, CLI exit behavior, path consistency, or provider-menu/runtime parity.

This record follows the current MADR 4 structure: YAML front matter followed by Context and Problem Statement, Decision Drivers, Considered Options, Decision Outcome, Consequences, Confirmation, Pros and Cons, and More Information. See the [MADR project](https://adr.github.io/madr/) and its [current template](https://adr.github.io/madr/decisions/adr-template.html).

### Findings Summary

| ID | Priority | Finding | User Impact |
|---|---:|---|---|
| CFG-01 | P0 | Thinking Tier passes a full display label to a numeric-only parser. | Every non-clear provider selection is rejected as invalid. |
| CFG-02 | P0 | Intelligence persistence is gated on Fast Tier provider presence. | Thinking or Embedding configured first reports success but is not saved. |
| CFG-03 | P0 | Empty optional values are skipped instead of removed. | Clear/switch operations retain stale thinking credentials, URLs, and fallback models. |
| CFG-04 | P0 | Generative Ollama is advertised but unsupported end-to-end. | Wizard can save a Fast/Thinking configuration that the runtime cannot construct. |
| CFG-05 | P0 | `--force` overwrites three files without the documented confirmation. | A reset can destroy configuration and server registry content immediately. |
| CFG-06 | P1 | Save is a broad, non-atomic read/modify/write of the whole YAML document. | Unrelated values/formatting change; concurrent writers or interruption can lose data. |
| CFG-07 | P1 | Runtime normalization is persisted during unrelated wizard saves. | User-authored search weights can change while configuring an LLM. |
| CFG-08 | P1 | Config target resolution is inconsistent and `--config` is misdescribed. | The wizard can initialize one path, load another, or rewrite an IDE `.json` as YAML. |
| CFG-09 | P1 | Provider credentials are not provider-scoped and environment values are copied to disk. | Switching providers can reuse the wrong key; selecting env storage leaks the secret into YAML. |
| CFG-10 | P1 | Saved intelligence changes are not fully activated live. | A running service can continue using old providers/models or an old vector engine despite success output. |
| CFG-11 | P1 | Option handlers swallow UI/save errors. | The command exits successfully after failed or aborted configuration operations. |
| CFG-12 | P1 | Thinking model selection is not thinking-capability-aware. | The top three general models may reject or fail to benefit from extended-thinking requests. |
| CFG-13 | P2 | `init` is only an alias of `configure`; `--non-interactive` merely skips the wizard. | CLI behavior and documentation diverge; there is no real non-interactive configuration interface. |
| CFG-14 | P2 | Backplane numeric inputs lack domain validation. | Invalid ports and non-positive or unreasonable limits can be accepted, omitted, or defaulted silently. |
| CFG-15 | P2 | Disable/clear and observability UX is incomplete. | Fast/Embedding cannot be cleared, and current config omits active path, URLs, source, and restart state. |
| CFG-16 | P2 | Provider definitions and documentation are duplicated and contradictory. | Voyage/Ollama support claims differ across template, README, wizard, and runtime. |
| CFG-17 | P2 | Dead or partial wiring remains. | `ConfigSyncFunc` and `Pool.Reload` exist but are never invoked, obscuring intended behavior. |

### Detailed Evidence

#### CFG-01: Thinking Tier choice parsing is definitively broken

The Thinking Tier select returns a rendered option such as `"1) Gemini    (Google Gemini API)"`, then calls `choiceToProvider(choice)`. That function accepts only the exact strings `"1"` through `"4"`. The Fast Tier correctly calls `choiceToProvider(choice[:1])`; the Thinking Tier does not. The clear branch works only because it separately checks `choice[:1] == "0"`.

Evidence: `cmd/mcp-server-magictools/config.go:194-221`, `588-622`.

#### CFG-02 and CFG-03: Persistence cannot represent independent tiers or removal

`SaveConfiguration` enters the entire `intelligence` update block only when `c.Intelligence.Provider != ""`. On a fresh config, configuring Thinking or Embedding before Fast mutates memory, calls save, and prints success, but none of those intelligence changes reach disk.

Inside that block, most optional fields are written only when non-empty. The Thinking Tier clear action sets three values to empty and calls save, but the existing YAML keys are never removed. Switching Thinking to Ollama likewise leaves the previous cloud `thinking_api_key`; switching away from a custom/local URL can leave stale URL keys; an empty fallback list leaves old fallbacks. The patch API must encode removal explicitly rather than infer it from a zero value.

Evidence: `cmd/mcp-server-magictools/config.go:207-215`, `263-267`, `432-441`; `internal/config/config.go:742-815`.

#### CFG-04: Generative Ollama is advertised but not constructible

Both Fast and Thinking menus offer Ollama and save it as a provider. The shared `llmprovider.NewProvider` used by the hydrator and LLM pool supports Gemini, Claude, and OpenAI only; Ollama returns `unsupported provider`. Thinking pool creation additionally requires a non-empty API key, which local Ollama configuration intentionally does not collect.

The entered Fast Ollama URL is not passed to model discovery or runtime construction. Thinking has no URL field or prompt. Therefore Ollama currently works only as an embedding provider through `internal/vector`, not as an advertised generative provider.

Evidence: `cmd/mcp-server-magictools/config.go:123-143`, `194-265`; `internal/llm/pool.go:105-115`, `472-474`; `internal/intelligence/hydrator.go:357-371`; `mcplib/llmprovider/provider.go:161-174` in dependency v0.2.0.

#### CFG-05 through CFG-07: The overwrite symptom has multiple concrete causes

`ensureInitialized` writes the full default templates whenever `force` is true. There is no confirmation prompt or backup even though the README promises confirmation. Because `init` and `configure` are the same Cobra command, this destructive flag is present on both behaviors.

Without `--force`, the template is not copied over an existing regular `config.yaml`. However, `SaveConfiguration` still parses and re-encodes the entire file, forces every flow-style collection to block style, replaces configured sequence nodes, and writes the result directly to the destination. It also writes many top-level runtime values unrelated to the selected wizard action. This is whole-document serialization, not a narrow persisted patch.

Runtime normalization makes that broader write materially destructive. The shipped/default tri-factor weights are `0.7`, `0.3`, `0.0`. `normalizeRRFBiases` treats a zero role weight as unset, changes it to `0.3`, then normalizes the `1.3` total. A later unrelated LLM save persists approximately `0.53846`, `0.23077`, `0.23077`. This is a confirmed mechanism by which reconfiguration changes non-LLM settings.

Finally, direct `os.WriteFile` truncation is not atomic, and the in-process mutex does not protect against the running server's `update_config` writer racing with a separate configure process. Both writers can read the same old AST and overwrite the other's update.

Evidence: `cmd/mcp-server-magictools/config.go:28-32`, `765-807`; `internal/config/config.go:684-840`, `1344-1369`, `1606-1619`; `README.md:179-182`.

#### CFG-08: Initialization and loading do not share one path resolver

`runConfigure` considers only `CfgPath` when choosing where to initialize. It then calls `config.New(Version, CfgPath)`, whose loader also considers `MCP_MAGIC_TOOLS_CONFIG`. With only the environment variable set, initialization can target the default directory while loading/saving the environment-selected path.

The persistent `--config` help says it is an IDE `mcp_config.json`, yet configure treats it as MagicTools `config.yaml`. JSON is valid enough YAML to parse, so a successful save can re-encode an IDE JSON file as YAML. Companion server/override loading elsewhere still uses `DefaultConfigDir`, even when configure initialized companions beside an alternate `--config` path.

Evidence: `cmd/mcp-server-magictools/config.go:43-64`; `cmd/mcp-server-magictools/root.go:58-61`; `internal/config/config.go:520-550`, `1102-1115`, `1323-1329`.

#### CFG-09: Credential source and provider identity are lost

`resolveAPIKey` receives a raw existing key but not the provider that owns it. When changing a tier from one provider to another, it can offer the old provider's key as the default "Keep existing" choice. Cross-tier reuse is provider-checked, but same-tier reuse is not.

Choosing an environment variable returns the secret string, after which the wizard assigns it to `api_key`, `thinking_api_key`, or `embedding_api_key` and saves it. The user's source choice is therefore not preserved; an environment-only secret is copied into plaintext YAML.

Evidence: `cmd/mcp-server-magictools/config.go:128-163`, `225-265`, `353-439`, `624-677`.

#### CFG-10 and CFG-17: Hot-reload plumbing stops before runtime consumers

The watcher reloads `Config.Intelligence` and calls `OrchestratorHandler.OnConfigReloaded`. That handler refreshes internal tools, log level, and search gates; it never invokes `Pool.Reload`. Although `llm.Pool.Reload` exists, no production caller uses it. Backplane listener enablement/port and the global vector engine are also established at boot and are not recreated by configure changes.

The current template says the config is hot-reloaded except for `logFormat`, while actual intelligence activation is partial or restart-only. Success output should not imply live effect until the relevant consumer confirms it.

Evidence: `internal/config/watcher.go:190-231`; `internal/handler/handlers.go:136-166`; `internal/llm/pool.go:352-388`; `cmd/mcp-server-magictools/main.go:533-610`.

#### CFG-11 through CFG-15: Error handling, validation, and UX gaps

Wizard option functions return no error. Prompt errors are discarded, and save failures are printed before returning to the menu; `runConfigure` can still return nil. Model-list APIs often return curated static lists after authentication/network failure, so "Fetching available models" is not credential validation. Thinking simply truncates the general model catalog to three entries despite the comment saying "thinking-capable".

Backplane integer parsing validates only that input is an integer. It does not enforce port range or positive/safe resource limits. There is no Fast Tier clear or Embedding disable action. `Show Current Config` omits the resolved file path, URL fields, credential source, whether values came from runtime defaults, and whether a restart is pending.

Evidence: `cmd/mcp-server-magictools/config.go:88-109`, `175-177`, `246-267`, `475-510`, `515-558`, `702-755`.

#### CFG-13 and CFG-16: CLI and support contracts drift from documentation

`init` is an alias, so an interactive `init` continues into the full wizard. `--non-interactive` performs no non-interactive configuration; it only initializes missing files and exits. The README describes init as template generation and promises a reset confirmation that does not exist.

The default template and README list Voyage as a Fast/Thinking provider, while the wizard does not offer it and the runtime constructor does not support it. They also list generative Ollama, which the wizard offers but the runtime cannot instantiate. Provider capability must have one source of truth.

Evidence: `cmd/mcp-server-magictools/config.go:28-32`, `66-70`, `765-767`; `internal/config/templates.go:214-241`; `README.md:170-193`, `488-494`, `732-740`.

### Recommended Delivery Order

1. **Contain data loss:** require reset confirmation, add backup/atomic write, stop broad snapshot persistence in configure, and add golden preservation/removal tests.
2. **Restore advertised basics:** fix Thinking selection parsing; persist Thinking/Embedding independently; make clear actions remove keys; propagate errors.
3. **Align provider capabilities:** hide unsupported generative Ollama/Voyage immediately or implement them end-to-end; add provider-scoped credential references and capability contract tests.
4. **Unify paths and commands:** create distinct `init` and `configure` commands and one resolver for config plus companion files.
5. **Close runtime wiring:** connect safe provider reloads, define restart-required changes, and report activation state.
6. **Harden UX:** validate ranges and candidate configs, add disable flows, improve current-config output, and provide a real flag-based non-interactive mode if automation is a requirement.

### Acceptance Invariants

* Running `configure` and exiting without accepting a change leaves every configuration file byte-identical.
* Accepting one option changes only the documented keys for that option.
* An option is never printed unless the current binary can construct and use that provider for that tier.
* "Clear" means the values are absent or explicitly empty after reload, with no stale credential or URL.
* A reported success means the candidate is persisted durably and its activation status is known.
* Resetting existing files requires an explicit destructive confirmation and creates a recoverable backup.
* Environment-selected secrets remain environment references unless the user explicitly chooses to persist them.
