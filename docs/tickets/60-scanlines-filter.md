# 60 — 独立 Scanlines 滤镜

**What to build:** 用户通过 Filter 菜单独立应用扫描线效果，能控制线条、亮度、错位和颜色并实时预览。

**Blocked by:** 33 — 滤镜实时预览管线; 36 — 抖动滤镜。

**Status:** done（2026-10-14）。内核 / 前端 / 黄金基准由实现者完成，滤镜管线的 kind 注册、
全尺寸预览名单与黄金基准的统一由 Lead 集成时补上（见"集成"）。

- [x] Scanlines 从 Dither 风格中独立出来；旧 Dither 的非扫描线模式不受影响。
      `internal/render/scanlines.go` 是独立内核（`ScanlinesParams` / `ScanlinesApply` /
      `ScanlinesSettings` / `ApplyScanlinesFilter`），**没有**改 `dither.go`；旧 Dither 的
      `DitherStyleScanlines` 分支与 `dither-scanlines` 黄金基准逐字节未动（重新生成
      `ref/` 后既有 35 张参考图零差异）。
- [x] 间距、厚度、辉光、点状、抖动、位移/平滑、阈值、色彩分离、密度、对比、黑位及颜色模式可调。
      内核 13 项参数全部落地（范围/默认对齐 `Scanlines.swift` 的 `normalized`）；
      `FilterDialog.svelte` 有 12 条滑块 + 颜色模式 + 双色调取色（屏色/线色）；
      `filters.ts` 的 `FILTER_KINDS` / `FilterSettings` / `defaultFilterSettings` 已加 scanlines。
- [x] 位移形成前后遮挡，纯黑/纯白和透明边界行为对齐原版；效果可预览、选区限制、撤销。**「端到端可用」没有专门用例**：黄金基准直接调 `render.ApplyScanlinesFilter`，没有 bridge 用例把 kind `scanlines` 走完 BeginFilterEdit/CommitFilter。
      遮挡（"抬起的线挡住后面的线"）、黑位、纯黑/纯白、透明边界已由
      `internal/render/scanlines_test.go` 逐条对齐原版 `ScanlinesTests.swift` 并验证；
      "可预览、选区限制、撤销"走既有管线（BeginFilterEdit / UpdateFilterPreview /
      CommitFilter 带选区 + 光栅撤销日志）——kind 已由 Lead 注册进 `internal/bridge/filter.go`，
      端到端可用。
- [x] 基于新 `ScanlinesTests` 和 `DitherPixels.c` 的扫描线数值用例增加黄金基准，记录允许误差。
      4 个 case（`scanlines-basic` / `scanlines-dots-wobble` / `scanlines-displace-split` /
      `scanlines-two-colors`），ε=1/255 显式写在 case JSON 的 `epsilon` 与 `note` 里；
      Go 移植实测最大逐通道偏差 **Δ0**（测试日志逐 case 打印）。

## 集成（Lead，2026-10-14）

实现者的写范围不含 `internal/bridge/**`，所以管线接线与两处对齐由 Lead 补：

1. **kind 注册（`internal/bridge/filter.go`）**：`FilterScanlines = "scanlines"` 常量、
   `filterNames` 加「扫描线」、`filterParams` 加 `Scanlines render.ScanlinesSettings`、
   `normalized()` 加 `case FilterScanlines: r.Scanlines = p.Scanlines.Normalized()`、
   `runFilterJob()` 加 `case FilterScanlines: render.ApplyScanlinesFilter(work, p.Scanlines)`。
   `validFilterKind` 认 `filterNames`，所以这一步之后"扫描线…"才能开、预览/选区/撤销才端到端可用。
2. **全尺寸预览（`internal/bridge/filter_session.go`）**：`prepareFilterGrid` 的 `fullSize`
   名单按原版 `Filters.swift:619-621` 补齐 —— 噪点、**抖动**、**扫描线**、内容感知填充、
   移除背景（颗粒是 `adjust:Grain`，由 `isAdjust` 分支覆盖）。此前只有 addNoise/contentAwareFill
   与调整类是全尺寸，Dither 与 Scanlines 在缩小的副本上做、放大后会变粗；这条一并修掉了
   Dither 的既有偏差。
