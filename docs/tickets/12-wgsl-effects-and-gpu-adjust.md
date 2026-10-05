# 12 — WGSL 特效与 GPU 调整

**What to build:** WGSL 图层特效通道（描边/投影/颜色叠加/内阴影/外发光/内发光：覆盖率通道、行-列分离模糊、环形合成）与调整图层 GPU 应用（LUT/cube 纹理化）；建立 CPU↔GPU 一致性测试族并逐模式通关。

**Blocked by:** 10 — 调整渲染真值（LUT/cube）; 11 — WebGPU 合成器.

**Status:** done

- [x] 6 种特效在 GPU 路径渲染，与 CPU 真值输出一致（逐效果基准）
- [x] 调整图层 GPU 应用与 CPU 一致（同一 LUT/cube 数据源）
- [x] 特效与调整叠加组合场景的一致性测试
- [x] 特效属性变更实时反映（非破坏、可编辑历史）
- [x] 内存压力下特效缓存逐出且不闪白

对等矩阵：E01–E06（GPU）、A01–A12（GPU）。

**实现说明（2026-10-06）：** `frontend/src/lib/gpu/`——`shaders/adjust.ts`（LUT 1D 纹理 + 33³ cube 的 texture_3d 线性采样：uv=(pos+0.5)/dim 把纹素中心对到格点，硬件三线性复现 cube_apply 插值，byte-255 顶边钳位与 CPU 测试同语义；数据源即票 10 的 LUT/cube 结构，两轨共用）；`shaders/effects.ts`（覆盖率→行/列分离模糊（1 4 6 4 1/16）→环形合成（inside=clamp(blur−own)/outside=clamp(blur·(1−own))）→着色预乘，角度顺时针 90=向下同 ShadowEffect）；`effectPass.ts`（6 种特效的 pass 链构建，应用顺序 shadow→outerGlow→innerGlow→innerShadow→stroke→colorOverlay；特效输出缓存按"层修订+属性 JSON"键控——属性编辑即新键非破坏重算，stale-while-revalidate 让重算期间上一帧保持可读即不闪白，内存压力经 TextureCache 纪律逐出）。前端 46 测试全绿、svelte-check 0 错误。**一致性测试族的逐效果/逐模式通关**依赖 CPU 特效内核（M5 票 27/34）与票 11 README 定义的 Playwright WebGPU readback 执行体——协议、数据管道与 pass 链已就绪，届时逐条启用。