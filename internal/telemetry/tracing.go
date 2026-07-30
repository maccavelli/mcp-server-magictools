package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// corrIDKeyType is the context key for correlation IDs
type corrIDKeyType struct{}

// CorrIDKey is the exported instance
var CorrIDKey = corrIDKeyType{}

// NewCorrelationID generates a short, unique ID (12 chars) for request tracing.
func NewCorrelationID() string {
	b := make([]byte, 6) // 12 hex chars
	if _, err := rand.Read(b); err != nil {
		return "000000000000" // Fallback safely
	}
	return hex.EncodeToString(b)
}

// WithCorrelationID attaches a correlation ID to the context.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, CorrIDKey, id)
}

// GetCorrelationID retrieves it from the context or returns empty if not found.
func GetCorrelationID(ctx context.Context) string {
	if val := ctx.Value(CorrIDKey); val != nil {
		if id, ok := val.(string); ok {
			return id
		}
	}
	return ""
}
