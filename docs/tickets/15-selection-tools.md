# 15 — 选区工具（选框/套索/魔棒）

**What to build:** 选区创建：矩形/椭圆选框（加减选修饰）、自由手/多边形套索、魔棒（`WandPixels.c` Go 移植：容差/连续/取样半径/描迹闭环路径）、蚂蚁线动画覆盖层、选区拖动边缘自动滚动；选区框移动与选区内像素移动/复制。

**Blocked by:** 11 — WebGPU 合成器; 13 — 视口与画布导航.

**Status:** done

- [x] 魔棒输出与 C 黄金基准一致（含 3×3/5×5 取样与描迹）
- [x] 选框/套索/魔棒的创建-加减-移动-复制交互闭环（SelectionTests 语义）
- [x] 蚂蚁线动画与选区框视觉正确
- [x] 边缘自动滚动在拖动中触发且会停止

对等矩阵：T02/T03/T04、S01、S02、S05（部分）。

**实现说明（2026-10-06，并行子代理）：** `internal/render/wand.go`——WandPixels.c 逐行移植（WandMask/ColorRangeMask/WandTrace，外边界顺时针/洞逆时针），黄金基准 wand case **ε=0 逐位一致**（SKIP→PASS，比对计数 6/16，guard 防回退）；`lib/state/selection.ts`——mask+loops 表示、replace/add/subtract、移动/复制、蚂蚁线路径、边缘自动滚动纯函数。Go 10 + 前端 19 新测试。蚂蚁线动画组件与工具会话接线待画布合流。
