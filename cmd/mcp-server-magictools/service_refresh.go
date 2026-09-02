package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

// refreshReceipt is the typed JSON result of a hidden service refresh. The
// updater captures it in ReconcileResult.State and hands it back on rollback,
// so a restore never has to re-derive what changed.
type refreshReceipt struct {
	Changed    bool   `json:"changed"`
	Path       string `json:"path,omitempty"`
	BackupPath string `json:"backup_path,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// serviceDefinitionPathFn is the seam tests replace so definition handling can
// be exercised without a real service manager.
var serviceDefinitionPathFn = platformServiceDefinitionPath

// serviceDefinitionPath returns this platform's service definition path and
// whether one is expected at all.
func serviceDefinitionPath() (string, bool) { return serviceDefinitionPathFn() }

// platformServiceDefinitionPath is the real per-platform location.
func platformServiceDefinitionPath() (string, bool) {
	switch runtime.GOOS {
	case goOSLinux:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		return filepath.Join(home, ".config", "systemd", "user", cmdName+".service"), true
	case goOSDarwin:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		return filepath.Join(home, "Library", "LaunchAgents", "com."+cmdName+".plist"), true
	case goOSWindows:
		// The Windows definition lives in the service control manager, not on
		// disk. The refresh path handles it through mgr.Config.
		return "", false
	default:
		return "", false
	}
}

// newServiceRefreshCmd builds the hidden `service refresh` operation.
//
// The child process is the point: an updater is the OLD binary and carries the
// OLD definition template, so only the binary just installed can render the
// definition its own release ships. This command therefore rewrites ONLY an
// existing definition and never installs or enables a missing service — an
// update must not create a service the user never asked for.
func newServiceRefreshCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:    "refresh",
		Short:  "Rewrite an existing service definition from this binary",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			receipt, err := refreshServiceDefinition()
			if err != nil {
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				return enc.Encode(receipt)
			}
			if !receipt.Changed {
				_, err := fmt.Fprintln(cmd.ErrOrStderr(), "service definition already current")
				return err
			}
			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "service definition refreshed: %s\n", receipt.Path)
			return err
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a machine-readable receipt")
	return cmd
}

// refreshServiceDefinition rewrites an existing definition in place, keeping a
// backup so a failed update can restore exactly what was there.
func refreshServiceDefinition() (refreshReceipt, error) {
	path, expected := serviceDefinitionPath()
	if !expected {
		// Windows and unsupported platforms: nothing on disk to rewrite. The
		// service manager configuration is left untouched rather than guessed.
		return refreshReceipt{Changed: false, Detail: "no on-disk definition on " + runtime.GOOS}, nil
	}
	current, err := os.ReadFile(path) // #nosec G304 — fixed per-platform path
	if err != nil {
		if os.IsNotExist(err) {
			// Absent means binary-only: never install a service implicitly.
			return refreshReceipt{Changed: false, Detail: "no installed definition"}, nil
		}
		return refreshReceipt{}, fmt.Errorf("read service definition: %w", err)
	}

	desired, err := renderServiceDefinition()
	if err != nil {
		return refreshReceipt{}, err
	}
	if string(current) == desired {
		return refreshReceipt{Changed: false, Path: path, Detail: "already current"}, nil
	}

	backup := path + ".prev"
	if err := os.WriteFile(backup, current, 0o600); err != nil {
		return refreshReceipt{}, fmt.Errorf("write definition backup: %w", err)
	}
	if err := writeFileSynced(path, []byte(desired)); err != nil {
		return refreshReceipt{}, fmt.Errorf("write service definition: %w", err)
	}
	if err := reloadServiceManager(); err != nil {
		// Put the old definition back before reporting: a half-applied
		// definition is worse than an unchanged one. A failure to restore is
		// joined so neither error is hidden.
		if rerr := writeFileSynced(path, current); rerr != nil {
			return refreshReceipt{}, errors.Join(fmt.Errorf("reload service manager: %w", err), fmt.Errorf("restore previous definition: %w", rerr))
		}
		return refreshReceipt{}, fmt.Errorf("reload service manager: %w", err)
	}
	return refreshReceipt{Changed: true, Path: path, BackupPath: backup, Detail: "definition rewritten"}, nil
}

// restoreServiceDefinition puts a backed-up definition back during rollback.
func restoreServiceDefinition(r refreshReceipt) error {
	if !r.Changed || r.Path == "" || r.BackupPath == "" {
		return nil
	}
	data, err := os.ReadFile(r.BackupPath) // #nosec G304 — path came from our own receipt
	if err != nil {
		return fmt.Errorf("read definition backup: %w", err)
	}
	if err := writeFileSynced(r.Path, data); err != nil {
		return fmt.Errorf("restore service definition: %w", err)
	}
	if err := reloadServiceManager(); err != nil {
		return fmt.Errorf("reload after restore: %w", err)
	}
	if rerr := os.Remove(r.BackupPath); rerr != nil && !os.IsNotExist(rerr) {
		return fmt.Errorf("remove definition backup: %w", rerr)
	}
	return nil
}

// writeFileSynced writes and fsyncs, so a definition is durable before the
// service manager is told to reload it.
func writeFileSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) // #nosec G304
	if err != nil {
		return err
	}
	if _, werr := f.Write(data); werr != nil {
		return errors.Join(werr, f.Close())
	}
	if serr := f.Sync(); serr != nil {
		return errors.Join(serr, f.Close())
	}
	return f.Close()
}

// renderServiceDefinition renders the definition THIS binary ships, for the
// executable this process is running as. It reuses the same templates the
// install path uses, so a refresh and a fresh install can never drift.
func renderServiceDefinition() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(self); rerr == nil {
		self = resolved
	}
	switch runtime.GOOS {
	case goOSLinux:
		return renderSystemdUnit(self)
	case goOSDarwin:
		return renderLaunchdPlist(self)
	default:
		return "", fmt.Errorf("no definition template for %s", runtime.GOOS)
	}
}

// launchdGUITarget is the per-user launchd domain this service runs in.
func launchdGUITarget() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

// renderSystemdUnit renders the unit for binPath using the same template and
// the same field values the install path uses.
func renderSystemdUnit(binPath string) (string, error) {
	tmpl, err := template.New("systemd").Parse(systemdUnitTemplate)
	if err != nil {
		return "", fmt.Errorf("parse systemd template: %w", err)
	}
	binDir := filepath.Dir(binPath)
	data := struct {
		BinPath     string
		BindAddr    string
		RecallURL   string
		SocraticURL string
		EnvPath     string
		Home        string
	}{
		BinPath:     binPath,
		BindAddr:    config.ResolveBindAddr(""),
		RecallURL:   resolveRecallURL(),
		SocraticURL: resolveSocraticURL(),
		EnvPath:     binDir + ":/usr/local/bin:/usr/bin:/bin",
		Home:        os.Getenv("HOME"),
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render systemd unit: %w", err)
	}
	return buf.String(), nil
}

// renderLaunchdPlist renders the plist for binPath using the same template and
// the same field values the install path uses.
func renderLaunchdPlist(binPath string) (string, error) {
	tmpl, err := template.New("launchd").Parse(launchdPlistTemplate)
	if err != nil {
		return "", fmt.Errorf("parse launchd template: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	data := struct {
		BinPath     string
		BindAddr    string
		LogDir      string
		EnvPath     string
		RecallURL   string
		SocraticURL string
	}{
		BinPath:     binPath,
		BindAddr:    config.ResolveBindAddr(""),
		LogDir:      filepath.Join(home, "Library", "Caches", cmdName),
		EnvPath:     buildLaunchdPath(home),
		RecallURL:   resolveRecallURL(),
		SocraticURL: resolveSocraticURL(),
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render launchd plist: %w", err)
	}
	return buf.String(), nil
}

// reloadServiceManager tells the platform manager to re-read the definition.
// launchd re-reads a plist on next bootstrap, so only systemd needs a reload.
func reloadServiceManager() error {
	if runtime.GOOS != goOSLinux {
		return nil
	}
	if _, err := timedExec("systemctl", systemctlUserFlag, "daemon-reload"); err != nil {
		return err
	}
	return nil
}
