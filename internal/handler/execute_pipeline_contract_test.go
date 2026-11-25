package handler

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pipelineContractEnv is a handler with the pipeline gate open and a counting
// executor seam: every stage execution path increments calls. Tests that must
// prove "nothing ran" assert calls == 0.
type pipelineContractEnv struct {
	h     *OrchestratorHandler
	calls *atomic.Int32
}

func newPipelineContractEnv(t *testing.T) *pipelineContractEnv {
	t.Helper()
	h, store, _, tmpDir := newTestHandler(t)
	t.Cleanup(func() { _ = store.Close() })
	_ = tmpDir
	h.PipelineEnabled = &atomic.Bool{}
	h.PipelineEnabled.Store(true)
	calls := &atomic.Int32{}
	h.pipelineTestCountStep = func() { calls.Add(1) }
	// Seed the pipeline tools the composer searches, as the dry-run test does.
	for _, tool := range []*db.ToolRecord{
		{URN: "brainstorm:analyzer", Name: "analyzer", Server: "brainstorm", Description: "Analyzes and refactors code", Role: "ANALYZER", Phase: 1},
		{URN: "go-modernizer:planner", Name: "planner", Server: "go-modernizer", Description: "Plans the refactor of the code", Role: "PLANNER", Phase: 2},
	} {
		if err := store.SaveTool(tool); err != nil {
			t.Fatal(err)
		}
	}
	// Bind the session store eagerly so tests can reach it.
	h.pipelineSessionStore()
	return &pipelineContractEnv{h: h, calls: calls}
}

func (e *pipelineContractEnv) call(t *testing.T, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{}}
	req.Params.Arguments = raw
	res, err := e.h.handleExecutePipeline(context.Background(), req)
	if err != nil {
		t.Fatalf("handleExecutePipeline: %v", err)
	}
	return res
}

