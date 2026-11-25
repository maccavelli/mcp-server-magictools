// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"

	"github.com/pterm/pterm"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/spf13/cobra"
)

// serviceState mirrors the JSON state file written by the running service.
type cliServiceState struct {
	PID           int    `json:"pid"`
	Addr          string `json:"addr"`
	Started       string `json:"started"`
	ConfigVersion string `json:"config_version"`
	BinaryPath    string `json:"binary_path"`
	StartToken    uint64 `json:"start_token"`
}

var forceServiceInstall bool
var jsonStatusOutput bool
var serviceBinPath string

var serviceCmd = &cobra.Command{
	Use: "service",
	// Short is augmented in init() with the live list of subcommands so the
	// top-level help never drifts from the registered children.
	Short: "Manage the magictools background service",
}

// -------------------------------------------------------------------------
// service install
// -------------------------------------------------------------------------

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the magictools service for the current OS",
	RunE:  runServiceInstall,
}

// getDefaultBinPath returns the OS-specific default binary installation path for magictools.
func getDefaultBinPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	switch runtime.GOOS {
	case goOSWindows:
		return filepath.Join(home, "AppData", "Local", "Programs", "magictools", "mcp-server-magictools.exe")
	default:
		return filepath.Join(home, ".local", "bin", "mcp-server-magictools")
	}
}

// promptForBinPath interactively asks the user for the binary path, falling back to defaults.
func promptForBinPath() string {
	if serviceBinPath != "" {
		return serviceBinPath
	}

	defaultPath := getDefaultBinPath()
	binPath, err := pterm.DefaultInteractiveTextInput.
		WithDefaultText("Enter the path to the magictools binary").
		WithDefaultValue(defaultPath).
		Show()
	if err != nil || binPath == "" {
		return defaultPath
	}
	return binPath
}

func runServiceInstall(cmd *cobra.Command, args []string) error {
	binPath := promptForBinPath()

	// Resolve symlinks for launchd absolute path requirement
	if resolved, err := filepath.EvalSymlinks(binPath); err == nil {
		binPath = resolved
	}

	// HARD-1: Pre-flight validation
	if err := validateBinary(binPath); err != nil {
		return fmt.Errorf("pre-flight validation failed: %w", err)
	}

	// MISS-5: Ensure config directory exists
	cfgDir := config.DefaultConfigDir()
	if err := os.MkdirAll(cfgDir, 0700); err != nil {
		return fmt.Errorf("failed to create config dir %s: %w", cfgDir, err)
	}

	// Platform-specific pre-flight checks
	switch runtime.GOOS {
	case goOSLinux:
		// MISS-6: Verify systemd --user session is available
		if _, err := timedExec("systemctl", systemctlUserFlag, "status"); err != nil {
			// HARD-19: Check DBUS_SESSION_BUS_ADDRESS
			if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
				fmt.Println("⚠ DBUS_SESSION_BUS_ADDRESS is not set.")
				fmt.Println("  systemd user services require an active D-Bus session.")
				fmt.Println("  If running via SSH, ensure PAM is configured or run from a desktop session.")
			}
			return fmt.Errorf("systemd --user session is not available: %w", err)
		}
		// MISS-1/REV-5: Linger warning
		out, err := timedExec("loginctl", "show-user", os.Getenv("USER"), "-p", "Linger")
		if err == nil && !strings.Contains(string(out), "Linger=yes") {
			fmt.Println("⚠ loginctl linger is NOT enabled for your user.")
			fmt.Println("  The service will stop when you log out.")
			fmt.Println("  WARNING: Enabling linger allows your processes to persist after logout.")
			fmt.Println("  Check with your system administrator if this is permitted by your organization's security policy.")
			fmt.Println("  To enable: loginctl enable-linger $USER")
			fmt.Println()
		}
	case goOSWindows:
		// HARD-5: Verify schtasks is available
		if _, err := exec.LookPath("schtasks"); err != nil {
			return fmt.Errorf("schtasks not found in PATH — Windows Task Scheduler is required: %w", err)
		}
	}

	var installErr error
	switch runtime.GOOS {
	case goOSLinux:
		installErr = installSystemd(binPath)
	case goOSDarwin:
		installErr = installLaunchd(binPath)
	case goOSWindows:
		installErr = installWindows(binPath)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	// MISS-2/REV-7: Post-install health verification
	if installErr == nil {
		fmt.Println("  Verifying service health...")
		healthy := false
		for range 20 { // 10 seconds
			time.Sleep(500 * time.Millisecond)
			if state, err := readExpectedServiceState(processIdentity{}); err == nil {
				fmt.Printf("  ✓ Service is healthy (PID: %d, Addr: %s)\n", state.PID, state.Addr)
				healthy = true
				break
			}
		}
		if !healthy {
			fmt.Println("  ⚠ Service may not have started successfully. Run 'service doctor' for diagnostics.")
		}
	}

	// TEL-2: Audit trail
	auditServiceEvent("install", binPath, installErr)
	return installErr
}

