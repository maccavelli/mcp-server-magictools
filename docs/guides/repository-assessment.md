# Repository assessment

## Audit scope and baseline

This assessment was performed on 2026-08-29 against commit `bcd1ac5` on
`main`. It covered the CLI, embedded templates, path resolution, process and
service implementations, MCP inventory, transports, persistence, search,
dashboard, installers, CI/release workflow, tests, and tracked repository
artifacts.

The repository contained 398 tracked files: 373 Go files and 163 Go test files.
The following verification passed before documentation edits:

```text
CGO_ENABLED=0 go test -count=1 ./...  PASS
go vet ./...                           PASS
make lint                              PASS (0 issues)
```

A locally built CLI was also exercised for version and help output. No live
tool calls or destructive database commands were used. One default-command
probe entered `serve`, invoked the repository's stale-PID cleanup against the
recorded process, and the configured launchd service restarted it immediately;
this directly confirmed the operational warning about ad hoc serve probes and
shared state.

## Overall state

The implementation is substantial and well tested. It has clear subsystem
boundaries for config, process management, MCP handlers, persistence, search,
vector indexing, telemetry, and platform service integration. CI exercises all
published operating-system targets natively and releases only after quality and
tag-binary smoke gates.

The previous 858-line README was the weakest interface: it mixed architecture,
installation, tuning, security, and troubleshooting while making stale or
incorrect claims. This documentation library replaces it with a navigable
landing page and code-grounded guides.

## Architecture findings

- `cmd/mcp-server-magictools/main.go` composes the orchestrator and its stdio or
  service transports.
- `internal/client` owns downstream lifecycle, registry synchronization,
  health, proxy safety, and process management.
- `internal/db` uses Badger as source storage and Bleve as a derived lexical
  index. `internal/vector` adds optional HNSW persistence.
- `internal/handler` exposes 18 native tools, a `pipeline-start` prompt, and a
  raw-output resource template.
- `internal/config` embeds and hot-reloads three YAML documents.
- Service mode provides OS-native IPC, authenticated loopback fallbacks, a
  separate IDE Streamable HTTP listener, and an optional scoped LLM backplane.

The single-owner datastore and managed-child model make one orchestrator per
data directory a fundamental invariant. Service-plus-proxy is the coherent
multi-client architecture.

## CLI findings

- Root command dispatch defaults to `serve`. This is convenient for MCP
  clients but surprising and potentially disruptive as a shell smoke test.
- `configure --force` and `configure --non-interactive` remain visible in help,
  but `runConfigure` rejects both. `init` owns those workflows.
- Root help says the default log is under the config directory as
  `magictools.log`; `config.DefaultLogPath` resolves
  `magictools_debug.log` under the cache directory.
- `dash --find` does not run its advertised historical search.
- `db wipe` has no confirmation or `--yes` gate.
- `service doctor` prints findings but does not fail solely because issues were
  detected, limiting its usefulness in automation.
- `showvars` is incomplete, and its `MAGIC_TOOLS_INTELLIGENCE_MODEL` example
  omits the top-level `configuration` namespace used by Viper.

## Configuration findings

- `ResolvePaths` has clear precedence and keeps YAML companions colocated.
  Legacy JSON support is intentionally serve-only, but companion files fall
  back to the default directory, which should be understood during migration.
- `init` safely preserves existing files by default and atomically creates
  owner-readable templates. Forced reset has a sound confirmation gate.
- The wizard stages a typed patch and writes only on **Save and Exit**, matching
  the lossless-update MADR and plan in `docs/`.
- File watches have a 30-second polling fallback and reconcile all three YAML
  files.
- The twelve generated server entries are disabled examples with placeholder
  commands, not an operational default fleet.
- `wake_servers` skips `Disabled` entries, and the watcher handles promotion and
  demotion. However, the initial `executeBootSequence` clones every configured
  server and partitions only on `DeferredBoot`; it does not filter `Disabled`.
  That contradicts the template's “ignored and will not boot” contract.
- `deferred_boot` is documented in the template as lazy first invocation, but
  code starts deferred servers in the background immediately after critical
  boot. It is deferred, not on-demand.
- `normalizeRRFBiases` treats non-positive weights as unset. Consequently the
  generated `.7/.3/.0` vector/synergy/role values do not remain those effective
  weights. Related default comments also disagree with runtime normalization.
- API keys can be persisted as plaintext YAML. No configuration encryption
  facility is present.

## MCP and orchestration findings

- Static inventory and registration agree on 18 native tools.
- Downstream tool exposure is dynamic and namespaced. Proxy calls validate
  schemas by default.
- `trustServers` is correctly separated from `pinnedServers`, making inline
  execution an explicit authorization setting.
- Pipeline tools are always advertised but correctly gate calls on Recall,
  Brainstorm, and Go Modernizer health.
- The native pipeline is Go-focused and includes mutation/build validation.
  The old README incorrectly presented the Go toolchain as a universal
  MagicTools prerequisite; it is a source-build or downstream workflow
  requirement.
