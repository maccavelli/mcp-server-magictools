// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/goccy/go-json"
	"github.com/spf13/cobra"

	"github.com/maccavelli/mcp-server-magictools/internal/ipc"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

// --- MULTIPLEXER IMPLEMENTATION (Phase A, B, C) ---

// ProxyLedger manages multiplexed client channels with Dual-Axis QoS.
// REV-3: Enforces both a slot high-water mark (80%) for notifications
// and a per-client byte quota (25MB) to prevent OOM.
type ProxyLedger struct {
	mu              sync.RWMutex
	clients         map[string]chan []byte
	clientBytes     map[string]*atomic.Int64 // per-client buffered byte tracker
	DroppedPayloads atomic.Int64             // HARD-1: Total dropped payloads counter
}

const (
	// ledgerChannelSize is the per-client channel buffer slot count.
	// HARD-1: Increased from 100 to 500 for heavy SSE fan-out.
	ledgerChannelSize = 500

	// ledgerByteQuota is the maximum bytes that may be buffered per-client (25MB).
	// REV-3 (Black Swan Mitigation): Prevents OOM from large payloads.
	ledgerByteQuota = 25 * 1024 * 1024
)

// NewProxyLedger creates a new ProxyLedger with Dual-Axis QoS support.
func NewProxyLedger() *ProxyLedger {
	return &ProxyLedger{
		clients:     make(map[string]chan []byte),
		clientBytes: make(map[string]*atomic.Int64),
	}
}

// AddClient registers a new client with a buffered channel.
func (l *ProxyLedger) AddClient(clientID string) chan []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	ch := make(chan []byte, ledgerChannelSize)
	l.clients[clientID] = ch
	l.clientBytes[clientID] = &atomic.Int64{}
	return ch
}

// PurgeClient removes a client and closes its channel.
func (l *ProxyLedger) PurgeClient(clientID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ch, ok := l.clients[clientID]; ok {
		close(ch)
		delete(l.clients, clientID)
		delete(l.clientBytes, clientID)
		slog.Info("multiplexer: cleanly purged client from ledger", "client_id", clientID)
	}
}

// RouteToClient routes an ID-bearing response payload to a specific client.
// Enforces the Dual-Axis QoS byte quota but NOT the slot HWM (responses are critical).
func (l *ProxyLedger) RouteToClient(clientID string, payload []byte) {
	l.mu.RLock()
	ch, ok := l.clients[clientID]
	byteTracker, bytesOk := l.clientBytes[clientID]
	l.mu.RUnlock()

	if !ok {
		return
	}

	// REV-3: Byte quota enforcement (axis 2). Prevents OOM from large payloads.
	if bytesOk && byteTracker.Load()+int64(len(payload)) > ledgerByteQuota {
		telemetry.StdioMux.ByteQuotaDrops.Add(1)
		telemetry.StdioMux.OutboundDrops.Add(1)
		l.DroppedPayloads.Add(1)
		slog.Warn("multiplexer: byte quota exceeded, dropping response payload",
			"client_id", clientID,
			"buffered_bytes", byteTracker.Load(),
			"payload_len", len(payload),
			"quota", ledgerByteQuota)
		return
	}

	// Non-blocking send to prevent deadlocks from unresponsive clients
	select {
	case ch <- payload:
		if bytesOk {
			byteTracker.Add(int64(len(payload)))
		}
	default:
		telemetry.StdioMux.OutboundDrops.Add(1)
		l.DroppedPayloads.Add(1)
		slog.Warn("multiplexer: client channel saturated, dropping payload", "client_id", clientID)
	}
}

