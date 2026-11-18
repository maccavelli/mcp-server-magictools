package handler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/util"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Characterisation of ListToolsMiddleware: one subtest per row L01 to L23 of the
// behaviour table in docs/decisions/0008-MADR-redesign-the-list-tools-middleware-
// under-the-complexity-limit.md, plus an end-to-end test over a real in-memory
// session (row E-1). Written against the closure at 310a541; every row must keep
// passing, unchanged, after the redesign. A row that pins a behaviour the record
// calls a defect (F1, F2, F4, F5) pins it on purpose: the fix is a later record.

type lsFakeSession struct {
	*mcp.ServerSession
	id string
}

func (f lsFakeSession) ID() string { return f.id }

type lsFakeReq struct {
	*mcp.ServerRequest[*mcp.ListToolsParams]
	sess mcp.Session
}

func (f lsFakeReq) GetSession() mcp.Session { return f.sess }

func lsTool(name string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: "d-" + name, InputSchema: map[string]any{"type": "object"}}
}

func lsNames(ts []*mcp.Tool) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		if t == nil {
			out = append(out, "<nil>")
			continue
		}
		out = append(out, t.Name)
	}
	return out
}

func lsNext(tools []*mcp.Tool) mcp.MethodHandler {
	return func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.ListToolsResult{Tools: tools}, nil
	}
}

type lsEnv struct {
	h *OrchestratorHandler
}

func newLSEnv(t *testing.T) *lsEnv {
	t.Helper()
	h, store, _, tmpDir := newTestHandler(t)
	t.Cleanup(func() {
		store.Close()
		os.RemoveAll(tmpDir)
	})
	return &lsEnv{h: h}
}

func (e *lsEnv) synced(v bool) { e.h.Registry.IsSynced.Store(v) }

func (e *lsEnv) cache(id string, size int, tools ...*mcp.Tool) *util.S3FIFOCache[string, *mcp.Tool] {
	c := util.NewS3FIFOCache[string, *mcp.Tool](size)
	for i, tl := range tools {
		key := "k" + string(rune('a'+i))
		if tl != nil {
			key = tl.Name + "#" + key
		}
		// Added twice: a key seen once never leaves the cache's small queue
		// (capacity/10 entries) with a frequency, and is evicted by the next
		// new key, so a single Add would not hold what the test needs.
		c.Add(key, tl)
		c.Add(key, tl)
	}
	e.h.ActiveSessions.Store(id, c)
	return c
}

func (e *lsEnv) reset() {
	e.synced(false)
	e.h.ActiveSessions.Range(func(k, _ any) bool { e.h.ActiveSessions.Delete(k); return true })
}

func lsList(t *testing.T, mw mcp.MethodHandler, req mcp.Request) (*mcp.ListToolsResult, error) {
	t.Helper()
	res, err := mw(context.Background(), "tools/list", req)
	if res == nil {
		return nil, err
	}
	lr, ok := res.(*mcp.ListToolsResult)
	if !ok {
		t.Fatalf("result type %T, want *mcp.ListToolsResult", res)
	}
	return lr, err
}

func lsEqualNames(t *testing.T, got []*mcp.Tool, want ...string) {
	t.Helper()
	if g := lsNames(got); !reflect.DeepEqual(g, append([]string{}, want...)) {
		t.Fatalf("tools = %v, want %v", g, want)
	}
}

