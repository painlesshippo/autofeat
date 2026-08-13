param(
    [string]$Version = $env:VERSION,
    [string]$InstallDir = $(
        if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $HOME "bin" }
    ),
    [string]$BinaryPath = $env:BINARY_PATH
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$TemporaryDir = $null
try {
    if (-not $BinaryPath) {
        $RepositoryUrl = "https://github.com/painlesshippo/autofeat"
        if (-not $Version) {
            $Release = Invoke-RestMethod "https://api.github.com/repos/painlesshippo/autofeat/releases/latest"
            $Version = $Release.tag_name -replace '^v', ''
        }
        if ($Version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$') {
            throw "Invalid release version: $Version"
        }

        $Archive = "autofeat_${Version}_windows_amd64.zip"
        $ReleaseUrl = "$RepositoryUrl/releases/download/v${Version}"
        $TemporaryDir = Join-Path ([IO.Path]::GetTempPath()) "autofeat-$([guid]::NewGuid())"
        New-Item -ItemType Directory -Path $TemporaryDir | Out-Null

        $ArchivePath = Join-Path $TemporaryDir $Archive
        $ChecksumsPath = Join-Path $TemporaryDir "checksums.txt"
        Invoke-WebRequest "$ReleaseUrl/$Archive" -OutFile $ArchivePath
        Invoke-WebRequest "$ReleaseUrl/checksums.txt" -OutFile $ChecksumsPath

        $ChecksumLine = Get-Content $ChecksumsPath | Where-Object {
            $_ -match "\s+$([regex]::Escape($Archive))$"
        } | Select-Object -First 1
        if (-not $ChecksumLine) {
            throw "No checksum found for $Archive."
        }
        $ExpectedChecksum = ($ChecksumLine -split '\s+')[0]
        $ActualChecksum = (Get-FileHash $ArchivePath -Algorithm SHA256).Hash
        if ($ActualChecksum -ne $ExpectedChecksum) {
            throw "Checksum verification failed for $Archive."
        }

        $ExtractDir = Join-Path $TemporaryDir "extract"
        Expand-Archive -Path $ArchivePath -DestinationPath $ExtractDir
        $BinaryPath = Join-Path $ExtractDir "autofeat.exe"
    }
    elseif (-not (Test-Path -LiteralPath $BinaryPath -PathType Leaf)) {
        throw "Binary not found: $BinaryPath"
    }

    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Copy-Item $BinaryPath $InstallDir -Force
}
finally {
    if ($TemporaryDir) {
        Remove-Item $TemporaryDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (($UserPath -split ";") -notcontains $InstallDir) {
    $UpdatedPath = if ($UserPath) { "$UserPath;$InstallDir" } else { $InstallDir }
    [Environment]::SetEnvironmentVariable("Path", $UpdatedPath, "User")
    Write-Host "Added $InstallDir to the user PATH."
}
$env:Path = "$InstallDir;$env:Path"

Write-Host "Installed $(Join-Path $InstallDir 'autofeat.exe')"
& (Join-Path $InstallDir "autofeat.exe") version