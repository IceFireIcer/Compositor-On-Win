# 73 — 渐进模糊滤镜

**What to build:** 用户通过 Filter 菜单在画布上拖出两点，控制从起点到终点的模糊渐变，可选线性/径向和反向。

**Blocked by:** 33 — 滤镜实时预览管线; 13 — 视口与画布导航。与 58 联合验收重复滤镜的位置，但重复滤镜无需先完成本票。

**Status:** done（2026-10-15）——内核、桥接、对话框控件与画布两点线均已落地；缩放/平移/重做上次滤镜沿用文档坐标。

- [x] 开启滤镜时显示两点定位线；拖动任一端更新预览，线性/径向、From/To、Invert 有效。
- [x] 滤镜位置以文档坐标保存于当前会话，缩放、平移和重做上次滤镜后仍对应同一图像位置。
- [x] 预览、提交、撤销与导出一致；透明边缘按既有路径门控。
- [x] 对照原版 `ProgressiveBlurTests` 的线性、径向和画布交互测试。

对等矩阵：U20。原版锚点：`Filters.swift`、`FilterSheet.swift`、`ProgressiveBlurTests.swift`；提交 `3ae11cd`。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `internal/render` `TestProgressiveBlurLinearIsSharpAtTheStartAndBlurredAtTheEnd` | 上游同名断言：起点前清晰、半程更糊、终点后糊掉 |
| `internal/render` `TestProgressiveBlurFromAndToSetEachEnd`、`...RadialIsSharpInTheMiddleAndBlurredOutside` | From/To 各自生效；径向与反相 |
| `internal/render` `TestProgressiveBlurWithNoAmountChangesNothing` | From 与 To 都是 0 时不动像素 |
| `internal/bridge` `TestProgressiveBlurOpensWithItsLineOnTheLayer` | 上游 `theLineStartsOnTheLayerAndMoves`：默认线 [100 35]→[100 100]，可移动 |
| `internal/bridge` `TestProgressiveLineIsInDocumentCoordinates` | 默认线取图层可见区域，不是整幅画布 |
| `frontend` `progressiveBlur.test.ts` | 默认落点、缺端点兜底、命中端点、Shift 锁八分圆 |

## 已知差异

1. **可变半径模糊是阶梯近似**：上游用 `CIMaskedVariableBlur`（每像素一个半径），这里用 8 档固定半径高斯按遮罩插值。语义相同，档与档之间是线性过渡而不是逐像素精确半径。
2. **16 位图层仍走 8 位内核**：`runsAtSixteenBits` 在上游含渐进模糊；这里和其它滤镜一样按票 82 处理（`runFilterOnGrid` 丢深层像素）。
