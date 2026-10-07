# 43 — 画布尺寸/图像尺寸/修剪

**What to build:** 三张文档级表单：画布尺寸（⌥⌘C，锚点九宫格）、图像尺寸（⌥⌘I，重采样质量选项）、修剪（按透明度/像素色自动裁边）；全部可撤销并写回 manifest。

**Blocked by:** 09 — CPU 合成器真值.

**Status:** done（2026-10-07）

实现注记：internal/render/canvasops.go — AnchorOffset（CanvasSizeOptions.offset 的 floor 语义）、CanvasExtension（扩展底层的彩色填充 + 旧画布交集打洞）、TrimRect（ImageTrim 三依据：alpha 边界复用 AlphaBounds、左上/右下取样色带容差）、LayerBox（旋转包围盒，与原版同 ceil/floor 半像素行为）、DrawTransformed / DrawMaskTransformed（ImageResizer 逐层重栅格化；旋转与翻转烘焙进像素，新变换为纯 origin/size；蒙版走原变换的 drawCoverage 腿，1×1 与独立 placement 保留不重采样）。bridge/doc_geometry.go 三端点：CanvasSize（图层+参考线平移、可选 Canvas Extension 层）、ImageSize（分辨率写 manifest、图层与蒙版重采样、预算校验）、Trim（渲染合成→修剪矩形→按显式 contentOffset 走画布尺寸路径）；均在单个历史事务内、可撤销。前端 geometry.ts + GeometrySheet.svelte（九宫格锚点、扩展色取色、长宽比锁定、重采样下拉、修剪四边与容差）+ 图像菜单三项 + ⌥⌘C/⌥⌘I 快捷键。对等矩阵 C35 ☑。

- [x] 三表单计算测试（CanvasSizeTests/ImageSizeTests/TrimTests 语义；锚点偏移/扩展填充层/透明与取样色修剪/重采样烘焙旋转）
- [x] 锚点/重采样质量语义正确（floor 九宫格；Nearest/Smooth/High quality 三档与原版一致）
- [x] 操作可撤销且 manifest 往返正确（单历史项；图层/参考线/蒙版位移与缩放；Canvas Extension 底层的像素同批入库）

对等矩阵：C35、故事 50。
