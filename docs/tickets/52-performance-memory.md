# 52 — 性能与内存预算

**What to build:** 性能与内存对齐：DocumentLimits 预算（RAM/16 语义在 Windows 的实现与上限测试）、大文档（100MP 级）打开/合成基准、4K 笔刷 <16ms 复测、纹理/历史缓存的内存压力逐出验证、启动到就绪基准（原版 LAUNCH_TO_READY_MEDIAN 对应指标）。

**Blocked by:** 09 — CPU 合成器真值; 11 — WebGPU 合成器; 22 — WGSL 笔刷覆盖与延迟管线.

**Status:** ready-for-agent

- [ ] 内存预算函数与限额测试（memoryBudget 语义）
- [ ] 大文档基准（打开/合成/缩放）记录并与原版 4K 笔记对照
- [ ] 内存压力注入测试：缓存逐出且应用不崩溃
- [ ] 启动到就绪中位数基准入库（CI 记录趋势）

对等矩阵：V09、V10、L08、P10。
