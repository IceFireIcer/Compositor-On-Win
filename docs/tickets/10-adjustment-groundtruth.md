# 10 — 调整渲染真值（LUT/cube）

**What to build:** 调整图层的渲染真值：从调整属性生成 1024 项 LUT 与 33³ 色彩立方（Levels/Curves 表、Hue-Saturation 分区立方、Exposure/Gradient Map 等），并应用到一个图层像素的路径；Camera Raw/Grain/Noise 等复杂组在 M5 接入（本票仅架好生成与接线）。

**Blocked by:** 09 — CPU 合成器真值.

**Status:** ready-for-agent

- [ ] 色阶/曲线/色相饱和度/曝光/渐变映射的 LUT/cube 数值与原版 Swift 生成逻辑一致（对照 reference 实现的手算用例）
- [ ] 调整图层在 CPU 合成器中正确渲染（非破坏、随时重算）
- [ ] 同一调整在 CPU 与 GPU 两轨共用同一份 LUT/cube 数据（结构定义可复用）
- [ ] 33³ 立方边界与插值语义测试

对等矩阵：A01–A05（渲染层）；后续 M5 内核票沿此接线。
