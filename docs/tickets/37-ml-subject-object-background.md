# 37 — ML 主体选择/对象选择/移除背景

**What to build:** Apple Vision 三项能力的 Windows 替代（ADR-0005）：ONNX Runtime + 开源分割模型（u2net/ISNet 系，许可/体积定案归档）；GuidedMatte 引导滤波抠边移植 + 边缘偏移/对比精修；接入对象选择（点选描摹）、选择主体（⌥⌘A）、移除背景（滤镜）。

**Blocked by:** 16 — 选区命令族与色彩范围; 21 — 笔刷引擎核心.

**Status:** ready-for-agent

- [ ] ONNX 推理管线（Go 侧后台执行 + 进度事件）与模型分发方案定案
- [ ] GuidedMatte 抠边与精修链有数值测试（蒙版域操作）
- [ ] 三个功能入口（对象选择/主体/移除背景）端到端可用
- [ ] 模型体积与许可决策记录进 ADR-0005 附录

对等矩阵：T04（对象侧）、C28、C42、S06、故事 28。
