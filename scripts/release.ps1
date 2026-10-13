#requires -version 5.1
<#
.SYNOPSIS
    票 50 的本地发布链路：构建 NSIS 安装包、可选签名、生成更新 feed。

.DESCRIPTION
    六步（任一步失败都以非零码退出并说明原因）：
      1. 幂等清理本次版本的旧产物（dist/ 与 build/bin 里的旧安装器）；
      2. wails build -nsis（构建钩子准备固定 SHA-256 的 ONNX 模型与运行时 DLL）；
      3. 可选 Authenticode 签名（证书只从参数或环境变量来，绝不落进仓库）；
      4. 把安装器改名为 Compositor-<ver>-setup.exe；
      5. 调 scripts/genfeed 生成 releases/feed.json；
      6. 打印产物清单（名字 / 大小 / sha256）与下一步。

    与 CI 的分工：.github/workflows/release.yml 与本脚本步骤等价，CI 只是把
    同一套参数换成 GitHub secrets。安装→升级→卸载的验收是 scripts/verify-install.ps1。

    降级路径（票面验收 4）：
      * 无签名证书（-SkipSign，或没有证书 / 没有 signtool）：跳过签名并打印醒目警告；
      * 无 COMPOSITOR_UPDATE_KEY（-SkipFeed，或环境里没有）：跳过 feed 生成并警告。
      两者都只警告不失败；真正配了却做不成（证书文件不存在、genfeed 报错）才算失败。
      scripts/genfeed 是仓库里的文件：找不到它是仓库不完整，**硬失败**而不是降级 ——
      否则会静默产出一个没有 feed 的"发布"，用户永远收不到更新。

.PARAMETER Version
    版本号，不带 v 前缀（如 0.2.0）。省略时取最近的 git tag（去掉 v）。

.PARAMETER SkipSign
    跳过代码签名。

.PARAMETER SkipFeed
    跳过 feed.json 生成。

.PARAMETER DryRun
    演练：照常检查工具链、算版本与路径、生成发行说明，但不构建（除非给了 -InstallerPath）/ 不签名 / 不写 dist。
    用来在没有完整工具链的机器上先看清这次发布会做什么。

.PARAMETER InstallerPath
    已有安装器的路径。配合 -DryRun，跳过构建直接对这份安装器跑 genfeed，
    用于在没有 NSIS 的机器上验证 feed 生成链路。

.PARAMETER CertPath
    PFX/P12 证书路径。默认取环境变量 COMPOSITOR_CERT_PATH。

.PARAMETER CertPassword
    证书密码。默认取环境变量 COMPOSITOR_CERT_PASSWORD。

.PARAMETER SignToolPath
    signtool.exe 路径。默认取环境变量 COMPOSITOR_SIGNTOOL，
    其次 vswhere 与 "C:\Program Files (x86)\Windows Kits\10\bin" 探测（取最新 x64）。

.PARAMETER TimestampUrl
    时间戳服务器。默认 http://timestamp.digicert.com。

.PARAMETER Repo
    GitHub 仓库 owner/name，用于拼 feed 里的安装器下载地址。
    默认取环境变量 GITHUB_REPOSITORY，缺失时用 <owner>/<repo> 占位符。

.PARAMETER NotesFile
    发行说明文件。默认从仓库根 CHANGELOG.md 抽本版本段落；
    没有 CHANGELOG.md 或没有该版本段落时退回 git log 摘要。

.PARAMETER AllowMissingKey
    COMPOSITOR_UPDATE_KEY 缺失时直接失败（而不是跳过 feed）。
    CI 里用它把「secret 配错了」和「故意不生成 feed」区分开。

.EXAMPLE
    pwsh -NoProfile -File scripts/release.ps1 -Version 0.2.0

.EXAMPLE
    # 降级路径（验收 4）：不签名、不生成 feed
    pwsh -NoProfile -File scripts/release.ps1 -Version 9.9.9 -SkipSign -SkipFeed
