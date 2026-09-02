package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/maccavelli/mcplib/selfupdate"
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
	// Managed: the lifecycle adapter is bound to THIS executable, so a service
	// definition pointing at another binary is treated as absent and is never
	// stopped, reconciled or restarted.
	managed, err := selfupdate.NewManagedInstaller(standalone,
		&updateLifecycle{target: target},
		&updateReconciler{})
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

// updateReconciler runs the NEW binary's hidden `service refresh --json` and
// keeps its receipt so a rollback can restore the previous definition.
type updateReconciler struct {
	// runFn is the seam; production spawns the installed binary.
	runFn func(ctx context.Context, executable string) ([]byte, error)
}

var _ selfupdate.Reconciler = (*updateReconciler)(nil)

// Reconcile rewrites the installed definition using the binary just installed.
// Under MADR 0005 a reconcile failure is fatal and enters the shared rollback
// path rather than being logged as a warning.
func (r *updateReconciler) Reconcile(ctx context.Context, product, executable string) (selfupdate.ReconcileResult, error) {
	if err := ctx.Err(); err != nil {
		return selfupdate.ReconcileResult{}, err
	}
	run := r.runFn
	if run == nil {
		run = runServiceRefresh
	}
	out, err := run(ctx, executable)
	if err != nil {
		return selfupdate.ReconcileResult{}, fmt.Errorf("refresh %s service definition: %w", product, err)
	}
	var receipt refreshReceipt
	if err := json.Unmarshal(out, &receipt); err != nil {
		return selfupdate.ReconcileResult{}, fmt.Errorf("parse refresh receipt: %w", err)
	}
	return selfupdate.ReconcileResult{
		Changed: receipt.Changed,
		Detail:  receipt.Detail,
		State:   receipt,
	}, nil
}

// Restore puts the previous definition back during rollback. A receipt this
// adapter did not produce is an error rather than a silent success, so a
// rollback never reports a restoration it did not perform.
func (r *updateReconciler) Restore(ctx context.Context, product string, receipt selfupdate.ReconcileResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !receipt.Changed {
		return nil
	}
	state, ok := receipt.State.(refreshReceipt)
	if !ok {
		return fmt.Errorf("restore %s: unexpected reconcile receipt %T", product, receipt.State)
	}
	return restoreServiceDefinition(state)
}

// runServiceRefresh executes the installed binary's hidden refresh operation.
// The child is the point: only the new binary carries the definition its own
// release ships.
func runServiceRefresh(ctx context.Context, executable string) ([]byte, error) {
	// #nosec G204 — executable is the path this process just verified and installed.
	cmd := exec.CommandContext(ctx, executable, "service", "refresh", "--json")
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%s service refresh: %s", executable, detail)
	}
	return out, nil
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