// captureLog swaps the default slog logger for one writing into a buffer.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var mu sync.Mutex
	buf := &bytes.Buffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{mu: &mu, w: buf}, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestListToolsCharacterization(t *testing.T) {
	e := newLSEnv(t)
	rows := []struct {
		name string
		fn   func(*testing.T, *lsEnv)
	}{
		{"L01 other method is passed through untouched, result and error", lsRowL01},
		{"L02 a panic in next for another method propagates (no recover)", lsRowL02},
		{"L03 next receives ctx, method and request unchanged", lsRowL03},
		{"L04 not synced: base layer only, same tool pointers, session cache ignored", lsRowL04},
		{"L05 synced, req nil: session id is active-session; base first, then cache Values order", lsRowL05},
		{"L06 synced, no cache for the session: base only", lsRowL06},
		{"L07 dedup by name against base and among cache entries; nil cache entries skipped", lsRowL07},
		{"L08 a nil entry in the base result is kept in place", lsRowL08},
		{"L09 the middleware adds every cache value: no cap of its own", lsRowL09},
		{"L10 session id comes from a non-nil session, empty id is not mapped", lsRowL10},
		{"L11 a request whose Session interface is nil uses active-session", lsRowL11},
		{"L12 next returns an error: fallback to the static inventory, error swallowed", lsRowL12},
		{"L13 fallback maps name, description and input schema only, and skips nil entries", lsRowL13},
		{"L14 fallback triggers: nil result, wrong type, nil Tools; not on an empty non-nil slice", lsRowL14},
		{"L15 fallback is followed by the session merge when synced", lsRowL15},
		{"L16 the result is a new object: cursor and meta of the base result are dropped", lsRowL16},
		{"L17 a panic in next: recovered, empty non-nil list, nil error, counter and log", lsRowL17},
		{"L18 a panic in the session merge discards the base tools too", lsRowL18},
		{"L19 a panic after next returned an error clears that error", lsRowL19},
		{"L20 a request whose session is a typed nil pointer panics in ID() and is recovered", lsRowL20},
		{"L21 the middleware writes nothing to the session cache", lsRowL21},
		{"L22 next returns a typed nil list result: the nil dereference is recovered, not the fallback", lsRowL22},
		{"L23 spare capacity in the base slice is used: session tools are appended into next's own array", lsRowL23},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) { r.fn(t, e) })
	}
}

func lsRowL01(t *testing.T, e *lsEnv) {
	h := e.h
	ctx := context.Background()
	e.reset()
	wantErr := errors.New("boom")
	marker := &mcp.CallToolResult{}
	var gotMethod string
	mw := h.ListToolsMiddleware(func(_ context.Context, m string, _ mcp.Request) (mcp.Result, error) {
		gotMethod = m
		return marker, wantErr
	})
	res, err := mw(ctx, "tools/call", nil)
	if res != mcp.Result(marker) || !errors.Is(err, wantErr) || gotMethod != "tools/call" {
		t.Fatalf("got (%v, %v) method %q", res, err, gotMethod)
	}
}

func lsRowL02(t *testing.T, e *lsEnv) {
	h := e.h
	ctx := context.Background()
	e.reset()
	mw := h.ListToolsMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) { panic("other") })
	defer func() {
		if r := recover(); r != "other" {
			t.Fatalf("recover() = %v, want the panic to propagate", r)
		}
	}()
	_, _ = mw(ctx, "tools/call", nil)
	t.Fatal("no panic")
}

func lsRowL03(t *testing.T, e *lsEnv) {
	h := e.h
	ctx := context.Background()
	e.reset()
	type key struct{}
	cctx := context.WithValue(ctx, key{}, "v")
	req := &mcp.ServerRequest[*mcp.ListToolsParams]{}
	var gotCtx context.Context
	var gotReq mcp.Request
	var gotMethod string
	mw := h.ListToolsMiddleware(func(c context.Context, m string, r mcp.Request) (mcp.Result, error) {
		gotCtx, gotMethod, gotReq = c, m, r
		return &mcp.ListToolsResult{Tools: []*mcp.Tool{}}, nil
	})
	if _, err := mw(cctx, "tools/list", req); err != nil {
		t.Fatal(err)
	}
	if gotCtx != cctx || gotMethod != "tools/list" || gotReq != mcp.Request(req) {
		t.Fatal("next did not receive the same ctx/method/req")
	}
}

func lsRowL04(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	a, b := lsTool("a"), lsTool("b")
	e.cache("active-session", 20, lsTool("s1"))
	res, err := lsList(t, h.ListToolsMiddleware(lsNext([]*mcp.Tool{a, b})), nil)
	if err != nil {
		t.Fatal(err)
	}
	lsEqualNames(t, res.Tools, "a", "b")
	if res.Tools[0] != a || res.Tools[1] != b {
		t.Fatal("tool pointers were copied")
	}
}

func lsRowL05(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	e.synced(true)
	c := e.cache("active-session", 20, lsTool("s1"), lsTool("s2"), lsTool("s3"))
	want := append([]string{"a"}, lsNames(c.Values())...)
	res, err := lsList(t, h.ListToolsMiddleware(lsNext([]*mcp.Tool{lsTool("a")})), nil)
	if err != nil {
		t.Fatal(err)
	}
	lsEqualNames(t, res.Tools, want...)
	if len(res.Tools) != 4 {
		t.Fatalf("len = %d", len(res.Tools))
	}
	for i, cv := range c.Values() {
		if res.Tools[i+1] != cv {
			t.Fatalf("session tool %d was copied; the cached pointer must be emitted as is", i)
		}
	}
}

