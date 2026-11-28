package handler

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
	"github.com/maccavelli/mcp-server-magictools/internal/db"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// saveSearchLikeTool persists a tool shaped like the 2026-11-27 incident:
// a required string field ("query") and an optional field. The schema is
// stored under its hash so both the hydration and compile paths run.
func saveSearchLikeTool(t *testing.T, h *OrchestratorHandler, store *db.Store, urn, server, name string) *db.ToolRecord {
	t.Helper()
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":       map[string]any{"type": "string"},
			"max_results": map[string]any{"type": "integer"},
		},
		"required":             []any{"query"},
		"additionalProperties": false,
	}
	hash := "testhash-" + name
	if err := store.SaveSchema(hash, schema); err != nil {
		t.Fatalf("SaveSchema: %v", err)
	}
	rec := &db.ToolRecord{
		URN:        urn,
		Name:       name,
		Server:     server,
		SchemaHash: hash,
	}
	if err := store.SaveTool(rec); err != nil {
		t.Fatalf("SaveTool: %v", err)
	}
	return rec
}

// TestS1_MissingRequiredRejected_CallProxy is the 0017 regression: a call
// with arguments {} against a schema requiring "query" must produce the
// Tier-2 structured correction, not a dispatched call with query "".
func TestS1_MissingRequiredRejected_CallProxy(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	rec := saveSearchLikeTool(t, h, store, "test-server:search_web", "test-server", "search_web")
	ps := NewProxyService(h)
	args := map[string]any{}
	err := ps.ValidateArguments(context.Background(), rec.URN, rec, args)
	if err == nil {
		t.Fatalf("expected MISSING_REQUIRED_FIELDS error, got nil; args after validation: %v", args)
	}
	if !strings.Contains(err.Error(), errorMissingRequiredFields) {
		t.Fatalf("expected %s in error, got: %v", errorMissingRequiredFields, err)
	}
	if !strings.Contains(err.Error(), "query") {
		t.Fatalf("expected missing field 'query' named in error, got: %v", err)
	}
	if _, forged := args["query"]; forged {
		t.Fatalf("required field 'query' was fabricated before rejection: %v", args)
	}
}

// registerReadyNoSession marks a managed server READY with no live session.
// If the firewall lets a call through, dispatch fails with "not running"
// instead of silently reaching a sub-server — the test can tell rejection
// from dispatch without starting any process.
func registerReadyNoSession(h *OrchestratorHandler, name string) {
	h.Registry.Servers[name] = &client.SubServer{Name: name, Status: client.StatusReady}
}

// TestS1_MissingRequiredRejected_Namespaced pins S1 on the namespaced
// direct-call path: the middleware must return the correction and never
// reach dispatch.
func TestS1_MissingRequiredRejected_Namespaced(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	saveSearchLikeTool(t, h, store, "test-server:search_web", "test-server", "search_web")
	registerReadyNoSession(h, "test-server")
	h.Config.ValidateProxyCalls = true

	mw := h.CallToolMiddleware(func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		t.Fatal("next handler must not be reached for a namespaced call")
		return nil, nil
	})
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{
			Name:      "test-server:search_web",
			Arguments: map[string]any{"max_results": 5},
		},
	}
	res, err := mw(context.Background(), "tools/call", req)
	if err != nil {
		t.Fatalf("expected an error result, got transport error: %v", err)
	}
	ctr := res.(*mcp.CallToolResult)
	if !ctr.IsError {
		t.Fatalf("expected IsError result, got: %+v", ctr)
	}
	text := ctr.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, errorMissingRequiredFields) || !strings.Contains(text, "query") {
		t.Fatalf("expected correction naming 'query', got: %s", text)
	}
}

