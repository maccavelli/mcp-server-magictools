package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
)

// serviceLifecycle is the one update-facing lifecycle used on every platform.
type serviceLifecycle interface {
	selfupdate.Lifecycle
	Restart(context.Context) error
}

type platformServiceLifecycle struct {
	target string
	job    selfupdate.Lifecycle
}

var newUpdateServiceJob = func(target string) (selfupdate.Lifecycle, error) {
	life, err := newPlatformServiceLifecycle(target)
	if err != nil {
		return nil, err
	}
	platform, ok := life.(*platformServiceLifecycle)
	if !ok {
		return nil, errors.New("unexpected service lifecycle implementation")
	}
	return platform.job, nil
}

func (l *platformServiceLifecycle) Installed(ctx context.Context, product string) (bool, error) {
	if runtime.GOOS != goOSDarwin {
		return l.job.Installed(ctx, product)
	}
	return serviceDefinitionTargets(l.target)
}
func (l *platformServiceLifecycle) Running(ctx context.Context, product string) (bool, error) {
	return l.job.Running(ctx, product)
}
func (l *platformServiceLifecycle) Stop(ctx context.Context, product string) error {
	if runtime.GOOS == goOSDarwin {
		return stopLaunchdPersistent(ctx, product, false)
	}
	return l.job.Stop(ctx, product)
}
func (l *platformServiceLifecycle) Start(ctx context.Context, product string) error {
	return l.job.Start(ctx, product)
}
func (l *platformServiceLifecycle) WaitHealthy(ctx context.Context, product string) error {
	return l.job.WaitHealthy(ctx, product)
}
func (l *platformServiceLifecycle) Restart(ctx context.Context) error {
	if runtime.GOOS != goOSDarwin {
		if err := l.job.Stop(ctx, cmdName); err != nil {
			return err
		}
		if err := l.Start(ctx, cmdName); err != nil {
			return err
		}
		return l.WaitHealthy(ctx, cmdName)
	}
	if err := stopLaunchdPersistent(ctx, cmdName, true); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := l.Start(ctx, cmdName); err != nil {
		return err
	}
	return l.WaitHealthy(ctx, cmdName)
}

func waitServiceHealth(ctx context.Context, product string, running func(context.Context, string) (bool, error), timeout, poll time.Duration) error {
	if timeout <= 0 {
		timeout = healthTimeout
	}
	if poll <= 0 {
		poll = healthPoll
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	var lastErr error
	for {
		ready, err := running(ctx, product)
		if ready {
			return nil
		}
		if err != nil {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("%s did not become healthy within %s: %w", product, timeout, lastErr)
			}
			return fmt.Errorf("%s did not become healthy within %s", product, timeout)
		case <-ticker.C:
		}
	}
}

func newPlatformServiceLifecycle(target string) (serviceLifecycle, error) {
	if target == "" {
		var err error
		target, err = expectedServiceExecutable()
		if err != nil {
			return nil, err
		}
	}
	job, err := newPlatformJobFn()
	if err != nil {
		return nil, fmt.Errorf("construct service lifecycle: %w", err)
	}
	return &platformServiceLifecycle{target: target, job: job}, nil
}

var newServiceLifecycle = newPlatformServiceLifecycle

var newPlatformJobFn = newPlatformJob

func runSharedServiceAction(ctx context.Context, action string) error {
	return withServiceLock(ctx, productionServiceLabel, func(ctx context.Context) error {
		return runSharedServiceActionLocked(ctx, action)
	})
}

func runSharedServiceActionLocked(ctx context.Context, action string) error {
	life, err := newServiceLifecycle("")
	if err != nil {
		return err
	}
	switch action {
	case serviceActionStart:
		if err := life.Start(ctx, cmdName); err != nil {
			return err
		}
		return life.WaitHealthy(ctx, cmdName)
	case serviceActionStop:
		return life.Stop(ctx, cmdName)
	case "restart":
		return life.Restart(ctx)
	default:
		return fmt.Errorf("unknown service action %q", action)
	}
}

func serviceActionContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 90*time.Second)
}
