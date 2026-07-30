package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/handler"
	"github.com/maccavelli/mcp-server-magictools/internal/intelligence"
	"github.com/maccavelli/mcp-server-magictools/internal/ipc"
	"github.com/maccavelli/mcp-server-magictools/internal/llm"
	"github.com/maccavelli/mcp-server-magictools/internal/logging"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"
	"github.com/maccavelli/mcplib"
)

var (
	realStdout = os.Stdout
	realStdin  = os.Stdin
	exitFunc   = os.Exit
)

var (
	isInitialized      atomic.Bool  // 🛡️ HANDSHAKE STATE: Tracks if notifications/initialized was received
	activeSessionCount atomic.Int64 // 🛡️ MULTI-WINDOW: Tracks concurrent IDE session count for diagnostics
)

// OrchestratorApp encapsulates the magictools runtime environment and lifecycle.
type OrchestratorApp struct {
	store    *db.Store
	cfg      *config.Config
	reg      *client.WarmRegistry
	server   *mcp.Server
	levelVar *slog.LevelVar
	mcpLog   *logging.McpLogHandler

	eg     *errgroup.Group
	egCtx  context.Context
	cancel context.CancelFunc

	httpSrv *http.Server // primary IPC HTTP server — used for graceful shutdown
	ideSrv  *http.Server // IDE-facing HTTP server — drained first on shutdown for close events

	bootOnce       sync.Once
	hydratorSignal chan struct{}        // 🛡️ HYDRATOR GATE: Signaled when boot or sync actions complete
	recallClient   *mcplib.RecallClient // 🛡️ RECALL CLIENT: HTTP streaming connection to mcp-server-recall

	activePipelineEnabled atomic.Bool // 🛡️ PIPELINE GATE: True when recall + brainstorm + go-modernizer are all online

	ProcessStartTime time.Time // 🛡️ TELEMETRY: Anchors the boot duration calculation at the physical binary entry point

	llmPool    *llm.Pool                    // Shared LLM backplane pool (nil if disabled)
	llmEnabled atomic.Bool                  // Runtime toggle — false on port conflict
	handler    *handler.OrchestratorHandler // 🛡️ HANDLER REF: Stored for post-init LLM pool wiring

	udpSrv  *telemetry.UDPServer // Phase 1: UDP telemetry server for dashboard
	watcher *config.Watcher      // config hot-reload watcher; Reload() invoked on SIGHUP in service mode

	stopOnce sync.Once // ROB-2: Prevents double-cancel/double-disconnect in Stop()
}

func main() {
	processStartTime := time.Now()
	// 🛡️ [BOILERPLATE] Standard I/O Redirection
	realStdin = os.Stdin

	// 🛡️ RESOURCE CONTROL: Set soft memory limit to 2048MiB (2GB) allowing massive parallel arrays
	debug.SetMemoryLimit(2048 << 20)
	// NOTE: GC is disabled inside ServeFunc (not here) so CLI commands like
	// 'db wipe', 'configure', and 'init' retain normal GC behavior.

	// 1. Wire up Cobra callbacks
	ServeFunc = func(cmdCtx context.Context, configPath, dbPath, logPath, logLevel string, noOptimize, debugFlag bool) error {
		// Update global state from Cobra flags
		realStdout = RealStdout
		// 🛡️ GC PAUSE: Disable GC during the boot-time mass allocation spike.
		// Scoped to serve only — CLI commands retain normal GC behavior.
		debug.SetGCPercent(-1)
		baseCtx, baseCancel := context.WithCancel(cmdCtx)
		defer baseCancel()
		// A1: signal ownership depends on mode.
		//   - Service mode: setupGracefulShutdown (installed in startHTTPMode) is
		//     the SOLE signal subscriber, so the ordered drain is never raced by a
		//     second context-cancel path. Use a plain cancel context here; SIGHUP
		//     there means config-reload, not shutdown.
		//   - Interactive/stdio mode: a signal-cancel context provides graceful
		//     shutdown on SIGINT/SIGTERM/SIGHUP.
		var ctx context.Context
		var cancel context.CancelFunc
		if config.IsServiceMode() {
			ctx, cancel = context.WithCancel(baseCtx)
		} else {
			ctx, cancel = makeSignalContext(baseCtx)
		}
		defer cancel()

		// Infrastructure Preparation
		util.TraceFunc(ctx, "step", "enforceResourceLimits")
		enforceResourceLimits()

		// InitialProcessSweep runs here

		app, err := NewOrchestratorApp(ctx, cancel, configPath, dbPath, logPath, logLevel, noOptimize, debugFlag, processStartTime)
		if err != nil {
			return err
		}

		// 🛡️ SINGLETON GATE: InitialProcessSweep runs AFTER AcquireLock (inside NewOrchestratorApp).
		// In stdio mode, the DB flock ensures only one instance reaches this point.
		// A second IDE window will fail on AcquireLock above and never sweep.
		util.TraceFunc(ctx, "step", "InitialProcessSweep")
		InitialProcessSweep()

		if handled, preErr := app.PreDrive(false); handled {
			return preErr
		}

		return app.Start()
	}

	DBWipeFunc = func(dbPath string) error {
		// Minimal setup for database wipe
		store, err := db.NewStore(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer store.Close()

		slog.Warn("CLI: wipe command detected, wiping database and search index...")
		if err := store.WipeAll(); err != nil {
			return fmt.Errorf("failed to wipe database: %w", err)
		}
		fmt.Fprintf(os.Stderr, "SUCCESS: Database and search index have been completely wiped.\n")
		return nil
	}

	DBSyncFunc = func(dbPath string) error {
		store, err := db.NewStore(dbPath)
		if err != nil {
			if strings.Contains(err.Error(), "lock") || strings.Contains(err.Error(), "resource temporarily unavailable") {
				return fmt.Errorf("database is locked: please sleep or stop the magictools orchestrator daemon before running 'db sync'")
			}
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer store.Close()

		slog.Warn("CLI: sync command detected, forcing BadgerDB source-of-truth reindex onto Bleve...")
		if err := store.ReindexAllTools(); err != nil {
			return fmt.Errorf("failed to sync bleve index: %w", err)
		}
		fmt.Fprintf(os.Stderr, "SUCCESS: Search index has been perfectly re-aligned to the BadgerDB source of truth.\n")
		return nil
	}

	// 2. Execute Cobra
	Execute()
}

// NewOrchestratorApp initializes a new OrchestratorApp, establishing the database, configuration, and logging lifecycle.
func NewOrchestratorApp(ctx context.Context, cancel context.CancelFunc, configPath, dbPath, logPath, logLevel string, noOptimize, debug bool, startTime time.Time) (*OrchestratorApp, error) {
	util.TraceFunc(ctx, "config", configPath, "db", dbPath, "debug", debug)

	// Create a master context tree using errgroup
	eg, egCtx := errgroup.WithContext(ctx)

	store, cfg, err := loadConfigAndDB(configPath, dbPath, logPath, noOptimize, debug)
	if err != nil {
		return nil, err
	}

	app := &OrchestratorApp{
		store:            store,
		cfg:              cfg,
		eg:               eg,
		egCtx:            egCtx,
		cancel:           cancel,
		hydratorSignal:   make(chan struct{}, 1),
		ProcessStartTime: startTime,
	}

	// 🛡️ LOG LEVEL DISCOVERY: Flag -> Config -> Default (DEBUG)
	var initialLevel slog.Level
	if debug {
		initialLevel = util.LevelTrace
	} else if logLevel != "" {
		initialLevel = logging.ParseLogLevel(logLevel)
	} else if cfg.LogLevel != "" {
		initialLevel = logging.ParseLogLevel(cfg.LogLevel)
	} else {
		initialLevel = slog.LevelDebug
	}

	app.levelVar = new(slog.LevelVar)
	app.levelVar.Set(initialLevel)

	// Single instantiation of MCP Log Bridge. At this point session is nil, so it acts as a no-op.
	app.mcpLog = logging.NewMcpLogHandler(app.levelVar)

	telemetry.MustInitializeRingBuffer(filepath.Join(config.DefaultCacheDir(), "telemetry.ring"))

	// 🛡️ UNIFIED LOGGING SETUP: Done exactly once.
	logging.SetupGlobalLogger(app.store, app.cfg.LogPath, app.cfg.GetLogFormat(), app.levelVar, noOptimize, debug, realStdout, app.mcpLog)

	// 🛡️ NATIVE VECTOR ENGINE: Mount the pure Go-HNSW Index (AFTER logger is configured)
	if err := vector.InitGlobalEngine(dbPath, cfg); err != nil {
		slog.Warn("Failed to initialize HNSW vector engine natively", "error", err)
	}

	// 🛡️ TELEMETRY BRIDGE: Wire vector stats callback to avoid circular imports
	telemetry.VectorStatsFunc = func() telemetry.VectorStats {
		e := vector.GetEngine()
		if e == nil {
			return telemetry.VectorStats{}
		}
		vs := telemetry.VectorStats{
			Enabled:                e.VectorEnabled(),
			Provider:               cfg.Intelligence.EmbeddingProvider,
			Model:                  cfg.Intelligence.EmbeddingModel,
			Dims:                   cfg.Intelligence.EmbeddingDimensionality,
			GraphNodes:             e.Len(),
			NeedsHydration:         e.RequiresHydration(),
			VectorWins:             telemetry.SearchMetrics.VectorWins.Load(),
			LexicalWins:            telemetry.SearchMetrics.LexicalWins.Load(),
			TotalSearches:          telemetry.SearchMetrics.TotalSearches.Load(),
			VectorSearches:         telemetry.SearchMetrics.VectorSearches.Load(),
			AlignSearchInvocations: telemetry.SearchMetrics.AlignSearchInvocations.Load(),
			LexicalSearches:        telemetry.SearchMetrics.LexicalSearches.Load(),
		}
		if vs.TotalSearches > 0 {
			vs.AvgLatencyMs = telemetry.SearchMetrics.TotalLatencyMs.Load() / vs.TotalSearches
		}
		return vs
	}

	pidDir := filepath.Join(dbPath, "pids")
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		slog.Warn("Failed to create PID directory", "path", pidDir, "error", err)
	}
	app.reg = client.NewWarmRegistry(pidDir, app.store, app.cfg)
	app.reg.LifecycleCtx = app.egCtx

	return app, nil
}

// PreDrive prepares the registry state by pruning orphaned processes before orchestrator bootstrapping.
func (app *OrchestratorApp) PreDrive(clearDB bool) (bool, error) {
	util.TraceFunc(app.egCtx, "step", "PruneOrphans")
	app.reg.PruneOrphans()

	// Harden PreDrive: Cleanup persistent orphaned UDS sockets left by hard crashes
	if runtime.GOOS != "windows" {
		sockPath := ipc.SocketPath()
		// Probe if the primary socket is actually dead before ripping it out
		if conn, err := net.DialTimeout("unix", sockPath, 100*time.Millisecond); err != nil {
			_ = os.Remove(sockPath)
		} else {
			conn.Close()
		}
	}

	// Harden PreDrive: Cleanup stale DB lockfiles from dirty shutdowns
	if app.cfg != nil && app.cfg.DBPath != "" {
		if clearDB {
			slog.Info("PreDrive: wiping vector database", "path", app.cfg.DBPath)
			_ = os.RemoveAll(app.cfg.DBPath)
		} else {
			// Wipe the fallback application-level lock just in case the OS didn't release it
			_ = os.Remove(filepath.Join(app.cfg.DBPath, "LOCK.magic"))
		}

		// Prune any stale PID files for sub-servers
		pidDir := filepath.Join(app.cfg.DBPath, "pids")
		if entries, err := os.ReadDir(pidDir); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".pid") {
					_ = os.Remove(filepath.Join(pidDir, entry.Name()))
				}
			}
		}
	}

	return false, nil
}

