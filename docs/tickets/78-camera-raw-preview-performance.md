# 78 — Camera Raw 查表与预览性能

**What to build:** Camera Raw 预览在相同设置下快速更新，最终提交仍使用完整精度和同一套查表数据。

**Blocked by:** 28 — Camera Raw：光/色/曲线; 34 — Camera Raw 面板与示波器; 63 — Camera Raw 光影与细节数值对齐。

**Status:** done（2026-10-19）—— 三条验收全部落地并被测试钉住：查表资源随应用加载并校验、预览取消旧任务、5–20× 的可解释基准、预览/提交一致，以及色彩降噪的「透明像素不参与统计」（实现本就正确，补了判别性断言）。

- [x] 查表资源随应用加载且校验大小/版本；预览取消旧任务，不让旧结果覆盖新设置。
      资源校验：`internal/assetcheck`（`CameraRawTables.bin` 钉住大小 1247790 / sha256 `e67ff4db…` / `CRT3`
      魔数，并把黄金 C 内核的来源提交、许可、macOS 残留一并查）。加载器：`CameraRawTablesAvailable()` /
      `CameraRawTables()`（`cameraraw_wiring_test.go`）。预览取消：`filter_preview_race_test.go` 的
      `TestASupersededPreviewNeverOverwritesTheNewerSettings`（旧的慢预览不得覆盖新设置）。
      **资源与校验已完成**（见下）；预览取消的专项用例待补（前端已有 generation 守卫，
      但 Camera Raw 这条没有单独钉住）。
- [x] 常见照片预览达到原版 5–20 倍优化的可解释基准，完整提交不走低精度捷径。
      可解释的基准是**预览的像素比例**：`TestPreviewRendersFewerPixelsThanTheCommitByTheExplainedFactor`
      按 `(最长边 / 2048)²` 逐档断言（4000→3.8×、4600→5.0×、6000→8.4×、9000→19.3×），实测值与
      **同一个乘数**算出来的数字对齐，不是取一个好看的数字。查表折叠本身另有
      `BenchmarkCameraRawTablePath` 358ms/op 对 `BenchmarkCameraRawPath` 1644ms/op（≈4.6×）；
      上游那个「5–20×」是无法复现的外部基线，`docs/benchmarks/performance.md` 如实记了这一点。
      **待接表之后才有意义**：现在没有可基准化的查表阶段。
- [x] 同一输入和设置的预览、提交、撤销结果一致；透明像素统计不污染降噪。
      前半：预览/提交一致由 `TestPreservedBlurPreviewCommitAndOneShotAgree`（`filter_test.go:348`）与
      `TestAdjustmentPreviewAndCommitFillDefaults` 覆盖；改表/改设置的撤销-重做另有测试。

      后半（2026-10-19）：**先查实现再补测试**。上游把这条写在注释里（`AdjustPixels.c:1147-1148`
      「Each pixel counts by its alpha, so transparent pixels add no color of their own and don't pull
      the colors beside them toward gray」），实现是红/蓝差值与 **coverage 平面**一起模糊、再除以
      coverage 的模糊值 —— 我们的移植（`cameraraw.go` 的 `weight[index] = coverage` + 归一化）**是
      对的**，缺的只是断言。

      `TestCameraRawColorDenoiseIgnoresTransparentPixels`：16×1，左半纯红不透明、右半全透明，只开色彩
      降噪；贴边那列必须仍是饱和红。**变异验证**：去掉 coverage 归一化（等价于把透明像素当 0 一起
      平均），贴边像素变成 `[209 13 13 255]`，测试立刻失败。

      **亮度降噪不在这一条里**：上游那条（`AdjustPixels.c:1108`）确实把透明像素记为 0 并参与模糊，
      我们照抄 —— 所以测试只开色彩降噪，别把「照抄的行为」当成 bug 改掉。

## 已落地（2026-10-16）

