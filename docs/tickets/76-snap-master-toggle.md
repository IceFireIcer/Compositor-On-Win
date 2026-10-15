# 76 — Snap 总开关与命令面板状态

**What to build:** 用户可用一个 View > Snap 总开关统一暂停吸附，并在命令搜索中看到总开关和各目标的当前状态。

**Blocked by:** 14 — 标尺/参考线/网格/吸附; 54 — 命令搜索面板。

**Status:** done（2026-10-15）——总开关、四个子目标、命令面板动态勾选与偏好持久化均已落地。

- [x] 总开关关闭时移动、裁剪不吸附；选区和形状创建本来就不吃 Snap（上游选区只取整到像素、形状只锁角度）。重新打开恢复各目标开关。
- [x] 菜单和命令搜索显示总开关及目标开关的动态勾选状态。
- [x] 序列化只保存用户偏好的目标设置，不把临时旁路修饰键误写成关闭总开关。

对等矩阵：U23。原版锚点：`EditorSession.swift`、`CommandPalette.swift`；提交 `bba92dc`。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `frontend` `snapPrefs.test.ts` | 四个子目标各写一个键、新会话读回；总开关保留；Ctrl 旁路不改也不写总开关；无存储时退回默认 |
| `frontend` `canvasSnap.test.ts` | 总开关关与 Ctrl 旁路都让移动不再吸附 |
| `frontend` `CommandPalette.browser.test.mjs` | 面板里总开关与四个目标的勾随 store 变化 |

## 已知差异

1. **选区与形状创建不参与 Snap**：上游 `Selection.swift:152` 的选区只「snapped to whole pixels」，`ShapeTool.swift:86` 只把角度锁到八分圆，都不读 Snap 总开关。票面要求的是「总开关关闭时它们也不吸附」，这一点成立。
2. 偏好存在 `localStorage`（键名照抄上游 `tool.` 前缀去掉后的短键）；原版是 UserDefaults，语义等价。