// TestS1_MissingRequiredRejected_AlignInline pins S1 on the align_tools
// inline path: validation failure falls back to discovery with
// skip_reason=validation_failure; no execution is attempted.
func TestS1_MissingRequiredRejected_AlignInline(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	saveSearchLikeTool(t, h, store, "test-server:search_web", "test-server", "search_web")
	registerReadyNoSession(h, "test-server")
	h.Config.ValidateProxyCalls = true
	h.Config.TrustServers = []string{"test-server"}
	h.Config.ScoreThreshold = 0.5

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name: "align_tools",
			Arguments: json.RawMessage(`{"query": "test-server:search_web", "server_name": "test-server", "arguments": {"max_results": 5}}`),
		},
	}
	res, err := h.AlignTools(context.Background(), req)
	if err != nil {
		t.Fatalf("AlignTools: %v", err)
	}
	text := alignResultText(res)
	payload := alignExtractJSONPayload(text)
	var envelope struct {
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		t.Fatalf("discovery envelope did not parse: %v\n%s", err, text)
	}
	if envelope.Metadata["mode"] != "discovery" {
		t.Fatalf("expected discovery fallback, got mode %v", envelope.Metadata["mode"])
	}
	if envelope.Metadata["skip_reason"] != "validation_failure" {
		t.Fatalf("expected skip_reason validation_failure, got %v", envelope.Metadata["skip_reason"])
	}
}

// TestS2_CoercibleRequiredFieldStillValidates pins S2: a required field
// present with a coercible wrong type is repaired, not rejected.
func TestS2_CoercibleRequiredFieldStillValidates(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	rec := saveSearchLikeTool(t, h, store, "test-server:search_web", "test-server", "search_web")
	ps := NewProxyService(h)
	args := map[string]any{"query": 42}
	if err := ps.ValidateArguments(context.Background(), rec.URN, rec, args); err != nil {
		t.Fatalf("coercible required field must validate, got: %v", err)
	}
	if args["query"] != "42" {
		t.Fatalf("expected coerced \"42\", got %v", args["query"])
	}
}

// TestS3_ExtraFieldsStillStripped pins S3: hallucinated extra fields are
// removed while the valid required field passes.
func TestS3_ExtraFieldsStillStripped(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	rec := saveSearchLikeTool(t, h, store, "test-server:search_web", "test-server", "search_web")
	ps := NewProxyService(h)
	args := map[string]any{"query": "cats", "hallucinated": true}
	if err := ps.ValidateArguments(context.Background(), rec.URN, rec, args); err != nil {
		t.Fatalf("valid call with extras must pass after stripping, got: %v", err)
	}
	if _, ok := args["hallucinated"]; ok {
		t.Fatalf("extra field not stripped: %v", args)
	}
}

// TestS4_OptionalDefaultInjected pins S4: an absent optional field with an
// explicit schema default receives that default.
func TestS4_OptionalDefaultInjected(t *testing.T) {
	ps := NewProxyService(nil)
	record := &db.ToolRecord{
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"limit": map[string]any{"type": "integer", "default": 10},
			},
			"required": []any{"query"},
		},
	}
	args := map[string]any{"query": "cats"}
	ps.AutoCoerceArguments(record, args)
	if args["limit"] != 10 {
		t.Fatalf("expected default limit 10, got %v", args["limit"])
	}
}

// TestS5_RequiredWithDefaultStillRejected pins S5: a required field that
// also declares a default is a contradictory schema; absence is surfaced,
// not guessed.
func TestS5_RequiredWithDefaultStillRejected(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "default": "cats"},
		},
		"required": []any{"query"},
	}
	if err := store.SaveSchema("testhash-s5", schema); err != nil {
		t.Fatal(err)
	}
	rec := &db.ToolRecord{URN: "test-server:s5", Name: "s5", Server: "test-server", SchemaHash: "testhash-s5"}
	if err := store.SaveTool(rec); err != nil {
		t.Fatal(err)
	}
	ps := NewProxyService(h)
	args := map[string]any{}
	err := ps.ValidateArguments(context.Background(), rec.URN, rec, args)
	if err == nil || !strings.Contains(err.Error(), errorMissingRequiredFields) {
		t.Fatalf("required-with-default absence must be rejected, got: %v", err)
	}
	if _, forged := args["query"]; forged {
		t.Fatalf("required field fabricated from its default: %v", args)
	}
}

// TestS6_ZeroValuesNotAnInjectionSource pins S6: a ZeroValues entry for a
// non-required key without a schema default is never injected.
func TestS6_ZeroValuesNotAnInjectionSource(t *testing.T) {
	ps := NewProxyService(nil)
	record := &db.ToolRecord{
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"extra": map[string]any{"type": "string"},
			},
			"required": []any{"query"},
		},
		ZeroValues: map[string]any{"query": "", "extra": "legacy-zero"},
	}
	args := map[string]any{"query": "cats"}
	ps.AutoCoerceArguments(record, args)
	if _, ok := args["extra"]; ok {
		t.Fatalf("ZeroValues must not inject a key without a schema default: %v", args)
	}
}

