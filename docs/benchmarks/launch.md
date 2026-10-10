# 启动到就绪基准（launch-to-ready）

> 票 52 / 对等矩阵 P10。原版基准：`reference/Swift/CompositorUITests/CompositorUITests.swift:38-52`
> 的 `testLaunchPerformance`。工具：[`scripts/launchbench.ps1`](../../scripts/launchbench.ps1)。

## 指标定义

**启动到就绪 = 从应用进程启动，到前端第一次拿到快照并认为"界面就绪"的墙钟秒数。**

契约（Go/Svelte 侧实现，基准脚本只消费）：

1. 应用启动时记录进程启动时刻；
2. 前端第一次拿到快照、认为界面就绪后，调用绑定的 `ReportReady()`；
3. Go 侧往 **stdout** 打印一行，前缀固定、3 位小数：

   ```text
   LAUNCH_TO_READY_SECONDS: 1.234
   ```

4. 进程**继续正常运行**（不退出）——所以基准脚本拿到这一行之后自己结束进程。

指标名与"取中位数"的语义刻意和 Swift 版对齐：

| Swift 原版 | 本仓库 |
| --- | --- |
| `print("LAUNCH_TO_READY_SECONDS: \(samples)")`（5 个样本） | `LAUNCH_TO_READY_SECONDS: [s1, s2, ...]` |
| `print("LAUNCH_TO_READY_MEDIAN: \(samples.sorted()[2])")`（排序后取中间那个） | `LAUNCH_TO_READY_MEDIAN: <中位数>` |
| `app.buttons["newCanvasWelcome"].waitForExistence(timeout: 5)` | 应用自己调 `ReportReady()` |

**两个数不能直接跨平台比**，差异是明说在前的：

- 原版从 XCTest 进程外部计时，包含 XCTest 启动 / 空闲 / 无障碍（accessibility）开销；
  本仓库测的是应用内部"进程启动 -> 前端报就绪"，没有测试框架开销。
- 原版以"欢迎按钮可见"为停表条件（UI 查询轮询粒度）；本仓库以应用自己报的就绪为准。

中位数语义：奇数个样本取排序后的中间那个；偶数个取中间两个的算术平均。默认 `-Runs 5`
与原版一致（5 个样本时就是排序后第 3 个）。**没等到就绪标记的那次不计入样本**，
也不参与中位数；脚本会逐次报"第 k 次没等到就绪标记"。

## 怎么跑

```powershell
# 默认：5 次，自动找 build\bin\Compositor.exe；不存在就先跑 wails build
pwsh -NoProfile -File scripts\launchbench.ps1

# 已经构建好、只跑 3 次，并把结果追加到趋势文件
pwsh -NoProfile -File scripts\launchbench.ps1 -SkipBuild -Runs 3 -Record

# 指定别的产物 / 另存一份文本记录 / 单次超时 60 秒
pwsh -NoProfile -File scripts\launchbench.ps1 -Exe build\bin\Compositor.exe -Out launch-run.txt -TimeoutSeconds 60
```

参数：

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `-Runs` | 5 | 启动次数（样本数上限），与原版一致 |
| `-Exe` | `build\bin\Compositor.exe` | 被测可执行文件 |
| `-Out` | 空 | 把本次文本记录（含真实 stdout）另存到文件 |
| `-Record` | 关 | 追加到 `docs/benchmarks/launch.json` |
| `-SkipBuild` | 关 | exe 不存在时直接报错，不跑 `wails build` |
| `-TimeoutSeconds` | 30 | 单次等待就绪标记的上限 |
| `-Force` | 关 | 检测到无交互式桌面会话时仍然尝试 |

输出长这样（数值仅示意格式，**不是**本机测量结果）：

```text
LAUNCH_TO_READY_SECONDS: [1.234, 1.301, 1.266]
LAUNCH_TO_READY_MEDIAN: 1.266
```

退出码：`0` 至少拿到一个样本；`1` 构建失败 / 趋势文件损坏；`2` 预检失败或一个样本都没拿到
（无交互式会话、缺 exe、`-Runs < 1`）。

## 本机实测结果

**已执行。** 环境与命令：

- 机器：Windows 11 专业版，AMD Ryzen 9 7945HX，15.18 GiB 内存，`SESSIONNAME=Console`
  （交互式桌面会话，满足 WebView2 的要求）。
- 产物：`wails build` 于 2026-10-10 23:24:45 构建的 `build/bin/Compositor.exe`
  （17.48 MB，含票 52 的 `ReportReady()` / `MarkLaunchStart()`）。
- 命令：`pwsh -NoProfile -File scripts\launchbench.ps1 -SkipBuild -Runs 5 -Record`
- 时间：2026-10-10 23:25:40 +08:00（UTC `2026-10-10T15:25:40Z`）。

真实输出（照抄，未做任何修饰）：

```text
  第 1 次: 0.542 s
  第 2 次: 0.560 s
  第 3 次: 0.613 s
  第 4 次: 0.545 s
  第 5 次: 0.547 s
----------------------------------------------------------------------
LAUNCH_TO_READY_SECONDS: [0.542, 0.560, 0.613, 0.545, 0.547]
LAUNCH_TO_READY_MEDIAN: 0.547
样本数: 5/5（没等到标记的次数不计入样本）
趋势记录已追加: docs/benchmarks/launch.json（第 1 条）
```

**本机基线：`LAUNCH_TO_READY_MEDIAN = 0.547 s`**（5/5 样本，范围 0.542–0.613 s）。
对应记录已写进 [`launch.json`](launch.json)。

