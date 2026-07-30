package logging

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestRenderData_Redacts is the B regression: secrets in a record delivered over
// the MCP notification channel must be redacted before reaching the client.
func TestRenderData_Redacts(t *testing.T) {
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "auth attempt", 0)
	r.AddAttrs(slog.String("header", "Authorization: Bearer eyJa.bbbccc.dddEEE"))
	out := renderData(r)
	if strings.Contains(out, "eyJa.bbbccc.dddEEE") {
		t.Errorf("secret leaked to MCP notification: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected redaction, got %q", out)
	}
}