func lsRowL06(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	e.synced(true)
	e.cache("another-session", 20, lsTool("s1"))
	res, _ := lsList(t, h.ListToolsMiddleware(lsNext([]*mcp.Tool{lsTool("a")})), nil)
	lsEqualNames(t, res.Tools, "a")
}

func lsRowL07(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	e.synced(true)
	c := e.cache("active-session", 20, lsTool("a"), lsTool("s1"), nil, lsTool("s1"), lsTool("s2"))
	_ = c
	res, _ := lsList(t, h.ListToolsMiddleware(lsNext([]*mcp.Tool{lsTool("a")})), nil)
	got := lsNames(res.Tools)
	count := map[string]int{}
	for _, n := range got {
		count[n]++
	}
	if count["a"] != 1 || count["s1"] != 1 || count["s2"] != 1 || count["<nil>"] != 0 || len(got) != 3 {
		t.Fatalf("tools = %v", got)
	}
	if got[0] != "a" {
		t.Fatalf("base must stay first: %v", got)
	}
}

func lsRowL08(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	res, _ := lsList(t, h.ListToolsMiddleware(lsNext([]*mcp.Tool{lsTool("a"), nil, lsTool("b")})), nil)
	lsEqualNames(t, res.Tools, "a", "<nil>", "b")
}

func lsRowL09(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	e.synced(true)
	var many []*mcp.Tool
	for i := range 30 {
		many = append(many, lsTool("m"+string(rune('A'+i))))
	}
	for _, size := range []int{30, 20} {
		c := e.cache("active-session", size, many...)
		held := len(c.Values())
		if held <= 2 || held > size {
			t.Fatalf("cache of %d holds %d, test premise broken", size, held)
		}
		res, _ := lsList(t, h.ListToolsMiddleware(lsNext([]*mcp.Tool{lsTool("a")})), nil)
		if len(res.Tools) != 1+held {
			t.Fatalf("size %d: len = %d, want 1 + %d held by the cache", size, len(res.Tools), held)
		}
	}
}

func lsRowL10(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	e.synced(true)
	e.cache("sess-1", 20, lsTool("s1"))
	e.cache("active-session", 20, lsTool("act"))
	mk := func(id string) mcp.Request {
		return lsFakeReq{ServerRequest: &mcp.ServerRequest[*mcp.ListToolsParams]{}, sess: lsFakeSession{id: id}}
	}
	mw := h.ListToolsMiddleware(lsNext([]*mcp.Tool{lsTool("a")}))
	res, _ := lsList(t, mw, mk("sess-1"))
	lsEqualNames(t, res.Tools, "a", "s1")
	res, _ = lsList(t, mw, mk(""))
	lsEqualNames(t, res.Tools, "a") // "" is looked up as "", not as "active-session"
	res, _ = lsList(t, mw, mk("unknown"))
	lsEqualNames(t, res.Tools, "a")
}

func lsRowL11(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	e.synced(true)
	e.cache("active-session", 20, lsTool("act"))
	req := lsFakeReq{ServerRequest: &mcp.ServerRequest[*mcp.ListToolsParams]{}, sess: nil}
	res, _ := lsList(t, h.ListToolsMiddleware(lsNext([]*mcp.Tool{lsTool("a")})), req)
	lsEqualNames(t, res.Tools, "a", "act")
}

func lsRowL12(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	logs := captureLog(t)
	mw := h.ListToolsMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.ListToolsResult{Tools: []*mcp.Tool{lsTool("ignored")}}, errors.New("base failed")
	})
	res, err := lsList(t, mw, nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	var want []string
	for _, it := range h.InternalTools {
		want = append(want, it.Name)
	}
	lsEqualNames(t, res.Tools, want...)
	if !strings.Contains(logs.String(), "falling back to static inventory") {
		t.Fatalf("no fallback warning in log: %s", logs.String())
	}
}

func lsRowL13(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	saved := h.InternalTools
	t.Cleanup(func() { h.InternalTools = saved })
	schema := map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}}
	h.InternalTools = []*InternalTool{nil, {Name: "n1", Description: "d1", Category: "cat", InputSchema: schema}, nil}
	res, _ := lsList(t, h.ListToolsMiddleware(lsNext(nil)), nil)
	if len(res.Tools) != 1 {
		t.Fatalf("tools = %v", lsNames(res.Tools))
	}
	got := res.Tools[0]
	want := &mcp.Tool{Name: "n1", Description: "d1", InputSchema: schema}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	// The schema is shared by reference and is not sanitised.
	gm, ok := got.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("schema type %T", got.InputSchema)
	}
	gm["probe"] = true
	if _, shared := schema["probe"]; !shared {
		t.Fatal("fallback copied the schema; it is shared today")
	}
}

