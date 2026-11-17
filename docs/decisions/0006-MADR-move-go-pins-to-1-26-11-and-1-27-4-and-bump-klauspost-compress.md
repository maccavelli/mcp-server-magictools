---
status: accepted
date: 2026-11-17
decision-makers: maccavelli
consulted: "govulncheck v1.9.0 (vulnerability database updated 2026-11-11 02:51 UTC) under go1.27.4 and go1.26.11, 2026-11-17; actions/setup-go v7.0.0's README at the pinned commit; a scratch copy of HEAD"
informed: the owner, who builds and installs this server from source
---

# Pin the floor to `go 1.26.11` and the toolchain to `go1.27.4`, and bump `klauspost/compress` for GO-2026-6865

## Context and Problem Statement

The owner's instruction, relayed by the coordinator, verbatim:

> "go-llmprovider-sdk, go-tui-lib, acp-go-sdk2, gobble-cli, magic-cli-remote, go-selfupdate-lib, mcp-server-duckduckgo, mcp-server-magictools, mcp-server-recall, mcp-server-sequential-thinking, mcplib, mcp-buntdb, and mcp-server-socratic-thinker all need to have their go floor pinned to 1.26.11, and go version/toolchain pinned to 1.27.4."

"Floor" is the `go` directive in `go.mod`; "version/toolchain" is the
`toolchain` line. A survey then found that this repository builds at 1.26.11,
and the owner ruled, labels quoted exactly and only as labels:

- "One short record pair per repository, all approved now through CI
  (Recommended)": this pair is drafted and executed for this repository.
- "Yes: bump the dependency in the same record as the Go move (Recommended)":
  GO-2026-6865 in `github.com/klauspost/compress` is fixed in this record,
  after being established on a scratch copy, and stops if it moves more than
  that one module.

