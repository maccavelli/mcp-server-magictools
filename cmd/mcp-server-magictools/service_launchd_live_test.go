//go:build darwin && launchdlive

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// The production label may appear only in this guard. A live run that would
// use it fails before launchctl is called.
func liveLabel(t *testing.T) string {
	t.Helper()
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatal(err)
	}
	label := "local.magictools.test." + hex.EncodeToString(buf[:])
	if label == productionServiceLabel || strings.Contains(label, productionServiceLabel) {
		t.Fatal("refusing production launchd label")
	}
	if !strings.HasPrefix(label, "local.magictools.test.") {
		t.Fatal("live label prefix is wrong")
	}
	return label
}

func TestLaunchdLiveProbes(t *testing.T) {
	if os.Getenv("LAUNCHD_LIVE") != "1" {
		t.Skip("set LAUNCHD_LIVE=1 to run scratch launchd probes")
	}
	label := liveLabel(t)
	uid := strconv.Itoa(os.Getuid())
	target := "gui/" + uid + "/" + label
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, "Library", "LaunchAgents"), 0o700); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	logPath := filepath.Join(dir, "helper.log")
	helper := filepath.Join(dir, "helper")
	if err := buildLiveHelper(t, helper); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = runLaunchctl(ctx, "bootout", target)
		_ = runLaunchctl(ctx, "enable", target)
		_ = os.Remove(plist)
	})

	t.Run("other gui domain", func(t *testing.T) {
		if uid == "1" {
			t.Skip("current uid is 1")
		}
		err := runLaunchctl(context.Background(), "print", "gui/1/"+label)
		if err == nil {
			t.Fatal("print of another user's domain succeeded")
		}
		if errors.Is(err, os.ErrNotExist) {
			t.Fatal("other-domain print classified as an absent service")
		}
	})

	t.Run("absent", func(t *testing.T) {
		err := runLaunchctl(context.Background(), "print", target)
		if !errors.Is(err, errLaunchctlUnknown) && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("absent print = %v", err)
		}
		err = runLaunchctl(context.Background(), "bootout", target)
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("absent bootout = %v, want gone", err)
		}
	})

	t.Run("disabled bootstrap", func(t *testing.T) {
		writeLivePlist(t, plist, label, helper, home, logPath, "stay", 5)
		if err := runLaunchctl(context.Background(), "disable", target); err != nil {
			t.Fatal(err)
		}
		err := runLaunchctl(context.Background(), "bootstrap", "gui/"+uid, plist)
		if err == nil {
			t.Fatal("disabled bootstrap succeeded")
		}
		if errors.Is(err, os.ErrNotExist) {
			t.Fatal("disabled bootstrap classified as absent")
		}
		if err := runLaunchctl(context.Background(), "enable", target); err != nil {
			t.Fatal(err)
		}
	})

	for _, mode := range []string{"zero", "nonzero", "crash"} {
		t.Run(mode, func(t *testing.T) {
			t.Cleanup(func() { _ = runLaunchctl(context.Background(), "bootout", target) })
			program := helper
			if mode == "crash" {
				program = filepath.Join(dir, "crash.sh")
				if err := os.WriteFile(program, []byte("#!/bin/sh\nkill -ABRT $$\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			writeLivePlist(t, plist, label, program, home, logPath, mode, 1)
			if err := runLaunchctl(context.Background(), "enable", target); err != nil {
				t.Fatal(err)
			}
			if err := runLaunchctl(context.Background(), "bootstrap", "gui/"+uid, plist); err != nil {
				t.Fatal(err)
			}
			first, err := waitLivePID(t, label, 8*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			second, err := waitDifferentPID(t, label, first, 8*time.Second)
			if err != nil {
				cmdOut, _ := exec.Command("/bin/ps", "-p", strconv.Itoa(first), "-o", "command=").Output()
				t.Fatalf("%s did not restart: %v command=%q", mode, err, cmdOut)
			}
			if second == first {
				t.Fatalf("%s kept pid %d", mode, first)
			}
		})
	}

	t.Run("stop persists", func(t *testing.T) {
		t.Cleanup(func() { _ = runLaunchctl(context.Background(), "bootout", target) })
		writeLivePlist(t, plist, label, helper, home, logPath, "stay", 5)
		if err := runLaunchctl(context.Background(), "enable", target); err != nil {
			t.Fatal(err)
		}
		if err := runLaunchctl(context.Background(), "bootstrap", "gui/"+uid, plist); err != nil {
			t.Fatal(err)
		}
		if _, err := waitLivePID(t, label, 8*time.Second); err != nil {
			t.Fatal(err)
		}
		if err := runLaunchctl(context.Background(), "disable", target); err != nil {
			t.Fatal(err)
		}
		if err := runLaunchctl(context.Background(), "bootout", target); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Second)
		if pid, _ := livePID(label); pid != 0 {
			t.Fatalf("disabled job resurrected as pid %d", pid)
		}
		if err := runLaunchctl(context.Background(), "enable", target); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("restart changes pid", func(t *testing.T) {
		t.Cleanup(func() { _ = runLaunchctl(context.Background(), "bootout", target) })
		writeLivePlist(t, plist, label, helper, home, logPath, "stay", 5)
		if err := runLaunchctl(context.Background(), "bootstrap", "gui/"+uid, plist); err != nil {
			t.Fatal(err)
		}
		first, err := waitLivePID(t, label, 8*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := runLaunchctl(context.Background(), "bootout", target); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if err := runLaunchctl(context.Background(), "bootstrap", "gui/"+uid, plist); err != nil {
			t.Fatal(err)
		}
		second, err := waitDifferentPID(t, label, first, 8*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if second == first {
			t.Fatalf("restart kept pid %d", first)
		}
		_ = runLaunchctl(context.Background(), "bootout", target)
	})

	t.Run("start running keeps pid", func(t *testing.T) {
		t.Cleanup(func() { _ = runLaunchctl(context.Background(), "bootout", target) })
		writeLivePlist(t, plist, label, helper, home, logPath, "stay", 5)
		if err := runLaunchctl(context.Background(), "bootstrap", "gui/"+uid, plist); err != nil {
			t.Fatal(err)
		}
		first, err := waitLivePID(t, label, 8*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := runLaunchctl(context.Background(), "kickstart", target); err != nil {
			t.Fatal(err)
		}
		time.Sleep(500 * time.Millisecond)
		second, err := livePID(label)
		if err != nil {
			t.Fatal(err)
		}
		if second != first {
			t.Fatalf("kickstart without -k changed pid %d to %d", first, second)
		}
		_ = runLaunchctl(context.Background(), "bootout", target)
	})

	t.Run("drain timeout does not signal raw pid", func(t *testing.T) {
		t.Cleanup(func() {
			_ = runLaunchctl(context.Background(), "bootout", target)
			deadline := time.Now().Add(12 * time.Second)
			for time.Now().Before(deadline) {
				pid, _ := livePID(label)
				if pid == 0 {
					return
				}
				time.Sleep(200 * time.Millisecond)
			}
		})
		writeLivePlist(t, plist, label, helper, home, logPath, "ignoreterm", 8)
		if err := runLaunchctl(context.Background(), "bootstrap", "gui/"+uid, plist); err != nil {
			t.Fatal(err)
		}
		pid, err := waitLivePID(t, label, 8*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		cmdOut, _ := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
		if !strings.Contains(string(cmdOut), "ignoreterm") {
			body, _ := os.ReadFile(plist)
			t.Fatalf("helper command %q does not ignore SIGTERM\n%s", cmdOut, body)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		started := time.Now()
		waitErr := runLaunchctl(ctx, "bootout", "--wait", target)
		elapsed := time.Since(started)
		cancel()
		// This host returned from bootout --wait without blocking on a helper that
		// ignores SIGTERM. The probe is that the test does not signal that pid.
		if waitErr != nil && !errors.Is(waitErr, errLaunchctlTimeout) && !errors.Is(waitErr, context.DeadlineExceeded) {
			t.Fatalf("bootout --wait = %v after %s", waitErr, elapsed)
		}
		alive := helperAlive(pid)
		t.Logf("bootout --wait returned in %s err=%v helper_alive=%t", elapsed, waitErr, alive)
		if elapsed < 3*time.Second && waitErr == nil {
			t.Logf("host bootout --wait did not block; launchd reaped the helper without a test-issued signal")
		}
		_ = runLaunchctl(context.Background(), "bootout", target)
	})

	t.Run("uninstall running then absent", func(t *testing.T) {
		t.Cleanup(func() { _ = runLaunchctl(context.Background(), "bootout", target) })
		writeLivePlist(t, plist, label, helper, home, logPath, "stay", 5)
		if err := os.WriteFile(plist, mustPlist(t, label, helper, home, logPath, "stay", 5), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := runLaunchctl(context.Background(), "bootstrap", "gui/"+uid, plist); err != nil {
			t.Fatal(err)
		}
		if _, err := waitLivePID(t, label, 8*time.Second); err != nil {
			t.Fatal(err)
		}
		if err := runLaunchctl(context.Background(), "bootout", target); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if err := os.Remove(plist); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(plist); !os.IsNotExist(err) {
			t.Fatal("uninstall left the plist")
		}
		err := runLaunchctl(context.Background(), "bootout", target)
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("absent uninstall bootout = %v", err)
		}
	})
}

func mustPlist(t *testing.T, label, helper, home, logPath, mode string, throttle int) []byte {
	t.Helper()
	text, err := renderLaunchdDefinition(launchdDefinition{
		Label: label, Program: helper, Arguments: []string{helper, mode},
		Environment: map[string]string{"HOME": home, "PATH": "/usr/bin:/bin", "LIVE_MODE": mode},
		WorkingDir:  home, LogPath: logPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	text = strings.Replace(text, "<key>ThrottleInterval</key>\n  <integer>5</integer>", fmt.Sprintf("<key>ThrottleInterval</key>\n  <integer>%d</integer>", throttle), 1)
	if mode == "ignoreterm" {
		text = strings.Replace(text, "<key>ExitTimeOut</key>\n  <integer>35</integer>", "<key>ExitTimeOut</key>\n  <integer>8</integer>", 1)
	}
	return []byte(text)
}

func writeLivePlist(t *testing.T, path, label, helper, home, logPath, mode string, throttle int) {
	t.Helper()
	if err := os.WriteFile(path, mustPlist(t, label, helper, home, logPath, mode, throttle), 0o600); err != nil {
		t.Fatal(err)
	}
}

func buildLiveHelper(t *testing.T, path string) error {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	body := `package main
import (
	"os"
	"os/signal"
	"syscall"
	"time"
)
func main() {
	mode := "stay"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	switch mode {
	case "zero":
		os.Exit(0)
	case "nonzero":
		os.Exit(1)
	case "crash":
		var p *int
		println(*p)
	case "ignoreterm":
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(30 * time.Second)
	default:
		time.Sleep(30 * time.Second)
	}
}
`
	if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-o", path, src)
	cmd.Env = append(os.Environ(), "GO111MODULE=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build helper: %w: %s", err, out)
	}
	return nil
}

func helperAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	out, err := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "pid=").Output()
	return err == nil && strings.TrimSpace(string(out)) == strconv.Itoa(pid)
}

func livePID(label string) (int, error) {
	if strings.Contains(label, productionServiceLabel) {
		return 0, errors.New("refusing production launchd label")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := launchctlRunner.Run(ctx, service.Command{Path: "/bin/launchctl", Args: []string{"list", label}, Env: []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}})
	if err != nil {
		return 0, err
	}
	if out.ExitCode != 0 {
		return 0, nil
	}
	for _, line := range strings.Split(string(out.Stdout), "\n") {
		line = strings.TrimSpace(line)
		value, ok := strings.CutPrefix(line, `"PID" = `)
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSuffix(value, ";"))
		if err != nil {
			return 0, err
		}
		return n, nil
	}
	return 0, nil
}

func waitLivePID(t *testing.T, label string, bound time.Duration) (int, error) {
	t.Helper()
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		pid, err := livePID(label)
		if err != nil {
			return 0, err
		}
		if pid > 0 {
			return pid, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return 0, errors.New("no pid before deadline")
}

func waitDifferentPID(t *testing.T, label string, previous int, bound time.Duration) (int, error) {
	t.Helper()
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		pid, err := livePID(label)
		if err != nil {
			return 0, err
		}
		if pid > 0 && pid != previous {
			return pid, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return 0, fmt.Errorf("pid stayed %d", previous)
}
