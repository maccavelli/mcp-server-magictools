package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func useScratchLaunchd(t *testing.T, home string) {
	t.Helper()
	oldResolve := resolveLaunchdTargetFn
	oldPlist := launchdPlistPathFn
	oldCleanup := cleanupServiceArtifactsFn
	oldLock := serviceLockDir
	oldForce := forceServiceInstall
	resolveLaunchdTargetFn = func() (string, string, error) { return home, "501", nil }
	launchdPlistPathFn = func() (string, error) {
		return filepath.Join(home, "Library", "LaunchAgents", productionServiceLabel+".plist"), nil
	}
	cleanupServiceArtifactsFn = func() {}
	serviceLockDir = func() (string, error) { return filepath.Join(home, "locks"), nil }
	t.Cleanup(func() {
		resolveLaunchdTargetFn = oldResolve
		launchdPlistPathFn = oldPlist
		cleanupServiceArtifactsFn = oldCleanup
		serviceLockDir = oldLock
		forceServiceInstall = oldForce
	})
}

func TestInstallLaunchdAbsentNoForceRollbackAndRelative(t *testing.T) {
	home := t.TempDir()
	useScratchLaunchd(t, home)
	fake := &recordingLifecycle{}
	oldLife := newServiceLifecycle
	newServiceLifecycle = func(string) (serviceLifecycle, error) { return fake, nil }
	t.Cleanup(func() { newServiceLifecycle = oldLife })
	forceServiceInstall = true

	bin := filepath.Join(home, "bin & <quote>")
	if err := installLaunchd(bin); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", productionServiceLabel+".plist")
	text, err := os.ReadFile(plist)
	if err != nil {
		t.Fatal(err)
	}
	values := plistValues(t, string(text))
	if values["ProgramArguments"].Items[0] != bin {
		t.Fatalf("program = %q", values["ProgramArguments"].Items[0])
	}
	if values["KeepAlive"].Type != "true" || values["StandardOutPath"].Text == "" {
		t.Fatalf("policy = %#v", values)
	}
	if len(fake.calls) == 0 || fake.calls[0] != "restart" {
		t.Fatalf("calls = %v, want restart first", fake.calls)
	}

	forceServiceInstall = false
	before := append([]string(nil), fake.calls...)
	if err := installLaunchd(bin); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(plist); err != nil || string(got) != string(text) {
		t.Fatal("no-force install rewrote the definition")
	}
	if len(fake.calls) != len(before) {
		t.Fatalf("no-force called lifecycle: %v", fake.calls[len(before):])
	}

	forceServiceInstall = true
	previous := append([]byte(nil), text...)
	fake.running = true
	fake.restartErr = errors.New("bootstrap failed")
	fake.calls = nil
	if err := installLaunchd(bin); err == nil {
		t.Fatal("force install reported success after restart failure")
	}
	restored, err := os.ReadFile(plist)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(previous) {
		t.Fatal("failed force install did not restore the previous definition")
	}
	if len(fake.calls) < 2 || fake.calls[0] != "restart" || fake.calls[1] != "stop" {
		t.Fatalf("rollback calls = %v", fake.calls)
	}

	if err := installLaunchd("relative/bin"); err == nil {
		t.Fatal("accepted a relative executable")
	}
}

func TestUninstallLaunchdAbsentAndRunningDoNotSignalRawPID(t *testing.T) {
	home := t.TempDir()
	useScratchLaunchd(t, home)
	fake := &recordingLifecycle{}
	oldLife := newServiceLifecycle
	newServiceLifecycle = func(string) (serviceLifecycle, error) { return fake, nil }
	t.Cleanup(func() { newServiceLifecycle = oldLife })

	if err := uninstallLaunchd(); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("absent uninstall called lifecycle: %v", fake.calls)
	}

	plist := filepath.Join(home, "Library", "LaunchAgents", productionServiceLabel+".plist")
	if err := os.MkdirAll(filepath.Dir(plist), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plist, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := uninstallLaunchd(); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "stop" {
		t.Fatalf("running uninstall calls = %v, want [stop]", fake.calls)
	}
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Fatal("uninstall left the definition in place")
	}

	if err := os.WriteFile(plist, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	stopping := &stopFailLifecycle{err: errors.New("stop failed")}
	newServiceLifecycle = func(string) (serviceLifecycle, error) { return stopping, nil }
	if err := uninstallLaunchd(); err == nil {
		t.Fatal("uninstall reported success after stop failure")
	}
	if _, err := os.Stat(plist); err != nil {
		t.Fatal("failed uninstall removed the definition")
	}
}

type stopFailLifecycle struct{ err error }

func (f *stopFailLifecycle) Installed(context.Context, string) (bool, error) { return true, nil }
func (f *stopFailLifecycle) Running(context.Context, string) (bool, error)   { return true, nil }
func (f *stopFailLifecycle) Stop(context.Context, string) error              { return f.err }
func (f *stopFailLifecycle) Start(context.Context, string) error             { return nil }
func (f *stopFailLifecycle) WaitHealthy(context.Context, string) error       { return nil }
func (f *stopFailLifecycle) Restart(context.Context) error                   { return f.err }
