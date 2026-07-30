// Package client provides functionality for the client subsystem.
package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"os"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/hfsc"
	"github.com/maccavelli/mcp-server-magictools/internal/logging"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/util"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MaxRunningServers is the LRU eviction threshold.
const MaxRunningServers = 50

// CircuitBreakerThreshold is the number of consecutive errors before the circuit opens.
const CircuitBreakerThreshold = 3

// BootSummary contains the results of the entire boot sequence.
type BootSummary struct {
	TotalAttempted int
	Success        int
	Limping        int
	Failed         int
	StartTime      time.Time
}

// LLMTraceEntry records a single LLM call trace received via HFSC_LLM_TRACE.
type LLMTraceEntry struct {
	Server     string    `json:"server"`
	Tier       string    `json:"tier"`
	Model      string    `json:"model"`
	LatencyMs  int64     `json:"latency_ms"`
	Tokens     int       `json:"tokens"`
	ReceivedAt time.Time `json:"received_at"`
}

// LLMMetricsProvider abstracts the LLM pool metrics surface to decouple
// the WarmRegistry from the concrete llm.Pool type.
type LLMMetricsProvider interface {
	Metrics() map[string]any
}

// WarmRegistry handles a pool of sub-servers and ecosystem synchronization.
// It is the SOLE OWNER of sub-process lifecycles.
type WarmRegistry struct {
	Servers           map[string]*SubServer
	HFSC              *hfsc.Registry
	PIDDir            string
	Config            *config.Config
	Store             *db.Store
	Logger            *logging.BackplaneLogger
	LogSink           io.Writer // 🛡️ Troubleshooting: Redirects all sub-server communication
	IsSynced          atomic.Bool
	lastConfigModTime time.Time
	mu                sync.RWMutex

	// Shared LLM Backplane connection details (set by orchestrator at startup)
	LLMEnabled atomic.Bool
	LLMAddr    string // e.g., "127.0.0.1:48081"
	LLMToken   string // scoped Bearer token for LLM endpoints

	// HFSC_LLM_TRACE ring buffer: stores last 50 trace entries from sub-servers
	llmTraceRing [50]LLMTraceEntry
	llmTraceIdx  int
	llmTraceMu   sync.Mutex

	// LLMMetrics is the injected LLM pool metrics provider (set by main.go at boot).
	LLMMetrics LLMMetricsProvider

	// LifecycleCtx is the parent context for restart goroutines.
	// Derived from the application errgroup context so restarts abort on shutdown.
	LifecycleCtx context.Context // 🛡️ HARDEN-ORPHAN: Parent context for restart goroutines

	// OnPromptListChanged is triggered by sub-server prompt list_changed notifications
	OnPromptListChanged func(serverName string)
}

// GetLLMTraces returns the last N entries from the HFSC_LLM_TRACE ring buffer.
// Used by self_check to surface sub-server LLM telemetry.
func (m *WarmRegistry) GetLLMTraces(limit int) []LLMTraceEntry {
	m.llmTraceMu.Lock()
	defer m.llmTraceMu.Unlock()

	count := min(limit, min(m.llmTraceIdx, len(m.llmTraceRing)))
	if count <= 0 {
		return nil
	}

	result := make([]LLMTraceEntry, 0, count)
	for i := range count {
		pos := (m.llmTraceIdx - 1 - i) % len(m.llmTraceRing)
		if pos < 0 {
			pos += len(m.llmTraceRing)
		}
		e := m.llmTraceRing[pos]
		if e.ReceivedAt.IsZero() {
			break
		}
		result = append(result, e)
	}
	return result
}

// NewWarmRegistry creates the single-owner registry.
func NewWarmRegistry(pidDir string, store *db.Store, cfg *config.Config) *WarmRegistry {
	return &WarmRegistry{
		Servers:           make(map[string]*SubServer),
		HFSC:              hfsc.NewRegistry(config.DefaultCacheDir()),
		PIDDir:            pidDir,
		Store:             store,
		Config:            cfg,
		Logger:            logging.Default,
		lastConfigModTime: time.Now(),
	}
}

// GetServer is undocumented but satisfies standard structural requirements.
func (m *WarmRegistry) GetServer(name string) (*SubServer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.Servers[name]
	return s, ok
}

// GetServerSession atomically snapshots a server's Session under RLock.
// The returned *mcp.ClientSession is safe to use after the lock is released
// because Go's GC keeps the object alive as long as any reference exists.
// If the session is closed concurrently, CallTool/Ping will return an error.
func (m *WarmRegistry) GetServerSession(name string) (*mcp.ClientSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.Servers[name]
	if !ok || s.Session == nil {
		return nil, false
	}
	return s.Session, true
}

// HasServer is undocumented but satisfies standard structural requirements.
func (m *WarmRegistry) HasServer(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.Servers[name]
	return ok
}

// RLock is undocumented but satisfies standard structural requirements.
func (m *WarmRegistry) RLock() {
	m.mu.RLock()
}

