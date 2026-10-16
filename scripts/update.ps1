# Rebuild keyrotator and restart it.
#
# A running keyrotator.exe keeps its file locked, so the instance is stopped
# first, then the new binary is built over it and started again.
# If the running instance was launched elevated, run this from an
# Administrator PowerShell.
#
# Usage:  powershell -ExecutionPolicy Bypass -File .\scripts\update.ps1

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $root 'bin\keyrotator.exe'

$running = Get-Process keyrotator -ErrorAction SilentlyContinue
foreach ($p in $running) {
    Write-Host "stopping running instance (pid $($p.Id))..."
    Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
}
Start-Sleep -Milliseconds 900

if (Get-Process keyrotator -ErrorAction SilentlyContinue) {
    throw "keyrotator.exe is still running and could not be stopped. Close it, or re-run this script from an Administrator PowerShell."
}

& (Join-Path $PSScriptRoot 'build.ps1')
if ($LASTEXITCODE -ne 0) { throw "build failed" }
Write-Host "keyrotator.exe updated" -ForegroundColor Green

Start-Process -FilePath $exe -ArgumentList '-quiet'
Start-Sleep -Seconds 1
Write-Host "restarted on http://127.0.0.1:8787/" -ForegroundColor Green
