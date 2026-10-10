# 52 — 性能与内存预算

**What to build:** 性能与内存对齐：DocumentLimits 预算（RAM/16 语义在 Windows 的实现与上限测试）、大文档（100MP 级）打开/合成基准、4K 笔刷 <16ms 复测、纹理/历史缓存的内存压力逐出验证、启动到就绪基准（原版 LAUNCH_TO_READY_MEDIAN 对应指标）。

**Blocked by:** 09 — CPU 合成器真值; 11 — WebGPU 合成器; 22 — WGSL 笔刷覆盖与延迟管线.

**Status:** implemented（2026-10-10）

- [x] 内存预算函数与限额测试（memoryBudget 语义）
- [ ] 大文档基准（打开/合成/缩放）记录并与原版 4K 笔记对照
- [x] 内存压力注入测试：缓存逐出且应用不崩溃
- [x] 启动到就绪中位数基准入库（CI 记录趋势）

对等矩阵：V09、V10、L08、P10。

> 没勾的那条（大文档基准）原因写在「已知差异」里：**打开/缩放**两项没做（只做了合成与笔刷）。
>
> 启动到就绪已经**真跑出中位数**并入库：本机 3 次测量 median 0.547 / 0.534 / **0.513 s**
> （趋势在 `docs/benchmarks/launch.json`，口径与跑法在 `docs/benchmarks/launch.md`）。CI 里的那一步
> 是 log-only + artifact（`continue-on-error`）：托管 runner 没有交互式桌面会话，真值要在本机/VM 取。

## 实现说明（2026-10-10）

**预算（L08）。** `DocumentLimits.documentPixelBudget` 的 RAM/16 语义以前只活在
`DocumentPixelBudget()` 里，用 `sync.Once` 把"这台机器"算一次 —— 没法测边界。
现在拆出一个纯函数：

```go
func DocumentPixelBudgetFor(totalBytes uint64) int   // min(800M, max(200M, total/16))
```

`DocumentPixelBudget()` 只是它加一次 `GlobalMemoryStatusEx` 采样；测试覆盖
0（读不到 → 下限）、1/2/3/4/8/11/12/16 GiB 与 1 TiB 的每个拐点 —— 其中 8 GiB = 537 MP
正是 macOS 源码注释里引用的数字（跨平台语义对齐的交叉验证）。另外断言两个像素上限都
低于 `MaxSide²`（否则"30000 边的正方形"不会被判超限，而好几个测试正是这么表达"太大"）。

**内存压力（V10）。** 新增 `internal/memory`：档位（`moderate`/`critical`）与纯函数
`LevelFor(total, available)`（可用低于 25% / 12%），外加一个只在**档位变化**时回调的
`Watcher`（`Step` 同步走一步，测试不必等时钟；`Start` 会先立刻采一次）。阈值取保守：
Windows 报的可用内存不含可丢弃的缓存页，而我们分配的是几十上百 MB 的位图。

桥接（`internal/bridge/memory.go`）把它接到真实动作上，**只丢可重建的东西**：

| 档位 | 历史上限（步数 / 字节） | 最近一帧（渲染缓存） |
| --- | --- | --- |
| 无压力 | 100 步 / 256MB（默认） | 保留（恢复时也不动它） |
| moderate | 50 步 / 128MB | **保留** —— 重合成正是紧张时要避开的开销（macOS 的 GPU 画布同样 keepLastFrame） |
| critical | 10 步 / 64MB | 丢弃（这是我们自己最大的一块可重建内存） |

**阈值是本应用的设计，不是从原版移植的**：原版监听系统的内存压力事件
（`DispatchSource` 的 `.warning`/`.critical`，见 `reference/Swift/Compositor/Rendering/GPUCanvas.swift`），
Windows 侧我们退化成"可用内存比例"（<25% / <12%）—— 系统事件那套在 Go 里没有等价物。
矩阵里也写明了这点。

- 字节上限只在装了 `RetainedBytes` 估计器时才咬人（Go 的像素在磁盘 PNG 上，见
  `history.RetainedEstimator`），所以实际收缩历史的是**步数**上限 —— 与原版字节预算
  同效（丢最旧的撤销），只是量纲不同，这点写进了 `History.SetLimits` 的注释。
- 丢的只有渲染缓存（`session.renderPNG`）与撤销历史；文档与位图库是用户的活，绝不逐出。
- 档位变化推 `memory:pressure`（负载就是档位字符串，`""` = 恢复）；前端
  `App.svelte` 的 `onMount` 订阅它（`watchMemoryPressure()`），`state/memory.ts` 转成 store
  并派发给注册过的处理器 —— GPU 纹理缓存接画布时 `registerPressureHandler(cache.onMemoryPressure)` 即可用。
- 采样**刻意不放在 `Service.Startup`**：那个钩子也被测试用来装运行时上下文，而采样会读
  真实机器内存并可能立刻收缩历史。改由 `main.go` 的 `OnStartup` 经 `ShellHooks.StartMemoryWatch()` 启动，
  退出时 `Shutdown` 停掉。

