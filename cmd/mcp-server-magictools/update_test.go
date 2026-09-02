package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/maccavelli/mcplib/selfupdate"
	"github.com/spf13/cobra"
)

func newTestUpdateRoot(t *testing.T, build func(io.Writer) (*selfupdate.Updater, error)) *cobra.Command {
	t.Helper()
	prev := newUpdater
	newUpdater = build
	t.Cleanup(func() { newUpdater = prev })

	root := &cobra.Command{Use: cmdName}
	root.AddCommand(newUpdateCmd())
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetIn(strings.NewReader(""))
	return root
}

func unreachableBuild(io.Writer) (*selfupdate.Updater, error) {
	return nil, errors.New("updater construction should not have been reached")
}

func TestUpdateFlagSurface(t *testing.T) {
	cmd := newUpdateCmd()
	for _, name := range []string{"check", "yes", "force", "version"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s", name)
		}
	}
	if f := cmd.Flags().ShorthandLookup("y"); f == nil || f.Name != "yes" {
		t.Error("missing -y shorthand for --yes")
	}
}

func TestUpdateRejectsPositionalArgs(t *testing.T) {
	cmd := newUpdateCmd()
	if err := cmd.Args(cmd, []string{"stray"}); err == nil {
		t.Fatal("expected positional arguments to be rejected")
	}
}

func TestUpdateRejectsContradictoryFlags(t *testing.T) {
	for _, flag := range []string{"--yes", "--force"} {
		t.Run(flag, func(t *testing.T) {
			root := newTestUpdateRoot(t, defaultUpdater)
			root.SetArgs([]string{"update", "--check", flag})
			err := root.ExecuteContext(context.Background())
			if err == nil {
				t.Fatalf("--check %s was accepted", flag)
			}
			if errors.Is(err, selfupdate.ErrUpdateAvailable) {
				t.Fatal("contradiction was not detected before evaluation")
			}
		})
	}
}