// validateBinary performs pre-flight checks on the binary before service installation.
func validateBinary(binPath string) error {
	info, err := os.Stat(binPath)
	if err != nil {
		return fmt.Errorf("binary not found at %s: %w", binPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a binary: %s", binPath)
	}
	// Check executable permission (Unix only)
	if runtime.GOOS != goOSWindows {
		if info.Mode()&0111 == 0 {
			return fmt.Errorf("binary is not executable: %s (mode: %s)", binPath, info.Mode())
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// service uninstall
// -------------------------------------------------------------------------

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall the magictools background service",
	RunE:  runServiceUninstall,
}

func runServiceUninstall(cmd *cobra.Command, args []string) error {
	binPath := promptForBinPath()

	var uninstallErr error
	switch runtime.GOOS {
	case goOSLinux:
		uninstallErr = uninstallSystemd()
	case goOSDarwin:
		uninstallErr = uninstallLaunchd()
	case goOSWindows:
		uninstallErr = uninstallWindows()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	// TEL-2: Audit trail
	auditServiceEvent("uninstall", binPath, uninstallErr)
	return uninstallErr
}

// -------------------------------------------------------------------------
// service status
// -------------------------------------------------------------------------

var serviceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the status of the magictools background service",
	RunE:  runServiceStatus,
}

// TEL-4: Machine-readable JSON status output
func runServiceStatus(cmd *cobra.Command, args []string) error {
	state, readErr := readExpectedServiceState(processIdentity{})
	registered, regErr := definitionRegistered()
	alive := readErr == nil
	if jsonStatusOutput {
		fmt.Println(string(marshalIndentOrEmpty(buildServiceStatus(state, readErr, registered))))
		return regErr
	}
	if alive {
		fmt.Println("✓ Service process identity is valid")
	} else {
		fmt.Printf("✗ Service is not verified: %v\n", readErr)
	}
	fmt.Printf("  PID:     %d\n  Addr:    %s\n  Started: %s\n  Version: %s\n  Binary:  %s\n",
		state.PID, state.Addr, state.Started, state.ConfigVersion, state.BinaryPath)
	fmt.Printf("  Registered: %t\n", registered)
	return regErr
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// -------------------------------------------------------------------------
// Linux: systemd user service
// -------------------------------------------------------------------------

const systemdUnitTemplate = `[Unit]
Description=MagicTools MCP Orchestrator Service
After=network.target
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=simple
ExecStart={{.BinPath}} serve
ExecReload=/bin/kill -HUP $MAINPID
Environment="HOME={{.Home}}"
Environment="PATH={{.EnvPath}}"
Environment="MCP_SERVICE_MODE=true"
Environment="MCP_ENDPOINT_IDE_PORT={{.BindAddr}}"{{if .RecallURL}}
Environment="MCP_REC_URL={{.RecallURL}}"{{end}}{{if .SocraticURL}}
Environment="MCP_SOC_URL={{.SocraticURL}}"{{end}}
SyslogIdentifier=magictools
Restart=always
RestartSec=5
# A5: mixed sends SIGTERM to the main process only (letting it run its ordered
# drain of the Setpgid sub-server groups), then SIGKILLs stragglers at timeout.
KillMode=mixed
# TimeoutStopSec must exceed the app's own 30s shutdown deadline so systemd does
# not SIGKILL mid-drain.
TimeoutStopSec=35

[Install]
WantedBy=default.target
`

func installSystemd(binPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home dir: %w", err)
	}

	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o750); err != nil {
		return fmt.Errorf("failed to create systemd dir: %w", err)
	}

	unitPath := filepath.Join(unitDir, "mcp-server-magictools.service")

	// BUG-5/HARD-2: Allow --force to overwrite existing service files for upgrades
	if _, err := os.Stat(unitPath); err == nil && !forceServiceInstall {
		fmt.Printf("Service file already exists at %s.\nUse --force to overwrite for upgrades.\n", unitPath)
		return nil
	}

	tmpl, err := template.New("systemd").Parse(systemdUnitTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	f, err := createConfigFile(unitPath)
	if err != nil {
		return fmt.Errorf("failed to create unit file: %w", err)
	}
	defer closeFileOrWarn(f, "systemd-unit")

	// BUG-9/REV-3: Deterministic PATH — binary dir + system base paths.
	binDir := filepath.Dir(binPath)
	envPath := binDir + ":/usr/local/bin:/usr/bin:/bin"

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
		EnvPath:     envPath,
		Home:        os.Getenv("HOME"),
	}

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("failed to write unit file: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync unit file: %w", err)
	}
	// BUG-2: Check chmod error
	if err := os.Chmod(unitPath, 0o600); err != nil {
		fmt.Printf("  ⚠ Failed to set unit file permissions: %v\n", err)
	}

	// Reload and enable (HARD-6/16: with timeout)
	if out, err := timedExec("systemctl", systemctlUserFlag, "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload failed: %s: %w", string(out), err)
	}

	if out, err := timedExec("systemctl", systemctlUserFlag, "enable", "mcp-server-magictools.service"); err != nil {
		// HARD-3: Rollback on partial failure
		removeFileOrWarn(unitPath)
		execSystemdOrWarn("systemctl", systemctlUserFlag, "daemon-reload")
		return fmt.Errorf("systemctl enable failed (rolled back): %s: %w", string(out), err)
	}

	if out, err := timedExec("systemctl", systemctlUserFlag, "start", "mcp-server-magictools.service"); err != nil {
		// HARD-3: Rollback on partial failure — disable + remove
		execSystemdOrWarn("systemctl", systemctlUserFlag, "disable", "mcp-server-magictools.service")
		removeFileOrWarn(unitPath)
		execSystemdOrWarn("systemctl", systemctlUserFlag, "daemon-reload")
		return fmt.Errorf("systemctl start failed (rolled back): %s: %w", string(out), err)
	}

	fmt.Printf("✓ systemd user service installed and started\n")
	fmt.Printf("  Unit: %s\n", unitPath)
	fmt.Printf("  Check: systemctl --user status mcp-server-magictools\n")
	return nil
}

