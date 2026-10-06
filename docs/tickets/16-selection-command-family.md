# 16 — 选区命令族与色彩范围

**What to build:** 选区全部命令：全选/取消/反向、载入图层像素、载入蒙版黑色区域、扩展/收缩/羽化（蒙版域操作）、变换选区（⌘T 选区态）、选区剪贴板；色彩范围（`color_range_mask` Go 移植 + 采样含/排除色表单）。

**Blocked by:** 15 — 选区工具（选框/套索/魔棒）.

**Status:** done

- [x] 全部命令可用且有选区存在性门控（SelectionEditsTests 语义）
- [x] 色彩范围输出与 C 黄金基准一致；表单含取样/羽化度
- [x] 变换选区：缩放/旋转选区框本身且不触碰像素
- [x] 选区内像素剪切/复制/粘贴（内部剪贴板）往返正确

对等矩阵：C26–C31、S03、S04、S05、C45（选区侧）。

**实现说明（2026-10-06）：** **色彩范围黄金基准**——C 驱动新增 color_range 命令（include/exclude 颜色表 + fuzziness + invert），cases/color-range.json（两 include 色 + 一 exclude 色、fuzziness 40），goPort 接入后与 C 参考 **ε=0 逐位一致**（7/17 比对）；Go 侧 ColorRangeMask 已随票 15 移植。**命令族** `lib/state/selectionCommands.ts`（Swift Selection.swift 语义移植到蒙版域）：全选/取消/反向（全选的反向=nothing，Photoshop 语义）、扩展/收缩（8 连通形态学 ±amount，画布裁剪，收缩过头=显式空选区）、羽化（标量属性 √(f²+a²) ≤250，可叠加——Swift featherSelection 语义，非蒙版模糊）、变换选区（仿射逆映射重采样蒙版，像素不动）、内部剪贴板（copy 裁 bbox 戳/cut 清透明/paste 预乘 Normal 合成含画布裁剪）；全部命令带存在性门控（无选区/空选区→null）。Selection 增加可选 feather 字段。前端 178 测试全绿、svelte-check 0 错误。**形态学近似说明**：Swift 的圆角描边带在蒙版域以 Chebyshev 形态学近似（模块头注释），一致性测试族随 CPU 选区真值落地时收紧。变换选区的旋转手柄 UI 与 ⌘T 会话接线随票 17 变换票合流。