#requires -version 5.1
<#
.SYNOPSIS
    票 50 验收 1：安装 -> 升级 -> 卸载在干净环境验证，并输出可 review 的文本记录。

.DESCRIPTION
    对两个安装器（-SetupA = 旧版，-SetupB = 新版）做同一套动作，每步都在报告里留下
    命令原文 + 退出码 + 观察到的结果：
      0. 工具链 / 输入预检（makensis、两个安装器、管理员权限）；
      1. 在一个干净前缀里静默安装旧版（/S /D=<dir>，/D 必须是最后一个参数且不带引号）；
      2. 断言 Compositor.exe 与 uninstall.exe 存在；
      3. 写探针文件 %APPDATA%\compositor-on-windows\window.json 模拟用户数据
         （internal/bridge 的 WindowStore 就落在那里）；
      4. 静默安装新版（覆盖升级），断言版本资源或 exe 修改时间变新；
      5. 断言用户数据仍在（升级不清用户数据）；
      6. uninstall.exe /S 静默卸载，断言安装目录被清掉；
      7. 观察卸载是否保留用户数据，并把观察结果按实际行为写进记录。

    PASS / FAIL 只给"票面明确要求"的断言；观察性结论写 NOTE，不做断言。
    真实用户数据（%APPDATA%\compositor-on-windows\*）会先备份再恢复，脚本不破坏本机状态。

    注意（诚实性）：本脚本没有被作者在本机跑过 —— 本机没有 makensis，也没有现成安装器。
    缺 makensis / 缺安装器时脚本会给出清晰错误而不是假装通过；报告里写"未执行"就不要当通过。

.PARAMETER SetupA
    旧版安装器路径（必填）。

.PARAMETER SetupB
    新版安装器路径（必填）。

.PARAMETER InstallDir
    干净安装前缀。默认 $env:TEMP\compositor-verify\app。
    注意：NSIS 的 /D= 不能带引号，所以路径里有空格会有问题（默认路径没有空格）。

.PARAMETER ReportPath
    记录文件路径。默认 .\install-verify.txt。

.PARAMETER SetupTimeoutSeconds
    每个安装器/卸载器等待上限，默认 300 秒。

.PARAMETER NoElevate
    不自动提权。默认行为：非管理员时用 Start-Process -Verb RunAs 重新拉起自己
    （安装器要写 HKLM 卸载项，非管理员会静默失败）。

.PARAMETER KeepUserDataBackup
    保留用户数据备份目录（排障用），默认恢复后删除。

