# 10 — 调整渲染真值（LUT/cube）

**What to build:** 调整图层的渲染真值：从调整属性生成 1024 项 LUT 与 33³ 色彩立方（Levels/Curves 表、Hue-Saturation 分区立方、Exposure/Gradient Map 等），并应用到一个图层像素的路径；Camera Raw/Grain/Noise 等复杂组在 M5 接入（本票仅架好生成与接线）。

**Blocked by:** 09 — CPU 合成器真值.

**Status:** done

- [x] 色阶/曲线/色相饱和度/曝光/渐变映射的 LUT/cube 数值与原版 Swift 生成逻辑一致（对照 reference 实现的手算用例）
- [x] 调整图层在 CPU 合成器中正确渲染（非破坏、随时重算）
- [x] 同一调整在 CPU 与 GPU 两轨共用同一份 LUT/cube 数据（结构定义可复用）
- [x] 33³ 立方边界与插值语义测试

对等矩阵：A01–A05（渲染层）；后续 M5 内核票沿此接线。

**实现说明（2026-10-06）：** `internal/render/lut.go`（LUT 结构与 Levels/Curves/Exposure/Gradient Map 生成，size 参数化：CPU 真值 256、GPU 轨 1024）、`cube.go`（hsvSettings 解析视图 + HueBand 权重 + hueResponse + 33³ 立方，`Cube` 结构 CPU/GPU 共用）、`apply.go`（`levels_apply`/`cube_apply`/`adjust_gradient_map` 三个 C 内核的移植）、`composite.go`（调整图层接线：对 below 合成应用 → 非普通混合模式先"不透明化"按模式混合再回贴覆盖率 → 自身蒙版×不透明度的预乘域线性交叉淡化）。M5 内核类（Grain/Add Noise/两种模糊及 B&W/Color Balance/Invert）原样透传直至对应票落地。测试覆盖手算数值用例、立方边界（byte 255 → lo 钳位 31/fraction 1）与插值、半透明往返、混合模式/蒙版/不透明度/组内/透传集成。
