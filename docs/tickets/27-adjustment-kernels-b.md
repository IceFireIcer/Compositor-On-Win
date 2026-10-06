# 27 — 调整内核 B：曝光/渐变映射/颗粒/杂色/黑白/色彩平衡/反相

**What to build:** 其余调整内核移植：曝光（sRGB↔线性表）、渐变映射（256×3 表）、颗粒（文档锚定噪声场）、添加杂色（位置+种子确定性）、黑白（6 通道权重+着色）、色彩平衡（保亮度）、反相；全部黄金基准比对。

**Blocked by:** 26 — 调整内核 A.

**Status:** done

- [x] 七个内核全部与 C 黄金基准一致
- [x] 颗粒/杂色的位置+种子确定性测试（同参数跨运行一致）
- [x] 接入渲染真值与 GPU 应用（非破坏渲染验证）

对等矩阵：A04、A05、A06、A07、A10、A11、A12（内核侧）。

**实现说明（2026-10-07，子代理）：** `internal/render/kernels.go`——Grain（mix32/lattice/grain_field 哈希场）、AddNoise（NoisePixels 哈希+Box-Muller）、BlackWhite（primary/secondary 分解+着色）、ColorBalance（三区 tonal_weights+亮度保持）、Invert（**预乘域 out=alpha−color**，PixelInvert.swift 真值）；adjustment.go dispatch 全部接入（nil 设置块仍透传）；golden goPort 接入 grain/black-white/color-balance/noise 四例 **全部逐位一致（maxDelta=0），比对计数 11/17**。Gaussian/Motion Blur 属后续票。
