package proctable

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
)

var errBadStat = errors.New("proctable: malformed /proc stat line")

// parseStatStart returns field 22 (starttime, in clock ticks since boot) of a /proc/<pid>/stat line. The command name
// (field 2) is in parentheses and may itself hold spaces and parentheses, so fields are counted after the last ')'.
// The value is fixed for the life of the process, unlike gopsutil's CreateTime on Linux, which adds a boot time each
// reader computes for itself (MADR F5).
func parseStatStart(line []byte) (uint64, error) {
	i := bytes.LastIndexByte(line, ')')
	if i < 0 {
		return 0, errBadStat
	}
	fields := strings.Fields(string(line[i+1:]))
	// fields[0] is field 3 (state); field 22 is fields[19].
	const startIdx = 22 - 3
	if len(fields) <= startIdx {
		return 0, errBadStat
	}
	v, err := strconv.ParseUint(fields[startIdx], 10, 64)
	if err != nil || v == 0 {
		return 0, errBadStat
	}
	return v, nil
}