#>
[CmdletBinding()]
param(
    [string]$Version,
    [switch]$SkipSign,
    [switch]$SkipFeed,
    [switch]$DryRun,
    [string]$InstallerPath,
    [string]$CertPath = $env:COMPOSITOR_CERT_PATH,
    [string]$CertPassword = $env:COMPOSITOR_CERT_PASSWORD,
    [string]$SignToolPath = $env:COMPOSITOR_SIGNTOOL,
    [string]$TimestampUrl = "http://timestamp.digicert.com",
    [string]$Repo = $env:GITHUB_REPOSITORY,
    [string]$NotesFile,
    [switch]$AllowMissingKey
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# 退出码约定：2 = 用法 / 工具链前置条件不满足；1 = 构建 / 签名 / feed / 校验失败。
$script:ExitUsage = 2
$script:ExitFailure = 1

function Write-Header([string]$Text) {
    Write-Host ""
    Write-Host ("=" * 78) -ForegroundColor DarkGray
    Write-Host "  $Text" -ForegroundColor Cyan
    Write-Host ("=" * 78) -ForegroundColor DarkGray
}
function Write-Info([string]$Text) { Write-Host "  [info] $Text" }
function Write-Ok([string]$Text) { Write-Host "  [ ok ] $Text" -ForegroundColor Green }
function Write-Warn([string]$Text) { Write-Host "  [warn] $Text" -ForegroundColor Yellow }
function Write-Err([string]$Text) { Write-Host "  [fail] $Text" -ForegroundColor Red }
function Write-Step([string]$Text) { Write-Host "  --- $Text" -ForegroundColor White }

function Stop-Release([string]$Message, [int]$Code = 1) {
    Write-Err $Message
    Write-Host ""
    Write-Err "发布中止（退出码 $Code）。修掉上面这条再重跑；脚本幂等，重跑安全。"
    exit $Code
}

# 跑外部命令并检查退出码：PowerShell 不会因为原生命令非零而自动停。
function Invoke-Native([string]$Exe, [string[]]$Arguments, [string]$What, [switch]$Capture) {
    Write-Host ("    $ " + $Exe + " " + ($Arguments -join " ")) -ForegroundColor DarkGray
    if ($Capture) {
        $output = & $Exe @Arguments 2>&1
        $code = $LASTEXITCODE
        if ($code -ne 0) { Stop-Release ("$What 失败（退出码 $code）：" + [Environment]::NewLine + ($output | Out-String)) 1 }
        return ($output | Out-String)
    }
    & $Exe @Arguments
    $code = $LASTEXITCODE
    if ($code -ne 0) { Stop-Release "$What 失败（退出码 $code）" 1 }
    return ""
}

function Resolve-Tool([string]$Name, [string[]]$Fallbacks) {
    $cmd = Get-Command $Name -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    foreach ($f in $Fallbacks) { if ($f -and (Test-Path -LiteralPath $f)) { return $f } }
    return $null
}

# 找 signtool：优先参数 / 环境变量，其次 vswhere 的 SDK，最后扫 Windows Kits（取最新 x64）。
function Resolve-SignTool([string]$Explicit) {
    if ($Explicit) {
        if (Test-Path -LiteralPath $Explicit) { return (Resolve-Path -LiteralPath $Explicit).Path }
        return $null
    }
    $candidates = New-Object System.Collections.Generic.List[string]

    $vswhere = Join-Path ${env:ProgramFiles(x86)} "Microsoft Visual Studio\Installer\vswhere.exe"
    if (Test-Path -LiteralPath $vswhere) {
        $vsRoot = (& $vswhere -latest -products * -property installationPath 2>$null | Out-String).Trim()
        if ($vsRoot) {
            $kits = Join-Path $vsRoot "SDK\bin"
            if (Test-Path -LiteralPath $kits) {
                Get-ChildItem -LiteralPath $kits -Directory -ErrorAction SilentlyContinue |
                    Sort-Object Name -Descending |
                    ForEach-Object {
                        $c = Join-Path $_.FullName "x64\signtool.exe"
                        if (Test-Path -LiteralPath $c) { $candidates.Add($c) }
                    }
            }
        }
    }

    $kitsRoot = "C:\Program Files (x86)\Windows Kits\10\bin"
    if (Test-Path -LiteralPath $kitsRoot) {
        Get-ChildItem -LiteralPath $kitsRoot -Directory -ErrorAction SilentlyContinue |
            Where-Object { $_.Name -match "^[0-9]" } |
            Sort-Object { [version]($_.Name -replace "[^0-9.]", "") } -Descending |
            ForEach-Object {
                $c = Join-Path $_.FullName "x64\signtool.exe"
                if (Test-Path -LiteralPath $c) { $candidates.Add($c) }
            }
    }

    if ($candidates.Count -gt 0) { return $candidates[0] }
    return $null
}

function Get-InstallerCandidate([string]$BinDir) {
    if (-not (Test-Path -LiteralPath $BinDir)) { return $null }
    $all = @(Get-ChildItem -LiteralPath $BinDir -Filter "*.exe" -File -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -match "installer" })
    if ($all.Count -eq 0) { return $null }
    return ($all | Sort-Object LastWriteTime -Descending)[0]
}

