// Package client provides functionality for the client subsystem.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/util"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DecoderTransport implements mcp.Transport using json.Decoder for robustness
// against large or non-newline-delimited JSON streams.
type DecoderTransport struct {
	Reader          io.ReadCloser
	Writer          io.WriteCloser
	PendingRequests *sync.Map
}

// Connect initializes the connection and starts the background read loop.
func (t *DecoderTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	dec := json.NewDecoder(t.Reader)
	dec.UseNumber() // [FIX] Prevent float64 coercion of numeric IDs
	c := &DecoderConnection{
		dec:             dec,
		enc:             json.NewEncoder(t.Writer),
		writer:          t.Writer,
		closer:          t,
		resChan:         make(chan readResult, 100),
		writeChan:       make(chan []byte, 1000),
		stop:            make(chan struct{}),
		pendingRequests: t.PendingRequests,
	}
	c.processor = newMessageProcessor(c)
	go c.readLoop(ctx) //nolint:gosec // readLoop uses the Connect-scoped ctx for cancellation
	go c.writerLoop(ctx)
	return c, nil
}

// Close closes the underlying reader and writer.
func (t *DecoderTransport) Close() error {
	var errs []error
	if t.Reader != nil {
		if err := t.Reader.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if t.Writer != nil {
		if err := t.Writer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors closing transport: %v", errs)
	}
	return nil
}

// DecoderConnection implements mcp.Connection.
type DecoderConnection struct {
	dec             *json.Decoder
	enc             *json.Encoder
	writer          io.Writer
	closer          io.Closer
	resChan         chan readResult
	writeChan       chan []byte
	stop            chan struct{}
	pendingRequests *sync.Map
	processor       *messageProcessor
	closeOnce       sync.Once
}

type readResult struct {
	msg jsonrpc.Message
	err error
}

// SessionID returns an empty string for non-session-aware connections.
func (c *DecoderConnection) SessionID() string {
	return ""
}

func (c *DecoderConnection) readLoop(ctx context.Context) {
	for {
		var raw json.RawMessage
		if err := c.dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				slog.Log(ctx, util.LevelTrace, "end of stream detected", "component", "transport", "server_eof", errors.Is(err, io.EOF))
			} else {
				// 🛡️ RECOVERY: Suppress noisy 'closed pipe' logs during intentional shutdowns or timeouts.
				errMsg := err.Error()
				if strings.Contains(errMsg, "closed pipe") || strings.Contains(errMsg, "file already closed") {
					slog.Log(ctx, util.LevelTrace, "underlying pipe closed", "component", "transport", "error", err)
				} else {
					bufferedData, bufErr := io.ReadAll(c.dec.Buffered())
					if bufErr == nil {
						slog.Error("decode failure", "component", "transport", "error", err, "buffer_remnant", string(bufferedData))
					} else {
						slog.Error("decode failure with buffer read error", "component", "transport", "error", err, "bufErr", bufErr)
					}
				}
			}

			select {
			case c.resChan <- readResult{err: err}:
			case <-c.stop:
			}
			return
		}

		// 🛡️ TRACE LOGGING: Record the raw JSON arriving from the backplane for JIT audit
		slog.Log(ctx, util.LevelTrace, "backplane message received", "component", "transport", "raw", string(raw))

		// 🛡️ MESSAGE PROCESSING: Decomposed into a dedicated processor
		msg, err := c.processor.process(raw)
		if err != nil {
			if !c.processor.handleError(ctx, err, raw) {
				return
			}
			continue
		}

		timer := time.NewTimer(1 * time.Second)
		select {
		case c.resChan <- readResult{msg: msg}:
			timer.Stop()
		case <-timer.C:
			slog.Warn("ipc transport backpressure detected, sdk reader stalled", "component", "transport", "pending", len(c.resChan))
			select {
			case c.resChan <- readResult{msg: msg}:
			case <-c.stop:
				return
			}
		case <-c.stop:
			timer.Stop()
			return
		}
	}
}

// Read returns the next JSON-RPC message from the background channel.
// It respects the context and provides a non-blocking read.
func (c *DecoderConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res, ok := <-c.resChan:
		if !ok {
			return nil, io.EOF
		}
		return res.msg, res.err
	}
}

