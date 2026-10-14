# 59 — 统一 Export As 与 PDF/TIFF 导出

**What to build:** 用户从同一导出窗口选择 PNG、JPEG、TIFF 或单页 PDF，调整尺寸并检查可缩放预览后保存。

**Blocked by:** 42 — 导出 PNG/JPEG 与拷贝合并。

**Status:** done（2026-10-14）

实现注记：render/export.go — ResampleBitmap（缩小走 DownscaleBitmap 盒式平均、放大走预乘空间双线性；1:1 返回副本）+ EncodeTIFFWithDPI（golang.org/x/image/tiff 写 Deflate 基线 RGBA8，NRGBA 让 x/image 标 ExtraSamples=2 非关联 alpha，再把 IFD 里写死的 72×72 XResolution/YResolution 有理数原地改成文档 DPI；ResolutionUnit 已是 2 英寸）+ EncodePDFWithDPI（手写单页 PDF：Catalog/Pages/Page + 一张无损 Flate /DeviceRGB 图像 XObject + 内容流，MediaBox = 像素 × 72/dpi 点，按选定背景先压平、不带 SMask）。bridge/export.go — ExportAs/ExportAsPreview（格式表 png/jpeg/tiff/pdf，目标像素或原尺寸，DocumentLimits 校验，缩放后编码；预览长边 ≤2048 暂存像素面，PNG/TIFF 保留透明、JPEG 出真实压缩字节、PDF 出压平 PNG），exportFile 抽成 writeExport 供快捷入口与 Export As 共用；BeginExportPreview 回报分辨率。前端 export.ts — ExportKind 扩到四格式、EXPORT_FORMATS/PRIMARY/MORE、linkedExportSize/exportSizeForScale/exportScalePercent 纯函数、openExportAs、commitExport、rememberedQuality（localStorage compositor.export.quality）、previewSeq 序号守卫（旧预览不覆盖新结果）；ExportDialog.svelte 重写为统一窗口（JPEG/PNG 分段 + 更多格式区、宽高联动 + 缩放百分比、JPEG 质量、背景色、PDF 页尺寸、透明棋盘格可缩放预览）。wailsjs 加 ExportAs/ExportAsPreview（字母序）。

- [x] Export As 提供格式、宽高等比缩放、JPEG 质量和透明棋盘格预览；JPEG/PNG 为首要格式，PDF/TIFF 放在更多格式区域；既有 PNG/JPEG 快捷入口保持可用
- [x] TIFF 保留 RGB 透明度；PDF 按选择的背景压平，写入尺寸与分辨率，保存后可重新打开核验
- [x] 大图预览可缩放且旧请求不会覆盖新结果；导出不修改项目或脏状态
- [x] 16 位、EXR 与 CMYK 选项分别由 69、71、72 号票承担；本票只覆盖当前 RGB 8bit 路径

**已知差异（评审登记）**：① 尺寸输入只按单边 30000 夹紧（`EXPORT_MAX_SIDE`），没有像原版
`setScale` 那样同时夹 MaxSurfacePixels；超面积时由 Go 以「像素超限：…」明确报错，而不是静默夹到
上限。② 预览暂存长边封顶 2048（原版 8192），所以超大图在 100% 下看的是暂存像素而不是导出像素；
弹窗会标注预览尺寸，要逐像素检查请用「实际像素」或直接导出。

**接线（Lead 集成）**：`MenuBar.svelte` 的 文件 菜单在「导出 PNG… / 导出 JPEG…」之后加「导出为…」
（`openExportAs()`，既有两个快捷入口保持可用）；定义表按原版补 `Menus:Export As`（⌥⇧⌘W →
Windows Ctrl+Alt+Shift+W，Menus 49 / 全表 117），动作表注册 `Menus:Export As`。命令搜索面板
（票 54）自动收录这条菜单项。

对等矩阵：U06。原版锚点：`ExportAsSheet.swift`、`ImageExporter.swift`、`ExportTests.swift`；提交 `9287cf1`、`450af5f`。
