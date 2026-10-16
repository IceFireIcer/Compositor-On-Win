# 82 — 16 位图层绘画与调整精度

**What to build:** 用户在 16 位图层上进行调整、模糊、液化、涂抹和绘画时，未触及区域继续保留 16 位精度。

**Blocked by:** 68 — 决策门槛与 .comp v12 / 16 位图层存取; 69 — 16 位图层转换与几何编辑; 26 — 调整内核（色阶/曲线/色相饱和度）。

**Status:** done（2026-10-16）——三条验收均已落地，并在核对过程中修掉三处「8 位编辑丢掉深层」的真 bug。

- [x] Levels、Hue/Saturation、Camera Raw、Blur、Liquify、Smudge 等操作按能力矩阵保留 16 位或明确降深。
      **滤镜菜单**（Levels / Hue/Saturation / Camera Raw / Tonal Contrast / Blur 及全部其它 kind）走
      `render.RunDeep`，即原版 `DeepLayer.run`：把 16 位栅格拆成「比它略低的 8 位值」与「再高一、二、
      四步」的那几份，内核各跑一遍，再在它们之间读回 16 位结果（`deep_split`/`deep_merge`）。
      混色类（Camera Raw / Tonal Contrast / Scanlines）走每通道单独步进的十次运行，与原版一致。
      **重绘工具**（Blur / Smudge / Liquify / Clone）走 `takePainted`（票 71 移植），只对被触碰的像素
      重算深层。两条路都保留了未触碰区域的 16 位精度。
      HDR 图层仍是原路（先归一化再恢复尺度）；没有深层栅格的 8 位图层照旧只跑 8 位。
      验收：`TestSixteenBitFilterKeepsMoreTonesThanTheEightBitCopy`（深层层次多于 8 位副本）、
      `TestFilterKeepsTheDeepRasterConsistentWithTheEdit`（深层反映编辑且清单写 v12），
      以及 `TestDeepRunKernelsMatchC` 与 C 的逐字节对拍。

- [x] 8 位笔刷补丁只改变覆盖区域，未绘画区域的 16 位细节可通过保存重开验证。
      `TestPaintingA16BitLayerLeavesUntouchedPixelsAtSixteenBits`：画一笔 → 保存 → 重开，
      未绘画区域逐像素回到原来的 16 位值（±1 级舍入），绘画区域确实变了，未绘画像素占一半以上。
- [x] 16 位渐变、填充和调整避免新增色带；撤销恢复原深度和像素。
      填充 `TestFillReconcilesTheDeepRaster`、渐变 `TestGradientOnA16BitLayerKeepsAndRestoresTheDeepRaster`
      （含撤销逐字节回到原深层）、调整走滤镜路径的 `RunDeep`。

对等矩阵：U29。原版锚点：`DeepLayers.swift`、`DeepLayerTests.swift`；提交 `6aea90a`、`2fe3524`。

## 落地（2026-10-15）

`internal/render/deeprun.go`：`deepAlpha8` / `deepNoise` / `deepLow` / `deepPast` / `deepChangeFor`
（`DeepChange`）/ `DeepSplit` / `DeepMerge` / `RunDeep`，逐行对应 `DeepPixels.c:22-130`。
驱动新增 `deep_split` 与 `deep_roundtrip` 两条命令（`tests/golden/c/driver_main.c`），夹具与配方见
`internal/render/testdata/deep_kernels.md`。

## 顺带修复：四处 8 位编辑丢掉深层栅格（2026-10-16）

写「未绘画区域保留精度」这条用例时发现，`takePainted` 只接在了部分路径上。凡是
「克隆出 8 位副本 → 改像素 → 存回图层」却没有折深层的操作，都会让深层停在编辑前的样本上：
画布看着变了，**保存写回的却是旧像素**。核出来并修掉的是这四处：

| 路径 | 症状 | 修法 |
| --- | --- | --- |
| `drawing.go` DrawGradient | 渐变后深层仍是渐变前的值 | 存回前 `takePainted(after, base)` |
| `gradient_session.go` 交互式渐变 | 同上（拖动松手那条） | 同上 |
| `paint_session.go` EndPaintStroke | **前端实际用的笔触路径**：`WarpStroke` 之外，画笔提交的是全新 8 位栅格，深层直接被丢掉 | `takePainted(after, p.grid)` |
| `retouch_session.go` warpResult | Blur / Smudge / Liquify 提交与预览都丢深层（`clone` 模式本来就有） | 抽出 `warpResult()`，提交与预览共用 |

两条回归：`TestTheLivePaintStrokeKeepsTheDeepRaster`、`TestALiveRetouchStrokeKeepsTheDeepRaster`。
注意画笔那条**不能**用 `BeginStroke/EndStroke` 验——那是旧 API，它本来就有 `takePainted`；
前端走的是 `BeginPaintStroke/PaintStrokePoint/EndPaintStroke`（`state/canvasDrawing.ts`），
两者曾经只差这一行，第一版用例因此给出了假信心。

其余 `sess.bitmaps[...] =` 站点核对过：复制 / 跨标签传输 / 粘贴走 `Bitmap.Clone()`（会带深层）、
图层蒙版与画布扩展是 8 位对象、`applyRasterJournal` 是撤销回放，都不属于这一类。

`internal/bridge/filter_session.go` 的 `runFilterOnGrid` 现在分三条路：HDR 图层走归一化/恢复、
**普通 16 位图层走 `RunDeep`**、没有深层的 8 位图层照旧。前两条都按选区限制（`blendDeepThroughSelection`）
并把 8 位副本重新派生自深层，最后按 `hdr || deep16` 裁剪深层。

