# 11 — WebGPU 合成器

**What to build:** 前端 WebGPU 合成器：设备探测与 CPU 回退声明、纹理缓存（LRU + 内存压力响应）、Lanczos mip 链（缩小取样）、WGSL 实现 24 种混合模式（sRGB 语义）、蒙版/编组/不透明度/变换采样合成；层位图经 HTTP 语义取回为 ImageBitmap、按修订号失效。

**Blocked by:** 09 — CPU 合成器真值.

**Status:** ready-for-agent

- [ ] 复杂 .comp（组/蒙版/特效外元素）以 60fps 合成（性能基准记录）
- [ ] WebGPU 不可用时显式回退 CPU 轨且功能不缺失
- [ ] 同文档 GPU 输出与 CPU 真值一致（一致性测试协议，逐模式）
- [ ] 修订号失效机制：层变更后仅重取该层位图
- [ ] 纹理缓存 LRU 与内存压力响应测试

对等矩阵：L02/L03/L05（GPU 极）、V05、V10。
