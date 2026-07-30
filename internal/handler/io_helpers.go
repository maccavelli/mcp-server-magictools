package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
	"github.com/maccavelli/mcplib"
)

func marshalJSONOrEmpty(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Warn("handler: json.Marshal failed", "error", err)
		return []byte("{}")
	}
	return b
}

func marshalIndentOrEmpty(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		slog.Warn("handler: json.MarshalIndent failed", "error", err)
		return []byte("{}")
	}
	return b
}

func unmarshalOrWarn(data []byte, v any) {
	if err := json.Unmarshal(data, v); err != nil {
		slog.Warn("handler: json.Unmarshal failed", "error", err)
	}
}

func unmarshalArgsOrWarn(args json.RawMessage, v any) {
	if len(args) == 0 {
		return
	}
	unmarshalOrWarn(args, v)
}

func sliceAnyOrWarn(v any) []any {
	s, ok := v.([]any)
	if !ok {
		slog.Warn("handler: expected []any type assertion failed")
		return nil
	}
	return s
}

func matchesSliceOrWarn(v any) []map[string]any {
	s, ok := v.([]map[string]any)
	if !ok {
		slog.Warn("handler: expected []map[string]any type assertion failed")
		return nil
	}
	return s
}

func schemaFromCacheOrWarn(cached any) *jsonschema.Schema {
	sch, ok := cached.(*jsonschema.Schema)
	if !ok {
		slog.Warn("handler: expected *jsonschema.Schema in cache")
		return nil
	}
	return sch
}

func lruCacheFromAny(v any) *util.S3FIFOCache[string, *mcp.Tool] {
	c, ok := v.(*util.S3FIFOCache[string, *mcp.Tool])
	if !ok {
		slog.Warn("handler: expected S3FIFO tool cache type assertion failed")
		return nil
	}
	return c
}

func newLRUOrWarn[K comparable, V any](size int) *lru.Cache[K, V] {
	c, err := lru.New[K, V](size)
	if err != nil {
		slog.Warn("handler: failed to create LRU cache", "size", size, "error", err)
		return nil
	}
	return c
}

func homeDirOrDot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("handler: user home dir unavailable", "error", err)
		return "."
	}
	return home
}

func relPathOrBase(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		slog.Warn("handler: filepath.Rel failed", "base", base, "path", path, "error", err)
		return path
	}
	return rel
}

func schemaFromStoreOrWarn(store *db.Store, hash string) map[string]any {
	if store == nil || hash == "" {
		return nil
	}
	schema, err := store.GetSchema(hash)
	if err != nil {
		slog.Warn("handler: GetSchema failed", "hash", hash, "error", err)
		return nil
	}
	return schema
}

func searchToolsOrEmpty(
	ctx context.Context,
	store *db.Store,
	query, category, serverConstraint string,
	scoreThreshold, alpha float64,
	domain db.SearchDomain,
	skipVector bool,
) []*db.ToolRecord {
	if store == nil {
		return nil
	}
	records, err := store.SearchTools(ctx, query, category, serverConstraint, scoreThreshold, alpha, domain, skipVector)
	if err != nil {
		slog.Warn("handler: SearchTools failed", "query", query, "error", err)
		return nil
	}
	return records
}

func writeFileOrWarn(path string, data []byte, perm os.FileMode) {
	if path == "" {
		return
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		slog.Warn("handler: WriteFile failed", "path", path, "error", err)
	}
}

func mkdirAllOrWarn(path string, perm os.FileMode) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(path, perm); err != nil {
		slog.Warn("handler: MkdirAll failed", "path", path, "error", err)
	}
}

func walkDirOrWarn(root string, fn filepath.WalkFunc) {
	if root == "" {
		return
	}
	if err := filepath.Walk(root, fn); err != nil {
		slog.Warn("handler: filepath.Walk failed", "root", root, "error", err)
	}
}

func saveToRecallOrWarn(ctx context.Context, client *mcplib.RecallClient, sessionID, target string, data map[string]any) {
	if client == nil {
		return
	}
	if err := client.SaveToRecall(ctx, sessionID, target, data); err != nil {
		slog.Warn("handler: SaveToRecall failed", "session_id", sessionID, "error", err)
	}
}

func reloadServersOrWarn(h *OrchestratorHandler) {
	if h == nil {
		return
	}
	if _, err := h.ReloadServers(context.Background(), &mcp.CallToolRequest{}); err != nil {
		slog.Warn("handler: ReloadServers failed", "error", err)
	}
}

func selectiveReloadOrWarn(ctx context.Context, h *OrchestratorHandler, names []string) {
	if h == nil {
		return
	}
	_ = h.executeSelectiveReload(ctx, names)
}

func recoverOrWarn() {
	if r := recover(); r != nil {
		slog.Warn("handler: recovered from panic", "panic", r)
	}
}

func combinedOutputOrEmpty(cmd interface{ CombinedOutput() ([]byte, error) }) []byte {
	if cmd == nil {
		return nil
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warn("handler: command CombinedOutput failed", "error", err)
	}
	return out
}