// BroadcastNotification fans out a JSON-RPC notification (no "id" field) to all
// registered clients. BUG-3: Notifications were previously silently dropped.
// REV-3: Implements the slot high-water mark QoS filter — if a client's channel
// exceeds 80% capacity, notification payloads are aggressively dropped to reserve
// the remaining 20% exclusively for critical ID-bearing responses.
func (l *ProxyLedger) BroadcastNotification(payload []byte) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for clientID, ch := range l.clients {
		byteTracker := l.clientBytes[clientID]

		// REV-3: Byte quota enforcement (axis 2).
		if byteTracker != nil && byteTracker.Load()+int64(len(payload)) > ledgerByteQuota {
			telemetry.StdioMux.ByteQuotaDrops.Add(1)
			telemetry.StdioMux.NotificationsDroppedQoS.Add(1)
			l.DroppedPayloads.Add(1)
			continue
		}

		// REV-3: Slot high-water mark QoS (axis 1).
		// If the channel is >80% full, drop notifications to preserve space for responses.
		if len(ch) > cap(ch)*4/5 {
			telemetry.StdioMux.NotificationsDroppedQoS.Add(1)
			l.DroppedPayloads.Add(1)
			continue
		}

		select {
		case ch <- payload:
			telemetry.StdioMux.NotificationsBroadcast.Add(1)
			if byteTracker != nil {
				byteTracker.Add(int64(len(payload)))
			}
		default:
			telemetry.StdioMux.OutboundDrops.Add(1)
			l.DroppedPayloads.Add(1)
		}
	}
}

// InboundMapping rewrites the JSON-RPC "id" field to prepend a client identifier.
// PERF-2: Uses surgical byte replacement instead of full json.Unmarshal→json.Marshal
// round-trip. Falls back to full parse only if the fast path fails.
// HARD-5: The fast path now requires "jsonrpc" to appear before the "id" match
// position, preventing corruption when "id":" appears inside nested string values.
func InboundMapping(clientID string, reqBytes []byte) ([]byte, error) {
	if clientID == "" {
		return reqBytes, nil
	}

	// HARD-5: Tighten heuristic — require "jsonrpc" to appear in the message
	// before the "id":" match. This prevents the fast path from corrupting
	// messages where "id":" appears inside nested string values.
	jsonrpcKey := []byte(`"jsonrpc"`)
	idKey := []byte(`"id":`)
	jsonrpcIdx := bytes.Index(reqBytes, jsonrpcKey)
	idx := bytes.Index(reqBytes, idKey)

	// Fast path: only proceed if both markers exist and "jsonrpc" comes first.
	if jsonrpcIdx >= 0 && idx >= 0 && jsonrpcIdx < idx {
		valStart := idx + len(idKey)
		// Skip whitespace
		for valStart < len(reqBytes) && (reqBytes[valStart] == ' ' || reqBytes[valStart] == '\t') {
			valStart++
		}
		if valStart < len(reqBytes) {
			// Find the end of the value (number, string, or null)
			valEnd := valStart
			if reqBytes[valStart] == '"' {
				// String value: find closing quote
				valEnd = bytes.IndexByte(reqBytes[valStart+1:], '"')
				if valEnd >= 0 {
					valEnd += valStart + 2 // Include both quotes
					original := reqBytes[valStart:valEnd]
					// Strip quotes from original for the rewrite
					inner := string(original[1 : len(original)-1])
					rewritten := fmt.Sprintf(`"%s::%s"`, clientID, inner)
					result := make([]byte, 0, len(reqBytes)+len(clientID)+2)
					result = append(result, reqBytes[:valStart]...)
					result = append(result, []byte(rewritten)...)
					result = append(result, reqBytes[valEnd:]...)
					return result, nil
				}
			} else {
				// Numeric/null value: find delimiter (, } or whitespace)
				for valEnd < len(reqBytes) && reqBytes[valEnd] != ',' && reqBytes[valEnd] != '}' && reqBytes[valEnd] != ' ' && reqBytes[valEnd] != '\n' {
					valEnd++
				}
				original := string(reqBytes[valStart:valEnd])
				rewritten := fmt.Sprintf(`"%s::%s"`, clientID, original)
				result := make([]byte, 0, len(reqBytes)+len(clientID)+4)
				result = append(result, reqBytes[:valStart]...)
				result = append(result, []byte(rewritten)...)
				result = append(result, reqBytes[valEnd:]...)
				return result, nil
			}
		}
	}

	// Fallback: full parse for edge cases (nested objects, unusual formatting,
	// or when "jsonrpc" keyword is absent/after "id" in the byte stream).
	var payload map[string]any
	if err := json.Unmarshal(reqBytes, &payload); err != nil {
		return nil, err
	}
	if idVal, ok := payload["id"]; ok {
		payload["id"] = fmt.Sprintf("%s::%v", clientID, idVal)
	}
	return json.Marshal(payload)
}

