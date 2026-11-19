//go:build linux

package proctable

import (
	"fmt"
	"os"
)

// startToken is the process's start time in clock ticks since boot (field 22 of /proc/<pid>/stat).
func startToken(pid int32) (uint64, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	return parseStatStart(b)
}

// pidNamespace identifies this process's pid namespace, so that an instance in a container never shares a lineage
// with one on the host that uses the same store path.
func pidNamespace() string {
	link, err := os.Readlink("/proc/self/ns/pid")
	if err != nil {
		return ""
	}
	return link
}
