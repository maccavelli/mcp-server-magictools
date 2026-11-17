---
status: in-progress
date: 2026-11-17
associated-madr: "0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md"
---

# Implement: `go 1.26.11`, `toolchain go1.27.4`, and `klauspost/compress` v1.20.2

Associated MADR:
[0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md](0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md)
(option A, as the owner ruled).

## Goal

- `go.mod` says `go 1.26.11` and `toolchain go1.27.4`; every current
  statement of the version agrees.
- `github.com/klauspost/compress` is `v1.20.2` and `govulncheck ./...` exits 0
  under `go1.27.4` and `go1.26.11`.
- No tag, no release, no install, no restart: the plan stops at the tree.

## Scope

### In scope

| File | Line at HEAD | Change |
| :--- | :--- | :--- |
| `go.mod` | 3 | `go 1.26.6` becomes `go 1.26.11`; a `toolchain go1.27.4` line follows it |
| `go.mod` | 16 | `github.com/klauspost/compress v1.18.6` becomes `v1.20.2` |
| `go.sum` | the two `klauspost/compress v1.18.6` lines | become the two `v1.20.2` lines (`h1:` and `/go.mod h1:`) |
| `Makefile` | 1 | `MOD_VERSION := 1.26.11` (defined, used nowhere) |
| `README.md` | 145 | "Go 1.26.11 or later is required to build this repository (CI builds with Go 1.27.4)" |
| `docs/guides/platform-installation.md` | 123 | "currently `go 1.26.11`, built and tested with Go 1.27.4" |
| `.github/workflows/ci.yml` | 89 | `golangci-lint@v2.13.1` becomes `@v2.15.0` (MADR E10, D3); no other line of the workflow |
| `internal/forks/hnsw/go.mod` | 3 | `go 1.21.4` at HEAD (the owner's uncommitted `go 1.26.6`) becomes `go 1.26.11` (MADR D5); requirements unchanged |
| `internal/handler/pipeline_tools.go` | 161 | the quoted `` `%s` `` becomes `%#q` (MADR D4, F1) |
| `internal/handler/pipeline_tools_cycle_test.go` | new | `TestTopologicalSortCycleWarning`: a two-tool cycle, the exact warning for an ordinary name and for a name containing a backquote |

### Out of scope

- `internal/handler/execute_pipeline.go:903-904` (MADR E7, D7: the 0005 plan
  is amended for it), and every file
  `0005-PLAN-make-execute-pipeline-schema-and-handler-agree.md` changes.
- `internal/handler/list_tools_handler.go` and F2 (MADR D6): the middleware
  redesign is its own record.
- History: `internal/client/cloexec_unix.go:13`'s "Go 1.26.5 idiom" comment,
  `docs/0002-MADR-...md:248`, and the records.
- Every other workflow line (the Go pins follow `go.mod`, MADR E3), a CI floor
  job (a named gap, an open owner question), suppressing or excluding any
  lint finding, and any tag, release or install.

## Rules

- One Python script with `assert` on each old string's count made every edit.
- Heavy runs go through `run-heavy` on this Mac, one at a time.
- Nothing committed names a host, an account or a machine path.
- Records are staged by path; every other file stays unstaged. No commit.

## Implementation Steps

1. **Scratch first** (done): the bump on a copy of HEAD, the proxy used for
   that one download (MADR E6). Stop rule not met.
2. **Apply** the table above to the tree by the script; read the diff back.
3. `go mod tidy -diff` shows nothing; `git diff --stat` names only the table's
   files and the owner's fork file (untouched).
4. **The version-agreement check** (`check_versions.py` in the scratch folder,
   not added to the tree): reads `go` and `toolchain` from `go.mod`, asserts
   the Makefile, README and platform guide agree, and sweeps for any other
   `1.2x.y` outside the allowlist (`cloexec_unix.go`, `execute_pipeline.go`,
   the records, the fork). Seen to fail on three planted copies.
5. **Gates** (below), then the records and docs checks.

## Verification

- V1 baseline: `govulncheck` exit 3 under both toolchains (MADR E4).
- V2 agreement: exit 0 on the tree; exit 1 on a wrong toolchain, a wrong floor
  and a stray old doc pin.
- V3: `govulncheck` exit 0 under both toolchains; build, vet, tests at 1.27.4
  and at 1.26.11; cross-builds; `gofmt`; `markdownlint`.

## Rollout and Rollback

Before any commit, `git checkout` of the six files in the table reverts the
change (the owner's fork file is not among them). After a commit, a new commit
reverts it. Nothing is released.

## Execution Record

### 2026-11-17

- Start state: branch `main`, HEAD `310a541`, level with its upstream (no
  fetch); one unstaged change, the owner's `internal/forks/hnsw/go.mod`
  (`go 1.21.4` to `go 1.26.6`). Number 0006 from a whole-repository scan for
  `NNNN-{MADR,PLAN,REPORT,GATES}-*.md` (0001 to 0005 found; no index, no
  tooling).
- Scratch bump (the proxy used for the one download): `go get
  github.com/klauspost/compress@v1.20.2` and `go mod tidy` moved `go.mod` line
  16 and the two `go.sum` lines only.
- Applied; `go mod tidy -diff` exit 0; the fork file's diff is byte-identical
  to its start state.
- Version-agreement check: on the tree "0 failure(s)", exit 0. Planted, on
  scratch copies: wrong toolchain (`toolchain go1.27.3`): exit 1, README and
  platform guide "missing ... Go 1.27.3"; wrong floor (`go 1.26.10`): exit 1,
  Makefile, README and platform guide "missing ... 1.26.10" and
  `Makefile:1: MOD_VERSION := 1.26.11`; stray doc pin
  (`docs/guides/platform-installation.md` says `go 1.26.5`): exit 1, "stray
  version docs/guides/platform-installation.md:123".

| Gate | Start | End | Exit | Result |
| :--- | :--- | :--- | :--- | :--- |
| `gofmt -l` over non-fork Go files | 13:35:16 | 13:35:16 | 0 | clean |
| `go build ./...` (1.27.4) | 13:35:16 | 13:35:25 | 0 | |
| `go vet ./...` (1.27.4) | 13:35:25 | 13:35:28 | 0 | |
| `go mod tidy -diff` | 13:35:28 | 13:35:28 | 0 | nothing moves |
| `go test -count=1 -timeout 20m ./...` via `run-heavy` | 13:35:28 | 13:38:01 | 0 | every package `ok` (queued 128 s) |
| floor: `GOTOOLCHAIN=go1.26.11 GOPROXY=off` build and vet | 13:38:07 | 13:38:18 | 0 | `go1.26.11` |
| floor: same, `go test -count=1 -timeout 20m ./...` via `run-heavy` | 13:38:18 | 13:39:56 | 0 | no `FAIL` |
| `govulncheck ./...` under `go1.27.4`, `GOPROXY=off` | 13:39:56 | 13:40:04 | 0 | "No vulnerabilities found." |
| `govulncheck ./...` under `go1.26.11`, `GOPROXY=off` | 13:40:04 | 13:40:10 | 0 | "No vulnerabilities found." |
| cross-build linux amd64, `CGO_ENABLED=0` | 13:40:10 | 13:40:21 | 0 | |
| cross-build windows amd64, `CGO_ENABLED=0` | 13:40:21 | 13:40:33 | 0 | |
| cross-build darwin arm64, `CGO_ENABLED=0` | 13:40:33 | 13:40:37 | 0 | |
| `make lint` with a newer local binary (superseded; see the lint entry below) | 13:40:37 | 13:40:53 | 2 | one `gocritic` finding at `pipeline_tools.go:161:33`; this run's "pre-existing" label is corrected below |
| `markdownlint-cli2` on the two edited docs | 13:40:53 | 13:40:53 | 0 | 0 issues |

Before and after (`govulncheck` v1.9.0, database 2026-11-11 02:51 UTC): HEAD
exit 3 under both toolchains with GO-2026-6865 at `klauspost/compress@v1.18.6`;
the changed tree exit 0 under both, with "your code is affected by 0
vulnerabilities" and one imported-package finding left that the code does not
call (two at HEAD).

Not done: no commit, push, tag, release or install; no CI run observed; no
floor job exists in CI.

### 2026-11-17, the lint pin (deviation and the owner's ruling D3)

The coordinator relayed, after the gates above, that golangci-lint v2.13.1
(CI's pin, `ci.yml:89`) cannot type-check code under Go 1.27.4 in two sibling
repositories, and that the owner ruled "v2.15.0 everywhere, in each
repository's Go-pins record (Recommended)" (MADR D3). The pair was
uncommitted, so it was corrected in place.

- Changed: `.github/workflows/ci.yml:89` to `@v2.15.0`; nothing else in the
  workflow. No other file states a lint version (Makefile hint says
  `@latest`; README and guides: none).
- Linters built as CI builds them: `GOBIN=<scratch>/bin go install
  github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<version>` under
  `GOTOOLCHAIN=go1.27.4`; both versions were in the module cache, so the
  proxy was not used for this. `version` lines: "golangci-lint has version
  2.15.0 built with go1.27.4" and "golangci-lint has version 2.13.1 built
  with go1.27.4".

| Run | Tree | Toolchain | Start | End | Exit | Result |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `make lint`, v2.15.0 | changed | `go1.27.4` | 13:47:44 | 13:48:01 | 2 | 2 issues: gocognit 1, gocritic 1 (MADR F2, F1); no type-check error |
| `make lint`, v2.15.0 | changed | `go1.26.11` | 13:48:01 | 13:48:19 | 2 | the same 2 issues |
| `make lint`, v2.13.1 | scratch copy of HEAD | `go1.26.11` | 13:48:20 | 13:48:45 | 0 | "0 issues." |
| `make lint`, v2.13.1 | scratch copy of HEAD | `go1.27.4` | 13:48:45 | 13:48:52 | 2 | `typecheck`: "could not import testing" (`internal/ui/init_test.go:3:8`) |

Both v2.15.0 findings are new with v2.15.0; CI's v2.13.1 does not report
`pipeline_tools.go:161` today. The first handoff's "pre-existing" label for
it is withdrawn: it was true of the code against a newer local binary, not of
what CI's pinned linter reports. Neither finding is fixed, suppressed or
excluded; the owner decides each (MADR F1, F2).

The version-agreement check gained a lint-pin part (`check_lint_pin.py` in
scratch): on the tree 0 failures, exit 0; on HEAD's `ci.yml` and on a planted
`@v2.13.1`, exit 1, "`.github/workflows/ci.yml:89: lint pin v2.13.1, want
v2.15.0`" and "install line is not @v2.15.0".

### 2026-11-17, the owner's rulings D4 to D7 (F1, the fork, F2, the search paths)

**Sequencing note (MADR D6).** This change must not be pushed until the F2
redesign (the middleware record, `0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md`)
has landed: CI's lint under v2.15.0 is red on F2 until then. Order of
commits: the redesign's record and change first, then this change. This plan
does not touch `list_tools_handler.go`.

**F1, tests first (D4).** The new test file was written before the fix. On
the unchanged code (`go test -count=1 -run TestTopologicalSortCycleWarning
-v ./internal/handler/`): exit 1; the ordinary-name case PASS (the text is
unchanged for ordinary names, as intended); the backquote case FAIL: "got: ...
connecting `srv:be`ta` natively." and "want: ... connecting "srv:be`ta"
natively.". The one-token change to `%#q` followed: exit 0, both PASS. Planted
break on a scratch copy (the verb changed to `%q`): exit 1, the ordinary-name
case FAIL: "got: ... connecting "srv:beta" natively." and "want: ...
connecting `srv:beta` natively." (the delimiters change), the backquote case
PASS. Coverage after the fix: `pipeline_tools.go:160.4,160.21` and
`:161.5,162.1` count 1 (they were 0); `topologicalSort` 94.1% (was 78.4%).

**The fork (D5).** `internal/forks/hnsw/go.mod:3`: before the change, the
working tree held the owner's uncommitted `go 1.26.6` (HEAD: `go 1.21.4`);
after, `go 1.26.11`, the root's floor. In a scratch copy first, then in
place: `go mod tidy -diff` in the fork exits 0 with nothing to change. Four
old dependency versions (testify v1.8.0, x/exp, x/sys v0.11.0, check.v1,
kr/pretty) were not in the module cache and were fetched through the proxy
into the cache; no file in the tree changed because of it. In
`internal/forks/hnsw`: `go build ./... && go vet ./...` exit 0 and
`go test -count=1 -timeout 20m ./...` exit 0 (packages `hnsw` and `hnsw/heap`
`ok`) under `go1.27.4` and under `GOTOOLCHAIN=go1.26.11`, both with
`GOPROXY=off` after the fetch. The root's own gates were re-run after the
last Go change (table below). Version-agreement check: its lint-pin part
now also asserts the fork's `go` line equals the root's floor: pass on the
tree; planted fork `go 1.26.6`: exit 1, "internal/forks/hnsw/go.mod: go
1.26.6, want root floor go 1.26.11".

**Search paths (D7).** `execute_pipeline.go:903-904` moved to the 0005 plan,
which is amended for it; it is no longer an open question here.

**Lint as CI will run it (scratch v2.15.0 binary, `make lint`,
`GOPROXY=off`).** Under `go1.27.4` (13:57:48 to 13:57:54) and under
`GOTOOLCHAIN=go1.26.11` (13:57:54 to 13:58:01) the exit is 2 with exactly one
finding, F2: internal/handler/list_tools_handler.go:19:1, "cognitive
complexity 52 of func (*OrchestratorHandler).ListToolsMiddleware is high
(> 50) (gocognit)", then "1 issues: gocognit: 1". F1 is gone. F2 is untouched.

| Gate, after the last Go change | Start | End | Exit | Result |
| :--- | :--- | :--- | :--- | :--- |
| `gofmt -l` (non-fork Go files and the new test) | 13:58:01 | 13:58:01 | 0 | clean |
| `go build ./...` (1.27.4) | 13:58:01 | 13:58:04 | 0 | |
| `go vet ./...` | 13:58:04 | 13:58:04 | 0 | |
| `go mod tidy -diff` | 13:58:04 | 13:58:05 | 0 | |
| `go test -count=1 -timeout 20m ./...` via `run-heavy` | 13:58:05 | 14:17:29 | 0 | 19 packages `ok` (most of it queue wait) |
| `go test -race -count=1 -timeout 20m ./internal/handler/...` via `run-heavy` | 14:17:29 | 14:28:28 | 0 | `ok` |
| floor: `GOTOOLCHAIN=go1.26.11` build and vet | 14:28:28 | 14:28:32 | 0 | |
| floor: same, `go test -count=1 -timeout 20m ./...` via `run-heavy` | 14:28:33 | 15:06:09 | 0 | 19 packages `ok` |
| `govulncheck ./...` under `go1.27.4`, `GOPROXY=off` | 15:06:09 | 15:06:17 | 0 | "No vulnerabilities found." |
| `govulncheck ./...` under `go1.26.11`, `GOPROXY=off` | 15:06:17 | 15:06:23 | 0 | "No vulnerabilities found." |
| cross-builds linux, windows (amd64), darwin (arm64), `CGO_ENABLED=0` | 15:06:23 | 15:06:31 | 0 each | |

## Deviations

- 2026-11-17: CI's linter pin v2.13.1 cannot type-check Go 1.27.4 (MADR E10).
  Resolution, by the owner's ruling D3: the pin moves to v2.15.0 in this
  record. Consequence: `make lint` with v2.15.0 exits 2 on two findings that
  are the owner's to decide (MADR F1, F2); CI's lint step will fail on them
  until the owner rules, and this plan does not hide them. Resolved
  2026-11-17: F1 fixed (D4); F2 is the redesign record's (D6), and this
  change waits for it (sequencing note above).
