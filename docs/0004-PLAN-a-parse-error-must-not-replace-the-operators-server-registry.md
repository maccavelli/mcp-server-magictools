---
status: complete
date: 2026-09-04
associated-madr: "0004-MADR-a-parse-error-must-not-replace-the-operators-server-registry.md"
---

# Implement: the load path never writes the server registry

Associated MADR:
[0004-MADR-a-parse-error-must-not-replace-the-operators-server-registry.md](0004-MADR-a-parse-error-must-not-replace-the-operators-server-registry.md)

## Goal

Loading configuration cannot destroy configuration.

When this is complete:

* A `servers.yaml` that does not parse fails the load, naming the line, and is
  left byte-identical on disk.
* No code path can write the registry to a directory it did not resolve.
* Running `go test ./...` cannot touch the operator's real config directory, and
  a guard fails the suite if anything tries.
* Every registry write is atomic and leaves a `.prev`.

## Scope

### In scope

| item | MADR reference |
| --- | --- |
| `LoadFromViper` never writes | Decision Outcome 1 |
| Load errors are distinguished and reported truthfully | Decision Outcome 2 |
| Only `init` writes the template | Decision Outcome 3 |
| Atomic write with a `.prev` backup | Decision Outcome 4 |
| Delete the unqualified `SaveManagedServers` | Decision Outcome 5 |
| `TestMain` isolation plus a guard | Decision Outcome 6 |

### Out of scope

* **The IDE-migration feature itself.** It is moved out of the load path, not
  removed. Whether magictools should import an IDE's server list at all is a
  separate question and a different record.
* **`config.yaml` and `tool_overrides.yaml`.** They have their own load paths;
  this record found no equivalent defect in them and did not audit them.
* **Recovering the operator's damaged registry.** Already done, out of band:
  restored from `servers.yaml.bak-20260823-092601` before this plan was
  written, verified as 5 enabled servers with paths that exist on this host.

## Implementation Steps

### Phase 1 — Stop the load path writing (`internal/config/config.go`)

**1.1 Distinguish the three load outcomes.** `LoadManagedServersAt` currently
collapses "absent", "unreadable" and "unparseable" into one error, and mislabels
two of them as "not found". Return a typed or wrapped error that the caller can
tell apart, keeping the parser's message — it already carries the line number.

**1.2 `LoadFromViper` handles them differently and writes in none of them.**

| outcome | behaviour |
| --- | --- |
| absent | load with an empty registry, log at info naming `init` |
| unreadable | **fail the load** |
| unparseable | **fail the load**, surfacing the parser's line |

Failing rather than continuing empty is the point: an empty registry and an
unreadable one are indistinguishable to every downstream caller, and one of them
means the operator's servers are still on disk and must not be replaced.

**1.3 Move the IDE migration out of the load path.** It is the only reason
`LoadFromViper` writes. It moves to `init`, where writing is the declared
purpose.

**1.4 Delete `SaveManagedServers` (the unqualified form).** Callers pass the
resolved path. This removes MADR F2 by making it inexpressible rather than by
being careful — the bug was reading a resolved path and writing a hardcoded one,
and with one function there is only one path.

### Phase 2 — Make every registry write survivable

**2.1 Atomic write.** `SaveManagedServersAt` writes to a temp file in the same
directory, `fsync`s, and renames into place. `os.WriteFile` truncates first, so
an interrupted write leaves a half-file, which under the old Phase-1 behaviour
was then replaced by the template.

**2.2 `.prev` backup before any overwrite**, matching
`cmd/mcp-server-magictools/service_refresh.go:121-123`, which already does this
for a service definition and carries the reason: *"backup so a failed update can
restore exactly what was there."*

One generation, same directory. The operator has been keeping these by hand
since at least 2026-08-23; the program should do it.

### Phase 3 — Make the defect unreachable from the test suite

This is the half that answers "codified so it never happens". Phases 1 and 2 fix
the code; this stops the next author — or the next agent — reintroducing the
damage by running tests, which is how it reached the operator's machine.

**3.1 `TestMain` in every package that can reach `DefaultConfigDir()`** sets
`MCP_MAGIC_TOOLS_CONFIG_DIR` to a temp directory for the whole binary. The
existing per-test `t.Setenv` calls stay correct but stop being load-bearing.

**3.2 A guard that fails the suite if the real directory is touched.** Record
the real registry's mtime and hash before `m.Run()` and compare after. It must
name the file and say what happened, because the failure it catches is silent by
nature.

The guard is the deliverable here. A `TestMain` can be omitted from a new
package; a guard that fails loudly is what makes the omission visible.

## Verification

```bash
cd ~/gitrepos/go/mcp-server-magictools
MCP_MAGIC_TOOLS_CONFIG_DIR=$(mktemp -d) go test ./... -count=1
go vet ./...
gofmt -l internal cmd
```

The environment override on the command line is belt-and-braces while 3.1 is
being written; after it lands the suite is safe without it, and 3.2 proves so.

### Fail-first evidence required

Each against a scratch copy, never the working tree, and never the operator's
real config directory.

* `S1` — the MADR's reproduction (`zz_repro_test.go` case B): a registry with one
  stray tab. Currently **fails** — the registry is replaced by the template. It
  must pass, with the file byte-identical.
* `S2` — case C: an unreadable registry. Currently the template is written over
  it; it must fail the load and leave the file alone.
* `S3` — restore the unqualified `SaveManagedServers` and call `LoadFromViper`
  against a temp config dir; a test asserting the real directory is untouched
  must FAIL. This is MADR F2, and it is the one that reached the operator.
* `S4` — interrupt a write between temp-file creation and rename; the previous
  registry must still be intact and parseable.
* `S5` — delete the `TestMain` override and have one test write to the default
  directory; the guard must FAIL and name the file.

