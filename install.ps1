# ponytail: PowerShell installer for Windows — mirrors install.sh, no package manager
param()

$ErrorActionPreference = "Stop"

$DOLLY_VERSION  = if ($env:DOLLY_VERSION)  { $env:DOLLY_VERSION }  else { "latest" }
$DOLLY_REPO     = if ($env:DOLLY_REPO)     { $env:DOLLY_REPO }     else { "VicenteOlmos/dolly" }
$DOLLY_INSTALL_DIR = if ($env:DOLLY_INSTALL_DIR) { $env:DOLLY_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\dolly\bin" }

function die { Write-Error "error: $args"; exit 1 }
function warn { Write-Warning $args }

function Path-ContainsDir {
    param($PathValue, $Dir)
    if (-not $PathValue) { return $false }
    $target = [System.IO.Path]::GetFullPath([Environment]::ExpandEnvironmentVariables($Dir)).TrimEnd('\', '/')
    foreach ($entry in ($PathValue -split [System.IO.Path]::PathSeparator)) {
        if (-not $entry) { continue }
        try {
            $entryFull = [System.IO.Path]::GetFullPath([Environment]::ExpandEnvironmentVariables($entry.Trim('"'))).TrimEnd('\', '/')
        } catch {
            continue
        }
        if ($entryFull -ieq $target) {
            return $true
        }
    }
    return $false
}

function Add-DirToPathValue {
    param($PathValue, $Dir)
    if (Path-ContainsDir $PathValue $Dir) { return $PathValue }
    if ($PathValue) { return "$PathValue$([System.IO.Path]::PathSeparator)$Dir" }
    return $Dir
}

if ($DOLLY_REPO -notmatch '^.+/[^/]+$') {
    die "DOLLY_REPO must use GitHub owner/repo format, got: $DOLLY_REPO"
}

$arch = switch -Wildcard ((Get-CimInstance Win32_Processor).Architecture) {
    9  { "x86_64" }  # x64
    12 { "arm64"  }  # ARM64
    default { die "unsupported architecture" }
}

if (Test-Path function:\Invoke-MockDownload) { Remove-Item function:\Invoke-MockDownload -ErrorAction SilentlyContinue }

# test hook: when DOLLY_MOCK_DOWNLOAD_DIR is set, copy assets from
# that directory instead of fetching from the network.
if ($env:DOLLY_MOCK_DOWNLOAD_DIR) {
    function Invoke-MockDownload {
        param($Url, $Output)
        $fname = ($Url -split '/')[-1]
        $src   = Join-Path $env:DOLLY_MOCK_DOWNLOAD_DIR $fname
        if (Test-Path $src) {
            Copy-Item $src $Output
        } else {
            return $false
        }
    }
}

function Download-File {
    param($Url, $Output)
    if ($env:DOLLY_MOCK_DOWNLOAD_DIR) {
        if (-not (Invoke-MockDownload $Url $Output)) {
            throw "mock download failed: $Url"
        }
        return
    }
    if (Get-Command curl.exe -ErrorAction SilentlyContinue) {
        & curl.exe -fsSL $Url -o $Output
    } elseif (Get-Command wget.exe -ErrorAction SilentlyContinue) {
        & wget.exe -q $Url -O $Output
    } else {
        die "curl.exe or wget.exe is required"
    }
}

$asset_name = "dolly_windows_${arch}.zip"
$tmpdir = Join-Path ([System.IO.Path]::GetTempPath()) "dolly-install-$([System.Guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Force $tmpdir | Out-Null

$archive   = Join-Path $tmpdir $asset_name
$checksums = Join-Path $tmpdir "checksums.txt"

if ($DOLLY_VERSION -eq "latest") {
    $base_url = "https://github.com/$DOLLY_REPO/releases/latest/download"
} else {
    $version = $DOLLY_VERSION -replace '^v', ''
    if (-not $version) { die "DOLLY_VERSION cannot be empty" }
    $base_url = "https://github.com/$DOLLY_REPO/releases/download/v$version"
}

Write-Host "Downloading $base_url/$asset_name"
Download-File "$base_url/$asset_name" $archive

# --- checksum verification ---
# download + parse first; mismatch is always fatal regardless of DOLLY_ALLOW_UNVERIFIED.
# DOLLY_ALLOW_UNVERIFIED only bypasses missing/unavailable checksums.

$checksum_ok = $false
$checksums_downloaded = $false
try {
    Download-File "$base_url/checksums.txt" $checksums
    $checksums_downloaded = $true
} catch {
    # fall through — handle missing checksums below
}

if ($checksums_downloaded) {
    $checksum_lines = Get-Content $checksums
    $match = $checksum_lines | Where-Object { $_ -match "\s+${asset_name}$" } | Select-Object -First 1
    if ($match) {
        $expected = ($match -split '\s+')[0].Trim()
        $actual   = (Get-FileHash $archive -Algorithm SHA256).Hash.ToLower()
        if ($expected -eq $actual) {
            $checksum_ok = $true
        } else {
            die "checksum mismatch for $asset_name"
        }
    } elseif ($DOLLY_VERSION -ne "latest") {
        die "checksum verification required for tagged release but $asset_name is not listed in checksums.txt"
    } elseif ($env:DOLLY_ALLOW_UNVERIFIED -eq "1") {
        warn "checksums.txt does not contain $asset_name; skipping checksum verification (DOLLY_ALLOW_UNVERIFIED=1)"
    } else {
        die "checksum verification required: $asset_name is not listed in checksums.txt (set DOLLY_ALLOW_UNVERIFIED=1 to skip)"
    }
} else {
    if ($DOLLY_VERSION -ne "latest") {
        die "checksum verification required for tagged release but checksums.txt could not be downloaded"
    } elseif ($env:DOLLY_ALLOW_UNVERIFIED -eq "1") {
        warn "checksums.txt was not found; checksum verification skipped (DOLLY_ALLOW_UNVERIFIED=1)"
    } else {
        die "checksum verification required: checksums.txt could not be downloaded (set DOLLY_ALLOW_UNVERIFIED=1 to skip)"
    }
}

# --- install ---

$extract_dir = Join-Path $tmpdir "extract"
Expand-Archive $archive $extract_dir

$binary = Get-ChildItem -Path $extract_dir -Recurse -File -Filter "dolly.exe" | Select-Object -First 1
if (-not $binary) { die "archive did not contain a dolly.exe binary" }
$binary_path = $binary.FullName

if (-not (Test-Path $DOLLY_INSTALL_DIR)) {
    New-Item -ItemType Directory -Force $DOLLY_INSTALL_DIR | Out-Null
}

$target = Join-Path $DOLLY_INSTALL_DIR "dolly.exe"
Copy-Item $binary_path $target -Force

Write-Host "Installed dolly to $target"

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (-not (Path-ContainsDir $userPath $DOLLY_INSTALL_DIR)) {
    [Environment]::SetEnvironmentVariable("Path", (Add-DirToPathValue $userPath $DOLLY_INSTALL_DIR), "User")
    Write-Host "Added $DOLLY_INSTALL_DIR to your user PATH. Open a new terminal if 'dolly' is not found."
}
if (-not (Path-ContainsDir $env:Path $DOLLY_INSTALL_DIR)) {
    $env:Path = Add-DirToPathValue $env:Path $DOLLY_INSTALL_DIR
}
