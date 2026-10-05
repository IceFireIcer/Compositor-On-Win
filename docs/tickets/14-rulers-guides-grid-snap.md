# 14 — 标尺/参考线/网格/吸附

**What to build:** 标尺（稀缺刻度、垂直旋转标签）、从标尺拖出参考线（可移动/拖回标尺删除/锁定/清除）、布局网格（间距/细分可设置的网格设置表单）、吸附系统（参考线/网格/图层/文档边界四项开关 + 总开关 + Ctrl 旁路）。

**Blocked by:** 13 — 视口与画布导航.

**Status:** done

- [x] 拖出/移动/删除参考线与锁定/清除命令可用（故事 6、7）
- [x] Snap To 四个目标分别生效且被总开关控制；Ctrl 旁路正确
- [x] 网格设置表单：间距/细分修改即时反映
- [x] 参考线随文档持久化（manifest 往返）、可撤销

对等矩阵：V03、V04、C22–C25。

**实现说明（2026-10-06，并行子代理）：** `lib/state/guides.ts`（拖拽生命周期对齐 GuideDrag——拖拽中不进文档 finish 才提交；拖回标尺删除；锁定/清除；操作日志可撤销；manifest JSON 与 domain Guide 往返等价测试）、`lib/state/snap.ts`（四目标+总开关+Ctrl 旁路，阈值 10 屏幕点=TransformSnap.distance，平票保留先出现目标）。42 新测试。标尺 UI 渲染与 viewport 换算随画布合流票落地（snap 的 tolerance 参数即缝合点）。
