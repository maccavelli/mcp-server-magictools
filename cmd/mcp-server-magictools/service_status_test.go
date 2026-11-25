package main

import (
	"errors"
	"testing"
)

func TestBuildServiceStatusSeparatesRegistrationReadinessAndStaleIdentity(t *testing.T) {
	ready := buildServiceStatus(cliServiceState{PID: 7, Addr: "127.0.0.1:1", BinaryPath: "/bin"}, nil, true)
	if ready["status"] != "running" || ready["ready"] != true || ready["registration"] != true {
		t.Fatalf("ready = %#v", ready)
	}
	if ready["readiness"] != "identity-verified" || ready["enabled"] != "unknown" || ready["manager_process"] != "unknown" || ready["stale"] != false {
		t.Fatalf("observations = %#v", ready)
	}

	stale := buildServiceStatus(cliServiceState{PID: 7}, errStaleIdentity, true)
	if stale["ready"] != false || stale["stale"] != true || stale["readiness"] != "not-ready" {
		t.Fatalf("stale = %#v", stale)
	}
	if stale["pid"] != 7 || stale["identity_error"] == "" {
		t.Fatalf("historical fields lost: %#v", stale)
	}

	other := buildServiceStatus(cliServiceState{}, errors.New("state has no process start token"), false)
	if other["stale"] != false || other["registration"] != false || other["status"] != "stopped" {
		t.Fatalf("other = %#v", other)
	}
}