// Stop gracefully terminates all background workers and unbinds the MCP registry sockets.
// ROB-2: Wrapped in sync.Once to prevent double-cancel/double-disconnect races
// between the deferred Stop() in Start() and the signal handler in setupGracefulShutdown().
func (app *OrchestratorApp) Stop() {
	app.stopOnce.Do(func() {
		// HARD-36 (REV-2): Unconditionally drain sub-servers before cancel().
		// DisconnectServer is idempotent (connector.go:25 s.Session == nil guard),
		// so double-drain from both signal handler and Stop() is safe.
		// Without this, a service-mode errgroup error exit (without signal)
		// would skip drain entirely, orphaning sub-servers.
		app.reg.DisconnectAll()

		app.cancel()
		if waitErr := app.eg.Wait(); waitErr != nil && !errors.Is(waitErr, context.Canceled) {
			slog.Error("Background shutdown error", "error", waitErr)
		}

		if app.recallClient != nil {
			app.recallClient.Close()
		}

		// 🛡️ SERIALIZATION: Flush the spatial-node relationships safely locally
		if e := vector.GetEngine(); e != nil {
			if err := e.Save(); err != nil {
				slog.Warn("failed to save vector database persistently", "error", err)
			}
		}

		// Phase 1: Stop UDP telemetry server
		if app.udpSrv != nil {
			app.udpSrv.Stop()
		}

		// A4: Remove the diagnostic state file on EVERY exit path (crash/errgroup
		// error included), not just the signal handler — otherwise a stale file
		// lingers and 'service status' must fall back to a PID liveness probe.
		statePath := filepath.Join(config.DefaultConfigDir(), "service.state")
		if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove service state file", "path", statePath, "error", err)
		}

		app.store.Close()
	})
}

// Start initializes the hot-loading server watchers and binds the primary JSON-RPC 2.0 streaming pipeline.
// In service mode (MCP_SERVICE_MODE=true), it binds to an HTTP listener instead of stdio.
func (app *OrchestratorApp) Start() error {
	defer app.Stop()

	// 🛡️ PRE-INJECT RECALL: Instantiated early to guarantee memory pointer availability for handlers
	recallURL := config.ResolveRecallURL()
	if recallURL != "" {
		app.recallClient = mcplib.NewRecallClient(recallURL, "magictools")
	}

	// 🛡️ [BOILERPLATE] Standard MCP Tool Registration
	h := handler.NewHandler(app.store, app.reg, app.cfg)
	h.SetLogLevel(app.levelVar)
	h.RecallClient = app.recallClient              // 🛡️ RECALL WIRING: Direct HTTP client for PM pipeline tools
	h.PipelineEnabled = &app.activePipelineEnabled // 🛡️ PIPELINE WIRING: Conditional enablement flag
	h.HydratorSignal = app.hydratorSignal          // 🛡️ HYDRATOR WIRING: Daemon continuation channel
	h.Responses.SetStore(app.store)                // ── Phase 5: L2 Persistent Cache Wire-up ──
	app.handler = h                                // 🛡️ HANDLER REF: Stored for post-init LLM pool wiring

	// 🛡️ HOT LOADING ACTIVATION: Start the config watcher
	w := config.NewWatcher(app.cfg.Viper(), app.cfg, h)
	app.watcher = w // A2: exposed for SIGHUP-triggered reload in service mode
	w.Start()
	defer w.Stop()

	app.server = mcp.NewServer(
		&mcp.Implementation{Name: "mcp-server-magictools", Version: Version},
		&mcp.ServerOptions{
			Logger: slog.Default(),
			InitializedHandler: func(handlerCtx context.Context, req *mcp.InitializedRequest) {
				sessionID := req.Session.ID()
				if sessionID == "" {
					sessionID = "active-session"
				}
				util.TraceFunc(app.egCtx, "event", "on_initialized", "session_id", sessionID)
				slog.Info("orchestrator: Handshake successful", "session_id", sessionID)

				// Instantiates the session-scoped cache
				app.handler.ActiveSessions.Store(sessionID, util.NewS3FIFOCache[string, *mcp.Tool](20))

				// 🛡️ ATOMIC LOGGING ACTIVATION
				// In service mode, AddSession handles per-session routing.
				// In stdio mode, SetSession handles the single-session case.
				if config.IsServiceMode() {
					app.mcpLog.AddSession(req.Session)
					count := activeSessionCount.Add(1)
					slog.Info("session initialized",
						"session_id", sessionID,
						"active_sessions", count)
					// Eagerly clean up mcpLog routing when the session closes
					// (via SessionTimeout expiry or explicit DELETE).
					// ServerSession.Wait() blocks until the connection is closed.
					session := req.Session
					go func() {
						_ = session.Wait()
						app.mcpLog.RemoveSession(sessionID)
						app.handler.ActiveSessions.Delete(sessionID)
						remaining := activeSessionCount.Add(-1)
						slog.Info("session expired: mcpLog entry removed",
							"session_id", sessionID,
							"remaining_sessions", remaining)
					}()
				} else {
					app.mcpLog.SetSession(req.Session)
				}
				isInitialized.Store(true)

				// In stdio mode, boot is triggered on first handshake.
				// In service mode, boot runs eagerly before the listener opens.
				if !config.IsServiceMode() {
					app.bootOnce.Do(func() {
						app.startBackgroundWorkers()
						app.executeBootSequence()
					})
				}
			},
		},
	)

	h.Register(app.server)
	h.RegisterPipelineTools(app.server) // 🛡️ PM TOOLS: execute_pipeline, validate_pipeline_step, cross_server_quality_gate
	h.RegisterPrompts(app.server)       // 🛡️ PROMPT WIRING: pipeline-start

	// 🛡️ TRANSPORT FORK: Service mode uses HTTP, stdio mode uses IOTransport
	if config.IsServiceMode() {
		return app.startHTTPMode()
	}
	return app.startStdioMode()
}