.EXAMPLE
    pwsh -NoProfile -File scripts/verify-install.ps1 -SetupA .\Compositor-0.1.0-setup.exe -SetupB .\dist\Compositor-0.2.0-setup.exe
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$SetupA,
    [Parameter(Mandatory = $true)][string]$SetupB,
    [string]$InstallDir = (Join-Path $env:TEMP "compositor-verify\app"),
    [string]$ReportPath = (Join-Path (Get-Location).Path "install-verify.txt"),
    [int]$SetupTimeoutSeconds = 300,
    [switch]$NoElevate,
    [switch]$KeepUserDataBackup,
    # 本机已经装过 Compositor 时，本脚本会按产品名写/删卸载项与快捷方式（NSIS 模板的
    # 行为，不受 /D= 影响），从而破坏那套真实安装。检测到已装就必须显式 -Force 才继续。
    [switch]$Force
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# ---------------------------------------------------------------------------
# 输入预检（放在提权之前：缺文件 / 路径非法要先给清晰错误，不要先弹 UAC）。
# ---------------------------------------------------------------------------
foreach ($s in @($SetupA, $SetupB)) {
    if (-not (Test-Path -LiteralPath $s)) {
        Write-Host "[fail] 安装器不存在：$s" -ForegroundColor Red
        Write-Host "       先构建安装器：pwsh -NoProfile -File scripts\release.ps1 -Version <ver>；本脚本不重新打包。" -ForegroundColor Red
        Write-Host "       结论：未执行（不是验证结果）。" -ForegroundColor Red
        exit 2
    }
}
if (-not [System.IO.Path]::IsPathRooted($InstallDir)) { $InstallDir = [System.IO.Path]::GetFullPath($InstallDir) }
if ($InstallDir -match " ") {
    Write-Host "[fail] 安装前缀含空格：$InstallDir" -ForegroundColor Red
    Write-Host "       NSIS 的 /D= 必须是最后一个参数且不能带引号，带空格路径没法传。换一个无空格目录（-InstallDir）。" -ForegroundColor Red
    Write-Host "       结论：未执行（不是验证结果）。" -ForegroundColor Red
    exit 2
}
# ---------------------------------------------------------------------------
# 隔离性预检：本脚本只把 INSTDIR 指到干净前缀，卸载项与开始菜单/桌面快捷方式仍按
# 产品名写删。本机已装 Compositor 时跑它会破坏真实安装（用户数据有备份，注册表与
# 快捷方式没有），所以要显式 -Force。
# ---------------------------------------------------------------------------
$existing = @()
foreach ($root in @("HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall", "HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall")) {
    if (-not (Test-Path $root)) { continue }
    foreach ($key in Get-ChildItem $root -ErrorAction SilentlyContinue) {
        $name = (Get-ItemProperty -LiteralPath $key.PSPath -ErrorAction SilentlyContinue).DisplayName
        if ($name -and $name -like "*Compositor*") { $existing += "$($key.PSChildName) = $name" }
    }
}
if ($existing.Count -gt 0 -and -not $Force) {
    Write-Host "[fail] 本机已安装 Compositor，本脚本会写/删同名卸载项与快捷方式：" -ForegroundColor Red
    $existing | ForEach-Object { Write-Host "       $_" -ForegroundColor Red }
    Write-Host "       请在干净的 VM/快照里跑，或确认无影响后加 -Force。结论：未执行（不是验证结果）。" -ForegroundColor Red
    exit 2
}

# ---------------------------------------------------------------------------
# 提权：安装器写 HKLM 卸载项，非管理员会失败，所以默认自动提权重跑。
# ---------------------------------------------------------------------------
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$isAdmin = (New-Object Security.Principal.WindowsPrincipal $identity).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    if ($NoElevate) {
        Write-Host "[fail] 非管理员且指定了 -NoElevate：NSIS 安装器要写 HKLM 卸载项，装完也验证不了。请以管理员身份重跑。" -ForegroundColor Red
        exit 2
    }
    Write-Host "[warn] 非管理员：用 Start-Process -Verb RunAs 重新拉起自己（会弹 UAC，输出在提权窗口与报告文件里）。" -ForegroundColor Yellow
    $argv = @(
        "-NoProfile", "-File", $PSCommandPath,
        "-SetupA", $SetupA, "-SetupB", $SetupB,
        "-InstallDir", $InstallDir, "-ReportPath", $ReportPath,
        "-SetupTimeoutSeconds", "$SetupTimeoutSeconds", "-NoElevate"
    )
    if ($KeepUserDataBackup) { $argv += "-KeepUserDataBackup" }
    try {
        $p = Start-Process -FilePath (Get-Process -Id $PID).Path -ArgumentList $argv -Verb RunAs -PassThru -Wait
        Write-Host "[info] 提权实例退出码：$($p.ExitCode)；记录文件：$ReportPath"
        exit $p.ExitCode
    }
    catch {
        Write-Host "[fail] 提权失败：$($_.Exception.Message)" -ForegroundColor Red
        Write-Host "       请手动以管理员身份运行：pwsh -NoProfile -File scripts\verify-install.ps1 ..." -ForegroundColor Red
        exit 2
    }
}

# ---------------------------------------------------------------------------
# 记录器：每一步都记命令原文 + 退出码 + 观察结果。
# ---------------------------------------------------------------------------
$script:Report = New-Object System.Collections.Generic.List[string]
$script:Failures = New-Object System.Collections.Generic.List[string]
$script:Notes = New-Object System.Collections.Generic.List[string]