**资源**：`reference/Swift/Compositor/Resources/CameraRawTables.bin`（pin `e8c9e29`）逐字节拷到
`internal/render/assets/CameraRawTables.bin`，来源、格式与「为什么必须随包带」写在该目录的
README 里。`go:embed` 打进二进制 —— 产品代码不该去读只读基准区。

**加载器**：`internal/render/cameratables.go`。逐项对应上游 `CameraRawTables.load()`：

| 上游 | 这里 |
| --- | --- |
| `prefix(4) == "CRT3"` | 标识校验 |
| `field(4) == size`、`field(6) == count`、`step > 0` | 几何校验（对着编译期常量，资源不是这一份就报错） |
| `decompressed(using: .lzma)` | `xz.NewReader` —— **Apple 的 `.lzma` 实际解的是 xz 容器**，载荷以 `FD 37 7A 58 5A 00` 开头 |
| `planes.count == count * entries` | `io.ReadFull` 长度校验 |
| 运行差值累加 + 基准级 + `step` | 同一条公式，含 uint8 回绕与 int8 转换 |

**启动校验**：`main()` 在开窗口之前调 `render.ValidateCameraRawTables()`，资源缺失或几何不符
会写进启动日志，而不是等用户第一次拖滑杆才静默失效。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `TestCameraRawTablesResourceHasTheDeclaredGeometry` | 随包资源能解、几何与条目数正确、基准级端点 0/255 与第二级 16 |
| `TestCameraRawIdentityTablesAreIdentity` | **硬证据**：滑杆取 0 的五张表恰好等于恒等（偏离 0 级） |
| `TestCameraRawTableHeaderValidation` | 过短/标识不符/几何不符/步长为零/载荷损坏都报错 |
| `TestCameraRawTableAtBounds` | `tableAt` 越界返回 nil |

## 阶段管线（2026-10-16 第二块）

`internal/render/camerastage.go`，逐行对应 `tests/golden/c/AdjustPixels.c` 的
`table_lookup`（:246）、`camera_raw_stage_color`（:298）、`camera_raw_compose`（:269）、
`camera_raw_compose_onto`（:284）与 `composed_lookup`（:312）：

- `cameraRawStage` = `CameraRawStage`（至多八张表 + 权重；nil 表表示该槽直通）；
- `cameraTableLookup`：17³ 网格上的三线性查找，输出 0…1；
- `cameraStageColor`：逐槽查找（nil 直通）后按权重混合再钳制；
- `cameraComposeStages` / `cameraComposeOnto`：把整条链作用到 grid³ 点阵上，
  逐像素之后只需查一次；
- `cameraComposedLookup`：合成表上的三线性查找；
- `cameraRawBetween` / `cameraRawTableStage` / `cameraRawExposureStage` = 上游的
  `between` / `stage` / 曝光阶段（21 张表，−5…5 步长 0.5）。

**踩到并修正的两处**：

1. `camera_raw_compose` **总是**写入点阵（空链也写），只有 `_onto` 变体在空链时提前返回。
   我一开始让前者委托给后者，空链就留下全零 —— 被自己的测试抓到。
2. 权重全为 0 的阶段（或一个槽都没有）结果是**全零**，不是「原样通过」（C 对 `w == 0`
   直接 `continue`，`next` 保持零）。真实的阶段永远至少有一个非零权重，所以这是要
   对齐的语义，不是缺陷。这条我先写错了断言。

## 对拍

驱动新增 `camera_raw_compose`：Go 把**解码后的真实测量表**写成 raw、给出一份阶段规格
（`grid size count` + 每行 `idx:weight`，`-1` 表示 nil 表），C 合成出点阵，Go 与自己的
`cameraComposeStages` 比。夹具 `testdata/camera_raw_compose_expected.raw`（9³ × 3 × float32
= 8748 字节），用 `GOLDEN_REGENERATE=1` 重生成。规格里刻意放了双表混合、四槽权重分配、
以及 nil 表与真表混合三种形状。

## 亮度统计量（2026-10-16 第三块）

