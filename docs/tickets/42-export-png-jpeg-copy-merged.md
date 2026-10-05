# 42 — 导出 PNG/JPEG 与拷贝合并

**What to build:** 导出管线：Export PNG…（⇧⌘E）与 Export JPEG…（⇧⌥⌘S，实时预览表单含质量滑杆、预览内可缩放）；拷贝合并（⇧⌘C，合成后进系统剪贴板——剪贴板 UI 通道在 48 号票完成，本票先出位图）；导出用 CPU 真值。

**Blocked by:** 09 — CPU 合成器真值; 40 — 栅格导入（JPEG/PNG/TIFF/SVG）.

**Status:** ready-for-agent

- [ ] 导出结果与 CPU 真值逐位一致（ExportTests/JPEGExportTests 语义）
- [ ] JPEG 实时预览（复用 33 号票管线）质量滑杆即时
- [ ] 导出不阻塞编辑（后台任务 + 完成提示）
- [ ] 文件名/位置记忆

对等矩阵：C06、C07、C12、I06、故事 48。