// startStdioMode runs the orchestrator in traditional stdio mode (IDE-managed child process).
func (app *OrchestratorApp) startStdioMode() error {
	pipeline := mcplib.NewStdioPipeline(realStdin, realStdout, app.cancel)

	go func() {
		<-app.egCtx.Done()
		// HARD-35: Bounded flush with write deadline prevents blocking on
		// a dead IDE stdin pipe. The 5s flush timeout is sufficient for any
		// remaining buffered JSON-RPC frames (typically <1KB).
		flushDone := make(chan struct{})
		go func() {
			_ = pipeline.Flush()
			close(flushDone)
		}()
		select {
		case <-flushDone:
		case <-time.After(5 * time.Second):
			slog.Warn("stdio pipeline flush timed out, exiting anyway")
		}
		exitFunc(2)
	}()

	// 🛡️ [DEBUG] WIRE TAP
	tapIn := &logging.WireTapReader{Rc: pipeline.Reader, Ctx: app.egCtx}
	tapOut := &logging.WireTapWriter{Wc: pipeline.Writer, Ctx: app.egCtx}

	slog.Info("orchestrator: starting I/O loop with IDE", "pid", os.Getpid(), "mode", "stdio")

	errChan := make(chan error, 1)
	go func() {
		errChan <- app.server.Run(app.egCtx, &mcp.IOTransport{
			Reader: tapIn,
			Writer: tapOut,
		})
	}()

	runErr := <-errChan
	if runErr != nil {
		slog.Error("IOTransport run aborted", "error", runErr)
	}
	app.cancel() // Explicitly trigger the errgroup teardown

	_ = pipeline.Flush()
	return nil
}