// OutboundFilter is the central reader loop filter for outbound JSON-RPC messages.
// BUG-5 (REV-2): Returns an error on marshal/unmarshal failures to enable callers
// to detect response black holes and trigger clean proxy restarts.
func OutboundFilter(resBytes []byte, ledger *ProxyLedger) error {
	telemetry.StdioMux.OutboundMessages.Add(1)

	var payload map[string]any
	if err := json.Unmarshal(resBytes, &payload); err != nil {
		telemetry.StdioMux.UnmarshalErrors.Add(1)
		// HARD-39: Log truncated SSE data for diagnosis instead of silent drop.
		prefix := string(resBytes)
		if len(prefix) > 128 {
			prefix = prefix[:128]
		}
		slog.Warn("proxy: outbound filter unmarshal failed (possible truncated SSE)",
			"error", err,
			"raw_bytes_len", len(resBytes),
			"raw_prefix", prefix)
		// BUG-5 (REV-2): Return the error to signal a response black hole.
		// The caller (handleSSE/handleJSON) will invoke p.cancel() to force
		// the IDE to detect EOF and cleanly restart the proxy connection.
		return fmt.Errorf("outbound unmarshal: %w", err)
	}

	if idVal, ok := payload["id"].(string); ok {
		// Extract ClientID using :: delimiter (dash-safe)
		parts := strings.SplitN(idVal, "::", 2)
		if len(parts) == 2 {
			clientID := parts[0]
			originalID := parts[1]

			// Restore original integer or string ID
			if originalInt, err := strconv.Atoi(originalID); err == nil {
				payload["id"] = originalInt
			} else {
				payload["id"] = originalID
			}

			// BUG-5: Check json.Marshal error instead of silently discarding.
			restoredBytes, marshalErr := json.Marshal(payload)
			if marshalErr != nil {
				telemetry.StdioMux.JSONMarshalErrors.Add(1)
				slog.Error("proxy: outbound filter marshal failed",
					"error", marshalErr,
					"client_id", clientID)
				return fmt.Errorf("outbound marshal: %w", marshalErr)
			}
			ledger.RouteToClient(clientID, restoredBytes)
			return nil
		}
	}

	// BUG-3: If no ID mapping exists, this is a JSON-RPC notification.
	// Broadcast to all registered clients in the ledger.
	ledger.BroadcastNotification(resBytes)
	return nil
}

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Stdio-to-HTTP proxy for IDEs utilizing Polymorphic IPC",
	Long:  `Bridges stdio (stdin/stdout) to the MagicTools HTTP service endpoint using ultra-optimized Polymorphic IPC (UDS/Named Pipes with TCP Fallback).`,
	RunE:  runProxy,
}

func init() {
	rootCmd.AddCommand(proxyCmd)
}

