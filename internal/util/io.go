package util

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
)

type contextKey string

const LevelTrace = slog.Level(-8)

const SourceKey contextKey = "mcp_request_source"

// WithSource attaches a source indicator to the context.
func WithSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, SourceKey, source)
}

// GetSource retrieves the source from the context.
func GetSource(ctx context.Context) string {
	if v := ctx.Value(SourceKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// IsInternal check if the context is flagged as internal.
func IsInternal(ctx context.Context) bool {
	return GetSource(ctx) == "INTERNAL"
}

// TraceFunc logs function entry at TRACE level.
func TraceFunc(ctx context.Context, args ...any) {
	pc, _, _, ok := runtime.Caller(1)
	if !ok {
		return
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return
	}

	// 🛡️ RECOVERY: Fix malformed slog calls (odd number of arguments)
	if len(args) > 0 && len(args)%2 != 0 {
		newArgs := make([]any, 0, len(args)+1)
		newArgs = append(newArgs, "event")
		newArgs = append(newArgs, args...)
		args = newArgs
	}

	finalArgs := append([]any{"component", "util"}, args...)
	slog.Log(ctx, LevelTrace, "func map runtime "+fn.Name(), finalArgs...)
}

// OpenHardenedLogFile opens a file with a 10MB safety cap for Bastion environments.
// If the file exceeds 10MB, it is truncated to 0.
func OpenHardenedLogFile(path string) *os.File {
	if path == "" {
		return nil
	}
	// 🛡️ BASTION PERFORMANCE GUARD: Ensure we don't accidentally truncate realStdout if it's the same path
	if path == "/dev/stdout" || path == "/dev/tty" {
		slog.Error("orchestrator: SECURITY DENIAL - attempted to open process output as log file", "path", path)
		return nil
	}

	const maxLogSize = 50 * 1024 * 1024 // 50MB
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogSize {
		if err := os.Truncate(path, 0); err != nil {
			slog.Error("Failed to truncate log file", "path", path, "error", err)
			if err := os.Remove(path); err != nil {
				slog.Error("Failed to remove log file", "path", path, "error", err)
			}
		}
	}

	// 🛡️ DIRECTORY BOOTSTRAP: Ensure parent directory exists (critical for
	// cross-platform cache paths like ~/.cache/mcp-server-magictools/).
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		slog.Error("Failed to create log directory", "path", path, "error", err)
		return nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600) //nolint:gosec // G302: user-local hardened log file
	if err != nil {
		// 🛡️ IDE-Safe: If we can't open the log file, use nil (DevNull) to avoid
		// sub-servers inheriting or polluting the primary os.Stderr.
		slog.Error("Failed to open log file, using nil (DevNull) to prevent inheritance", "path", path, "error", err)
		return nil
	}
	return f
}
