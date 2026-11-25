//go:build darwin

package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

type argvJob struct {
	selfupdate.Lifecycle
	calls []string
	err   error
}

func (j *argvJob) Stop(context.Context, string) error {
	j.calls = append(j.calls, "bootout")
	return j.err
}

func TestRestartUsesTheSameJob(t *testing.T) {
	home := t.TempDir()
	oldResolve := resolveLaunchdTargetFn
	resolveLaunchdTargetFn = func() (string, string, error) { return home, "501", nil }
	t.Cleanup(func() { resolveLaunchdTargetFn = oldResolve })
	oldJob := newPlatformJobFn
	newPlatformJobFn = func() (selfupdate.Lifecycle, error) {
		t.Fatal("restart constructed a second job")
		return nil, nil
	}
	t.Cleanup(func() { newPlatformJobFn = oldJob })
	f := &recordingLifecycle{}
	if err := (&platformServiceLifecycle{job: f}).Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) == 0 || f.calls[0] != "stop" {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestStopLaunchdDisableBootoutTimeoutAndRestart(t *testing.T) {
	home := t.TempDir()
	oldResolve := resolveLaunchdTargetFn
	resolveLaunchdTargetFn = func() (string, string, error) { return home, "501", nil }
	t.Cleanup(func() { resolveLaunchdTargetFn = oldResolve })

	oldRunner := launchctlRunner
	oldJob := newPlatformJobFn
	t.Cleanup(func() {
		launchctlRunner = oldRunner
		newPlatformJobFn = oldJob
	})

	job := &argvJob{}
	newPlatformJobFn = func() (selfupdate.Lifecycle, error) { return job, nil }
	var argv [][]string
	launchctlRunner = service.RunnerFunc(func(_ context.Context, c service.Command) (service.Output, error) {
		argv = append(argv, append([]string(nil), c.Args...))
		return service.Output{}, context.DeadlineExceeded
	})

	err := stopLaunchdPersistent(context.Background(), cmdName, false, job)
	if !errors.Is(err, errLaunchctlTimeout) || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stop timeout = %v", err)
	}
	if len(argv) != 1 || argv[0][0] != "disable" {
		t.Fatalf("stop argv = %v, want disable before any bootout", argv)
	}
	if len(job.calls) != 0 {
		t.Fatal("timeout on disable still booted out")
	}

	argv = nil
	err = stopLaunchdPersistent(context.Background(), cmdName, true, job)
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) != 0 || len(job.calls) != 1 || job.calls[0] != "bootout" {
		t.Fatalf("restart argv = %v job = %v, want bootout without disable", argv, job.calls)
	}

	argv = nil
	job.calls = nil
	launchctlRunner = service.RunnerFunc(func(_ context.Context, c service.Command) (service.Output, error) {
		argv = append(argv, append([]string(nil), c.Args...))
		return service.Output{ExitCode: 1, Stderr: []byte("Operation not permitted")}, nil
	})
	err = stopLaunchdPersistent(context.Background(), cmdName, false, job)
	if errors.Is(err, os.ErrNotExist) || !errors.Is(err, errLaunchctlUnknown) {
		t.Fatalf("permission = %v", err)
	}
}
