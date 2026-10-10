#requires -version 5.1
<#
.SYNOPSIS
    票 52：启动到就绪（launch-to-ready）基准 —— 与 Swift 版 LAUNCH_TO_READY_MEDIAN 同名同语义。

.DESCRIPTION
    对齐 reference/Swift/CompositorUITests/CompositorUITests.swift:38-52 的 testLaunchPerformance：
    连续启动应用 N 次（默认 5），每次测"进程启动 -> 界面就绪"，最后打印
    LAUNCH_TO_READY_SECONDS: [...] 与 LAUNCH_TO_READY_MEDIAN: <中位数>。

    与原版的差异（诚实记录，别当成同一个数）：
      * 原版是 XCTest/XCUIApplication 从测试进程外部测，包含 XCTest 启动/空闲/无障碍开销，
        而且是在 app.launch() 返回且欢迎按钮可见时停表；
      * 本脚本测的是"应用自己报的就绪时刻"：前端第一次拿到快照并认为界面就绪后调绑定的
        ReportReady()，Go 侧往 stdout 打印一行 LAUNCH_TO_READY_SECONDS: <秒，3 位小数>。
      指标名与"取中位数"的语义对齐；绝对值不能直接跨平台比。

    契约（由 Go/Svelte 侧实现，本脚本只消费）：
      stdout 里出现一行  LAUNCH_TO_READY_SECONDS: 1.234
      进程打印后继续正常运行（不退出），所以拿到数就由本脚本结束它。

    诚实性：WebView2 需要交互式桌面会话。CI 的 headless runner 起不来 WebView2，
    这里不会编造样本 —— 拿不到就绪标记就报"第 k 次没等到就绪标记"并建议在交互式
    桌面会话里跑；一个样本都没有时退出码为 2，记录文件里也不会写入任何测量值。

.PARAMETER Runs
    启动次数（样本数上限）。默认 5，与原版 Swift 基准一致。

.PARAMETER Exe
    被测可执行文件。默认 <仓库根>\build\bin\Compositor.exe（wails build 的产物）。

.PARAMETER Out
    把本次运行的文本记录（含真实 stdout）另存到该文件。默认不写。

.PARAMETER Record
    把本次结果追加到 docs/benchmarks/launch.json（趋势文件，JSON 数组）。

.PARAMETER SkipBuild
    不自动构建：exe 不存在时直接报错退出，不跑 wails build。

.PARAMETER TimeoutSeconds
    单次启动等待就绪标记的上限秒数。默认 30。

.PARAMETER Force
    在检测到无交互式桌面会话（CI / Session 0）时仍然尝试。默认直接报错退出。

.EXAMPLE
    pwsh -NoProfile -File scripts/launchbench.ps1
    pwsh -NoProfile -File scripts/launchbench.ps1 -SkipBuild -Runs 3 -Record
