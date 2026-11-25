package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

// healthPoll and healthTimeout bound WaitHealthy. A service manager accepting
// `start` is not evidence the unit came up, so health is polled rather than
// assumed, and a timeout is fatal (MADR 0005).
const (
	healthPoll    = 250 * time.Millisecond
	healthTimeout = 30 * time.Second
)

// serviceActionStop and serviceActionStart name the two control verbs.
const (
	serviceActionStop  = "stop"
	serviceActionStart = "start"
)

// updateLifecycle adapts this repository's service control to the shared
// selfupdate.Lifecycle seam, bound to the executable the update will replace.
//
// Binding to the target matters: a service definition that points at some
// OTHER binary is treated as absent, so an update never stops, reconciles or
// restarts a service it does not own.
type updateLifecycle struct {
	target string
	job    selfupdate.Lifecycle

	// Seams. Production leaves them nil and the platform implementations run.
	installedFn func(ctx context.Context, target string) (bool, error)
	runningFn   func(ctx context.Context) (bool, error)
	stopFn      func(ctx context.Context) error
	startFn     func(ctx context.Context) error

	HealthTimeout time.Duration
	Poll          time.Duration
}

var _ selfupdate.Lifecycle = (*updateLifecycle)(nil)

// Installed reports whether a definition exists AND targets this executable.
func (l *updateLifecycle) Installed(ctx context.Context, _ string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if l.installedFn != nil {
		return l.installedFn(ctx, l.target)
	}
	return serviceDefinitionTargets(l.target)
}
func (l *updateLifecycle) Running(ctx context.Context, _ string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if l.runningFn != nil {
		return l.runningFn(ctx)
	}
	life, err := l.serviceJob()
	if err != nil {
		return false, err
	}
	return life.Running(ctx, cmdName)
}

// Stop halts the managed service.
func (l *updateLifecycle) Stop(ctx context.Context, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.stopFn != nil {
		return l.stopFn(ctx)
	}
	life, err := l.serviceJob()
	if err != nil {
		return err
	}
	return life.Stop(ctx, cmdName)
}

// Start launches the managed service.
func (l *updateLifecycle) Start(ctx context.Context, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.startFn != nil {
		return l.startFn(ctx)
	}
	life, err := l.serviceJob()
	if err != nil {
		return err
	}
	return life.Start(ctx, cmdName)
}

// WaitHealthy polls the recorded service PID until it is alive, the deadline
// passes, or the caller cancels. A timeout is fatal and enters the shared
// rollback path.
func (l *updateLifecycle) WaitHealthy(ctx context.Context, product string) error {
	timeout := l.HealthTimeout
	if timeout <= 0 {
		timeout = healthTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if l.runningFn != nil {
		return waitServiceHealth(ctx, product, func(ctx context.Context, _ string) (bool, error) { return l.runningFn(ctx) }, timeout, l.Poll)
	}
	life, err := l.serviceJob()
	if err != nil {
		return err
	}
	return life.WaitHealthy(ctx, product)
}

func (l *updateLifecycle) serviceJob() (selfupdate.Lifecycle, error) {
	if l.job != nil {
		return l.job, nil
	}
	job, err := newUpdateServiceJob(l.target)
	if err != nil {
		return nil, err
	}
	l.job = job
	return job, nil
}

// serviceStatePath is where the running service records its PID and binary.
func serviceStatePath() string {
	return filepath.Join(config.DefaultConfigDir(), "service.state")
}

// serviceDefinitionTargets reports whether an OS service definition exists and
// refers to this exact executable. A definition for another binary is reported
// absent so the update leaves it alone.
func serviceDefinitionTargets(target string) (bool, error) {
	path, ok := serviceDefinitionPath()
	if !ok {
		return false, nil
	}
	if filepath.Ext(path) == ".plist" {
		return launchdDefinitionTargets(path, target)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- fixed per-platform service definition path
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err == nil {
		target = resolved
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ExecStart=") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "ExecStart="))
		if len(fields) == 0 {
			continue
		}
		candidate, err := filepath.EvalSymlinks(fields[0])
		if err == nil {
			fields[0] = candidate
		}
		return fields[0] == target, nil
	}
	return false, nil
}

func launchdDefinitionProgram(path string) (string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- resolved LaunchAgent definition path
	if err != nil {
		return "", err
	}
	var p plistXML
	if err := xml.Unmarshal(data, &p); err != nil {
		return "", fmt.Errorf("parse launchd definition: %w", err)
	}
	values := map[string]plistXMLValue{}
	for i, key := range p.Dict.Keys {
		if i < len(p.Dict.Values) {
			values[key] = plistXMLValue{Text: strings.TrimSpace(p.Dict.Values[i].Text), Items: p.Dict.Values[i].Items}
		}
	}
	program := values["Program"].Text
	if program == "" && len(values["ProgramArguments"].Items) > 0 {
		program = values["ProgramArguments"].Items[0]
	}
	if program == "" {
		return "", errors.New("launchd definition has no program")
	}
	if resolved, err := filepath.EvalSymlinks(program); err == nil {
		program = resolved
	}
	return program, nil
}

func launchdDefinitionTargets(path, target string) (bool, error) {
	program, err := launchdDefinitionProgram(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	return program == target, nil
}