// RUnlock is undocumented but satisfies standard structural requirements.
func (m *WarmRegistry) RUnlock() {
	m.mu.RUnlock()
}

// RequestState is undocumented but satisfies standard structural requirements.
func (m *WarmRegistry) RequestState(name string, state Status) {
	m.mu.Lock()
	s, ok := m.Servers[name]
	if !ok {
		s = m.initSubServer(name)
		m.Servers[name] = s
	}
	s.DesiredState = state
	mailbox := s.Mailbox
	m.mu.Unlock()

	select {
	case mailbox <- cmdConnect:
	default:
	}
}

// Boot ensures all managed servers are requested to be healthy.
func (m *WarmRegistry) Boot(ctx context.Context, servers []config.ServerConfig) {
	slog.Info("WarmRegistry: initiating Boot sequence", "total", len(servers))
	for _, srv := range servers {
		m.mu.Lock()
		s, ok := m.Servers[srv.Name]
		if !ok {
			s = m.initSubServer(srv.Name)
			m.Servers[srv.Name] = s
		}
		s.Command = srv.Command
		s.Args = srv.Args
		s.Env = srv.Env
		s.MemoryLimitMB = srv.MemoryLimitMB
		s.GoMemLimitMB = srv.GoMemLimitMB
		s.MaxCPULimit = srv.MaxCPULimit
		s.ConfigHash = srv.Hash()
		s.DesiredState = StatusHealthy
		mailbox := s.Mailbox
		m.mu.Unlock()

		select {
		case mailbox <- cmdConnect:
		default:
		}
	}
}

func (m *WarmRegistry) initSubServer(name string) *SubServer {
	ctx, cancel := context.WithCancel(context.Background())
	s := &SubServer{
		Name:            name,
		Status:          StatusDisconnected,
		ReadyChan:       make(chan struct{}),
		Mailbox:         make(chan subServerCmd, 10),
		Ctx:             ctx,
		CancelFunc:      cancel,
		PendingRequests: &sync.Map{},
	}
	return s
}

// CallProxy executes a tool on a sub-server.
// Session is snapshotted under RLock to prevent a race with lifecycle teardown.
// reservedWireKeys are MCP params-level metadata keys (notably "_meta") that must never
// be sent to a sub-server as tool arguments — sub-servers with additionalProperties:false
// reject unknown keys. This is the wire-boundary backstop mirroring
// handler.reservedEnvelopeKeys; the two sets cannot be shared without an import cycle,
// since the handler package imports client.
var reservedWireKeys = []string{"_meta"}

// stripReservedWireKeys removes reserved envelope metadata from an argument map just
// before it crosses to a sub-server. Safe to call on a nil map. It is the final
// defense-in-depth complement to handler.stripReservedKeys, covering every caller —
// including cascade/DAG steps that bypass the handler dispatch funnel.
func stripReservedWireKeys(arguments map[string]any) {
	for _, k := range reservedWireKeys {
		delete(arguments, k)
	}
}

