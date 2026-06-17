$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..\..")

Write-Host "Preparing backend dependencies..."
Push-Location (Join-Path $Root "backend")
go mod tidy
Pop-Location

Write-Host "Preparing frontend dependencies..."
Push-Location (Join-Path $Root "frontend")
npm install
Pop-Location

Write-Host "OpenCuttles development dependencies are ready."
Write-Host "Start the app with: powershell -ExecutionPolicy Bypass -File scripts\dev\start.ps1"
