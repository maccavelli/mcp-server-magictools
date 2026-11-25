package handler

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// emittedToolsList registers a handler as the binary does and reads tools/list
// over the SDK's in-memory line-delimited transport (0005's R1 method).
func emittedToolsList(t *testing.T, h *OrchestratorHandler) []*mcp.Tool {
	t.Helper()
	ctx := context.Background()
	s := mcp.NewServer(&mcp.Implementation{Name: "magictools-test", Version: "0"}, nil)
	h.Register(s)
	h.RegisterPipelineTools(s)
	h.RegisterPrompts(s)

	ct, st := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res.Tools
}

func findTool(tools []*mcp.Tool, name string) *mcp.Tool {
	for _, tl := range tools {
		if tl.Name == name {
			return tl
		}
	}
	return nil
}

func schemaMap(t *testing.T, tl *mcp.Tool) map[string]any {
	t.Helper()
	raw, err := json.Marshal(tl.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestExecutePipelineSchema_FlatNoUnion is the union guard (0013 D1/D4): no own
// tool's inputSchema root holds anyOf/oneOf/allOf, and execute_pipeline's root
// is the flat seven-property object.
func TestExecutePipelineSchema_FlatNoUnion(t *testing.T) {
	h, store, _, _ := newTestHandler(t)
	defer store.Close()
	tools := emittedToolsList(t, h)

	for _, tl := range tools {
		m := schemaMap(t, tl)
		for _, kw := range []string{"anyOf", "oneOf", "allOf"} {
			if _, has := m[kw]; has {
				t.Errorf("tool %s: root holds %s — refused by Anthropic's Messages API", tl.Name, kw)
			}
		}
	}

	ep := findTool(tools, "execute_pipeline")
	if ep == nil {
		t.Fatal("execute_pipeline not in tools/list")
	}
	m := schemaMap(t, ep)
	props, _ := m["properties"].(map[string]any)
	want := []string{"session_id", "target", "intent", "plan_hash", "dry_run", "target_roles", "reject"}
	if len(props) != len(want) {
		t.Fatalf("execute_pipeline properties: got %d, want exactly %d (%v)", len(props), len(want), want)
	}
	for _, w := range want {
		if _, has := props[w]; !has {
			t.Errorf("execute_pipeline missing property %s", w)
		}
	}
	if ap, _ := m["additionalProperties"].(bool); ap {
		t.Error("execute_pipeline additionalProperties must be false")
	}
	if _, has := m["required"]; has {
		t.Error("execute_pipeline root must carry no required list (flat, all-optional)")
	}
}

// TestExecutePipelineContract_SchemaAcceptsWellTypedRows: the flat schema
// accepts every well-typed row of the contract table and rejects unknown keys
// and wrong types. Mode rules are the gate's, not the schema's.
func TestExecutePipelineContract_SchemaAcceptsWellTypedRows(t *testing.T) {
	h, store, _, _ := newTestHandler(t)
	defer store.Close()
	tools := emittedToolsList(t, h)
	ep := findTool(tools, "execute_pipeline")
	if ep == nil {
		t.Fatal("execute_pipeline not found")
	}
	raw, err := json.Marshal(ep.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := jsonschemaCompile(string(raw))
	if err != nil {
		t.Fatalf("schema does not compile: %v", err)
	}

	for _, row := range executePipelineContract() {
		t.Run(row.name, func(t *testing.T) {
			argsRaw, _ := json.Marshal(row.args)
			var v any
			_ = json.Unmarshal(argsRaw, &v)
			err := compiled.Validate(v)
			// A row is schema-valid when its refusal (if any) is a mode rule,
			// not a type/unknown-key/empty violation.
			schemaShouldAccept := !isSchemaInvalidRow(row)
			if schemaShouldAccept && err != nil {
				t.Fatalf("schema rejected well-typed row %s: %v", row.name, err)
			}
			if !schemaShouldAccept && err == nil {
				t.Fatalf("schema accepted ill-typed row %s", row.name)
			}
		})
	}
}

// isTypeOrKeyRefusal reports whether a row's refusal is a type/unknown-key
// violation the schema itself must catch (rather than a mode rule).
func isTypeOrKeyRefusal(row contractRow) bool {
	switch row.name {
	case "unknown key", "dry_run as string", "target_roles non-string":
		return true
	}
	return false
}

// isSchemaInvalidRow reports whether the flat schema must reject the row: an
// unknown key, a wrong type, or an empty argument set (the schema requires at
// least one property via minProperties).
func isSchemaInvalidRow(row contractRow) bool {
	return isTypeOrKeyRefusal(row) || row.name == "none"
}

// TestExecutePipelineDryRun_Disclosure pins the fixed disclosure block of
// 0005's D2 (Decided 9), word for word, with the issued id and expiry filled in.
func TestExecutePipelineDryRun_Disclosure(t *testing.T) {
	e := newPipelineContractEnv(t)
	res := e.call(t, map[string]any{"target": t.TempDir(), "intent": "analyze the code", "dry_run": true})
	text := resultText(res)

	for _, want := range []string{
		"Approval of this plan covers the analysis stages listed above, not file writes.",
		"Mutating stages are not part of this plan: they are added at run time, after the",
		"reporting stage, when the Socratic verdict and the analysis call for them. With no",
		"plan_hash, mutation proceeds under the placeholder approval",
		`"auto-approved-orchestrator-override".`,
		"To continue: call execute_pipeline with session_id",
		"To discard: call it with session_id",
		"and reject: true.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("disclosure missing fixed words: %q\nfull text:\n%s", want, text)
		}
	}
	// The issued id is ep- + 32 hex chars (128 bits).
	if !regexp.MustCompile(`ep-[0-9a-f]{32}`).MatchString(text) {
		t.Errorf("no 128-bit ep- id in dry-run result:\n%s", text)
	}
}

// TestExecutePipelineDryRun_MutatorPlanNotStored: a plan holding a MUTATOR-role
// or apply_vetted_edit stage is shown but not stored, and says so.
func TestExecutePipelineDryRun_MutatorPlanNotStored(t *testing.T) {
	e := newPipelineContractEnv(t)
	// Force a mutator stage into the composed plan via the test seam.
	e.h.pipelineTestInjectStage = &PipelineStep{ToolName: "go-modernizer:apply_vetted_edit", Role: "MUTATOR", Phase: 8}
	res := e.call(t, map[string]any{"target": t.TempDir(), "intent": "refactor the code", "dry_run": true})
	text := resultText(res)
	if !strings.Contains(text, "not stored") {
		t.Fatalf("mutator plan must say it is not stored; got:\n%s", text)
	}
	if e.calls.Load() != 0 {
		t.Fatalf("dry run executed %d stages", e.calls.Load())
	}
}

// TestExecutePipelineContinuation_Refusals pins each refusal vocabulary word.
func TestExecutePipelineContinuation_Refusals(t *testing.T) {
	e := newPipelineContractEnv(t)
	ctx := context.Background()
	ps := e.h.pipelineSessions
	if ps == nil {
		t.Fatal("handler has no pipeline session store")
	}

	mk := func(id string) {
		t.Helper()
		rec := &pipelineSessionRecord{
			SessionID: id, Target: "/tmp/x", Intent: "analyze",
			Status: "PLANNED",
			Stages: []db.PipelineStage{{ToolName: "brainstorm:analyzer", Role: "ANALYZER", Phase: 1}},
		}
		if err := createPlannedForTest(ctx, ps, rec); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name  string
		setup func(id string)
		args  map[string]any
		want  string
	}{
		{"unknown_session", func(string) {}, map[string]any{"session_id": "ep-nope"}, "unknown_session"},
		{"session_rejected", func(id string) {
			mk(id)
			if _, err := rejectForTest(ctx, ps, id); err != nil {
				t.Fatal(err)
			}
		}, map[string]any{"session_id": "ep-rej"}, "session_rejected"},
		{"plan_hash_mismatch", func(id string) {
			mk(id)
			setPlanHashForTest(ctx, ps, id, "stored-hash")
		}, map[string]any{"session_id": "ep-ph", "plan_hash": "other-hash"}, "plan_hash_mismatch"},
		{"plan_stale", func(id string) {
			mk(id)
			setStagesForTest(ctx, ps, id, []PipelineStep{{ToolName: "gone:tool", Role: "ANALYZER", Phase: 1}})
		}, map[string]any{"session_id": "ep-stale"}, "plan_stale"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, _ := tc.args["session_id"].(string)
			tc.setup(id)
			before := e.calls.Load()
			res := e.call(t, tc.args)
			text := resultText(res)
			if !strings.Contains(text, tc.want) {
				t.Fatalf("want %s; got:\n%s", tc.want, text)
			}
			if e.calls.Load() != before {
				t.Fatalf("%s executed stages", tc.name)
			}
		})
	}
}
