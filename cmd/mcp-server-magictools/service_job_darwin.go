//go:build darwin

package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/launchd"
)

var launchctlRunner service.Runner = service.ExecRunner()

func runLaunchctl(ctx context.Context, args ...string) error {
	out, err := launchctlRunner.Run(ctx, service.Command{Path: "/bin/launchctl", Args: args, Env: []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LC_ALL=C"}})
	return classifyLaunchctl(args, out.ExitCode, string(out.Stderr), err)
}

func launchdTarget() (string, string, string, error) {
	home, uid, err := resolveLaunchdTargetFn()
	if err != nil {
		return "", "", "", err
	}
	return home, uid, fmt.Sprintf("gui/%s/%s", uid, productionServiceLabel), nil
}

func newPlatformJob() (selfupdate.Lifecycle, error) {
	home, uid, _, err := launchdTarget()
	if err != nil {
		return nil, err
	}
	uidInt, err := strconv.Atoi(uid)
	if err != nil {
		return nil, err
	}
	plist := home + "/Library/LaunchAgents/" + productionServiceLabel + ".plist"
	return launchd.New(launchd.Options{
		Label: productionServiceLabel, Domain: launchd.GUI(uidInt), Plist: plist,
		Probe: probeServiceHealth,
		Poll:  servicePollOptions(),
	})
}

func servicePollOptions() service.PollOptions {
	return service.PollOptions{Interval: 250 * time.Millisecond, Timeout: healthTimeout, Settle: -1}
}

// stopLaunchdPersistent disables before bootout, making the operator's stop
// stable under unconditional KeepAlive. Restart deliberately skips disable.
func stopLaunchdPersistent(ctx context.Context, product string, restarting bool) error {
	_, _, target, err := launchdTarget()
	if err != nil {
		return err
	}
	if !restarting {
		if err := runLaunchctl(ctx, "disable", target); err != nil {
			return err
		}
	}
	job, err := newPlatformJobFn()
	if err != nil {
		return err
	}
	return job.Stop(ctx, product)
}
