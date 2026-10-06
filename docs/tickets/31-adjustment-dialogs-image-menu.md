# 31 — 调整对话框与图像菜单入口

**What to build:** 调整 UI 全集：Levels/Curves/Hue-Saturation 表单（含直方图、吸管取点、预设）+ 其余调整表单；图像菜单破坏性入口（⌘M/⌘L/⌘U 与图像调整族）；反相（⌘I，选中蒙版时为"反相蒙版"）；调整图层属性可随时重开编辑（Edit Adjustment）。

**Blocked by:** 19 — 吸管/色板/颜色选择器/数值控件; 26 — 调整内核 A; 27 — 调整内核 B.

**Status:** done

- [x] 表单参数实时预览（复用 33 号票前先用 CPU 真值直渲）
- [x] 图像菜单入口作用于像素且可撤销；反相/反相蒙版语义正确
- [x] 调整图层非破坏：改参数即时重渲染（HueSaturationTests/LevelsTests UI 语义）
- [x] 数值控件（19 号票）全表单复用

对等矩阵：C32–C34、A01–A12（表单侧）、故事 37/38。

**实现记录**：
- 图像菜单（MenuBar.svelte）破坏性入口：九种调整（Curves⌘M/Levels⌘L/HueSat⌘U/
  Exposure/Gradient Map/Grain/Add Noise/Black & White/Color Balance）+ 反相⌘I +
  反相蒙版；走 `ApplyFilter(adjust:<Kind>)`/`invert`/`invertMask` 一次性通道，
  管线与撤销与滤镜完全一致（历史 + 栅格日志）。
- 反相语义：像素 = 预乘域 out=alpha−color（render.ApplyInvert）；蒙版 =
  灰度 255−v（R=G=B 反相、alpha 保持）；选区内混合方向与滤镜一致。
- 表单实时预览：调整会话与滤镜同管线（BeginFilterEdit/UpdateFilterPreview，
  kind="adjust:<Kind>"，settings.adjustment 携带 domain.Adjustment JSON），
  CPU 真值直渲（ApplyAdjustment），≤2048px 预览源 + 全尺寸提交。
- 调整图层编辑（Edit Adjustment）：LayersPanel 调整图层出现"编辑"按钮 →
  同一对话框预填 → 确定走 `LayerOp("setAdjustment")` 更新记录（非破坏），
  合成路径按新参数重渲。调整图层会话不做像素预览（其图层无位图，预览属
  34 号票面板 + GPU 合成）。
- Levels 表单：直方图（LayerHistogram + log10 纵轴 RGB 三通道折线）+ 通道
  页签（RGB/Red/Green/Blue）+ 黑/白场与输出数值 + 三种自动（LevelsAuto：
  对比/颜色/中性）+ 吸管（armed 状态在 filters store，CanvasSurface 点击
  转发 SampleLevelsPoint/ApplyLevelsSample）。
- Curves 表单：SVG 点编辑器（拖动/双击加点/双击锚点删除，0–255 坐标），
  四通道页签；数值控件（range+number 对）全表单复用票 19 模式。
- 已知留待：Hue/Saturation 的 per-range hsvSettings 表（域内 json.RawMessage
  原样往返，主滑杆可编辑；分区表随 34 号票面板）。
