// Package client provides functionality for the client subsystem.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/logging"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

// jsonrpcFirewall wraps a jsonFilterReader and validates every frame
// against JSON-RPC 2.0 structural requirements before passing it
// to the downstream json.Decoder. Invalid frames are dropped and
// logged with per-server attribution to identify sub-servers emitting
// bad data during orchestrator restarts.
type jsonrpcFirewall struct {
	source     *jsonFilterReader
	serverName string
	logger     *logging.BackplaneLogger
	delivery   bytes.Buffer

	// Telemetry: per-server counters
	ValidFrames   atomic.Int64
	DroppedFrames atomic.Int64
	LastDropTime  atomic.Value // time.Time
	LastDropRaw   atomic.Value // string (truncated to 512 bytes)
}

// newJsonRPCFirewall creates a firewall wrapping the given filter for a named sub-server.
func newJsonRPCFirewall(source *jsonFilterReader, serverName string, logger *logging.BackplaneLogger) *jsonrpcFirewall {
	return &jsonrpcFirewall{
		source:     source,
		serverName: serverName,
		logger:     logger,
	}
}

// Read satisfies io.Reader. It obtains complete balanced-JSON frames from the
// upstream jsonFilterReader via scanNextFrame(), validates each against
// JSON-RPC 2.0 structural rules, and only delivers validated frames to the
// downstream json.Decoder. Invalid frames are dropped silently from the
// reader's perspective but are logged with full attribution.
//
// CRITICAL: We call source.scanNextFrame() directly (same package) instead of
// source.Read(p) because Read() fragments frames across multiple calls via its
// internal delivery buffer. Validating a 512-byte chunk of a 10KB valid
// JSON-RPC response would cause a false positive drop.
func (fw *jsonrpcFirewall) Read(p []byte) (int, error) {
	// 1. Drain delivery buffer first (frame already validated)
	if fw.delivery.Len() > 0 {
		return fw.delivery.Read(p)
	}

	// 2. Get the next complete balanced-JSON frame from the upstream filter.
	//    scanNextFrame() blocks until a fully balanced { ... } object is assembled
	//    from the sub-server's stdout, guaranteeing we validate complete frames.
	for {
		frame, err := fw.source.scanNextFrame()
		if err != nil {
			return 0, err
		}

		if fw.validate(frame) {
			fw.ValidFrames.Add(1)
			telemetry.StdioMux.FirewallValid.Add(1)
			fw.delivery.Write(frame)
			return fw.delivery.Read(p)
		}

		// DROPPED: Log with full attribution and continue to next frame
		fw.DroppedFrames.Add(1)
		telemetry.StdioMux.FirewallDropped.Add(1)
		fw.LastDropTime.Store(time.Now())

		truncated := string(frame)
		if len(truncated) > 512 {
			truncated = truncated[:512] + "..."
		}
		fw.LastDropRaw.Store(truncated)

		slog.Error("jsonrpc firewall: dropped invalid frame",
			"component", "firewall",
			"server", fw.serverName,
			"size", len(frame),
			"preview", truncated,
		)
		if fw.logger != nil {
			fw.logger.Log(logging.ERROR, fw.serverName,
				fmt.Sprintf("FIREWALL DROP: %d bytes rejected (not valid JSON-RPC 2.0)", len(frame)))
		}
	}
}

// Close closes the underlying filter reader.
func (fw *jsonrpcFirewall) Close() error {
	return fw.source.Close()
}

// firewallEnvelope is the minimal structural representation of a JSON-RPC 2.0
// message used for validation. Only the fields needed for structural checks are
// decoded; the full payload is passed through unmodified to the downstream decoder.
type firewallEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// validate checks a single frame against JSON-RPC 2.0 structural compliance rules.
// Returns true if the frame is valid and should be forwarded to the decoder.
func (fw *jsonrpcFirewall) validate(frame []byte) bool {
	// Rule 1: Must be a JSON object
	trimmed := bytes.TrimSpace(frame)
	if len(trimmed) < 2 || trimmed[0] != '{' {
		return false
	}

	// Fast-path: check for "jsonrpc" before full unmarshal.
	// Text bleed, log output, or partial frames almost never contain this key.
	if !bytes.Contains(trimmed, []byte(`"jsonrpc"`)) {
		return false
	}

	// Partial unmarshal — only the fields needed for structural validation.
	var envelope firewallEnvelope
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return false
	}

	// Rule 2: Protocol version must be "2.0"
	if envelope.JSONRPC != "2.0" {
		return false
	}

	hasMethod := envelope.Method != ""
	hasResult := len(envelope.Result) > 0
	hasError := len(envelope.Error) > 0
	hasID := len(envelope.ID) > 0

	// Rule 3: Mutual exclusivity — a frame is either a request/notification OR a response
	if hasMethod && (hasResult || hasError) {
		return false
	}
	if !hasMethod && !hasResult && !hasError {
		return false
	}

	// Rule 4: Responses must have an ID
	if (hasResult || hasError) && !hasID {
		return false
	}

	// Rule 5: ID type validation (if present) — must be string, number, or null
	if hasID {
		id := bytes.TrimSpace(envelope.ID)
		if len(id) == 0 {
			return false
		}
		first := id[0]
		// Valid: string ("..."), number (digit/-), or null
		if first != '"' && first != 'n' && first != '-' && (first < '0' || first > '9') {
			return false
		}
	}

	// Rule 6: Frame size cap (defense-in-depth; already enforced by L1 filter)
	if len(trimmed) > 25*1024*1024 {
		return false
	}

	return true
}

// Snapshot returns per-server firewall metrics for the telemetry snapshot.
func (fw *jsonrpcFirewall) Snapshot() map[string]any {
	snap := map[string]any{
		"valid_frames":   fw.ValidFrames.Load(),
		"dropped_frames": fw.DroppedFrames.Load(),
	}
	if t, ok := fw.LastDropTime.Load().(time.Time); ok && !t.IsZero() {
		snap["last_drop_ago"] = time.Since(t).Truncate(time.Second).String()
	}
	if raw, ok := fw.LastDropRaw.Load().(string); ok && raw != "" {
		snap["last_drop_preview"] = raw
	}
	return snap
}
