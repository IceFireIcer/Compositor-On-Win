# 35 — 内容感知填充与污点修复

**What to build:** 内容感知填充（`content_fill` 移植：从选区外补丁合成，可延伸画布边缘）与污点修复画笔三模式（`spot_heal` 移植：内容感知/创建纹理/临近匹配，覆盖率×不透明度混合）；修复画笔复用笔刷交互（21 号票）。

**Blocked by:** 09 — CPU 合成器真值; 21 — 笔刷引擎核心.

**Status:** done

- [x] 两个内核与 C 黄金基准一致
- [x] 内容感知填充支持画布外延伸（grow 语义）
- [x] 三种修复模式行为与覆盖率混合正确（SpotHealingTests 语义）
- [x] 全部可撤销；大区域性能可接受（基准记录）

对等矩阵：S07、T07、C17、故事 21/42。

**实现记录**：
- ContentFill（render/contentfill.go）与 SpotHeal（render/heal.go）逐行移植；
  黄金基准 case 首次接入——content-fill ε=0 逐位（LCG 固定种子补丁搜索），
  heal-spot ε=1（sqrt/log/cos 颗粒跨库）。移植纠错：C 的 donorCount 从 0 起
  （donors[0] 是首个供体，我误读为 1 使 RNG 序列整体偏移）；heal_score 用真
  INFINITY + isfinite 拒收（MaxFloat64 哨兵不触发 isfinite）；修复表达式按
  C 提升序：补丁项 float 相加后再并入 double 颗粒项。
- contentAwareFill 滤镜：选区（文档像素灰度载荷）经 selectionLayerBounds
  反映射 + growBitmapTo 长大网格（可延伸画布外，expandTransformOffset 定
  位）→ selectionLayerMask 栅格化 → content_fill → AlphaBounds 裁回；无选
  区报"需要选区"。
- 污点修复画笔（J 工具）：render.HealStroke 复用笔刷交互几何（tipCoverage
  硬度衰减、dab 间距、字符串平滑）只积累覆盖率；BeginHealStroke/HealPoint/
  EndHealStroke 端点在抬笔时跑 SpotHeal（覆盖率×不透明度混合），栅格日志
  + 历史"污点修复"可撤销；工具栏出现模式（内容感知/创建纹理/临近匹配）+
  直径/不透明度选项。
- 性能：填充/修复在小网格毫秒级；全尺寸大区域基准随 52 号票统一执行。
