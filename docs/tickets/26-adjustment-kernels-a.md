# 26 — 调整内核 A：色阶/曲线/色相饱和度

**What to build:** 像素内核移植第一批：`levels_apply`/`levels_histogram`（色阶含自动+三吸管数据）、Curves 256 级表、色相/饱和度 33³ 立方（`cube_apply`）；与黄金基准比对；接入渲染真值与 GPU（同一 LUT/cube 数据）。

**Blocked by:** 10 — 调整渲染真值（LUT/cube）.

**Status:** ready-for-agent

- [ ] `levels_apply`/`levels_histogram`/`cube_apply` 与 C 黄金基准一致
- [ ] 色阶自动（LevelsAutomatic 语义）与吸管取点计算一致
- [ ] 色相/饱和度分区/分段语义（HueSaturationTests 语义）
- [ ] 直方图数据可供 UI 消费（后续表单复用）

对等矩阵：A02、A03、A01（内核侧）。
