package logging

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/util"

	mcplogging "github.com/maccavelli/mcplib/logging"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const logFormatText = "text"

// ----------------------------------------------------------------------------
// Core Configuration & Setup
// ----------------------------------------------------------------------------

func SetupGlobalLogger(store *db.Store, logPath string, logFormat string, level *slog.LevelVar, noOptimize, debug bool, realStdout io.Writer, mcpLogger ImcpLogHandler) *os.File {
	if logPath == "" {
		logPath = config.DefaultLogPath()
	}

	// 🛡️ STDOUT PROHIBITION (SECURITY GOLW):
	// In service mode, stdout is safe — no IDE pipe to corrupt.
	if !config.IsServiceMode() {
		if rs, ok := realStdout.(*os.File); ok && rs == os.Stdout {
			fmt.Fprintf(os.Stderr, "SECURITY VIOLATION: Logger directed to IDE pipe (direct os.Stdout)\n")
			os.Exit(1)
		}
	}

	format := os.Getenv("ORCHESTRATOR_LOG_FORMAT")
	if format == "" {
		format = logFormat
	}
	if format == "" {
		format = "json"
	}

	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if format == logFormatText && a.Key == slog.MessageKey {
				prefix := "[ORCHESTRATOR] "
				if noOptimize || debug {
					prefix = "[TRACE] "
				}
				a.Value = slog.StringValue(prefix + a.Value.String())
			}
			if (noOptimize || debug) && a.Key == slog.LevelKey {
				a.Value = slog.StringValue("TRACE")
			}
			return a
		},
	}

	var stderrHandler slog.Handler
	if format == logFormatText {
		stderrHandler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		stderrHandler = slog.NewJSONHandler(os.Stderr, opts)
	}

	if noOptimize || debug {
		var rootHandler = stderrHandler
		if telemetry.GlobalRingBuffer != nil {
			rootHandler = telemetry.NewRingBufferHandler(rootHandler, telemetry.GlobalRingBuffer)
		}
		slog.SetDefault(slog.New(rootHandler))
		log.SetOutput(os.Stderr)
		return os.Stderr
	}

	logFile := util.OpenHardenedLogFile(logPath)
	if logFile == nil {
		var rootHandler = stderrHandler
		if telemetry.GlobalRingBuffer != nil {
			rootHandler = telemetry.NewRingBufferHandler(rootHandler, telemetry.GlobalRingBuffer)
		}
		slog.SetDefault(slog.New(rootHandler))
		log.SetOutput(os.Stderr)
		return os.Stderr
	}

	var fileHandler slog.Handler
	if format == logFormatText {
		fileHandler = slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: level})
	} else {
		fileHandler = slog.NewJSONHandler(logFile, &slog.HandlerOptions{Level: level})
	}

	handlers := []slog.Handler{fileHandler, stderrHandler}

	if store != nil {
		dbHandler := slog.NewJSONHandler(&badgerLogWriter{store: store, ttl: 4 * time.Hour}, &slog.HandlerOptions{Level: level})
		handlers = append(handlers, dbHandler)
	}

	memHandler := slog.NewJSONHandler(GlobalLogBuffer, &slog.HandlerOptions{Level: level})
	handlers = append(handlers, memHandler)

	if mcpLogger != nil {
		handlers = append(handlers, mcpLogger)
	}

	rootHandler := NewMultiHandler(handlers...)
	if telemetry.GlobalRingBuffer != nil {
		rootHandler = telemetry.NewRingBufferHandler(rootHandler, telemetry.GlobalRingBuffer)
	}
	slog.SetDefault(slog.New(rootHandler))
	log.SetOutput(logFile)
	return logFile
}

// badgerLogWriter writes log records async to badger
type badgerLogWriter struct {
	store *db.Store
	ttl   time.Duration
}

func (b *badgerLogWriter) Write(p []byte) (n int, err error) {
	data := make([]byte, len(p))
	copy(data, p)
	if err := b.store.SaveLog(data, b.ttl); err != nil {
		return 0, err
	}
	return len(p), nil
}

// ParseLogLevel maps a string to a slog.Level.
func ParseLogLevel(level string) slog.Level {
	switch strings.ToUpper(level) {
	case "ERROR":
		return slog.LevelError
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "INFO":
		return slog.LevelInfo
	case "DEBUG":
		return slog.LevelDebug
	case "TRACE":
		return util.LevelTrace
	default:
		return slog.LevelDebug
	}
}

// ----------------------------------------------------------------------------
// Utilities (AsyncWriter & MCP/Multi Handlers)
// ----------------------------------------------------------------------------

