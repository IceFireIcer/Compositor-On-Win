# 06 — 快照式撤销历史

**What to build:** Go 端撤销/重做：快照式 Entry（文档 + 活动图层 + revision），嵌套事务（begin/end 深度计数）、操作名命名、100 条/256MB 修剪（按不可变像素缓冲唯一引用计数）、脏标记（revision UUID vs 已保存 revision）；编辑事务与文档 API 集成。

**Blocked by:** 04 — 文档域模型与限额.

**Status:** ready-for-agent

- [ ] 撤销/重做菜单名显示操作名（对应原版 undoName 语义）
- [ ] 嵌套事务：内层提交并入外层；no-op 编辑不产生快照
- [ ] 修剪策略测试：超 100 条或超 256MB 时按序驱逐，且驱逐不破坏重做链
- [ ] 修改态：编辑后 isModified=true，保存后为 false；revision 变化可被外部比对
- [ ] HistoryTests 语义移植通过

对等矩阵：L07、故事 57。
