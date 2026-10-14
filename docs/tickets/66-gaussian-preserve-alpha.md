# 66 — Gaussian Blur 保留透明度

**What to build:** 用户可选择只模糊图层颜色、保留原始透明轮廓；关闭该选项时，满画布图层边缘仍为实色而非出现暗边。

**Blocked by:** 32 — 滤镜菜单核心; 33 — 滤镜实时预览管线。

**Status:** done（1.4.10 增量候选）

- [x] 滤镜表单提供 Preserve Transparency 开关，预览与最终提交使用相同边界处理。
- [x] 开启时 alpha 逐像素不变；关闭时图像填满画布的边缘不会卷入透明黑。
- [x] 扩展画布、选区、撤销和导出下的结果与上游用例一致。

对等矩阵：U13。原版锚点：`Filters.swift`、`FilterSheet.swift`；提交 `9a0ad3e`。

---

## 实现

上游 `9a0ad3e` 的三件事，逐条落地：

1. **半径默认 1 → 5**：`filterParams.normalized` 的高斯兜底与前端 `defaultFilterSettings` 同步。
2. **画布边缘不再往透明里糊**：新增 `render.EdgeSides`（左/右/上/下位）与
   `ApplyGaussianBlurSides` / `ApplyMotionBlurSides`——命中的边越界采样按边缘像素复制，
   其余边仍是透明。`filter_session.go` 的 `canvasEdgeSides` 把画布矩形经图层变换的逆映射
   回图层像素（仅当图层未旋转），得到「压在画布边上的那几条边」；padding 因此改为**非对称**
   （只在没被 clamp 的边上扩），`grownMargin` 记账避免重复扩。
3. **Preserve Transparency**：`filterParams.PreserveTransparency` 打开时 `blurMargin` 返回 0
   （不扩网格、提交不套用 grown 网格，图层尺寸不变），内核走
   `ApplyGaussianBlurPreservingAlpha`——对预乘结果先模糊，再 `straight = blurred.rgb / blurred.a`
   去掉透明像素的拉低，最后乘回**原** alpha。对应 Swift 的
   `unpremultiplyingAlpha → settingAlphaOne → CIBlendWithAlphaMask(edges)`。
   面板中途打开该开关也不会留下之前为扩散扩出的网格（`TestPreserveToggledOnAfterOpeningKeepsTheLayerGrid`）。

前端：`FilterDialog.svelte` 高斯分区加「保留透明度」开关（tooltip：只模糊颜色，保留图层轮廓与透明度）；
`filters.ts` 的默认半径与 wire 形状同步。

## 验证

| 测试 | 覆盖 |
| --- | --- |
| `TestGaussianBlurSidesCarryTheEdgePixelOn` / `TestMotionBlurSidesCarryTheEdgePixelOn` | 命中的边按边缘像素复制，未命中的边仍透明 |
| `TestGaussianBlurPreservingAlphaKeepsAlphaAndClearsTheDarkRim` | alpha 逐像素不变、剪影边缘不出现暗边 |
| `TestBlurKeepsAPictureFillingTheCanvasSolidAtItsEdges`（高斯 + 运动模糊） | 铺满画布的图边缘保持实色、尺寸不变；高斯下最外四列的颜色也**逐字节不变**（评审指出只查 alpha 证不出「边缘没被压暗」） |
| `TestGaussianBlurInsideTheCanvasStillSoftensIntoClearPixels` | 画布内的笔触仍向透明处柔化 |
| `TestBlurSpreadsOnlyWhereTheLayerIsInsideTheCanvas` | 只有画布内的边扩散 |
| `TestGaussianBlurPreservingTransparencyKeepsAlphaAndOutline` | 轮廓不动、alpha 不变 |
| `TestPreservedBlurPreviewCommitAndOneShotAgree` | 预览、提交、一次性应用三者一致 |
| `TestPreservedBlurStaysInsideTheSelection` | 选区下仍只在选区内生效 |
| `TestPreserveTransparencyOnAnExtendedCanvasKeepsTheLayerGrid` | 扩展画布下网格与尺寸不变 |
| `TestPreservedBlurUndoRedoAndExportConsistency` | 撤销/重做/导出一致 |
| `TestPreserveToggledOnAfterOpeningKeepsTheLayerGrid` | 打开开关后不留扩散网格 |
| 前端 vitest | 默认半径 5、`preserveTransparency` 的默认值与发送形状 |

既有 `TestApplyFilterGaussianGrowsAndUndoes`（非 preserve 的扩网格 + 撤销）保持通过，
证明未命中画布边时行为与改动前逐像素一致。

## 已知差异

- 原版在 Core Image 上做（`clampedToExtent` + `CIGaussianBlur`）；Go 侧是自研可分离高斯，
  边界语义按上游注释对齐（命中边复制、其余透明），不是逐位复现 CI 的输出。
  这与票 32 的既有约定一致。
- `scale`（预览像素/图层像素）仍作用于半径，因此缩小的预览与全尺寸提交在
  「保留透明度」与「边缘延伸」两种边界处理下都一致。