function Add-Line([string]$Text = "") {
    $script:Report.Add($Text)
    Write-Host $Text
}
function Add-Step([string]$Title, [string]$Command, [int]$Code, [string]$Observed) {
    Add-Line ""
    Add-Line "--- $Title"
    if ($Command) { Add-Line ("    $ " + $Command) }
    if ($Code -ge 0) { Add-Line ("    退出码: " + $Code) }
    if ($Observed) { Add-Line ("    观察  : " + $Observed) }
}
function Add-Pass([string]$Text) { Add-Line "    PASS  : $Text" }
function Add-Warn([string]$Text) { Add-Line "    WARN  : $Text"; $script:Notes.Add($Text) }
function Add-Fail([string]$Text) { Add-Line "    FAIL  : $Text"; $script:Failures.Add($Text) }
function Add-Note([string]$Text) { Add-Line "    NOTE  : $Text"; $script:Notes.Add($Text) }
function Add-Skip([string]$Text) { Add-Line "    SKIP  : $Text" ; $script:Failures.Add("SKIP " + $Text) }

function Get-FileVersionString([string]$Path) {
    try {
        $fi = [System.Diagnostics.FileVersionInfo]::GetVersionInfo($Path)
        if ($fi.FileVersion) { return $fi.FileVersion.Trim() }
        if ($fi.ProductVersion) { return $fi.ProductVersion.Trim() }
        return ""
    }
    catch { return "" }
}

function Get-Sha256([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) { return "" }
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLower()
}