func uninstallSystemd() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home dir: %w", err)
	}

	unitPath := filepath.Join(home, ".config", "systemd", "user", "mcp-server-magictools.service")

	ctx, cancel := serviceActionContext(context.Background())
	defer cancel()
	if err := runSharedServiceAction(ctx, serviceActionStop); err != nil {
		return err
	}
	if _, err := timedExec("systemctl", systemctlUserFlag, "disable", "mcp-server-magictools.service"); err != nil {
		return err
	}

	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove unit file: %w", err)
	}

	execSystemdOrWarn("systemctl", systemctlUserFlag, "daemon-reload")

	cleanupServiceArtifacts()

	fmt.Println("✓ systemd user service removed")
	return nil
}

// -------------------------------------------------------------------------
// macOS: LaunchAgent plist
// -------------------------------------------------------------------------

func launchdPlistPath() (string, error) {
	home, _, err := resolveLaunchdTargetFn()
	if err != nil {
		return "", err
	}
	return home + "/Library/LaunchAgents/" + productionServiceLabel + ".plist", nil
}

// resolveLaunchdTargetFn is the launchd home/uid seam. Tests point it at a
// scratch home so install and uninstall never touch the operator plist.
var resolveLaunchdTargetFn = resolveLaunchdTarget

// resolveLaunchdTarget resolves the current user's LaunchAgent home and uid.
// A mismatched SUDO_UID is refused before any path is used: sudo must not
// mix the invoking process with another user's launchd domain.
func resolveLaunchdTarget() (string, string, error) {
	if sudo := os.Getenv("SUDO_UID"); sudo != "" && sudo != strconv.Itoa(os.Getuid()) {
		return "", "", fmt.Errorf("refusing mismatched sudo launchd target uid %s while running as uid %d: run service commands as the logged-in user in a GUI session", sudo, os.Getuid())
	}

	currentUser, err := user.Current()
	if err != nil {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", "", fmt.Errorf("failed to resolve user home: %w (current error: %w)", homeErr, err)
		}
		// BUG-8: Return error instead of guessing UID 501/staff
		return "", "", fmt.Errorf("failed to resolve current user: %w (home resolved to: %s)", err, home)
	}

	return currentUser.HomeDir, currentUser.Uid, nil
}

