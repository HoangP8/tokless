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

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$destDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$destDir;$userPath", "User")
    $env:Path = "$destDir;$env:Path"
}

$v = & $dest --version 2>$null
Write-Host "✔ tokless $v ready → $dest" -ForegroundColor Green
Write-Host "Open a new terminal, then run: tokless" -ForegroundColor Cyan
