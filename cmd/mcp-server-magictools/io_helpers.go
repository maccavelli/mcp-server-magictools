package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

func readConfigFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Clean(path)) //nolint:gosec // paths constructed from controlled config directory
}

func createConfigFile(path string) (*os.File, error) {
	return os.Create(filepath.Clean(path)) //nolint:gosec // paths constructed from controlled config directory
}

func openConfigFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(filepath.Clean(path), flag, perm) //nolint:gosec // paths constructed from controlled config directory
}

func bufferFromPool(pool *sync.Pool) *bytes.Buffer {
	v := pool.Get()
	buf, ok := v.(*bytes.Buffer)
	if !ok {
		return &bytes.Buffer{}
	}
	return buf
}

func execOrWarn(name string, args ...string) {
	if _, err := timedExec(name, args...); err != nil {
		slog.Warn("cmd: exec failed", "cmd", name, "args", args, "error", err)
	}
}

func signalOrWarn(p *os.Process, sig os.Signal) {
	if p == nil {
		return
	}
	if err := p.Signal(sig); err != nil {
		slog.Warn("cmd: signal failed", "signal", sig, "error", err)
	}
}

func writeStringOrWarn(f *os.File, s string) {
	if f == nil {
		return
	}
	if _, err := f.WriteString(s); err != nil {
		slog.Warn("cmd: write failed", "error", err)
	}
}

func mapFromAny(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func safeInt32FromInt(n int) int32 {
	const maxInt32 = int(^uint32(0) >> 1)
	if n > maxInt32 {
		return int32(maxInt32) //nolint:gosec // clamped to int32 max
	}
	if n < -maxInt32-1 {
		return int32(-maxInt32 - 1) //nolint:gosec // clamped to int32 min
	}
	return int32(n) //nolint:gosec // bounded to int32 range
}

func homeDirOrDot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("cmd: user home dir unavailable", "error", err)
		return "."
	}
	return home
}

func mkdirAllOrWarn(path string, perm os.FileMode) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(path, perm); err != nil {
		slog.Warn("cmd: mkdir failed", "path", path, "error", err)
	}
}

func closeConnOrWarn(conn net.Conn) {
	if conn == nil {
		return
	}
	if err := conn.Close(); err != nil {
		slog.Warn("cmd: failed to close connection", "error", err)
	}
}

func closeBodyOrWarn(body io.ReadCloser) {
	if body == nil {
		return
	}
	if err := body.Close(); err != nil {
		slog.Warn("cmd: failed to close response body", "error", err)
	}
}

func closeFileOrWarn(f *os.File, label string) {
	if f == nil {
		return
	}
	if err := f.Close(); err != nil {
		slog.Warn("cmd: failed to close file", "label", label, "error", err)
	}
}

func removeFileOrWarn(path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		slog.Warn("cmd: failed to remove file", "path", path, "error", err)
	}
}

func closeWatcherOrWarn(w *fsnotify.Watcher) {
	if w == nil {
		return
	}
	if err := w.Close(); err != nil {
		slog.Warn("cmd: failed to close watcher", "error", err)
	}
}

func chownOrWarn(path string, uid, gid int) {
	if path == "" {
		return
	}
	if err := os.Chown(path, uid, gid); err != nil {
		slog.Warn("cmd: failed to chown path", "path", path, "error", err)
	}
}

func flagStringOrEmpty(cmd *cobra.Command, name string) string {
	v, err := cmd.Flags().GetString(name)
	if err != nil {
		slog.Warn("cmd: failed to read flag", "name", name, "error", err)
		return ""
	}
	return v
}

func marshalIndentOrEmpty(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		slog.Warn("cmd: json.MarshalIndent failed", "error", err)
		return []byte("{}")
	}
	return b
}