func lsRowL14(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	n := len(h.InternalTools)
	cases := []struct {
		name string
		next mcp.MethodHandler
		want int
	}{
		{"nil result nil error", func(context.Context, string, mcp.Request) (mcp.Result, error) { return nil, nil }, n},
		{"wrong result type", func(context.Context, string, mcp.Request) (mcp.Result, error) { return &mcp.CallToolResult{}, nil }, n},
		{"nil Tools", lsNext(nil), n},
		{"empty non-nil Tools", lsNext([]*mcp.Tool{}), 0},
	}
	for _, c := range cases {
		res, err := lsList(t, h.ListToolsMiddleware(c.next), nil)
		if err != nil || len(res.Tools) != c.want {
			t.Fatalf("%s: len=%d err=%v want len %d", c.name, len(res.Tools), err, c.want)
		}
	}
}

func lsRowL15(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	e.synced(true)
	e.cache("active-session", 20, lsTool("s1"))
	res, _ := lsList(t, h.ListToolsMiddleware(lsNext(nil)), nil)
	if len(res.Tools) != len(h.InternalTools)+1 || res.Tools[len(res.Tools)-1].Name != "s1" {
		t.Fatalf("tools = %v", lsNames(res.Tools))
	}
}

func lsRowL16(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	base := &mcp.ListToolsResult{Tools: []*mcp.Tool{lsTool("a")}, NextCursor: "next-page", Meta: mcp.Meta{"k": "v"}}
	mw := h.ListToolsMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) { return base, nil })
	res, _ := lsList(t, mw, nil)
	if res == base || res.NextCursor != "" || res.Meta != nil {
		t.Fatalf("res=%+v", res)
	}
}

func lsRowL17(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	logs := captureLog(t)
	before := telemetry.GetPanicCount()
	mw := h.ListToolsMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) { panic("test panic") })
	res, err := lsList(t, mw, nil)
	if err != nil || res == nil || res.Tools == nil || len(res.Tools) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if telemetry.GetPanicCount() != before+1 {
		t.Fatalf("panic counter %d -> %d", before, telemetry.GetPanicCount())
	}
	out := logs.String()
	if !strings.Contains(out, "PANIC RECOVERY during tools/list") || !strings.Contains(out, "panic=\"test panic\"") || !strings.Contains(out, "stack=") {
		t.Fatalf("log = %s", out)
	}
	// Exactly one recovery record, and it is an error record: a recovered panic must not be demoted to a warning,
	// and must not be logged twice.
	var recovery []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "PANIC RECOVERY during tools/list") {
			recovery = append(recovery, line)
		}
	}
	if len(recovery) != 1 {
		t.Fatalf("want exactly one recovery record, got %d: %s", len(recovery), out)
	}
	if !strings.Contains(recovery[0], "level=ERROR") {
		t.Fatalf("the recovery record is not at ERROR level: %s", recovery[0])
	}
	// Counted over the recovery records only: other goroutines of the package (the background pool) log through the
	// same logger while this row runs, so a count of every ERROR record could fail on their records.
	errors := 0
	for _, line := range recovery {
		if strings.Contains(line, "level=ERROR") {
			errors++
		}
	}
	if errors != 1 {
		t.Fatalf("want exactly one recovery record at ERROR level, got %d: %s", errors, out)
	}
}

func lsRowL18(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	e.synced(true)
	e.h.ActiveSessions.Store("active-session", "not a cache") // lruCacheFromAny returns a nil cache; Values() panics
	before := telemetry.GetPanicCount()
	res, err := lsList(t, h.ListToolsMiddleware(lsNext([]*mcp.Tool{lsTool("a"), lsTool("b")})), nil)
	if err != nil || res == nil || res.Tools == nil || len(res.Tools) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if telemetry.GetPanicCount() != before+1 {
		t.Fatal("panic not counted")
	}
}

