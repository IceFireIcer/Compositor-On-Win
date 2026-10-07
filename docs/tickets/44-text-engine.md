# 44 — 文本引擎

**What to build:** 文本工具基础：段落文本框（拖拽创建、拖拽/缩放框体、内联多行编辑、⌘回车提交、Esc 取消）、IME 输入（WebView2 原生）、文本光栅化进图层、文本作为剪贴蒙版目标、随文档保存往返。

**Blocked by:** 09 — CPU 合成器真值; 13 — 视口与画布导航.

**Status:** done（2026-10-07）

实现注记：Go 侧 internal/bridge/text.go — TextCommit 端点（新建/编辑两条路径：点文本把首基线放在指针上（origin = anchor − padding、y = anchor − (padding+lineHeight−descent)），段落框把左上角放在拖拽起点；编辑复用原资产名替换像素并保留 origin/rotation/flip，尺寸跟随新光栅）。样式校验覆盖 LayerTextStyle.isValid 的范围（字号 1–2000、颜色 0–1、字距 −100–1000、行高 0–5000、框 ≥16px 且表面预算内）。layerrender.TextBaselineInset 提供点击基线公式。前端：text.ts 会话 store（点按/拖拽/命中现有文字层/提交/取消 + 样式默认值 + clampOption/渠道↔hex）；CanvasSurface 内联 textarea overlay（真实编辑器承载 IME、⌘回车/Esc、拖拽虚线框预览）；TextOptionsBar 工具选项栏（字体名/字号/颜色/对齐/字距/行高）；文档 store 解析 text/transform 字段。测试：bridge 6 例（点文本/段落框/编辑既有层/空提交/校验/剪贴蒙版逐像素）+ 前端 7 例 + project store 文本层深比较往返。字体缺省回落与逐 run 混排随票 45。对等矩阵 X01 ☑。

- [x] 创建/编辑/提交/取消交互闭环（TypeToolTests 基础语义：点按建点文本/拖拽建段落框/再点既有文字层进入编辑/⌘回车提交/Esc 取消）
- [x] 中英混排 + IME 输入正常（内联编辑用真实 textarea 承载，WebView2 原生 IME；渲染层中英混排走 sfnt 逐字形——手动验收待真机走查记录）
- [x] 文本层参与合成/变换/剪贴蒙版（文字层就是普通像素层 + Text 元数据，合成器无需特例；测试覆盖剪贴蒙版逐像素）
- [x] 文本随 .comp 保存往返（manifest text 记录深比较往返测试：内容/字体/字号/颜色/对齐/字距/行高/段落框）

对等矩阵：X01、故事 43。