// buildLaunchdPath builds a robust PATH including standard macOS and Homebrew locations.
func buildLaunchdPath(userHome string) string {
	path := os.Getenv("PATH")

	// Add common macOS paths if they are not already present
	extraPaths := []string{
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		filepath.Join(userHome, ".local", "bin"),
	}

	pathList := filepath.SplitList(path)
	pathMap := make(map[string]bool)
	for _, p := range pathList {
		pathMap[p] = true
	}

	var merged []string
	for _, p := range extraPaths {
		if !pathMap[p] {
			merged = append(merged, p)
		}
	}
	// Prepend extra paths to ensure Homebrew/local binaries take precedence
	if len(merged) > 0 {
		path = strings.Join(merged, string(filepath.ListSeparator)) + string(filepath.ListSeparator) + path
	}
	return path
}

// chownForUser sets ownership of path to the target UID/GID.
// HARD-1: DRY helper extracted from 3 duplicated blocks in installLaunchd.
func chownForUser(path, uidStr string) {
	uid, err := strconv.Atoi(uidStr)
	if err != nil {
		return
	}
	gid := -1
	if u, err := user.LookupId(uidStr); err == nil {
		if g, err := strconv.Atoi(u.Gid); err == nil {
			gid = g
		}
	}
	chownOrWarn(path, uid, gid)
}

// timedExec runs a command with a 30-second timeout.
// HARD-6/16: Prevents hung systemctl/launchctl/schtasks from blocking forever.
func timedExec(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // platform service-management commands with fixed argv
}

func installLaunchd(binPath string) error {
	ctx, cancel := serviceActionContext(context.Background())
	defer cancel()
	return withServiceLock(ctx, productionServiceLabel, func(context.Context) error {
		return installLaunchdLocked(binPath)
	})
}

func installLaunchdLocked(binPath string) error {
	home, uidStr, err := resolveLaunchdTargetFn()
	if err != nil {
		return err
	}

	agentDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(agentDir, 0o750); err != nil {
		return fmt.Errorf("failed to create LaunchAgents dir: %w", err)
	}

	currentUser, err := user.Current()
	isRoot := err == nil && currentUser.Uid == "0"

	if isRoot {
		chownForUser(agentDir, uidStr)
	}

	plistPath := filepath.Join(agentDir, productionServiceLabel+".plist")

	// BUG-5/HARD-2: Allow --force to overwrite existing service files for upgrades
	if _, err := os.Stat(plistPath); err == nil && !forceServiceInstall {
		fmt.Printf("Service file already exists at %s.\nUse --force to overwrite for upgrades.\n", plistPath)
		return nil
	}

	// CRITICAL: Create log AND config directories BEFORE writing the plist.
	// launchctl pre-validates StandardOutPath/StandardErrorPath directories
	// and returns an IO error if they do not exist.
	logDir := filepath.Join(home, "Library", "Caches", "mcp-server-magictools")
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return fmt.Errorf("failed to create cache/log directory %s: %w", logDir, err)
	}

	configDir := filepath.Join(home, "Library", "Application Support", "mcp-server-magictools")
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", configDir, err)
	}

	if isRoot {
		chownForUser(logDir, uidStr)
		chownForUser(configDir, uidStr)
	}

	desired, err := renderLaunchdDefinition(defaultLaunchdDefinition(binPath, home, productionServiceLabel))
	if err != nil {
		return err
	}
	previous, readErr := os.ReadFile(plistPath) // #nosec G304 -- fixed per-user LaunchAgent definition path
	existed := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("read previous plist: %w", readErr)
	}
	if err := writeFileSynced(plistPath, []byte(desired)); err != nil {
		return fmt.Errorf("failed to atomically write plist: %w", err)
	}

	if isRoot {
		chownForUser(plistPath, uidStr)
	}

	ctx, cancel := serviceActionContext(context.Background())
	defer cancel()
	life, err := newServiceLifecycle(binPath)
	if err != nil {
		return err
	}
	wasRunning, err := life.Running(ctx, cmdName)
	if err != nil {
		return err
	}
	if err := life.Restart(ctx); err != nil {
		var recovery error
		if stopErr := life.Stop(ctx, cmdName); stopErr != nil {
			recovery = errors.Join(recovery, stopErr)
		}
		if existed {
			if restoreErr := writeFileSynced(plistPath, previous); restoreErr != nil {
				recovery = errors.Join(recovery, restoreErr)
			}
		} else if removeErr := os.Remove(plistPath); removeErr != nil && !os.IsNotExist(removeErr) {
			recovery = errors.Join(recovery, removeErr)
		}
		if wasRunning {
			startErr := life.Start(ctx, cmdName)
			if startErr == nil {
				startErr = life.WaitHealthy(ctx, cmdName)
			}
			if startErr != nil {
				recovery = errors.Join(recovery, startErr)
			}
		}
		return errors.Join(fmt.Errorf("launchd install failed: %w", err), recovery)
	}

	domainTarget := fmt.Sprintf("gui/%s/%s", uidStr, productionServiceLabel)

	fmt.Printf("✓ LaunchAgent installed and bootstrapped\n")
	fmt.Printf("  Plist: %s\n", plistPath)
	fmt.Printf("  Logs:  %s/magictools_debug.log\n", logDir)
	fmt.Printf("  Check: launchctl print %s\n", domainTarget)
	fmt.Printf("\n  ⚠ IMPORTANT DIAGNOSTICS:\n")
	fmt.Printf("  1. If the service hangs, check System Settings > General > Login Items and ensure MagicTools is enabled.\n")
	fmt.Printf("  2. If the agent does not start due to Apple Silicon restrictions, ad-hoc sign the binary:\n")
	fmt.Printf("     codesign -s - %s\n", binPath)
	return nil
}

