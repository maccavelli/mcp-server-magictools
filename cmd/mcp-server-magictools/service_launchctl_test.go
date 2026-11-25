package main

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestClassifyLaunchctlAbsentTimeoutAndPermission(t *testing.T) {
	absent := classifyLaunchctl([]string{"bootout", "gui/1/label"}, launchctlAbsentBootout, "Could not find specified service", nil)
	if !errors.Is(absent, os.ErrNotExist) {
		t.Fatalf("absent bootout = %v", absent)
	}
	if errors.Is(absent, errLaunchctlUnknown) {
		t.Fatal("pinned absent bootout was also unknown")
	}

	permission := classifyLaunchctl([]string{"bootout", "gui/1/label"}, 1, "Operation not permitted", nil)
	if errors.Is(permission, os.ErrNotExist) || !errors.Is(permission, errLaunchctlUnknown) {
		t.Fatalf("permission = %v", permission)
	}

	disabled := classifyLaunchctl([]string{"bootstrap", "gui/1", "/tmp/job.plist"}, 5, "Operation not permitted", nil)
	if errors.Is(disabled, os.ErrNotExist) {
		t.Fatal("bootstrap exit 5 classified as absent")
	}

	printDenied := classifyLaunchctl([]string{"print", "gui/1/label"}, 113, "Could not find specified service", nil)
	if errors.Is(printDenied, os.ErrNotExist) {
		t.Fatal("print exit 113 classified as absent; print text is not the absence contract")
	}

	timedOut := classifyLaunchctl([]string{"bootout", "--wait", "gui/1/label"}, 0, "", context.DeadlineExceeded)
	if !errors.Is(timedOut, errLaunchctlTimeout) || errors.Is(timedOut, os.ErrNotExist) {
		t.Fatalf("timeout = %v", timedOut)
	}
	cancelled := classifyLaunchctl([]string{"disable", "gui/1/label"}, 0, "", context.Canceled)
	if !errors.Is(cancelled, errLaunchctlTimeout) || errors.Is(cancelled, os.ErrNotExist) {
		t.Fatalf("cancel = %v", cancelled)
	}
}