function Get-ChangelogSection([string]$FilePath, [string]$Ver, [string]$OutPath) {
    $lines = @(Get-Content -LiteralPath $FilePath)
    $escaped = [regex]::Escape($Ver)
    $start = -1
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -match "^#{1,4}\s*\[?v?$escaped\]?(\s|`$|\[|\()") { $start = $i; break }
    }
    if ($start -lt 0) { return $false }
    $end = $lines.Count
    for ($j = $start + 1; $j -lt $lines.Count; $j++) {
        if ($lines[$j] -match "^#{1,4}\s") { $end = $j; break }
    }
    ($lines[$start..($end - 1)] -join [Environment]::NewLine) | Set-Content -LiteralPath $OutPath -Encoding UTF8
    return $true
}

function Get-GitSummary([string]$RepoRoot, [string]$Ver, [string]$OutPath) {
    $log = @(& git -C $RepoRoot log --no-merges --pretty=format:"- %s (%h)" -n 40 2>$null)
    if ($LASTEXITCODE -ne 0 -or -not $log) { $log = @("（git log 拿不到历史：这不是 git 仓库，或者 HEAD 还没有提交）") }
    $tags = @(& git -C $RepoRoot tag --sort=-creatordate 2>$null | Select-Object -First 5)
    $body = New-Object System.Collections.Generic.List[string]
    $body.Add("## Compositor $Ver")
    $body.Add("")
    $body.Add("（CHANGELOG.md 里没有本版本段落，这里是 git log 摘要）")
    $body.Add("")
    $body.AddRange([string[]]$log)
    ($body -join [Environment]::NewLine) | Set-Content -LiteralPath $OutPath -Encoding UTF8
    Write-Warn "没有 CHANGELOG.md 或没有 $Ver 段落，已退回 git log 摘要：$OutPath"
    if ($tags.Count -gt 0) { Write-Info "最近的 tag：$($tags -join ', ')" }
    return $true
}
# ---------------------------------------------------------------------------
# 0. 参数与环境
# ---------------------------------------------------------------------------

$RepoRoot = Split-Path -Parent $PSScriptRoot
if (-not (Test-Path -LiteralPath (Join-Path $RepoRoot "wails.json"))) {
    Stop-Release "脚本不在仓库里：找不到 $RepoRoot\wails.json。请从仓库根目录跑 scripts\release.ps1。" $script:ExitUsage
}
if ($env:OS -ne "Windows_NT") {
    Stop-Release "本脚本只支持 Windows（NSIS + signtool）。" $script:ExitUsage
}

Write-Header "票 50 发布链路（构建 -> 可选签名 -> 改名 -> feed -> 清单）"
Write-Info "仓库根目录：$RepoRoot"

if (-not $Version) {
    $latestTag = (& git -C $RepoRoot describe --tags --abbrev=0 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -eq 0 -and $latestTag) { $Version = ($latestTag -replace "^v", "") }
}
if (-not $Version) {
    Stop-Release "缺少版本号：用 -Version 0.2.0（或不带 v 前缀的 tag）。" $script:ExitUsage
}
if ($Version -notmatch "^[0-9]+\.[0-9]+\.[0-9]+([-.+][0-9A-Za-z.\-]+)?$") {
    Stop-Release "版本号格式不对：$Version。期望形如 0.2.0 或 0.2.0-beta.1。" $script:ExitUsage
}

$InstallerName = "Compositor-$Version-setup.exe"
$DistDir = Join-Path $RepoRoot "dist"
$BinDir = Join-Path $RepoRoot "build\bin"
$OutInstaller = Join-Path $DistDir $InstallerName
$FeedPath = Join-Path $RepoRoot "releases\feed.json"
$NotesOut = Join-Path $DistDir "release-notes-$Version.md"
$GoPkgPrefix = "compositor-win"

