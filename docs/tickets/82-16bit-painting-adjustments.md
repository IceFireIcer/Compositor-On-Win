# 82 — 16 位图层绘画与调整精度

**What to build:** 用户在 16 位图层上进行调整、模糊、液化、涂抹和绘画时，未触及区域继续保留 16 位精度。

**Blocked by:** 68 — 决策门槛与 .comp v12 / 16 位图层存取; 69 — 16 位图层转换与几何编辑; 26 — 调整内核（色阶/曲线/色相饱和度）。

**Status:** in-progress（2026-10-15；滤镜与重绘两条路都已接，渐变/填充与保存重开待做）

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

- [ ] 8 位笔刷补丁只改变覆盖区域，未绘画区域的 16 位细节可通过保存重开验证。
- [ ] 16 位渐变、填充和调整避免新增色带；撤销恢复原深度和像素。

对等矩阵：U29。原版锚点：`DeepLayers.swift`、`DeepLayerTests.swift`；提交 `6aea90a`、`2fe3524`。

## 落地（2026-10-15）

`internal/render/deeprun.go`：`deepAlpha8` / `deepNoise` / `deepLow` / `deepPast` / `deepChangeFor`
（`DeepChange`）/ `DeepSplit` / `DeepMerge` / `RunDeep`，逐行对应 `DeepPixels.c:22-130`。
驱动新增 `deep_split` 与 `deep_roundtrip` 两条命令（`tests/golden/c/driver_main.c`），夹具与配方见
`internal/render/testdata/deep_kernels.md`。

`internal/bridge/filter_session.go` 的 `runFilterOnGrid` 现在分三条路：HDR 图层走归一化/恢复、
**普通 16 位图层走 `RunDeep`**、没有深层的 8 位图层照旧。前两条都按选区限制（`blendDeepThroughSelection`）
并把 8 位副本重新派生自深层，最后按 `hdr || deep16` 裁剪深层。

