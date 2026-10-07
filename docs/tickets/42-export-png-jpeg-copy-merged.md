# 42 — 导出 PNG/JPEG 与拷贝合并

**What to build:** 导出管线：Export PNG…（⇧⌘E）与 Export JPEG…（⇧⌥⌘S，实时预览表单含质量滑杆、预览内可缩放）；拷贝合并（⇧⌘C，合成后进系统剪贴板——剪贴板 UI 通道在 48 号票完成，本票先出位图）；导出用 CPU 真值。

**Blocked by:** 09 — CPU 合成器真值; 40 — 栅格导入（JPEG/PNG/TIFF/SVG）.

**Status:** done（2026-10-07）

实现注记：render/export.go — FlattenOverBackground（预乘→不透明白底，JPEG 无 alpha）+ EncodePNGWithDPI（pHYs 块）+ EncodeJPEGWithDPI（Go 编码器不写 APP0，SOI 后拼接 JFIF v1.01 units=1 段）；bridge：Begin/EndExportPreview 持有一次压平光栅、ExportJPEGPreview 重编码、ExportPNG/ExportJPEG 走保存对话框（defaultExportSaveDialog seam）+ 扩展名兜底、CopyMerged → internal/winclip（user32 CF_DIB 32bpp top-down，直通 BGRA 反预乘；seam 可替换避免测试污染剪贴板）。前端 export.ts + ExportDialog.svelte（质量滑杆/缩放/字节估算）+ ⇧⌘E/⇧⌥⌘S/⇧⌘C 快捷键 + toast。DPI 取 manifest resolution（原版 kCGImagePropertyDPIWidth 语义）。

- [x] 导出结果与 CPU 真值逐位一致（ExportTests/JPEGExportTests 语义；压平走 render.Render）
- [x] JPEG 实时预览（复用 33 号票管线）质量滑杆即时（预览 ≤2048 下采样，150ms 防抖重编码，代次防乱序）
- [x] 导出不阻塞编辑（后台任务 + 完成提示；binding 协程 + toast）
- [x] 文件名/位置记忆（localStorage compositor.export.name → DefaultFilename）

对等矩阵：C06、C07、C12、I06、故事 48。
