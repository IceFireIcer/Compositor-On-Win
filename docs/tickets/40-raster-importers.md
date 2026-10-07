# 40 — 栅格导入（JPEG/PNG/TIFF/SVG）

**What to build:** 图像导入管线：JPEG/PNG/TIFF 解码（Go 标准库 + TIFF 扩展）、EXIF 方向、SVG 前端光栅化；导入落点（新文档/新图层）；"导入图像…"菜单与拖放通道共用（拖放 UI 在 48 号票）。

**Blocked by:** 05 — .comp 存取校验与互开.

**Status:** done（2026-10-07）

实现注记：internal/rasterio（JPEG/PNG/TIFF + 手写 EXIF/IFD0/eXIf 方向解析，0–8 全变换表测）；SVG 按票走前端 WebView2 栅格化（`frontend/src/lib/state/svg.ts`）；bridge Begin/Finish 两段式批次（Begin 解码入缓冲并回报逐文件状态，Finish 收前端 SVG 栅格后一次提交）。顺带修复 render.BitmapFromImage 对 color.Color RGBA() 已预乘值再乘 alpha 的双预乘 bug（现有金测全不透明所以未暴露）。依赖：golang.org/x/image（BSD-3，TIFF）；EXIF 与 SVG 无新依赖——选型见 docs/dependencies.md 与 ADR-0006。

- [x] 各格式正确解码（色彩/透明度/EXIF 方向测试）
- [x] SVG 一次栅格化语义与原版一致（decode 语义；WebView2 前端栅格化，适配画布/声明尺寸）
- [x] 大图导入流式且不阻塞 UI；失败可操作报错（binding 协程解码，逐文件失败累积为一条弹窗）
- [x] 新建文档与追加图层两条路径可用（首图定画布/居中追加，单一"导入图像"历史项）

对等矩阵：I01、I03、C04。

审查修复（2026-10-07，code-review 双轴）：SVG 声明尺寸在栅格化前按 maxSide/像素预算校验（decodeSVG 语义）；SVG 源码与栅格改走 HTTP 像素面（GET /pixel/stage、PUT /pixel/upload），不再 base64 走 JSON 桥（ADR 架构 §3.3 红线）；像素预算改用原版公式 min(800M, max(200M, RAM/16))；对话框补上 RAW/HEIC 全部扩展名（原票 41 的格式此前选不到）。
