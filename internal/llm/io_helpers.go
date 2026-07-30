package llm

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

func encodeJSONOrWarn(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("llm: failed to encode JSON response", "error", err)
	}
}
