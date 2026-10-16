# 26 — 调整内核 A：色阶/曲线/色相饱和度

**What to build:** 像素内核移植第一批：`levels_apply`/`levels_histogram`（色阶含自动+三吸管数据）、Curves 256 级表、色相/饱和度 33³ 立方（`cube_apply`）；与黄金基准比对；接入渲染真值与 GPU（同一 LUT/cube 数据）。

**Blocked by:** 10 — 调整渲染真值（LUT/cube）.

**Status:** done

- [x] `levels_apply`/`levels_histogram`/`cube_apply` 与 C 黄金基准一致
- [x] 色阶自动（LevelsAutomatic 语义）与吸管取点计算一致
- [x] 色相/饱和度分区/分段语义（HueSaturationTests 语义）
- [x] 直方图数据可供 UI 消费（后续表单复用）

对等矩阵：A02、A03、A01（内核侧）。

## 验收证据审计（2026-10-16，第六轮：调整层与特效）**—— 本轮没查出缺口**

前五轮每轮都查出东西，这一轮**没有**。如实记下来，因为这个结论本身有意义：审计不是「总能找到问题」的
流程，它也可能回来是干净的 —— 如果每轮都必须交出「发现」，那迟早会开始制造发现。

| 变异 | 结果 |
| --- | --- |
| 调整层的交叉淡入权重对调 | ✓ 被抓住 |
| 调整层丢掉组不透明度衰减 | ✓ 被抓住 |
| 调整层跳过目录蒙版 | ✓ 被抓住 |
| 交叉淡入改为截断（丢掉 `+0.5`） | ✓ 被抓住（连取整都钉住了） |
| 特效前不再把图层蒙版作用到源上 | ✓ 被抓住 |
| 特效放置忽略内缩（`inset` 传 0） | ✓ 被抓住 |
| `hasEffects` 不再考虑 `effectsMargin` | ✓ 被抓住 |

7 处变异全部让测试失败，覆盖到 `TestRenderAdjustmentMaskAndOpacity`、`TestRenderColorOverlay*`、
`TestRenderStrokeMaskedAndScaled` 等。**这一层是真的覆盖到了**，不是「碰巧没人测」。

### 顺带查了别的票

同一轮里对 `.comp` 的版本闸门（硬性不变量 #1）也做了变异：放宽 `FormatVersion` 上限、去掉 per-letter
颜色的 v10 闸门 —— 两条都被抓住 ✓（`TestValidateV1Gates`、`TestValidateTextGates`）。

**实现说明（2026-10-07，子代理）：** levels_apply/cube_apply/Curves/HSV 分区已在票 08/10 逐位对齐；本票补缺口 `internal/render/histogram.go`——LevelsHistogram（levels_histogram 逐行，alpha+coverage 加权，RGB 均值 bin，刻意串行保浮点求和顺序）、HistogramScale（95 分位×4 展示缩放）、AutoLevels 三模式（Contrast 共享区间/Color 逐通道/Neutral 加权均值 gamma，LevelsAutomatic.swift 逐式）、吸管三语义（SampleLevelsPoint+ApplyLevelsSample，联合标定 gamma=log(f)/log(0.5)）。API 供表单复用。