// startHTTPMode runs the orchestrator as a standalone HTTP service exposing the
// MCP Streamable HTTP transport via Polymorphic IPC (UDS/Named Pipes and Dual-Stack TCP).
func (app *OrchestratorApp) startHTTPMode() error {
	slog.Info("orchestrator: service mode enabled, booting eagerly before listener")

	// A1: install the signal handler BEFORE eager boot so a SIGTERM/SIGHUP during
	// the boot sequence (which spawns sub-servers) is handled by the ordered drain
	// / reload path rather than the default terminate disposition.
	app.setupGracefulShutdown()

	app.bootOnce.Do(func() {
		app.startBackgroundWorkers()
		app.executeBootSequence()
	})

	// 1. Primary IPC (UDS or Named Pipe)
	primaryListener, err := ipc.ListenPrimary()
	if err != nil {
		slog.Warn("failed to bind primary IPC socket", "error", err)
	} else {
		slog.Info("orchestrator: primary IPC bound")
	}

	// 2. Dual-Stack TCP Fallback (random ports for internal proxy IPC)
	tcpListeners, port4, port6, err := ipc.ListenDualStackTCP()
	if err != nil && len(tcpListeners) == 0 {
		slog.Warn("failed to bind TCP fallback ports", "error", err)
	}

	token := ipc.GenerateToken()
	llmToken := ipc.GenerateToken() // Scoped token for LLM endpoints
	auth := ipc.AuthFile{
		Port:  port4,
		Port6: port6,
		Token: token,
	}

	// ── Shared LLM Backplane Initialization ─────────────────────────────
	var llmListener net.Listener
	var llmSrv *http.Server
	if app.cfg.Intelligence.SharedLLMEnabled && app.cfg.Intelligence.Provider != "" {
		pool, poolErr := llm.NewPool(app.cfg)
		if poolErr != nil {
			slog.Error("llm backplane disabled: pool init failed",
				"component", "llm_backplane", "error", poolErr)
		} else {
			app.llmPool = pool

			// Wire pool into handler for self_check telemetry surfacing
			if app.handler != nil {
				app.handler.LLMPool = pool
			}

			// Resolve LLM bind address from config port
			llmCfgAddr := ""
			if app.cfg.Intelligence.LLMPort > 0 {
				llmCfgAddr = fmt.Sprintf("%d", app.cfg.Intelligence.LLMPort)
			}
			llmAddr := config.ResolveLLMBindAddr(llmCfgAddr)

			// LLM mux: scoped token auth, request body limit
			llmMux := http.NewServeMux()
			llmMux.HandleFunc("POST /llm/generate", pool.HandleGenerate)
			llmMux.HandleFunc("POST /llm/generate-thinking", pool.HandleThinking)
			llmMux.HandleFunc("GET /llm/status", pool.HandleStatus)

			var llmHandler http.Handler = llmMux
			llmHandler = app.llmAuthMiddleware(llmHandler, llmToken)

			llmListener, err = net.Listen("tcp", llmAddr)
			if err != nil {
				slog.Error("llm backplane disabled: port bind failed",
					"component", "llm_backplane", "addr", llmAddr, "error", err)
				app.llmPool = nil
			} else {
				slog.Info("orchestrator: LLM backplane listener bound", "addr", llmAddr)
				app.llmEnabled.Store(true)

				// ── Phase 4: Token Quotas ──
				// Load persistent quotas from BadgerDB on boot and configure the flusher.
				totalTokens, serverTokens := db.LoadTokenQuotas(app.store)
				pool.RestoreTokens(totalTokens, serverTokens)

				app.eg.Go(func() error {
					ticker := time.NewTicker(60 * time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-app.egCtx.Done():
							// Final flush before shutdown
							m := pool.Metrics()
							db.FlushTokenQuotas(app.store, m.TotalTokens, m.PerServer)
							return nil
						case <-ticker.C:
							m := pool.Metrics()
							db.FlushTokenQuotas(app.store, m.TotalTokens, m.PerServer)
						}
					}
				})

				// Publish LLM connection details to auth file for diagnostics
				auth.LLMPort = llmListener.Addr().(*net.TCPAddr).Port
				auth.LLMToken = llmToken

				// Configure WarmRegistry with LLM push details
				app.reg.LLMEnabled.Store(true)
				app.reg.LLMAddr = llmAddr
				app.reg.LLMToken = llmToken
				app.reg.LLMMetrics = &llm.PoolMetricsAdapter{Pool: pool}

				llmSrv = &http.Server{
					Handler:           llmHandler,
					ReadHeaderTimeout: 10 * time.Second,
					WriteTimeout:      0, // unbounded — context deadlines provide timeout
					IdleTimeout:       300 * time.Second,
				}
			}
		}
	} else {
		slog.Info("orchestrator: shared LLM backplane disabled (config)")
	}

	if err := ipc.WriteAuthFileAtomic(auth); err != nil {
		slog.Warn("failed to write atomic TCP fallback auth token", "error", err)
	} else {
		slog.Info("orchestrator: TCP fallback ready", "port4", port4, "port6", port6)
	}

	// 🛡️ IDE SESSION TIMEOUT
	// Default 2h — the SDK's POST ref-counting pauses the timer during active
	// tool calls, so only true idle time counts. 2 hours covers typical IDE
	// idle periods (lunch, meetings) while ensuring orphaned sessions from
	// crashed IDEs or disconnected SSH are reaped promptly.
	// Override via MCP_SESSION_TIMEOUT_SECONDS for testing or constrained envs.
	ideSessionTimeout := 2 * time.Hour
	if v := os.Getenv("MCP_SESSION_TIMEOUT_SECONDS"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			ideSessionTimeout = time.Duration(secs) * time.Second
		}
	}
	slog.Info("orchestrator: IDE session timeout configured",
		"timeout", ideSessionTimeout)

	// 🛡️ STREAM RESUMPTION: MemoryEventStore enables Last-Event-ID based
	// stream resumption after transient TCP disconnects. Events are buffered
	// in-memory (10 MiB default with automatic LRU purge). The store is
	// thread-safe and session-scoped, shared across both IDE and IPC handlers.
	eventStore := mcp.NewMemoryEventStore(nil)

	// IDE handler: SSE response mode enables close events with retry hints,
	// stream resumption via EventStore, and server-initiated notifications.
	// Session timeout ensures stale sessions are reaped after disconnect.
	// POST ref-counting pauses the timer during active tool calls.
	ideMcpHandler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return app.server },
		&mcp.StreamableHTTPOptions{
			SessionTimeout: ideSessionTimeout,
			JSONResponse:   false,
			EventStore:     eventStore,
		},
	)

	// IPC handler: JSON response mode for internal callers (proxy, CLI).
	// EventStore enables resumption for long-running proxy tool calls.
	// SessionTimeout zero-value is treated as disabled by the SDK.
	ipcMcpHandler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return app.server },
		&mcp.StreamableHTTPOptions{
			JSONResponse: true,
			EventStore:   eventStore,
		},
	)

	// IDE-facing mux: readiness gate only, no token auth (IDEs connect via serverUrl)
	ideMux := http.NewServeMux()
	ideMux.Handle("/mcp", app.readinessMiddleware(app.ideTelemetryMiddleware(ideMcpHandler)))
	ideMux.HandleFunc("/health", app.healthHandler)

	// Internal IPC mux: readiness + token auth + rate limiting (for proxy/internal callers)
	ipcMux := http.NewServeMux()
	authedIPC := app.polymorphicAuthMiddleware(app.readinessMiddleware(ipcMcpHandler), token)
	// 🛡️ HARDENING §4: Application-layer DoS protection — 100 req/s sustained, 200 burst.
	// Defense-in-depth for IPC only; IDE endpoint is exempt (hangResponse conflicts).
	ipcLimiter := rate.NewLimiter(100, 200)
	ipcMux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ipcLimiter.Allow() {
			telemetry.IPCSessionCounters.RateLimitRejects.Add(1)
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		authedIPC.ServeHTTP(w, r)
	}))
	ipcMux.HandleFunc("/health", app.healthHandler)

	// 3. IDE-facing HTTP listener on MCP_ENDPOINT_IDE_PORT (default localhost:48080)
	ideAddr := config.ResolveBindAddr("")
	var ideListener net.Listener
	ideListener, err = net.Listen("tcp", ideAddr)
	if err != nil {
		slog.Error("failed to bind IDE HTTP listener", "addr", ideAddr, "error", err)
	} else {
		slog.Info("orchestrator: IDE HTTP listener bound", "addr", ideAddr)
	}

	app.writeServiceState(ideAddr)

	// errChan sized for: primary IPC + IDE listener + TCP fallback listeners + LLM listener
	errChan := make(chan error, len(tcpListeners)+4)

	if llmListener != nil && llmSrv != nil {
		go func() {
			errChan <- llmSrv.Serve(llmListener)
		}()
	}

	if primaryListener != nil {
		ipcSrv := &http.Server{
			Handler:           ipcMux,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      0, // SDK context deadlines provide timeout; non-zero kills hangResponse
			IdleTimeout:       300 * time.Second,
		}
		app.httpSrv = ipcSrv
		go func() {
			errChan <- ipcSrv.Serve(primaryListener)
		}()
	}

	if ideListener != nil {
		app.ideSrv = &http.Server{
			Handler:           ideMux,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       0, // SDK hangResponse keeps POST open until tool completes; non-zero causes truncated JSON
			IdleTimeout:       0, // SSE standalone streams are long-lived by design
		}
		go func() {
			errChan <- app.ideSrv.Serve(ideListener)
		}()
	}

	for _, l := range tcpListeners {
		ln := l
		tcpSrv := &http.Server{
			Handler:           ipcMux,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      0, // SDK context deadlines provide timeout; non-zero kills hangResponse
			IdleTimeout:       300 * time.Second,
		}
		go func() {
			errChan <- tcpSrv.Serve(ln)
		}()
	}

	select {
	case srvErr := <-errChan:
		slog.Error("IPC listener died", "error", srvErr)
		return srvErr
	case <-app.egCtx.Done():
		if primaryListener != nil {
			primaryListener.Close()
		}
		if ideListener != nil {
			ideListener.Close()
		}
		for _, l := range tcpListeners {
			l.Close()
		}
		// Graceful shutdown of LLM backplane
		if llmSrv != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			llmSrv.Shutdown(shutdownCtx)
			shutdownCancel()
		}
		if app.llmPool != nil {
			app.llmPool.Close()
		}
		slog.Info("orchestrator: IPC listeners closed (context cancelled)")
		return nil
	}
}

// responseWriterTracker captures bytes written to compute IDE throughput.
// Upgraded to OutboundSSEFirewall: Mutex serializes chunk frames, and SetWriteDeadline prevents deadlock.
type responseWriterTracker struct {
	http.ResponseWriter
	bytesWritten atomic.Int64
	mu           sync.Mutex
}

func (rw *responseWriterTracker) Write(p []byte) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	// Deadlock Prevention: Guarantee this write terminates
	rc := http.NewResponseController(rw.ResponseWriter)
	rc.SetWriteDeadline(time.Now().Add(5 * time.Second))

	n, err := rw.ResponseWriter.Write(p)
	rw.bytesWritten.Add(int64(n))
	return n, err
}

func (rw *responseWriterTracker) Flush() {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ideTelemetryMiddleware tracks IDE connection lifecycle and total bytes transferred.
// TEL-3: Enhanced with SSE/POST byte split, stream resumption detection, and POST counting.
func (app *OrchestratorApp) ideTelemetryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isSSE := r.Header.Get("Accept") == "text/event-stream"
		if isSSE {
			telemetry.IPCSessionCounters.Connects.Add(1)
			telemetry.IPCSessionCounters.Active.Add(1)
			// TEL-3: Detect SSE stream resumption via Last-Event-ID header.
			if r.Header.Get("Last-Event-ID") != "" {
				telemetry.IPCSessionCounters.SSEResumed.Add(1)
			}
			defer func() {
				telemetry.IPCSessionCounters.Disconnects.Add(1)
				telemetry.IPCSessionCounters.Active.Add(-1)
			}()
		} else {
			// TEL-3: Count non-SSE POST requests.
			telemetry.IPCSessionCounters.PostRequests.Add(1)
		}

		tracker := &responseWriterTracker{ResponseWriter: w}
		next.ServeHTTP(tracker, r)

		written := tracker.bytesWritten.Load()
		if written > 0 {
			telemetry.IPCSessionCounters.TotalBytes.Add(written)
			// TEL-3: Split byte tracking into SSE vs POST.
			if isSSE {
				telemetry.IPCSessionCounters.SSEBytesSent.Add(written)
			} else {
				telemetry.IPCSessionCounters.PostBytesSent.Add(written)
			}
		}
	})
}

