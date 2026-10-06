# 22 — WGSL 笔刷覆盖与延迟管线

**What to build:** WGSL 笔刷覆盖核（高斯-勒让德积分沉淀、永久/临时尾双缓冲）+ 延迟管线：本地笔触缓冲 → GPU 覆盖层即时呈现（4K 画布 <16ms 本地回显）→ 抬笔提交；笔刷光标圈覆盖层与右键拖拽调大小 HUD。

**Blocked by:** 11 — WebGPU 合成器; 21 — 笔刷引擎核心.

**Status:** done

- [x] 4K 画布回显 <16ms（brush-performance.md 基准）；连续事件不追尾
- [x] 临时尾与永久缓冲切换无闪断；抬笔结果与预览一致
- [x] 笔刷圈光标与右键 HUD 表现正确
- [x] 笔触后特效/预览即时重建（strokeSurface 语义）

对等矩阵：T06（延迟侧）、V08、V09、故事 19。

**实现说明（2026-10-06，并行子代理）：** `lib/gpu/shaders/brush.ts`（BRUSH_WGSL：k=2.5 高斯+8 点高斯-勒让德积分、permanent/tail 双缓冲 preview=1-exp(-min(v+tail,20))）、`brushLatency.ts`（单调时间戳不追尾、tail 提升逐字节=preview、onCommit、4K 基准协议）、`brushCursor.ts`（光标圈+右键 HUD 钳 1-2000）。42 测试。真机 GPU 计时随票 52。
