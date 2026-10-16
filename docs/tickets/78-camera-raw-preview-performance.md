# 78 — Camera Raw 查表与预览性能

**What to build:** Camera Raw 预览在相同设置下快速更新，最终提交仍使用完整精度和同一套查表数据。

**Blocked by:** 28 — Camera Raw：光/色/曲线; 34 — Camera Raw 面板与示波器; 63 — Camera Raw 光影与细节数值对齐。

**Status:** in-progress（2026-10-16；查表资源与加载器已落地，阶段管线待接）

- [ ] 查表资源随应用加载且校验大小/版本；预览取消旧任务，不让旧结果覆盖新设置。
      **资源与校验已完成**（见下）；预览取消的专项用例待补（前端已有 generation 守卫，
      但 Camera Raw 这条没有单独钉住）。
- [ ] 常见照片预览达到原版 5–20 倍优化的可解释基准，完整提交不走低精度捷径。
      **待接表之后才有意义**：现在没有可基准化的查表阶段。
- [ ] 同一输入和设置的预览、提交、撤销结果一致；透明像素统计不污染降噪。**待做**。

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

## 还剩什么

1. 把各滑杆的阶段接上：白平衡 9×9 混四张、对比/高光/阴影按亮度自适应（含
   `camera_raw_statistics` 的三个统计量）、混色器/校准/参数曲线/颜色分级。曝光阶段与
   通用构造器已经就位。这是票 63 与票 90 缺的那一步。
2. Camera Raw 预览取消旧任务的专项用例（验收 1 后半）。
3. 5–20× 的可解释基准（验收 2）。
4. 预览/提交/撤销一致性与透明像素统计（验收 3）。

对等矩阵：U25。原版锚点：`CameraRawTables.swift`、`CameraRawTables.bin`、`CameraRawTests.swift`；提交 `7b5a592`、`a8630e4`。
