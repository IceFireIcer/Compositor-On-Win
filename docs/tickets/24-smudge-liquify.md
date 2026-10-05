# 24 — 涂抹与液化

**What to build:** 涂抹工具三模式（模糊/涂抹/液化）：`MetalWarp` WGSL 移植——工作副本纹理逐 dab 原位变形、液化偏移场（像素不重复重采样变糊）、抬笔回读到 CPU 并入撤销；模糊模式复用模糊语义。

**Blocked by:** 22 — WGSL 笔刷覆盖与延迟管线.

**Status:** ready-for-agent

- [ ] 三模式交互与快捷键（R 工具内切换）正确
- [ ] 液化偏移场：重复 dab 不累积模糊（SmudgeLiquifyTests 语义）
- [ ] 抬笔回读一致、可撤销

对等矩阵：T09、故事 22。
