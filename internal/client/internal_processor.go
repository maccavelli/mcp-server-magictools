// Package client provides functionality for the client subsystem.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/util"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// messageProcessor handles the logic for a single message in the readLoop.
type messageProcessor struct {
	conn *DecoderConnection
}

func newMessageProcessor(conn *DecoderConnection) *messageProcessor {
	return &messageProcessor{conn: conn}
}

// process handles a raw JSON message.
func (p *messageProcessor) process(raw json.RawMessage) (jsonrpc.Message, error) {
	// 🛡️ PRIMARY DECODER: Use the SDK's native decoder first for maximum compatibility
	msg, err := jsonrpc.DecodeMessage(raw)
	if err != nil {
		slog.Log(context.Background(), util.LevelTrace, "unknown message type", "component", "processor", "raw_payload", string(raw), "error", err)
		return nil, fmt.Errorf("unknown message type: %w", err)
	}

	slog.Log(context.Background(), util.LevelTrace, "mapped json-rpc sub-server response", "component", "processor", "raw_payload", string(raw))

	// 🛡️ REPAIR LOGIC: Intercept responses to "initialize" for sanitization
	if resp, ok := msg.(*jsonrpc.Response); ok {
		idVal := resp.ID
		idBytes, err := json.Marshal(idVal) //nolint:staticcheck // jsonrpc2.ID is SDK opaque type
		if err != nil {
			slog.Log(context.Background(), slog.LevelWarn, "failed to marshal id. generating fallback.", "component", "transport", "error", err)
			idBytes = []byte("\"fallback-id\"")
		}
		idStr := normalizeID(idBytes)

		if val, ok := p.conn.pendingRequests.Load(idStr); ok {
			p.conn.pendingRequests.Delete(idStr)
			info, ok := val.(*pendingRequest)
			if !ok {
				return msg, nil
			}
			if info.Method == "initialize" && len(resp.Result) > 0 {
				resp.Result = p.sanitizeInitializeResult(resp.Result)
			}
		}
	}

	return msg, nil
}

func (p *messageProcessor) sanitizeInitializeResult(result json.RawMessage) json.RawMessage {
	var rawRes map[string]any
	if err := json.Unmarshal(result, &rawRes); err != nil {
		return result
	}

	// 🛡️ PRESERVE EXTENSIONS: Some servers send 'instructions' or other metadata.
	// We only overwrite the core fields if they are missing or malformed.

	pVer, _ := rawRes["protocolVersion"].(string) //nolint:errcheck // optional field with fallback below
	if pVer == "" {
		rawRes["protocolVersion"] = "2024-11-05" // Fallback
	}

	if _, ok := rawRes["capabilities"]; !ok {
		rawRes["capabilities"] = map[string]any{}
	}
	if _, ok := rawRes["serverInfo"]; !ok {
		rawRes["serverInfo"] = map[string]any{"name": serverUnknownName, "version": "1.0.0"}
	}

	cleanBytes, err := json.Marshal(rawRes)
	if err != nil {
		slog.Warn("failed to marshal sanitized initialize result", "component", "processor", "error", err)
		return result
	}
	return cleanBytes
}

func (p *messageProcessor) handleError(ctx context.Context, err error, raw json.RawMessage) bool {
	slog.Log(ctx, util.LevelTrace, "decode error", "component", "processor", "raw_payload", string(raw), "error", err)

	if strings.Contains(err.Error(), "Unknown message type") {
		// Continue without sleeping — the 2s sleep was stalling the entire read loop.
		return true
	}

	// 🛡️ FIX: Return false for non-recoverable errors to allow the readLoop
	// to terminate cleanly instead of generating infinite error logs.
	select {
	case p.conn.resChan <- readResult{err: err}:
	case <-ctx.Done():
	case <-p.conn.stop:
	}
	return false
}
