[CmdletBinding()]
param(
  [string]$BaseUrl = $(if ($env:COMPOSITOR_SMOKE_URL) { $env:COMPOSITOR_SMOKE_URL } else { "http://127.0.0.1:34115" }),
  [string]$EdgePath = "C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe",
  [string]$PlaywrightRoot = (Join-Path ([System.IO.Path]::GetTempPath()) "compositor-playwright-smoke"),
  [string]$EvidenceDir = "",
  [switch]$StartWails,
  [switch]$Headed
)

$ErrorActionPreference = "Stop"
$repo = (Resolve-Path (Join-Path $PSScriptRoot "..\..\")).Path
$smokeScript = Join-Path $repo "tests\e2e\real-browser-smoke.mjs"

if (-not (Test-Path -LiteralPath $EdgePath -PathType Leaf)) {
  throw "Edge executable not found: $EdgePath"
}
if (-not (Test-Path -LiteralPath $smokeScript -PathType Leaf)) {
  throw "Smoke harness not found: $smokeScript"
}

$playwrightPackage = Join-Path $PlaywrightRoot "node_modules\playwright\package.json"
if (-not (Test-Path -LiteralPath $playwrightPackage -PathType Leaf)) {
  New-Item -ItemType Directory -Force -Path $PlaywrightRoot | Out-Null
  Write-Host "Installing Playwright into isolated temp directory: $PlaywrightRoot"
  & npm install --prefix $PlaywrightRoot --no-save --no-package-lock playwright@1.64.0
  if ($LASTEXITCODE -ne 0) {
    throw "Isolated Playwright installation failed with exit code $LASTEXITCODE"
  }
}

function Test-DevServer {
  try {
    $response = Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/" -TimeoutSec 2
    return $true
  } catch {
    # A 4xx still proves that a listener is present; connection failures do not.
    return $null -ne $_.Exception.Response
  }
}

if ($StartWails -and -not (Test-DevServer)) {
  Write-Host "Starting Wails dev server (the process is left running for inspection)..."
  $dev = Start-Process -FilePath "go" `
    -ArgumentList @("run", "github.com/wailsapp/wails/v2/cmd/wails@v2.16.0", "dev", "-browser") `
    -WorkingDirectory $repo -PassThru -WindowStyle Hidden
  Write-Host "Wails dev process id: $($dev.Id)"
}

if (-not (Test-DevServer)) {
  $deadline = (Get-Date).AddSeconds(90)
  do {
    Start-Sleep -Milliseconds 500
    if (Test-DevServer) { break }
  } while ((Get-Date) -lt $deadline)
}

if (-not (Test-DevServer)) {
  throw "Wails dev server is not reachable at $BaseUrl. Start it with: go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 dev -browser"
}

$nodeArgs = @(
  $smokeScript,
  "--base-url", $BaseUrl,
  "--edge-path", $EdgePath,
  "--playwright-root", $PlaywrightRoot
)
if ($EvidenceDir) { $nodeArgs += @("--evidence-dir", $EvidenceDir) }
if ($Headed) { $nodeArgs += "--headed" }

Write-Host "Running real Edge smoke against $BaseUrl"
& node @nodeArgs
exit $LASTEXITCODE
