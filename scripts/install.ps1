# DevContract installer for Windows (PowerShell)
# Usage: irm https://devcontract.dev/install.ps1 | iex

$ErrorActionPreference = 'Stop'

$Repo = if ($env:DEVCONTRACT_INSTALL_REPO) { $env:DEVCONTRACT_INSTALL_REPO } else { "dantwoashim/devcontract" }
$InstallDir = if ($env:DEVCONTRACT_INSTALL_DIR) { $env:DEVCONTRACT_INSTALL_DIR } else { "$env:LOCALAPPDATA\DevContract\bin" }
$Version = if ($env:DEVCONTRACT_VERSION) { $env:DEVCONTRACT_VERSION } else { "latest" }

if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") {
    $Arch = "arm64"
} elseif ([Environment]::Is64BitOperatingSystem) {
    $Arch = "amd64"
} else {
    throw "Unsupported architecture: only amd64 and arm64 Windows builds are published."
}

Write-Host "Installing DevContract for windows/$Arch"

try {
    if ($Version -eq "latest") {
        $Release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
        $Version = $Release.tag_name -replace '^v', ''
    } else {
        $Version = $Version -replace '^v', ''
    }
} catch {
    throw "Failed to determine the release version: $($_.Exception.Message)"
}

Write-Host "Version: v$Version"

$Filename = "devcontract_${Version}_windows_${Arch}.zip"
$ArchiveUrl = "https://github.com/$Repo/releases/download/v$Version/$Filename"
$ChecksumsUrl = "https://github.com/$Repo/releases/download/v$Version/checksums.txt"
$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("devcontract-install-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $TempDir -Force | Out-Null

try {
    $ArchivePath = Join-Path $TempDir $Filename
    $ChecksumsPath = Join-Path $TempDir "checksums.txt"

    Write-Host "Downloading $Filename"
    Invoke-WebRequest -Uri $ArchiveUrl -OutFile $ArchivePath
    Invoke-WebRequest -Uri $ChecksumsUrl -OutFile $ChecksumsPath

    $Expected = Select-String -Path $ChecksumsPath -Pattern ([regex]::Escape($Filename) + '$') |
        ForEach-Object { ($_ -split '\s+')[0] } |
        Select-Object -First 1
    if (-not $Expected) {
        throw "Checksum for $Filename not found in checksums.txt"
    }

    $Actual = (Get-FileHash -Path $ArchivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($Actual -ne $Expected.ToLowerInvariant()) {
        throw "Checksum verification failed for $Filename"
    }

    Write-Host "Checksum verified"
    Expand-Archive -Path $ArchivePath -DestinationPath $TempDir -Force

    if (!(Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    Move-Item (Join-Path $TempDir "devcontract.exe") (Join-Path $InstallDir "devcontract.exe") -Force

    $UserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    if ($UserPath -notlike "*$InstallDir*") {
        $NewPath = if ([string]::IsNullOrWhiteSpace($UserPath)) { $InstallDir } else { "$UserPath;$InstallDir" }
        [Environment]::SetEnvironmentVariable("PATH", $NewPath, "User")
        Write-Host "Added $InstallDir to PATH"
    }

    Write-Host "Installed devcontract v$Version to $InstallDir\devcontract.exe"
    Write-Host ""
    Write-Host "Get started:"
    Write-Host "  devcontract init"
} finally {
    if (Test-Path $TempDir) {
        Remove-Item $TempDir -Recurse -Force
    }
}
