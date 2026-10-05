# 02 — 编辑器外壳 UI 与新建画布

**What to build:** 单窗口编辑器外壳：顶部工具栏与标签条（空态）、左侧工具轨（16 个工具入口含无工具态）、状态栏、右侧图层面板容器；欢迎屏/新建画布表单（宽度/高度/分辨率），可新建空白文档并以棋盘格与文档边界显示。

**Blocked by:** 01 — 脚手架与开发链路.

**Status:** done（2026-10-05）

- [x] 工具栏/标签条/工具轨/状态栏/图层面板容器按原版布局呈现（TabStrip/NewCanvasSheet/CanvasSurface 组件 + 暗色主题）
- [x] 新建画布表单校验（DocumentLimits：边长 ≤30,000px、单面 ≤200M 像素、分辨率 1–9600ppi）错误提示直接来自 Go 校验（domain.ValidateNewDocument，含边界值测试）
- [x] 新建后画布显示棋盘格与文档边界（1:1 滚动舞台，视口缩放归票 13），状态栏显示文档名/尺寸/分辨率与"未保存"标记
- [x] 窗口尺寸持久化（bridge.WindowStore → %ConfigDir%/compositor-on-windows/window.json；OnStartup 恢复，前端 resize 防抖 400ms 保存；丢失/损坏文件降级处理有测试）
- [x] 标签条可新建/切换/关闭标签（关闭活动标签自动激活相邻标签；脏标签关闭前 confirm；关闭最后一个回到欢迎屏；快照式状态同步有测试）

对等矩阵：P06（暗色 UI/多标签）、故事 9/10。
