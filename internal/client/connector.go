// Package client provides functionality for the client subsystem.
package client

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DisconnectServer gracefully disconnects a single sub-server.
// If deleteFromMap is true, the server entry is removed from the registry.
func (m *WarmRegistry) DisconnectServer(name string, deleteFromMap bool) {
	m.mu.Lock()
	s, ok := m.Servers[name]
	if !ok {
		m.mu.Unlock()
		return
	}
	s.ReadyChan = nil
	s.Status = StatusDisconnected
	// When the server is being removed from the map (eviction, sleep, shutdown),
	// also set DesiredState so the controlLoop goroutine — which holds a direct
	// pointer to this SubServer — does not auto-reconnect via reconcile().
	if deleteFromMap {
		s.DesiredState = StatusDisconnected
	}
	// Snapshot and nil-out session/process/cancel under lock to prevent
	// races with the spawnProcess Wait goroutine.
	session := s.Session
	process := s.Process
	cancelFunc := s.CancelFunc
	s.Session = nil
	s.Process = nil
	if deleteFromMap {
		delete(m.Servers, name)
	}
	m.mu.Unlock()

	if m.PIDDir != "" {
		if err := os.Remove(filepath.Join(m.PIDDir, name+".pid")); err != nil && !os.IsNotExist(err) {
			slog.Warn("lifecycle: failed to remove pid file on disconnect", "server", name, "error", err)
		}
	}

	m.closeSubServerSnapshot(session, process, name)

	if cancelFunc != nil {
		cancelFunc()
	}
	slog.Info("lifecycle: server offline", "name", name)
}

// DisconnectAll disconnects all registered sub-servers.
func (m *WarmRegistry) DisconnectAll() {
	var names []string
	m.mu.RLock()
	for name := range m.Servers {
		names = append(names, name)
	}
	m.mu.RUnlock()

	for _, name := range names {
		m.DisconnectServer(name, true)
	}
}

// DrainAndDisconnectAll disconnects all sub-servers concurrently,
// honoring the provided shutdown context deadline.
// HARD-33: Replaces sequential DisconnectAll during shutdown to bound
// total drain latency to O(5s) instead of O(5s × N).
func (m *WarmRegistry) DrainAndDisconnectAll(ctx context.Context) {
	var names []string
	m.mu.RLock()
	for name := range m.Servers {
		names = append(names, name)
	}
	m.mu.RUnlock()

	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			m.DisconnectServer(n, true)
		}(name)
	}

	// Wait for all disconnects or shutdown deadline
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("lifecycle: all servers disconnected cleanly")
	case <-ctx.Done():
		slog.Warn("lifecycle: shutdown deadline hit, some servers may not have drained")
	}
}

// closeSubServerSnapshot safely closes a session and kills a process using snapshotted values.
// HARD-32: Session close MUST complete before process kill. The session's final
// write to stdin must succeed or timeout cleanly before killProcessGroup destroys
// the pipe, preventing partial JSON-RPC writes.
func (m *WarmRegistry) closeSubServerSnapshot(session *mcp.ClientSession, process *exec.Cmd, name string) {
	var sessionDone chan struct{}
	if session != nil {
		sessionDone = make(chan struct{})
		go func(sess *mcp.ClientSession, serverName string, done chan struct{}) {
			defer close(done)
			defer func() { recover() }() //nolint:errcheck // intentional panic guard during teardown
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// REV-3: Renamed inner channel to closeDone to avoid shadowing the
			// outer 'done' parameter (maintenance hazard identified in dialectic).
			closeDone := make(chan struct{})
			go func() {
				defer func() { recover() }() //nolint:errcheck // intentional panic guard during teardown
				if err := sess.Close(); err != nil {
					slog.Debug("lifecycle: session close error during teardown", "server", serverName, "error", err)
				}
				close(closeDone)
			}()
			select {
			case <-closeDone:
			case <-ctx.Done():
				slog.Warn("lifecycle: session close timed out", "server", serverName)
			}
		}(session, name, sessionDone)
	}

	// HARD-32: Wait for session close to finish before killing the process.
	// This ensures the session's final JSON-RPC close frame is physically
	// written before the pipe is destroyed by killProcessGroup.
	if sessionDone != nil {
		<-sessionDone
	}

	if process != nil {
		if err := killProcessGroup(process); err != nil {
			slog.Debug("lifecycle: kill process group failed during teardown", "error", err)
		}
	}
}
