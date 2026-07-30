// Package main: non-destructive regression tests for the service-hardening work.
//
// These exercise pure logic (template rendering, signal classification, state
// serialization, helpers) without mutating the OS — the live install/uninstall
// path stays behind the skip in service_extra_test.go.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"text/template"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

func renderServiceTemplate(t *testing.T, tmplStr string, data map[string]string) string {
	t.Helper()
	tmpl, err := template.New("svc").Parse(tmplStr)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	return buf.String()
}

// A5/A2: the systemd unit must carry the new supervision directives.
func TestSystemdUnitTemplateHardening(t *testing.T) {
	out := renderServiceTemplate(t, systemdUnitTemplate, map[string]string{
		"BinPath":     "/home/u/.local/bin/mcp-server-magictools",
		"BindAddr":    "localhost:48080",
		"EnvPath":     "/home/u/.local/bin:/usr/local/bin:/usr/bin:/bin",
		"Home":        "/home/u",
		"RecallURL":   "",
		"SocraticURL": "",
	})
	for _, want := range []string{
		"KillMode=mixed",                     // A5: fork-aware kill
		"TimeoutStopSec=35",                  // A5: must exceed app's 30s deadline
		"ExecReload=/bin/kill -HUP $MAINPID", // A2: SIGHUP config reload
		"Restart=always",
		"RestartSec=5",
		"ExecStart=/home/u/.local/bin/mcp-server-magictools serve",
		"WantedBy=default.target",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("systemd unit missing %q\nrendered:\n%s", want, out)
		}
	}
}

// A3: the launchd plist ExitTimeOut must exceed the app's 30s shutdown deadline
// so launchd does not SIGKILL mid-drain.
func TestLaunchdPlistTemplateHardening(t *testing.T) {
	out := renderServiceTemplate(t, launchdPlistTemplate, map[string]string{
		"BinPath":     "/Users/u/.local/bin/mcp-server-magictools",
		"BindAddr":    "localhost:48080",
		"EnvPath":     "/opt/homebrew/bin:/usr/local/bin",
		"LogDir":      "/Users/u/Library/Caches/mcp-server-magictools",
		"RecallURL":   "",
		"SocraticURL": "",
	})
	if !strings.Contains(out, "<key>ExitTimeOut</key>") {
		t.Fatalf("plist missing ExitTimeOut key:\n%s", out)
	}
	// The ExitTimeOut value must be >= 35 (and strictly > 30).
	if strings.Contains(out, "<key>ExitTimeOut</key>\n    <integer>20</integer>") {
		t.Error("plist still uses the old ExitTimeOut=20 (would SIGKILL mid-drain)")
	}
	for _, want := range []string{
		"<integer>35</integer>",
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
		"<string>serve</string>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plist missing %q\nrendered:\n%s", want, out)
		}
	}
}

// A1/A2: SIGHUP is classified as a reload signal (Unix), SIGINT/SIGTERM are not.
func TestReloadSignalClassification(t *testing.T) {
	if isReloadSignal(syscall.SIGINT) {
		t.Error("SIGINT must not be a reload signal")
	}
	if isReloadSignal(syscall.SIGTERM) {
		t.Error("SIGTERM must not be a reload signal")
	}
	if runtime.GOOS == "windows" {
		if len(reloadSignals()) != 0 {
			t.Error("windows must not register reload signals")
		}
		return
	}
	if !isReloadSignal(syscall.SIGHUP) {
		t.Error("SIGHUP must be a reload signal on Unix")
	}
	if len(reloadSignals()) != 1 {
		t.Errorf("expected exactly one reload signal on Unix, got %d", len(reloadSignals()))
	}
}

// A8: the state file must be written 0600 and round-trip cleanly.
func TestWriteServiceStateModeAndRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.EnvConfigDir, tmp)

	app := &OrchestratorApp{}
	app.writeServiceState("localhost:54321")

	statePath := filepath.Join(tmp, "service.state")
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("state file not written: %v", err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("state file mode = %o, want 0600", perm)
		}
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st cliServiceState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}
	if st.PID != os.Getpid() {
		t.Errorf("state PID = %d, want %d", st.PID, os.Getpid())
	}
	if st.Addr != "localhost:54321" {
		t.Errorf("state Addr = %q, want localhost:54321", st.Addr)
	}
}

// BUG-2: isProcessAlive reports liveness accurately and is safe for bad PIDs.
func TestIsProcessAlive(t *testing.T) {
	if !isProcessAlive(os.Getpid()) {
		t.Error("current process should report alive")
	}
	for _, pid := range []int{0, -1, -1000} {
		if isProcessAlive(pid) {
			t.Errorf("isProcessAlive(%d) should be false", pid)
		}
	}
}

// resolveLaunchdTarget falls back to the current user and honours SUDO_UID.
func TestResolveLaunchdTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uid semantics differ on Windows")
	}
	wantUID := strconv.Itoa(os.Getuid())

	// Fallback path: no SUDO_* set → current user.
	t.Setenv("SUDO_UID", "")
	t.Setenv("SUDO_USER", "")
	if _, uid, _, err := resolveLaunchdTarget(); err != nil || uid != wantUID {
		t.Errorf("fallback: got uid=%q err=%v, want uid=%q", uid, err, wantUID)
	}

	// SUDO path: SUDO_UID set to our own uid → resolves via LookupId.
	t.Setenv("SUDO_UID", wantUID)
	if _, uid, _, err := resolveLaunchdTarget(); err != nil || uid != wantUID {
		t.Errorf("sudo: got uid=%q err=%v, want uid=%q", uid, err, wantUID)
	}
}