if (-not $Repo) { $Repo = "<owner>/<repo>" }
# 两个不同的 URL，别混：
#   $FeedUrl      —— 应用要拉的那张 feed（releases/latest，通道稳定）
#   $InstallerUrl —— feed 里 installer.url 指的那个安装器（钉在本版本 tag 上）
# 早期版本把 FeedURL 注入成了安装器地址：发布版会去 GET 一个 50MB 的 exe、解析失败、
# 检查更新永远报 error。下面有断言挡住这种回归。
$FeedUrl = "https://github.com/$Repo/releases/latest/download/feed.json"
$InstallerUrl = "https://github.com/$Repo/releases/download/v$Version/$InstallerName"
if ($FeedUrl -notlike "*feed.json") {
    Stop-Release "注入给应用的 FeedURL 必须以 feed.json 结尾（实际 $FeedUrl）。" $script:ExitFailure
}

Write-Info "版本        ：$Version"
Write-Info "安装器目标名：$InstallerName"
Write-Info "feed 地址   ：$FeedUrl"
Write-Info "安装器下载址：$InstallerUrl"

$goExe = Resolve-Tool "go" @()
if (-not $goExe) { Stop-Release "找不到 go。请安装 Go（go.mod 要求 go 1.26+）并保证 go 在 PATH 上。" $script:ExitUsage }
$goPathBin = (& $goExe env GOPATH 2>$null | Out-String).Trim()
if (-not $goPathBin) { $goPathBin = Join-Path $env:USERPROFILE "go" }

$wailsExe = Resolve-Tool "wails" @((Join-Path $goPathBin "bin\wails.exe"))
$makensisExe = Resolve-Tool "makensis" @("C:\Program Files (x86)\NSIS\makensis.exe")

if (-not $wailsExe) {
    $msg = "找不到 wails CLI。装它：go install github.com/wailsapp/wails/v2/cmd/wails@latest（装完在 $goPathBin\bin）。"
    if ($DryRun) { Write-Warn $msg } else { Stop-Release $msg $script:ExitUsage }
}
if (-not $makensisExe) {
    $msg = "找不到 makensis（NSIS）。wails build -nsis 缺 makensis 时只警告并跳过安装器，所以这里前置失败：choco install nsis -y，或装 NSIS 后把 makensis.exe 加进 PATH。"
    if ($DryRun) { Write-Warn $msg } else { Stop-Release $msg $script:ExitUsage }
}
if ($wailsExe) { Write-Ok "wails    : $wailsExe" }
if ($makensisExe) { Write-Ok "makensis : $makensisExe" }
Write-Ok "go       : $goExe"

# 签名材料：只从参数 / 环境变量来，不读仓库里的任何东西。
$signingRequested = -not $SkipSign
$signTool = $null
if ($signingRequested) {
    if (-not $CertPath) {
        Write-Warn "没有证书：未给 -CertPath，环境变量 COMPOSITOR_CERT_PATH 也为空 —— 跳过签名（降级路径，构建仍可用）。"
        $signingRequested = $false
    }
    elseif (-not (Test-Path -LiteralPath $CertPath)) {
        Stop-Release "证书文件不存在：$CertPath（-SkipSign 可跳过签名）。" $script:ExitFailure
    }
    else {
        $signTool = Resolve-SignTool $SignToolPath
        if (-not $signTool) {
            Write-Warn "找不到 signtool.exe（没给 -SignToolPath / COMPOSITOR_SIGNTOOL，vswhere 与 Windows Kits 也没探测到）—— 跳过签名（降级路径）。"
            $signingRequested = $false
        }
        else {
            Write-Ok "signtool : $signTool"
            Write-Info "证书     : $CertPath"
            if (-not $CertPassword) { Write-Warn "证书密码为空（-CertPassword / COMPOSITOR_CERT_PASSWORD）；无密码的 PFX 也能签。" }
        }
    }
}
else {
    Write-Warn "请求了 -SkipSign：不签名（降级路径）。产出的安装器没有 Authenticode 签名，SmartScreen 会拦。"
}