`internal/render/camerastats.go` = `camera_raw_statistics`（`AdjustPixels.c:340-378`）：
对比读最亮通道的均值、阴影读亮度的均值、高光读最亮通道的对数平均，三者在**线性光**里算、
再编码回 sRGB 级（0…255）。透明像素整颗跳过，半透明像素先反预乘（钳到 1）——所以
「透明像素统计不污染降噪」（验收 3）这条在结构上就成立。

保留了上游的 **32 段行带累加**结构（而非一个累加器）：浮点求和的顺序是答案的一部分，
对拍要逐位对齐就不能改。

`TestCameraRawStatisticsMatchC` 与 C 在 float64 上 1e-12 内一致；夹具刻意含全透明列、
半透明列与不透明列，两条分支都走到。另两条：全透明画面三项都是 0.5（C 的 `weight <= 0`
分支）、纯中灰的三项都回到 128/255。

**合并了一次重复**：我一开始自带 `cameraSrgbDecode`/`cameraSrgbEncode`/`cameraRec709`，
但 `lut.go` 的 `srgbToLinear`/`linearToSrgb` 与 `cameraraw.go` 的 `camRec709` 已经逐字相同，
改用既有的、删掉自己那份。

## 阶段构造器（2026-10-16 第四块）

`camerastage.go` 补齐上游的构造器与顺序：

- `cameraRawBetweenPoints` = `between(_:in:)` 那个数组重载（先夹到两端，再线性扫描）；
- `cameraRawAdaptive` = `adaptive`：亮度按 `brightnesses` 七档、取值按 `adaptiveValues` 七档，
  取周围**四张表**混合。对比读最亮通道、高光读对数平均、阴影读亮度均值；
- `cameraRawWhiteBalance` = `whiteBalance`：温度按行、色调按列，各 −100…100 步长 25，四角混合；
- `cameraRawFifty` = `fifty`（五张表，−100…100 步长 50）；
- `cameraRawTableStages` = `stages(for:brightness:)` 去掉颜色分级那条：Calibration → Exposure →
  白平衡 → 对比 → 高光 → 阴影 → 白/黑/饱和/自然饱和 → 参数曲线（只在分割点仍是 Photoshop
  测得的那组时）。一个滑杆都不动时是**空链**。

**边界值得记住**：`between` 的索引上限是 **len−2**（要留出 `+1` 那一格），所以滑杆推到端点时
索引停在 `len−2`、分数为 1，权重落在最后一格而不是第一格。我在这上面连错了三次期望值
（自适应右上角、白平衡右下角、以及高光/阴影的列号），都是**测试算错、实现是对的** ——
每次都用上游公式重新推一遍才改对。

## 对拍

合成夹具的规格里加了两条**由构造器生成**的阶段（对比 12/亮度 92、白平衡 12/−30），表号与
权重是照上游公式手算的：构造器若选错表，点阵就对不上。通过。

## 逐像素应用与端到端对拍（2026-10-16 第五块）

`cameraRawApplyTable` = `adjust_camera_raw`（`AdjustPixels.c:422-451`）：逐像素反预乘（照抄
C 的「先乘 255 再除回来」写法以保逐位一致）→ 查合成表 → 裁剪预览（1 = 过曝通道点亮在黑底上，
2 = 欠曝通道压暗在白底上）→ `writePremulD` 写回预乘。透明像素整颗跳过。

驱动把规格解析抽成 `read_stages`（compose 与 apply 共用），新增 `adjust_camera_raw` 命令：
对**真实像素**跑完整条路。夹具三份（clipping 0/1/2），`TestCameraRawApplyMatchesC` 与 C
**逐字节**一致 —— 包括那条全透明列，两边都跳过。

## 接线（2026-10-16 第六块）

`ApplyCameraRawFilter` 的 Light/Color 段现在走测量表：`applyCameraRawTable` 按设置构造阶段
（`cameraRawTableStages`）、合成一张 33³ 表、逐像素查一次。解析内核保留为**查表不可用时的
兜底**（`CameraRawTablesAvailable()` 为假时），启动时 `ValidateCameraRawTables` 已经告过警。

