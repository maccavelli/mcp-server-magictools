package telemetry

import (
	"log/slog"
	"net"
	"os"

	"github.com/blevesearch/mmap-go"
)

func closeFileOrWarn(f *os.File, label string) {
	if f == nil {
		return
	}
	if err := f.Close(); err != nil {
		slog.Warn("telemetry: failed to close file", "label", label, "error", err)
	}
}

func unmapOrWarn(m mmap.MMap) {
	if m == nil {
		return
	}
	if err := m.Unmap(); err != nil {
		slog.Warn("telemetry: failed to unmap file", "error", err)
	}
}

func closeConnOrWarn(conn net.Conn) {
	if conn == nil {
		return
	}
	if err := conn.Close(); err != nil {
		slog.Warn("telemetry: failed to close connection", "error", err)
	}
}

func writeUDPOrWarn(conn *net.UDPConn, payload []byte, addr *net.UDPAddr) {
	if conn == nil || addr == nil {
		return
	}
	if _, err := conn.WriteToUDP(payload, addr); err != nil {
		slog.Warn("telemetry: failed to write udp packet", "error", err)
	}
}

func serverNodeFromMapVal(v any) (*serverNode, bool) {
	n, ok := v.(*serverNode)
	return n, ok
}

func internalToolMetricsFromMapVal(v any) (*internalToolMetrics, bool) {
	m, ok := v.(*internalToolMetrics)
	return m, ok
}

func stringFromMapKey(k any) (string, bool) {
	s, ok := k.(string)
	return s, ok
}

func safeUint16FromInt(n int) uint16 {
	if n < 0 {
		return 0
	}
	const maxUint16 = ^uint16(0)
	if n > int(maxUint16) {
		return maxUint16 //nolint:gosec // clamped to uint16 max
	}
	return uint16(n) //nolint:gosec // bounded to uint16 range
}