- Large proxy outputs can be retained behind
  `mcp://magictools/raw/{id}`, preserving access after context-saving
  transformation.

## Transport and security findings

- Unix-domain sockets and user-scoped Windows named pipes provide a strong
  local primary IPC boundary. TCP fallbacks require owner-file bearer tokens
  and include application rate limiting.
- LLM generation endpoints use a distinct scoped token; `/llm/status` is
  intentionally unauthenticated.
- The IDE `/mcp` listener and `/health` have no authentication. Bind resolution
  rejects non-loopback addresses unless explicitly overridden, but
  `MCP_ENDPOINT_ALLOW_NONLOOPBACK=true` creates a high-risk unauthenticated
  network service.
- Auth files are atomic and owner-readable. YAML secrets and Badger data are
  plaintext; no at-rest database encryption is configured.
- Service shutdown ordering and 30/35-second drain budgets are consistent
  across Linux, macOS, and Windows.
- Windows now uses a real SCM service, but cross-platform preflight still checks
  for `schtasks` and reports Task Scheduler as required. That is obsolete and
  can reject an otherwise valid SCM installation.
- Startup stale-PID cleanup can terminate the PID recorded in the datastore.
  This reinforces the rule that version probes should use `--version`, not a
  bare invocation, and that a live service data path should not be shared with
  ad hoc instances.

## Storage and operations findings

- Badger's exclusive lock protects datastore consistency and makes concurrent
  direct stdio sessions fail fast.
- Bleve has an offline rebuild path through `db sync`.
- There is no first-class datastore backup, export, or restore command.
- `db wipe` is immediately destructive and should gain confirmation and an
  explicit automation override.
- Telemetry is stored in a fixed 128 MiB mmap ring under the cache directory.
- `service.state` and auth metadata provide useful status/proxy coordination but
  must be treated as runtime state, not portable backup content.

## Dashboard findings

The dashboard is broad and code-backed: ten views cover fleet, transports,
orchestration, tool intelligence/analytics, system/storage/LLM backplanes,
tracing, and logs. It reads every ten seconds and avoids contending for the
primary datastore. The major gap is the advertised but unimplemented
`dash --find` path.

## Release and platform findings

- `.github/workflows/ci.yml` runs on every branch push, tag, pull request, and
  manual dispatch.
- Linux runs gofmt drift checks, module-tidiness checks, vet, cgo-free tests,
  golangci-lint, and POSIX installer tests.
- macOS arm64 and Windows amd64 build, vet, and run all tests natively. Windows
  also dry-runs the installer and asserts it performs binary placement only.
- Tags must match `vX.Y.Z`. Release binaries are stamped from the tag, checked
  for cgo-free linkage, checksummed, executed on native target runners, and
  attached only after every dependency succeeds.
- Published architecture coverage is intentionally narrow: Linux amd64, macOS
  arm64, and Windows amd64.
- Source fallback `RawVersion` is `v4.3.2`, while the audited published line is
  `v1.0.3`. Tag builds override it correctly, but unstamped local binaries
  report a misleading version.

## Repository hygiene findings

The Go tree is unusually test-dense and passes its automated quality gates.
Remaining tracked artifacts should be reviewed:

- `internal/handler/cov.txt` is generated coverage output.
- `internal/handler/proxy_service_extra_test.go.orig` is a backup file.
- `fix_test.sh`, `run_test.sh`, and `leak_finder.sh` are ad hoc root scripts with
  unclear supported-workflow status.

The repository also contains a large amount of defensive-comment annotation in
production code. It communicates history, but sometimes preserves obsolete
claims—as seen in Task Scheduler and deferred-boot comments. Prefer executable
tests and concise current contracts when touching those areas.

## Recommended remediation order

1. Filter disabled servers from initial boot and add a regression test.
2. Correct the `deferred_boot` contract or implement true first-use activation.
3. Add confirmation to `db wipe` with an explicit non-interactive override.
4. Remove rejected `configure` flags and correct root log/help and `showvars`.
5. Protect or remove the non-loopback IDE bind override, or add mandatory auth
   and TLS for remote exposure.
6. Align synthesis-weight defaults, normalization, comments, and tests.
7. Remove the stale Windows `schtasks` preflight.
8. Stamp a coherent source fallback version.
9. Implement `dash --find` or remove the flag.
10. Add supported cold backup/restore commands and clean generated artifacts.

## Documentation corrections made

This documentation rewrite:

- replaces the monolithic README with a task-oriented entry point;
- distinguishes binary installation, initialization, configuration, and
  service installation;
- documents only the three published and natively tested targets;
- corrects Windows service management from Task Scheduler to SCM;
- replaces stale health, timeout, config path, and log path claims;
- treats Go as workflow-specific rather than universally required;
- documents actual MCP inventory, pipeline gates, and transport authentication;
- makes destructive, security-sensitive, and incomplete behaviors explicit.