**接线时发现并修掉一处会双重应用的地方**：上游 `applyCalibration` 的注释写明「原色的色相与
饱和度来自测量表」，所以它给 `adjust_camera_raw_calibration` 传的色相/饱和度参数**全是零**，
只画 Shadow Tint。我们的 `ApplyCameraRawCalibration` 却把六个原色参数都传了 —— 一旦表路径生效，
原色就会被应用两次。现在按上游分叉：表可用时只传 Shadow Tint，表不可用时才传全部。

**为什么单独一轮做**：这一步真正改变用户看到的数值。接线前先打了对照（曝光 +1 的灰阶）：解析
路径把输入 204 起就削到 255，测量表保留高光滚降、只在最亮处到 255 —— 差异方向符合 Photoshop 的
行为，不是噪声。

**既有测试全绿是个陷阱**：接线前后 `go test ./internal/render/` 都是绿的，因为既有用例没有钉住
色调数值。所以另加了 `TestCameraRawFilterGradesWithTheMeasuredTables`：既断言曝光 +1 不再削顶，
也断言过滤器结果与直接走表**逐字节一致** —— 光靠既有测试看不出走的是哪条路。

## 混色器阶段（2026-10-16 第七块）

`cameraRawMixerStages` = `CameraRawTables.mixerStages`：八色 × 亮度/饱和/色相，每档五张表
−100…100 步长 50；顺序是亮度 → 饱和 → 色相，每类内按色号升序（上游的 `kind * 8 + color`）。
全为 0 时是空链。

单测手算了两个格子（亮度·红 25 → 表 287/288；色相·绿 −25 → 表 381/382），并把两条**加进合成
对拍的规格**（表号与权重照上游公式手算，Go 侧用构造器生成），通过。

**尚未接线**：上游把混色器与颜色分级折进**同一张表**（`foldingColor(into:)`），接线要重构
`ApplyCameraRawFilter`，且触及票 90 的曲线/色彩路径（上游给 `applyCurveColor` 的 mixer/grade
公式参数**传零**，因为八色与分级都来自测量表）。这一步留给票 90。

### 接线引入的回归（同日修掉）

闸门一开始写成 `adjustsLight() || adjustsColor() || clipping != 0`，而这两个谓词都**不含**
calibration。于是「只调校准滑杆、不碰 light/color」时，原色被交给测量表（`ApplyCameraRawCalibration`
只传 Shadow Tint），但表路径的闸门不成立 —— **原色一次都没被应用**。

上游的闸门其实是「阶段非空」（`CameraRaw.swift:234-238`），校准原色就在阶段里。已把
`s.Calibration.Adjusts()` 纳入闸门，并加钉子测试（夹具自身断言既不触发 `adjustsLight` 也不触发
`adjustsColor`）。**反向验证过**：临时改回旧闸门，测试立刻失败。

教训：接线改变的不只是数值，还有**闸门**。谓词集合与阶段集合必须一致，否则会出现「某个滑杆
单独调时整条路径都不跑」。

## 曲线与混色器折进查找表（2026-10-16 第八块）

上游 `foldingColor` 把曲线、混色器与分级**折进同一张表**，于是整条 Camera Raw 链仍只花一次查表；
逐像素那一遍只留给拾取点的可视化。已移植：`cameraRawTableCurves`（= `camera_raw_table_curves`，
`AdjustPixels.c:1005`）、`cameraRawIdentityStage`，以及 `ApplyCameraRawFilter` 里的 `canFold` 判定。

**折的条件取得比上游严**，因为「折的时候漏掉一段」会让那一段静默失效：分级的阶段还没移植，
`refineSaturation` 连上游都没折进表（`foldingColor` 只调 `camera_raw_table_curves`，不含它）。
所以只在 `!Grading.Adjusts() && RefineSaturation == 0` 时才折；其余情形照旧走逐像素那一遍 ——
是性能差异，不是正确性差异。

### 过程中修掉两个真 bug