func uninstallLaunchd() error {
	ctx, cancel := serviceActionContext(context.Background())
	defer cancel()
	return withServiceLock(ctx, productionServiceLabel, func(ctx context.Context) error {
		return uninstallLaunchdLocked(ctx)
	})
}

// launchdPlistPathFn is the uninstall path seam. Tests point it at a scratch
// plist so an absent or running uninstall never reads the operator definition.
var launchdPlistPathFn = launchdPlistPath

func uninstallLaunchdLocked(ctx context.Context) error {
	plistPath, err := launchdPlistPathFn()
	if err != nil {
		return err
	}
	if _, err := os.Stat(plistPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := runSharedServiceActionLocked(ctx, serviceActionStop); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove plist: %w", err)
	}
	cleanupServiceArtifactsFn()
	fmt.Println("✓ LaunchAgent removed")
	return nil
}

// cleanupServiceArtifactsFn is the uninstall cleanup seam. Tests replace it
// so a scratch uninstall cannot delete the operator service.state file.
var cleanupServiceArtifactsFn = cleanupServiceArtifacts

// -------------------------------------------------------------------------
// Windows: Service Control Manager (SCM) service — see service_windows.go
// -------------------------------------------------------------------------

func installWindows(binPath string) error {
	// B2: env baked into the per-service registry key so the SCM-launched
	// process boots in service mode with the right endpoints (no .cmd wrapper).
	env := []string{
		"MCP_SERVICE_MODE=true",
		"MCP_ENDPOINT_IDE_PORT=" + config.ResolveBindAddr(""),
	}
	if recallURL := resolveRecallURL(); recallURL != "" {
		env = append(env, "MCP_REC_URL="+recallURL)
	}
	if socraticURL := resolveSocraticURL(); socraticURL != "" {
		env = append(env, "MCP_SOC_URL="+socraticURL)
	}

	binPathWin := strings.ReplaceAll(binPath, "/", "\\")
	return installWindowsService(binPathWin, env)
}

func uninstallWindows() error {
	// B2: stop + delete the SCM service (idempotent if absent).
	//nolint:staticcheck // SA4023: always true on !windows, where the stub always errors; the windows build returns nil
	if err := uninstallWindowsService(); err != nil {
		return err
	}

	ctx, cancel := serviceActionContext(context.Background())
	defer cancel()
	if err := runSharedServiceAction(ctx, serviceActionStop); err != nil {
		return err
	}

	// Legacy cleanup: remove the old Task Scheduler .cmd wrapper from versions
	// that predate the SCM service, if present.
	removeFileOrWarn(filepath.Join(config.DefaultConfigDir(), "magictools-service.cmd"))

	cleanupServiceArtifacts()

	fmt.Println("✓ Windows service removed")
	return nil
}

// isProcessAlive checks if a PID is still running.
// BUG-2: Returns false on error instead of assuming alive, which would
// cause 'service status' to falsely report a dead service as running.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	exists, err := process.PidExists(safeInt32FromInt(pid))
	if err != nil {
		return false
	}
	return exists
}

// resolveRecallURL returns the MCP_REC_URL for recall connectivity.
// Reads from the environment at install time so the value is baked into
// the service unit file.
func resolveRecallURL() string {
	return os.Getenv("MCP_REC_URL")
}

// resolveSocraticURL returns the MCP_SOC_URL for socratic thinker connectivity.
func resolveSocraticURL() string {
	return os.Getenv("MCP_SOC_URL")
}

