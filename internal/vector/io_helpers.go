package vector

import (
	"log/slog"
	"os"
)

func closeFileOrWarn(f *os.File, label string) {
	if f == nil {
		return
	}
	if err := f.Close(); err != nil {
		slog.Warn("vector: failed to close file", "label", label, "error", err)
	}
}

func removeOrWarn(path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		slog.Warn("vector: failed to remove file", "path", path, "error", err)
	}
}

func mkdirAllOrWarn(path string, perm os.FileMode) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(path, perm); err != nil {
		slog.Warn("vector: failed to create directory", "path", path, "error", err)
	}
}

func writeFileOrWarn(path string, data []byte, perm os.FileMode) {
	if path == "" {
		return
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		slog.Warn("vector: failed to write file", "path", path, "error", err)
	}
}

func embeddingHashFromMapVal(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}
