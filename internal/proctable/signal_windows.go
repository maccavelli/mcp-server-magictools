//go:build windows

package proctable

import (
	"os/exec"
	"strconv"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
)

// signalGroupAndPID ends pid and its tree with taskkill, as internal/client's killPIDGroup did for foreign pids.
// Windows has no TERM; both signals are forceful.
func signalGroupAndPID(pid int, _ client.Sig) error {
	if err := groupSignalTarget(pid); err != nil {
		return err
	}
	return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run() //nolint:gosec // pid validated and re-identified by the caller
}
