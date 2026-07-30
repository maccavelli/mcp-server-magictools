// Package client provides functionality for the client subsystem.
package client

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"

	"github.com/maccavelli/mcp-server-magictools/internal/util"
)

// TEL-2: StdinBytesWritten tracks total bytes written to all sub-server stdin pipes.
var StdinBytesWritten atomic.Int64

func (m *WarmRegistry) setupIOBridges(_ context.Context, name string, realStdin io.WriteCloser, stdinPR io.ReadCloser) {
	// Stdin Bridge (To Sub-Server) — 🛡️ "SPEAK CLEARLY" MANDATE (Newline + Flush)
	// Single-owner goroutine: no Mutex needed, no ticker needed.
	// Flush is called inline after every Write for deterministic delivery.
	go func() {
		writer := bufio.NewWriter(realStdin)

		buf := util.GetBuffer()
		defer util.PutBuffer(buf)
		defer func() {
			if err := writer.Flush(); err != nil {
				slog.Warn("final flush failed during shutdown", "component", "io_bridge", "server", name, "error", err)
			}
			if err := realStdin.Close(); err != nil {
				slog.Warn("failed to close stdin pipe during shutdown", "component", "io_bridge", "server", name, "error", err)
			}
		}()

		for {
			n, err := stdinPR.Read(*buf)
			if n > 0 {
				if _, writeErr := writer.Write((*buf)[:n]); writeErr != nil {
					slog.Warn("stdin write failed", "component", "io_bridge", "server", name, "error", writeErr)
					break
				}
				// TEL-2: Track total bytes written to sub-server stdin.
				StdinBytesWritten.Add(int64(n))
				if flushErr := writer.Flush(); flushErr != nil {
					slog.Warn("stdin flush failed", "component", "io_bridge", "server", name, "error", flushErr)
					break
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					slog.Info("stdin pipe closed cleanly", "component", "io_bridge", "server", name)
				} else {
					slog.Warn("stdin read error", "component", "io_bridge", "server", name, "error", err)
				}
				break
			}
		}
	}()
	// 🛡️ PERF: Stdout is passed directly to the transport layer (no bridge goroutine needed)
}
