// Package client provides functionality for the client subsystem.
package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/backplane"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Status is undocumented but satisfies standard structural requirements.
type Status string
type subServerCmd string

const (
	cmdConnect    subServerCmd = "CONNECT"
	cmdSync       subServerCmd = "SYNC"
	cmdDisconnect subServerCmd = "DISCONNECT"
)

const (
	StatusStarting     Status = "STARTING"
	StatusReady        Status = "READY"
	StatusHealthy      Status = "HEALTHY"
	StatusSyncing      Status = "SYNCING"
	StatusCrashed      Status = "CRASHED"
	StatusDisconnected Status = "DISCONNECTED"
	StatusOffline      Status = "OFFLINE"
)

// SubServer instance
type SubServer struct {
	Name                    string
	Status                  Status
	Session                 *mcp.ClientSession
	Process                 *exec.Cmd
	LastUsed                time.Time
	PingLatency             time.Duration
	LastPing                time.Time
	ConsecutivePingFailures int
	StartTime               time.Time
	TotalCalls              int64
	LastLatency             time.Duration
	ConsecutiveErrors       int
	RestartCount            atomic.Int64
	LastFailure             time.Time
	ConfigHash              string
	Command                 string
	Args                    []string
	Env                     map[string]string
	HandshakeError          error
	BackoffLevel            int
	LastRestartAt           time.Time
	CancelFunc              context.CancelFunc
	DesiredState            Status
	ReadyChan               chan struct{}
	Mailbox                 chan subServerCmd
	Ctx                     context.Context
	PendingRequests         *sync.Map // [string]bool (Response Router)
	Filter                  *jsonFilterReader
	Firewall                *jsonrpcFirewall // 🛡️ JSON-RPC stdio firewall instance

	ReadyOnce         sync.Once
	ActorStartedOnce  sync.Once
	HandshakeComplete bool
	LastCompletedAt   time.Time
	MemoryRSS         uint64
	CPUUsage          float64
	LastKnownPID      int          // 🛡️ Track last spawned PID to prevent self-orphan kills
	ActiveCalls       atomic.Int64 // 🛡️ In-flight proxy calls — guards against mid-flight restarts
	MemoryLimitMB     int
	GoMemLimitMB      int
	MaxCPULimit       int
	StderrFile        *os.File // 🛡️ HARDEN-FD: Retained for explicit close on process exit
}

// SendInitializedNotification implements a strict Initialization Notification for a ManagedServer (SubServer).
func (s *SubServer) SendInitializedNotification(w io.Writer) error {
	// Initialization: Construct the InitializedNotification struct.
	notif := backplane.InitializedNotification{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}

	// Verification: marshal the struct to JSON and verify the key "id" does not exist.
	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal initialized notification: %w", err)
	}
	if strings.Contains(string(data), `"id"`) {
		return fmt.Errorf("FATAL: initialized notification contains forbidden 'id' field: %s", string(data))
	}

	// The Write: Write the JSON to the sub-server's stdin, append a newline \n, and call Flush().
	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write initialized notification: %w", err)
	}

	switch flusher := w.(type) {
	case *bufio.Writer:
		if err := flusher.Flush(); err != nil {
			return fmt.Errorf("bufio flush initialized notification: %w", err)
		}
	case interface{ Flush() error }:
		if err := flusher.Flush(); err != nil {
			return fmt.Errorf("flush initialized notification: %w", err)
		}
	}

	// Timing: Add a time.Sleep(50 * time.Millisecond) immediately after the flush.
	// This prevents 'Message Interleaving'.
	time.Sleep(50 * time.Millisecond)

	return nil
}
