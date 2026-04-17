# DevContract installer for Windows (PowerShell)
# Usage: irm https://devcontract.dev/install.ps1 | iex

$ErrorActionPreference = 'Stop'

$Repo = if ($env:DEVCONTRACT_INSTALL_REPO) { $env:DEVCONTRACT_INSTALL_REPO } else { "dantwoashim/DevContract" }
$Module = if ($env:DEVCONTRACT_INSTALL_MODULE) { $env:DEVCONTRACT_INSTALL_MODULE } else { "github.com/dantwoashim/devcontract" }
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

function Ensure-InstallDir {
    if (!(Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }
}

function Add-InstallDirToPath {
    $UserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    if ($UserPath -notlike "*$InstallDir*") {
        $NewPath = if ([string]::IsNullOrWhiteSpace($UserPath)) { $InstallDir } else { "$UserPath;$InstallDir" }
        [Environment]::SetEnvironmentVariable("PATH", $NewPath, "User")
        Write-Host "Added $InstallDir to PATH"
    }
}

function Install-FromSource {
    param(
        [Parameter(Mandatory = $true)]
        [string]$TempDir
    )

    $GoCommand = Get-Command go -ErrorAction SilentlyContinue
    if (-not $GoCommand) {
        throw "No published DevContract release exists yet, and Go is not installed. Install Go from https://go.dev/dl or publish a GitHub release first."
    }

    $Gobin = Join-Path $TempDir "gobin"
    New-Item -ItemType Directory -Path $Gobin -Force | Out-Null

    Write-Host "No published release found. Falling back to source install from $Module"

    $OldGobin = $env:GOBIN
    try {
        $env:GOBIN = $Gobin
        & $GoCommand.Source install "$Module@latest"
        if ($LASTEXITCODE -ne 0) {
            throw "go install failed with exit code $LASTEXITCODE"
        }
    } finally {
        $env:GOBIN = $OldGobin
    }

    $BuiltBinary = Join-Path $Gobin "devcontract.exe"
    if (!(Test-Path $BuiltBinary)) {
        throw "go install completed but devcontract.exe was not produced"
    }

    Ensure-InstallDir
    Move-Item $BuiltBinary (Join-Path $InstallDir "devcontract.exe") -Force
    Add-InstallDirToPath

    Write-Host "Installed devcontract from source to $InstallDir\devcontract.exe"
    Write-Host ""
    Write-Host "Get started:"
    Write-Host "  devcontract init"
}

$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("devcontract-install-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $TempDir -Force | Out-Null

try {
    $ResolvedVersion = $null
    $UseSourceFallback = $false

    if ($Version -eq "latest") {
        try {
            $Release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
            $ResolvedVersion = $Release.tag_name -replace '^v', ''
        } catch {
            $StatusCode = $null
            if ($_.Exception.Response -and $_.Exception.Response.StatusCode) {
                $StatusCode = [int]$_.Exception.Response.StatusCode
            }
            if ($StatusCode -eq 404) {
                $UseSourceFallback = $true
            } else {
                throw "Failed to determine the release version: $($_.Exception.Message)"
            }
        }
    } elseif ($Version -in @("source", "main")) {
        $UseSourceFallback = $true
    } else {
        $ResolvedVersion = $Version -replace '^v', ''
    }

    if ($UseSourceFallback) {
        Install-FromSource -TempDir $TempDir
        return
    }

    Write-Host "Version: v$ResolvedVersion"

    $Filename = "devcontract_${ResolvedVersion}_windows_${Arch}.zip"
    $ArchiveUrl = "https://github.com/$Repo/releases/download/v$ResolvedVersion/$Filename"
    $ChecksumsUrl = "https://github.com/$Repo/releases/download/v$ResolvedVersion/checksums.txt"

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

    Ensure-InstallDir
    Move-Item (Join-Path $TempDir "devcontract.exe") (Join-Path $InstallDir "devcontract.exe") -Force
    Add-InstallDirToPath

    Write-Host "Installed devcontract v$ResolvedVersion to $InstallDir\devcontract.exe"
    Write-Host ""
    Write-Host "Get started:"
    Write-Host "  devcontract init"
} finally {
    if (Test-Path $TempDir) {
        Remove-Item $TempDir -Recurse -Force
    }
}
