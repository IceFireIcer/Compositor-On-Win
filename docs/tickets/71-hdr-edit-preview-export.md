# 71 — HDR 高光编辑与画布预览

**What to build:** 用户编辑 HDR 图层时保留超白高光，通过曝光等调整将高光带回可见范围，并在画布上预览。

**Blocked by:** 70 — 决策门槛与 .comp v13 / HDR 图层导入; 83 — 16 位色调调整与滤镜。

**Status:** done（2026-10-19）——图层上的曝光、色阶、模糊、绘画与变换都按上游规则保留高光；**调整图层也在深层上合成了**：调整层作用在 `DeepRaster` 上（曝光原生吃 uint16，其余走归一化/还原），合成器按「有 HDR 层就 HDR」决定累积栅格，source-over/alpha 缩放都在半浮点上算，EXR 导出因此能吃「HDR 层 + 调整层」（4.0 经 -2 档峰值仍 ≈2.22）。

- [x] 曝光、色阶、调整层、模糊、变换和绘画按上游 HDR 精度/降级规则工作，未触及高光可恢复。
  - [x] 曝光：走精确线性光内核（`HDRExposure`），不经 8 位查找表。
  - [x] 色阶/模糊/其余滤镜：先按像素归一化到白内、跑内核、再按比例乘回（`HDRFilterNormalize`/`HDRFilterRestore`），选区内外分别对账。
  - [x] 绘画/修复/仿制/填充/剪切：只把改过的像素折进深层栅格（`DeepTakePainted`）。
  - [x] 变换：只改 placement，不重采样；图像尺寸另走 `DrawTransformedDeep`。
  - [x] 调整**图层**：原先 `renderAdjustment` 在 8 位合成图上作用，高光在到达它之前就被裁掉。
        原先记「属票 82」，而**票 82 已经 done**（16 位合成器落地）—— 阻塞解除后两步做完了：

        **第一步（2026-10-19）**：调整层现在作用在**深层**上。
        - `render/deepcomposite.go` 新增 `deepAdjustmentLayer` / `deepAdjust` / `deepFadeToward`：
          `CompositeDeep` 遇到调整层就在累积的深栅格上跑内核，再按不透明度交叉淡入。
        - **曝光**原生吃 uint16（`HDRExposure` 在线性光里算），超白高光被精确保留：4.0 经 -2 档得
          ≈2.22；8 位路径先把 4.0 裁成 1.0，只得 ≈0.54 —— 这条差别就是测试的分辨力。
        - 其余内核走 `HDRFilterNormalize` / `HDRFilterRestore`（归一化到白内 → 跑 8 位内核 → 按
          逐像素比例还原）：恒等色阶下 4.0 原样回来。
        - `deepFadeToward` 在 HDR 上**解码半浮点后插值**（1.0 与 4.0 的中点是 2.5；直接对位模式插值
          会得到 2.0）。
        - 16 位导出（PNG/TIFF）不再因为「有调整层」被拒：`export16.go` 的闸门放开，并补了
          `TestExportAsSixteenBitAppliesAnAdjustmentLayer`（色阶确实压暗了像素）。
        - 两处变异验证：去掉 `deepAdjust` 的 HDR 分支 → 曝光测试失败；去掉 `deepFadeToward` 的
          半浮点分支 → 插值测试失败；闸门重新拒绝调整层 → 导出测试失败。

        **第二步（2026-10-19）**：合成器做成 **HDR 感知**，整条链通了。
        - `CompositeDeep` 先加载各层栅格再决定累积栅格：**只要有一层是 HDR，累积栅格就是 HDR**
          （否则超白值在 source-over 里被截断）。原先「HDR 图层直接拒绝」那条删掉了。
        - `deepSourceOver` / `deepScaleAlpha` 在 HDR 上**解码半浮点后算**：两层 HDR 叠（上层预乘 0.5、
          底层 4.0）得 2.5；直接对位模式算会得到别的数。
        - `sixteenBitExportRaster` 在**合成结果**是 HDR 时仍然拒绝（16 位 PNG/TIFF 装不下超白值），
          而 `hdrExportRaster` 改成先试深层合成 —— EXR 于是能吃「HDR 层 + 调整层」。

        **变异审计逼出的两个测试缺口**（值得记一笔）：第一版只有单层 HDR 夹具，dst 是透明的，
        source-over 退化成复制，于是把 HDR 分支删掉测试**照样通过**；不透明度又都是 1，没走到 alpha
        缩放。补了 `TestDeepSourceOverBlendsHalfFloats`、`TestDeepScaleAlphaScalesHalfFloats`、
        `TestDeepCompositeBlendsTwoHDRLayers` 之后，两个变异都被抓住。

        端到端：`TestExportAsEXRAppliesAnAdjustmentLayerOverHDR` —— 4.0 经调整层 -2 档，EXR 峰值
        ≈2.22；把它改回「不试深层合成」，峰值落到 **0.537**（正是 8 位裁断后的值），测试立刻失败。
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