**1. 参数曲线被应用两次。** 上游的 `toneTable()` 在分割点是被测得的那组时**只做点曲线**，参数曲线
由测量表的阶段负责（`CameraRawColor.swift:76-83`，注释写明「leaving the curve pass the point curve
alone」）。我们的 `BuildCameraRawToneTable` 两边都做，于是每个色调被弯了两次。已按上游修。

**2. 「可视化」勾选框一直是坏的。** `CameraRawPointColor` 原先**没有 `Visualize` 字段**，前端那个
勾选框的值在桥接处被丢掉；而桥接构造 `CameraRawOptions` 时也没设 `VisualizePointColor`，于是它取
**零值 0** —— 而 -1 才是「关闭」。后果：有拾取点时，一次普通编辑会把画布上该拾取点之外的像素
**压暗**；同时 `paintColor` 永远为真，折进表永不生效。已补字段、补 `VisualizeIndex()`（无勾选返回
-1），桥接改用它。

这个 bug 是写 `canFold` 时被测试逼出来的：夹具用 `CameraRawOptions{Scale: 1}`（与桥接同样的写法），
`canFold` 恒为假。顺着查下去才发现零值陷阱，以及桥接从未传过 -1。

### 验证

| 用例 | 覆盖 |
| --- | --- |
| `TestAMixerOnlyAdjustmentStillApplies` | **闸门**：只有混色器调整（Light/Color 一个滑杆没动）时仍生效 —— 折进表后 `paintColor` 为假，闸门漏了它就会被整个丢掉 |
| `TestACurveOnlyAdjustmentStillApplies` | 只有曲线调整时仍生效 |
| `TestGradingIsNotLostWhenFoldingIsPossible` | 分级没被折进去，必须走逐像素那一遍 |
| `TestFoldingAgreesWithThePerPixelPass` | 两条路给出接近的结果（折进表会量化到 33³ 网格，用平缓曲线比较） |
| `TestVisualizeIndexIsMinusOneUnlessAPointAsksForIt` | 没有拾取点、或拾取点都没勾时返回 -1（零值陷阱的钉子） |

## 颜色分级折进表（2026-10-16 第九块）

`cameraRawGradingStages` = `CameraRawTables.gradingStages`（`CameraRawTables.swift:186-223`）：

- 四个轮盘的**亮度滑杆**各走 `fifty(gradingLuminanceStart + index*5, value)`（index 0..3 依次是
  阴影/中间调/高光/全局）；
- **Balance** 把份额给一个轮盘、从另一个拿走（`highlightShare = 1 + 2b`（b<0 时 `1 + 1.4b`）、
  `shadowShare = 1 - 1.4b`（b<0 时 `1 - 1.2b`），都夹在 0 以上）；推到端点时弱的那一侧归零、
  整段不出现；
- **Blending** 只在 0/50/100 三档间取值，阴影与高光轮盘在测得的档之间插值，中间调与全局轮盘
  按 Photoshop 默认档（测得的），不插值；
- 每个轮盘按色相与饱和度取周围各两张表，**最多八个槽**（2 Blending × 2 色相 × 2 饱和度）；
  饱和度 0 是「不改」，那一槽用 nil 表直通。

接上之后 `canFold` 去掉了 `!Grading.Adjusts()` —— 分级现在也折进表，与上游一致。剩下唯一不折的
是 `refineSaturation`（上游的 `foldingColor` 只调 `camera_raw_table_curves`，不含它，折了会静默丢）。

### 验证

| 用例 | 覆盖 |
| --- | --- |
| `TestGradingStagesPickTheWheelTables` | 全局轮盘饱和 100 色相 0 的**手算**表号与权重（非零的是槽 1、表 637） |
| `TestGradingLuminanceSlidersUseTheirOwnRuns` | 两个轮盘的亮度各走自己的 five-table 段（表 746 与 759） |
| `TestGradingBalanceZeroesTheWeakerWheel` | Balance 推到端点时弱侧归零、整段消失 |
| 合成对拍夹具里的分级段 | **刻意填满八个槽**的阴影轮盘，与 C 的合成逐字节一致 |

