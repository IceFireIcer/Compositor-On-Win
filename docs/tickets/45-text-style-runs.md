# 45 — 文本样式（逐 run）

**What to build:** 文本样式系统：字体/字号/颜色/对齐/字距/行高；工具头设置区与行内选中范围（逐 run 字体与颜色混排）；Windows 字体目录枚举与字体选择器； PSD 字体名映射表。

**Blocked by:** 44 — 文本引擎.

**Status:** ready-for-agent

- [ ] 逐 run 样式（colorRuns/fontRuns）编辑与渲染测试（TypeToolTests 样式语义）
- [ ] 字距/行高（TextKit leading/kern 语义）与原版排版对齐（抽样对照）
- [ ] 字体枚举/选择器/回退字体策略
- [ ] 样式随 manifest v11 往返

对等矩阵：X02、故事 44。