#>
[CmdletBinding()]
param(
    [int]$Runs = 5,
    [string]$Exe = "",
    [string]$Out = "",
    [switch]$Record,
    [switch]$SkipBuild,
    [int]$TimeoutSeconds = 30,
    [switch]$Force
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# ---------------------------------------------------------------------------
# 常量与路径（仓库根 = 本脚本上一级目录）
# ---------------------------------------------------------------------------
$ReadyMarker = "LAUNCH_TO_READY_SECONDS:"
$scriptRoot = $PSScriptRoot
if (-not $scriptRoot) { $scriptRoot = (Get-Location).Path }
$repoRoot = Split-Path -Parent $scriptRoot
$trendPath = Join-Path $repoRoot "docs\benchmarks\launch.json"
$defaultExe = Join-Path $repoRoot "build\bin\Compositor.exe"

if (-not $Exe) { $Exe = $defaultExe }
if (-not [System.IO.Path]::IsPathRooted($Exe)) { $Exe = [System.IO.Path]::GetFullPath($Exe) }
if ($Out -and -not [System.IO.Path]::IsPathRooted($Out)) { $Out = [System.IO.Path]::GetFullPath($Out) }

$script:Report = New-Object System.Collections.Generic.List[string]
function Add-Line([string]$Text = "") {
    $script:Report.Add($Text)
    Write-Host $Text
}

function Get-Median([double[]]$Values) {
    if (-not $Values -or $Values.Count -eq 0) { return $null }
    $sorted = @($Values | Sort-Object)
    $n = $sorted.Count
    if ($n % 2 -eq 1) { return [double]$sorted[[int](($n - 1) / 2)] }
    return [double](($sorted[$n / 2 - 1] + $sorted[$n / 2]) / 2)
}

function Get-MachineInfo {
    $os = ""
    $cpu = ""
    $memoryBytes = 0
    try { $os = (Get-CimInstance Win32_OperatingSystem -ErrorAction Stop).Caption } catch { $os = [Environment]::OSVersion.VersionString }
    try {
        $cpu = (Get-CimInstance Win32_Processor -ErrorAction Stop | Select-Object -First 1).Name
    }
    catch { $cpu = $env:PROCESSOR_IDENTIFIER }
    if (-not $cpu) { $cpu = "unknown" }
    try {
        $memoryBytes = [int64](Get-CimInstance Win32_ComputerSystem -ErrorAction Stop).TotalPhysicalMemory
    }
    catch { $memoryBytes = 0 }
    return [pscustomobject]@{ Os = $os; Cpu = $cpu; MemoryBytes = $memoryBytes }
}

# 无交互式桌面会话的探测：CI 的 runner 是 Session 0 / 无桌面，WebView2 起不来。
function Test-HeadlessSession {
    $reasons = @()
    if ($env:CI) { $reasons += "环境变量 CI=$($env:CI)" }
    if ($env:GITHUB_ACTIONS) { $reasons += "GITHUB_ACTIONS=$($env:GITHUB_ACTIONS)" }
    if ($env:TF_BUILD) { $reasons += "TF_BUILD=$($env:TF_BUILD)" }
    $session = $env:SESSIONNAME
    # "Services" 是 Session 0；纯数字会话名也不是可交互桌面会话（RDP 会话名形如 RDP-Tcp#0）。
    if ($session -and ($session -ieq "Services" -or $session -match "^\d+$")) {
        $reasons += "SESSIONNAME=$session"
    }
    return $reasons
}

# 把可读写的 FileStream 从头读成文本（不关闭流，调用方负责 Dispose）。
function Read-StreamText($Stream) {
    if (-not $Stream) { return "" }
    try {
        $Stream.Flush()
        $Stream.Position = 0
        $reader = New-Object System.IO.StreamReader($Stream, [System.Text.Encoding]::UTF8, $true, 4096, $true)
        $text = $reader.ReadToEnd()
        $reader.Dispose()
        return [string]$text
    }
    catch { return "" }
}

# 启动一次应用并等就绪标记；返回对象里保留真实 stdout，便于记录与排障。
function Invoke-LaunchOnce {
    param(
        [string]$ExePath,
        [int]$Index,
        [int]$TimeoutSeconds
    )
    $result = [pscustomobject]@{
        Index     = $Index
        Success   = $false
        Seconds   = $null
        Reason    = ""
        ExitCode  = $null
        StdOut    = ""
        StdErr    = ""
        ElapsedMs = 0
    }
    $proc = $null
    $lineTask = $null
    $errTask = $null
    $killed = $false
    try {
        $psi = New-Object System.Diagnostics.ProcessStartInfo
        $psi.FileName = $ExePath
        $psi.UseShellExecute = $false
        $psi.CreateNoWindow = $true
        $psi.RedirectStandardOutput = $true
        $psi.RedirectStandardError = $true
        # 用 .NET Process 而不是 Start-Process：Start-Process 的 -Redirect* 只收路径、句柄由它
        # 自己管（父进程删不掉临时文件），也没法干净地整树结束子进程。
        $proc = New-Object System.Diagnostics.Process
        $proc.StartInfo = $psi

        # 进程启动时刻（前端要测的就是从这一刻到 ReportReady 的墙钟时间）。
        $sw = [System.Diagnostics.Stopwatch]::StartNew()
        if (-not $proc.Start()) {
            $result.Reason = "第 $Index 次启动进程失败：Process.Start 返回 false"
            return $result
        }

        # 应用打印就绪标记后继续运行（stdout 不关闭），所以：
        #   * 不能同步 ReadToEnd —— 会一直阻塞到进程退出；
        #   * 不能 BeginOutputReadLine —— 回调跑在线程池线程上没有 Runspace，脚本块会抛异常；
        #   * 也不能只 ReadToEndAsync —— 它同样要等管道关闭（即进程退出）才完成。
        # 这里按行异步读，每次只等一小段，主线程继续轮询超时/进程退出。
        $stdout = $proc.StandardOutput
        $stderr = $proc.StandardError
        $errTask = $stderr.ReadToEndAsync()
        $lines = New-Object System.Collections.Generic.List[string]
        $lineTask = $stdout.ReadLineAsync()
        $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
        $hit = $null
        $pendingLine = $true
        while ((Get-Date) -lt $deadline) {
            if ($pendingLine) {
                if ($lineTask.Wait(100)) {
                    $line = $null
                    try { $line = [string]$lineTask.Result } catch { $line = $null }
                    if ($null -eq $line) { $pendingLine = $false }
                    else {
                        $lines.Add($line)
                        $m = [regex]::Match($line, [regex]::Escape($ReadyMarker) + "\s*([0-9]+(?:\.[0-9]+)?)")
                        if ($m.Success) { $hit = $m; break }
                        $lineTask = $stdout.ReadLineAsync()
                    }
                }
            }
            $exited = $false
            try { $exited = $proc.HasExited } catch { $exited = $false }
            if ($exited) { break }
            if (-not $pendingLine) { Start-Sleep -Milliseconds 50 }
        }
        $result.ElapsedMs = [int]$sw.ElapsedMilliseconds

        if ($hit) {
            $result.Seconds = [double]$hit.Groups[1].Value
            $result.Success = $true
        }
        else {
            try { $exited = $proc.HasExited } catch { $exited = $true }
            if ($exited) {
                try { $result.ExitCode = $proc.ExitCode } catch { }
                $result.Reason = "进程在报就绪之前就退出了（退出码 $($result.ExitCode)）"
            }
            else {
                $result.Reason = "第 $Index 次没等到就绪标记（$TimeoutSeconds s 超时）"
            }
        }
    }
    catch {
        $result.Reason = "第 $Index 次启动进程失败：$($_.Exception.Message)"
    }
    finally {
        # 只结束本次拿到的那个进程（及其子进程树），绝不按名字 kill 别的实例。
        # 子进程也要收：Wails 的 WebView2 渲染进程会占住用户数据目录，只杀主进程会让后续启动受影响。
        if ($proc) {
            $exited = $false
            try { $exited = $proc.HasExited } catch { $exited = $true }
            if (-not $exited) {
                $killed = $true
                try { $proc.Kill($true) } catch { try { $proc.Kill() } catch { } }
            }
            # 进程结束、管道关闭后异步读取任务才会完成。
            try { if ($lineTask) { $null = $lineTask.Wait(5000) } } catch { }
            try { if ($errTask) { $null = $errTask.Wait(5000) } } catch { }
            # 被结束的进程可能还有最后几行没读出来，补读一次。
            if ($lineTask -and $lineTask.IsCompleted) {
                try {
                    $tail = [string]$lineTask.Result
                    if ($null -ne $tail) { $lines.Add($tail) }
                }
                catch { }
            }
            $result.StdOut = [string]($lines -join [Environment]::NewLine)
            try { if ($errTask -and $errTask.IsCompleted) { $result.StdErr = [string]$errTask.Result } } catch { }
            try { $proc.Dispose() } catch { }
        }
    }
    if ($killed -and -not $result.Success) {
        # 超时也要如实说明：进程是被本脚本结束的，不是自己退出的。
        if ($result.Reason) { $result.Reason = $result.Reason + "；进程已由本脚本结束" }
    }
    return $result
}

function Write-ReportFile([string]$Path) {
    if (-not $Path) { return }
    $dir = Split-Path -Parent $Path
    if ($dir -and -not (Test-Path -LiteralPath $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null }
    Set-Content -LiteralPath $Path -Value (($script:Report -join [Environment]::NewLine) + [Environment]::NewLine) -Encoding UTF8
    Write-Host "记录已写出：$Path" -ForegroundColor Cyan
}

# ---------------------------------------------------------------------------
# 预检
# ---------------------------------------------------------------------------
if ($Runs -lt 1) {
    Write-Host "[fail] -Runs 必须 >= 1（收到 $Runs）。" -ForegroundColor Red
    exit 2
}

# 注意 @(...)：函数只返回一项时 PowerShell 会拆包成字符串，Set-StrictMode 下 .Count 会报错。
$headless = @(Test-HeadlessSession)
if ($headless.Count -gt 0 -and -not $Force) {
    Write-Host "[fail] 检测到没有交互式桌面会话，WebView2 大概率起不来，不会产出可信样本：" -ForegroundColor Red
    foreach ($r in $headless) { Write-Host "       - $r" -ForegroundColor Red }
    Write-Host "       本项基准必须在交互式桌面会话里跑（本机登录后的普通终端窗口，或 RDP 会话）。" -ForegroundColor Red
    Write-Host "       CI 的 headless runner 上这一项是'尽力而为'，真值看 docs/benchmarks/launch.md。" -ForegroundColor Red
    Write-Host "       确实要在这种会话里试：加 -Force（拿不到样本时脚本不会编造，退出码为 2）。" -ForegroundColor Red
    exit 2
}

if (-not (Test-Path -LiteralPath $Exe)) {
    if ($SkipBuild) {
        Write-Host "[fail] 找不到 $Exe，且指定了 -SkipBuild。" -ForegroundColor Red
        Write-Host "       先构建：wails build（CLI 在 go env GOPATH 下的 bin\wails.exe）。" -ForegroundColor Red
        exit 2
    }
    $wails = Join-Path (go env GOPATH) "bin\wails.exe"
    if (-not (Test-Path -LiteralPath $wails)) {
        Write-Host "[fail] 找不到 exe（$Exe），也找不到 wails CLI（$wails）。" -ForegroundColor Red
        Write-Host "       先装 wails：go install github.com/wailsapp/wails/v2/cmd/wails@latest" -ForegroundColor Red
        exit 2
    }
    Write-Host "[info] 未找到 $Exe，先跑 wails build ..." -ForegroundColor Cyan
    Push-Location $repoRoot
    try {
        & $wails build
        $buildCode = $LASTEXITCODE
    }
    finally {
        Pop-Location
    }
    if ($buildCode -ne 0) {
        Write-Host "[fail] wails build 退出码 $buildCode；没有可测产物。" -ForegroundColor Red
        exit 1
    }
    if (-not (Test-Path -LiteralPath $Exe)) {
        Write-Host "[fail] wails build 退出码 0，但仍找不到 $Exe。" -ForegroundColor Red
        exit 1
    }
}

# ---------------------------------------------------------------------------
# 跑基准
# ---------------------------------------------------------------------------
$machine = Get-MachineInfo
$exeItem = Get-Item -LiteralPath $Exe
$startedAt = Get-Date
$invariant = [System.Globalization.CultureInfo]::InvariantCulture

Add-Line "======================================================================"
Add-Line "Compositor 启动到就绪基准（票 52；指标名对齐 Swift LAUNCH_TO_READY_MEDIAN）"
Add-Line "======================================================================"
Add-Line "时间        : $($startedAt.ToString('yyyy-MM-dd HH:mm:ss zzz'))"
Add-Line "被测程序    : $($exeItem.FullName)"
Add-Line "程序写入时间: $($exeItem.LastWriteTime.ToString('yyyy-MM-dd HH:mm:ss'))"
Add-Line "程序大小    : $([math]::Round($exeItem.Length / 1MB, 2)) MB"
Add-Line "机器        : $($machine.Os)"
Add-Line "CPU         : $($machine.Cpu)"
Add-Line "内存        : $([math]::Round($machine.MemoryBytes / 1GB, 2)) GiB ($($machine.MemoryBytes) bytes)"
Add-Line "次数        : $Runs（单次超时 $TimeoutSeconds s）"
Add-Line "会话        : SESSIONNAME=$($env:SESSIONNAME)"
Add-Line "----------------------------------------------------------------------"

$samples = New-Object System.Collections.Generic.List[double]
for ($i = 1; $i -le $Runs; $i++) {
    $r = Invoke-LaunchOnce -ExePath $Exe -Index $i -TimeoutSeconds $TimeoutSeconds
    if ($r.Success) {
        $samples.Add([double]$r.Seconds)
        Add-Line ("  第 {0} 次: {1:N3} s" -f $i, $r.Seconds)
    }
    else {
        Add-Line ("  第 {0} 次: 失败 —— {1}" -f $i, $r.Reason)
        if ($r.StdErr) { Add-Line ("            stderr: " + ($r.StdErr.Trim())) }
        if ($r.StdOut) { Add-Line ("            stdout: " + (($r.StdOut.Trim()) -replace "[\r\n]+", " | ")) }
    }
}

Add-Line "----------------------------------------------------------------------"
if ($samples.Count -eq 0) {
    Add-Line "[fail] $Runs 次启动一个就绪标记都没拿到，没有样本。"
    Add-Line "       常见原因：无交互式桌面会话（CI/headless）时 WebView2 起不来；或构建产物里还没有"
    Add-Line "       ReportReady() / LAUNCH_TO_READY_SECONDS 这段契约（本脚本只消费，不实现）。"
    Add-Line "       建议：在交互式桌面会话里跑，并先确认应用能正常打开。绝不把这种结果记成一次测量。"
    Write-ReportFile $Out
    exit 2
}

$median = Get-Median -Values $samples.ToArray()
$listText = ($samples | ForEach-Object { $_.ToString("0.000", $invariant) }) -join ", "
Add-Line "LAUNCH_TO_READY_SECONDS: [$listText]"
Add-Line "LAUNCH_TO_READY_MEDIAN: $($median.ToString('0.000', $invariant))"
Add-Line "样本数: $($samples.Count)/$Runs（没等到标记的次数不计入样本）"

# ---------------------------------------------------------------------------
# 趋势记录（追加到 docs/benchmarks/launch.json）
# ---------------------------------------------------------------------------
if ($Record) {
    # 手工拼 JSON 而不是 ConvertTo-Json：字段顺序、缩进、数字格式都不随 PowerShell 版本变，
    # 趋势文件才能稳定 diff。所有数字用 InvariantCulture，避免逗号小数点。
    $ci = $invariant
    $sampleText = ($samples | ForEach-Object { ([double]$_).ToString("0.000", $ci) }) -join ", "
    # 一条记录一行：追加式文件不会因为重排缩进而产生巨大 diff，也便于 grep。
    $newRecord = '{' +
        '"timestamp":' + (ConvertTo-Json -InputObject $startedAt.ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ") -Compress) + ',' +
        '"runs":' + $samples.Count + ',' +
        '"samples":[' + $sampleText + '],' +
        '"medianSeconds":' + ([double]$median).ToString("0.000", $ci) + ',' +
        '"os":' + (ConvertTo-Json -InputObject ([string]$machine.Os) -Compress) + ',' +
        '"cpu":' + (ConvertTo-Json -InputObject ([string]$machine.Cpu) -Compress) + ',' +
        '"memoryBytes":' + [int64]$machine.MemoryBytes +
        '}'

    # 先收集每条记录的 JSON 文本，再用逗号连接成数组 —— 直接拼行会漏掉分隔逗号。
    $recordLines = New-Object System.Collections.Generic.List[string]
    if (Test-Path -LiteralPath $trendPath) {
        $existingRaw = [string](Get-Content -LiteralPath $trendPath -Raw)
        if ($existingRaw -and $existingRaw.Trim()) {
            try {
                # 解析一次既做校验、也做规范化（老记录无论是单行还是多行都收敛成稳定格式）。
                $existing = @(ConvertFrom-Json -InputObject $existingRaw)
            }
            catch {
                Write-Host "[fail] $trendPath 不是合法 JSON，为免覆盖历史记录，本次不写入：$($_.Exception.Message)" -ForegroundColor Red
                exit 1
            }
            foreach ($rec in $existing) {
                # ConvertTo-Json -Compress 在 PowerShell 7 上仍可能带换行（例如数组元素），压成一行。
                $oneLine = [string](ConvertTo-Json -InputObject $rec -Depth 6 -Compress)
                $oneLine = ($oneLine -replace "[\r\n]+", " ").Trim()
                $recordLines.Add('  ' + $oneLine)
            }
        }
    }
    $recordLines.Add('  ' + $newRecord)

    $text = '[' + [Environment]::NewLine + ($recordLines -join (',' + [Environment]::NewLine)) + [Environment]::NewLine + ']' + [Environment]::NewLine

    # 自检：写盘前先确认拼出来的确实是合法 JSON，免得把趋势文件写坏。
    try { $check = ConvertFrom-Json -InputObject $text } catch {
        Write-Host "[fail] 生成的趋势 JSON 不合法，本次不写入：$($_.Exception.Message)" -ForegroundColor Red
        exit 1
    }
    $dir = Split-Path -Parent $trendPath
    if ($dir -and -not (Test-Path -LiteralPath $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null }
    Set-Content -LiteralPath $trendPath -Value $text -Encoding UTF8
    Add-Line "趋势记录已追加: $trendPath（第 $(@($check).Count) 条）"
}

Write-ReportFile $Out
exit 0
