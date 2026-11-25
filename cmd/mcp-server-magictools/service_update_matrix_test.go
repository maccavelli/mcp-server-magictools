package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
)

type matrixLife struct {
	installed bool
	running   bool
	calls     *[]string
	healthErr error
}

func (m *matrixLife) Installed(context.Context, string) (bool, error) {
	*m.calls = append(*m.calls, "installed")
	return m.installed, nil
}
func (m *matrixLife) Running(context.Context, string) (bool, error) {
	*m.calls = append(*m.calls, "running")
	return m.running, nil
}
func (m *matrixLife) Stop(context.Context, string) error {
	*m.calls = append(*m.calls, "stop")
	m.running = false
	return nil
}
func (m *matrixLife) Start(context.Context, string) error {
	*m.calls = append(*m.calls, "start")
	m.running = true
	return nil
}
func (m *matrixLife) WaitHealthy(context.Context, string) error {
	*m.calls = append(*m.calls, "health")
	return m.healthErr
}

type matrixRec struct {
	calls      *[]string
	recErr     error
	restoreErr error
}

func (r *matrixRec) Reconcile(context.Context, string, string) (selfupdate.ReconcileResult, error) {
	*r.calls = append(*r.calls, "reconcile")
	if r.recErr != nil {
		// A partial rewrite still carries a receipt. Recovery must restore it.
		return selfupdate.ReconcileResult{Changed: true, State: "partial"}, r.recErr
	}
	return selfupdate.ReconcileResult{Changed: true, State: "receipt"}, nil
}
func (r *matrixRec) Restore(context.Context, string, selfupdate.ReconcileResult) error {
	*r.calls = append(*r.calls, "restore")
	return r.restoreErr
}

type matrixSession struct {
	calls       *[]string
	applyErr    error
	rollbackErr error
}

func (s *matrixSession) Target() selfupdate.Target { return selfupdate.Target{Path: "/bin"} }
func (s *matrixSession) CreateStaging(context.Context) (*os.File, string, error) {
	return nil, "", nil
}
func (s *matrixSession) Install(context.Context, selfupdate.InstallRequest) (selfupdate.InstallResult, error) {
	*s.calls = append(*s.calls, "install")
	return selfupdate.InstallResult{Applied: true}, nil
}
func (s *matrixSession) Close() error     { return nil }
func (s *matrixSession) Owns(string) bool { return true }
func (s *matrixSession) Apply(context.Context, selfupdate.InstallRequest) (selfupdate.AppliedReplacement, error) {
	*s.calls = append(*s.calls, "apply")
	if s.applyErr != nil {
		return selfupdate.AppliedReplacement{}, s.applyErr
	}
	return selfupdate.AppliedReplacement{Target: "/bin", Backup: "/bin.bak"}, nil
}
func (s *matrixSession) Commit(context.Context, selfupdate.AppliedReplacement) (selfupdate.InstallResult, error) {
	*s.calls = append(*s.calls, "commit")
	return selfupdate.InstallResult{Applied: true, Target: "/bin"}, nil
}
func (s *matrixSession) Rollback(context.Context, selfupdate.AppliedReplacement) error {
	*s.calls = append(*s.calls, "rollback")
	return s.rollbackErr
}

type matrixInstaller struct{ sess *matrixSession }

func (m matrixInstaller) ResolveTarget(context.Context) (selfupdate.Target, error) {
	return selfupdate.Target{Path: "/bin"}, nil
}
func (m matrixInstaller) Begin(context.Context, selfupdate.Target) (selfupdate.InstallSession, error) {
	return m.sess, nil
}

func runMatrix(t *testing.T, life *matrixLife, rec *matrixRec, sess *matrixSession) error {
	t.Helper()
	managed, err := selfupdate.NewManagedInstallerFor(matrixInstaller{sess: sess}, life, rec)
	if err != nil {
		t.Fatal(err)
	}
	session, err := managed.Begin(context.Background(), selfupdate.Target{Path: "/bin"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err = session.Install(context.Background(), selfupdate.InstallRequest{Product: cmdName})
	return err
}

func TestManagedUpdatePreservesStoppedAndRecoversRunning(t *testing.T) {
	calls := []string{}
	life := &matrixLife{installed: true, running: true, calls: &calls}
	rec := &matrixRec{calls: &calls}
	sess := &matrixSession{calls: &calls}
	if err := runMatrix(t, life, rec, sess); err != nil {
		t.Fatal(err)
	}
	want := []string{"installed", "running", "stop", "apply", "reconcile", "start", "health", "commit"}
	if !sameStrings(calls, want) {
		t.Fatalf("running update = %v, want %v", calls, want)
	}

	calls = nil
	life = &matrixLife{installed: true, running: false, calls: &calls}
	rec = &matrixRec{calls: &calls}
	sess = &matrixSession{calls: &calls}
	if err := runMatrix(t, life, rec, sess); err != nil {
		t.Fatal(err)
	}
	want = []string{"installed", "running", "apply", "reconcile", "commit"}
	if !sameStrings(calls, want) {
		t.Fatalf("stopped update started the service: %v", calls)
	}

	calls = nil
	probe := errors.New("not ready")
	life = &matrixLife{installed: true, running: true, calls: &calls, healthErr: probe}
	rec = &matrixRec{calls: &calls}
	sess = &matrixSession{calls: &calls}
	err := runMatrix(t, life, rec, sess)
	if !errors.Is(err, probe) {
		t.Fatalf("health failure lost original error: %v", err)
	}
	if !containsInOrder(calls, []string{"stop", "apply", "reconcile", "start", "health", "stop", "restore", "rollback", "start"}) {
		t.Fatalf("recovery restored while replacement could still be running, or dropped a step: %v", calls)
	}

	calls = nil
	origin := errors.New("reconcile failed")
	life = &matrixLife{installed: true, running: false, calls: &calls}
	rec = &matrixRec{calls: &calls, recErr: origin}
	sess = &matrixSession{calls: &calls}
	err = runMatrix(t, life, rec, sess)
	if !errors.Is(err, origin) {
		t.Fatalf("reconcile failure lost original error: %v", err)
	}
	if containsString(calls, "start") || !containsInOrder(calls, []string{"apply", "reconcile", "restore", "rollback"}) {
		t.Fatalf("stopped recovery = %v", calls)
	}
}

func TestUpdateLifecycleDoesNotExposeEnabled(t *testing.T) {
	var life selfupdate.Lifecycle = &updateLifecycle{target: "/service"}
	if _, ok := life.(selfupdate.EnabledLifecycle); ok {
		t.Fatal("update adapter exposes EnabledLifecycle; a stopped service would be started")
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func containsString(got []string, want string) bool {
	for _, g := range got {
		if g == want {
			return true
		}
	}
	return false
}

func containsInOrder(got, want []string) bool {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i == len(want)
}
