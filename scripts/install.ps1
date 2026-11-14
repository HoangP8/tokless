# tokless installer for Windows (PowerShell 5.1+).
#   irm https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.ps1 | iex

$ErrorActionPreference = "Stop"
$started = Get-Date

function Write-AvHint([string]$Path, [string]$Hash) {
    $detected = $false
    try {
        $detected = [bool](Get-MpThreatDetection -ErrorAction Stop | Where-Object { $_.InitialDetectionTime -gt $started -and ($_.Resources -join ";") -like "*tokless*" })
    } catch {}
    if ($detected) {
        Write-Host "Windows Defender reported a detection involving tokless during this install." -ForegroundColor Yellow
    } else {
        Write-Host "tokless may have been quarantined or removed by Windows Defender or other antivirus software." -ForegroundColor Yellow
    }
    Write-Host "File: $Path"
    if ($Hash) { Write-Host "SHA-256: $Hash" }
    Write-Host "Restore it from Windows Security protection history or rerun the installer."
    Write-Host "Do not disable real-time protection."
    Write-Host "Report a false positive: https://www.microsoft.com/en-us/wdsi/filesubmission"
}

function Get-InstalledVersion([string]$Path) {
    # Read the PE version resource; never execute the download.
    if (-not (Test-Path -LiteralPath $Path)) { return $null }
    try {
        $info = [System.Diagnostics.FileVersionInfo]::GetVersionInfo($Path)
        if ($info.FileVersion) { return $info.FileVersion }
        if ($info.ProductVersion) { return $info.ProductVersion }
        return ""
    } catch {
        return $null
    }
}

function Restore-Previous([string]$Dest, [string]$Old) {
    if (-not (Test-Path -LiteralPath $Old)) { return $false }
    Remove-Item -LiteralPath $Dest -Force -ErrorAction SilentlyContinue
    try {
        Move-Item -LiteralPath $Old -Destination $Dest -Force
        return $true
    } catch {
        return $false
    }
}

function Write-InstallMarker([string]$Version) {
    $dataRoot = if ($env:LOCALAPPDATA) { $env:LOCALAPPDATA } else { Join-Path $env:USERPROFILE "AppData\Local" }
    $dataDir = Join-Path $dataRoot "tokless"
    New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
    $installJson = @{
        method  = "install script"
        path    = $dest
        version = $Version
        at      = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    } | ConvertTo-Json -Compress
    [System.IO.File]::WriteAllText((Join-Path $dataDir "install.json"), $installJson)
}

$Owner = "HoangP8"
$Repo  = "tokless"

$asset = "tokless-windows-x64.exe"
$base  = "https://github.com/$Owner/$Repo/releases/latest/download"
$url   = "$base/$asset"
$sumsUrl = "$base/SHA256SUMS"
$selfUpdatePath = $env:TOKLESS_SELF_UPDATE_PATH
if ($selfUpdatePath) {
    $dest = $selfUpdatePath
    $destDir = Split-Path -Parent $dest
} else {
    $destDir = Join-Path $env:LOCALAPPDATA "Programs\tokless"
    $dest = Join-Path $destDir "tokless.exe"
}

New-Item -ItemType Directory -Force -Path $destDir | Out-Null
if ($PSVersionTable.PSVersion.Major -eq 5 -and $PSVersionTable.PSVersion.Minor -eq 1) {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
}

# Stable names keep update paths predictable for antivirus heuristics.
$tmp = "$dest.download"
$old = "$dest.old"
Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
Get-ChildItem -LiteralPath $destDir -Filter ((Split-Path -Leaf $dest) + ".old*") -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue
Get-ChildItem -LiteralPath $destDir -Filter ((Split-Path -Leaf $dest) + ".*.bak") -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue
if (Test-Path -LiteralPath $old) {
    # A previous process may still hold the old image.
    $old = "$dest.old.$PID"
}

try {
    Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing
} catch {
    Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
    throw "Download failed ($asset): $($_.Exception.Message)"
}
if (-not (Test-Path -LiteralPath $tmp)) {
    Write-Host "Download finished but $tmp is missing." -ForegroundColor Red
    Write-AvHint $tmp ""
    throw "Downloaded executable is missing"
}

