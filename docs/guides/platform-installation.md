# Platform installation

## Installation at a glance

| Platform | Published target | Default installer destination |
| :--- | :--- | :--- |
| Linux | amd64 | `~/.local/bin/mcp-server-magictools` |
| macOS | arm64 | `~/.local/bin/mcp-server-magictools` |
| Windows | amd64 | `%LOCALAPPDATA%\Programs\magictools\mcp-server-magictools.exe` |

Release builds use `CGO_ENABLED=0`. They do not need a Go installation.

## Recommended one-line installation

### Linux or macOS

```bash
curl -fsSL https://github.com/maccavelli/mcp-server-magictools/releases/latest/download/install.sh | sh
```

The POSIX installer requires `curl` or `wget`, a SHA-256 implementation, `awk`,
and `grep`. It verifies the release checksum before moving the binary. An
existing binary becomes `mcp-server-magictools.prev`.

### Windows PowerShell

```powershell
irm https://github.com/maccavelli/mcp-server-magictools/releases/latest/download/install.ps1 | iex
```

The PowerShell installer uses `Get-FileHash`, preserves an existing executable
as `.prev`, and reports when the destination is absent from the user `PATH`.

Neither installer initializes configuration or installs a service.

## Installer options

Download `install.sh` first when passing POSIX flags:

```bash
sh install.sh --version 1.0.3 --dir /custom/bin --dry-run --verbose
sh install.sh --dir /custom/bin --uninstall
```

Piped POSIX installation supports `MCP_MAGICTOOLS_VERSION` and
`MCP_MAGICTOOLS_INSTALL_DIR`.

PowerShell supports `-Version`, `-InstallDir`, `-Verbose`, and `-WhatIf`:

```powershell
./install.ps1 -Version 1.0.3 -WhatIf
```

## Linux walkthrough

Linux releases currently support amd64 only.

```bash
mcp-server-magictools --version
mcp-server-magictools init
```

The standard locations are:

- Config: `~/.config/mcp-server-magictools/`
- Cache and log: `~/.cache/mcp-server-magictools/`
- Data: `~/.config/mcp-server-magictools/db/`

The optional background service is a systemd user unit. It requires a working
`systemctl --user` session. Without user linger, it stops when the user logs
out.

## macOS walkthrough

The published macOS binary is Apple Silicon (`darwin/arm64`).

```bash
mcp-server-magictools --version
mcp-server-magictools init
```

The standard locations are:

- Config: `~/Library/Application Support/mcp-server-magictools/`
- Cache and log: `~/Library/Caches/mcp-server-magictools/`
- Data: `~/Library/Application Support/mcp-server-magictools/db/`

The optional service is the per-user LaunchAgent
`com.magictools.mcp-server-magictools`.

## Windows walkthrough

The published Windows binary is amd64. After installation, open a new shell if
you changed the user `PATH`.

```powershell
mcp-server-magictools.exe --version
mcp-server-magictools.exe init
```

The standard locations are derived from Go's platform APIs:

- Config and data: `%APPDATA%\mcp-server-magictools\`
- Cache and log: `%LOCALAPPDATA%\mcp-server-magictools\`

The optional service is the Windows Service Control Manager service
`MagicToolsService` and requires Administrator privileges.

## Manual verified download

Every release includes versioned binaries, stable unversioned aliases,
`SHA256SUMS`, and versioned checksum manifests. Download the binary and
`SHA256SUMS` from the same release, then verify before installation.

The stable artifact names are:

- `mcp-server-magictools-linux-amd64`
- `mcp-server-magictools-darwin-arm64`
- `mcp-server-magictools-windows-amd64.exe`

## Build current source

Building requires the Go version declared by `go.mod` (currently Go 1.26.5):

```bash
git clone https://github.com/maccavelli/mcp-server-magictools.git
cd mcp-server-magictools
make build
./dist/mcp-server-magictools --version
```

`make build-all` cross-compiles the three release targets. CI additionally runs
the complete suite natively on each target operating system.

## Upgrade or remove

Running an installer again upgrades the binary and keeps one `.prev` backup.
For an installed service, update the binary and then use:

```bash
mcp-server-magictools service reinstall
```

The POSIX installer supports `--uninstall`. On Windows, remove the executable
and `.prev` file manually after uninstalling any service. Removing the binary
does not remove configuration, data, logs, or service definitions.

## Troubleshooting

- Unsupported architecture errors mean no native release is published for that
  target. Build from source or use a supported machine.
- A checksum mismatch installs nothing.
- If the command is not found, add the installer destination to `PATH` or use
  its absolute path.
- macOS security controls may require explicitly allowing a downloaded binary.
- Service installation is a separate operation; see
  [Services and transports](services-and-transports.md).
