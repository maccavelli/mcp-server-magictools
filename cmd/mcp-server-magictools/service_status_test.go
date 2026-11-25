package main

import (
	"errors"
	"testing"
)

func TestBuildServiceStatusSeparatesRegistrationReadinessAndStaleIdentity(t *testing.T) {
	ready := buildServiceStatus(cliServiceState{PID: 7, Addr: "127.0.0.1:1", BinaryPath: "/bin"}, nil, managerObservation{Registered: true, Loaded: true, Enabled: "true", Process: "7"})
	if ready["status"] != "running" || ready["ready"] != true || ready["registration"] != true {
		t.Fatalf("ready = %#v", ready)
	}
	if ready["readiness"] != "identity-verified" || ready["enabled"] != "true" || ready["manager_process"] != "7" || ready["loaded"] != true || ready["stale"] != false {
		t.Fatalf("observations = %#v", ready)
	}

	stale := buildServiceStatus(cliServiceState{PID: 7}, errStaleIdentity, managerObservation{Registered: true, Enabled: "false", Process: "stopped"})
	if stale["ready"] != false || stale["stale"] != true || stale["readiness"] != "not-ready" {
		t.Fatalf("stale = %#v", stale)
	}
	if stale["pid"] != 7 || stale["identity_error"] == "" {
		t.Fatalf("historical fields lost: %#v", stale)
	}

	other := buildServiceStatus(cliServiceState{}, errors.New("state has no process start token"), managerObservation{})
	if other["stale"] != false || other["registration"] != false || other["status"] != "stopped" {
		t.Fatalf("other = %#v", other)
	}
}
