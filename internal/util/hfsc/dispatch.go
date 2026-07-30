// Package hfsc implements the High-Fidelity Smart Chunking selective dispatch
// middleware. It manages the Tier-2 (Extreme Payload) IPC transport, streaming
// geometrically massive payloads natively through session Log notifications
// to strictly bypass the OOM limits of standard JSON-RPC marshalling.
package hfsc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// ReadSize is chosen so Base64 encoding remains efficiently bounded (3000 bytes -> 4000 base64 chars)
	ReadSize = 3000
)

// generateSessionID returns a cryptographically random hex session ID.
func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("fallback-%d", b[0])
	}
	return hex.EncodeToString(b)
}

// Session defines the minimal interface required for HFSC streaming.
type Session interface {
	Log(ctx context.Context, params *mcp.LoggingMessageParams) error
}

// StreamHeavyPayload executes Tier-2 extreme scaling transfer protocol.
// It instantly returns an HFSC proxy intercept pointer to the Orchestrator
// and spins off an asynchronous stream converting the io.Reader into sequential
// Base64 Log chunks, bounding RAM footprint precisely to the chunk size.
func StreamHeavyPayload(
	ctx context.Context,
	session Session,
	filename string,
	projectID string,
	model string,
	reader io.Reader,
) (*mcp.CallToolResult, error) {
	// Gate 0: Standalone Mode check
	if os.Getenv("MCP_ORCHESTRATOR_OWNED") != "true" || session == nil {
		// Fallback to reading the stream into a string if not running under orchestrator
		b, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to read stream for standalone delivery: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(b)},
			},
		}, nil
	}

	sessionID := generateSessionID()
	ext := strings.ToLower(filepath.Ext(filename))

	// Intercept Manifest Payload
	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("Extreme-scale %s payload detected. Bypassing massive serialization limits via HFSC stream...", ext),
			},
		},
		Meta: map[string]any{
			"hfsc_stream": true,
			"session_id":  sessionID,
			"filename":    filename,
			"project_id":  projectID,
			"model":       model,
		},
	}

	// Detach context so the stream survives after the Result is pushed down the pipe
	asyncCtx := context.WithoutCancel(ctx)
	go executeContinuousStream(asyncCtx, session, sessionID, reader)

	return result, nil
}

// executeContinuousStream chunks the io.Reader locally without buffering the whole struct.
func executeContinuousStream(ctx context.Context, session Session, sessionID string, reader io.Reader) {
	slog.Info("hfsc: extreme payload stream started", "session", sessionID)

	hasher := sha256.New()
	buf := make([]byte, ReadSize)
	index := 0

	for {
		n, err := reader.Read(buf)
		if n > 0 {
			hasher.Write(buf[:n])
			chunk := base64.StdEncoding.EncodeToString(buf[:n])

			wire := fmt.Sprintf("HFSC_STREAM|v2|%s|%d|%s", sessionID, index, chunk)
			dataBytes, marshalErr := json.Marshal(wire)
			if marshalErr != nil {
				slog.Error("hfsc: chunk wire marshal failed", "error", marshalErr, "session", sessionID, "index", index)
				return
			}
			params := &mcp.LoggingMessageParams{
				Logger: "hfsc",
				Level:  "info",
				Data:   json.RawMessage(dataBytes),
			}
			if logErr := session.Log(ctx, params); logErr != nil {
				slog.Error("hfsc: chunk log push failed, stream violently terminated", "error", logErr, "session", sessionID, "index", index)
				return
			}
			index++
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			slog.Error("hfsc: unrecoverable read fault inside extreme payload pipeline", "error", err, "session", sessionID)
			// Proceed to finalize whatever was extracted before fault
			break
		}
	}

	// Complete Stream Checksum Finalizer
	hashHex := hex.EncodeToString(hasher.Sum(nil))
	wire := fmt.Sprintf("HFSC_FINALIZE|v2|%s|%d|%s", sessionID, index, hashHex)
	dataBytes, err := json.Marshal(wire)
	if err != nil {
		slog.Error("hfsc: finalize wire marshal failed", "error", err, "session", sessionID)
		return
	}
	params := &mcp.LoggingMessageParams{
		Logger: "hfsc",
		Level:  "info",
		Data:   json.RawMessage(dataBytes),
	}

	if err := session.Log(ctx, params); err != nil {
		slog.Error("hfsc: failed to broadcast stream finalizer hash", "error", err, "session", sessionID)
	} else {
		slog.Info("hfsc: extreme stream successfully finalized natively", "session", sessionID, "total_chunks", index, "sha256", hashHex)
	}
}
