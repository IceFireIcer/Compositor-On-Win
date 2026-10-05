# 36 — 抖动滤镜

**What to build:** 抖动（Dither）滤镜：11 种风格（Atkinson、Floyd–Steinberg、Bayer 2/4/8、圆点、线条、菱形、图案、字形、扫描线）的 `dither_apply` Go 移植，含半调单元/角度/字形图；参数表单接入 33 号票预览管线。

**Blocked by:** 09 — CPU 合成器真值; 21 — 笔刷引擎核心（像素提交路径）.

**Status:** ready-for-agent

- [ ] 11 种风格全部与 C 黄金基准一致（DitherTests 语义）
- [ ] 表单参数（风格/单元/角度/强度）实时预览
- [ ] 可撤销、可选区限制

对等矩阵：C39、故事 40（抖动项）。
