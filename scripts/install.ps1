# tokless installer for Windows (PowerShell 5.1+).
#   irm https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.ps1 | iex


$ErrorActionPreference = "Stop"
$started = Get-Date
$Owner = "HoangP8"
$Repo  = "tokless"

$asset = "tokless-windows-x64.exe"
$url   = "https://github.com/$Owner/$Repo/releases/latest/download/$asset"
$selfUpdatePath = $env:TOKLESS_SELF_UPDATE_PATH
if ($selfUpdatePath) {
    $dest = $selfUpdatePath
    $destDir = Split-Path -Parent $dest
} else {
    $destDir = Join-Path $env:LOCALAPPDATA "Programs\tokless"
    $dest = Join-Path $destDir "tokless.exe"
}

New-Item -ItemType Directory -Force -Path $destDir | Out-Null
$selfUpdate = $env:TOKLESS_SELF_UPDATE_PID
if ($PSVersionTable.PSVersion.Major -eq 5 -and $PSVersionTable.PSVersion.Minor -eq 1) {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
}
$tmp = Join-Path $destDir ("tokless-download-" + [Guid]::NewGuid().ToString("N") + ".exe")
if ($selfUpdate -and $selfUpdate -notmatch '^[1-9][0-9]*$') {
    Write-Host "Invalid self-update process ID: $selfUpdate" -ForegroundColor Red
    exit 1
}
try {
    Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing
} catch {
    Remove-Item $tmp -Force -ErrorAction SilentlyContinue
    Write-Host "Download failed ($asset): $($_.Exception.ToString())`n$($_ | Out-String)" -ForegroundColor Red
    exit 1
}
if (-not (Test-Path -LiteralPath $tmp)) {
    Write-Host "Download finished but $tmp is missing; it may have been removed by Windows Defender or other antivirus software." -ForegroundColor Red
    Write-Host "Restore it from Windows Security protection history or add an exclusion."
    Write-Host "Report a false positive: https://www.microsoft.com/en-us/wdsi/filesubmission"
    exit 1
}

if ($selfUpdate) {
    try {
        $script = @'
param([string]$ConfigFile)
$StatusFile = $null
$staged = $null
$restore = $null
$verified = $false
$Source = $null
$failure = $null
try {
    $config = Get-Content -LiteralPath $ConfigFile -Raw | ConvertFrom-Json
    $ProcessId = [int]$config.ProcessId
    $Source = [string]$config.Source
    $Destination = [string]$config.Destination
    $StatusFile = [string]$config.StatusFile
    if ($ProcessId -le 0) { throw "Process ID must be a positive integer" }
    if (-not $StatusFile) { throw "Status file is required" }
    $process = Get-Process -Id $ProcessId -ErrorAction Stop
    if ($process.StartTime.ToUniversalTime().ToString("o") -ne [string]$config.StartTime -or $process.Path -ne [string]$config.Path) { throw "Self-update process identity changed" }
    $process.WaitForExit()
    $staged = $Destination + ".new"
    Move-Item -LiteralPath $Source -Destination $staged -Force
    $bak = "$Destination.$([Guid]::NewGuid().ToString('N')).bak"
    if (Test-Path -LiteralPath $Destination) {
        $restore = $bak
        [System.IO.File]::Replace($staged, $Destination, $bak)
    } else {
        Move-Item -LiteralPath $staged -Destination $Destination
    }
    Start-Sleep -Seconds 2
    $versionOutput = & $Destination --version 2>$null
    $versionExit = $LASTEXITCODE
    $version = (($versionOutput | Out-String).Trim() -replace '^tokless\s+', '')
    if ($versionExit -ne 0 -or -not $version) { throw "Could not read version from replaced executable" }
    $verified = $true
    $dataRoot = if ($env:LOCALAPPDATA) { $env:LOCALAPPDATA } else { Join-Path $env:USERPROFILE "AppData\Local" }
    $dataDir = Join-Path $dataRoot "tokless"
    New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
    $installJson = @{
        method  = "install script"
        path    = $Destination
        version = $version
        at      = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    } | ConvertTo-Json -Compress
    [System.IO.File]::WriteAllText((Join-Path $dataDir "install.json"), $installJson)
    Get-ChildItem -LiteralPath (Split-Path -Parent $Destination) -Filter ((Split-Path -Leaf $Destination) + ".*.bak") -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue
    $message = "Update replacement completed: $Destination"
} catch {
    $failure = $_.Exception.ToString()
    $message = "Update replacement failed: $failure"
    if ($restore -and -not $verified -and (Test-Path -LiteralPath $restore)) {
        try { Move-Item -LiteralPath $restore -Destination $Destination -Force -ErrorAction Stop; $message += "; previous executable restored" }
        catch { $message += "; restore failed: " + $_.Exception.Message }
    }
} finally {
    if ($failure -and $staged) { Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue }
    if ($Source) { Remove-Item -LiteralPath $Source -Force -ErrorAction SilentlyContinue }
    Remove-Item -LiteralPath $ConfigFile -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $PSCommandPath -Force -ErrorAction SilentlyContinue
    try { if ($StatusFile) { Set-Content -LiteralPath $StatusFile -Value $message -Encoding UTF8 -ErrorAction Stop } }
    catch { [Console]::Error.WriteLine($message + "; status write failed: " + $_.Exception.Message) }
}
'@
        $targetProcess = Get-Process -Id ([int]$selfUpdate) -ErrorAction Stop
        $targetStartTime = $targetProcess.StartTime.ToUniversalTime().ToString("o")
        $targetPath = $targetProcess.Path
        if ($targetPath -ne $selfUpdatePath) { throw "Self-update executable path does not match process path" }
        $configFile = Join-Path $env:TEMP ("tokless-update-" + [Guid]::NewGuid().ToString("N") + ".json")
        $statusFile = Join-Path $destDir "tokless-update-status-$([Guid]::NewGuid().ToString('N')).txt"
        @{ ProcessId = $selfUpdate; StartTime = $targetStartTime; Path = $targetPath; Source = $tmp; Destination = $dest; StatusFile = $statusFile } | ConvertTo-Json -Compress | Set-Content -LiteralPath $configFile -Encoding UTF8
        $helper = Join-Path $env:TEMP ("tokless-update-" + [Guid]::NewGuid().ToString("N") + ".ps1")
        Set-Content -LiteralPath $helper -Value $script -Encoding UTF8
        $powershell = (Get-Process -Id $PID).Path
        $helperArgs = @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", $helper, $configFile)
        $quotedArgs = $helperArgs | ForEach-Object { '"' + $_.Replace('"', '\\"') + '"' }
        Start-Process -FilePath $powershell -ArgumentList ($quotedArgs -join " ") -WindowStyle Hidden
    } catch {
        Remove-Item $tmp -Force -ErrorAction SilentlyContinue
        Write-Host "Could not schedule replacement: $_" -ForegroundColor Red
        exit 1
    }
    Write-Host "Update scheduled; replacement runs after tokless exits; run tokless again. Status file: $statusFile" -ForegroundColor Cyan
    exit 0
}
$bak = "$dest.$([Guid]::NewGuid().ToString('N')).bak"
try {
    Move-Item -LiteralPath $tmp -Destination ($dest + ".new") -Force
    if (Test-Path -LiteralPath $dest) {
        [System.IO.File]::Replace($dest + ".new", $dest, $bak)
    } else {
        Move-Item -LiteralPath ($dest + ".new") -Destination $dest
    }
} catch {
    if (-not (Test-Path -LiteralPath $dest) -and (Test-Path -LiteralPath $bak)) { Move-Item -LiteralPath $bak -Destination $dest -Force -ErrorAction SilentlyContinue }
    Remove-Item ($dest + ".new") -Force -ErrorAction SilentlyContinue
    Remove-Item $tmp -Force -ErrorAction SilentlyContinue
    Write-Host "Replacement failed: $_" -ForegroundColor Red
    exit 1
}

