# tokless installer for Windows (PowerShell 5.1+).
#   irm https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.ps1 | iex


$ErrorActionPreference = "Stop"
$Owner = "HoangP8"
$Repo  = "tokless"

$asset = "tokless-windows-x64.exe"
$url   = "https://github.com/$Owner/$Repo/releases/latest/download/$asset"
$destDir = Join-Path $env:LOCALAPPDATA "Programs\tokless"
$dest = Join-Path $destDir "tokless.exe"

New-Item -ItemType Directory -Force -Path $destDir | Out-Null
try {
    Invoke-WebRequest -Uri $url -OutFile $dest -UseBasicParsing
} catch {
    Write-Host "✖ Download failed ($asset). See https://github.com/$Owner/$Repo/releases" -ForegroundColor Red
    exit 1
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

$v = & $dest --version 2>$null
Write-Host "✔ tokless $v ready → $dest" -ForegroundColor Green

$hadInstallerRun = Test-Path Env:TOKLESS_INSTALLER_RUN
$prevInstallerRun = $env:TOKLESS_INSTALLER_RUN
if ([Environment]::UserInteractive -and -not $env:CI) {
    Write-Host ""
    try {
        $env:TOKLESS_INSTALLER_RUN = "1"
        & $dest
    } finally {
        if ($hadInstallerRun) {
            $env:TOKLESS_INSTALLER_RUN = $prevInstallerRun
        } else {
            Remove-Item Env:TOKLESS_INSTALLER_RUN -ErrorAction SilentlyContinue
        }
    }
} else {
    Write-Host "Run: tokless" -ForegroundColor Cyan
}