func runProxy(cmd *cobra.Command, args []string) error {
	initWindowsStdio()

	// HARD-38: Suppress SIGPIPE to prevent noisy crash when IDE closes stdin
	// during the shutdown window. Go's runtime only suppresses SIGPIPE for
	// net.Conn writes (since Go 1.1), not for os.File writes to pipe fds.
	// Without this, writeLine to a broken stdout pipe delivers SIGPIPE which
	// terminates the proxy process — the exact "crash during restart" symptom.
	signal.Ignore(syscall.SIGPIPE)

	// Wait for service to be reachable via IPC
	client, serviceURL, token, err := waitForServiceIPC(30 * time.Second)
	if err != nil {
		return fmt.Errorf("service not reachable via IPC: %w", err)
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	proxy := &stdioProxy{
		serviceURL: serviceURL,
		token:      token,
		client:     client,
		stdout:     os.Stdout,
		cancel:     cancel,
		bufferPool: &sync.Pool{
			New: func() any {
				// Pre-size at 64KB — sufficient for most MCP JSON-RPC messages
				// without resizing, while staying well within SSE/relay payload limits.
				return bytes.NewBuffer(make([]byte, 0, 64*1024))
			},
		},
		ledger: NewProxyLedger(),
	}

	return proxy.run(ctx)
}

func waitForServiceIPC(timeout time.Duration) (*http.Client, string, string, error) {
	deadline := time.Now().Add(timeout)

	var transport *http.Transport
	var serviceURL string
	var token string

	for time.Now().Before(deadline) {
		// Attempt Primary IPC (UDS / Named Pipe)
		conn, err := ipc.DialPrimary()
		if err == nil {
			closeConnOrWarn(conn)
			serviceURL = ipc.DialPrimaryURL()
			transport = &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return ipc.DialPrimary()
				},
			}
			break
		}

		// Attempt TCP Fallback
		conn, tkn, err := ipc.DialTCPFallback()
		if err == nil {
			addr := conn.RemoteAddr().String()
			closeConnOrWarn(conn)
			serviceURL = fmt.Sprintf("http://%s/mcp", addr)
			token = tkn
			transport = &http.Transport{}
			break
		}

		timer := time.NewTimer(500 * time.Millisecond)
		<-timer.C
	}

	if transport == nil {
		return nil, "", "", fmt.Errorf("timeout waiting for IPC socket or fallback TCP")
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Minute,
	}

	// Verify health check (to match old behavior, though we use /mcp URL base here, health might be at /health)
	// We'll skip formal HTTP health polling because successfully dialing the IPC socket essentially confirms readiness in this architecture.

	return client, serviceURL, token, nil
}

type stdioProxy struct {
	serviceURL string
	token      string
	client     *http.Client
	stdout     io.Writer

	sessionID  string
	mu         sync.Mutex
	cancel     context.CancelFunc
	bufferPool *sync.Pool

	ledger *ProxyLedger
}

func (p *stdioProxy) run(ctx context.Context) error {
	// Parse inbound JSON-RPC messages from stdin using json.Decoder (bufio.Reader backed).
	// Transport context: the go-sdk Streamable HTTP transport sends newline-delimited JSON
	// over stdin (one JSON object per line). json.Decoder reads raw bytes via bufio.Reader,
	// which does NOT strip \r characters \u2014 however RFC 8259 defines \r (CR) as valid JSON
	// whitespace, so json.Decoder is inherently CRLF-tolerant even if Windows ConPTY injects
	// extra CR bytes between JSON objects. No explicit CR-stripping pass is required here.
	dec := json.NewDecoder(os.Stdin)

	// Start the central reader loop for this proxy instance
	clientID := "local-ide"
	ch := p.ledger.AddClient(clientID)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("proxy: panic in client channel reader", "panic", r)
			}
		}()
		for payload := range ch {
			p.writeLine(payload)
			// REV-3: Decrement per-client byte tracker after consumption.
			// This keeps the byte quota tracker accurate for QoS enforcement.
			p.ledger.mu.RLock()
			if b, ok := p.ledger.clientBytes[clientID]; ok {
				b.Add(-int64(len(payload)))
			}
			p.ledger.mu.RUnlock()
		}
	}()

	for {
		var msg json.RawMessage
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				// Phase C: Ledger Purging on EOF
				p.ledger.PurgeClient(clientID)
				return nil
			}
			return fmt.Errorf("stdin decode error: %w", err)
		}
		// TEL-1: Count inbound messages from IDE stdin.
		telemetry.StdioMux.InboundMessages.Add(1)

		// Phase B: Inbound Mapping (Sequence Rewriting)
		rewrittenMsg, rewriteErr := InboundMapping(clientID, msg)
		if rewriteErr != nil {
			slog.Warn("proxy: inbound mapping failed", "error", rewriteErr)
			rewrittenMsg = msg
		}

		// Relay blocks to enforce sequential JSON-RPC processing (concurrency control)
		// BUG-2: Pass retryDepth=0 for initial call; relay internally guards against >1.
		if err := p.relay(ctx, rewrittenMsg, 0); err != nil {
			slog.Error("proxy relay error", "error", err)
		}
	}
}