$feedRequested = -not $SkipFeed
if ($feedRequested -and [string]::IsNullOrWhiteSpace($env:COMPOSITOR_UPDATE_KEY)) {
    if ($AllowMissingKey) {
        Stop-Release "COMPOSITOR_UPDATE_KEY 缺失（-AllowMissingKey 生效）：无法签名 feed。" $script:ExitFailure
    }
    Write-Warn "COMPOSITOR_UPDATE_KEY 缺失：跳过 feed.json 生成（降级路径）。应用内「检查更新」拿不到本次发行。"
    $feedRequested = $false
}
elseif ($SkipFeed) {
    Write-Warn "请求了 -SkipFeed：跳过 feed.json 生成。"
}
if ($feedRequested -and -not (Test-Path -LiteralPath (Join-Path $RepoRoot "scripts\genfeed\main.go"))) {
    # 这不是降级路径：genfeed 是仓库里的文件（scripts/genfeed/main.go）。缺它说明
    # checkout 不完整，硬失败比产出一个"没有 feed 的发布"诚实。
    Stop-Release "找不到 scripts\genfeed\main.go：仓库不完整，无法生成更新 feed（这不是可降级的路径）。" $script:ExitFailure
}

# ---------------------------------------------------------------------------
# 1. 幂等清理：重复运行不会把已存在的产物搞坏
# ---------------------------------------------------------------------------
Write-Header "1/6 清理本次版本的旧产物（幂等）"
if (-not (Test-Path -LiteralPath $DistDir)) { New-Item -ItemType Directory -Path $DistDir | Out-Null }
foreach ($stale in @(Get-ChildItem -LiteralPath $DistDir -Filter "*$Version*" -File -ErrorAction SilentlyContinue)) {
    Write-Info "删除旧产物：$($stale.FullName)"
    Remove-Item -LiteralPath $stale.FullName -Force
}
if (Test-Path -LiteralPath $FeedPath) {
    Write-Info "删除旧 feed：$FeedPath"
    Remove-Item -LiteralPath $FeedPath -Force
}
if (Test-Path -LiteralPath $BinDir) {
    foreach ($stale in @(Get-ChildItem -LiteralPath $BinDir -Filter "*installer*.exe" -File -ErrorAction SilentlyContinue)) {
        Write-Info "删除 build\bin 里的旧安装器：$($stale.Name)"
        Remove-Item -LiteralPath $stale.FullName -Force
    }
}
Write-Ok "清理完成。"

# ---------------------------------------------------------------------------
# 发行说明：-NotesFile > CHANGELOG.md 的本版本段落 > git log 摘要。
# 公钥也在这里先算出来，供第 3 步构建注入。
# ---------------------------------------------------------------------------
Write-Header "2/6 发行说明与更新公钥"
$notesUsed = $NotesFile
if ($notesUsed) {
    if (-not (Test-Path -LiteralPath $notesUsed)) {
        Stop-Release "指定的 -NotesFile 不存在：$notesUsed" $script:ExitFailure
    }
    Write-Info "发行说明：$notesUsed（-NotesFile）"
}
else {
    $changelog = Join-Path $RepoRoot "CHANGELOG.md"
    if ((Test-Path -LiteralPath $changelog) -and (Get-ChangelogSection $changelog $Version $NotesOut)) {
        $notesUsed = $NotesOut
        Write-Info "发行说明：$NotesOut（CHANGELOG.md 的 $Version 段落）"
    }
    else {
        Get-GitSummary $RepoRoot $Version $NotesOut | Out-Null
        $notesUsed = $NotesOut
    }
}

if ($feedRequested) {
    $goPk = @'
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
)

func main() {
	raw, err := base64.StdEncoding.DecodeString(os.Getenv("COMPOSITOR_UPDATE_KEY"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "COMPOSITOR_UPDATE_KEY 不是合法 base64:", err)
		os.Exit(1)
	}
	var key ed25519.PrivateKey
	switch len(raw) {
	case ed25519.SeedSize:
		key = ed25519.NewKeyFromSeed(raw)
	case ed25519.PrivateKeySize:
		key = ed25519.PrivateKey(raw)
	default:
		fmt.Fprintf(os.Stderr, "COMPOSITOR_UPDATE_KEY 长度应为 32 或 64 字节，得到 %d\n", len(raw))
		os.Exit(1)
	}
	fmt.Print(base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)))
}
'@
    $goPkFile = Join-Path $env:TEMP ("compositor-pubkey-" + [guid]::NewGuid().ToString("N") + ".go")
    Set-Content -LiteralPath $goPkFile -Value $goPk -Encoding UTF8
    try {
        $pkOut = (& $goExe run $goPkFile 2>&1 | Out-String).Trim()
        if ($LASTEXITCODE -ne 0) {
            Stop-Release "从 COMPOSITOR_UPDATE_KEY 推导公钥失败：$pkOut" $script:ExitFailure
        }
        $publicKey = $pkOut
        Write-Info "更新公钥（由私钥推导）：$publicKey"
    }
    finally {
        Remove-Item -LiteralPath $goPkFile -Force -ErrorAction SilentlyContinue
    }
}