Everything in this record other than the quoted words and labels is
evidence or the author's text. Why now: `govulncheck` v1.9.0 (database of
2026-11-11) reports GO-2026-6859 in `net/http` under Go 1.27.3 and 1.26.10 and
not under 1.27.4 and 1.26.11 (the coordinator's measurement of 2026-11-17).

**Evidence index.** Read or observed on 2026-11-17 at HEAD `310a541`, with
one unstaged change that is not this record's (E8).

- **E1, the pins [read].** `go.mod:3` is `go 1.26.6`; there is no `toolchain`
  line. `Makefile:1` is `MOD_VERSION := 1.26.6`; the variable is defined and
  used nowhere (`grep -rn MOD_VERSION` finds only its definition), so it is
  documentation that must still be true. `README.md:145` says "Go 1.26.5 is
  required to build this repository" and
  `docs/guides/platform-installation.md:123` says "currently Go 1.26.5": both
  were already stale against `go.mod`.
- **E2, what stays [read].** `internal/client/cloexec_unix.go:13` says "Go
  1.26.5 idiom compliance": the history of an idiom, not a requirement, so it
  stays. `docs/0002-MADR-...md:248` names the Go 1.26.5 `os.Rename`
  documentation: history, stays. `internal/handler/execute_pipeline.go:903-904`
  is not a pin (E7). `internal/forks/hnsw/go.mod` is out of scope (E8).
- **E3, CI [read, and the action's documentation].** `ci.yml:54`, `:189` and
  `:266` use `actions/setup-go` (pinned by commit, v7.0.0) with
  `go-version-file: go.mod`. Its README at that commit says: "If the
  `toolchain` directive is present, its version is used; otherwise, the action
  falls back to the `go` directive." So after this change every CI job builds
  with Go 1.27.4, and before it, with 1.26.6. **CI has no job that builds and
  tests on the floor** (1.26.11): the floor is declared and checked locally
  only. This is a named gap (Consequences). The release job
  (`ci.yml:340`) calls mcplib's reusable workflow, pinned by commit; what that
  workflow builds with comes from mcplib's own `go.mod`, which is mcplib's
  record, not this one.
- **E4, the baseline [observed on a scratch copy of HEAD].**
  `govulncheck ./...` with `GOPROXY=off` exits 3 under both `go1.27.4` and
  `go1.26.11`: "Vulnerability #1: GO-2026-6865, Panic on two-block frames in
  github.com/klauspost/compress", "Found in:
  github.com/klauspost/compress@v1.18.6", "Fixed in:
  github.com/klauspost/compress@v1.20.2", trace
  `internal/util/compress.go:41` (`util.Compress`) to `zstd.Encoder.EncodeAll`.
  The fixed version comes from the advisory data `govulncheck` printed (the
  Go vulnerability database, read by `govulncheck`; nothing about this machine
  or its owner was sent by this author). Database updated 2026-11-11 02:51:11
  UTC.
- **E5, who requires it [observed].** `go mod why -m
  github.com/klauspost/compress` says the root module imports
  `github.com/klauspost/compress/zstd` from `internal/util`; `go.mod:16` is a
  direct requirement. `go mod graph` shows a second requirer,
  `github.com/dgraph-io/badger/v4@v4.9.1`, which asks for `v1.18.0` (lower, so
  the root's version wins).
- **E6, the bump, on a scratch copy [observed].** `go get
  github.com/klauspost/compress@v1.20.2` then `go mod tidy`, under go1.27.4.
  **The proxy was used for this one download, on the scratch copy only** (the
  module was not in the cache); the tree's own checks run with `GOPROXY=off`.
  The result moved exactly: `go.mod` one line (`v1.18.6` to `v1.20.2`) and
  `go.sum` the two lines of that module (`h1:` and `/go.mod h1:`, `v1.18.6` to
  `v1.20.2`). No other requirement's version changed, no module was added, and
  the `go` line did not move (the new version's own `go.mod` says `go 1.25`,
  below the floor). The owner's stop rule was therefore not met, and the bump
  proceeds.
- **E7, the Go-binary search paths [read].** `execute_pipeline.go:903-904`
  list `~/sdk/go1.26.5/bin/go` and `~/sdk/go1.26.1/bin/go` as fallbacks in
  `mcpGoBinary()`, after `MCP_GO_BIN_PATH` and before a `~/sdk/go*/bin/go`
  glob whose matches are placed first. With the glob first, the two named
  paths are reached only when the glob finds no executable, and the host SDK
  directory holds only `go1.27.4` on this Mac (observed) and, per
  go-selfupdate-lib's 0045-MADR E3 (read, not re-observed here), on the other
  gate hosts. They are stale names that never match; they are not a pin and
  not this record's to change (Questions).
- **E8, the vendored fork [read, and observed on a scratch copy].**
  `internal/forks/hnsw/go.mod` (module `github.com/coder/hnsw`, used through
  `go.mod:131` `replace github.com/coder/hnsw v0.6.1 => ./internal/forks/hnsw`)
  says `go 1.21.4` at HEAD. The owner has an uncommitted edit in the working
  tree changing it to `go 1.26.6`; that edit was the owner's and this record's
  first draft did not touch it (the owner's ruling D5 later directed this
  record to set the line to 1.26.11). In a scratch copy at HEAD, with the
  fork's line set to `go 1.26.11` and the root left at `go 1.26.6`,
  `GOTOOLCHAIN=local GOFLAGS=-mod=readonly go build` fails ("updates to go.mod
  needed, disabled by -mod=readonly") and `go mod tidy -diff` shows the root's
  `go` line being raised to `go 1.26.11`: **a `replace`d module's `go` line
  must not exceed the root's, and the toolchain enforces that by raising the
  root.** So the fork's line is part of the floor only as an upper bound: at
  1.21.4 it constrains nothing, and at or below the root's 1.26.11 it is
  harmless. The fork has no function literals, no goroutines, no `defer
  func` and no `t.Run` (grep over its Go files, including `heap/`); its loops
  take `&` only of variables declared inside the loop body, not of the loop
  variable. Raising it past 1.21 would therefore change no behaviour there
  today (Go 1.22's per-iteration loop variables matter only for capture), but
  it is a decision about a vendored third-party module, not a pin clean-up.
  Ruled 2026-11-17 (D5): `go 1.26.11`. Measured then, in the fork directory:
  `go mod tidy -diff` exits 0 with nothing to change (the fork's requirements
  do not move; testify v1.8.0, x/exp, x/sys v0.11.0, check.v1 and kr/pretty
  were fetched through the proxy into the module cache because they were not
  cached, and nothing in the tree changed by it); `go build ./...`, `go vet
  ./...` and `go test -count=1 -timeout 20m ./...` exit 0 under `go1.27.4`
  and `go1.26.11`, packages `github.com/coder/hnsw` and `.../heap` `ok`.
- **E9, consumers [read].** No sibling repository's `go.mod` requires this
  module (`grep` over `~/gitrepos/go/*/go.mod`, itself excluded). It is a
  server binary; the latest tag is `v1.1.0` (four commits before HEAD).
- **E10, the CI linter cannot read Go 1.27.4 [observed on this tree and on a
  scratch copy of HEAD, 2026-11-17; added on the owner's ruling D3].**
  `ci.yml:89` installs `golangci-lint@v2.13.1` with `go install`, and runs
  `make lint`. Sibling repositories showed the same day that this version
  cannot type-check code under Go 1.27.4 (their CI failed with `could not
  import os`, `typecheck`, exit 2; another reproduced "export data version 5
  is greater than maximum supported version 4"). Reproduced here: v2.13.1 and
  v2.15.0 were each built with `GOBIN=<scratch>/bin go install ...@<version>`
  under `GOTOOLCHAIN=go1.27.4` (both were in the module cache; no download),
  as CI builds them after this change. `version` lines: "golangci-lint has
  version 2.13.1 built with go1.27.4" and "golangci-lint has version 2.15.0
  built with go1.27.4". `make lint` with v2.13.1 on a scratch copy of HEAD:
  under `GOTOOLCHAIN=go1.26.11` exit 0, "0 issues."; under `go1.27.4` exit 2,
  `typecheck: 1`, "could not import testing" at `internal/ui/init_test.go:3:8`.
  So with the toolchain line at 1.27.4, CI's lint step fails whatever the code
  says. With v2.15.0, `make lint` on the changed tree exits 2 under both
  `go1.27.4` and `go1.26.11` with the same two findings and no type-check
  error (the table in the findings section). Both findings are **new with
  v2.15.0**: v2.13.1 reports neither today, so CI does not see the
  `pipeline_tools.go:161` `sprintfQuotedString` finding now. (Item 6 of the
  first handoff called it pre-existing against HEAD: that run used a newer
  local binary, and the claim was about this repository's code, not about what
  CI's pinned linter reports; this entry corrects it.)

The question: how should this repository's floor, toolchain and the
`klauspost/compress` requirement follow the owner's rulings?

## Decision Drivers

- The owner's instruction and rulings above.
- Ship nothing on a toolchain or dependency `govulncheck` reports (E4).
- No gate loosened; `govulncheck` exit 0 under both toolchains is the proof.
- One statement of the version: every current statement agrees with `go.mod`.
- The change moves only what the rulings name: `go.mod` and `go.sum` change in
  exactly the lines E6 lists.
- No compatibility engineering: the owner is the only user of the fleet.

## Considered Options

- **A. Floor `go 1.26.11`, toolchain `go1.27.4`, and `klauspost/compress`
  `v1.20.2`, in one change.**
- **B. A different floor: keep `go 1.26.6`, add only the toolchain line, and
  bump the dependency.**
- **C. Do nothing.**

## Decision Outcome

Chosen option: **A**, the owner's instruction and rulings.

1. `go.mod`: `go 1.26.6` becomes `go 1.26.11`; a `toolchain go1.27.4` line is
   added after it; `github.com/klauspost/compress v1.18.6` becomes `v1.20.2`.
   `go.sum`: the two `klauspost/compress` lines move with it. Nothing else.
2. `Makefile:1` `MOD_VERSION := 1.26.11`. `README.md:145` and
   `docs/guides/platform-installation.md:123` say the floor and the
   toolchain truthfully. There is no changelog in this repository.
3. CI's `go-version-file: go.mod` steps follow the `toolchain` line (E3) and
   build with 1.27.4 after the change. The one workflow line that changes is
   the lint install, `ci.yml:89`, `golangci-lint@v2.13.1` to `@v2.15.0`
   (E10, D3); nothing else in the workflow is edited. The Makefile installs
   no pinned version (its hint says `@latest`), and no README or guide states
   one.
4. `internal/forks/hnsw/go.mod` line 3 becomes `go 1.26.11`, on the owner's
   ruling D5 (it replaces the owner's uncommitted `go 1.26.6`, which D5
   directs). It equals the root's floor, the most a `replace`d module may
   say (E8). Its requirements do not change.
5. `internal/handler/pipeline_tools.go:161` takes the verb `%#q`, with a test
   (D4, F1). `ListToolsMiddleware` (F2) is not touched here: its redesign is
   its own record and lands before this change is pushed (D6).
6. `execute_pipeline.go:903-904` is not changed here (E7): the 0005 plan is
   amended for it (D7).

### Consequences

- Good, because every CI build and `go install`/`make install` build uses a
  standard library and a compress library `govulncheck` reports clean (E4,
  Confirmation).
- Good, because the three statements of the version (`go.mod`, README, the
  platform guide) and the Makefile variable agree.
- Bad, because **CI has no floor job** (E3): nothing in CI builds and tests on
  1.26.11, so the floor is checked only by a local run with
  `GOTOOLCHAIN=go1.26.11`. Whether to add floor jobs fleet-wide is the
  owner's call and is not made here.
- Neutral, because the owner's own installed binary benefits when the owner
  builds it from this tree with Go 1.27.4 (the host default); no release is
  needed for that. A release is needed only for the release assets that CI
  builds on a tag to carry the fix (E9: latest `v1.1.0` predates this). The
  plan stops before any tag; the tag and the release need the owner's word.
- Neutral, because a host's `GOTOOLCHAIN` already names 1.27.4, so the
  `toolchain` line changes nothing on a host; only CI's `setup-go` follows it.
- Bad, because **this change cannot be pushed until F2's redesign has
  landed**: CI's lint under v2.15.0 is red on F2 until then (D6). The
  redesign's record lands first, then this change.
- Neutral, because the stale search paths (E7) are the 0005 plan's (D7).

### Confirmation

- **Before:** E4's exit 3 under both toolchains.
- **After, on the changed tree:** `govulncheck ./...` exits 0 under
  `go1.27.4` and `go1.26.11`, both with `GOPROXY=off`; `go mod tidy -diff`
  exits 0; `go build ./...`, `go vet ./...` and `go test -count=1 ./...` pass
  at 1.27.4 and at 1.26.11 (`GOPROXY=off`); the cross-builds for linux and
  windows (`CGO_ENABLED=0`) and darwin build.
- **Agreement:** the PLAN's version-agreement check passes on the tree and
  fails on three planted copies, and its lint-pin part passes on the tree and
  fails on a planted `v2.13.1`.
- **Lint:** `make lint` with the v2.15.0 binary runs without a type-check
  error under both toolchains. It exits 2 on two findings that the owner
  decides (below); this record does not fix, suppress or exclude them.

## Pros and Cons of the Options

### A. Floor 1.26.11, toolchain 1.27.4, compress 1.20.2 (chosen)

- Good, because it is the owner's instruction and ruling, and the bump is
  one module (E6).
- Bad, because it raises the floor by five patch releases for anyone building
  from source (no one but the owner does) and leaves the CI floor gap open.

### B. Keep the floor at `go 1.26.6`

- Good, because it moves fewer lines.
- Bad, because it is not what the owner instructed, and 1.26.6 is below the
  fixed standard library (GO-2026-6859 is fixed in 1.26.11).

### C. Do nothing

- Good, because nothing changes.
- Bad, because `govulncheck` stays at exit 3 (E4), and the pins stay wrong.

## More Information

### Decided

The owner's ruling of 2026-11-17, relayed by the coordinator, label exact
and only as a label.

- **D3, the linter pin.** Chosen label: "v2.15.0 everywhere, in each
  repository's Go-pins record (Recommended)". As put: six repositories pin
  v2.13.1 in CI, which cannot type-check under Go 1.27.4; each record is
  amended to include the CI pin; any new finding the newer linter reports is
  brought to the owner with the code rather than suppressed. This pair was
  uncommitted, so its text was corrected in place and this entry dated.
- **D4, F1.** Chosen label: "Use the backquote verb `%#q`, and add a test for
  the cycle warning (Recommended)". As put: for an ordinary name the output is
  byte-for-byte what it is today; only a name with a backquote, a control
  character or invalid UTF-8 prints differently; the new test builds a cycle
  and asserts the warning's text, covering a branch that has no test.
- **D5, the fork's line.** Chosen label: "`go 1.26.11`, committed under the
  Go-pins record (Recommended)". As put: the fork matches the repository's
  floor; added to this record by amendment, with a build and the fork's
  tests; it replaces the owner's uncommitted 1.26.6 edit with a committed
  1.26.11, on this word.
- **D6, F2.** Chosen label: "A fuller redesign of the middleware under its
  own record". As put: turn the closure into a small type with methods;
  cleaner long term and larger; the Go move waits for it. Another agent
  drafts that record; this record does not build it. Consequence: this change
  is not pushed until the redesign has landed, because CI's lint under
  v2.15.0 would be red on F2 in between (PLAN, sequencing note).
- **D7, the Go search paths.** Chosen label: "Fix it in the 0005 plan: drop
  the stale names, prefer the newest SDK (Recommended)". The 0005 plan is
  amended for `execute_pipeline.go:903-904`; this record's former open
  question 1 is answered there and removed from the list below.

### Findings of golangci-lint v2.15.0

Outcomes (2026-11-17): F1 is fixed under D4 (the verb is `%#q`; the test is
`internal/handler/pipeline_tools_cycle_test.go`; `make lint` under v2.15.0
no longer reports it). F2 is D6's, untouched here, and is the only finding
`make lint` reports. The text below is the finding as put to the owner.

Both are new with v2.15.0 (v2.13.1 reports 0 issues under 1.26.11, E10); both
reproduce under `go1.27.4` and `go1.26.11`; neither file is in the 0005
plan's set (`execute_pipeline*.go`, `inventory.go`, `schema_util.go`,
`prompts.go`, `docs/guides/mcp-tools.md`).

**F1, `gocritic` `sprintfQuotedString`, `internal/handler/pipeline_tools.go:161:33`**

```go
for _, neighbor := range adj[urn] {
    if dfs(neighbor) {
        warnings = append(warnings, fmt.Sprintf("⚠️ Semantic Gatekeeper Warning: Circular Dependency Detected connecting `%s` natively.", neighbor))
    }
}
```

- The rule: a `%s` wrapped in quote characters (here backquotes) should be
  `%q` (or `%#q` for backquotes), because `%q` escapes what the plain form
  prints raw.
- Proposed fix: `connecting %#q natively.` with the same `neighbor` argument.
- Behaviour change: **only for unusual values.** `neighbor` is a tool name or
  URN from the server registry (the keys of `adj`, built from `ToolName` and
  `URN` fields, normally `server:tool`). For a name with no backquote, no
  control character and valid UTF-8, `%#q` prints `` `name` ``, byte-identical
  to today. For a name containing a backquote, a control character or invalid
  UTF-8, `%#q` prints a double-quoted escaped string instead of raw text. The
  text is appended to the `warnings` slice and rendered by
  `execute_pipeline.go:120-125` as a Markdown bullet under "Semantic
  Gatekeeper Warnings" in the pipeline plan that a model and the operator
  read; it is not parsed by code or sent as a protocol field.
- Coverage: none for this line. `go test -coverprofile` shows the block at
  `pipeline_tools.go:160-161` with count 0; `topologicalSort` is 78.4%
  covered, by `TestTopologicalSortChronological`
  (`pipeline_tools_test.go:127`), which has no cycle and asserts no warnings.
  No test asserts the warning text, so a fix is not exercised.

**F2, `gocognit`, `internal/handler/list_tools_handler.go:19:1`**

```go
// ListToolsMiddleware intercepts 'tools/list' to provide robust recovery and aggregation.
func (h *OrchestratorHandler) ListToolsMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
    return func(ctx context.Context, method string, req mcp.Request) (result mcp.Result, err error) {
```

- The rule: cognitive complexity 52 of `ListToolsMiddleware` exceeds the
  repository's own limit of 50 (`.golangci.yml:32-33`, `min-complexity: 50`).
  The complexity comes from the nested closure: the recover block, the base
  layer, the static-inventory fallback and the LRU merge (lines 40-100).
- Proposed fix: extract the two self-contained loops into methods or
  functions, for example `staticInventoryTools(h.InternalTools)` and
  `mergeSessionTools(finalTools, lruTools)`, leaving the closure to sequence
  them. A pure extraction.
- Behaviour change: none if the extraction is exact. One care: the closure
  assigns `baseRes, err := next(...)`, which shadows the named result `err`
  and is the reason the deferred recover sets `err = nil`; the extraction must
  keep the `return &mcp.ListToolsResult{...}, nil` and the recover as they
  are.
- Coverage: `ListToolsMiddleware` is 95.0% covered, by
  `list_tools_handler_test.go`, `list_tools_concurrency_test.go` and
  `handler_exhaustive_test.go`. The file is cited as evidence (E11,
  `:70-96`) by the 0005 MADR but is not in the set that plan changes; a
  refactor here should wait for or be sequenced with 0005.

### Questions for the owner (not decided here)

The Go search paths (E7) went to the 0005 plan (D7) and the fork's `go`
line (E8) was ruled (D5); one question remains.

1. A floor job in CI (E3): (a) add one here, (b) add them fleet-wide in a
   separate record (recommended), (c) leave as is. Cost: (a) and (b) a CI job
   per repository.

### Relation to earlier records

There is no earlier Go-version record in this repository: `docs/0001` to
`0005` are about configuration, the orphan reaper, and the pipeline schema.
None is amended.

### Not verified

- That CI's `setup-go` v7.0.0 installs 1.27.4 on a runner: documentation only,
  no run observed, and nothing was pushed.
- That the Linux, Windows and WSL gate hosts hold only `go1.27.4` in their SDK
  directories: read from go-selfupdate-lib's 0045-MADR, not re-observed here.
- That `golangci-lint` is clean: under v2.15.0 it is not (F1, F2); under
  CI's former v2.13.1 it is clean on 1.26.11 and cannot type-check on 1.27.4
  (E10). CI's lint step itself was not run: nothing was pushed.
- Whether the release job's reusable workflow builds with 1.27.4: that is
  mcplib's `go.mod`.
- What GO-2026-6865 is beyond its title and the fixed version.

### Links

- [GO-2026-6865](https://pkg.go.dev/vuln/GO-2026-6865).
- [actions/setup-go at v7.0.0](https://github.com/actions/setup-go/blob/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e/README.md).