// cleanupServiceArtifacts removes ONLY service-specific state files.
// BUG-10: IPC socket and auth token belong to the SERVER lifecycle,
// not the service wrapper. Removing them would break stdio-mode operation.
func cleanupServiceArtifacts() {
	statePath := filepath.Join(config.DefaultConfigDir(), "service.state")
	if err := os.Remove(statePath); err == nil {
		fmt.Printf("  Cleaned up service state: %s\n", statePath)
	}
}

// TEL-2: auditServiceEvent writes a timestamped audit record for service lifecycle events.
func auditServiceEvent(action, binPath string, err error) {
	auditDir := config.DefaultConfigDir()
	mkdirAllOrWarn(auditDir, 0o700)
	auditPath := filepath.Join(auditDir, "audit.log")

	status := "OK"
	if err != nil {
		status = "FAILED: " + err.Error()
	}

	entry := fmt.Sprintf("%s  action=%s  binary=%s  platform=%s  version=%s  status=%s\n",
		time.Now().UTC().Format(time.RFC3339),
		action, binPath, runtime.GOOS, Version, status)

	f, fErr := openConfigFile(auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if fErr != nil {
		return
	}
	writeStringOrWarn(f, entry)
	closeFileOrWarn(f, "audit")
}

// -------------------------------------------------------------------------
// service start (MISS-4/REV-4)
// -------------------------------------------------------------------------

var serviceStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the magictools background service",
	RunE: func(cmd *cobra.Command, args []string) error {
		switch runtime.GOOS {
		case goOSLinux:
			out, err := timedExec("systemctl", systemctlUserFlag, "start", "mcp-server-magictools.service")
			if err != nil {
				return fmt.Errorf("systemctl start failed: %s: %w", string(out), err)
			}
		case goOSDarwin:
			ctx, cancel := serviceActionContext(cmd.Context())
			defer cancel()
			if err := runSharedServiceAction(ctx, serviceActionStart); err != nil {
				return err
			}
		case goOSWindows:
			//nolint:staticcheck // SA4023: always true on !windows, where the stub always errors; the windows build returns nil
			if err := startWindowsService(); err != nil {
				return fmt.Errorf("failed to start windows service: %w", err)
			}
		default:
			return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
		}
		fmt.Println("✓ Service started")
		return nil
	},
}

// -------------------------------------------------------------------------
// service stop (MISS-4/REV-4)
// -------------------------------------------------------------------------

var serviceStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the magictools background service",
	RunE: func(cmd *cobra.Command, args []string) error {
		switch runtime.GOOS {
		case goOSLinux:
			out, err := timedExec("systemctl", systemctlUserFlag, "stop", "mcp-server-magictools.service")
			if err != nil {
				return fmt.Errorf("systemctl stop failed: %s: %w", string(out), err)
			}
		case goOSDarwin:
			ctx, cancel := serviceActionContext(cmd.Context())
			defer cancel()
			if err := runSharedServiceAction(ctx, serviceActionStop); err != nil {
				return err
			}
		case goOSWindows:
			//nolint:staticcheck // SA4023: always true on !windows, where the stub always errors; the windows build returns nil
			if err := stopWindowsService(); err != nil {
				return fmt.Errorf("failed to stop windows service: %w", err)
			}
		default:
			return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
		}
		fmt.Println("✓ Service stopped")
		return nil
	},
}

// -------------------------------------------------------------------------
// service logs (MISS-3)
// -------------------------------------------------------------------------

var serviceLogsFollow bool
var serviceLogsLines int

var serviceLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show magictools service logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		switch runtime.GOOS {
		case goOSLinux:
			// Use journalctl for systemd
			jArgs := []string{systemctlUserFlag, "-u", "mcp-server-magictools", "-n", strconv.Itoa(serviceLogsLines), "--no-pager"}
			if serviceLogsFollow {
				jArgs = append(jArgs, "-f")
			}
			c := exec.Command("journalctl", jArgs...) //nolint:gosec // fixed journalctl argv for local service logs
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		case goOSDarwin:
			home := homeDirOrDot()
			logFile := filepath.Join(home, "Library", "Caches", "mcp-server-magictools", "magictools_service.log")
			if serviceLogsFollow {
				c := exec.Command("tail", "-f", "-n", strconv.Itoa(serviceLogsLines), logFile) //nolint:gosec // log path from controlled config directory
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				return c.Run()
			}
			c := exec.Command("tail", "-n", strconv.Itoa(serviceLogsLines), logFile) //nolint:gosec // log path from controlled config directory
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		case goOSWindows:
			// B2: the SCM service logs via the app's own hardened log file (there
			// is no .cmd stdout redirect any more).
			logFile := config.DefaultLogPath()
			data, err := readConfigFile(logFile)
			if err != nil {
				return fmt.Errorf("failed to read log file: %w", err)
			}
			lines := strings.Split(string(data), "\n")
			start := max(len(lines)-serviceLogsLines, 0)
			fmt.Print(strings.Join(lines[start:], "\n"))
			return nil
		default:
			return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
		}
	},
}

