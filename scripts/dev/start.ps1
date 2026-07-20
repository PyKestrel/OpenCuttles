$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..\..")
$DataDir = Join-Path $Root "data\dev"
$ImageDir = Join-Path $Root "data\images"
$DbPath = if ($env:OPENCUTTLES_DB) { $env:OPENCUTTLES_DB } else { Join-Path $DataDir "opencuttles.db" }

New-Item -ItemType Directory -Force -Path $DataDir, $ImageDir | Out-Null

$env:OPENCUTTLES_LISTEN = if ($env:OPENCUTTLES_LISTEN) { $env:OPENCUTTLES_LISTEN } else { "127.0.0.1:8080" }
$env:OPENCUTTLES_DB = $DbPath
$env:OPENCUTTLES_ALLOWED_ORIGIN = if ($env:OPENCUTTLES_ALLOWED_ORIGIN) { $env:OPENCUTTLES_ALLOWED_ORIGIN } else { "http://localhost:5173" }
$env:OPENCUTTLES_SECURE_COOKIES = "0"
# Local dev only: allows claiming the first admin without a bootstrap token.
$env:OPENCUTTLES_DEV_MODE = "1"
$env:OPENCUTTLES_EXECUTE_CVD = if ($env:OPENCUTTLES_EXECUTE_CVD) { $env:OPENCUTTLES_EXECUTE_CVD } else { "0" }
$env:OPENCUTTLES_IMAGE_ROOT = if ($env:OPENCUTTLES_IMAGE_ROOT) { $env:OPENCUTTLES_IMAGE_ROOT } else { $ImageDir }

Write-Host "Starting OpenCuttles API on http://$env:OPENCUTTLES_LISTEN"
$Api = Start-Process -FilePath "go" -ArgumentList "run ./cmd/opencuttles-api" -WorkingDirectory (Join-Path $Root "backend") -PassThru -NoNewWindow

$NodeModules = Join-Path $Root "frontend\node_modules"
if (-not (Test-Path $NodeModules)) {
  Write-Host "Installing frontend dependencies..."
  Push-Location (Join-Path $Root "frontend")
  npm install
  Pop-Location
}

Write-Host "Starting OpenCuttles dashboard on http://localhost:5173"
$Ui = Start-Process -FilePath "npm" -ArgumentList "run dev -- --host 127.0.0.1" -WorkingDirectory (Join-Path $Root "frontend") -PassThru -NoNewWindow

Write-Host ""
Write-Host "Open http://localhost:5173"
Write-Host "Bootstrap token is not required in local dev mode."
Write-Host "Press Ctrl+C to stop both services."

try {
  while (-not $Api.HasExited -and -not $Ui.HasExited) {
    Start-Sleep -Seconds 1
  }
}
finally {
  if (-not $Api.HasExited) { Stop-Process -Id $Api.Id -Force }
  if (-not $Ui.HasExited) { Stop-Process -Id $Ui.Id -Force }
}
