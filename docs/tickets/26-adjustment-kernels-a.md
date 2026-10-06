# 26 — 调整内核 A：色阶/曲线/色相饱和度

**What to build:** 像素内核移植第一批：`levels_apply`/`levels_histogram`（色阶含自动+三吸管数据）、Curves 256 级表、色相/饱和度 33³ 立方（`cube_apply`）；与黄金基准比对；接入渲染真值与 GPU（同一 LUT/cube 数据）。

**Blocked by:** 10 — 调整渲染真值（LUT/cube）.

**Status:** done

- [x] `levels_apply`/`levels_histogram`/`cube_apply` 与 C 黄金基准一致
- [x] 色阶自动（LevelsAutomatic 语义）与吸管取点计算一致
- [x] 色相/饱和度分区/分段语义（HueSaturationTests 语义）
- [x] 直方图数据可供 UI 消费（后续表单复用）

对等矩阵：A02、A03、A01（内核侧）。

**实现说明（2026-10-07，子代理）：** levels_apply/cube_apply/Curves/HSV 分区已在票 08/10 逐位对齐；本票补缺口 `internal/render/histogram.go`——LevelsHistogram（levels_histogram 逐行，alpha+coverage 加权，RGB 均值 bin，刻意串行保浮点求和顺序）、HistogramScale（95 分位×4 展示缩放）、AutoLevels 三模式（Contrast 共享区间/Color 逐通道/Neutral 加权均值 gamma，LevelsAutomatic.swift 逐式）、吸管三语义（SampleLevelsPoint+ApplyLevelsSample，联合标定 gamma=log(f)/log(0.5)）。API 供表单复用。
