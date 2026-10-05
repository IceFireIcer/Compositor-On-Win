# 40 — 栅格导入（JPEG/PNG/TIFF/SVG）

**What to build:** 图像导入管线：JPEG/PNG/TIFF 解码（Go 标准库 + TIFF 扩展）、EXIF 方向、SVG 前端光栅化；导入落点（新文档/新图层）；"导入图像…"菜单与拖放通道共用（拖放 UI 在 48 号票）。

**Blocked by:** 05 — .comp 存取校验与互开.

**Status:** ready-for-agent

- [ ] 各格式正确解码（色彩/透明度/EXIF 方向测试）
- [ ] SVG 一次栅格化语义与原版一致（decode 语义）
- [ ] 大图导入流式且不阻塞 UI；失败可操作报错
- [ ] 新建文档与追加图层两条路径可用

对等矩阵：I01、I03、C04。