// -------------------------------------------------------------------------
// service doctor (MISS-12/HARD-22)
// -------------------------------------------------------------------------

var serviceDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run diagnostic checks on the installed service",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Running service diagnostics...")
		issues := 0

		// 1. Check service state file
		statePath := filepath.Join(config.DefaultConfigDir(), "service.state")
		data, err := readConfigFile(statePath)
		if err != nil {
			fmt.Println("  ✗ No service state file found")
			issues++
		} else {
			var state cliServiceState
			if err := json.Unmarshal(data, &state); err != nil {
				fmt.Printf("  ✗ State file corrupt: %v\n", err)
				issues++
			} else {
				fmt.Printf("  ✓ State file: PID=%d, Addr=%s\n", state.PID, state.Addr)
				if _, err := readExpectedServiceState(processIdentity{}); err == nil {
					fmt.Printf("  ✓ Process %d identity is verified\n", state.PID)
				} else {
					fmt.Printf("  ✗ Process identity is not verified: %v\n", err)
					issues++
				}
				if state.BinaryPath != "" {
					if _, err := os.Stat(state.BinaryPath); err != nil {
						fmt.Printf("  ✗ Binary not found: %s\n", state.BinaryPath)
						issues++
					} else {
						fmt.Printf("  ✓ Binary exists: %s\n", state.BinaryPath)
					}
				}
			}
		}

		// 2. Platform-specific checks
		switch runtime.GOOS {
		case goOSLinux:
			home := homeDirOrDot()
			unitPath := filepath.Join(home, ".config", "systemd", "user", "mcp-server-magictools.service")
			if _, err := os.Stat(unitPath); err != nil {
				fmt.Printf("  ✗ systemd unit not found: %s\n", unitPath)
				issues++
			} else {
				fmt.Printf("  ✓ systemd unit exists: %s\n", unitPath)
			}
			// MISS-1/REV-5: Linger check with compliance warning
			out, err := timedExec("loginctl", "show-user", os.Getenv("USER"), "-p", "Linger")
			if err == nil {
				if strings.Contains(string(out), "Linger=yes") {
					fmt.Println("  ✓ Linger is enabled")
				} else {
					fmt.Println("  ⚠ Linger is NOT enabled — service will stop on logout")
					fmt.Println("    WARNING: Enabling linger allows your processes to persist after logout.")
					fmt.Println("    Check with your system administrator if this is permitted.")
					fmt.Println("    Fix: loginctl enable-linger $USER")
					issues++
				}
			}
			// HARD-19: DBUS check
			if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
				fmt.Println("  ⚠ DBUS_SESSION_BUS_ADDRESS is not set — systemctl --user may fail")
				issues++
			} else {
				fmt.Println("  ✓ DBUS_SESSION_BUS_ADDRESS is set")
			}
		case goOSDarwin:
			home := homeDirOrDot()
			plistPath := filepath.Join(home, "Library", "LaunchAgents", productionServiceLabel+".plist")
			if _, err := os.Stat(plistPath); err != nil {
				fmt.Printf("  ✗ LaunchAgent plist not found: %s\n", plistPath)
				issues++
			} else {
				fmt.Printf("  ✓ LaunchAgent plist exists: %s\n", plistPath)
				ctx, cancel := serviceActionContext(cmd.Context())
				life, err := newServiceLifecycle("")
				if err != nil {
					issues++
					fmt.Printf("  ✗ Lifecycle construction failed: %v\n", err)
				} else {
					running, runErr := life.Running(ctx, cmdName)
					cancel()
					if runErr != nil {
						issues++
						fmt.Printf("  ✗ launchd state unknown: %v\n", runErr)
					} else {
						fmt.Printf("  launchd running: %t\n", running)
					}
				}
			}
		case goOSWindows:
			//nolint:staticcheck // SA4023: always true on !windows, where the stub always errors; the windows build returns nil
			if installed, err := windowsServiceInstalled(); err != nil {
				fmt.Printf("  ✗ Could not query service manager: %v\n", err)
				issues++
			} else if !installed {
				fmt.Println("  ✗ Windows service not installed")
				issues++
			} else {
				fmt.Println("  ✓ Windows service is installed")
			}
		}

		if issues == 0 {
			fmt.Println("\n✓ All checks passed")
		} else {
			fmt.Printf("\n⚠ %d issue(s) found\n", issues)
			return fmt.Errorf("service doctor found %d issue(s)", issues)
		}
		return nil
	},
}

