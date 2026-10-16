# Build the keyrotator binary into bin\keyrotator.exe.
# Usage:  powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1
$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
$go = "$env:LOCALAPPDATA\Programs\GoToolchain\go\bin\go.exe"
if (-not (Test-Path $go)) { $go = 'go' }

$out = Join-Path $root 'bin\keyrotator.exe'
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $out) | Out-Null

Push-Location $root
try {
    & $go build -trimpath -ldflags "-s -w" -o $out ./cmd/keyrotator
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    Write-Host "Built $out" -ForegroundColor Green
    Get-Item $out | Select-Object Name, Length, LastWriteTime | Format-List
} finally {
    Pop-Location
}