# 跑一个 exe 并等它结束（NSIS：/S 静默，/D=<dir> 必须是最后一个参数且不带引号）。
function Invoke-Setup([string]$Exe, [string[]]$Arguments, [int]$TimeoutSeconds) {
    $argLine = ($Arguments -join " ")
    $outFile = Join-Path $env:TEMP ("compositor-verify-" + [guid]::NewGuid().ToString("N") + ".out.txt")
    $errFile = $outFile + ".err.txt"
    $cmd = "`"$Exe`" $argLine"
    Write-Host "    $ $cmd" -ForegroundColor DarkGray
    $proc = Start-Process -FilePath $Exe -ArgumentList $Arguments -PassThru -RedirectStandardOutput $outFile -RedirectStandardError $errFile
    $timedOut = $false
    if (-not $proc.WaitForExit($TimeoutSeconds * 1000)) {
        $timedOut = $true
        try { $proc.Kill() } catch { }
        $proc.WaitForExit(30000) | Out-Null
    }
    $stdout = ""
    $stderr = ""
    if (Test-Path -LiteralPath $outFile) { $stdout = (Get-Content -LiteralPath $outFile -Raw) }
    if (Test-Path -LiteralPath $errFile) { $stderr = (Get-Content -LiteralPath $errFile -Raw) }
    Remove-Item -LiteralPath $outFile, $errFile -Force -ErrorAction SilentlyContinue
    $code = if ($timedOut) { -1 } else { $proc.ExitCode }
    return [pscustomobject]@{
        Command   = $cmd
        ExitCode  = $code
        TimedOut  = $timedOut
        StdOut    = $stdout
        StdErr    = $stderr
    }
}

# ---------------------------------------------------------------------------
# 预检
# ---------------------------------------------------------------------------
$startedAt = (Get-Date).ToString("yyyy-MM-dd HH:mm:ss")
$hostName = $env:COMPUTERNAME
$InstallDir = [System.IO.Path]::GetFullPath($InstallDir)
if (-not [System.IO.Path]::IsPathRooted($ReportPath)) { $ReportPath = [System.IO.Path]::GetFullPath($ReportPath) }

# 用户数据目录与探针文件：internal/bridge/windowstate.go 用 os.UserConfigDir()，Windows
# 上就是 %APPDATA%compositor-on-windows。必须在表头之前定义 —— Set-StrictMode 下引用
# 未定义变量会直接终止脚本（曾因此在第一屏就退出，备份/恢复都没跑到）。
$userDataDir = Join-Path $env:APPDATA "compositor-on-windows"
$userDataFile = Join-Path $userDataDir "window.json"

Add-Line "======================================================================"
Add-Line "Compositor 安装 -> 升级 -> 卸载 验证记录（票 50 验收 1）"
Add-Line "======================================================================"
Add-Line "时间        : $startedAt"
Add-Line "机器        : $hostName（$([Environment]::OSVersion.VersionString)）"
Add-Line "管理员      : $isAdmin"
Add-Line "安装前缀    : $InstallDir"
Add-Line "用户数据探针: $userDataFile"
Add-Line "旧版安装器  : $SetupA"
Add-Line "新版安装器  : $SetupB"

$aInfo = Get-Item -LiteralPath $SetupA
$bInfo = Get-Item -LiteralPath $SetupB
$setupAVer = Get-FileVersionString $aInfo.FullName
$setupBVer = Get-FileVersionString $bInfo.FullName
Add-Line "----------------------------------------------------------------------"
Add-Line "预检（本脚本只跑安装器，不重新打包）"
Add-Line "----------------------------------------------------------------------"
$makensis = Get-Command makensis -ErrorAction SilentlyContinue
if ($makensis) { Add-Line "  [ ok ] makensis: $($makensis.Source)" }
else { Add-Line "  [warn] 找不到 makensis：本脚本不需要它；没装安装器时先跑 scripts\release.ps1（那个需要 NSIS）。" }
Add-Line "  [ ok ] A: $($aInfo.Name)  $([math]::Round($aInfo.Length / 1MB, 2)) MB  版本资源=$setupAVer  sha256=$(Get-Sha256 $aInfo.FullName)"
Add-Line "  [ ok ] B: $($bInfo.Name)  $([math]::Round($bInfo.Length / 1MB, 2)) MB  版本资源=$setupBVer  sha256=$(Get-Sha256 $bInfo.FullName)"

# 备份真实用户数据，脚本结束（含提前失败）时恢复（$userDataDir 已在表头之前定义）。
$backupDir = Join-Path $env:TEMP ("compositor-verify-userdata-" + [guid]::NewGuid().ToString("N"))
$hadUserData = Test-Path -LiteralPath $userDataDir
if ($hadUserData) {
    New-Item -ItemType Directory -Path $backupDir -Force | Out-Null
    Copy-Item -LiteralPath $userDataDir -Destination (Join-Path $backupDir "compositor-on-windows") -Recurse -Force
    Add-Line "  [info] 已备份真实用户数据：$userDataDir -> $backupDir"
}

function Restore-UserData {
    if ($script:RestoreDone) { return }
    $script:RestoreDone = $true
    if (Test-Path -LiteralPath $userDataDir) { Remove-Item -LiteralPath $userDataDir -Recurse -Force -ErrorAction SilentlyContinue }
    if ($hadUserData) {
        Copy-Item -LiteralPath (Join-Path $backupDir "compositor-on-windows") -Destination $userDataDir -Recurse -Force -ErrorAction SilentlyContinue
        Add-Line "  [info] 已恢复真实用户数据：$userDataDir"
    }
    if (-not $KeepUserDataBackup) { Remove-Item -LiteralPath $backupDir -Recurse -Force -ErrorAction SilentlyContinue }
    else { Add-Line "  [info] 保留备份目录（-KeepUserDataBackup）：$backupDir" }
}

function Invoke-Step([string]$Title, [string]$Exe, [string[]]$Arguments) {
    $res = Invoke-Setup -Exe $Exe -Arguments $Arguments -TimeoutSeconds $SetupTimeoutSeconds
    Add-Step -Title $Title -Command $res.Command -Code $res.ExitCode -Observed ""
    if ($res.StdOut) { Add-Line ("    stdout: " + ($res.StdOut.Trim())) }
    if ($res.StdErr) { Add-Line ("    stderr: " + ($res.StdErr.Trim())) }
    return $res
}

try {
    # ---------------- 步骤 1：干净前缀 + 静默安装旧版 ----------------
    Add-Line ""
    Add-Line "----------------------------------------------------------------------"
    Add-Line "步骤 1：静默安装旧版到干净前缀"
    Add-Line "----------------------------------------------------------------------"
    if (Test-Path -LiteralPath $InstallDir) {
        Remove-Item -LiteralPath $InstallDir -Recurse -Force
        Add-Line "    清理旧前缀：$InstallDir"
    }
    $resA = Invoke-Step "静默安装旧版（/S /D=$InstallDir）" $SetupA @("/S", "/D=$InstallDir")
    if ($resA.TimedOut) { Add-Fail "旧版安装超时（$SetupTimeoutSeconds 秒）" }
    elseif ($resA.ExitCode -ne 0) { Add-Fail "旧版安装退出码非 0：$($resA.ExitCode)" }
    else { Add-Pass "旧版安装退出码 0" }

    # ---------------- 步骤 2：断言安装结果 ----------------
    Add-Line ""
    Add-Line "----------------------------------------------------------------------"
    Add-Line "步骤 2：断言 exe 与 uninstall.exe 存在"
    Add-Line "----------------------------------------------------------------------"
    $appExe = Join-Path $InstallDir "Compositor.exe"
    $uninstExe = Join-Path $InstallDir "uninstall.exe"
    if (Test-Path -LiteralPath $appExe) { Add-Pass "存在 $appExe" } else { Add-Fail "缺少 $appExe" }
    if (Test-Path -LiteralPath $uninstExe) { Add-Pass "存在 $uninstExe" } else { Add-Fail "缺少 $uninstExe" }
    if (Test-Path -LiteralPath $InstallDir) {
        $listing = (Get-ChildItem -LiteralPath $InstallDir | Select-Object -ExpandProperty Name) -join ", "
        Add-Note "安装目录内容：$listing"
    }
    $verA = if (Test-Path -LiteralPath $appExe) { Get-FileVersionString $appExe } else { "" }
    $fileTimeA = if (Test-Path -LiteralPath $appExe) { (Get-Item -LiteralPath $appExe).LastWriteTimeUtc.ToString("o") } else { "" }
    $hashA = Get-Sha256 $appExe
    if ($verA) { Add-Note "旧版 Compositor.exe 版本资源：$verA" } else { Add-Note "旧版 Compositor.exe 没有版本资源（或读不到）。" }

    # ---------------- 步骤 3：写用户数据探针 ----------------
    Add-Line ""
    Add-Line "----------------------------------------------------------------------"
    Add-Line "步骤 3：写用户数据探针（模拟用户在应用里留下的状态）"
    Add-Line "----------------------------------------------------------------------"
    if (-not (Test-Path -LiteralPath $userDataDir)) { New-Item -ItemType Directory -Path $userDataDir -Force | Out-Null }
    $probe = "compositor-verify-probe-A"
    $probeJson = "{ " + [char]34 + "probe" + [char]34 + ": " + [char]34 + $probe + [char]34 + ", " + [char]34 + "writtenBy" + [char]34 + ": " + [char]34 + "scripts/verify-install.ps1" + [char]34 + " }"
    Set-Content -LiteralPath $userDataFile -Value $probeJson -Encoding UTF8
    Add-Line "    写入探针：$userDataFile"
    if ((Test-Path -LiteralPath $userDataFile) -and ((Get-Content -LiteralPath $userDataFile -Raw) -match $probe)) { Add-Pass "探针文件已写入" } else { Add-Fail "探针文件写入失败" }

    # ---------------- 步骤 4：静默安装新版（覆盖升级） ----------------
    Add-Line ""
    Add-Line "----------------------------------------------------------------------"
    Add-Line "步骤 4：静默安装新版（覆盖升级）"
    Add-Line "----------------------------------------------------------------------"
    $resB = Invoke-Step "静默安装新版（/S /D=$InstallDir）" $SetupB @("/S", "/D=$InstallDir")
    if ($resB.TimedOut) { Add-Fail "新版安装超时（$SetupTimeoutSeconds 秒）" }
    elseif ($resB.ExitCode -ne 0) { Add-Fail "新版安装退出码非 0：$($resB.ExitCode)" }
    else { Add-Pass "新版安装退出码 0（覆盖升级）" }

    $verB = if (Test-Path -LiteralPath $appExe) { Get-FileVersionString $appExe } else { "" }
    $fileTimeB = if (Test-Path -LiteralPath $appExe) { (Get-Item -LiteralPath $appExe).LastWriteTimeUtc.ToString("o") } else { "" }
    $hashB = Get-Sha256 $appExe
    Add-Line ""
    Add-Line "步骤 4b：升级后的版本 / 时间戳断言"
    Add-Line "    旧: 版本=$verA  写入=$fileTimeA  sha256=$hashA"
    Add-Line "    新: 版本=$verB  写入=$fileTimeB  sha256=$hashB"
    $versionChanged = ($verA -ne $verB)
    $timeChanged = ($fileTimeA -ne $fileTimeB)
    $hashChanged = ($hashA -ne $hashB)
    if ($versionChanged) { Add-Pass "Compositor.exe 版本资源变了（$verA -> $verB）" }
    elseif ($timeChanged) { Add-Pass "Compositor.exe 修改时间变新（版本资源都是 $verB）：$fileTimeA -> $fileTimeB" }
    elseif ($hashChanged) { Add-Pass "Compositor.exe 内容变了（sha256 不同）；版本资源与时间戳都没变" }
    else { Add-Fail "升级后 Compositor.exe 完全没变（版本 / 时间 / 内容都相同）—— 覆盖升级没有生效" }
    if ($timeChanged -and -not $versionChanged) { Add-Note "时间戳变了但版本资源没变：确认安装器内的 INFO_PRODUCTVERSION 是否随 tag 更新。" }

    # ---------------- 步骤 5：用户数据仍在 ----------------
    Add-Line ""
    Add-Line "----------------------------------------------------------------------"
    Add-Line "步骤 5：断言升级没有清掉用户数据"
    Add-Line "----------------------------------------------------------------------"
    if ((Test-Path -LiteralPath $userDataFile) -and ((Get-Content -LiteralPath $userDataFile -Raw) -match $probe)) {
        Add-Pass "升级后探针仍在且内容未变：$userDataFile"
    }
    else {
        Add-Fail "升级后探针丢失或被改写：$userDataFile"
    }

    # ---------------- 步骤 6：静默卸载 ----------------
    Add-Line ""
    Add-Line "----------------------------------------------------------------------"
    Add-Line "步骤 6：静默卸载并断言安装目录被清掉"
    Add-Line "----------------------------------------------------------------------"
    if (Test-Path -LiteralPath $uninstExe) {
        $resU = Invoke-Step "静默卸载（uninstall.exe /S）" $uninstExe @("/S")
        if ($resU.TimedOut) { Add-Fail "卸载超时（$SetupTimeoutSeconds 秒）" }
        elseif ($resU.ExitCode -ne 0) { Add-Fail "卸载退出码非 0：$($resU.ExitCode)" }
        else { Add-Pass "卸载退出码 0" }

        $gone = $false
        for ($i = 1; $i -le 10; $i++) {
            if (-not (Test-Path -LiteralPath $InstallDir)) { $gone = $true; break }
            Start-Sleep -Milliseconds 500
        }
        if ($gone) { Add-Pass "安装目录已删除：$InstallDir" }
        else {
            $left = (Get-ChildItem -LiteralPath $InstallDir -Recurse -ErrorAction SilentlyContinue | Select-Object -ExpandProperty FullName) -join ", "
            Add-Fail "卸载后安装目录仍在：$InstallDir（残留：$left）"
        }
    }
    else {
        Add-Skip "没有 uninstall.exe（步骤 2 已报过），跳过卸载。"
    }

    # ---------------- 步骤 7：卸载是否保留用户数据（观察，不断言） ----------------
    Add-Line ""
    Add-Line "----------------------------------------------------------------------"
    Add-Line "步骤 7：观察卸载是否保留用户数据（按实际行为记录，不作为通过条件）"
    Add-Line "----------------------------------------------------------------------"
    $userDataSurvivedUninstall = $false
    if (Test-Path -LiteralPath $userDataFile) {
        $userDataSurvivedUninstall = ((Get-Content -LiteralPath $userDataFile -Raw) -match $probe)
    }
    if ($userDataSurvivedUninstall) {
        Add-Note ('卸载保留了用户数据（' + $userDataFile + ' 仍在）。当前 NSIS 模板 build/windows/installer/project.nsi 只删了安装目录与 WebView2 数据目录，没删 APPDATA/compositor-on-windows，所以保留是预期行为；如果产品决定卸载即清干净，要改的是 NSIS 模板，不是本脚本。')
    }
    elseif (Test-Path -LiteralPath $userDataDir) {
        Add-Note "用户数据目录还在但探针文件没了：$userDataDir"
    }
    else {
        Add-Note "卸载删掉了用户数据目录：$userDataDir（观察结果，非断言）。"
    }
}
finally {
    Restore-UserData
}

# ---------------------------------------------------------------------------
# 记录落盘
# ---------------------------------------------------------------------------
$finishedAt = (Get-Date).ToString("yyyy-MM-dd HH:mm:ss")
Add-Line ""
Add-Line "======================================================================"
Add-Line "结论"
Add-Line "======================================================================"
$setupNameA = Split-Path -Leaf $SetupA
$setupNameB = Split-Path -Leaf $SetupB
Add-Line "旧版: $setupNameA（版本资源 $setupAVer，sha256 $(Get-Sha256 $SetupA)）"
Add-Line "新版: $setupNameB（版本资源 $setupBVer，sha256 $(Get-Sha256 $SetupB)）"
if ($script:Failures.Count -eq 0) {
    Add-Line "RESULT: PASS — 安装 / 覆盖升级 / 卸载 6 项断言全通过。"
}
else {
    Add-Line "RESULT: FAIL — $($script:Failures.Count) 项没通过："
    foreach ($f in $script:Failures) { Add-Line "  * $f" }
}
if ($script:Notes.Count -gt 0) {
    Add-Line ""
    Add-Line "观察 / 说明（不是断言）："
    foreach ($n in $script:Notes) { Add-Line "  - $n" }
}
Add-Line ""
Add-Line "结束时间: $finishedAt"
Add-Line ""
Add-Line "怎么算通过（人工复核这张记录时照这个看）："
Add-Line "  1. 两条安装命令的退出码都是 0；"
Add-Line "  2. 安装目录里 Compositor.exe 与 uninstall.exe 都存在；"
Add-Line "  3. 升级后 Compositor.exe 的版本资源或修改时间变新（两者都没变就是覆盖升级没生效）；"
Add-Line "  4. 升级后 %APPDATA%\compositor-on-windows\window.json 探针仍在（升级不清用户数据）；"
Add-Line "  5. 卸载退出码 0，且安装目录被删掉；"
Add-Line "  6. 卸载是否保留用户数据按上面 NOTE 记录的实际情况判断（产品自定语义，本脚本只如实记录）。"
Add-Line ""
Add-Line "本脚本不做的事：不重新打包安装器、不测应用内检查更新（那是 internal/update 的测试）、"
Add-Line "不测代码签名（签名验证要在装了证书的机器上用 signtool verify /pa 另测）。"

$reportText = ($script:Report -join [Environment]::NewLine) + [Environment]::NewLine
$reportDir = Split-Path -Parent $ReportPath
if ($reportDir -and -not (Test-Path -LiteralPath $reportDir)) { New-Item -ItemType Directory -Path $reportDir -Force | Out-Null }
Set-Content -LiteralPath $ReportPath -Value $reportText -Encoding UTF8

Write-Host ""
Write-Host "记录已写出：$ReportPath" -ForegroundColor Cyan
if ($script:Failures.Count -eq 0) {
    Write-Host "RESULT: PASS" -ForegroundColor Green
    exit 0
}
Write-Host "RESULT: FAIL（$($script:Failures.Count) 项）" -ForegroundColor Red
exit 1