// BUG-2: retryDepth parameter prevents unbounded 401 recursive relay.
// The caller passes 0; on 401 auto-heal, relay calls itself with retryDepth+1.
// If retryDepth >= 1, the retry is rejected to prevent stack overflow.
func (p *stdioProxy) relay(ctx context.Context, msg []byte, retryDepth int) error {
	// Use the sync.Pool-backed buffer to avoid per-call allocation on the hot relay path.
	buf := bufferFromPool(p.bufferPool)
	buf.Reset()
	buf.Write(msg)
	req, err := http.NewRequestWithContext(ctx, "POST", p.serviceURL, buf)
	if err != nil {
		p.bufferPool.Put(buf)
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")

	p.mu.Lock()
	if p.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", p.sessionID)
	}
	p.mu.Unlock()

	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := p.client.Do(req) //nolint:bodyclose // response body closed per branch or handed to SSE goroutine
	// The request body has been fully consumed by client.Do; return buffer to pool.
	p.bufferPool.Put(buf)
	if err != nil {
		if resp != nil && resp.Body != nil {
			closeBodyOrWarn(resp.Body)
		}
		// If we encounter a 401 equivalent due to daemon restart dropping the connection,
		// the proxy retry loop (in waitForServiceIPC during startup, though maybe here we could dynamically reconnect)
		// Actually, standard proxy simply errors out and IDE restarts it automatically, but let's log it.
		return fmt.Errorf("HTTP request failed: %w", err)
	}

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		p.mu.Lock()
		p.sessionID = sid
		p.mu.Unlock()
	}

	if resp.StatusCode == http.StatusAccepted {
		closeBodyOrWarn(resp.Body)
		return nil
	}

	// Dynamic 401 Auto-Healing for Daemon Restart
	// BUG-2: Depth guard prevents infinite recursion on persistent auth failures.
	if resp.StatusCode == http.StatusUnauthorized {
		closeBodyOrWarn(resp.Body)
		if retryDepth >= 1 {
			telemetry.StdioMux.RelayErrors.Add(1)
			return fmt.Errorf("HTTP 401: auth retry exhausted (depth=%d), aborting", retryDepth)
		}
		// Try to read the new token atomically
		if conn, newToken, fallbackErr := ipc.DialTCPFallback(); fallbackErr == nil {
			addr := conn.RemoteAddr().String() // Capture before close to prevent use-after-close
			closeConnOrWarn(conn)
			p.token = newToken
			p.serviceURL = fmt.Sprintf("http://%s/mcp", addr)
			telemetry.StdioMux.Relay401Heals.Add(1)
			return p.relay(ctx, msg, retryDepth+1)
		}
		telemetry.StdioMux.RelayErrors.Add(1)
		return fmt.Errorf("HTTP 401: unauthorized and fallback recovery failed")
	}

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		closeBodyOrWarn(resp.Body)
		if readErr != nil {
			return fmt.Errorf("HTTP %d: read body: %w", resp.StatusCode, readErr)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	ct := resp.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "text/event-stream"):
		go func(body io.ReadCloser) {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("proxy: panic in SSE stream handler", "panic", r)
				}
			}()
			defer closeBodyOrWarn(body)
			if err := p.handleSSE(body); err != nil {
				// HARD-39 + BUG-5 (REV-2): When service dies mid-SSE or OutboundFilter
				// returns a marshal error (response black hole), trigger context cancellation
				// for clean restart. The IDE detects stdin EOF and spawns a fresh proxy.
				slog.Warn("proxy: SSE stream error (service may have restarted)", "error", err)
				p.cancel()
			}
		}(resp.Body)
		return nil
	case strings.HasPrefix(ct, "application/json"):
		defer closeBodyOrWarn(resp.Body)
		return p.handleJSON(resp.Body)
	default:
		closeBodyOrWarn(resp.Body)
		return fmt.Errorf("unexpected content-type: %s", ct)
	}
}