// TestS7_NestedRequiredRejectedByValidator pins S7: a missing nested
// required field is rejected by the compiled validator after coercion.
func TestS7_NestedRequiredRejectedByValidator(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
			"opts": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"depth": map[string]any{"type": "integer"},
				},
				"required": []any{"depth"},
			},
		},
		"required": []any{"query"},
	}
	if err := store.SaveSchema("testhash-s7", schema); err != nil {
		t.Fatal(err)
	}
	rec := &db.ToolRecord{URN: "test-server:s7", Name: "s7", Server: "test-server", SchemaHash: "testhash-s7"}
	if err := store.SaveTool(rec); err != nil {
		t.Fatal(err)
	}
	ps := NewProxyService(h)
	args := map[string]any{"query": "cats", "opts": map[string]any{}}
	err := ps.ValidateArguments(context.Background(), rec.URN, rec, args)
	if err == nil {
		t.Fatal("missing nested required field must be rejected by the compiled validator")
	}
	if !strings.Contains(err.Error(), "depth") {
		t.Fatalf("expected nested field 'depth' named, got: %v", err)
	}
}

// TestS8_ValidationOffDoesNotFabricateRequired pins S8: with validation
// disabled the coercion path still runs, but never invents the required key.
func TestS8_ValidationOffDoesNotFabricateRequired(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	rec := saveSearchLikeTool(t, h, store, "test-server:search_web", "test-server", "search_web")
	ps := NewProxyService(h)
	ps.hydrateRecord(rec)
	args := map[string]any{}
	ps.CoerceAndStrip(rec, args)
	if _, forged := args["query"]; forged {
		t.Fatalf("validation-off path fabricated the required key: %v", args)
	}
}

// TestS9_EmptySchemaHashEarlyReturn pins S9: no schema hash, no check —
// the existing early return stands.
func TestS9_EmptySchemaHashEarlyReturn(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	rec := &db.ToolRecord{URN: "test-server:s9", Name: "s9", Server: "test-server"}
	if err := store.SaveTool(rec); err != nil {
		t.Fatal(err)
	}
	ps := NewProxyService(h)
	if err := ps.ValidateArguments(context.Background(), rec.URN, rec, map[string]any{}); err != nil {
		t.Fatalf("empty SchemaHash must keep the early return, got: %v", err)
	}
}

// TestS10_SchemaInfraErrorRoleSplit pins S10 through ValidateArguments:
// a hash whose schema cannot be fetched fails closed for a mutator and
// proceeds for a read-only tool.
func TestS10_SchemaInfraErrorRoleSplit(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	ps := NewProxyService(h)
	mutator := &db.ToolRecord{URN: "test-server:mut", Name: "mut", Server: "test-server", SchemaHash: "absent-hash", Role: roleMutator}
	if err := ps.ValidateArguments(context.Background(), mutator.URN, mutator, map[string]any{}); err == nil {
		t.Fatal("mutator must fail closed when the schema cannot be fetched")
	}
	reader := &db.ToolRecord{URN: "test-server:read", Name: "read", Server: "test-server", SchemaHash: "absent-hash"}
	if err := ps.ValidateArguments(context.Background(), reader.URN, reader, map[string]any{}); err != nil {
		t.Fatalf("read-only tool must proceed on schema infrastructure error, got: %v", err)
	}
}

// TestS11_NilArgsRejected pins S11: a nil argument map with a non-empty
// required list is treated as empty and rejected.
func TestS11_NilArgsRejected(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	rec := saveSearchLikeTool(t, h, store, "test-server:search_web", "test-server", "search_web")
	ps := NewProxyService(h)
	err := ps.ValidateArguments(context.Background(), rec.URN, rec, nil)
	if err == nil || !strings.Contains(err.Error(), errorMissingRequiredFields) {
		t.Fatalf("nil args with required fields must be rejected, got: %v", err)
	}
}