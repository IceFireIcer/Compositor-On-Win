# 07 — 外部变更监视与原位重载

**What to build:** 已打开项目的外部写入监视：fsnotify 300ms 合并、原子重命名后重挂、内容 digest 比对（sha256）、忙等退避（250ms×2^n）、原位重载（保留缩放/平移/选区、清撤销）；有未保存编辑时询问"放弃/保留"。

**Blocked by:** 05 — .comp 存取校验与互开; 06 — 快照式撤销历史.

**Status:** done（2026-10-06，监视基础设施；第 2/3 项的应用层接线随 M2 桥接落地）

- [x] 外部写入 PNG + 原子改名 manifest 后 ~300ms 触发（watcher 合并 + coordinator digest 稳定回调，实测测试通过）
- [ ] 重载保留视口与选区、清空撤销栈——**基础设施就绪**（coordinator 提供 OnStable(digest) 回调，接线方调用 domain 重载 + history.Reset 即可）；应用层接线随 M2 画布/桥接落地（ExternalChangeTests 语义届时移植）
- [ ] 脏文档"放弃/保留"询问——同上，属桥接/UI 层
- [x] 原子重命名替换后监视继续有效（Windows fsnotify rename 语义下的重挂有实测测试）
- [x] 忙等退避不无限循环（250ms×2^n 有上限与终态，测试覆盖写入进行中场景）

对等矩阵：P05（内核）、故事 56。

> 备注：为并行开发隔离，监视器落在独立包 `internal/watch/`（通过回调与 store/history 解耦，不 import 二者）；architecture.md 已同步。
