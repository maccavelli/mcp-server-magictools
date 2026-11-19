package client

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

// The per-instance owner stamp of 0010-MADR-make-tests-unable-to-signal-real-processes.md ("Design as approved", D4).
//
// Every sub-server keeps MCP_ORCHESTRATOR_OWNED=true, which the sub-servers themselves read to know they run under
// the orchestrator. Ownership is carried by a separate variable, which names the instance's lineage (its store
// directory) and the owning process (its pid and start token). These functions are pure.

// EnvOwner is the environment variable that carries the owner stamp.
const EnvOwner = "MAGICTOOLS_OWNER"

const (
	ownerStampVersion = "v1"
	lineageHexLen     = 32
)

// Owner names the orchestrator process that started a sub-server, and the instance (lineage) it belongs to.
type Owner struct {
	Lineage string
	PID     int32
	Start   uint64
}

// Stamp renders the owner as the value of EnvOwner: v1:<lineage>:<pid>:<start token>.
func (o Owner) Stamp() string {
	return fmt.Sprintf("%s:%s:%d:%d", ownerStampVersion, o.Lineage, o.PID, o.Start)
}

// EnvEntry renders the owner as one KEY=VALUE environment entry.
func (o Owner) EnvEntry() string { return EnvOwner + "=" + o.Stamp() }

// ParseOwner finds and parses the owner stamp in env. It reports false for a missing stamp, a malformed one, a foreign
// version, or two different values: a stamp the reaper cannot attribute is never acted on.
func ParseOwner(env []string) (Owner, bool) {
	prefix := EnvOwner + "="
	value, seen := "", false
	for _, e := range env {
		v, ok := strings.CutPrefix(e, prefix)
		if !ok {
			continue
		}
		if seen && v != value {
			return Owner{}, false
		}
		value, seen = v, true
	}
	if !seen {
		return Owner{}, false
	}
	return parseOwnerValue(value)
}

func parseOwnerValue(v string) (Owner, bool) {
	f := strings.Split(v, ":")
	if len(f) != 4 || f[0] != ownerStampVersion || !validLineage(f[1]) {
		return Owner{}, false
	}
	pid, ok := parseDecimal(f[2])
	// pid 1 is init or launchd: no orchestrator owns sub-servers from there (AttachProcessTable refuses pid <= 1), so a
	// stamp naming it is not attributable (the review's state A14).
	if !ok || pid < 2 || pid > math.MaxInt32 {
		return Owner{}, false
	}
	start, ok := parseDecimal(f[3])
	if !ok || start == 0 {
		return Owner{}, false
	}
	return Owner{Lineage: f[1], PID: int32(pid), Start: start}, true
}

// parseDecimal accepts only plain decimal digits: no sign, no spaces, no leading plus.
func parseDecimal(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil
}

func validLineage(s string) bool {
	if len(s) != lineageHexLen {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// LineageFor names an instance: the first 32 hexadecimal characters of SHA-256 over the canonical store directory and
// the pid-namespace identity, separated by a NUL byte.
func LineageFor(canonicalStoreDir, namespace string) string {
	sum := sha256.Sum256([]byte(canonicalStoreDir + "\x00" + namespace))
	return hex.EncodeToString(sum[:])[:lineageHexLen]
}

// envPeerID is the random per-spawn variable older binaries stamped and their start-up sweep matched by prefix. It has
// no reader left (0010-MADR, F7, D6); it is no longer stamped and is stripped from what a child inherits.
const envPeerID = "MAGIC_TOOLS_PEER_ID"

// inheritedStamps are the orchestrator variables a child must never inherit: this process may itself run under another
// orchestrator, or under a sub-server, or in a test, and a stale value would name the wrong owner.
var inheritedStamps = []string{EnvManaged + "=", EnvServerName + "=", EnvOwner + "=", envPeerID + "="}

// stampEnvironment returns env without any inherited orchestrator variable, followed by the stamp of a child named
// name: MCP_ORCHESTRATOR_OWNED=true (the sub-servers' own signal that they run under the orchestrator, kept as is),
// MCP_ORCHESTRATOR_SERVER_NAME=<name>, and, when owner is not nil, the owner stamp. env is not modified.
func stampEnvironment(env []string, owner *Owner, name string) []string {
	out := make([]string, 0, len(env)+3)
	for _, e := range env {
		inherited := false
		for _, p := range inheritedStamps {
			if strings.HasPrefix(e, p) {
				inherited = true
				break
			}
		}
		if !inherited {
			out = append(out, e)
		}
	}
	out = append(out, EnvManaged+"="+EnvManagedValue, EnvServerName+"="+name)
	if owner != nil {
		out = append(out, owner.EnvEntry())
	}
	return out
}

// ownerStamp is the owner this registry's children carry, or nil before a table is attached.
func (m *WarmRegistry) ownerStamp() *Owner {
	b := m.procs.Load()
	if b == nil {
		return nil
	}
	o := b.owner
	return &o
}

// CanonicalStoreDir resolves dir to the absolute, symlink-free, cleaned path the lineage is computed from.
func CanonicalStoreDir(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("canonical store directory: empty path")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("canonical store directory: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("canonical store directory: %w", err)
	}
	return filepath.Clean(resolved), nil
}
