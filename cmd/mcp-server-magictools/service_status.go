package main

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strconv"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/launchd"
)

// managerObservation is what the library job reports. Enabled is the library's
// own reading. This package does not parse launchctl print or print-disabled.
type managerObservation struct {
	Registered bool
	Loaded     bool
	Enabled    string
	Process    string
	Err        error
}

const managerUnknown = "unknown"

var observeManager = observeManagerStatus

func unknownManager(err error) managerObservation {
	return managerObservation{Enabled: managerUnknown, Process: managerUnknown, Err: err}
}

func observeManagerStatus(ctx context.Context) managerObservation {
	if runtime.GOOS != goOSDarwin {
		registered, err := definitionRegistered()
		observed := unknownManager(err)
		observed.Registered = registered
		return observed
	}
	life, err := newServiceLifecycle("")
	if err != nil {
		return unknownManager(err)
	}
	platform, ok := life.(*platformServiceLifecycle)
	if !ok {
		return unknownManager(errors.New("launchd status requires the platform job"))
	}
	statter, ok := platform.job.(interface {
		Status(context.Context, string) (launchd.Status, error)
	})
	if !ok {
		return unknownManager(errors.New("launchd job has no status"))
	}
	st, err := statter.Status(ctx, cmdName)
	if err != nil {
		return unknownManager(err)
	}
	process := "stopped"
	if st.Running {
		process = strconv.Itoa(st.PID)
	}
	enabled := "false"
	if st.Enabled {
		enabled = "true"
	}
	return managerObservation{Registered: st.Installed, Loaded: st.Loaded, Enabled: enabled, Process: process}
}

// buildServiceStatus keeps the historical status fields and adds registration,
// enablement, manager process, readiness, and stale identity as separate
// observations.
func buildServiceStatus(state cliServiceState, readErr error, mgr managerObservation) map[string]any {
	alive := readErr == nil
	status := "stopped"
	readiness := "not-ready"
	if alive {
		status = "running"
		readiness = "identity-verified"
	}
	enabled, process := mgr.Enabled, mgr.Process
	if enabled == "" {
		enabled = managerUnknown
	}
	if process == "" {
		process = managerUnknown
	}
	return map[string]any{
		"status": status, "ready": alive, "pid": state.PID, "addr": state.Addr,
		"started": state.Started, "version": state.ConfigVersion, "binary": state.BinaryPath,
		"identity_error": errorString(readErr), "registration": mgr.Registered,
		"loaded": mgr.Loaded, "enabled": enabled, "manager_process": process, "readiness": readiness,
		"stale": errors.Is(readErr, errStaleIdentity),
	}
}

func definitionRegistered() (bool, error) {
	path, ok := serviceDefinitionPath()
	if runtime.GOOS == goOSDarwin {
		var err error
		path, err = launchdPlistPathFn()
		if err != nil {
			return false, err
		}
		ok = true
	}
	if !ok || path == "" {
		return false, nil
	}
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