### Acceptance criteria

1. An unparseable registry fails the load with the parser's line number, and the
   file's sha256 is unchanged.
2. An unreadable registry fails the load, and the file is unchanged.
3. An absent registry loads empty, logs at info naming `init`, and writes
   nothing.
4. `grep -rn "SaveManagedServers(" --include='*.go'` matches only the definition
   of the `At` form.
5. `go test ./... -count=1` with **no** environment override leaves the
   operator's real `servers.yaml` byte-identical, asserted by hash.
6. An interrupted write leaves the previous registry parseable, and a completed
   one leaves a `.prev` holding exactly the previous bytes.

## Rollout and Rollback

**Rollout.** One binary; no migration. The behaviour change an operator can
notice is that a first run with no `servers.yaml` now starts with no managed
servers and a message naming `init`, where before it silently generated a
default set.

**Compatibility.** No file format change. An existing `servers.yaml` loads
exactly as before when it parses.

**Rollback.** Revert the commit. That reinstates a load path that overwrites the
operator's registry on a parse error, so it should only be done if the fail-closed
behaviour proves worse in practice — in which case the correct response is to
soften 1.2's *absent* case, not to restore the write.

## Execution Record

### 2026-09-04 — complete

**Phase 1.** `LoadManagedServersAt` now returns three distinguishable errors —
`ErrRegistryAbsent`, `ErrRegistryUnreadable`, `ErrRegistryUnparseable` — and
`LoadFromViper` acts on the difference: absent loads empty and points at `init`;
unreadable and unparseable **fail the load**. The template write and the IDE
migration are gone from the load path, and the unqualified `SaveManagedServers`
is deleted, replaced by a comment explaining why it must not come back.

**Phase 2.** `writeRegistryAtomically` copies the current file to `.prev`,
writes a temp file in the same directory, `fsync`s, and renames. It refuses to
overwrite a registry it could not read — the case where the existing contents
matter most and are least known.

**Phase 3.** `TestMain` redirects `MCP_MAGIC_TOOLS_CONFIG_DIR` for the whole
test binary and fingerprints the operator's real registry before and after
`m.Run()`, failing the suite with the before/after hashes if it changed.

### Fail-first evidence — five breakages, on a scratch copy

```text
S1/S2. the destructive load path restored
       FAIL TestUnparseableRegistryFailsTheLoadAndChangesNothing
       FAIL TestUnreadableRegistryFailsTheLoadAndChangesNothing
       FAIL TestAbsentRegistryLoadsEmptyAndWritesNothing
            LOADING CREATED A REGISTRY. Creating one is `init`'s job.

S3.    the unqualified SaveManagedServers restored
       FAIL TestNoUnqualifiedSaveHelperExists
            SaveManagedServers(servers) is back. It resolves DefaultConfigDir()
            while its callers resolve their own path …

S4.    the write abandoned between temp-file creation and rename
       save returned: simulated interruption before rename
       previous registry intact: 2 servers

S5.    TestMain's redirect removed, plus a test that writes the default dir
       THE TEST SUITE WROTE TO THE OPERATOR'S REAL SERVER REGISTRY.
         before: 6151422ed882508f327a100584e7809adf0bbbeab3a01e0291fb1dec3a641c8d
         after:  a53c38b20ed2a0a549697ebf04050b22ee8c55364128e2f100d990dfa5e4ac64
```

### S5 damaged the operator's registry, and Phase 2 is why that cost nothing

S5 proves the guard by actually doing the thing the guard detects, and it did:
the live `servers.yaml` was overwritten during that run. That was avoidable —
the case should have been driven with a fake default directory — and it is
recorded rather than tidied away, because of what happened next.

**The `.prev` written by Phase 2 held exactly the pre-write bytes**
(`6151422ed882508f…`). The first time the new code met a real accidental
overwrite, it preserved the previous contents unprompted. The file was restored
and its hash re-verified against the value recorded before any work began.

This is also the third time in this investigation that the defect reached the
operator's real file — twice by tests written by someone who had just read the
code that does it. That is the argument for Phase 3 being a guard rather than a
convention, stated by demonstration.

### Acceptance

| # | criterion | result |
| --- | --- | --- |
| 1 | unparseable fails the load with the line number; file unchanged | **pass** — asserted on sha256 |
| 2 | unreadable fails the load; file unchanged | **pass** |
| 3 | absent loads empty, writes nothing, names `init` | **pass** |
| 4 | no unqualified `SaveManagedServers` | **pass** — pinned by a source scan |
| 5 | `go test ./...` with no override leaves the live registry byte-identical | **pass** — `6151422e…` before and after the full suite |
| 6 | interrupted write leaves the registry parseable; `.prev` holds the old bytes | **pass** — S4, and observed live |

```text
gofmt -l internal cmd   -> clean
go vet ./...            -> clean
go test ./... -count=1  -> all packages ok, live registry unchanged
```

### Out-of-band: the operator's registry was already damaged, and was restored

Before this plan was written, the live registry was found to be
template-derived: every command had been rewritten from real macOS paths to
template values, including Linux placeholders (`/home/your-user/.venv/bin/python`)
on a macOS host, and four enabled servers had been disabled.

`servers.yaml.bak-20260823-092601` was the last good copy. Restored with the
operator's approval, keeping the damaged file as
`servers.yaml.bak-<ts>-template-derived`. Verified afterwards: 13 servers, 5
enabled, and all five enabled commands exist on this host.

The degradation happened between 2026-08-23 and 2026-08-29. The operator's own
backup named `-preclaude`, taken 2026-08-29 16:59, already contains the damage —
it was made after the fact by someone who had noticed the correlation without
being able to name the cause.

## Plan complete

All three phases executed with the fail-first evidence above.
