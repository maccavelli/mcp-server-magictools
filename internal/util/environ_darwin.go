//go:build darwin

package util

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// GetProcessEnviron returns the environment variables for the process as a
// slice of "KEY=VALUE" entries. gopsutil does not implement Environ on
// darwin, so this reads the kern.procargs2 sysctl, which the kernel permits
// for same-UID processes — sufficient for orphan detection of our own
// children. Two caveats inherent to the source data: entries reflect the
// environment as passed at exec time, and the trailing Apple runtime vector
// (executable_file=, ptr_munge=, ...) is indistinguishable from environment
// entries and is included. Callers do exact-match lookups, so the extra
// entries are harmless.
func GetProcessEnviron(pid int) ([]string, error) {
	buf, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, fmt.Errorf("kern.procargs2 pid %d: %w", pid, err)
	}
	if len(buf) < 4 {
		return nil, fmt.Errorf("kern.procargs2 pid %d: short read (%d bytes)", pid, len(buf))
	}
	argc := int(binary.NativeEndian.Uint32(buf[:4]))
	rest := buf[4:]

	// Executable path, then NUL padding.
	i := bytes.IndexByte(rest, 0)
	if i < 0 {
		return nil, fmt.Errorf("kern.procargs2 pid %d: malformed buffer", pid)
	}
	rest = rest[i+1:]
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}

	// argv[0..argc-1].
	for range argc {
		i = bytes.IndexByte(rest, 0)
		if i < 0 {
			return nil, fmt.Errorf("kern.procargs2 pid %d: truncated argv", pid)
		}
		rest = rest[i+1:]
	}

	// Environment entries until the terminating empty string or end of buffer.
	var env []string
	for len(rest) > 0 {
		i = bytes.IndexByte(rest, 0)
		var entry string
		if i < 0 {
			entry, rest = string(rest), nil
		} else {
			entry, rest = string(rest[:i]), rest[i+1:]
		}
		if entry == "" {
			break
		}
		env = append(env, entry)
	}
	return env, nil
}