3. **黄金基准统一到 harness**：`tests/golden/golden_test.go` 的 `goPort` 加 `case "scanlines"`
   分支（参数映射与 `driver_main.c` 的 argv 顺序一致），并把 `"scanlines"` 加进
   `TestGoldenCasesHaveReferences` 的必备内核名单与 `TestGoldenReferencesMatchGoPorts` 的
   "必须真比对"名单；`internal/render/scanlines_test.go` 里那段等价的比对随之删掉
   （内核包里只留单元测试）。现在 `go test ./tests/golden/` 会**真的比对**这 4 个 case，
   不再显示"内核尚未移植"：`38/39 cases compared`，4 个 scanlines case 全 PASS。

## 黄金基准与来源

- C 内核拷贝件：`tests/golden/c/ScanlinesPixels.{c,h}`，逐字拷自
  `reference/Swift/Compositor/Rendering/DitherPixels.{c,h}` 的扫描线段
  （`clamp01` / `in_bands` / `adjust_tone` / `write_pixel` / `draw_lines` /
  `scanlines_apply`）。既有的 `tests/golden/c/DitherPixels.*` 是 1.4.8 之前的拷贝、
  没有 `ScanlinesParams`，按票面要求保持不动（AGENTS.md 不变量 #4）。
- `tests/golden/c/driver_main.c` 新增 `scanlines` 命令分支（18 个参数，顺序见文件头注释）；
  `tests/golden/golden.go` 的 `DriverArgs` 新增同名分支；`golden_driver.exe` 已用
  README 里的命令重建。
- 复核可复现性：`go run ./tests/golden/gen -out <tmp>` 与已提交 `ref/` 逐字节一致，
  只多出 4 张新的 scanlines 参考图。
- Go 侧比对在黄金基准 harness 里（`tests/golden/golden_test.go` 的 `goPort`），
  与其它内核同一条路：`TestGoldenReferencesMatchGoPorts` 逐个 case 比 ref PNG，
  ε 取 case JSON 里的声明。实测 4 个 case 全部通过（最大逐通道偏差 Δ0，声明 ε1）。

## 已知差异

1. **Glow 不是上游算法**：上游在 1/shrink 尺度上模糊（`Scanlines.swift:84-89`），Go 侧做的是全尺寸
   高斯；黄金驱动的 `scanlines` 命令也没有 glow 参数，所以这条偏离**从未与参考比对过**。
2. **注释语言**：`internal/render/scanlines.go` 的注释是中文，而该目录其余是英文、它移植的
   `tests/golden/c/ScanlinesPixels.c` 也是英文。数值常数与出处注释都保留了，只是与 C 拷贝的逐行
   可比性变差。

## 参数与默认值（对齐 `Scanlines.swift`）

| 参数 | 范围 | 默认 |
| --- | --- | --- |
| lineSpacing | 2–32 px（取整） | 4 |
| thickness | 5–100% | 70 |
| glow | 0–100% | 0 |
| dots | 0–100% | 0 |
| wobble | 0–64 px | 0 |
| displace | −100–100 px | 0 |
| smoothness | 0–100% | 50 |
| threshold | 0–100% | 0 |
| split | 0–16 px（取整） | 0 |
| density / contrast | −100–100 | 0 |
| blackLevel | 0–100% | 0 |
| colors | 黑白 / 双色调 / 原始色 | 黑白 |
| dark / light | 0–1 直出 sRGB | (0,0,0) / (1,1,1) |

原版锚点：`Scanlines.swift`、`ScanlinesTests.swift`、`DitherPixels.c`、`DitherPixels.h`；
提交 `06c3a22`、`1187736`、`cdde327`。