# ---------------------------------------------------------------------------
# 3. 构建（公钥/feed 地址在这一步注入）
# ---------------------------------------------------------------------------
$wailsJson = Join-Path $RepoRoot "wails.json"
Write-Header "3/6 wails build -nsis"
# wails.json hooks verify the ML cache before compiling and stage it afterward.
$originalWailsJson = Get-Content -LiteralPath $wailsJson -Raw
$ldflags = "-X $GoPkgPrefix/internal/update.FeedURL=$FeedUrl -X $GoPkgPrefix/internal/bridge.BuildVersion=$Version"
if ($publicKey) { $ldflags += " -X $GoPkgPrefix/internal/update.PublicKeyB64=$publicKey" }
Write-Info "ldflags（版本 / feed 注入）：$ldflags"
if (-not $publicKey) { Write-Warn "本次构建没有注入更新公钥：应用内「检查更新」会以未配置处理（降级路径）。" }

if ($DryRun -and $InstallerPath) {
    Write-Warn "-DryRun -InstallerPath：跳过 wails build -nsis，改用已给的安装器。"
}
elseif ($DryRun) {
    Write-Warn "-DryRun：跳过 wails build -nsis（以及 wails.json 的临时版本同步）。"
}
else {
Push-Location $RepoRoot
try {
    # wails.json 的 info.productVersion 决定安装器与 exe 的版本资源；
    # 只为本次构建临时同步成 $Version，构建结束无论成败都恢复原文件。
    try {
        $cfg = Get-Content -LiteralPath $wailsJson -Raw | ConvertFrom-Json
        $cfg.info.productVersion = $Version
        ($cfg | ConvertTo-Json -Depth 20) | Set-Content -LiteralPath $wailsJson -Encoding UTF8
        Write-Info "已把 wails.json 的 info.productVersion 临时设为 $Version（构建后恢复）。"
    }
    catch {
        Write-Warn "无法改写 wails.json 的 productVersion（$($_.Exception.Message)）：安装器版本资源可能仍是旧值。"
    }

    try {
        Invoke-Native $wailsExe @("build", "-nsis", "-ldflags", $ldflags) "wails build -nsis"
    }
    finally {
        Set-Content -LiteralPath $wailsJson -Value $originalWailsJson -Encoding UTF8
        Write-Info "已恢复 wails.json。"
    }
}
finally {
    Pop-Location
}
Write-Ok "构建完成。"
}

# ---------------------------------------------------------------------------
# 4. 可选签名
# ---------------------------------------------------------------------------
Write-Header "4/6 可选代码签名"
$appExe = Join-Path $BinDir "Compositor.exe"
if ($DryRun) {
    Write-Warn "-DryRun：跳过签名步骤。"
}
elseif ($signingRequested) {
    if (-not (Test-Path -LiteralPath $appExe)) {
        Write-Warn "build\bin\Compositor.exe 不存在，跳过对 exe 的签名。"
    }
    else {
        Write-Step "签名应用 exe：$appExe"
        $signArgs = @("sign", "/fd", "SHA256", "/tr", $TimestampUrl, "/td", "SHA256", "/f", $CertPath)
        if ($CertPassword) { $signArgs += @("/p", $CertPassword) }
        $signArgs += $appExe
        Invoke-Native $signTool $signArgs "signtool（应用 exe）"
        Write-Ok "应用 exe 已签名。"
    }
}
else {
    Write-Warn "跳过签名：安装器与 exe 没有 Authenticode 签名（降级路径）。"
}