// readinessMiddleware returns 503 Service Unavailable until the boot sequence completes.
// TEL-3: Increments Readiness503s counter on rejection.
func (app *OrchestratorApp) readinessMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !app.reg.IsSynced.Load() {
			telemetry.IPCSessionCounters.Readiness503s.Add(1)
			w.Header().Set("Retry-After", "3")
			http.Error(w, "orchestrator booting", http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// polymorphicAuthMiddleware validates Authorization: Bearer <token> on TCP requests.
// UDS and Named Pipes bypass token auth because they are secured via OS-level ACLs.
func (app *OrchestratorApp) polymorphicAuthMiddleware(next http.Handler, token string) http.Handler {
	expected := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		isTCP := strings.HasPrefix(r.RemoteAddr, "127.0.0.1") || strings.HasPrefix(r.RemoteAddr, "[::1]")
		if isTCP {
			if r.Header.Get("Authorization") != expected {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// llmAuthMiddleware validates Bearer token authentication for LLM backplane endpoints.
// Uses a scoped token distinct from the admin IPC token for least-privilege access.
func (app *OrchestratorApp) llmAuthMiddleware(next http.Handler, llmToken string) http.Handler {
	expected := "Bearer " + llmToken
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /llm/status is unauthenticated for sub-server health probes
		if r.URL.Path == "/llm/status" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// healthHandler reports orchestrator status with boot readiness and degraded state detection.
func (app *OrchestratorApp) healthHandler(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	code := http.StatusOK

	if !app.reg.IsSynced.Load() {
		status = "booting"
		code = http.StatusServiceUnavailable
	} else {
		// Check for degraded state: boot completed but some sub-servers failed
		failedServers := app.reg.GetFailedServers()
		if len(failedServers) > 0 {
			status = "degraded"
			// Still return 200 — orchestrator is functional, just partial
		}
	}

	resp := struct {
		Status        string   `json:"status"`
		Uptime        string   `json:"uptime"`
		Servers       int      `json:"servers"`
		BootReady     bool     `json:"boot_ready"`
		Degraded      bool     `json:"degraded"`
		FailedServers []string `json:"failed_servers,omitempty,omitzero"`
	}{
		Status:        status,
		Uptime:        time.Since(app.ProcessStartTime).Truncate(time.Second).String(),
		Servers:       len(app.cfg.GetManagedServers()),
		BootReady:     app.reg.IsSynced.Load(),
		Degraded:      status == "degraded",
		FailedServers: app.reg.GetFailedServers(),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(resp)
}

// serviceState is serialized to ~/.config/mcp-server-magictools/service.state on successful bind.
type serviceState struct {
	PID           int    `json:"pid"`
	Addr          string `json:"addr"`
	Started       string `json:"started"`
	ConfigVersion string `json:"config_version"`
	BinaryPath    string `json:"binary_path"`
}

// writeServiceState writes the service state file for diagnostic and status checks.
func (app *OrchestratorApp) writeServiceState(addr string) {
	binPath, _ := os.Executable()
	state := serviceState{
		PID:           os.Getpid(),
		Addr:          addr,
		Started:       time.Now().UTC().Format(time.RFC3339),
		ConfigVersion: Version,
		BinaryPath:    binPath,
	}

	stateDir := config.DefaultConfigDir()
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		slog.Warn("failed to create state directory", "dir", stateDir, "error", err)
		return
	}

	statePath := filepath.Join(stateDir, "service.state")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		slog.Warn("failed to marshal service state", "error", err)
		return
	}

	// A8: 0600 — the state file carries PID/addr/binary path; keep it owner-only,
	// matching the auth token (the previous 0644 made it world-readable).
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		slog.Warn("failed to write service state", "path", statePath, "error", err)
		return
	}
	slog.Info("service state written", "path", statePath)
}

// setupGracefulShutdown installs SIGTERM/SIGINT handlers that clean up child sub-servers
// and remove the service.state file before exiting. This is critical on Windows where
// TerminateProcess does NOT propagate to child PIDs.
// A 30-second hard timeout is enforced so a misbehaving sub-server cannot block exit.
func (app *OrchestratorApp) setupGracefulShutdown() {
	sigChan := make(chan os.Signal, 1)
	// A1: setupGracefulShutdown is the SOLE signal owner in service mode — the
	// serve context is a plain cancel context (not signal-wired), so the ordered
	// drain below is never raced by a second cancellation path. A2: SIGHUP (Unix
	// only, via reloadSignals) triggers a live config reload instead of shutdown.
	signal.Notify(sigChan, append([]os.Signal{syscall.SIGTERM, syscall.SIGINT}, reloadSignals()...)...)
	go func() {
		for sig := range sigChan {
			if isReloadSignal(sig) {
				slog.Info("received reload signal, reloading configuration", "signal", sig)
				if app.watcher != nil {
					app.watcher.Reload()
				}
				continue
			}
			slog.Info("received shutdown signal", "signal", sig)
			app.performGracefulShutdown()
			return
		}
	}()
}

// performGracefulShutdown runs the ordered drain within a 30s hard deadline:
// gate new requests, drain the IDE server (SSE close hints), cancel sessions,
// drain sub-servers, remove the state file, then shut the telemetry and IPC
// servers. A misbehaving sub-server cannot block exit past the deadline.
func (app *OrchestratorApp) performGracefulShutdown() {
	// Hard deadline: 30 seconds for all sub-servers to terminate gracefully.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// HARD-31: Immediately gate new requests. Any IDE POST arriving after this
	// point receives HTTP 503 with Retry-After, which is the SDK's standard
	// reconnection signal. This prevents new tool calls from starting mid-drain.
	app.reg.IsSynced.Store(false)

	// 1. Drain IDE HTTP server first — sends SSE close events with retry
	// hints to connected IDE clients, giving them a reconnection window.
	// This must happen BEFORE app.cancel() which tears down sessions.
	if app.ideSrv != nil {
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := app.ideSrv.Shutdown(drainCtx); err != nil {
			slog.Warn("IDE HTTP server drain timed out", "error", err)
		}
		drainCancel()
	}

	// 2. Stop accepting new connections
	app.cancel()

	// 3. HARD-31: Gracefully disconnect all sub-servers with drain fence
	// (session close → SIGTERM → SIGKILL escalation), using parallel
	// disconnect bounded by the shutdown context deadline.
	app.reg.DrainAndDisconnectAll(shutdownCtx)

	// 4. Remove service.state file
	statePath := filepath.Join(config.DefaultConfigDir(), "service.state")
	os.Remove(statePath)

	// 5. Close UDP telemetry server
	if app.udpSrv != nil {
		app.udpSrv.Stop()
	}

	// 6. Gracefully shut down the primary IPC HTTP server within the remaining deadline.
	if app.httpSrv != nil {
		if err := app.httpSrv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("IPC HTTP server shutdown timed out or failed", "error", err)
		}
	}

	slog.Info("graceful shutdown complete")
}

func (app *OrchestratorApp) startBackgroundWorkers() {
	util.TraceFunc(app.egCtx, "step", "starting_background_loops")

	// Phase 1: Initialize and start UDP telemetry server
	if udpSrv, err := telemetry.NewUDPServer(); err == nil {
		app.udpSrv = udpSrv
		telemetry.GlobalUDPServer = udpSrv
		udpSrv.Start(func() map[string]any {
			if v := telemetry.LatestSnapshot.Load(); v != nil {
				if snap, ok := v.(map[string]any); ok {
					return snap
				}
			}
			return nil
		})
		slog.Info("UDP telemetry server started", "port", udpSrv.BoundPort())
	} else {
		slog.Warn("UDP telemetry server failed to initialize", "error", err)
	}

	// Phase 4: Hybrid Async-Flush Telemetry
	telemetry.StartMetricsFlusher(app.store.SaveWithTTL)

	app.eg.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				panicAudit(app, "Reconciler", r)
			}
		}()
		app.reg.StartReconciler(app.egCtx)
		return nil
	})
	app.eg.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				panicAudit(app, "HealthMonitor", r)
			}
		}()
		app.reg.StartHealthMonitor(app.egCtx, 10*time.Second)
		return nil
	})

	app.eg.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				panicAudit(app, "BackgroundGC", r)
			}
		}()
		app.store.StartBackgroundGC(app.egCtx, db.ResolveGCInterval())
		return nil
	})
	// 🛡️ WATCHDOG: Skip parent PID monitoring in service mode — parent PID is meaningless
	// for OS-supervised services. SIGTERM from systemd/launchd is the correct lifecycle signal.
	if !config.IsServiceMode() {
		app.eg.Go(func() error {
			defer func() {
				if r := recover(); r != nil {
					panicAudit(app, "watchdog", r)
				}
			}()
			watchdog(app.egCtx, app.cancel)
			return nil
		})
	}
	app.eg.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				panicAudit(app, "zombieReaper", r)
			}
		}()
		setupZombieReaper(app.egCtx)
		return nil
	})
	app.eg.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				panicAudit(app, "watchBinary", r)
			}
		}()
		watchBinary(app.egCtx, app.cancel)
		return nil
	})

	app.eg.Go(func() error {
		// Guard against Go's nil-interface trap: a typed nil *MCPClient passed
		// as RecallMiner creates a non-nil interface value, causing a nil pointer
		// dereference when RecallEnabled() is called on the nil receiver.
		var rc intelligence.RecallMiner
		if app.recallClient != nil {
			rc = app.recallClient
		}
		intelligence.StartHydratorDaemon(app.egCtx, app.store, app.cfg, rc, app.hydratorSignal, app.llmPool)
		return nil
	})

	// 🛡️ SESSION REAPER: Service mode only — reap stale MCP sessions to prevent memory leaks
	if config.IsServiceMode() {
		app.eg.Go(func() error {
			app.mcpLog.StartReaper(app.egCtx)
			return nil
		})
	}

	app.eg.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				panicAudit(app, "RecallMiner", r)
			}
		}()

		// Wait for initial delay to let recall client establish
		select {
		case <-time.After(30 * time.Second):
		case <-app.egCtx.Done():
			return nil
		}

		// Run operational priming and boot snapshot once on activation
		if app.recallClient != nil {
			primeFromRecall(app.egCtx, app.recallClient, app.cfg)

			if app.recallClient.RecallEnabled() {
				app.recallClient.SaveToRecallWithNamespace(app.egCtx, "magictools-diagnostics", "", "server_status", struct {
					Stage     string `json:"stage"`
					BootTime  string `json:"boot_time"`
					Servers   int    `json:"servers"`
					Recall    bool   `json:"recall"`
					DBMetrics any    `json:"db_metrics"`
				}{
					Stage:     "boot_snapshot",
					BootTime:  time.Now().Format(time.RFC3339),
					Servers:   len(app.cfg.GetManagedServers()),
					Recall:    true,
					DBMetrics: app.store.GetMetrics(),
				})
				slog.Info("boot diagnostic snapshot emitted to recall", "component", "recall_diagnostics", "namespace", "server_status")
			}
		}
		return nil
	})
	app.eg.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				panicAudit(app, "cacheMetrics", r)
			}
		}()
		metricsTicker := time.NewTicker(30 * time.Second)
		defer metricsTicker.Stop()

		// 🛡️ HEARTBEAT TELEMETRY: 5-minute periodic snapshot to Recall server_status namespace
		heartbeatTicker := time.NewTicker(5 * time.Minute)
		defer heartbeatTicker.Stop()

		for {
			select {
			case <-app.egCtx.Done():
				return nil
			case <-metricsTicker.C:
				m := app.store.GetMetrics()
				slog.Info("cache metrics", "type", "registry", "hits", m.Hits, "misses", m.Misses, "entries", m.Entries, "db_tools", m.Tools, "db_intel", m.Intel, "bleve_docs", m.BleveDocs)
				if e := vector.GetEngine(); e != nil && e.VectorEnabled() {
					slog.Info("cache metrics", "type", "vector",
						"graph_nodes", e.Len(),
						"needs_hydration", e.RequiresHydration(),
						"vector_wins", telemetry.SearchMetrics.VectorWins.Load(),
						"lexical_wins", telemetry.SearchMetrics.LexicalWins.Load(),
						"total_searches", telemetry.SearchMetrics.TotalSearches.Load(),
					)
				}
			case <-heartbeatTicker.C:
				if app.recallClient == nil || !app.recallClient.RecallEnabled() {
					continue
				}
				// Collect metrics atomically (read-only, no locks held)
				sysStats := telemetry.GetSystemProcessStats()
				dbMetrics := app.store.GetMetrics()

				cachePayload := map[string]any{
					"align_cache": map[string]any{
						"hits":   telemetry.SearchMetrics.AlignCacheHits.Load(),
						"misses": telemetry.SearchMetrics.AlignCacheMisses.Load(),
					},
					"registry_cache": map[string]any{
						"hits":    dbMetrics.Hits,
						"misses":  dbMetrics.Misses,
						"entries": dbMetrics.Entries,
					},
				}

				vectorPayload := map[string]any{}
				if e := vector.GetEngine(); e != nil && e.VectorEnabled() {
					vectorPayload = map[string]any{
						"graph_nodes":     e.Len(),
						"needs_hydration": e.RequiresHydration(),
						"vector_wins":     telemetry.SearchMetrics.VectorWins.Load(),
						"lexical_wins":    telemetry.SearchMetrics.LexicalWins.Load(),
						"total_searches":  telemetry.SearchMetrics.TotalSearches.Load(),
					}
				}

				// Phase 2: Sub-server telemetry aggregation from GlobalTracker
				subServers := map[string]any{}
				for name, metrics := range telemetry.GlobalTracker.GetAll() {
					subServers[name] = map[string]any{
						"calls":           metrics.Calls,
						"total_spinup_ms": metrics.TotalSpinupMs,
						"faults":          metrics.Faults,
						"soft_failures":   metrics.SoftFailures,
					}
				}

				heartbeat := map[string]any{
					"stage":       "heartbeat",
					"timestamp":   time.Now().Format(time.RFC3339),
					"system":      sysStats,
					"cache":       cachePayload,
					"vector":      vectorPayload,
					"db_metrics":  dbMetrics,
					"sub_servers": subServers,
					"latencies": map[string]any{
						"align_tools":    telemetry.MetaLatencies.AlignTools,
						"call_proxy":     telemetry.MetaLatencies.CallProxy,
						"call_proxy_hot": telemetry.MetaLatencies.CallProxyHot,
						"boot_latency":   telemetry.MetaLatencies.BootLatency,
					},
				}

				heartbeatJSON, err := json.Marshal(heartbeat)
				if err != nil {
					slog.Warn("heartbeat: failed to marshal payload", "error", err)
					continue
				}

				// Write with 2-second timeout to prevent blocking if Recall hangs
				heartbeatCtx, heartbeatCancel := context.WithTimeout(context.Background(), 2*time.Second)
				_, hErr := app.recallClient.CallDatabaseToolE(heartbeatCtx, "save_to_recall", map[string]any{
					"server_id":  "magictools",
					"project_id": "global",
					"outcome":    "saved",
					"session_id": fmt.Sprintf("magictools-heartbeat-%d", time.Now().Unix()),
					"namespace":  "server_status",
					"state_data": string(heartbeatJSON),
				})
				heartbeatCancel()
				if hErr != nil {
					slog.Warn("heartbeat: failed to emit telemetry to recall", "error", hErr)
				} else {
					slog.Info("heartbeat telemetry emitted to recall", "component", "telemetry", "namespace", "server_status")
				}
			}
		}
	})

	// 🛡️ RECALL CLIENT: Establish HTTP streaming connection to mcp-server-recall
	app.establishStreamingClient()
}