// -------------------------------------------------------------------------
// service reinstall (HARD-6)
// -------------------------------------------------------------------------

var serviceReinstallCmd = &cobra.Command{
	Use:   "reinstall",
	Short: "Atomically stop, remove, regenerate, and start the service",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Reinstalling magictools service...")

		// 1. Uninstall (best-effort)
		if err := runServiceUninstall(cmd, args); err != nil {
			slog.Debug("service reinstall: uninstall step failed", "error", err)
		}

		// BUG-5: Save/restore global to prevent latent mutation
		origForce := forceServiceInstall
		forceServiceInstall = true
		defer func() { forceServiceInstall = origForce }()

		if err := runServiceInstall(cmd, args); err != nil {
			return fmt.Errorf("reinstall failed: %w", err)
		}

		fmt.Println("✓ Service reinstalled successfully")
		auditServiceEvent("reinstall", "", nil)
		return nil
	},
}

// -------------------------------------------------------------------------
// service restart (A7)
// -------------------------------------------------------------------------

var serviceRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the magictools background service (stop then start)",
	Long: "Restart performs a true stop then start of the already-installed " +
		"service. Unlike 'reinstall' it does not remove or regenerate the unit/" +
		"plist/SCM registration — use it to bounce the process after a config or " +
		"binary change without re-running install.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS == goOSDarwin {
			ctx, cancel := serviceActionContext(cmd.Context())
			defer cancel()
			if err := runSharedServiceAction(ctx, "restart"); err != nil {
				return err
			}
		} else {
			if err := serviceStopCmd.RunE(cmd, args); err != nil {
				fmt.Printf("⚠ stop reported: %v (continuing to start)\n", err)
			}
			if err := serviceStartCmd.RunE(cmd, args); err != nil {
				return fmt.Errorf("restart failed during start: %w", err)
			}
		}
		auditServiceEvent("restart", "", nil)
		return nil
	},
}

func init() {
	serviceInstallCmd.Flags().BoolVarP(&forceServiceInstall, "force", "f", false, "Overwrite existing service files for upgrades")
	serviceInstallCmd.Flags().StringVar(&serviceBinPath, "bin-path", "", "Target binary path for the service (default: OS user bin dir)")
	serviceUninstallCmd.Flags().StringVar(&serviceBinPath, "bin-path", "", "Binary path to record in the audit log only; uninstall targets are derived from fixed locations")
	serviceStatusCmd.Flags().BoolVar(&jsonStatusOutput, "json", false, "Output status as JSON for machine consumption")
	serviceLogsCmd.Flags().BoolVarP(&serviceLogsFollow, "follow", "f", false, "Follow log output in real-time")
	serviceLogsCmd.Flags().IntVarP(&serviceLogsLines, "lines", "n", 50, "Number of log lines to show")
	serviceCmd.AddCommand(serviceInstallCmd)
	serviceCmd.AddCommand(serviceUninstallCmd)
	serviceCmd.AddCommand(serviceStatusCmd)
	serviceCmd.AddCommand(serviceReinstallCmd)
	serviceCmd.AddCommand(serviceStartCmd)
	serviceCmd.AddCommand(serviceStopCmd)
	serviceCmd.AddCommand(serviceRestartCmd)
	serviceCmd.AddCommand(serviceLogsCmd)
	serviceCmd.AddCommand(serviceDoctorCmd)
	// Hidden operation the updater runs on the NEWLY installed binary; it
	// rewrites only an existing definition and never installs one.
	serviceCmd.AddCommand(newServiceRefreshCmd())

	// Append the live list of subcommands to the top-level Short so the root
	// help stays in sync with the registered children (no hand-maintained list).
	names := make([]string, 0, len(serviceCmd.Commands()))
	for _, c := range serviceCmd.Commands() {
		names = append(names, c.Name())
	}
	serviceCmd.Short = fmt.Sprintf("%s (%s)", serviceCmd.Short, strings.Join(names, ", "))

	rootCmd.AddCommand(serviceCmd)
	rootCmd.AddCommand(newUpdateCmd())
}
