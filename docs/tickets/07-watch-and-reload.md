# 07 — 外部变更监视与原位重载

**What to build:** 已打开项目的外部写入监视：fsnotify 300ms 合并、原子重命名后重挂、内容 digest 比对（sha256）、忙等退避（250ms×2^n）、原位重载（保留缩放/平移/选区、清撤销）；有未保存编辑时询问"放弃/保留"。

**Blocked by:** 05 — .comp 存取校验与互开; 06 — 快照式撤销历史.

**Status:** done（2026-10-06，监视基础设施；第 2/3 项的应用层接线随 M2 桥接落地）

- [x] 外部写入 PNG + 原子改名 manifest 后 ~300ms 触发（watcher 合并 + coordinator digest 稳定回调，实测测试通过）
- [x] 重载保留视口与选区、清空撤销栈——**票 51 完成接线**：`internal/bridge/watch.go` 的
  `Workspace.ReloadDocument` 原地换会话（清撤销、标签不变、rev 前进），视口/选区在前端且重载不碰；
  `frontend/src/lib/state/external.ts` 有 store 层断言
- [x] 脏文档"放弃/保留"询问——**票 51 完成接线**：`external:changed`（`pending:true`）+
  `ResolveExternalChange(tabId, accept)` + `ExternalChangePrompt.svelte`；另外有编辑在飞
  （笔刷/滤镜）时也走询问而不是静默换会话
- [x] 原子重命名替换后监视继续有效（Windows fsnotify rename 语义下的重挂有实测测试）
- [x] 忙等退避不无限循环（250ms×2^n 有上限与终态，测试覆盖写入进行中场景）

对等矩阵：P05（内核）、故事 56。

> 备注：为并行开发隔离，监视器落在独立包 `internal/watch/`（通过回调与 store/history 解耦，不 import 二者）；architecture.md 已同步。
