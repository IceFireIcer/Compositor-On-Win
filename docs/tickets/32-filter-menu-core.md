# 32 — 滤镜菜单核心

**What to build:** 滤镜菜单全部条目（10 项）：高斯模糊/运动模糊（含溢出层边缘语义）、添加杂色、晕影、辉光（Bloom）、色调对比、镜头校正、Camera Raw、移除背景（占位至 37 号票）、抖动（占位至 36 号票）；滤镜以一次性破坏操作作用于像素并进入撤销。

**Blocked by:** 09 — CPU 合成器真值; 26 — 调整内核 A; 27 — 调整内核 B.

**Status:** done

- [x] 高斯/运动模糊溢出语义（FilterTests 语义）与晕影/辉光/色调对比/镜头校正黄金基准一致
- [x] 滤镜菜单项与原版一一对应（C37–C40）
- [x] 滤镜作用于选区时有正确限制
- [x] 全部可撤销

对等矩阵：C37–C40、故事 40（除两个占位项）。

**实现记录**：
- 内核（internal/render/filters.go）：ApplyGaussianBlur（可分离真高斯，
  透明补边——配合层网格 grow 实现原版"向外扩散"语义）、ApplyMotionBlur
  （ oriented 盒平均，半径 = 距离×(1/√12) 对齐 CIMotionBlur 的扩散换算，
  角度 y-up→位图 y-down 翻转）、ApplyBloom（CIBloom 语义近似：模糊增量
  只加不减 × intensity=amount/50）、ApplyColoredVignette 与
  ApplyTonalContrast（AdjustPixels.c 逐行移植，黄金基准验证）。
- 溢出语义：blurMargin（高斯 3σ+2 / 运动 距离/2+2 / 辉光 3r+2）→
  padBitmap 长大网格 → 内核 → selection 混合 → bitmapAlphaBounds 裁回 +
  trimTransform 调整变换（Swift growForBlur/trimmed 移植）——模糊真正
  越出层界，撤销同时还原网格与变换。
- 黄金基准：colored_vignette（ε=1，hypot 跨库）+ tonal_contrast（driver
  与 Go 镜像 box_blur_rgba 生成模糊基准，ε=1，tanh 跨库）。
- 桥（internal/bridge/filter.go + filter_session.go）：ApplyFilter 一次性
  破坏应用（grow→内核→选区混合→裁回→历史 EndForced + 栅格日志撤销）；
  选区以文档像素灰度蒙版载荷传入，选区外逐字节还原原像素。
- 撤销：history 增 EndForced（像素变化但文档未变时强制入栈）；
  session.rasterJournal 按历史修订号记录 before/after 位图，
  UndoActive/RedoActive 跨条目还原像素。
- 菜单（MenuBar.svelte 滤镜组）：高斯/运动/添加杂色/晕影/辉光/色调对比/
  镜头校正/Camera Raw 八项可用；移除背景（37 号票）与抖动（36 号票）
  占位禁用——与原版 FilterKind 一一对应（Content-Aware Fill 属 37 号票族）。
- Camera Raw 菜单项开放 Light/Color 快速组（走 ApplyCameraRawFilter 全管线，
  scale=1 全尺寸）；完整面板随 34 号票。
