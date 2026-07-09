# install.ps1 behavior tests — checksum policy (no network calls)
$ErrorActionPreference = "Stop"

$script_dir = Split-Path $MyInvocation.MyCommand.Path -Parent
$install_ps1 = Join-Path $script_dir "..\install.ps1" -Resolve

$tmpdir = Join-Path ([System.IO.Path]::GetTempPath()) "dolly-ps-test-$([System.Guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Force $tmpdir | Out-Null
$original_user_path = [Environment]::GetEnvironmentVariable("Path", "User")

# Create a fake dolly.exe binary zip
$fake_exe = Join-Path $tmpdir "dolly.exe"
'echo dolly test binary' | Out-File -Encoding ascii $fake_exe
$asset_zip = Join-Path $tmpdir "dolly_windows_x86_64.zip"
Compress-Archive $fake_exe $asset_zip

# mock_no_checksums: only asset, no checksums.txt
$mock_no = Join-Path $tmpdir "mock_no_checksums"
New-Item -ItemType Directory -Force $mock_no | Out-Null
Copy-Item $asset_zip $mock_no

# mock_with_checksums: asset + checksums.txt
$mock_with = Join-Path $tmpdir "mock_with_checksums"
New-Item -ItemType Directory -Force $mock_with | Out-Null
Copy-Item $asset_zip $mock_with
$hash = (Get-FileHash $asset_zip -Algorithm SHA256).Hash.ToLower()
"$hash  dolly_windows_x86_64.zip" | Out-File -Encoding ascii (Join-Path $mock_with "checksums.txt")

# --- helpers ---

function Run-Install {
    param($MockDir, [hashtable]$ExtraEnv = @{})
    $install_target = Join-Path $tmpdir "install_dir"
    New-Item -ItemType Directory -Force $install_target | Out-Null

    $env_vars = @{
        DOLLY_REPO              = "test/test"
        DOLLY_INSTALL_DIR       = $install_target
        DOLLY_MOCK_DOWNLOAD_DIR = $MockDir
    }
    foreach ($k in $ExtraEnv.Keys) { $env_vars[$k] = $ExtraEnv[$k] }

    try {
        & $install_ps1 6>&1 | Out-Null
        return $LASTEXITCODE
    } catch {
        return 1
    }
}

# --- tests ---

Write-Host ""

# Test 1: latest fails when checksums are unavailable
$Env:DOLLY_REPO              = "test/test"
$Env:DOLLY_INSTALL_DIR       = Join-Path $tmpdir "install_dir_1"
$Env:DOLLY_MOCK_DOWNLOAD_DIR = $mock_no
$Env:DOLLY_VERSION           = "latest"
Remove-Item Env:DOLLY_ALLOW_UNVERIFIED -ErrorAction SilentlyContinue
try { & $install_ps1 2>&1 | Out-Null; $rc = 0 } catch { Write-Host $_; $rc = 1 }
if ($rc -eq 0) {
    Write-Host "FAIL latest: expected failure when checksums.txt is missing, but install succeeded" -ForegroundColor Red
    exit 1
}
Write-Host "PASS latest: fails when checksums.txt is unavailable" -ForegroundColor Green

# Test 2: latest succeeds with DOLLY_ALLOW_UNVERIFIED=1
$Env:DOLLY_REPO              = "test/test"
$Env:DOLLY_INSTALL_DIR       = Join-Path $tmpdir "install_dir_2"
$Env:DOLLY_MOCK_DOWNLOAD_DIR = $mock_no
$Env:DOLLY_VERSION           = "latest"
$Env:DOLLY_ALLOW_UNVERIFIED  = "1"
try { & $install_ps1 2>&1 | Out-Null; $rc = 0 } catch { Write-Host $_; $rc = 1 }
if ($rc -ne 0) {
    Write-Host "FAIL latest: expected success with DOLLY_ALLOW_UNVERIFIED=1, but install failed" -ForegroundColor Red
    [Environment]::SetEnvironmentVariable("Path", $original_user_path, "User")
    exit 1
}
if (-not (Test-Path (Join-Path $Env:DOLLY_INSTALL_DIR "dolly.exe"))) {
    Write-Host "FAIL latest: expected dolly.exe in install dir" -ForegroundColor Red
    [Environment]::SetEnvironmentVariable("Path", $original_user_path, "User")
    exit 1
}
if (($Env:Path -split [System.IO.Path]::PathSeparator) -notcontains $Env:DOLLY_INSTALL_DIR) {
    Write-Host "FAIL latest: expected install dir in process PATH" -ForegroundColor Red
    [Environment]::SetEnvironmentVariable("Path", $original_user_path, "User")
    exit 1
}
$user_path_after_install = [Environment]::GetEnvironmentVariable("Path", "User")
if (($user_path_after_install -split [System.IO.Path]::PathSeparator) -notcontains $Env:DOLLY_INSTALL_DIR) {
    Write-Host "FAIL latest: expected install dir in user PATH" -ForegroundColor Red
    [Environment]::SetEnvironmentVariable("Path", $original_user_path, "User")
    exit 1
}
Write-Host "PASS latest: succeeds with DOLLY_ALLOW_UNVERIFIED=1 (checksums skip)" -ForegroundColor Green

# mock_corrupt: asset + checksums.txt with WRONG hash
$mock_corrupt = Join-Path $tmpdir "mock_corrupt"
New-Item -ItemType Directory -Force $mock_corrupt | Out-Null
Copy-Item $asset_zip $mock_corrupt
"0000000000000000000000000000000000000000000000000000000000000000  dolly_windows_x86_64.zip" | Out-File -Encoding ascii (Join-Path $mock_corrupt "checksums.txt")

# Test 3: checksum mismatch is fatal even with DOLLY_ALLOW_UNVERIFIED=1
$Env:DOLLY_REPO              = "test/test"
$Env:DOLLY_INSTALL_DIR       = Join-Path $tmpdir "install_dir_3"
$Env:DOLLY_MOCK_DOWNLOAD_DIR = $mock_corrupt
$Env:DOLLY_VERSION           = "latest"
$Env:DOLLY_ALLOW_UNVERIFIED  = "1"
try { & $install_ps1 2>&1 | Out-Null; $rc = 0 } catch { Write-Host $_; $rc = 1 }
if ($rc -eq 0) {
    Write-Host "FAIL checksum-mismatch: expected failure on corrupt checksum even with DOLLY_ALLOW_UNVERIFIED=1, but install succeeded" -ForegroundColor Red
    [Environment]::SetEnvironmentVariable("Path", $original_user_path, "User")
    exit 1
}
Write-Host "PASS checksum-mismatch: fails with DOLLY_ALLOW_UNVERIFIED=1 when checksums.txt is corrupt" -ForegroundColor Green

Write-Host ""
Write-Host "All install.ps1 behavior tests passed." -ForegroundColor Green
[Environment]::SetEnvironmentVariable("Path", $original_user_path, "User")
