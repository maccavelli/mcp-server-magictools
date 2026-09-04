---
status: proposed
date: 2026-09-04
decision-makers: maccavelli
consulted: —
informed: —
---

# A parse error must not replace the operator's server registry with the shipped template

## Context and Problem Statement

The operator reports that their live `servers.yaml` is repeatedly replaced with
the shipped default template while working on this project with AI agents. The
evidence for how long this has been going on is in their own config directory:

```text
servers.yaml                                 16,566   Sep  2 16:55
servers.yaml.bak-20260823-092601             17,331
servers.yaml.bak-20260829-165904-preclaude   20,252   <- named after the cause
servers.yaml.bak-20260902-055413             11,648
```

Someone has been taking manual backups named after what they are defending
against.

This record is the analysis. Three defects were found; two were reproduced, and
one was observed writing to the operator's real configuration file during this
investigation.

**The live file is not currently damaged.** Only 17% of its 434 lines match the
template, and it carries 256 comment lines, 21 disabled entries, 23 `env` blocks
and 22 memory limits — it is substantially the operator's own work. Nothing
below rests on forensics of the current file; the mechanisms are demonstrated
directly instead.

### F1 — any load failure replaces the registry with the template

`internal/config/config.go:1103-1116`, inside `LoadFromViper`:

```go
managed, err := LoadManagedServersAt(serversPath)
if err != nil {
    slog.Info("config: servers.yaml not found, generating defaults", "error", err)
    ...
    if err := os.WriteFile(serversPath, []byte(defaultServersTemplate), 0600); err != nil {
```

The guard is `err != nil`, and `LoadManagedServersAt` (`:1457-1466`) returns an
error for **two** distinct conditions:

```go
data, err := os.ReadFile(path)
if err != nil {
    return nil, fmt.Errorf("servers.yaml not found: %w", err)   // ANY read error
}
var reg serversYAML
if err := yaml.Unmarshal(data, &reg); err != nil {
    return nil, fmt.Errorf("servers.yaml parse error: %w", err) // ANY parse error
}
```

So a file that exists, is readable, and contains the operator's entire registry
is deleted and replaced because one line of it does not parse.

Reproduced against a realistic registry with a single stray tab appended — the
classic YAML error, and exactly what a half-finished edit or a tool writing YAML
by hand produces:

```text
BEFORE: 3 servers [my-private-tool team-scanner gitlab]
config: servers.yaml not found, generating defaults
  error="servers.yaml parse error: yaml: line 10: found a tab character that violates indentation"
config: generated default servers.yaml
AFTER : 12 servers [brainstorm ddg-search evolve-plan filesystem git github
                    glab go-modernizer magicskills recall seq-thinking socratic-thinker]
```

Note the log line: it says **"not found"** while reporting a parse error. An
operator grepping their logs for why their registry vanished is told the file
was missing, which it never was.

The same branch fires on a transient read error — a permission problem, an I/O
error, a directory where a file was expected — all of which report as "not
found" and all of which discard the registry.

**`handleServersChange` in `watcher.go:270-275` gets this right**, on the same
data, three hundred lines away:

```go
servers, err := LoadManagedServersAt(path)
if err != nil {
    slog.Error("failed to reload servers.yaml", "component", "config", "error", err)
    return
}
```

It logs and returns. The reload path refuses to act on a registry it could not
read; the load path replaces it. The correct behaviour already exists in this
package and is not applied where it matters.

### F2 — the load path reads a resolved path and writes the hardcoded one

`LoadFromViper` resolves the registry path from the active config
(`config.go:1096-1101`), honouring a custom config file. It then reads from that
resolved path. But when the IDE-migration branch saves, it calls the
**unqualified** helper (`config.go:1149`):

```go
if saveErr := SaveManagedServers(managed); saveErr != nil {
```

and `SaveManagedServers` (`:1489-1491`) ignores the resolved path entirely:

```go
func SaveManagedServers(servers []ServerConfig) error {
	return SaveManagedServersAt(filepath.Join(DefaultConfigDir(), ServersConfigFile), servers)
}
```

Read from where you were told; write to where you assume. Any run against a
non-default config directory — a test, a second instance, a `--config` flag, a
CI job — writes its result over the operator's real registry.

**This was observed, not deduced.** The F1 reproduction above ran entirely
inside `t.TempDir()`, and its log contains:

```text
config: saved native server registry count=13
  path="/Users/saxsmith/Library/Application Support/mcp-server-magictools/servers.yaml"
config: migrated IDE servers to servers.yaml count=1
```

A test writing to a temp directory wrote to the operator's live configuration.
It did no harm on this occasion — `SaveManagedServersAt` preserves comments
through a `yaml.Node` tree, and the same 13 servers were rewritten, leaving the
file byte-identical at 434 lines and 16,566 bytes with only its mtime moved —
but that is luck, not design. The in-memory list at that moment was the
template's twelve plus one migrated entry, and had the surrounding conditions
differed by a little, that is what would have landed.

