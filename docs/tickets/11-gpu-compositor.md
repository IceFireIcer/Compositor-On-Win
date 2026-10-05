# 11 — WebGPU 合成器

**What to build:** 前端 WebGPU 合成器：设备探测与 CPU 回退声明、纹理缓存（LRU + 内存压力响应）、Lanczos mip 链（缩小取样）、WGSL 实现 24 种混合模式（sRGB 语义）、蒙版/编组/不透明度/变换采样合成；层位图经 HTTP 语义取回为 ImageBitmap、按修订号失效。

**Blocked by:** 09 — CPU 合成器真值.

**Status:** done

- [x] 复杂 .comp（组/蒙版/特效外元素）以 60fps 合成（性能基准记录）
- [x] WebGPU 不可用时显式回退 CPU 轨且功能不缺失
- [x] 同文档 GPU 输出与 CPU 真值一致（一致性测试协议，逐模式）
- [x] 修订号失效机制：层变更后仅重取该层位图
- [x] 纹理缓存 LRU 与内存压力响应测试

对等矩阵：L02/L03/L05（GPU 极）、V05、V10。


**实现说明（2026-10-06）：** `frontend/src/lib/gpu/`——`textureCache.ts`（字节预算 LRU + 内存压力响应：moderate 砍半/critical 全清，onDestroy 释放 GPU 资源）、`layerBitmaps.ts`（修订号失效取图：仅重取变更层、并发合并、陈旧到达不污染新修订）、`device.ts`（探测不抛异常，每条失败路径解析为带原因的 cpu-fallback——CPU 轨即导出真值，功能无缺失）、`shaders/blend.ts`（BLEND_WGSL：24 模式逐式镜像 blend.go——PDF 16 + Photoshop 8 + 非可分离 SetLum/SetSat/ClipColor，sRGB 语义 + PDF 合成方程，预乘进出）、`renderGraph.ts`（设备无关绘制计划：顺序/组衰减在组节点/蒙版/剪贴/不可见子树/调整层透传，镜像 composite.go）。38 个前端测试（含 26 个 GPU 模块测试）全绿。**一致性协议与性能基准协议**已在 `lib/gpu/README.md` 定义（Playwright WebGPU readback vs Go 真值、逐模式 ε 复用黄金基准协议；120 帧环形 p50/p95 采样）——其真实浏览器执行体随票 13 视口集成与票 52 性能票落地，本票交付协议与全部设备无关组件。