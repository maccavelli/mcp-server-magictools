//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/launchd"
)

var launchctlRunner service.Runner = service.ExecRunner()

func init() {
	bootstrapLaunchdFn = bootstrapLaunchd
}

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
		Runner: launchctlRunner,
		Probe:  probeServiceHealth,
		Poll:   servicePollOptions(),
	})
}

// stopLaunchdPersistent disables before bootout on the job that will start
// again. Restart, refresh, and update skip disable. The job is the caller's:
// a second job would forget the PID WaitHealthy must reject.
func stopLaunchdPersistent(ctx context.Context, product string, restarting bool, job selfupdate.Lifecycle) error {
	_, _, target, err := launchdTarget()
	if err != nil {
		return err
	}
	if !restarting {
		if err := runLaunchctl(ctx, "disable", target); err != nil {
			return err
		}
	}
	if job == nil {
		return errors.New("launchd stop requires the operation's job")
	}
	return job.Stop(ctx, product)
}

func bootstrapLaunchd(ctx context.Context) error {
	home, uid, _, err := launchdTarget()
	if err != nil {
		return err
	}
	plist := home + "/Library/LaunchAgents/" + productionServiceLabel + ".plist"
	return runLaunchctl(ctx, "bootstrap", "gui/"+uid, plist)
}
