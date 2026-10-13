#requires -version 5.1
<#
.SYNOPSIS
    Prepare pinned ONNX assets for a Windows amd64 build.

.DESCRIPTION
    Keep verified downloads in build/ml-assets, outside Wails' build/bin cleanup.
    The pre-build hook uses -CacheOnly; the post-build hook stages them -Offline.
    Running this script directly prepares the cache and stages the output.
#>
[CmdletBinding()]
param(
    [string]$RepoRoot = "",
    [switch]$Offline,
    [switch]$CacheOnly,
    [string]$Platform = "windows/amd64"
)

$ErrorActionPreference = "Stop"
if (-not $RepoRoot) { $RepoRoot = Join-Path $PSScriptRoot ".." }
if ($Platform -ne "windows/amd64") {
    throw "Bundled ONNX Runtime supports windows/amd64 only; requested $Platform"
}
$RepoRoot = (Resolve-Path -LiteralPath $RepoRoot).Path
$ModelUrl = "https://github.com/danielgatis/rembg/releases/download/v0.0.0/u2netp.onnx"
$ModelSha256 = "309C8469258DDA742793DCE0EBEA8E6DD393174F89934733ECC8B14C76F4DDD8"
$RuntimeSha256 = "69D8E6D3879A3B4001CDC74C8ED9CCC7E7F799A5B847059738323404519EC471"
$BinDir = Join-Path $RepoRoot "build\bin"
$CacheDir = Join-Path $RepoRoot "build\ml-assets"
$ModelPath = Join-Path $CacheDir "u2netp.onnx"
$RuntimePath = Join-Path $CacheDir "onnxruntime.dll"
$RuntimeModule = "github.com/yalue/onnxruntime_go@v1.36.0"
$NoticesSource = Join-Path $RepoRoot "THIRD-PARTY-NOTICES.md"
$NoticesPath = Join-Path $BinDir "THIRD-PARTY-NOTICES.md"
$LicensesSource = Join-Path $RepoRoot "licenses"
$LicensesPath = Join-Path $BinDir "licenses"
$LicenseNames = @("u2netp-Apache-2.0.txt", "onnxruntime-MIT.txt", "onnxruntime-go-MIT.txt")

function Assert-Hash([string]$Path, [string]$Expected, [string]$Label) {
    $item = Get-Item -LiteralPath $Path
    if ($item.Length -lt 2MB) { throw "$Label is too small: $($item.Length) bytes" }
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToUpperInvariant()
    if ($actual -ne $Expected) { throw "$Label SHA-256 mismatch: $actual" }
    Write-Host "[ ok ] $Label $($item.Length) bytes SHA-256 $actual" -ForegroundColor Green
}

function Import-ExistingAsset([string]$Source, [string]$Destination, [string]$Hash, [string]$Label) {
    if (-not (Test-Path -LiteralPath $Destination) -and (Test-Path -LiteralPath $Source)) {
        Assert-Hash $Source $Hash $Label
        Copy-Item -LiteralPath $Source -Destination $Destination -Force
    }
}

# Verify notices before starting a build, including cache-only runs.
if (-not (Test-Path -LiteralPath $NoticesSource -PathType Leaf)) { throw "Missing third-party notices: $NoticesSource" }
foreach ($name in $LicenseNames) {
    $source = Join-Path $LicensesSource $name
    if (-not (Test-Path -LiteralPath $source -PathType Leaf)) { throw "Missing license: $source" }
}
New-Item -ItemType Directory -Path $CacheDir -Force | Out-Null
Import-ExistingAsset (Join-Path $BinDir "models\u2netp.onnx") $ModelPath $ModelSha256 "existing u2netp.onnx"
Import-ExistingAsset (Join-Path $BinDir "onnxruntime.dll") $RuntimePath $RuntimeSha256 "existing onnxruntime.dll"
if (Test-Path -LiteralPath $ModelPath) {
    try { Assert-Hash $ModelPath $ModelSha256 "u2netp.onnx" }
    catch {
        if ($Offline) { throw }
        Remove-Item -LiteralPath $ModelPath -Force
    }
}
if (-not (Test-Path -LiteralPath $ModelPath)) {
    if ($Offline) { throw "Offline mode: missing $ModelPath" }
    $temporary = "$ModelPath.download"
    Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
    Write-Host "[info] Downloading pinned u2netp model..."
    try {
        Invoke-WebRequest -Uri $ModelUrl -OutFile $temporary -UseBasicParsing
        Assert-Hash $temporary $ModelSha256 "u2netp.onnx download"
        Move-Item -LiteralPath $temporary -Destination $ModelPath -Force
    }
    finally {
        Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
    }
}

if (-not (Test-Path -LiteralPath $RuntimePath)) {
    Push-Location -LiteralPath $RepoRoot
    try {
        if ($Offline) {
            $moduleCache = & go env GOMODCACHE
            if ($LASTEXITCODE -ne 0) { throw "go env GOMODCACHE failed" }
            $moduleDir = Join-Path ($moduleCache.Trim()) $RuntimeModule
        }
        else {
            # A clean CI checkout may not have downloaded app dependencies yet.
            $moduleJson = & go mod download -json $RuntimeModule
            if ($LASTEXITCODE -ne 0) { throw "Could not download locked dependency $RuntimeModule" }
            $moduleInfo = ($moduleJson -join "`n") | ConvertFrom-Json
            if ($moduleInfo.Error -or -not $moduleInfo.Dir) { throw "Invalid Go module download result for $RuntimeModule" }
            $moduleDir = $moduleInfo.Dir
        }
        $source = Join-Path $moduleDir "test_data\onnxruntime.dll"
        if (-not (Test-Path -LiteralPath $source)) { throw "Missing ONNX Runtime DLL: $source" }
        Assert-Hash $source $RuntimeSha256 "dependency onnxruntime.dll"
        Copy-Item -LiteralPath $source -Destination $RuntimePath -Force
    }
    finally { Pop-Location }
}
Assert-Hash $RuntimePath $RuntimeSha256 "onnxruntime.dll"
if ($CacheOnly) {
    Write-Host "[ ok ] Verified ML cache under $CacheDir" -ForegroundColor Green
    return
}
$ModelDir = Join-Path $BinDir "models"
New-Item -ItemType Directory -Path $ModelDir, $LicensesPath -Force | Out-Null
Copy-Item -LiteralPath $ModelPath -Destination (Join-Path $ModelDir "u2netp.onnx") -Force
Copy-Item -LiteralPath $RuntimePath -Destination (Join-Path $BinDir "onnxruntime.dll") -Force
Copy-Item -LiteralPath $NoticesSource -Destination $NoticesPath -Force
foreach ($name in $LicenseNames) {
    Copy-Item -LiteralPath (Join-Path $LicensesSource $name) -Destination (Join-Path $LicensesPath $name) -Force
}
Write-Host "[ ok ] ML assets prepared under $BinDir" -ForegroundColor Green
