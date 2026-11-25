//go:build !darwin

package main

import (
	"context"
	"fmt"
	"runtime"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/scm"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/systemd"
)

func newPlatformJob() (selfupdate.Lifecycle, error) {
	poll := service.PollOptions{Timeout: healthTimeout, Settle: -1}
	switch runtime.GOOS {
	case goOSLinux:
		return systemd.New(systemd.Options{Unit: cmdName + ".service", Scope: systemd.User, Probe: probeServiceHealth, Poll: poll})
	case goOSWindows:
		return scm.New(scm.Options{Name: "MagicToolsService", Probe: probeServiceHealth, Poll: poll})
	default:
		return nil, fmt.Errorf("%w: service lifecycle for %s", service.ErrUnsupported, runtime.GOOS)
	}
}

func stopLaunchdPersistent(context.Context, string, bool) error {
	return fmt.Errorf("launchd lifecycle requires darwin")
}
