package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type recordingLifecycle struct {
	calls      []string
	running    bool
	healthErr  error
	restartErr error
}

func (f *recordingLifecycle) Installed(context.Context, string) (bool, error) { return true, nil }
func (f *recordingLifecycle) Running(context.Context, string) (bool, error)   { return f.running, nil }
func (f *recordingLifecycle) Stop(context.Context, string) error {
	f.calls = append(f.calls, "stop")
	return nil
}
func (f *recordingLifecycle) Start(context.Context, string) error {
	f.calls = append(f.calls, "start")
	return nil
}
func (f *recordingLifecycle) WaitHealthy(context.Context, string) error {
	f.calls = append(f.calls, "health")
	return f.healthErr
}
func (f *recordingLifecycle) Healthy(ctx context.Context, product string) error {
	return f.WaitHealthy(ctx, product)
}
func (f *recordingLifecycle) Restart(ctx context.Context) error {
	f.calls = append(f.calls, "restart")
	if f.restartErr != nil {
		return f.restartErr
	}
	_ = f.Stop(ctx, cmdName)
	if err := f.Start(ctx, cmdName); err != nil {
		return err
	}
	return f.WaitHealthy(ctx, cmdName)
}

func TestSharedStartWaitsForHealth(t *testing.T) {
	f := &recordingLifecycle{}
	old := newServiceLifecycle
	newServiceLifecycle = func(string) (serviceLifecycle, error) { return f, nil }
	t.Cleanup(func() { newServiceLifecycle = old })
	if err := runSharedServiceAction(context.Background(), serviceActionStart); err != nil {
		t.Fatal(err)
	}
	if want := []string{"start", "health"}; !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
}

func TestSharedStartReportsHealthFailure(t *testing.T) {
	probe := errors.New("health failed")
	f := &recordingLifecycle{healthErr: probe}
	old := newServiceLifecycle
	newServiceLifecycle = func(string) (serviceLifecycle, error) { return f, nil }
	t.Cleanup(func() { newServiceLifecycle = old })
	if err := runSharedServiceAction(context.Background(), serviceActionStart); !errors.Is(err, probe) {
		t.Fatalf("err = %v", err)
	}
}

func TestPlatformStartLeavesHealthToManagedInstaller(t *testing.T) {
	f := &recordingLifecycle{}
	l := &platformServiceLifecycle{job: f}
	if err := l.Start(context.Background(), cmdName); err != nil {
		t.Fatal(err)
	}
	if want := []string{"start"}; !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
}
