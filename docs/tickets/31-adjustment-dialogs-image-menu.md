# 31 — 调整对话框与图像菜单入口

**What to build:** 调整 UI 全集：Levels/Curves/Hue-Saturation 表单（含直方图、吸管取点、预设）+ 其余调整表单；图像菜单破坏性入口（⌘M/⌘L/⌘U 与图像调整族）；反相（⌘I，选中蒙版时为"反相蒙版"）；调整图层属性可随时重开编辑（Edit Adjustment）。

**Blocked by:** 19 — 吸管/色板/颜色选择器/数值控件; 26 — 调整内核 A; 27 — 调整内核 B.

**Status:** ready-for-agent

- [ ] 表单参数实时预览（复用 33 号票前先用 CPU 真值直渲）
- [ ] 图像菜单入口作用于像素且可撤销；反相/反相蒙版语义正确
- [ ] 调整图层非破坏：改参数即时重渲染（HueSaturationTests/LevelsTests UI 语义）
- [ ] 数值控件（19 号票）全表单复用

对等矩阵：C32–C34、A01–A12（表单侧）、故事 37/38。
