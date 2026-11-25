package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/servicetest"
)

func TestServicePollUsesLibrarySettle(t *testing.T) {
	if servicePollOptions().Settle != 0 {
		t.Fatalf("Settle = %s, want the library default", servicePollOptions().Settle)
	}
}

func TestReloadDoesNotDisable(t *testing.T) {
	f := &recordingLifecycle{running: true}
	old := newServiceLifecycle
	newServiceLifecycle = func(string) (serviceLifecycle, error) {
		return &platformServiceLifecycle{job: f}, nil
	}
	t.Cleanup(func() { newServiceLifecycle = old })
	bootstrapped := false
	oldBoot := bootstrapLaunchdFn
	bootstrapLaunchdFn = func(context.Context) error {
		bootstrapped = true
		return nil
	}
	t.Cleanup(func() { bootstrapLaunchdFn = oldBoot })
	if err := reloadLaunchdDefinition(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !bootstrapped {
		t.Fatal("loaded job was not bootstrapped")
	}
	for _, call := range f.calls {
		if call == "disable" {
			t.Fatal("reload disabled the job")
		}
	}
	if len(f.calls) != 1 || f.calls[0] != "stop" {
		t.Fatalf("calls = %v, want [stop]", f.calls)
	}
}

func TestSharedStartUsesTheController(t *testing.T) {
	f := &recordingLifecycle{}
	old := newServiceLifecycle
	newServiceLifecycle = func(string) (serviceLifecycle, error) { return f, nil }
	t.Cleanup(func() { newServiceLifecycle = old })
	if err := serviceStartCmd.RunE(serviceStartCmd, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) < 2 || f.calls[0] != "start" || f.calls[1] != "health" {
		t.Fatalf("calls = %v", f.calls)
	}
}

func ownDefinition(t *testing.T, bin string) {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == goOSDarwin {
		text, err := renderLaunchdDefinition(defaultLaunchdDefinition(bin, dir, productionServiceLabel))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, productionServiceLabel+".plist")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		old := launchdPlistPathFn
		launchdPlistPathFn = func() (string, error) { return path, nil }
		t.Cleanup(func() { launchdPlistPathFn = old })
		oldDef := serviceDefinitionPathFn
		serviceDefinitionPathFn = func() (string, bool) { return path, true }
		t.Cleanup(func() { serviceDefinitionPathFn = oldDef })
		return
	}
	path := filepath.Join(dir, "job.service")
	if err := os.WriteFile(path, []byte("ExecStart="+bin+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := serviceDefinitionPathFn
	serviceDefinitionPathFn = func() (string, bool) { return path, true }
	t.Cleanup(func() { serviceDefinitionPathFn = old })
}

func TestHandOffOutsideDoesNotDetach(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.plist")
	oldPath := launchdPlistPathFn
	launchdPlistPathFn = func() (string, error) { return missing, nil }
	t.Cleanup(func() { launchdPlistPathFn = oldPath })
	oldDef := serviceDefinitionPathFn
	serviceDefinitionPathFn = func() (string, bool) { return missing, true }
	t.Cleanup(func() { serviceDefinitionPathFn = oldDef })
	handed, err := handOffUpdate(context.Background(), filepath.Join(dir, "bin"), nil)
	if err != nil || handed {
		t.Fatalf("absent definition handed=%t err=%v", handed, err)
	}
	bin := filepath.Join(dir, "bin")
	ownDefinition(t, bin)
	called := 0
	oldHand := handOffIfInside
	handOffIfInside = func(context.Context, service.Detacher, service.HandOff) (service.Detached, bool, error) {
		called++
		return service.Detached{}, false, nil
	}
	t.Cleanup(func() { handOffIfInside = oldHand })
	oldJob := newUpdateServiceJob
	newUpdateServiceJob = func(string) (selfupdate.Lifecycle, error) { return &detachingJob{}, nil }
	t.Cleanup(func() { newUpdateServiceJob = oldJob })
	handed, err = handOffUpdate(context.Background(), bin, []string{"update"})
	if err != nil || handed || called != 1 {
		t.Fatalf("outside handed=%t calls=%d err=%v", handed, called, err)
	}
}

func TestHandOffInsideReturnsDetachedResult(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	ownDefinition(t, bin)
	result := filepath.Join(dir, "result.json")
	if err := os.WriteFile(result, []byte("{\"schema_version\":1,\"id\":\"1\",\"exit_code\":0}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldHand := handOffIfInside
	handOffIfInside = func(context.Context, service.Detacher, service.HandOff) (service.Detached, bool, error) {
		return service.Detached{ResultPath: result}, true, nil
	}
	t.Cleanup(func() { handOffIfInside = oldHand })
	oldJob := newUpdateServiceJob
	newUpdateServiceJob = func(string) (selfupdate.Lifecycle, error) { return &detachingJob{}, nil }
	t.Cleanup(func() { newUpdateServiceJob = oldJob })
	handed, err := handOffUpdate(context.Background(), bin, nil)
	if err != nil || !handed {
		t.Fatalf("handed=%t err=%v", handed, err)
	}
}

func TestHandOffDoesNotRecurse(t *testing.T) {
	t.Setenv(service.EnvHandOff, "already")
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	ownDefinition(t, bin)
	oldJob := newUpdateServiceJob
	newUpdateServiceJob = func(string) (selfupdate.Lifecycle, error) { return &detachingJob{inside: true}, nil }
	t.Cleanup(func() { newUpdateServiceJob = oldJob })
	oldHand := handOffIfInside
	handOffIfInside = service.HandOffIfInside
	t.Cleanup(func() { handOffIfInside = oldHand })
	handed, err := handOffUpdate(context.Background(), bin, nil)
	if err != nil || handed {
		t.Fatalf("recursive handoff handed=%t err=%v", handed, err)
	}
}

type detachingJob struct {
	recordingLifecycle
	inside bool
}

func (d *detachingJob) Inside(context.Context) (bool, error) { return d.inside, nil }
func (d *detachingJob) Detach(context.Context, service.HandOff) (service.Detached, error) {
	return service.Detached{}, nil
}

func TestDelegatedLifecycleConforms(t *testing.T) {
	fake := servicetest.NewFakeService(servicetest.FakeOptions{StopBound: 200 * time.Millisecond, StartBound: 200 * time.Millisecond, StopLatency: 20 * time.Millisecond, Interval: 5 * time.Millisecond})
	life := &updateLifecycle{job: fake, installedFn: func(context.Context, string) (bool, error) {
		return fake.Installed(context.Background(), cmdName)
	}}
	err := servicetest.VerifyLifecycle(context.Background(), life, servicetest.LifecycleCase{
		Product: cmdName, Lab: fake, StopBound: fake.StopBound(), StartBound: fake.StartBound(), Slack: 2 * time.Second,
		RestoresAfterFailedStop: true,
	})
	if err != nil {
		t.Fatal(err)
	}
}