type AsyncWriter struct {
	writer      io.Writer
	ch          chan []byte
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	dropped     int64
	maxDuration time.Duration
	closed      atomic.Bool
}

func NewAsyncWriter(w io.Writer, capacity int) *AsyncWriter {
	ctx, cancel := context.WithCancel(context.Background())
	aw := &AsyncWriter{
		writer:      w,
		ch:          make(chan []byte, capacity),
		ctx:         ctx,
		cancel:      cancel,
		maxDuration: 100 * time.Millisecond,
	}
	aw.wg.Add(1)
	go aw.worker()
	return aw
}

func (aw *AsyncWriter) worker() {
	defer aw.wg.Done()
	for {
		select {
		case p, ok := <-aw.ch:
			if !ok {
				return
			}
			if _, err := aw.writer.Write(p); err != nil {
				slog.Debug("async writer: write failed", "error", err)
			}
		case <-aw.ctx.Done():
			for p := range aw.ch {
				if _, err := aw.writer.Write(p); err != nil {
					slog.Debug("async writer: drain write failed", "error", err)
				}
			}
			return
		}
	}
}

func (aw *AsyncWriter) Write(p []byte) (n int, err error) {
	if aw.closed.Load() {
		return len(p), nil
	}
	data := make([]byte, len(p))
	copy(data, p)
	timer := time.NewTimer(aw.maxDuration)
	select {
	case aw.ch <- data:
		timer.Stop()
		return len(p), nil
	case <-timer.C:
		aw.dropped++
		return len(p), nil
	}
}

func (aw *AsyncWriter) Close() error {
	aw.closed.Store(true)
	aw.cancel()
	close(aw.ch)
	aw.wg.Wait()
	return nil
}

type multiHandler struct {
	handlers []slog.Handler
}

func NewMultiHandler(handlers ...slog.Handler) slog.Handler {
	return &multiHandler{handlers: handlers}
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return NewMultiHandler(handlers...)
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}
	return NewMultiHandler(handlers...)
}

// ImcpLogHandler defines the required contract for MCP IDE log bridges.
type ImcpLogHandler interface {
	Enabled(ctx context.Context, level slog.Level) bool
	Handle(ctx context.Context, r slog.Record) error
	WithAttrs(attrs []slog.Attr) slog.Handler
	WithGroup(name string) slog.Handler
}

type McpLogHandler struct {
	level   *slog.LevelVar
	session *mcp.ServerSession // single-session mode (stdio)
	mu      sync.RWMutex

	// Multi-session support for service mode.
	// Each log entry is routed only to the session that triggered it.
	sessions sync.Map // map[string]*mcp.ServerSession
}

func NewMcpLogHandler(level *slog.LevelVar) *McpLogHandler {
	return &McpLogHandler{level: level}
}

// SetSession sets the single-session reference for stdio mode.
func (m *McpLogHandler) SetSession(s *mcp.ServerSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.session = s
}

func (m *McpLogHandler) getSession() *mcp.ServerSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.session
}

// AddSession registers a session for multi-session routing in service mode.
func (m *McpLogHandler) AddSession(sess *mcp.ServerSession) {
	id := sess.ID()
	if id == "" {
		id = "default"
	}
	m.sessions.Store(id, sess)
}

// RemoveSession deregisters a session when its connection closes.
func (m *McpLogHandler) RemoveSession(id string) {
	m.sessions.Delete(id)
}

// StartReaper periodically cleans up stale sessions that were never properly closed
// (e.g., RST'd TCP connections where onClose never fires). This prevents sync.Map
// memory leaks in long-running service mode. Uses Ping as a liveness probe since
// hasInitialized is unexported in the go-sdk.
func (m *McpLogHandler) StartReaper(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sessions.Range(func(key, value any) bool {
				sess, ok := value.(*mcp.ServerSession)
				if !ok {
					return true
				}
				pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				if err := sess.Ping(pingCtx, nil); err != nil {
					m.sessions.Delete(key)
					slog.Info("reaped stale session", "session_id", key, "error", err)
				}
				cancel()
				return true
			})
		}
	}
}

func (m *McpLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= m.level.Level()
}

