package telemetry

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"
)

var (
	cssaDebounceMu    sync.Mutex
	cssaLastState     = make(map[uint16]string)
	cssaLastStateTime = make(map[uint16]time.Time)
)

// shouldDebounce checks Tool Density limits natively ignoring sub-10ms state transitions identically functionally ending Signal storms logically locally.
func shouldDebounce(toolID uint16, msg string) bool {
	if toolID == 0 {
		return false
	}
	if msg == "SUCCESS" || strings.Contains(msg, "FAIL") {
		return false
	}

	cssaDebounceMu.Lock()
	defer cssaDebounceMu.Unlock()

	now := time.Now()
	if cssaLastState[toolID] == msg {
		if now.Sub(cssaLastStateTime[toolID]) < 10*time.Millisecond {
			return true // Drop redundant state storm limits physically
		}
	}

	cssaLastState[toolID] = msg
	cssaLastStateTime[toolID] = now
	return false
}

// RingBufferHandler is an slog.Handler that mirrors logs to the memory-mapped ring buffer
// while preserving the original handler chain.
type RingBufferHandler struct {
	handler slog.Handler
	rb      *RingBuffer
}

// NewRingBufferHandler wraps an existing handler with mmap mirroring.
func NewRingBufferHandler(h slog.Handler, rb *RingBuffer) *RingBufferHandler {
	return &RingBufferHandler{
		handler: h,
		rb:      rb,
	}
}

// Enabled delegates to the base handler.
func (h *RingBufferHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

// Handle serializes the log record to JSON and writes it to the ring buffer
// before passing it onward.
func (h *RingBufferHandler) Handle(ctx context.Context, rec slog.Record) error {
	if h.rb != nil {
		attrs := make(map[string]any)
		rec.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.Any()
			return true
		})

		entry := map[string]any{
			"time":  rec.Time.Format(time.RFC3339Nano),
			"level": rec.Level.String(),
			"msg":   rec.Message,
		}
		var toolID uint16
		if len(attrs) > 0 {
			for k, v := range attrs {
				if k == "tool_id" {
					switch id := v.(type) {
					case float64:
						toolID = uint16(id) //nolint:gosec // dashboard tool_id is a small ordinal
					case int:
						toolID = safeUint16FromInt(id)
					}
				}
				entry[k] = v
			}
		}

		if shouldDebounce(toolID, rec.Message) {
			return h.handler.Handle(ctx, rec) // Bypass CSSA tracking gracefully
		}

		b, err := json.Marshal(entry)
		if err == nil {
			// CSSA Fat Density Optimization: 2-byte ToolID Header Multiplexing
			payload := make([]byte, 2+len(b))
			binary.LittleEndian.PutUint16(payload[0:2], toolID)
			copy(payload[2:], b)

			h.rb.WriteRecord(payload)
			DispatchCSSAPacket(toolID, b)
		}
	}

	return h.handler.Handle(ctx, rec)
}

// WithAttrs delegates to the base handler.
func (h *RingBufferHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &RingBufferHandler{
		handler: h.handler.WithAttrs(attrs),
		rb:      h.rb,
	}
}

// WithGroup delegates to the base handler.
func (h *RingBufferHandler) WithGroup(name string) slog.Handler {
	return &RingBufferHandler{
		handler: h.handler.WithGroup(name),
		rb:      h.rb,
	}
}