### F3 — no backup, and the destructive write is not atomic

The template overwrite (`config.go:1111`) and `SaveManagedServersAt`
(`config.go:1565`) both use `os.WriteFile`, which truncates in place. An
interrupted write leaves a truncated registry, which on the next load fails to
parse, which under F1 is replaced by the template.

Nothing is backed up before either write. The pattern is not unknown here:
`cmd/mcp-server-magictools/service_refresh.go:121-123` writes a `.prev` copy
before rewriting a service definition, with the comment *"backup so a failed
update can restore exactly what was there."* The registry — the more valuable
file — gets none.

### F4 — nothing stops the test suite from writing the operator's config, and this is the likeliest cause of the report

`DefaultConfigDir()` is redirectable (`MCP_MAGIC_TOOLS_CONFIG_DIR`), and several
existing tests use it correctly:

```text
internal/config/config_test.go:41,96,256   t.Setenv(EnvConfigDir, …)
internal/config/paths_test.go:18           t.Setenv(EnvConfigDir, dirOverride)
```

But it is **per-test and opt-in**. There is no `TestMain`, and no guard. A test
that forgets the line writes to the operator's real configuration directory —
which is precisely what happened during this investigation, on the first attempt,
by someone who had just finished reading the code that does it.

This is the finding that best explains *"every time I work with AI agents"*. An
agent working in this repository runs `go test ./...`. Any test that reaches
`LoadFromViper` without the environment override writes the registry it happens
to hold in memory over the operator's live file, and under F1 a template is
exactly what it is most likely to be holding.

The operator's backup named `-preclaude` was created on 2026-08-29 at 16:59.
That is a person who had worked out the correlation without being able to name
the cause.

## Decision Drivers

* Every path here destroys data that the program did not create and cannot
  reconstruct. A server registry holds hand-written commands, environment
  values and per-server limits.
* The correct behaviour already exists in this package (`handleServersChange`),
  which makes the inconsistency a bug rather than an open design question.
* F4 makes the defect reachable by anyone running the test suite, which is the
  normal act of working on the repository. It cannot be fixed by being careful.
* "It has to stop and be codified so it never happens" is a request for a
  mechanism, not a promise. Any fix that relies on future authors remembering
  something is not a fix.
* The operator has been defending themselves with manual `.bak` files for at
  least twelve days. Whatever ships must make that unnecessary rather than
  easier.

## Considered Options

* **Fail closed on load, separate initialisation from loading, and enforce test
  isolation in `TestMain`** — the load path never writes; only an explicit
  initialise may create a registry; the test binary cannot reach the real
  directory.
* **Fail closed on load only** — change the `err != nil` guard to
  `os.IsNotExist` and stop there.
* **Back up before overwriting** — keep the current behaviour but write a
  timestamped copy first.
* **Make the template overwrite opt-in via a flag or env var** — keep the code
  path, require `--init` or an env var to reach it.

## Decision Outcome

Chosen option: **"Fail closed on load, separate initialisation from loading, and
enforce test isolation in `TestMain`"**, because the three defects have one
shape — a *read* path that writes — and fixing them separately would leave the
shape intact. A load must be able to fail without consequence; creating a
registry is a different operation, and `init` already exists for it
(`cmd/mcp-server-magictools/init.go`).

Concretely:

1. **`LoadFromViper` never writes.** A missing registry loads as empty and logs
   at info, pointing at `init`. A registry that cannot be **parsed or read** is a
   hard error: it fails the load rather than continuing with an empty set, so no
   caller can mistake "I could not read your servers" for "you have no servers".
2. **The log line stops lying.** Distinguish not-found, unreadable, and
   unparseable, and report which one — with the parser's line number, which it
   already has.
3. **Only `init` writes the template**, which is what its `--force` flag and its
   existing confirmation are for.
4. **Every write is atomic and backed up.** Write to a temp file in the same
   directory, `fsync`, rename into place, after copying the current file to
   `.prev` — the shape `service_refresh.go` already uses.
5. **`SaveManagedServers`' unqualified form is deleted.** Callers pass the
   resolved path or they do not write. This removes F2 by removing the ability
   to express it.
6. **`TestMain` in every package that can reach `DefaultConfigDir()` points
   `MCP_MAGIC_TOOLS_CONFIG_DIR` at a temp directory**, and a guard test fails if
   the real directory's registry is touched during a run.

### Consequences

* Good, because the operator's registry stops being something the program feels
  entitled to replace. The only writer becomes an explicit, confirmed command.
* Good, because a YAML typo becomes a startup error naming the line, which is
  recoverable in seconds, instead of a silent replacement that is not
  recoverable at all.
