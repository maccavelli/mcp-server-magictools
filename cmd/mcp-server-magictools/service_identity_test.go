package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type controlledIdentity struct{ valid bool }

func (f controlledIdentity) Self() (processIdentity, error) {
	return processIdentity{PID: 10, Start: 20, Executable: "/service"}, nil
}
func (f controlledIdentity) Verify(processIdentity) (bool, error) { return f.valid, nil }

func writeIdentityFixture(t *testing.T, state cliServiceState) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "service.state")
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	oldState := serviceStateFile
	serviceStateFile = func() string { return path }
	t.Cleanup(func() { serviceStateFile = oldState })
}

func TestServiceIdentityRejectsStaleReusedAndWrongBinary(t *testing.T) {
	old := serviceIdentity
	serviceIdentity = controlledIdentity{valid: false}
	t.Cleanup(func() { serviceIdentity = old })
	for _, state := range []cliServiceState{
		{PID: 10, BinaryPath: "/service"},
		{PID: 10, StartToken: 20, BinaryPath: "/service"},
		{PID: 10, StartToken: 20, BinaryPath: "/other"},
	} {
		writeIdentityFixture(t, state)
		if _, err := readExpectedServiceState(processIdentity{}); err == nil {
			t.Fatalf("accepted state %#v", state)
		}
	}
	serviceIdentity = controlledIdentity{valid: true}
	writeIdentityFixture(t, cliServiceState{PID: 10, StartToken: 20, BinaryPath: "/service"})
	if _, err := readExpectedServiceState(processIdentity{PID: 11}); err == nil {
		t.Fatal("accepted different manager pid")
	}
}

func TestServiceHealthHTTPReadiness(t *testing.T) {
	for _, tc := range []struct {
		code  int
		body  string
		ready bool
	}{
		{http.StatusServiceUnavailable, `{"status":"booting"}`, false},
		{http.StatusOK, `{"status":"ok"}`, true},
		{http.StatusOK, `{"status":"degraded"}`, true},
		{http.StatusOK, `{"status":"unrelated"}`, false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.code)
			_, _ = w.Write([]byte(tc.body))
		}))
		h, err := serviceHealthHTTP(context.Background(), srv.URL)
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if h.Ready != tc.ready {
			t.Errorf("HTTP %d %s ready=%t", tc.code, tc.body, h.Ready)
		}
	}
	if _, err := serviceHealthHTTP(context.Background(), "http://example.com"); err == nil {
		t.Fatal("accepted non-loopback health address")
	}
}

func TestUnrelatedHTTPServerCannotSatisfyIdentityHealth(t *testing.T) {
	old := serviceIdentity
	serviceIdentity = controlledIdentity{valid: false}
	t.Cleanup(func() { serviceIdentity = old })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) }))
	defer srv.Close()
	writeIdentityFixture(t, cliServiceState{PID: 10, StartToken: 20, BinaryPath: "/service", Addr: srv.URL})
	oldExpected := serviceExpectedExecutable
	serviceExpectedExecutable = func() (string, error) { return "/service", nil }
	t.Cleanup(func() { serviceExpectedExecutable = oldExpected })
	h, err := probeServiceHealth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Ready {
		t.Fatal("unrelated HTTP server satisfied identity readiness")
	}
}

func TestLaunchdDefinitionTargetsEscapedExactProgram(t *testing.T) {
	d := defaultLaunchdDefinition(testAbs("tmp", "one & two"), testAbs("tmp"), "local.magictools.test")
	text, err := renderLaunchdDefinition(d)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "job.plist")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, err := launchdDefinitionTargets(path, d.Program); err != nil || !ok {
		t.Fatalf("own program not recognized: %t %v", ok, err)
	}
	if ok, err := launchdDefinitionTargets(path, testAbs("tmp", "one")); err != nil || ok {
		t.Fatalf("substring program accepted: %t %v", ok, err)
	}
	if err := os.WriteFile(path, []byte("<plist>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := launchdDefinitionTargets(path, d.Program); err == nil {
		t.Fatal("malformed XML treated as absent")
	}
}

func TestLegacyStoppedProcessIsNotHealthy(t *testing.T) {
	old := serviceIdentity
	serviceIdentity = controlledIdentity{valid: true}
	t.Cleanup(func() { serviceIdentity = old })
	oldExpected := serviceExpectedExecutable
	serviceExpectedExecutable = func() (string, error) { return "/service", nil }
	t.Cleanup(func() { serviceExpectedExecutable = oldExpected })
	writeIdentityFixture(t, cliServiceState{PID: os.Getpid(), BinaryPath: "/service"})
	if h, err := probeServiceHealth(context.Background()); err != nil || h.Ready {
		t.Fatalf("legacy PID state accepted: %#v %v", h, err)
	}
}

func TestHealthRequiresDefinitionExecutable(t *testing.T) {
	oldIdentity := serviceIdentity
	serviceIdentity = controlledIdentity{valid: true}
	t.Cleanup(func() { serviceIdentity = oldIdentity })
	oldExpected := serviceExpectedExecutable
	serviceExpectedExecutable = func() (string, error) { return "/expected", nil }
	t.Cleanup(func() { serviceExpectedExecutable = oldExpected })
	writeIdentityFixture(t, cliServiceState{PID: 10, StartToken: 20, BinaryPath: "/other"})
	h, err := probeServiceHealth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Ready {
		t.Fatal("valid process from another executable satisfied definition readiness")
	}
}

var _ = errors.New
