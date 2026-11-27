package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// pidRecord is a server.pid. Start and Executable are empty on a file written
// by a binary from before 0016, which stored only the decimal pid.
type pidRecord struct {
	PID        int
	Start      uint64
	Executable string
}

func pidFilePath(store string) string {
	return filepath.Join(store, "server.pid")
}

// parsePIDFile accepts a bare decimal pid, or pid, start and executable on
// three lines. The first field is the pid so a reader that takes the first
// line still sees it. strconv.Atoi of the whole file does not: an old binary
// treats a new file as corrupt and does not signal.
func parsePIDFile(data []byte) (pidRecord, bool) {
	text := strings.TrimRight(string(data), "\r\n")
	if text == "" {
		return pidRecord{}, false
	}
	lines := strings.Split(text, "\n")
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || pid <= 0 {
		return pidRecord{}, false
	}
	rec := pidRecord{PID: pid}
	if len(lines) > 1 {
		start, err := strconv.ParseUint(strings.TrimSpace(lines[1]), 10, 64)
		if err != nil {
			return pidRecord{}, false
		}
		rec.Start = start
	}
	if len(lines) > 2 {
		rec.Executable = strings.Join(lines[2:], "\n")
	}
	return rec, true
}

func formatPIDFile(rec pidRecord) []byte {
	if rec.Start == 0 && rec.Executable == "" {
		return []byte(strconv.Itoa(rec.PID))
	}
	return []byte(fmt.Sprintf("%d\n%d\n%s\n", rec.PID, rec.Start, rec.Executable))
}

func writePIDFile(path string) error {
	rec := pidRecord{PID: os.Getpid()}
	if exe, err := selfExecutable(); err == nil {
		rec.Executable = exe
	}
	if start, err := selfStart(); err == nil {
		rec.Start = start
	}
	return os.WriteFile(pidFilePath(path), formatPIDFile(rec), 0o600)
}

func removePIDFile(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