func resultText(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// gateOutcome is the pure argument gate's classification of one argument set.
// The contract table asserts the gate returns exactly this for each row.
type gateOutcome struct {
	mode   string // "fresh", "dry_run", "continue", "reject", "refuse"
	reason string // refusal vocabulary, empty when not refused
}

// contractRow is one argument combination and its expected gate outcome.
type contractRow struct {
	name string
	args map[string]any
	want gateOutcome
}

// executePipelineContract is the one table (0005's R8 method, re-derived
// against the flat schema of 0013). The schema column accepts every well-typed
// row; the gate column carries the mode rules.
func executePipelineContract() []contractRow {
	return []contractRow{
		{"none", map[string]any{}, gateOutcome{"refuse", "bad_arguments"}},
		{"session alone", map[string]any{"session_id": "ep-x"}, gateOutcome{"continue", ""}},
		{"session+plan_hash", map[string]any{"session_id": "ep-x", "plan_hash": "abc"}, gateOutcome{"continue", ""}},
		{"session+reject:false", map[string]any{"session_id": "ep-x", "reject": false}, gateOutcome{"continue", ""}},
		{"session+reject:true", map[string]any{"session_id": "ep-x", "reject": true}, gateOutcome{"reject", ""}},
		{"reject:true alone", map[string]any{"reject": true}, gateOutcome{"refuse", "reject_needs_session"}},
		{"reject:false alone", map[string]any{"reject": false}, gateOutcome{"refuse", "bad_arguments"}},
		{"session+dry_run", map[string]any{"session_id": "ep-x", "dry_run": true}, gateOutcome{"refuse", "bad_arguments"}},
		{"session+target_roles", map[string]any{"session_id": "ep-x", "target_roles": []string{"ANALYZER"}}, gateOutcome{"refuse", "bad_arguments"}},
		{"target alone", map[string]any{"target": "/tmp/x"}, gateOutcome{"refuse", "bad_arguments"}},
		{"intent alone", map[string]any{"intent": "analyze"}, gateOutcome{"refuse", "bad_arguments"}},
		{"target+session", map[string]any{"target": "/tmp/x", "session_id": "ep-x"}, gateOutcome{"refuse", "bad_arguments"}},
		{"intent+session", map[string]any{"intent": "analyze", "session_id": "ep-x"}, gateOutcome{"refuse", "bad_arguments"}},
		{"target+intent", map[string]any{"target": "/tmp/x", "intent": "analyze"}, gateOutcome{"fresh", ""}},
		{"target+intent+plan_hash+roles", map[string]any{"target": "/tmp/x", "intent": "analyze", "plan_hash": "abc", "target_roles": []string{"ANALYZER"}}, gateOutcome{"fresh", ""}},
		{"target+intent+dry_run:false", map[string]any{"target": "/tmp/x", "intent": "analyze", "dry_run": false}, gateOutcome{"fresh", ""}},
		{"target+intent+dry_run:true", map[string]any{"target": "/tmp/x", "intent": "analyze", "dry_run": true}, gateOutcome{"dry_run", ""}},
		{"all-three", map[string]any{"target": "/tmp/x", "intent": "analyze", "session_id": "ep-x"}, gateOutcome{"fresh", ""}},
		{"all-three+dry_run", map[string]any{"target": "/tmp/x", "intent": "analyze", "session_id": "ep-x", "dry_run": true}, gateOutcome{"dry_run", ""}},
		{"target+intent+reject:true", map[string]any{"target": "/tmp/x", "intent": "analyze", "reject": true}, gateOutcome{"refuse", "bad_arguments"}},
		{"target+intent+reject:false", map[string]any{"target": "/tmp/x", "intent": "analyze", "reject": false}, gateOutcome{"refuse", "bad_arguments"}},
		{"all-three+reject:true", map[string]any{"target": "/tmp/x", "intent": "analyze", "session_id": "ep-x", "reject": true}, gateOutcome{"refuse", "bad_arguments"}},
		{"session+reject:true+dry_run", map[string]any{"session_id": "ep-x", "reject": true, "dry_run": true}, gateOutcome{"refuse", "bad_arguments"}},
		{"unknown key", map[string]any{"target": "/tmp/x", "intent": "analyze", "bogus": 1}, gateOutcome{"refuse", "bad_arguments"}},
		{"dry_run as string", map[string]any{"target": "/tmp/x", "intent": "analyze", "dry_run": "true"}, gateOutcome{"refuse", "bad_arguments"}},
		{"target_roles non-string", map[string]any{"target": "/tmp/x", "intent": "analyze", "target_roles": []any{1}}, gateOutcome{"refuse", "bad_arguments"}},
	}
}

func TestExecutePipelineContract_Gate(t *testing.T) {
	for _, row := range executePipelineContract() {
		t.Run(row.name, func(t *testing.T) {
			raw, err := json.Marshal(row.args)
			if err != nil {
				t.Fatal(err)
			}
			got := classifyPipelineArgs(raw)
			if got.mode != row.want.mode || got.reason != row.want.reason {
				t.Fatalf("gate: got {%s %s}, want {%s %s}", got.mode, got.reason, row.want.mode, row.want.reason)
			}
		})
	}
}

func TestExecutePipelineContract_RejectNeverExecutes(t *testing.T) {
	for _, row := range executePipelineContract() {
		isRejectRow := row.want.mode == "reject" ||
			(row.want.mode == "refuse" && row.args["reject"] == true)
		if !isRejectRow {
			continue
		}
		t.Run(row.name, func(t *testing.T) {
			e := newPipelineContractEnv(t)
			res := e.call(t, row.args)
			if e.calls.Load() != 0 {
				t.Fatalf("reject row executed %d stages, want 0", e.calls.Load())
			}
			_ = res
		})
	}
}

func TestExecutePipelineContract_DryRunNeverExecutes(t *testing.T) {
	e := newPipelineContractEnv(t)
	res := e.call(t, map[string]any{"target": t.TempDir(), "intent": "analyze the code", "dry_run": true})
	if e.calls.Load() != 0 {
		t.Fatalf("dry run executed %d stages, want 0", e.calls.Load())
	}
	text := resultText(res)
	if !strings.Contains(text, "session_id") {
		t.Fatalf("dry run must return an issued session_id; got:\n%s", text)
	}
}

func TestExecutePipelineContract_FreshGeneratesID(t *testing.T) {
	e := newPipelineContractEnv(t)
	// No downstream servers are configured in the test env, so the run fails
	// at lazy-boot; the issued id must still be returned in the envelope.
	res := e.call(t, map[string]any{"target": t.TempDir(), "intent": "analyze the code"})
	text := resultText(res)
	if !regexp.MustCompile(`ep-[0-9a-f]{32}`).MatchString(text) {
		t.Fatalf("fresh run must return a server-issued 128-bit ep- id; got:\n%s", text)
	}
}

func TestExecutePipelineContract_RejectStoredSessionNeverExecutes(t *testing.T) {
	e := newPipelineContractEnv(t)
	ctx := context.Background()
	ps := e.h.pipelineSessions
	rec := &pipelineSessionRecord{
		SessionID: "ep-reject-zero", Target: "/tmp/x", Intent: "analyze",
		Status: "PLANNED",
		Stages: []db.PipelineStage{{ToolName: "brainstorm:analyzer", Role: "ANALYZER", Phase: 1}},
	}
	if err := createPlannedForTest(ctx, ps, rec); err != nil {
		t.Fatal(err)
	}
	res := e.call(t, map[string]any{"session_id": "ep-reject-zero", "reject": true})
	if e.calls.Load() != 0 {
		t.Fatalf("rejection of a stored session executed %d stages, want 0", e.calls.Load())
	}
	if !strings.Contains(resultText(res), "REJECTED") {
		t.Fatalf("want REJECTED result; got:\n%s", resultText(res))
	}
	// A repeated rejection returns the same REJECTED result, still zero calls.
	res = e.call(t, map[string]any{"session_id": "ep-reject-zero", "reject": true})
	if e.calls.Load() != 0 {
		t.Fatalf("repeated rejection executed %d stages", e.calls.Load())
	}
	if !strings.Contains(resultText(res), "REJECTED") {
		t.Fatalf("repeated reject: want REJECTED; got:\n%s", resultText(res))
	}
}

func TestExecutePipelineContract_UnknownSession(t *testing.T) {
	e := newPipelineContractEnv(t)
	res := e.call(t, map[string]any{"session_id": "ep-does-not-exist"})
	text := resultText(res)
	if !strings.Contains(text, "unknown_session") {
		t.Fatalf("want unknown_session refusal; got:\n%s", text)
	}
	if e.calls.Load() != 0 {
		t.Fatalf("unknown session executed %d stages", e.calls.Load())
	}
}

func TestExecutePipelineContract_RejectNeedsSession(t *testing.T) {
	e := newPipelineContractEnv(t)
	res := e.call(t, map[string]any{"reject": true})
	if !strings.Contains(resultText(res), "reject_needs_session") {
		t.Fatalf("want reject_needs_session; got:\n%s", resultText(res))
	}
	if e.calls.Load() != 0 {
		t.Fatal("reject without session executed stages")
	}
}

func TestExecutePipelineContract_BadArguments(t *testing.T) {
	e := newPipelineContractEnv(t)
	res := e.call(t, map[string]any{"target": "/tmp/x", "intent": "analyze", "bogus": 1})
	if !strings.Contains(resultText(res), "bad_arguments") {
		t.Fatalf("want bad_arguments; got:\n%s", resultText(res))
	}
	if e.calls.Load() != 0 {
		t.Fatal("bad arguments executed stages")
	}
}
