# ADR-0004: WebGPU 从第一天开始 + Go CPU 真值双轨

- 状态：已接受（2026-10-05，用户确认）
- 背景：原版渲染是双轨——GPU 快路径（Core Image over Metal + 自写 compute 内核：图层特效/笔刷覆盖/扭曲/杂色）+ CPU 真值路径（Core Graphics 合成，导出与兜底）。Windows 版必须重建等价物。

## 决策

1. **屏上合成从 M2 起即 WebGPU/WGSL**：24 种混合模式、图层特效通道、调整应用（LUT/cube）、mip 降采样、笔刷覆盖核、扭曲核全部 WGSL 实现，对齐原版 Metal 内核算法。
2. **Go CPU 合成器同步建设**（`internal/render`）：导出真值 + WebGPU 不可用兜底 + GPU 疑难的对仲裁基准。
3. **数值纪律**：混合模式在 sRGB（非线性）空间计算——逐模式复制原版行为（包括原版刻意复刻的 Photoshop 偏差），禁止按"物理正确"的线性光实现改写。
4. 启动时 WebGPU 特性探测；不可用静默走 CPU 轨（不阻塞应用启动）。

## 理由

- 用户明确选择"WebGPU 从第一天开始"：避免 Canvas2D 先行导致后期二次重写特效/混合管线（Canvas2D 缺 8 种模式且数值不可控）。
- 双轨是原版已验证的架构：GPU 轨给交互速度，CPU 轨给正确性锚点与导出；两轨一致性测试是回归网。
- WebView2 常青运行时已带 WebGPU；Wails 的 WebView2 版本随系统更新。

## 备选与放弃原因

- Canvas2D 先行：落地快，但 `globalCompositeOperation` 覆盖不了 Linear Burn/Vivid Light/Pin Light/Hard Mix/Subtract/Divide 等，且原版的"CG 算错所以走 Core Image 修正"类数值问题无解——M5/M8 还得重写。
- 纯 CPU（Go 合成 + 位图推送）：实现最简，但交互帧率与笔刷延迟上限低，4K 大文档不可接受。

## 后果

- WGSL 与 Metal 数值差异是最大风险源——黄金基准 + CPU↔GPU 一致性测试（testing.md §1 缝合点 4）为强制门禁。
- 纹理上传/回读策略：层位图经 HTTP 语义零拷贝（ImageBitmap），修订号失效；瓦片上传只传脏区（对位原版 blit 瓦片写入）。
- 需维护一份"模式 × 实现矩阵"（24 模式 × WGSL/CPU/Golden），作为 M2 出口的可视证据。
