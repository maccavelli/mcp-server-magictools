package main

import (
	"errors"
	"os"
	"runtime"
)

// buildServiceStatus keeps the historical status fields and adds registration,
// enablement, manager process, readiness, and stale identity as separate
// observations. Enablement and the manager process stay unknown until a
// manager probe says otherwise; a state-file identity is not that probe.
func buildServiceStatus(state cliServiceState, readErr error, registered bool) map[string]any {
	alive := readErr == nil
	status := "stopped"
	readiness := "not-ready"
	if alive {
		status = "running"
		readiness = "identity-verified"
	}
	return map[string]any{
		"status": status, "ready": alive, "pid": state.PID, "addr": state.Addr,
		"started": state.Started, "version": state.ConfigVersion, "binary": state.BinaryPath,
		"identity_error": errorString(readErr), "registration": registered,
		"enabled": "unknown", "manager_process": "unknown", "readiness": readiness,
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