func (m *McpLogHandler) Handle(ctx context.Context, r slog.Record) error {
	// Try multi-session routing first (service mode).
	// If sessions map is non-empty, we're in service mode.
	sessionCount := 0
	m.sessions.Range(func(_, _ any) bool {
		sessionCount++
		return false // just need to know if non-empty
	})

	if sessionCount > 0 {
		// System-level logs (no specific session) broadcast to all sessions
		m.sessions.Range(func(_, v any) bool {
			sess, ok := v.(*mcp.ServerSession)
			if !ok {
				return true
			}
			if err := deliverMcpLog(ctx, sess, r); err != nil {
				slog.Debug("mcp log delivery failed", "error", err)
			}
			return true
		})
		return nil
	}

	// Fall back to single-session mode (stdio)
	s := m.getSession()
	if s == nil {
		return nil
	}
	return deliverMcpLog(ctx, s, r)
}

// deliverMcpLog sends a log record to a specific MCP server session.
func deliverMcpLog(ctx context.Context, s *mcp.ServerSession, r slog.Record) error {
	var mcpLevel mcp.LoggingLevel
	switch {
	case r.Level >= slog.LevelError:
		mcpLevel = "error"
	case r.Level >= slog.LevelWarn:
		mcpLevel = "warning"
	case r.Level >= slog.LevelInfo:
		mcpLevel = "info"
	default:
		mcpLevel = "debug"
	}
	logCtx := ctx
	if logCtx == nil {
		logCtx = context.Background()
	}
	// Bound the per-delivery call so a stalled/slow client cannot block the
	// logging goroutine (and thus every other logger behind it).
	logCtx, cancel := context.WithTimeout(logCtx, 250*time.Millisecond)
	defer cancel()
	return s.Log(logCtx, &mcp.LoggingMessageParams{
		Level:  mcpLevel,
		Logger: "magictools",
		Data:   renderData(r),
	})
}

// renderData flattens a slog.Record into the notification message string with
// secret redaction applied (this MCP-notification path does not pass through the
// SanitizingWriter, so without this secrets would reach connected clients).
func renderData(r slog.Record) string {
	sb := strings.Builder{}
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&sb, " %s=%v", a.Key, a.Value.Any())
		return true
	})
	return mcplogging.RedactString(sb.String())
}

func (m *McpLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return m
}

func (m *McpLogHandler) WithGroup(name string) slog.Handler {
	return m
}

// ----------------------------------------------------------------------------
// Wiretap Writers for Raw Traffic
// ----------------------------------------------------------------------------

type WireTapReader struct {
	Rc  io.ReadCloser
	Ctx context.Context
}

func (w *WireTapReader) Read(p []byte) (n int, err error) {
	n, err = w.Rc.Read(p)
	if n > 0 {
		slog.Log(w.Ctx, util.LevelTrace, "[WIRE-IN]", "raw", string(p[:n]))
	}
	return
}

func (w *WireTapReader) Close() error {
	return w.Rc.Close()
}

type WireTapWriter struct {
	Wc  io.WriteCloser
	Ctx context.Context
}

func (w *WireTapWriter) Write(p []byte) (n int, err error) {
	slog.Log(w.Ctx, util.LevelTrace, "[WIRE-OUT]", "raw", string(p))
	return w.Wc.Write(p)
}

func (w *WireTapWriter) Close() error {
	return w.Wc.Close()
}

// ----------------------------------------------------------------------------
// Backplane Logger
// ----------------------------------------------------------------------------

var Default *BackplaneLogger

func init() {
	Default = NewBackplaneLogger()
}

type Milestone string

const (
	SPAWN     Milestone = "SPAWN"
	HANDSHAKE Milestone = "HANDSHAKE"
	READY     Milestone = "READY"
	SYNC      Milestone = "SYNC"
	ERROR     Milestone = "ERROR"
	WARNING   Milestone = "WARNING"
)

type BackplaneLogger struct {
	mu    sync.Mutex
	start time.Time
}

func NewBackplaneLogger() *BackplaneLogger {
	return &BackplaneLogger{
		start: time.Now(),
	}
}

func (l *BackplaneLogger) Log(milestone Milestone, serverID string, details string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	slog.Info("boot milestone", "component", "lifecycle", "milestone", milestone, "server", serverID, "details", details)
}

type BootSummary struct {
	TotalAttempted int
	Success        int
	Limping        int
	Failed         int
	StartTime      time.Time
}

func (l *BackplaneLogger) Report(summary BootSummary) {
	l.mu.Lock()
	defer l.mu.Unlock()
	duration := time.Since(summary.StartTime).Seconds()
	slog.Info("global boot sequence complete", "component", "lifecycle", "total", summary.TotalAttempted, "success", summary.Success, "limping", summary.Limping, "failed", summary.Failed, "duration_sec", duration)
}
