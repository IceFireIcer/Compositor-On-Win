# 87 — CMYK JPEG/TIFF/PDF 导出

**What to build:** 用户可将画布按选定 ICC 配置导出为 CMYK JPEG、TIFF 或单页 PDF，并嵌入配置。

**Blocked by:** 59 — 统一 Export As 与 PDF/TIFF 导出; 72 — CMYK ICC 校样与色域警告; 86 — CMYK 文件导入与配置保留。

**Status:** in-progress（2026-10-16）—— **TIFF 与 PDF 已通**；JPEG 的四通道编码器未写。

（票面原标 `blocked-by-decision`；三个阻塞票 59/72/86 均已完成，ADR-0008 已定 CMYK 校样与导出。
`colorio.ConvertToInk` 早已存在并有测试。）

- [x] 写出四通道颜色空间或 ICC profile；普通 RGB 导出路径不受影响。
      `render.EncodeCMYKTIFFWithDPI`（基线无压缩 TIFF，`PhotometricInterpretation=5`、
      `InkSet=CMYK`、ICC 走 34675 标签）与 `render.EncodeCMYKPDFWithDPI`（单张 Flate 图，
      色彩空间是 `/ICCBased` 的 N=4 流、`/Alternate /DeviceCMYK`，配置随文件走）。
      导出表单加了 CMYK 开关（只对 TIFF/PDF 显示），配置取自 `View › CMYK › 配置`，与校样共用。
      RGB 路径一行没动。
      **JPEG 未做**：四通道 JPEG 要自写熵编码器（`image/jpeg` 只写 1/3 分量）。**明确拒绝**并说明，
      而不是静默写出 RGB 冒充 CMYK。
- [x] 透明背景、页面尺寸、分辨率和 JPEG 质量按导出表单处理。
      油墨没有 alpha，所以先按背景色压平（表单里 TIFF+CMYK 也会显示背景控件，与 PDF/JPEG 一致）；
      分辨率走 TIFF 的 XResolution/YResolution 与 PDF 的 MediaBox（页面尺寸 = 像素 × 72 / dpi）。
      JPEG 质量与 CMYK 无关（JPEG 未做）。
- [x] 用带配置的样本核验 profile 嵌入与重开颜色。
      **闭环测试**：`render` 写出的 CMYK TIFF 由**票 86 的读取器**读回来 —— 油墨平面逐字节相同、
      配置原样回来、按同一份配置转回 RGB 后各像素主通道不变（白与黑这两个端点基本精确）。
      按 ADR-0008 §5 取结构性断言，不钉死数值。

## 已知差异

**油墨约定**：本仓读写两侧都按 ICC/PDF 约定（0 = 不上墨，255 = 满墨，`ConvertToInk` 的现有测试
就钉着「纸白是 0 墨、黑是满墨」）。**Photoshop 写出的 CMYK TIFF 用的是反相约定**（255 = 不上墨），
所以与真实 Photoshop 文件的互通**尚未用真文件核对过**。要确认需要一份 Photoshop 导出的 CMYK TIFF；
在没有它之前，猜一个极性比记下这条差异更糟。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `TestCMYKTIFFRoundTripsThroughTheReader` | 写出 → 读回 → 转回 RGB：油墨逐字节相同、配置原样、主通道不变、白黑端点精确 |
| `TestExportAsCMYKPDFDeclaresItsInkSpace` | PDF 里有 `/ICCBased`、`/N 4`、`/Alternate /DeviceCMYK`，且没有 `/DeviceRGB` |
| `TestExportAsCMYKTIFFCarriesTheProfile` | 走真实导入路径 `rasterio.Decode` 读回导出的 TIFF，配置在、尺寸对 |
| `TestExportAsCMYKRefusesJPEG` | JPEG 的 CMYK 明确报错 |
| `TestExportAsCMYKNeedsAProfile` | 没有配置时报错（本机因 `Chosen()` 有回退而跳过，跳过信息说明原因） |

对等矩阵：U34。原版锚点：`ImageExporter.swift`、`CMYKTests.swift`；提交 `59025aa`。