# ---------------------------------------------------------------------------
# 5. 改名安装器
# ---------------------------------------------------------------------------
Write-Header "5/6 重命名安装器"
$produced = Get-InstallerCandidate $BinDir
if ($DryRun -and $InstallerPath) {
    if (-not (Test-Path -LiteralPath $InstallerPath)) { Stop-Release "-InstallerPath 不存在：$InstallerPath" $script:ExitFailure }
    if ((Resolve-Path -LiteralPath $InstallerPath).Path -ne (Join-Path $DistDir $InstallerName)) {
        if (-not (Test-Path -LiteralPath $DistDir)) { New-Item -ItemType Directory -Path $DistDir | Out-Null }
        Copy-Item -LiteralPath $InstallerPath -Destination $OutInstaller -Force
    }
    Write-Info "已就位（-InstallerPath 原样），版本号 $Version 只用于文件名与 feed 元数据。"
}
elseif ($DryRun) {
    Write-Warn "-DryRun：跳过重命名与签名安装器（预期产物：$OutInstaller）。"
}
if (-not $produced -and -not $DryRun) {
    Stop-Release "wails build -nsis 没在 $BinDir 里留下 *installer*.exe。检查 build\windows\installer 是否完整、makensis 是否真的跑了。" $script:ExitFailure
}
if ($DryRun) {
    if ($produced) { Write-Info "build\bin 里当前最新的安装器：$($produced.Name)" }
    else { Write-Info "build\bin 里还没有安装器（-DryRun 没有真的构建）。" }
}
else {
    Write-Info "wails 产出：$($produced.FullName)（$([math]::Round($produced.Length / 1MB, 2)) MB）"
    if (-not (Test-Path -LiteralPath $DistDir)) { New-Item -ItemType Directory -Path $DistDir | Out-Null }
    if (Test-Path -LiteralPath $OutInstaller) { Remove-Item -LiteralPath $OutInstaller -Force }
    Move-Item -LiteralPath $produced.FullName -Destination $OutInstaller -Force
}
if (-not $DryRun) { Write-Ok "安装器：$OutInstaller" }

if ($DryRun) { Write-Info "-DryRun：跳过安装器签名。" }
elseif ($signingRequested) {
    Write-Step "签名安装器：$OutInstaller"
    $signArgs2 = @("sign", "/fd", "SHA256", "/tr", $TimestampUrl, "/td", "SHA256", "/f", $CertPath)
    if ($CertPassword) { $signArgs2 += @("/p", $CertPassword) }
    $signArgs2 += $OutInstaller
    Invoke-Native $signTool $signArgs2 "signtool（安装器）"
    Write-Ok "安装器已签名。"
}

# ---------------------------------------------------------------------------
# 6. 更新 feed（在签名之后：genfeed 要对**最终**安装器字节签名）
# ---------------------------------------------------------------------------
Write-Header "6/6 生成更新 feed（scripts/genfeed）"
if ($DryRun -and -not (Test-Path -LiteralPath $OutInstaller)) {
    Write-Warn "-DryRun：跳过 feed 生成（没有已就位的安装器）。"
}
elseif (-not $feedRequested) {
    Write-Warn "按前面的降级判定跳过 feed 生成。"
}
else {
    $releasesDir = Split-Path -Parent $FeedPath
    if (-not (Test-Path -LiteralPath $releasesDir)) { New-Item -ItemType Directory -Path $releasesDir | Out-Null }

    Write-Step "go run ./scripts/genfeed -installer $InstallerName -version $Version ..."
    $genOut = Invoke-Native $goExe @(
        "run", "./scripts/genfeed",
        "-installer", $OutInstaller,
        "-version", $Version,
        "-url", $InstallerUrl,
        "-notes-file", $notesUsed,
        "-out", $FeedPath
    ) "genfeed 生成 feed" -Capture
    Write-Host $genOut

    if (-not (Test-Path -LiteralPath $FeedPath)) {
        Stop-Release "genfeed 没写出 $FeedPath（退出码 0 但文件不存在）。" $script:ExitFailure
    }
    # 发布前兜底断言：feed 形状由 genfeed 权威，这里只挡住明显不对的。
    try {
        $feed = Get-Content -LiteralPath $FeedPath -Raw | ConvertFrom-Json
    }
    catch {
        Stop-Release "feed.json 不是合法 JSON：$($_.Exception.Message)" $script:ExitFailure
    }
    if ($feed.app -ne "compositor") { Stop-Release "feed.json 的 app 应为 compositor，实际 $($feed.app)。" $script:ExitFailure }
    if ($feed.latest.version -ne $Version) { Stop-Release "feed.json 的 latest.version 应为 $Version，实际 $($feed.latest.version)。" $script:ExitFailure }
    if (-not $feed.latest.installer.sha256 -or $feed.latest.installer.sha256.Length -ne 64) { Stop-Release "feed.json 的 latest.installer.sha256 不是 64 位十六进制。" $script:ExitFailure }
    if (-not $feed.latest.installer.edSignature) { Stop-Release "feed.json 缺少 edSignature。" $script:ExitFailure }
    if ($feed.latest.installer.url -ne $InstallerUrl) { Write-Warn "feed.json 的 installer.url（$($feed.latest.installer.url)）与预期（$InstallerUrl）不同，确认 -Repo 给对了。" }
if ($feed.latest.installer.url -like "*feed.json") { Stop-Release "feed.json 的 installer.url 指向了 feed 自己（$($feed.latest.installer.url)）：应该指向安装器 .exe。" $script:ExitFailure }
    $feedGenerated = $true
    Write-Ok "feed：$FeedPath"

    $pk = [regex]::Match(($genOut | Out-String), "publicKey=([A-Za-z0-9+/=]+)")
    if ($pk.Success) {
        $publicKey = $pk.Groups[1].Value
        Write-Info "更新公钥（genfeed 打印）：$publicKey"
    }
    else {
        Write-Warn "genfeed 的 stdout 里没有 publicKey=<b64> 行：应用内校验更新时没有公钥可配（genfeed 侧要打印它）。"
    }
}


