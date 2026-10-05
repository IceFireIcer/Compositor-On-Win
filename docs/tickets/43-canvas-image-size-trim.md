# 43 — 画布尺寸/图像尺寸/修剪

**What to build:** 三张文档级表单：画布尺寸（⌥⌘C，锚点九宫格）、图像尺寸（⌥⌘I，重采样质量选项）、修剪（按透明度/像素色自动裁边）；全部可撤销并写回 manifest。

**Blocked by:** 09 — CPU 合成器真值.

**Status:** ready-for-agent

- [ ] 三表单计算测试（CanvasSizeTests/ImageSizeTests/TrimTests 语义）
- [ ] 锚点/重采样质量语义正确
- [ ] 操作可撤销且 manifest 往返正确

对等矩阵：C35、故事 50。
