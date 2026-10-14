# 67 — 图层特效与可编辑内容随尺寸编辑保留

**What to build:** 用户调整画布/图像尺寸、裁剪、修剪或编辑图层像素后，图层特效与可编辑内容按上游规则保留。

**Blocked by:** 43 — 画布尺寸/图像尺寸/修剪; 45 — 文本样式逐 run; 20 — 图层操作命令族与图层面板。

**Status:** done（1.4.6–1.4.9 增量候选）

- [x] 画布尺寸、裁剪、修剪、仅改分辨率保留效果、文字及形状的可编辑元数据；重采样文字时按上游规则栅格化。
- [x] 图像尺寸重采样按比例缩放特效尺寸、距离和模糊；像素调整与选区变换不意外删除效果。
- [x] 特效、文字、形状在保存重开和撤销后仍遵循上述保留或栅格化规则；半透明重新着色另见 80。
- [x] 保存重开、撤销和原版 `LayerEffectsSurviveTests`、`ColorOverlayTests` 语义对齐。

对等矩阵：U14。原版锚点：`LayerEffects.swift`、`LayerEffectsSurviveTests.swift`；提交 `94e90bd`。半透明着色见 80。

---

## 实现

上游 `94e90bd` 的根因是「画布尺寸/图像尺寸/裁剪/修剪从快照重建图层时没带上 effects / live text /
live shape」，修法是抽出一份共享的图层构造列表。Go 侧**没有**从快照重建图层的路径——各命令都
就地改 `*domain.Layer`——所以只有图像尺寸的重采样需要补，其余路径本来就是对的，用测试逐条钉住。

1. `internal/domain/effects.go` 新增 `(*Effects).Scaled(factor float64) *Effects`，逐条对齐
   `LayerEffects.scaled(by:)`（`LayerEffects.swift:142-153`）：stroke.size ≤ 500；
   shadow.distance ≤ 5000、shadow.blur ≤ 500；innerShadow 同 shadow；outerGlow.size ≤ 500；
   innerGlow.size ≤ 500。nil 接收者返回 nil；只有存在的子特效被缩放（返回副本，不动接收者）；
   angle / 颜色 / 不透明度 / inside / enabled 原样。
2. `internal/bridge/doc_geometry.go` 的 `applyImageSize`：宽或高真的变化时
   `effectScale = sqrt(sx*sy)`（非等比时取几何平均，与上游一致），逐层缩放 Effects，
   并**丢弃 `l.Text` / `l.Shape`**（重采样把文字与形状变成像素）；只改分辨率时仍提前 return，
   三样不动。

## 验证

`internal/bridge/layer_effects_survive_test.go`（11 条，逐条对应上游 `LayerEffectsSurviveTests.swift`）：

| 上游用例 | Go 测试 |
| --- | --- |
| canvasSizeKeepsEffectsTextAndShapes | `TestCanvasSizeKeepsEffectsTextAndShapes` |
| cropKeepsEffectsTextAndShapes | `TestCropKeepsEffectsTextAndShapes` |
| trimKeepsEffectsTextAndShapes | `TestTrimKeepsEffectsTextAndShapes` |
| resolutionOnlyImageSizeKeepsEffectsTextAndShapes | `TestResolutionOnlyImageSizeKeepsEffectsTextAndShapes` |
| imageSizeKeepsEffectsScaledWithTheImage | `TestImageSizeKeepsEffectsScaledWithTheImage`（4→2 stroke、8→4 glow、20→10 distance、10→5 blur、angle 不变、Text/Shape 被栅格化、Undo 复原） |
| — | `TestImageSizeStretchedScalesEffectsByMeanFactor`（48×48 → 96×24，`sx*sy = 1` → 特效不变） |
| — | `TestImageSizeScalesEffectsOnAssetlessLayers`（编组/调整层同样缩放） |
| hueSaturationKeepsEffects | `TestHueSaturationKeepsEffects` |
| levelsKeepsEffects | `TestLevelsKeepsEffects` |
| invertKeepsEffects | `TestInvertKeepsEffects` |
| transformingASelectionKeepsEffects | `TestTransformingASelectionKeepsEffects` |

`internal/domain/effects_test.go`（6 条）钉住 `Scaled` 本身：逐字段、上限截断
`min(limit, value*factor)`、nil 语义（nil bundle → nil、缺失的子特效不被造出）、
colorOverlay 原样、缩放结果不越界。

`TestEffectsSurviveSaveAndReopen` 覆盖第三条的「保存重开」：重采样 → 保存 `.comp` →
全新 workspace 打开，特效仍是缩放后的那一份。

## 顺带修掉的既有缺陷

上面这条测试第一次运行就失败了，暴露的是票 43（图像尺寸）留下的真缺陷：
`applyImageSize` 用 `newUUID() + ".png"` 给重采样后的资产改名，而 `.comp` 的命名约定
（`internal/project` 的 validate）要求 `<图层ID 大写>.png` / `<图层ID 大写>.mask.png`。
于是**任何一次图像尺寸重采样之后都无法保存**（`Store.Save` 校验直接拒绝）。
现在改为按图层 ID 取名（`layerAssetName` / `layerMaskName`），保存重开可用了。

## 已知差异

1. 上游的修复形态是「共享图层构造列表」；Go 没有那条路径，因此没有引入对应的抽象——
   用测试而不是重构来保证语义。这是形态差异，不是行为差异。
2. **选区变换**：Swift 走 FloatingSelection，合并回图层时会丢 live text/shape（只保留 effects）；
   Go 没有浮动选区，`MoveSelectedPixels` 是就地位图搬运，三样都不动。票面只要求 effects 保留，
   按 Go 现状钉住 effects。
3. 半透明像素的重新着色（Color Overlay / Inner Glow / Inner Shadow）见票 79/80。
