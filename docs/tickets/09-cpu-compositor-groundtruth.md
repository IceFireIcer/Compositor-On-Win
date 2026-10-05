# 09 — CPU 合成器真值

**What to build:** Go 全文档合成器（导出真值与 CPU 兜底）：按底到顶合成图层（变换采样 Nearest/Smooth/High quality、不透明度、组衰减、蒙版/剪贴蒙版、24 种混合模式 sRGB 语义）、棋盘格/文档边界、输出可编码 PNG；出土"同一文档一次渲染"的单测。

**Blocked by:** 05 — .comp 存取校验与互开; 08 — 黄金基准 harness.

**Status:** done（2026-10-06；对 08 的依赖重排见备注）

- [x] 典型文档合成测试：恒等放置/不透明度衰减/蒙版（链接+解链 maskPlacement）/剪贴链/编组衰减+组蒙版/旋转 90°（顺时针 y-down 约定）各有像素级断言
- [x] 24 种混合模式逐模式有参考用例：31 个手算公式断言（PDF 32000 的 16 种 + Photoshop 扩展 8 种的标准公式，全部在 **sRGB 空间**求值——复制原版行为而非线性光"修正"，ADR-0004）；Hue/Saturation/Color/Luminosity 走 PDF 非可分离算法（SetSat/SetLum/ClipColor），亮度/色相模式有手算端到端断言
- [x] CPU 合成输出黄金比对——**先例形态就位**：本票建立"逐字节 Bitmap 对比"协议（并行 vs 串行字节数组 DeepEqual）；08 号票落地后，C 内核参考 PNG 沿同一协议接入（混合模式数学本身出自 CG/CoreImage 遵循的公开公式，不依赖 08 的 C 内核夹具，故先行实现）
- [x] 大文档合成可并行且结果与串行一致：GOMAXPROCS(1) vs 多核渲染 64×64 多层旋转文档，逐字节相等（parallelFor 分带保证每像素纯函数）
- [x] WebGPU 对比协议先例就位：Render() 返回确定性 *Bitmap（预乘 RGBA8）+ EncodePNG 直出，WGSL 管线落地后以同一 Bitmap 逐字节对比

对等矩阵：L02、L03、L05（渲染）、I06（真值源）。

> 范围说明：① 棋盘格/文档边界属查看器关注点，不在导出真值内（真值输出透明背景 RGBA）；② 调整图层暂以直通跳过——其内核随票 26（M5）接入真值路径（validate 层已保证其元数据合法）；③ 重采样 High quality 采用 2×2 旋转网格超采样双线性，待 macOS 黄金夹具到位后按需校准。
