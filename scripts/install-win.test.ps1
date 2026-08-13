param(
    [string]$Version = $env:TEST_VERSION
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$ScriptDir = $PSScriptRoot
$TestDir = Join-Path ([IO.Path]::GetTempPath()) "autofeat-test-$([guid]::NewGuid())"
$InstallDir = Join-Path $TestDir "bin"
$ProfilePath = Join-Path $TestDir "PowerShell\profile.ps1"
$OriginalUserPath = [Environment]::GetEnvironmentVariable("Path", "User")
$OriginalProcessPath = $env:Path

try {
    $Arguments = @{
        InstallDir = $InstallDir
        ProfilePath = $ProfilePath
    }
    if ($Version) {
        $Arguments.Version = $Version
    }
    & (Join-Path $ScriptDir "install-win.ps1") @Arguments
    & (Join-Path $ScriptDir "install-win.ps1") @Arguments

    $Binary = Join-Path $InstallDir "autofeat.exe"
    if (-not (Test-Path -LiteralPath $Binary -PathType Leaf)) {
        throw "Expected executable at $Binary."
    }

    $VersionOutput = (& $Binary version) -join "`n"
    if ($VersionOutput -notmatch '(?m)^autofeat [0-9]+\.[0-9]+\.[0-9]+') {
        throw "Installed binary did not report a release version."
    }
    if ($VersionOutput -notmatch '(?m)^commit: [0-9a-f]{40}$') {
        throw "Installed binary did not report a full commit hash."
    }
    if ($VersionOutput -notmatch '(?m)^built: [0-9]{4}-[0-9]{2}-[0-9]{2}T') {
        throw "Installed binary did not report a build timestamp."
    }
    if ($VersionOutput -notmatch '(?m)^go: go[0-9]+\.') {
        throw "Installed binary did not report a Go version."
    }

    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (($UserPath -split ";") -notcontains $InstallDir) {
        throw "Installer did not add $InstallDir to the user PATH."
    }

    $CompletionSource = '(& autofeat completion powershell) -join "`n" | Invoke-Expression'
    $ProfileLines = @(Get-Content -LiteralPath $ProfilePath)
    $CompletionLines = @($ProfileLines | Where-Object { $_ -eq $CompletionSource })
    if ($CompletionLines.Count -ne 1) {
        throw "Installer did not add PowerShell completion exactly once to $ProfilePath."
    }

    Write-Host "PASS: install-win.ps1"
}
finally {
    [Environment]::SetEnvironmentVariable("Path", $OriginalUserPath, "User")
    $env:Path = $OriginalProcessPath
    Remove-Item $TestDir -Recurse -Force -ErrorAction SilentlyContinue
}