// establishStreamingClient initializes the recall HTTP MCPClient and spawns its
// reconnect loop as a background worker. Mirrors the brainstorm/go-modernizer pattern.
// If MCP_REC_URL is not configured, this is a safe no-op.
func (app *OrchestratorApp) establishStreamingClient() {
	if app.recallClient == nil {
		slog.Info("MCP_REC_URL not configured — recall HTTP client disabled (standalone mode)", "component", "recall")
		return
	}

	slog.Info("starting external context client background connection", "component", "recall")
	app.eg.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				panicAudit(app, "RecallClient", r)
			}
		}()
		// Start() has its own exponential backoff retry loop (1s → 30s).
		// The 30s boot delay inside Start() prevents premature connection attempts.

		app.recallClient.Start(app.egCtx)
		return nil
	})
}

func (app *OrchestratorApp) executeBootSequence() {
	app.eg.Go(func() error {
		// 🛡️ OPTIMIZATION: GC is already disabled (set in ServeFunc).
		// It will be restored to 100 at line ~1294 after the boot spike completes.
		// No defer needed — the explicit restore at boot completion is the correct lifecycle.

		defer func() {
			if r := recover(); r != nil {
				panicAudit(app, "BootSequence", r)
			}
		}()

		ctx := app.egCtx
		util.TraceFunc(ctx, "event", "boot_sequence_start")
		managed := app.cfg.GetManagedServers()
		if len(managed) == 0 {
			slog.Warn("BootSequence: ZERO managed servers found in config. Orchestrator will run in internal-only mode.", "servers_registry", filepath.Join(config.DefaultConfigDir(), config.ServersConfigFile))
		} else {
			slog.Info("Initiating Fault-Tolerant BootSequence (Background)", "total_servers", len(managed))
		}

		// 🛡️ THE GLOBAL ASYNC TRACKER
		// Guarantees the Boot Duration metric reports the exact moment 100% of all daemon tasks complete.
		var asyncTimelineWg sync.WaitGroup

		// 🛡️ ASYNC WARM BOOT: ReindexAllTools populates the Bleve search cache
		// but is NOT needed for server handshakes — only for align_tools queries.
		// Fire-and-forget so it doesn't block the boot sequence.
		var successCount, offlineCount atomic.Int64
		asyncTimelineWg.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("warm boot panicked", "panic", r)
				}
			}()
			if err := app.reg.Store.ReindexAllTools(); err != nil {
				slog.Warn("WarmBoot: Failed to populate search cache from DB", "error", err)
			} else {
				slog.Info("Warm boot successful: search index cached in-memory", "component", "registry")
			}

			// 🛡️ L1 CACHE WARM: Pre-load all tools from Badger into the RegistryCache
			// so the first align_tools query hits warm L1 instead of falling through to Badger.
			if warmed := app.reg.Store.WarmCache(); warmed > 0 {
				slog.Info("L1 registry cache warmed", "component", "registry", "tools", warmed)
			}
		})

		// 🛡️ CONCURRENT BOOT: All critical servers (including recall) boot in parallel.
		// Sub-servers with recall dependencies handle connectivity independently
		// via their 5-retry/30s-hold circuit breaker logic — no serial Phase 1 needed.
		eg, egCtx := errgroup.WithContext(ctx)
		eg.SetLimit(10)

		// 🛡️ BOOT PARTITIONING: Split servers into critical-path (blocks boot)
		// and deferred (boots in background after IDE handshake).
		// Controlled by deferred_boot directive in servers.yaml.
		critical := slices.Clone(managed)
		critical = slices.DeleteFunc(critical, func(sc config.ServerConfig) bool {
			return sc.DeferredBoot
		})

		deferred := slices.Clone(managed)
		deferred = slices.DeleteFunc(deferred, func(sc config.ServerConfig) bool {
			return !sc.DeferredBoot
		})
		if len(deferred) > 0 {
			slog.Info("BootSequence: deferring non-critical servers to background", "deferred", len(deferred))
		}

		// 🛡️ CONCURRENT BOOT: All critical servers spawn and sync in parallel.
		for _, sc := range critical {
			eg.Go(func() error {
				// Worker isolated recover()
				defer func() {
					if r := recover(); r != nil {
						slog.Error("sub-server lifecycle panicked. skipping...", "component", "lifecycle", "server", sc.Name, "panic", r)
						offlineCount.Add(1)
						app.reg.RequestState(sc.Name, client.StatusOffline)
					}
				}()

				if egCtx.Err() != nil {
					offlineCount.Add(1)
					app.reg.RequestState(sc.Name, client.StatusOffline)
					return nil
				}

				// Handshake Hard-Stop: Use a 60-second timeout for initialize and tools/list
				bootCtx, bootCancel := context.WithTimeout(egCtx, 60*time.Second)
				defer bootCancel()

				slog.Info("BootSequence: syncing server", "name", sc.Name)
				if err := app.reg.SyncServer(bootCtx, sc.Name); err != nil {
					slog.Error("server failed to boot. skipping...", "component", "lifecycle", "server", sc.Name, "error", err)
					offlineCount.Add(1)
					app.reg.RequestState(sc.Name, client.StatusOffline)
				} else {
					successCount.Add(1)
					slog.Info("BootSequence: server ready", "name", sc.Name)
				}
				return nil
			})
		}

		// ✨ NATIVE BOOT: Synchronize the orchestrator's internal tools
		eg.Go(func() error {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("native server lifecycle panicked. skipping...", "component", "lifecycle", "server", "magictools", "panic", r)
				}
			}()

			bootCtx, bootCancel := context.WithTimeout(egCtx, 60*time.Second)
			defer bootCancel()

			slog.Info("BootSequence: syncing native server", "name", "magictools")
			var internalTools []*mcp.Tool
			if err := json.Unmarshal(handler.InternalToolsInventoryJSON, &internalTools); err != nil {
				slog.Error("failed to parse native tools.", "component", "inventory", "error", err)
				return nil
			}

			listRes := mcp.ListToolsResult{Tools: internalTools}
			if _, err := app.reg.SyncNativeTools(bootCtx, &listRes); err != nil {
				slog.Error("native server failed to boot.", "component", "lifecycle", "server", "magictools", "error", err)
			} else {
				slog.Info("BootSequence: native server ready", "name", "magictools")
			}
			return nil
		})

		if err := eg.Wait(); err != nil {
			slog.Warn("BootSequence: encountered errors during background sync", "error", err)
		}
		active := successCount.Load()
		failed := offlineCount.Load()
		skipped := int64(len(managed)) - active - failed

		// 🛡️ SYNC COMPLETION: Signal that the ecosystem is now considered synced.
		app.reg.IsSynced.Store(true)

		// 🛡️ HYDRATOR GATE: Send non-blocking pulse to wake the hydrator daemon
		select {
		case app.hydratorSignal <- struct{}{}:
		default:
		}

		// 🛡️ GC RESTORE: Re-enable GC loops now that the mass allocation sequence is securely flushed
		debug.SetGCPercent(100)

		asyncTimelineWg.Go(func() {
			app.reg.PruneOrphans() // Non-urgent cleanup, runs in background
		})

		// 🛡️ DEFERRED BOOT: Non-critical servers (git, github, glab) boot in
		// background after the main boot completes. They don't block IDE readiness.
		if len(deferred) > 0 {
			asyncTimelineWg.Go(func() {
				var defWg sync.WaitGroup
				for _, sc := range deferred {
					defWg.Go(func() {
						defer func() {
							if r := recover(); r != nil {
								slog.Error("deferred server lifecycle panicked", "server", sc.Name, "panic", r)
								app.reg.RequestState(sc.Name, client.StatusOffline)
							}
						}()
						bootCtx, bootCancel := context.WithTimeout(ctx, 60*time.Second)
						defer bootCancel()
						slog.Info("BootSequence: syncing deferred server", "name", sc.Name)
						if err := app.reg.SyncServer(bootCtx, sc.Name); err != nil {
							slog.Error("deferred server failed to boot", "server", sc.Name, "error", err)
							app.reg.RequestState(sc.Name, client.StatusOffline)
						} else {
							slog.Info("BootSequence: deferred server ready", "name", sc.Name)
						}
					})
				}
				defWg.Wait() // Wait for all deferred servers in this batch
			})
		}

		slog.Info("BootSequence phase complete.",
			"success", active,
			"offline", failed,
			"duration", time.Since(app.ProcessStartTime))

		// 🛡️ AUDIT: Pipe isolation verified. (Routing to Stderr)
		io.WriteString(os.Stderr, "[ORCHESTRATOR] [AUDIT] Pipe isolation verified.\n")
		slog.Info("Boot Complete", "component", "backplane", "active", active, "failed", failed, "skipped", skipped)

		slog.Info("orchestrator: signaling tools/list_changed to IDE")
		util.TraceFunc(ctx, "event", "signaling_sync")
		type emptySchema struct {
			Type string `json:"type"`
		}
		dummy := &mcp.Tool{
			Name:        "__magic_sync_signal__",
			Description: "Internal synchronization signal",
			InputSchema: emptySchema{Type: "object"},
		}
		app.server.AddTool(dummy, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, nil
		})
		app.server.RemoveTools("__magic_sync_signal__")

		// 🛡️ PIPELINE GATE: Final boot verdict — enable the active pipeline
		// only when ALL required servers are online. Must be LAST in boot.
		pipelineReady := app.checkPipelineServers()
		app.activePipelineEnabled.Store(pipelineReady)

		// 🛡️ TIMELINE CLOSURE: Wait for all asynchronous daemon tasks to settle before reporting the exact boot duration.
		go func() {
			asyncTimelineWg.Wait()

			slog.Info("======================================================")
			slog.Info("               ORCHESTRATOR BOOT SUMMARY              ")
			slog.Info("======================================================")

			if app.cfg.Intelligence.Provider != "" && app.cfg.Intelligence.APIKey != "" {
				slog.Info("Hydrator: ENABLED")
			} else {
				slog.Warn("Hydrator: DISABLED (missing provider or API key)")
			}

			if vector.GetEngine() != nil && vector.GetEngine().VectorEnabled() {
				slog.Info("compose_pipeline vector search: ENABLED")
			} else {
				slog.Warn("compose_pipeline vector search: DISABLED")
			}

			if pipelineReady {
				slog.Info("Active code generation pipeline: ENABLED (recall + brainstorm + go-modernizer online)", "component", "pipeline")
			} else {
				slog.Warn("Active code generation pipeline: DISABLED (missing required servers)", "component", "pipeline")
			}

			slog.Info(fmt.Sprintf("Boot duration: %.2f seconds", time.Since(app.ProcessStartTime).Seconds()))
			slog.Info("======================================================")
		}()

		return nil
	})
}

