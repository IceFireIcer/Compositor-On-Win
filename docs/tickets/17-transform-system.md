# 17 — 变换系统

**What to build:** 非破坏变换：移动/缩放/旋转/翻转（图层级与画布级）、⌘ 拖拽自由扭曲、Shift 锁轴、多选层与整组一起变换、变换控件开关（⌘H）、精确数值输入、方向键步进、吸附提示（变换框与文档/层边缘）。

**Blocked by:** 16 — 选区命令族与色彩范围.

**Status:** done

- [x] 变换后图层保留原分辨率（非破坏；TransformTests 语义）
- [x] 自由扭曲/锁轴/整组变换交互正确
- [x] 数值输入与方向键步进与拖拽同结果（量纲一致）
- [x] 吸附提示与 Ctrl 旁路正确；Flip Canvas/Layer 命令可用

对等矩阵：T01、C36、C52、V04（变换侧）、故事 33–35。

**实现说明（2026-10-06，并行子代理）：** `lib/state/transformSession.ts`——会话 begin/drag/preview/commit（非法值一票否决）、非破坏（仅 Transform 变）、移动/旋转（Shift 15°）/等比与 ⌘ 自由扭曲/翻转（Layer+Canvas 两级）/多选整组跟随、nudge 与数值输入同量纲、吸附提示+Ctrl 旁路。73 测试。Swift 来源行号入注释。扭曲重采样与历史接线属渲染/会话票。