func (p *stdioProxy) handleSSE(r io.Reader) error {
	// HARD-2: Removed unused pool buffer acquisition. The previous code acquired
	// a pool buffer (buf), reset it, and returned it — but never used it.
	// The actual work uses lineBuf and chunkBuf directly.

	// TEL-1: Track active SSE streams.
	telemetry.StdioMux.SSEStreamsActive.Add(1)
	defer telemetry.StdioMux.SSEStreamsActive.Add(-1)

	var lineBuf bytes.Buffer
	chunkBuf := make([]byte, 32*1024)
	for {
		n, err := r.Read(chunkBuf)
		if n > 0 {
			lineBuf.Write(chunkBuf[:n])
			for {
				lineBytes := lineBuf.Bytes()
				idx := bytes.IndexByte(lineBytes, '\n')
				if idx == -1 {
					break
				}
				line := string(lineBytes[:idx])
				lineBuf.Next(idx + 1)

				// BUG-4: Strip trailing \r from SSE lines. The SSE spec allows
				// \r\n line endings; without this, the \r becomes part of the
				// JSON data, causing unmarshal failures in OutboundFilter.
				line = strings.TrimRight(line, "\r")

				if strings.HasPrefix(line, "data: ") {
					data := line[6:]
					if data != "" {
						telemetry.StdioMux.SSEEventsProcessed.Add(1)
						// BUG-5 (REV-2): OutboundFilter now returns error.
						// On marshal failure, propagate to trigger p.cancel().
						if filterErr := OutboundFilter([]byte(data), p.ledger); filterErr != nil {
							return fmt.Errorf("outbound filter: %w", filterErr)
						}
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (p *stdioProxy) handleJSON(r io.Reader) error {
	// HARD-4: Wrap the reader with io.LimitReader to prevent unbounded memory
	// consumption from a malicious or broken service sending an infinite stream.
	// 50 MB safety cap, matching filter.go's 25 MB × 2 reasonable limit.
	limited := io.LimitReader(r, 50*1024*1024)

	// Use pool buffer for reading the JSON response body.
	buf := bufferFromPool(p.bufferPool)
	buf.Reset()
	defer p.bufferPool.Put(buf)

	chunk := make([]byte, 32*1024)
	for {
		n, err := limited.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("read JSON body: %w", err)
		}
	}

	body := buf.Bytes()
	if len(bytes.TrimSpace(body)) > 0 {
		// BUG-5 (REV-2): Propagate OutboundFilter errors.
		if filterErr := OutboundFilter(body, p.ledger); filterErr != nil {
			return fmt.Errorf("outbound filter: %w", filterErr)
		}
	}
	return nil
}

// writeLine writes a JSON-RPC payload to stdout with a trailing newline.
// BUG-1 (REV-1): Stratified error handling.
//   - syscall.EPIPE: Terminal pipe sever → cancel proxy context immediately.
//   - Transient errors: Bounded exponential backoff retry (up to 3 attempts).
func (p *stdioProxy) writeLine(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// HARD-30: Atomic write — pre-concatenate payload + newline into a single
	// contiguous buffer to guarantee the OS receives a single write(2) syscall.
	buf := make([]byte, len(data)+1)
	copy(buf, data)
	buf[len(data)] = '\n'

	const maxRetries = 3
	backoff := 5 * time.Millisecond

	for attempt := range maxRetries {
		_, err := p.stdout.Write(buf)
		if err == nil {
			return
		}

		telemetry.StdioMux.WriteErrors.Add(1)

		// BUG-1 (REV-1): Terminal error — IDE pipe is severed.
		if errors.Is(err, syscall.EPIPE) {
			slog.Error("proxy: stdout pipe broken (EPIPE), cancelling proxy",
				"attempt", attempt+1)
			p.cancel()
			return
		}

		// Transient error — bounded exponential backoff.
		slog.Warn("proxy: stdout write failed (transient), retrying",
			"attempt", attempt+1,
			"max_retries", maxRetries,
			"error", err)
		time.Sleep(backoff)
		backoff *= 2
	}

	// All retries exhausted — escalate to cancellation.
	slog.Error("proxy: stdout write retries exhausted, cancelling proxy")
	p.cancel()
}