**启动到就绪（P10）。** `main()` 第一条语句 `bridge.MarkLaunchStart()`；前端第一次拿到快照、
**等一个 DOM flush（`tick()`）**之后、在 `finally` 里调一次 `ReportReady()`（首次快照失败也要给出数字，
否则基准脚本每次白等到超时）；Go 侧打印

```
LAUNCH_TO_READY_SECONDS: 1.234
```

（只认第一次，重复调用不重新计时）。`scripts/launchbench.ps1` 反复启动真实 exe、抓这一行、
算中位数并按原版的指标名打印 `LAUNCH_TO_READY_MEDIAN`。

**本机实测**（Windows 11 / Ryzen 9 7945HX / 15.18 GiB，`SESSIONNAME=Console`）：三次测量
median **0.547 / 0.534 / 0.513 s**（样本范围 0.484–0.613 s），趋势入库
`docs/benchmarks/launch.json`。排障一条：应用是单实例的，机器上有残留的 Compositor 进程时
基准会 5/5 "进程在报就绪前就退出"、一个样本都拿不到 —— 先结束残留实例。

**基准（V09）。** `internal/render/composite_bench_test.go` 与
`internal/bridge/brush_bench_test.go`，基线数字与跑法在
`docs/benchmarks/performance.md`。日常 `go test ./...` 不受影响：基准在 `-short` 下跳过，
100MP 那条额外要 `COMPOSITOR_BENCH_HEAVY=1`。

## 测试

- `internal/domain`：预算拐点表（0/1/2/3/4/8/11/12/16 GiB/1 TiB）+ 上限关系 + 本机预算落在合法区间。
- `internal/memory`（4 个）：`LevelFor` 阈值表（含读不到总量 → 不动）、只在档位变化时回调、
  `Start` 立刻采样且 `Stop` 干净可重复、间隔默认值。
- `internal/history`（2 个）：`SetLimits` 立刻收缩且**丢最旧**、剩下 5 步仍可撤销与重做、
  恢复上限不会凭空恢复历史、负值按 0（清空）。
- `internal/bridge`（6 个）：档位到上限的映射；注入 critical 后历史收缩 + 渲染缓存丢弃 +
  事件推出 + **撤销/重做/重新渲染都还能用**；压力过去恢复默认上限；没有运行时上下文时不推事件
  也不 panic；脚本化采样序列端到端只在档位变化时推事件；`ReportReady` 只认第一次。
- 前端 `state/memory.test.ts`（5 个）：档位归一化（未知负载当没压力）、注册/注销与去重、
  无运行时空订阅、有运行时时事件 → store + 派发。

## 已知差异 / 未验证

1. **大文档基准只做了"合成"**：`BenchmarkComposite100MP`（10000×10000，单次 21.2s、分配 3.8GB，
   只跑 1 个样本 —— 意义是"这条路能跑通且量级可测"，不是稳定回归基线）。
   **打开**与**缩放**没有单独基准：打开的开销主要是 PNG 解码（`internal/project` +
   `rasterio`），缩放在前端是 CSS 变换、不触发重新合成，所以这一项要测的是解码而不是缩放 ——
   本票没做，别把它当成已覆盖。
2. **4K 笔刷只能测到直径 30**：原版场景是直径 40/800，而桥接层目前只暴露固定的默认笔刷
   （`internal/bridge/brush.go` 的 `defaultBrushSettings`），没有设直径的入口；要按原版直径测
   必须先给 bridge 加设置入口（本票没做）。实测 median 1.5–2.0ms、p95 2.1–2.5ms 都远低于
   16ms 目标，但**不含视图显示**，与原版的"含显示"口径不可直接比 —— 对照表里写清了三条原因。
3. **前端没有可逐出的 GPU 纹理缓存**：画布显示的是 Go 合成好的 PNG（`<img>`，按 rev 换 URL），
   `lib/gpu` 里的 `TextureCache`/`LayerBitmapStore` 目前只在库层与测试里存在。所以 V10 是
   "Go 侧缓存真的逐出 + pressure 事件与注册口就位"，GPU 路径（票 11）接上画布时
   `registerPressureHandler(cache.onMemoryPressure)` 即可用 —— 注册口有测试，不是摆设，但
   **今天确实还没有生产端注册**。
4. **CI 里的启动基准是"尽力而为"**：`.github/workflows/verify.yml` 新增的步骤跑
   `scripts/launchbench.ps1 -SkipBuild -Runs 3` 但带 `continue-on-error`（托管 runner 没有交互式
   桌面会话，WebView2 未必起得来）；同一步还上传 artifact。**硬门禁没有**，真值靠本机/VM 跑并
   `-Record`。跨平台对比注意口径：我们的计时窗**包含** WebView2 创建与前端加载，
   **不含** `main()` 之前的进程创建与 XCTest/辅助功能等待。
5. **阈值是保守的经验值**（25% / 12%），没有在真实内存耗尽场景下调过参：压力路径的测试是
   **注入档位**，不是真把机器内存吃掉（后者会让 CI 不可预测，也可能把测试机器搞崩）。
6. **历史字节预算在 Go 侧是惰性的**（默认没有 `RetainedBytes` 估计器），压力实际上靠步数上限生效。