// checkPipelineServers verifies that all 3 required servers (recall,
// brainstorm, go-modernizer) are online. Returns true only when the
// full pipeline is available.
func (app *OrchestratorApp) checkPipelineServers() bool {
	required := []string{"recall", "brainstorm", "go-modernizer"}
	for _, name := range required {
		ss, ok := app.reg.GetServer(name)
		if !ok {
			slog.Warn("Required server not registered", "component", "pipeline", "server", name)
			return false
		}
		if ss.Status != client.StatusReady && ss.Status != client.StatusHealthy {
			slog.Warn("Required server not online", "component", "pipeline", "server", name, "status", ss.Status)
			return false
		}
	}
	return true
}

// panicAudit implements fail-safe crash recovery for background goroutines.
// It writes the crash dump to stderr first (guaranteed visibility), then to
// structured logging, then attempts a best-effort Recall write. The Recall
// write is wrapped in a nested recover() to suppress secondary panics caused
// by OOM or heap corruption during JSON marshaling or HTTP allocation.
func panicAudit(app *OrchestratorApp, funcName string, r any) {
	stack := debug.Stack()

	// 1. Stderr-first: guaranteed visibility even if slog or Recall is broken
	fmt.Fprintf(os.Stderr, "PANIC [%s]: %v\n%s\n", funcName, r, stack)

	// 2. Structured log (may fail under OOM)
	slog.Error("errgroup: panic in background goroutine", "func", funcName, "panic", r)

	// 3. Recall audit (wrapped in nested recovery to suppress secondary panics)
	func() {
		defer func() { recover() }()
		if app.recallClient != nil && app.recallClient.RecallEnabled() {
			crashCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			crashPayload, _ := json.Marshal(map[string]any{
				"panic":     fmt.Sprint(r),
				"stack":     string(stack),
				"func":      funcName,
				"timestamp": time.Now().Format(time.RFC3339),
			})
			_, _ = app.recallClient.CallDatabaseToolE(crashCtx, "save_to_recall", map[string]any{
				"server_id":  "magictools",
				"project_id": "global",
				"outcome":    "error",
				"session_id": fmt.Sprintf("magictools-crash-%d", time.Now().Unix()),
				"namespace":  "server_status",
				"state_data": string(crashPayload),
			})
		}
	}()
}