func TestUpdateUsesCallerContext(t *testing.T) {
	root := newTestUpdateRoot(t, defaultUpdater)
	root.SetArgs([]string{"update", "--check"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := root.ExecuteContext(ctx); err == nil {
		t.Fatal("a cancelled caller context did not abort the command")
	}
}

// TestUpdateWritesNothingToStdout keeps update output off the JSON-RPC stream.
func TestUpdateWritesNothingToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	prev := newUpdater
	newUpdater = func(w io.Writer) (*selfupdate.Updater, error) {
		if w != io.Writer(&stderr) {
			t.Error("updater was given a stream other than the command's error stream")
		}
		return nil, errors.New("stop before any work")
	}
	t.Cleanup(func() { newUpdater = prev })

	cmd := newUpdateCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--check"})
	_ = cmd.ExecuteContext(context.Background())
	if stdout.Len() != 0 {
		t.Fatalf("update wrote %q to stdout", stdout.String())
	}
}

// TestUpdateStartsNoRuntime proves check does not construct the orchestrator,
// open the datastore, or hijack stdout. RealStdout staying nil is the
// observable form of "HijackStdout was never called".
func TestUpdateStartsNoRuntime(t *testing.T) {
	prevStdout := RealStdout
	RealStdout = nil
	t.Cleanup(func() { RealStdout = prevStdout })

	root := newTestUpdateRoot(t, unreachableBuild)
	root.SetArgs([]string{"update", "--check"})
	_ = root.ExecuteContext(context.Background())

	if RealStdout != nil {
		t.Fatal("update hijacked stdout; it must not touch the protocol stream")
	}
}

func TestBuildKindMapping(t *testing.T) {
	prev := RawBuildKind
	t.Cleanup(func() { RawBuildKind = prev })
	for _, tc := range []struct {
		stamp string
		want  selfupdate.BuildKind
	}{
		{"release", selfupdate.ReleaseBuild},
		{"local", selfupdate.LocalBuild},
		{"", selfupdate.LocalBuild},
		{"Release", selfupdate.LocalBuild},
	} {
		RawBuildKind = tc.stamp
		if got := buildKind(); got != tc.want {
			t.Errorf("RawBuildKind=%q -> %v, want %v", tc.stamp, got, tc.want)
		}
	}
}

// TestDefaultVersionIsNotAReleaseIdentity guards the hard-coded "v4.3.2"
// default, which outranked every real tag.
func TestDefaultVersionIsNotAReleaseIdentity(t *testing.T) {
	if RawVersion == "v4.3.2" {
		t.Fatal("RawVersion still defaults to the fabricated v4.3.2")
	}
	if RawBuildKind == releaseBuildKind {
		t.Fatal("an unstamped build must not claim to be a release build")
	}
	if err := selfupdate.NewStrictVersionPolicy().Validate(RawVersion); err == nil {
		t.Fatalf("default RawVersion %q validates as a release tag; it must not", RawVersion)
	}
}

func TestUpdatePlatformsAreTheFrozenMatrix(t *testing.T) {
	want := map[string]bool{"linux/amd64": true, "darwin/arm64": true, "windows/amd64": true}
	if len(updatePlatforms) != len(want) {
		t.Fatalf("updatePlatforms = %v", updatePlatforms)
	}
	for _, p := range updatePlatforms {
		if !want[p.OS+"/"+p.Arch] {
			t.Errorf("unexpected platform %v", p)
		}
	}
	if _, err := selfupdate.NewExactAssetSelector(updatePlatforms); err != nil {
		t.Fatalf("selector rejected the frozen matrix: %v", err)
	}
}

// --- reconciler ------------------------------------------------------------

func TestReconcilerCapturesReceipt(t *testing.T) {
	want := refreshReceipt{Changed: true, Path: "/u/x.service", BackupPath: "/u/x.service.prev", Detail: "definition rewritten"}
	body, _ := json.Marshal(want)
	r := &updateReconciler{runFn: func(context.Context, string) ([]byte, error) { return body, nil }}

	res, err := r.Reconcile(context.Background(), cmdName, "/bin/x")
	if err != nil {
		t.Fatalf("Reconcile = %v", err)
	}
	if !res.Changed || res.Detail != want.Detail {
		t.Fatalf("result = %+v", res)
	}
	got, ok := res.State.(refreshReceipt)
	if !ok || got != want {
		t.Fatalf("receipt = %+v (ok=%v), want %+v", got, ok, want)
	}
}

// TestReconcilerFailureIsFatal proves a refresh failure surfaces as an error
// so the shared rollback path runs, rather than being logged as a warning.
func TestReconcilerFailureIsFatal(t *testing.T) {
	r := &updateReconciler{runFn: func(context.Context, string) ([]byte, error) {
		return nil, errors.New("refresh exploded")
	}}
	if _, err := r.Reconcile(context.Background(), cmdName, "/bin/x"); err == nil {
		t.Fatal("a refresh failure must be fatal")
	}
}

func TestReconcilerRejectsMalformedReceipt(t *testing.T) {
	r := &updateReconciler{runFn: func(context.Context, string) ([]byte, error) {
		return []byte("not json"), nil
	}}
	if _, err := r.Reconcile(context.Background(), cmdName, "/bin/x"); err == nil {
		t.Fatal("a malformed receipt must be an error")
	}
}

func TestReconcilerRestoreSkipsUnchanged(t *testing.T) {
	r := &updateReconciler{}
	if err := r.Restore(context.Background(), cmdName, selfupdate.ReconcileResult{Changed: false}); err != nil {
		t.Fatalf("Restore = %v", err)
	}
}

// TestReconcilerRestoreRejectsForeignReceipt refuses to report a rollback it
// did not perform.
func TestReconcilerRestoreRejectsForeignReceipt(t *testing.T) {
	r := &updateReconciler{}
	err := r.Restore(context.Background(), cmdName, selfupdate.ReconcileResult{Changed: true, State: "not a receipt"})
	if err == nil {
		t.Fatal("expected an error for a foreign receipt")
	}
}
