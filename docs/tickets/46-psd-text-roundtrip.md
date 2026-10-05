# 46 — PSD 文本往返

**What to build:** PSD 文本层导出/导入往返：从样式对象重建 PSD 文本记录、字体名跨平台映射（Windows 字体 ↔ Photoshop 字体名）、与 39 号票的导入侧对齐测试。

**Blocked by:** 45 — 文本样式（逐 run）; 39 — PSD 文本/矢量与转换报告.

**Status:** ready-for-agent

- [ ] 导入→导出→再导入的文本语义等价（逐 run 保持）
- [ ] 字体缺失时的回退与报告提示
- [ ] PSDRoundTripTests 文本族语义通过

对等矩阵：X04、I05（文本导出侧）。
