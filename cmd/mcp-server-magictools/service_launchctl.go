package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// launchctlAbsentBootout is the exit status observed for bootout of a missing
// job on the reviewed host (macOS 27.2.1, Darwin Bootstrapper 7.0.0). It is an
// exit status, not parsed launchctl text. Any other nonzero status stays unknown.
const launchctlAbsentBootout = 3

var (
	errLaunchctlUnknown = errors.New("launchctl result is not an absent service")
	errLaunchctlTimeout = errors.New("launchctl timed out")
)

type launchctlCallError struct {
	args   []string
	exit   int
	stderr string
	cause  error
}

func (e *launchctlCallError) Error() string {
	if e.cause != nil && e.exit == 0 {
		return fmt.Sprintf("launchctl %v: %v", e.args, e.cause)
	}
	return fmt.Sprintf("launchctl %v: exit %d: %s", e.args, e.exit, e.stderr)
}

func (e *launchctlCallError) Unwrap() error { return e.cause }

// classifyLaunchctl turns one launchctl result into an error. A context
// deadline or cancellation is a timeout, never absence. Only the pinned
// absent-bootout status is os.ErrNotExist. Permission, duplicate, and
// disabled failures stay unknown so callers cannot treat them as absent.
func classifyLaunchctl(args []string, exit int, stderr string, runErr error) error {
	if runErr != nil {
		cause := runErr
		if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(runErr, context.Canceled) {
			cause = fmt.Errorf("%w: %w", errLaunchctlTimeout, runErr)
		}
		return &launchctlCallError{args: args, stderr: stderr, cause: cause}
	}
	if exit == 0 {
		return nil
	}
	cause := errLaunchctlUnknown
	if launchctlBootoutAbsent(args, exit) {
		cause = os.ErrNotExist
	}
	return &launchctlCallError{args: args, exit: exit, stderr: stderr, cause: cause}
}

func launchctlBootoutAbsent(args []string, exit int) bool {
	if len(args) == 0 || args[0] != "bootout" {
		return false
	}
	switch exit {
	case launchctlAbsentBootout, 36, 113:
		return true
	default:
		return false
	}
}
