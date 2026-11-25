package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
	"github.com/spf13/cobra"
)

// releaseBuildKind is the only stamped value that marks a release build.
const releaseBuildKind = "release"

// updateRepository and updatePlatforms are this product's frozen release
// identity and matrix.
var (
	updateRepository = selfupdate.Repository{Owner: "maccavelli", Name: cmdName}
	updatePlatforms  = []selfupdate.Platform{
		{OS: goOSLinux, Arch: "amd64"},
		{OS: goOSDarwin, Arch: "arm64"},
		{OS: goOSWindows, Arch: "amd64"},
	}
)

// updateOperationTimeout bounds one whole update.
const updateOperationTimeout = 15 * time.Minute

// newUpdater is the construction seam; tests replace it so the CLI matrix
// makes no live GitHub call and touches no service manager.
var newUpdater = defaultUpdater

func defaultUpdater(errw io.Writer) (*selfupdate.Updater, error) {
	limits := selfupdate.DefaultLimits()
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubOptions{
		Repository: updateRepository,
		Client:     &http.Client{Timeout: updateOperationTimeout},
		UserAgent:  cmdName + "/" + RawVersion,
		Limits:     limits,
	})
	if err != nil {
		return nil, err
	}
	selector, err := selfupdate.NewExactAssetSelector(updatePlatforms)
	if err != nil {
		return nil, err
	}
	standalone, err := selfupdate.NewStandaloneInstaller(selfupdate.InstallOptions{})
	if err != nil {
		return nil, err
	}
	target, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable: %w", err)
	}
	// Managed: one adapter is both the lifecycle and the reconciler, so stop,
	// reconcile, and start share the job that recorded the previous PID.
	// A definition pointing at another binary is treated as absent.
	life := &updateLifecycle{target: target}
	managed, err := selfupdate.NewManagedInstaller(standalone, life, life)
	if err != nil {
		return nil, err
	}
	return selfupdate.New(selfupdate.Config{
		Source:    source,
		Versions:  selfupdate.NewStrictVersionPolicy(),
		Assets:    selector,
		Installer: managed,
		Reporter:  selfupdate.NewTextReporter(errw),
		Confirmer: selfupdate.NewTerminalConfirmer(os.Stdin, errw),
		Limits:    limits,
	})
}

// handOffIfInside is the library handoff. Tests replace it.
var handOffIfInside = service.HandOffIfInside

// handOffUpdate detaches an update that would die with the service. A second
// call does not recurse: the library refuses when SELFUPDATE_HANDOFF is set.
// No installed definition means there is nothing to detach from.
func handOffUpdate(ctx context.Context, target string, args []string) (bool, error) {
	installed, err := serviceDefinitionTargets(target)
	if err != nil || !installed {
		return false, err
	}
	job, err := newUpdateServiceJob(target)
	if err != nil {
		return false, err
	}
	detacher, ok := job.(service.Detacher)
	if !ok {
		return false, nil
	}
	detached, handed, err := handOffIfInside(ctx, detacher, service.HandOff{Executable: target, Args: args})
	if err != nil || !handed {
		return handed, err
	}
	return true, waitHandOffResult(ctx, detached.ResultPath)
}

func waitHandOffResult(ctx context.Context, path string) error {
	ticker := time.NewTicker(healthPoll)
	defer ticker.Stop()
	for {
		res, err := service.ReadHandOffResult(path)
		if err == nil {
			if res.Error != "" {
				return errors.New(res.Error)
			}
			if res.ExitCode != 0 {
				return fmt.Errorf("detached update exited %d", res.ExitCode)
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// buildKind maps the linker stamp. Only the exact string "release" counts.
func buildKind() selfupdate.BuildKind {
	if RawBuildKind == releaseBuildKind {
		return selfupdate.ReleaseBuild
	}
	return selfupdate.LocalBuild
}

func newUpdateCmd() *cobra.Command {
	var (
		check, yes, force bool
		targetVersion     string
	)
	cmd := &cobra.Command{
		Use:   "update",
		Args:  cobra.NoArgs,
		Short: "Download and install a GitHub release for " + cmdName,
		Long: `Check GitHub releases, verify the release checksum, replace this executable,
and reconcile and restart the managed service when one is installed for it.

Exit codes: 0 = up to date or declined, 10 = --check found an actionable
target, 1 = error. Set GH_TOKEN or GITHUB_TOKEN to raise API rate limits.

This command starts no orchestrator, datastore or MCP server.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Human output goes to the command's error stream; stdout carries
			// JSON-RPC when this binary serves.
			updater, err := newUpdater(cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), updateOperationTimeout)
			defer cancel()
			target, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve executable: %w", err)
			}
			handed, err := handOffUpdate(ctx, target, os.Args[1:])
			if err != nil || handed {
				return err
			}
			_, err = updater.Run(ctx, selfupdate.Request{
				Product:        cmdName,
				CurrentVersion: RawVersion,
				CurrentBuild:   buildKind(),
				TargetVersion:  targetVersion,
				CheckOnly:      check,
				Force:          force,
				Yes:            yes,
			})
			return err
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report only; exit 0 up to date, 10 actionable, 1 error")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "approve the selected operation without prompting")
	cmd.Flags().BoolVar(&force, "force", false, "replace a local build, or reinstall the selected version")
	cmd.Flags().StringVar(&targetVersion, "version", "", "install this exact release tag (vX.Y.Z); a lower tag is an explicit rollback")
	return cmd
}