// primeFromRecall queries recall for historical session telemetry and
// auto-tunes runtime parameters. Currently calibrates TokenSpendThresh
// to 120% of the historical average token spend, preventing both
// runaway burns and overly conservative static limits.
//
// Safe no-op when recallClient is nil or recall is offline.
func primeFromRecall(ctx context.Context, rc *mcplib.RecallClient, cfg *config.Config) {
	if rc == nil || !rc.RecallEnabled() {
		return
	}

	profile := rc.GetOperationalProfile(ctx, 20)
	if profile == nil || profile.SessionCount < 3 {
		slog.Info("operational priming: insufficient historical data, using static defaults",
			"component", "priming")
		return
	}

	// Auto-calibrate TokenSpendThresh to 120% of historical average.
	// Only override if the current value is at the default (1,500,000).
	const defaultTokenThresh = 1500000
	if profile.AvgTokenSpend > 0 && cfg.TokenSpendThresh == defaultTokenThresh {
		calibrated := min(
			// Clamp to reasonable bounds [500_000, 5_000_000]
			max(

				int(profile.AvgTokenSpend*1.2), 500000), 5000000)
		slog.Info("operational priming: TokenSpendThresh auto-calibrated from recall",
			"component", "priming",
			"old_value", cfg.TokenSpendThresh,
			"new_value", calibrated,
			"avg_historical", int(profile.AvgTokenSpend),
			"sessions_analyzed", profile.SessionCount)
		cfg.TokenSpendThresh = calibrated
	}

	// Log server failure rates for observability
	for server, rate := range profile.ServerFailureRates {
		if rate > 0.3 {
			slog.Warn("operational priming: elevated server failure rate detected",
				"component", "priming",
				"server", server,
				"failure_rate", fmt.Sprintf("%.1f%%", rate*100))
		}
	}

	// 🛡️ LRU CACHE AUTO-TUNING: Increase LRU limit if cache miss rate is persistently high.
	// Only applies when we have at least 3 heartbeat records providing cache data.
	if profile.AvgCacheMissRate > 0.6 && profile.SessionCount >= 3 {
		newLimit := min(max(int(float64(cfg.LRULimit)*1.2), 500), 10000)
		if newLimit != cfg.LRULimit {
			slog.Info("operational priming: LRULimit auto-tuned from recall heartbeat data",
				"component", "priming",
				"old_value", cfg.LRULimit,
				"new_value", newLimit,
				"avg_cache_miss_rate", fmt.Sprintf("%.1f%%", profile.AvgCacheMissRate*100),
				"avg_memory_mb", fmt.Sprintf("%.1f", profile.AvgMemoryMB),
				"sessions_analyzed", profile.SessionCount)
			cfg.LRULimit = newLimit
		}
	}
}

func loadConfigAndDB(configPath, dbPath, logPath string, noOptimize, debug bool) (*db.Store, *config.Config, error) {
	cfg, err := config.New(Version, configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}
	cfg.NoOptimize = noOptimize
	cfg.Debug = debug
	if logPath != "" && logPath != config.DefaultLogPath() {
		cfg.LogPath = logPath
	}

	store, err := db.NewStore(dbPath, cfg.LRULimit)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open database: %w", err)
	}

	return store, cfg, nil
}
