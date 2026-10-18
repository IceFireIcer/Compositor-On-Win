# 71 — HDR 高光编辑与画布预览

**What to build:** 用户编辑 HDR 图层时保留超白高光，通过曝光等调整将高光带回可见范围，并在画布上预览。

**Blocked by:** 70 — 决策门槛与 .comp v13 / HDR 图层导入; 83 — 16 位色调调整与滤镜。

**Status:** in-progress（2026-10-19 回写）——图层上的曝光、色阶、模糊、绘画与变换都已按上游规则保留高光；**唯一未勾项仍是调整图层在 HDR 深度上合成**。它原先记「属票 82」，而**票 82 已经 done**（`CompositeDeep` / 16 位合成器已落地），所以这个阻塞**已解除** —— 剩下的是把 `renderAdjustment` 从 8 位合成图搬到深层合成上。

- [ ] 曝光、色阶、调整层、模糊、变换和绘画按上游 HDR 精度/降级规则工作，未触及高光可恢复。
  - [x] 曝光：走精确线性光内核（`HDRExposure`），不经 8 位查找表。
  - [x] 色阶/模糊/其余滤镜：先按像素归一化到白内、跑内核、再按比例乘回（`HDRFilterNormalize`/`HDRFilterRestore`），选区内外分别对账。
  - [x] 绘画/修复/仿制/填充/剪切：只把改过的像素折进深层栅格（`DeepTakePainted`）。
  - [x] 变换：只改 placement，不重采样；图像尺寸另走 `DrawTransformedDeep`。
  - [ ] 调整**图层**：`renderAdjustment` 仍在 8 位合成图上作用，高光在到达它之前已被裁掉。
        原先记「属票 82」，而**票 82 已经 done**（16 位合成器落地，`render.CompositeDeep` 能按图层
        合成出 `DeepRaster`，见票 84/U31）—— **阻塞已解除**（2026-10-19 核对）。剩下的工作是把
        `renderAdjustment` 的交叉淡入从 `*Bitmap` 搬到 `*DeepRaster`，并让预览/导出两侧都走新路径。
- [x] 屏幕显示有受控映射，预览不把原始高光永久截断为白色（显示副本派生自深层栅格、每次编辑后重算、从不写回）。
- [x] HDR 转 16 位的用户命令、位深标签、撤销及状态刷新正确。
- [x] 保存重开与撤销后高光仍可恢复，按 `DeepLayerTests` 的高光保真用例验证；EXR 导出另见 85。

对等矩阵：U18。原版锚点：`DeepLayers.swift`、`ImageExporter.swift`、`DeepLayerTests.swift`；提交 `a10fcb7`、`b9186f7`。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `TestExposureOnHDRIsExactNotAnEightBitApproximation` | 曝光走线性光内核，结果落在曲线上而不是 8 位近似 |
| `TestExposureBringsHighlightsIntoThePreview` | 降曝光把高光带进可见范围，深层仍留着大于白的光 |
| `TestLevelsDimsHDRHighlightsInProportion` | 与上游同名用例互证：色阶按比例压暗 |
| `TestFilterOnHDRKeepsHighlightsRecoverable` | 反相后图层仍是 HDR |
| `TestBrushStrokeOnHDRKeepsUntouchedHighlights` | 笔触只改覆盖区域，没画到的高光原样保留 |
| `TestConvertHDRToSixteenBitClipsHighlights` | 裁到白、保持 16 位、撤销装回高光 |
| `TestExposureOnHDRRespectsTheSelection`、`TestLevelsOnHDRRespectsTheSelection` | 选区只让选中的像素动 |

## 已知差异

1. **调整图层仍在 8 位合成图上作用**：`renderAdjustment` 克隆的是 8 位合成结果，高光在到达它之前
   已经被裁掉。上游在 HDR 深度上合成（`EditorCanvas.adjusted` 把 `hdr: hasHDR` 交给
   `GPUAdjustment.apply`）。要 16 位合成器，属票 82。
2. **非曝光滤镜的中间量是 8 位**：上游 `normalizing` 把 16 位栅格交给 `run()`；这里的内核只吃 8 位
   像素，所以白内的层次是 8 位精度（高光本身按比例还原，不受影响）。16 位内核属票 82。
3. ~~**纯 16 位图层跑滤镜仍会丢深层像素**~~：**票 82 已修**——`runFilterOnGrid` 对普通 16 位
   图层走 `render.RunDeep`（原版 `DeepLayer.run`：拆分跑内核再读回 16 位），不再 `DropDeep`。
   见票 68 的第 2 条与对等矩阵 U29。
4. 屏幕抖动（8 位屏上的平滑显示）属票 83；这里的显示副本是抖动后的派生副本。