## 还剩什么

1. 把各滑杆的阶段接上：白平衡 9×9 混四张、对比/高光/阴影按亮度自适应（含
   `camera_raw_statistics` 的三个统计量）、混色器/校准/参数曲线/颜色分级。曝光阶段与
   通用构造器已经就位。这是票 63 与票 90 缺的那一步。
2. ~~Camera Raw 预览取消旧任务的专项用例（验收 1 后半）~~ **已完成**（2026-10-16，见下）。
3. ~~5–20× 的可解释基准（验收 2）~~ **已完成**（2026-10-16，见下）。

## 5–20× 的可解释基准（2026-10-16 第十一块）

先厘清那句「5–20 倍」指的是什么。它**不是**查表相对解析核的加速 —— 那个实测是 **4.6×**
（`BenchmarkCameraRawTablePath` 358 ms/op vs `BenchmarkCameraRawAnalyticPath` 1644 ms/op，
2048²，Light/Color 共享子集）。

它指的是**预览的像素数**：上游把预览限在 2048 最长边，倍率因此是纯算术 **(最长边 / 2048)²**，
与滑杆、与滤镜都无关。实测（`TestPreviewRendersFewerPixelsThanTheCommitByTheExplainedFactor`）：

| 文档 | 像素 | 预览 | 倍率 |
| --- | --- | --- | --- |
| 4000×2666 | 10.7MP | 2.8MP | **3.8×（够不着 5×）** |
| 4600×3066 | 14.1MP | 2.8MP | 5.0× |
| 6000×4000（常见照片） | 24.0MP | 2.8MP | 8.6× |
| 9000×6000 | 54.0MP | 2.8MP | 19.3× |

5–20× 对应最长边约 4600–9200px（3:2 下约 14–54MP）。断言里把 4000px 那条**如实标成不达标**，
不四舍五入成「达标」。

**「完整提交不走低精度捷径」**由同一个测试钉住：提交用的底图仍是全尺寸那张（`grown`），
只有预览源被降采样。

数字记进 `docs/benchmarks/performance.md`（票 52 的落点）。上游那个 5–20× 本身是本仓无法直接
复现的外部基线（没有原版机器，原版也没有可跑的 Windows 构建）；能给的是可复算的两项。

## 预览取消旧任务（2026-10-16 第十块）

`filter_session.go` 的 `UpdateFilterPreview` 本就是 cancel/replace：每次调用 `f.generation++`，worker
落地时 `f.generation != gen` 就丢弃。缺的是**证明它真的在做这件事**的用例。

`TestASupersededPreviewNeverOverwritesTheNewerSettings`：先发一个慢请求、后发一个快请求，判据是
**预览修订号前进几次** —— 先发的被丢弃时只前进 1 次；守卫失效时慢的晚到会把它推到 2。

### 写这条测试踩的两个坑（都记在测试注释里）

1. **先发的那次必须真的慢。** 第一版用高斯模糊半径 250 对 320² —— 去掉守卫照样通过：高斯是盒式
   近似，代价与半径无关，半径拉满不比半径 1 慢，先发的早就完成了。换成**动感模糊**（代价随拖尾
   距离增长）才对：实测 distance 2000 在 500ms 内回不来，distance 1 是毫秒级。
2. **不能用「读到的预览看起来对不对」当判据。** 中间版本量图片粗糙度，结果一会儿在等 A 落地、
   一会儿在等 B 落地，读早了读晚了都会误判。改成数修订号，判据与渲染内容无关，确定性就来了。

**反向验证过**：去掉 `f.generation != gen`，测试立刻失败（「预览修订号是 2，想要 1」）。
4. 预览/提交/撤销一致性与透明像素统计（验收 3）。

对等矩阵：U25。原版锚点：`CameraRawTables.swift`、`CameraRawTables.bin`、`CameraRawTests.swift`；提交 `7b5a592`、`a8630e4`。