$tmpHash = ""
try { $tmpHash = (Get-FileHash -LiteralPath $tmp -Algorithm SHA256).Hash } catch {}
$expected = ""
try {
    $resp = Invoke-WebRequest -Uri $sumsUrl -UseBasicParsing
    $sums = if ($resp.Content -is [byte[]]) { [System.Text.Encoding]::UTF8.GetString($resp.Content) } else { [string]$resp.Content }
    foreach ($line in ($sums -split "`n")) {
        $parts = $line.Trim() -split '\s+'
        if ($parts.Count -ge 2 -and $parts[1] -eq $asset) {
            $expected = $parts[0]
            break
        }
    }
} catch {}
if (-not $expected) {
    Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
    throw "Could not read the published SHA-256 for $asset"
}
if ($tmpHash -ne $expected) {
    Write-Host "Checksum mismatch for $asset." -ForegroundColor Red
    Write-AvHint $tmp $tmpHash
    Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
    throw "Checksum mismatch"
}

$moved = $false
try {
    if (Test-Path -LiteralPath $dest) {
        Move-Item -LiteralPath $dest -Destination $old -Force
        $moved = $true
    }
    Move-Item -LiteralPath $tmp -Destination $dest -Force
} catch {
    if ($moved -and -not (Test-Path -LiteralPath $dest)) {
        if (-not (Restore-Previous $dest $old)) {
            Write-Host "Previous tokless.exe is still at ${old}." -ForegroundColor Yellow
        }
    }
    Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
    throw "Replacement failed: $($_.Exception.Message)"
}

# Defender may quarantine the new binary right after it is written.
Start-Sleep -Seconds 2
$v = Get-InstalledVersion $dest
if ($null -eq $v) {
    Write-Host "Install failed: $dest is missing or could not be read." -ForegroundColor Red
    Write-AvHint $dest $tmpHash
    if ($moved) {
        if (Restore-Previous $dest $old) {
            Write-Host "Previous tokless.exe restored." -ForegroundColor Cyan
        } else {
            Write-Host "Could not restore previous tokless.exe from ${old}." -ForegroundColor Red
        }
    }
    throw "tokless install failed"
}
if (-not $v) { $v = "unknown" }
Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue

if ($selfUpdatePath) {
    Write-InstallMarker $v
    Write-Host "Update installed: $v" -ForegroundColor Green
    Write-Host "Run tokless again to use the new version." -ForegroundColor Cyan
    exit 0
}

$key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey("Environment", $true)
$userPath = ""
if ($null -ne $key.GetValue("Path")) {
    $userPath = $key.GetValue("Path", "", [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
}
$expanded = ($userPath -split ";") | ForEach-Object { [Environment]::ExpandEnvironmentVariables($_).TrimEnd("\") }
if ($expanded -notcontains $destDir.TrimEnd("\")) {
    $newPath = if ($userPath) { "$destDir;$userPath" } else { $destDir }
    $key.SetValue("Path", $newPath, [Microsoft.Win32.RegistryValueKind]::ExpandString)
    $env:Path = "$destDir;$env:Path"
}
$key.Close()

Write-Host "[OK] tokless $v ready -> $dest" -ForegroundColor Green

Write-InstallMarker $v

$hadInstallerRun = Test-Path Env:TOKLESS_INSTALLER_RUN
$prevInstallerRun = $env:TOKLESS_INSTALLER_RUN
if ([Environment]::UserInteractive -and -not $env:CI) {
    Write-Host ""
    try {
        $env:TOKLESS_INSTALLER_RUN = "1"
        & $dest
    } catch {
        Write-Host "Could not run ${dest}: $_" -ForegroundColor Red
    } finally {
        if ($hadInstallerRun) {
            $env:TOKLESS_INSTALLER_RUN = $prevInstallerRun
        } else {
            Remove-Item Env:TOKLESS_INSTALLER_RUN -ErrorAction SilentlyContinue
        }
    }
    if (-not (Test-Path -LiteralPath $dest)) {
        Write-Host "tokless.exe was removed after install (likely Windows Defender); hooks may be broken." -ForegroundColor Red
        Write-Host "Run this installer again, then run: tokless doctor" -ForegroundColor Yellow
        throw "tokless install failed"
    }
} else {
    Write-Host "Run: tokless" -ForegroundColor Cyan
}
