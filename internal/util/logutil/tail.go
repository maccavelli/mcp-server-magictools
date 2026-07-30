package logutil

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// TailFile reads roughly the last N lines from a file using a backwards-seeking buffer.
func TailFile(path string, linesToScan int) ([]string, error) {
	if linesToScan <= 0 {
		return nil, nil
	}

	f, err := os.Open(filepath.Clean(path)) //nolint:gosec // caller supplies log paths under controlled directories
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Debug("logutil: tail file close failed", "path", path, "error", closeErr)
		}
	}()

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file %s: %w", path, err)
	}
	size := stat.Size()
	if size == 0 {
		return nil, nil
	}

	var candidateLines []string
	buffer := make([]byte, 4096)
	leftOver := ""
	pos := size

	for pos > 0 && len(candidateLines) < linesToScan {
		chunkSize := min(pos, int64(len(buffer)))
		pos -= chunkSize

		if _, err := f.Seek(pos, io.SeekStart); err != nil {
			return nil, fmt.Errorf("seek failed: %w", err)
		}

		n, err := f.Read(buffer[:chunkSize])
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read failed: %w", err)
		}

		content := string(buffer[:n]) + leftOver
		parts := strings.Split(content, "\n")

		if pos > 0 {
			// First part might be incomplete
			leftOver = parts[0]
			parts = parts[1:]
		} else {
			leftOver = ""
		}

		// Keep order by prepending
		if len(parts) > 0 {
			candidateLines = append(parts, candidateLines...)
		}

		// Cap to requested lines
		if len(candidateLines) > linesToScan {
			candidateLines = candidateLines[len(candidateLines)-linesToScan:]
		}
	}

	if leftOver != "" && len(candidateLines) < linesToScan {
		candidateLines = append([]string{leftOver}, candidateLines...)
		if len(candidateLines) > linesToScan {
			candidateLines = candidateLines[len(candidateLines)-linesToScan:]
		}
	}

	return candidateLines, nil
}