func lsRowL19(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	e.synced(true)
	e.h.ActiveSessions.Store("active-session", "not a cache")
	mw := h.ListToolsMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, errors.New("base failed")
	})
	res, err := lsList(t, mw, nil)
	if err != nil || res == nil || len(res.Tools) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func lsRowL20(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	e.synced(true)
	e.cache("active-session", 20, lsTool("act"))
	req := &mcp.ServerRequest[*mcp.ListToolsParams]{} // Session is a nil *ServerSession inside a non-nil interface
	res, err := lsList(t, h.ListToolsMiddleware(lsNext([]*mcp.Tool{lsTool("a")})), req)
	if err != nil || res == nil || len(res.Tools) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// L23 documents an implementation detail, not a contract. When the base result has spare capacity, the session
// tools are appended into that same backing array, past the base length. No client can observe it (the SDK builds a
// new slice for every call), and a later change may remove the aliasing, for example by copying the base slice as
// a defensive measure, and this row with it, without a decision record.
func lsRowL23(t *testing.T, e *lsEnv) {
	e.reset()
	e.synced(true)
	e.cache("active-session", 20, lsTool("s1"))
	backing := make([]*mcp.Tool, 1, 4)
	backing[0] = lsTool("a")
	res, _ := lsList(t, e.h.ListToolsMiddleware(lsNext(backing)), nil)
	lsEqualNames(t, res.Tools, "a", "s1")
	if &res.Tools[0] != &backing[0] {
		t.Fatal("the result no longer shares the base slice's backing array")
	}
	if got := backing[:2][1]; got == nil || got.Name != "s1" {
		t.Fatalf("the session tool was not appended into the base array: %v", got)
	}
}

func lsRowL22(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	logs := captureLog(t)
	before := telemetry.GetPanicCount()
	var typedNil *mcp.ListToolsResult // a non-nil mcp.Result holding a nil pointer
	mw := h.ListToolsMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) { return typedNil, nil })
	res, err := lsList(t, mw, nil)
	if err != nil || res == nil || res.Tools == nil || len(res.Tools) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if telemetry.GetPanicCount() != before+1 {
		t.Fatalf("panic counter %d -> %d: the nil dereference was not recovered", before, telemetry.GetPanicCount())
	}
	if strings.Contains(logs.String(), "falling back to static inventory") {
		t.Fatalf("a typed nil result took the fallback: %s", logs.String())
	}
}

func lsRowL21(t *testing.T, e *lsEnv) {
	h := e.h
	e.reset()
	e.synced(true)
	c := e.cache("active-session", 20, lsTool("s1"), lsTool("s2"))
	before := lsNames(c.Values())
	_, _ = lsList(t, h.ListToolsMiddleware(lsNext([]*mcp.Tool{lsTool("a")})), nil)
	_, _ = lsList(t, h.ListToolsMiddleware(lsNext([]*mcp.Tool{lsTool("a")})), nil)
	if !reflect.DeepEqual(before, lsNames(c.Values())) {
		t.Fatal("cache changed")
	}
}

// The registration the binary uses: the middleware on a real server and a real
// (in-memory, line-delimited) session, whose ID() is empty.
func TestListToolsOverInMemorySession(t *testing.T) {
	e := newLSEnv(t)
	h := e.h
	ctx := context.Background()

	s := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	s.AddReceivingMiddleware(h.ListToolsMiddleware)
	s.AddTool(&mcp.Tool{Name: "base_tool", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})

	ct, st := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	e.reset()
	e.synced(true)
	e.cache("active-session", 20, lsTool("proxied:tool"))

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("in-memory session tools: %v", lsNames(res.Tools))
	lsEqualNames(t, res.Tools, "base_tool") // active-session is never consulted: ID() is ""

	e.reset()
	e.synced(true)
	e.cache("", 20, lsTool("proxied:tool"))
	res, err = cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	lsEqualNames(t, res.Tools, "base_tool", "proxied:tool")
}

// F3 of the 0008 record: the static-inventory fallback reads h.InternalTools while
// OnConfigReloaded replaces it under toolsMu. One writer and four listers on the
// fallback path; meaningful under -race only. One writer, because concurrent
// OnConfigReloaded calls race with each other in syncSearchGates, which is not what
// this test asks.
func TestListToolsFallbackWhileConfigReloads(t *testing.T) {
	e := newLSEnv(t)
	e.reset()
	captureLog(t) // OnConfigReloaded and the fallback log at info and warn level
	mw := e.h.ListToolsMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, nil // nothing usable from the base layer: take the fallback
	})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 50 {
			e.h.OnConfigReloaded(e.h.Config)
		}
	}()
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if _, err := mw(context.Background(), "tools/list", nil); err != nil {
					t.Errorf("tools/list failed: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
