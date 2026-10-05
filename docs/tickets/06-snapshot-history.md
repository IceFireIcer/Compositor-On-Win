# 06 — 快照式撤销历史

**What to build:** Go 端撤销/重做：快照式 Entry（文档 + 活动图层 + revision），嵌套事务（begin/end 深度计数）、操作名命名、100 条/256MB 修剪（按不可变像素缓冲唯一引用计数）、脏标记（revision UUID vs 已保存 revision）；编辑事务与文档 API 集成。

**Blocked by:** 04 — 文档域模型与限额.

**Status:** done（2026-10-06）

- [x] 撤销/重做菜单名显示操作名（`UndoName()/RedoName()` 对齐 DocumentHistory.swift 语义）
- [x] 嵌套事务：内层提交并入外层（不重复捕获/不 bump revision/外层名胜出）；no-op 编辑不产生快照且**保留 redo 栈**
- [x] 修剪策略测试：超 100 条或超 256MB 时按序驱逐且不破坏重做链（字节驱逐测试复现 Swift byteLimit=0 下 undoCount==2 的共享资产行为）
- [x] 修改态：isModified 随编辑/undo/redo/MarkSavedRevision 正确翻转，revision UUID 可被外部比对
- [x] HistoryTests.swift 可移植语义 11 个测试全部移植通过（逐测试对照表见 internal/history/history_test.go 注释）

对等矩阵：L07、故事 57。

> 适配说明：Swift 快照依赖 CanvasDocument 值语义（COW）免费获得不可变性；Go 侧在四个快照边界用 JSON 往返深拷贝（domain 为纯 JSON 数据，位精确）。256MB 字节核算改为可注入估算器（Go 像素在磁盘 PNG、内存无位图），默认仅条目数修剪——接线到渲染层后注入真实核算。