# Defender may quarantine the new binary right after it is written.
Start-Sleep -Seconds 2
$v = $null
if (Test-Path -LiteralPath $dest) {
    $prevEap = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $out = & $dest --version 2>$null
        if ($LASTEXITCODE -eq 0) { $v = ($out | Out-String).Trim() }
    } catch {} finally { $ErrorActionPreference = $prevEap }
}
if (-not $v) {
    $detected = $false
    try {
        $detected = [bool](Get-MpThreatDetection -ErrorAction Stop | Where-Object { $_.InitialDetectionTime -gt $started -and ($_.Resources -join ";") -like "*tokless*" })
    } catch {}
    Write-Host "Install failed: $dest is missing or does not run." -ForegroundColor Red
    if ($detected) {
        Write-Host "Windows Defender reported a detection involving tokless.exe during this install." -ForegroundColor Yellow
    } else {
        Write-Host "tokless.exe may have been removed by Windows Defender or other antivirus software." -ForegroundColor Yellow
    }
    Write-Host "Restore it from Windows Security protection history or add an exclusion."
    Write-Host "Report a false positive: https://www.microsoft.com/en-us/wdsi/filesubmission"
    if (Test-Path -LiteralPath $bak) {
        try {
            Move-Item -LiteralPath $bak -Destination $dest -Force
            Write-Host "Previous tokless.exe restored." -ForegroundColor Cyan
        } catch {
            Write-Host "Could not restore previous tokless.exe from ${bak}: $_" -ForegroundColor Red
        }
    }
    throw "tokless install failed"
}
Get-ChildItem -LiteralPath $destDir -Filter ((Split-Path -Leaf $dest) + ".*.bak") -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue

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

# install.json for `tokless info`
$dataDir = Join-Path $env:LOCALAPPDATA "tokless"
New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
$installJson = @{
    method  = "install script"
    path    = $dest
    version = "$v"
    at      = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
} | ConvertTo-Json -Compress
[System.IO.File]::WriteAllText((Join-Path $dataDir "install.json"), $installJson)

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