* Good, because F2 is removed structurally: with no unqualified save, no caller
  can write a path it did not resolve.
* Good, because `TestMain` makes F4 unreachable rather than discouraged, which
  is what "codified" has to mean — the guard is what stops the next author, and
  the next agent, repeating what happened here today.
* Neutral, because `init` gains no new behaviour; it already creates these files
  and already has `--force`.
* Bad, because a first run with no `servers.yaml` now starts with no managed
  servers and a message, where before it silently produced a working default
  set. That is a real regression in first-run convenience, and it is the price
  of the file never being written behind the operator's back. `init` is one
  command and the message names it.
* Bad, because failing the load on an unparseable registry means a typo stops
  the orchestrator from starting, where today it starts with the wrong servers.
  Not starting is the better failure: it is loud, it is immediate, and it leaves
  the file intact to be fixed.
* Bad, because `.prev` files accumulate one per write. One generation only, in
  the same directory, is a bounded cost and is what `service_refresh.go`
  already accepts.

### Confirmation

* The F1 reproduction — a registry with one stray tab — leaves the file
  byte-identical and fails the load with the parser's line number. The test
  exists (`TestZZ_B`) and currently **fails**, which is what makes it evidence.
* The F2 reproduction — `LoadFromViper` against a temp config directory — writes
  nothing outside that directory. Asserted by comparing the real registry's
  hash across the test run, not by reading the log.
* A guard test fails when any test in the package writes to the directory named
  by `MCP_MAGIC_TOOLS_CONFIG_DIR`'s default, verified by deliberately writing
  there once and watching it fail.
* An interrupted write leaves the previous registry intact, driven by writing to
  the temp file and abandoning it before the rename.
* `grep -rn "SaveManagedServers(" --include='*.go'` returns only the definition
  of the `At` form.

## Pros and Cons of the Options

### Fail closed on load, separate initialisation from loading, and enforce test isolation

* Good, because it removes the class: a read path that cannot write cannot
  destroy anything, whatever the trigger.
* Good, because it fixes F2 by deletion rather than by care.
* Good, because `TestMain` is enforcement rather than convention, and F4 was
  caused by a convention being forgotten.
* Neutral, because it is four small changes in one package plus a test harness.
* Bad, because it changes first-run behaviour, which is the one place the
  current design is genuinely convenient.

### Fail closed on load only

* Good, because it is a two-line change and stops the reported symptom.
* Bad, because F2 survives: a test or a second instance still writes the
  operator's registry, and that is the mechanism most likely to be behind the
  report.
* Bad, because F4 survives entirely, so the next agent to run the suite
  reproduces the damage.

### Back up before overwriting

* Good, because it makes every current path recoverable without changing any
  behaviour.
* Good, because it is the smallest possible change that helps.
* Bad, because it accepts that the program will keep destroying the file and
  only promises to keep a copy. The operator is already doing this by hand; a
  program doing it automatically is a better version of the wrong answer.
* Bad, because a repeated fault overwrites the backup with the damaged state.
  One `.prev` plus a second failure is one `.prev` of the template.

### Make the template overwrite opt-in via a flag or env var

* Good, because it keeps first-run convenience for anyone who opts in.
* Bad, because the destructive path stays in the load path, one environment
  variable away, and environment variables are inherited by subprocesses and
  test runners — precisely the population that caused this.
* Bad, because it adds a configuration surface to control whether the program
  destroys data, which is not a decision that should be configurable.

## More Information

* **F1**: `internal/config/config.go:1103-1116` (the guard and the write),
  `:1457-1466` (`LoadManagedServersAt`'s two error returns). Correct
  counter-example: `internal/config/watcher.go:270-275`.
* **F2**: `internal/config/config.go:1096-1101` (path resolution), `:1149` (the
  unqualified save), `:1489-1491` (`SaveManagedServers`).
* **F3**: `internal/config/config.go:1111` and `:1565` (`os.WriteFile`).
  Existing correct pattern:
  `cmd/mcp-server-magictools/service_refresh.go:96-137`.
* **F4**: `DefaultConfigDir()` in `internal/config/paths.go`; `EnvConfigDir =
  "MCP_MAGIC_TOOLS_CONFIG_DIR"` at `internal/config/config.go:26`; existing
  per-test overrides at `internal/config/config_test.go:41,96,256` and
  `internal/config/paths_test.go:18`. No `TestMain` in the package.
* Reproduction: `internal/config/zz_repro_test.go` as written during this
  investigation — three cases (valid survives, unparseable is replaced,
  unreadable is replaced). It is not committed; the plan paired with this record
  should adopt it.
* The operator's config directory, for the backup filenames quoted above:
  `~/Library/Application Support/mcp-server-magictools/`.
* No implementation plan is paired with this record yet. Nothing above is
  implemented.