# ---------------------------------------------------------------------------
# 7. 清单与下一步
# ---------------------------------------------------------------------------
Write-Header "产物清单（本次版本）"
$artifacts = New-Object System.Collections.Generic.List[object]
foreach ($p in @($OutInstaller, $FeedPath, $notesUsed)) {
    if ($p -and (Test-Path -LiteralPath $p)) {
        $item = Get-Item -LiteralPath $p
        $hash = (Get-FileHash -LiteralPath $p -Algorithm SHA256).Hash.ToLower()
        $artifacts.Add([pscustomobject]@{ Path = $item.FullName; MB = [math]::Round($item.Length / 1MB, 2); SHA256 = $hash })
    }
}
$artifacts | Format-Table -AutoSize | Out-String -Width 200 | Write-Host

Write-Host ""
Write-Ok "本次版本 $Version 的产物就绪。"
if (-not $signingRequested) { Write-Warn "提醒：安装器未签名（降级路径），用户首次运行会看到 SmartScreen 警告。" }
if (-not $feedGenerated) { Write-Warn "提醒：本次没有 feed.json（降级路径），应用内「检查更新」看不到这个版本。" }

Write-Header "下一步（打 tag / 上传）"
Write-Host "  1) 打 tag 并推送（CI 会跑同一套步骤并创建 Release）："
Write-Host "       git tag v$Version"
Write-Host "       git push origin v$Version"
Write-Host "     或手动触发：gh workflow run release.yml -f version=$Version"
Write-Host ""
Write-Host "  2) 只想本地手动上传："
$ghCmd = "gh release create v$Version --title " + [char]34 + "Compositor $Version" + [char]34 + " --notes-file " + [char]34 + $NotesOut + [char]34 + " " + [char]34 + $OutInstaller + [char]34
if ($feedGenerated) { $ghCmd += " " + [char]34 + $FeedPath + [char]34 }
Write-Host "       $ghCmd"
Write-Host ""
Write-Host "  3) CI 需要的 secrets（Settings -> Secrets and variables -> Actions）："
Write-Host "       COMPOSITOR_UPDATE_KEY   base64 的 32 字节 Ed25519 seed（genfeed 用；缺失 = 降级不生成 feed）"
Write-Host "       WINDOWS_CERT_BASE64     PFX 的 base64（缺失 = 降级不签名）"
Write-Host "       WINDOWS_CERT_PASSWORD   PFX 密码"
Write-Host "     证书与私钥绝不写进仓库；.gitignore 已排除 *.pfx / *.p12 / releases/。"
Write-Host ""
Write-Host "  4) 发行后验收（票面验收 1，在干净 VM 上）："
Write-Host ("       pwsh -NoProfile -File scripts/verify-install.ps1 -SetupA <旧版安装器> -SetupB {0}$OutInstaller{0}" -f [char]34)
Write-Host ""
exit 0
