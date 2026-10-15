# 75 — 中键平移与修饰键缩放

**What to build:** 用户可在任意工具中用中键平移画布，按住 Control 的中键拖动则围绕按下点缩放。

**Blocked by:** 13 — 视口与画布导航; 47 — 快捷键系统与重映射。

**Status:** done（2026-10-15）——中键平移原已就位；本轮补上 Command/Control + 中键的按下点缩放。

- [x] 无修饰键中键拖动只改变平移；Control 中键向上放大、向下缩小（Command 同效，Windows 等价和弦）。
- [x] 缩放锚点是按下位置，结束拖动不改变当前工具或左键操作。
- [x] 光标（放大/缩小镜）与画布边界行为稳定；中键与左键手势各自独立。

对等矩阵：U22。原版锚点：`EditorCanvas.swift`、`MiddleButtonTests.swift`；提交 `e03923f`。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `frontend` `middleButton.test.ts` | 每 100 视图点翻一倍/减半；按下点处为 1；目标值夹在缩放范围内 |

## 已知差异

1. 上游 Command 与 Control 都触发缩放；Windows 侧同样接受两者（`ctrlKey || metaKey`），没有另设和弦。
2. 触控板替代输入（双指捏合）走既有滚轮/缩放路径，不在本票范围。
