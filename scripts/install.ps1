#Requires -Version 5.1
<#
.SYNOPSIS
    Install mcp-server-magictools on Windows.

.DESCRIPTION
    Downloads the published windows/amd64 binary, verifies it against the
    release SHA256SUMS manifest, and places it under
    %LOCALAPPDATA%\Programs\magictools. It does not configure or start
    MagicTools and does not install a service.
#>
[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [string]$Version,
    [string]$InstallDir = $env:MCP_MAGICTOOLS_INSTALL_DIR,
    [string]$BaseUrl = 'https://github.com/maccavelli/mcp-server-magictools/releases'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$Product = 'mcp-server-magictools'

function Write-Log { param([string]$Message) Write-Host "install: $Message" }
function Write-Warn { param([string]$Message) Write-Warning "install: $Message" }

function Get-TargetArch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) { $arch = $env:PROCESSOR_ARCHITEW6432 }
    switch ($arch) {
        'AMD64' { return 'amd64' }
        'ARM64' {
            throw @'
windows/arm64 is not a published target. Download the amd64 binary manually
if running it under Windows on Arm emulation is acceptable.
'@
        }
        default { throw "unsupported processor architecture '$arch'; only amd64 is published" }
    }
}

function Get-UrlDir {
    if ($Version) { return "$BaseUrl/download/v$Version" }
    return "$BaseUrl/latest/download"
}

function Get-File {
    param([string]$Url, [string]$Destination)
    Write-Verbose "fetching $Url"
    $previous = $ProgressPreference
    $ProgressPreference = 'SilentlyContinue'
    try {
        Invoke-WebRequest -Uri $Url -OutFile $Destination -UseBasicParsing
    } finally {
        $ProgressPreference = $previous
    }
}

function Resolve-ProductVersion {
    param(
        [string]$Arch,
        [string]$BinaryPath,
        [string[]]$Sums
    )
    $prefix = "$Product-windows-$Arch-"
    $line = $Sums | Where-Object { $_ -match [regex]::Escape($prefix) } | Select-Object -First 1
    if (-not $line) {
        throw "no checksum entry for $prefix* in SHA256SUMS"
    }
    $fields = $line -split '\s+' | Where-Object { $_ }
    $want = $fields[0]
    $name = $fields[-1]
    $got = (Get-FileHash -Path $BinaryPath -Algorithm SHA256).Hash.ToLower()

    if ($want.ToLower() -ne $got) {
        throw @"
checksum mismatch for $Product
  expected $($want.ToLower())
  got      $got
Nothing was installed.
"@
    }

    $resolved = $name.Substring($prefix.Length)
    if ($resolved.EndsWith('.exe')) {
        $resolved = $resolved.Substring(0, $resolved.Length - 4)
    }
    return $resolved
}

function Add-ToPathNotice {
    param([string]$Dir)
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($userPath -and ($userPath -split ';' | Where-Object { $_.TrimEnd('\') -ieq $Dir.TrimEnd('\') })) {
        return
    }
    Write-Warn "$Dir is not on your PATH. To add it for this user:"
    Write-Host "  [Environment]::SetEnvironmentVariable('Path', `"`$env:Path;$Dir`", 'User')"
}

if ($Version) {
    $Version = $Version.TrimStart('v')
    if ($Version -notmatch '^\d+\.\d+\.\d+$') {
        throw "version must be X.Y.Z (got $Version)"
    }
}

$arch = Get-TargetArch
$urlDir = Get-UrlDir
if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\magictools'
}

Write-Log "source $urlDir"
Write-Log "target windows/$arch -> $InstallDir"
Write-Log 'action place binary only'

if ($WhatIfPreference) {
    Write-Log "would download $urlDir/$Product-windows-$arch.exe"
    Write-Log "would install $(Join-Path $InstallDir "$Product.exe")"
    Write-Log 'nothing was downloaded (-WhatIf)'
    return
}

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ('magictools-install-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
    $sumsPath = Join-Path $tmp 'SHA256SUMS'
    Get-File -Url "$urlDir/SHA256SUMS" -Destination $sumsPath
    $sums = Get-Content -Path $sumsPath

    $download = Join-Path $tmp "$Product.exe"
    Get-File -Url "$urlDir/$Product-windows-$arch.exe" -Destination $download
    $resolvedVersion = Resolve-ProductVersion -Arch $arch -BinaryPath $download -Sums $sums
    Write-Log "$Product verified, version $resolvedVersion"

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $target = Join-Path $InstallDir "$Product.exe"
    if ($PSCmdlet.ShouldProcess($target, 'install verified binary')) {
        if (Test-Path $target) {
            $backup = "$target.prev"
            Remove-Item -Path $backup -Force -ErrorAction SilentlyContinue
            Move-Item -Path $target -Destination $backup -Force
        }
        Move-Item -Path $download -Destination $target -Force
        Write-Log "installed $target"
    }

    Add-ToPathNotice -Dir $InstallDir
    Write-Log "$Product $resolvedVersion installed to $InstallDir"
} finally {
    Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