// Write encodes a JSON-RPC message to the stream using the SDK's native encoder
// to ensure mandatory version tags (jsonrpc: "2.0") are included.
func (c *DecoderConnection) Write(ctx context.Context, msg jsonrpc.Message) error {
	// 🛡️ LOCK-FREE SERIALIZATION: Encode the message outside the mutex
	// to allow true concurrency across multiple callers multiplexing over this connection.
	data, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("transport encode fail: %w", err)
	}

	// 🛡️ JSON-RPC 2.0 COMPLIANCE: Absolute protocol enforcement for the backplane.
	// Some Go SDK versions omit the "jsonrpc" tag for certain notification/request types.
	// Pydantic-based sub-servers (python-mcp-server) will REJECT messages without it.
	trimmed := bytes.TrimSpace(data)
	if bytes.HasPrefix(trimmed, []byte("{")) && !bytes.Contains(trimmed, []byte("\"jsonrpc\":")) {
		// Injection: prepend the jsonrpc field cleanly using strings.Builder to minimize GC allocs.
		var sb strings.Builder
		sb.Grow(len(trimmed) + 20)
		sb.WriteString(`{"jsonrpc":"2.0",`)
		sb.Write(trimmed[1:])
		data = []byte(sb.String())
	}

	// 🛡️ TRACE LOGGING: Record outgoing message to match IDs with responses
	slog.Log(ctx, util.LevelTrace, "backplane message sent", "component", "transport", "raw", string(data))

	// When sending a Request (including initialize), store the request ID for the response router.
	// This map operation is natively concurrent.
	if req, ok := msg.(*jsonrpc.Request); ok && c.pendingRequests != nil {
		if req.ID.IsValid() {
			idBytes, idErr := json.Marshal(req.ID) //nolint:staticcheck // jsonrpc2.ID is SDK opaque type
			if idErr == nil {
				idStr := normalizeID(idBytes)
				info := &pendingRequest{
					Method:   req.Method,
					ActualID: idBytes,
				}
				c.pendingRequests.Store(idStr, info)
			} else {
				slog.Warn("failed to marshal req id", "component", "transport", "error", idErr)
			}
		}
	}

	select {
	case c.writeChan <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.stop:
		return fmt.Errorf("transport closed")
	}
}

// writerLoop handles asynchronous physical writes natively.
func (c *DecoderConnection) writerLoop(_ context.Context) {
	type deadlineWriter interface {
		SetWriteDeadline(t time.Time) error
	}
	dw, isDeadlineWriter := c.writer.(deadlineWriter)
	flusher, isFlusher := c.writer.(interface{ Flush() error })

	writeMsg := func(data []byte) {
		if isDeadlineWriter {
			if err := dw.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				slog.Debug("transport: set write deadline failed", "error", err)
			}
		}

		// Pre-allocate contiguous payload to guarantee atomic IPC boundary and reduce syscalls
		buf := make([]byte, 0, len(data)+1)
		buf = append(buf, data...)
		buf = append(buf, '\n')

		if _, err := c.writer.Write(buf); err != nil {
			slog.Error("write pipeline failed natively", "component", "transport", "error", err)
			return
		}

		if isFlusher {
			if err := flusher.Flush(); err != nil {
				slog.Debug("transport: flush failed", "error", err)
			}
		}
	}

	for {
		select {
		case <-c.stop:
			// HARD-37: Drain remaining messages using canonical Go drain-on-close
			// pattern. Go's select with multiple ready channels (stop + writeChan)
			// chooses non-deterministically, meaning the last enqueued JSON-RPC
			// message can be silently dropped. This ensures all enqueued messages
			// are physically written before the loop exits.
			for {
				select {
				case data := <-c.writeChan:
					writeMsg(data)
				default:
					return
				}
			}
		case data := <-c.writeChan:
			writeMsg(data)
			// HARD-3: Saturation gauge — warn when writeChan exceeds 80% capacity.
			// This detects backpressure before the channel fills completely and
			// Write() blocks the caller context, creating cascading stalls.
			if pending := len(c.writeChan); pending > 800 {
				slog.Warn("transport: writeChan near saturation",
					"component", "transport",
					"pending", pending,
					"capacity", cap(c.writeChan))
			}
		}
	}
}

// Close closes the connection via the transport.
func (c *DecoderConnection) Close() error {
	c.closeOnce.Do(func() { close(c.stop) })
	return c.closer.Close()
}

// RPCError is undocumented but satisfies standard structural requirements.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty,omitzero"`
}

// Error is undocumented but satisfies standard structural requirements.
func (e *RPCError) Error() string { return e.Message }

// Response is undocumented but satisfies standard structural requirements.
type Response struct {
	Result json.RawMessage `json:"result,omitempty,omitzero"`
	Error  *RPCError       `json:"error,omitempty,omitzero"`
	ID     json.RawMessage `json:"id"`
	Extra  any             `json:"-"`
}

type pendingRequest struct {
	Method   string
	ActualID json.RawMessage
}

// normalizeID strips quotes from string IDs that are purely numeric,
// ensuring "1" and 1 match for sub-servers that coerce ID types.
func normalizeID(raw json.RawMessage) string {
	s := string(bytes.TrimSpace(raw))
	// Strip quotes from string IDs that are entirely numeric (common for node/npm sub-servers)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		inner := s[1 : len(s)-1]
		if _, err := strconv.ParseInt(inner, 10, 64); err == nil {
			return inner
		}
	}
	return s
}