func (m *WarmRegistry) CallProxy(ctx context.Context, serverName, toolName string, arguments map[string]any, timeout time.Duration) (*mcp.CallToolResult, error) {
	// 🛡️ ACTIVE-CALL TRACKING: PROXY-L2 — increment the counter on the server
	// snapshot BEFORE acquiring the session, so the health monitor / EvictLRU
	// (which skip servers with ActiveCalls > 0) cannot tear this server down in
	// the window between the lookup and dispatch.
	m.mu.RLock()
	srv := m.Servers[serverName]
	m.mu.RUnlock()
	if srv != nil {
		srv.ActiveCalls.Add(1)
		defer srv.ActiveCalls.Add(-1)
	}

	sess, ok := m.GetServerSession(serverName)
	if !ok {
		return nil, fmt.Errorf("server %s not running", serverName)
	}

	// Decouple the parent transport DEADLINE (AI agent → orchestrator, typically
	// shorter than long-running tools) from this call, but still propagate parent
	// CANCELLATION so a disconnected/aborted agent aborts the in-flight call rather
	// than leaving it running to the full timeout. context.WithoutCancel preserves
	// values (correlation context); AfterFunc re-propagates cancellation. (PROXY-H2)
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	stopPropagation := context.AfterFunc(ctx, cancel)
	defer stopPropagation()

	telemetry.LifecycleEvents.BackpressurePending.Add(1)
	defer telemetry.LifecycleEvents.BackpressurePending.Add(-1)

	// 🛡️ WIRE-BOUNDARY BACKSTOP: strip reserved params-level metadata before it crosses
	// to a sub-server — the single point every dispatch path passes through.
	stripReservedWireKeys(arguments)

	start := time.Now()
	res, err := sess.CallTool(callCtx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: arguments,
	})
	latency := time.Since(start)

	// Persist the request/response payload for Time Travel Debugging (Phase 2)
	go func(r *mcp.CallToolResult, e error, args map[string]any) {
		errStr := ""
		if e != nil {
			errStr = e.Error()
		}
		db.FlushProxyCall(m.Store, db.ProxyCallRecord{
			URN:       fmt.Sprintf("%s:%s", serverName, toolName),
			Arguments: args,
			Response:  r,
			Error:     errStr,
			LatencyMs: latency.Milliseconds(),
			Timestamp: time.Now().Unix(),
		})
	}(res, err, arguments)

	if err == nil {
		m.mu.Lock()
		if s, ok := m.Servers[serverName]; ok {
			s.LastUsed = time.Now()
			s.TotalCalls++
			s.LastLatency = latency
			s.ConsecutiveErrors = 0
			s.Status = StatusHealthy // PROXY-M1: a successful call clears any prior CRASHED/transient state
		}
		m.mu.Unlock()
		m.EvictLRU(serverName)
	} else {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timeout") {
			telemetry.LifecycleEvents.BackpressureReject.Add(1)
			// PROXY-M1: a timeout is transient backpressure, not a crash. Bump the
			// soft error counter; only escalate to CRASHED on persistent timeouts so
			// one slow call can't leave a healthy server permanently "crashed".
			m.mu.Lock()
			if s, ok := m.Servers[serverName]; ok {
				s.ConsecutiveErrors++
				s.LastFailure = time.Now()
				if s.ConsecutiveErrors >= 3 {
					s.Status = StatusCrashed
				}
			}
			m.mu.Unlock()
			return nil, err
		}

		// 🛡️ EOF DIAGNOSTIC ENRICHMENT: When the stdio pipe closes unexpectedly,
		// capture process state to determine if the sub-server was OOM-killed,
		// health-monitor restarted, or crashed independently.
		if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "EOF") {
			telemetry.ErrorTaxonomy.PipeError.Add(1)
			diag := m.diagnoseEOF(serverName)
			slog.Error("proxy: EOF detected on sub-server pipe",
				"component", "proxy",
				"server", serverName,
				"tool", toolName,
				"latency_ms", latency.Milliseconds(),
				"diagnostic", diag,
			)
			m.markFailure(serverName)
			return nil, fmt.Errorf("%s EOF (%s): %w", serverName, diag, err)
		}

		m.markFailure(serverName)
		return nil, err
	}

	return res, nil
}

// diagnoseEOF captures forensic data when an EOF is detected on a sub-server's
// stdio pipe. Returns a human-readable diagnostic string including PID, process
// state, exit status, and server lifecycle status.
func (m *WarmRegistry) diagnoseEOF(serverName string) string {
	m.mu.RLock()
	srv, ok := m.Servers[serverName]
	if !ok {
		m.mu.RUnlock()
		return "server not in registry"
	}
	status := srv.Status
	pid := srv.LastKnownPID
	proc := srv.Process
	activeCalls := srv.ActiveCalls.Load()
	m.mu.RUnlock()

	var processState string
	exitInfo := ""
	if proc != nil && proc.Process != nil {
		if proc.ProcessState != nil {
			// Process has exited — capture exit status
			processState = "DEAD"
			exitInfo = fmt.Sprintf(", exit: %s", proc.ProcessState.String())
		} else {
			// Check if still alive via signal 0
			if err := proc.Process.Signal(syscall.Signal(0)); err != nil {
				processState = "DEAD"
			} else {
				processState = "ALIVE"
			}
		}
	} else {
		processState = "NO_PROCESS"
	}

	return fmt.Sprintf("pid=%d %s, status=%s, active_calls=%d%s", pid, processState, status, activeCalls, exitInfo)
}

// GetFailedServers returns the names of sub-servers that are in a non-healthy state.
// This is used by the health endpoint to report degraded status when boot is complete
// but some sub-servers failed to start or have crashed.
func (m *WarmRegistry) GetFailedServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var failed []string
	for name, srv := range m.Servers {
		switch srv.Status {
		case StatusCrashed, StatusOffline, StatusDisconnected:
			failed = append(failed, name)
		}
	}
	return failed
}

// AuditGlobalRegistry verifies that no internally managed component is holding a reference to os.Stdout.
func (m *WarmRegistry) AuditGlobalRegistry() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	safeWriter := m.LogSink
	if safeWriter == nil {
		logPath := m.Config.LogPath
		if logPath == "" {
			logPath = config.DefaultLogPath()
		}
		safeWriter = util.OpenHardenedLogFile(logPath)
	}

	for name, srv := range m.Servers {
		if srv.Process != nil {
			if srv.Process.Stdout == os.Stdout {
				slog.Error("server process holding os.stdout leak", "component", "manager", "server", name)
				srv.Process.Stdout = safeWriter
			}
			if srv.Process.Stderr == os.Stdout {
				slog.Error("server stderr holding os.stdout leak", "component", "manager", "server", name)
				srv.Process.Stderr = safeWriter
			}
		}
	}
}