同一天再跑一次 3 次（`-Runs 3`）得到 0.534 s，另一次 `-Runs 5 -Record` 得到 **0.513 s**
（样本 0.484/0.588/0.513/0.559/0.505），也已记入 `launch.json` 第 2 条。所以本机基线的量级是
**0.51–0.55 s**，三次测量的离散度约 ±0.03 s。

> 排障：应用是单实例的。如果机器上已经有残留的 Compositor 进程（占着单实例锁），基准每次都会
> 看到"进程在报就绪之前就退出了（退出码 0）"，一个样本都拿不到 —— 先结束残留实例再跑。

两点必须一起读的限定：

1. 这个数**不能**和原版的 `LAUNCH_TO_READY_MEDIAN` 直接比。原版从 XCTest 进程外部
   `app.launch()` 计时（含进程创建、XCTest/无障碍开销，停表条件是欢迎按钮可见）；
   本仓库量的是 `main()` 第一行到前端第一次拿到快照，**少了 CreateProcess 与 WebView2
   环境创建**这一段。契约实现里也写明了这一点（`internal/bridge/launch.go`）。
2. 运行时应用会打印一行无关警告（`任务栏覆盖图标不可用: 找不到主窗口...`），
   它出现在就绪标记之前，不影响采集。

工具本身另外用桩程序验证过正确性（**这些数字是桩的固定延迟，不是应用性能**）：
两个假可执行文件分别模拟"会打印就绪标记"和"永远不打印"，脚本给出
`LAUNCH_TO_READY_SECONDS: [0.400, 0.400, 0.400]` / `LAUNCH_TO_READY_MEDIAN: 0.400`；
超时路径报"第 k 次没等到就绪标记"且退出码为 2；`-Record` 追加多条后 JSON 仍可解析；
临时文件与子进程都被清理。

## CI 里为什么是"尽力而为"

`.github/workflows/verify.yml` 里这一步带 `continue-on-error: true`，步骤名直接写明
"best effort"。原因：

- GitHub 的 `windows-latest` runner 是**没有交互式桌面会话**的（Session 0 / headless）。
  WebView2 需要一个真实的桌面会话才能创建窗口并渲染，起不来自然也就没有"界面就绪"。
- 脚本遇到这种情况会**明确报错**（"检测到没有交互式桌面会话……请在交互式桌面会话里跑"）
  并且**不产出样本**，而不是编造一个 0 或者拿失败当通过。
- 因此 CI 上这一步通常是跳过 / 失败：它的作用是**留下痕迹**（"这项没在 CI 里量到"），
  真值来自开发机上的交互式会话，记录在本文档与 `launch.json`。
- CI 里同一批还加了"轻量基准"一步（`go test ./internal/... -run '^$' -bench . -benchtime=1x`），
  它才是 CI 日志里的趋势落点；`-benchtime=1x` 是为了不在 windows-latest 上拖长流水线。

## 趋势文件 `launch.json` 的格式

`-Record` 会**追加**一条记录（文件不存在就新建；目录不存在就建目录）。结构是 JSON 数组，
**一条记录一行**，方便 diff 与后续画趋势：

```json
[
  {"timestamp":"2026-01-02T03:04:05Z","runs":5,"samples":[1.234,1.301,1.266,1.240,1.288],"medianSeconds":1.266,"os":"Microsoft Windows 11 专业版","cpu":"AMD Ryzen 9 7945HX with Radeon Graphics","memoryBytes":16299335680}
]
```

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `timestamp` | string | 本次运行开始的 UTC 时间（`yyyy-MM-ddTHH:mm:ssZ`） |
| `runs` | number | **实际计入的样本数**（没等到就绪标记的次数不算） |
| `samples` | number[] | 每次的秒数 |
| `medianSeconds` | number | 上面那组样本的中位数（与 stdout 的 `LAUNCH_TO_READY_MEDIAN` 同值） |
| `os` | string | 操作系统描述 |
| `cpu` | string | CPU 型号 |
| `memoryBytes` | number | 物理内存字节数 |

约定：

- 数值一律 3 位小数、用 invariant culture（小数点，不受系统区域设置影响）；
- 写入前脚本会自己 `ConvertFrom-Json` 校验一遍，拼错了就**不写**（宁可这次不记录，
  也不把趋势文件写坏）；文件已损坏时也会拒绝写入并报错，不会覆盖历史；
- 只有拿到真实样本才会写记录——没有样本就没有记录，不会写 `null` 或 `0`。

## 脚本做了什么（排障用）

- 每次启动用 `.NET Process` + 重定向 stdout/stderr，**逐行异步读**（`ReadLineAsync`）：
  应用打印标记后继续运行，同步 `ReadToEnd` 会一直阻塞；`BeginOutputReadLine` 的回调跑在
  线程池线程上没有 Runspace，PowerShell 脚本块会抛异常。两种坑都踩过，所以用现在这个写法。
- 拿到标记后**按 PID 结束那一个进程及其子进程树**（`Kill(entireProcessTree: true)`）：
  只杀主进程会留下 WebView2 渲染进程占住用户数据目录、影响后续几次启动；按进程名杀则会
  误伤用户自己开的实例，所以绝不按名字杀。
- 临时输出文件用完即删（父进程自己持有流并显式释放，删不掉会出声警告）。
- **启动前先确认没有残留的 Compositor 实例**：应用是单实例的，已经有一个实例在跑时，
  新进程会立刻退出（退出码 0）且不打印就绪标记——脚本会如实报"进程在报就绪之前就退出了"。
  这不是基准失败，是环境没清干净；先结束残留实例再跑。（实测时踩过一次。）

## 不在这份文档里的事

- 大文档（100MP 级）打开/合成基准、4K 笔刷延迟、内存预算与缓存逐出：见
  [performance.md](performance.md)（另一张票的交付物）。
- 本项**只测启动到就绪**，不测稳态帧率、不测内存占用。
