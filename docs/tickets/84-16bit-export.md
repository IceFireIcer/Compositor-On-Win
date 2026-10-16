# 84 — 16 位 PNG/TIFF 导出

**What to build:** 用户可从 Export As 导出保留 16 位通道精度的 PNG 或 TIFF，并继续使用普通 8 位导出。

**Blocked by:** 59 — 统一 Export As 与 PDF/TIFF 导出; 68 — 决策门槛与 .comp v12 / 16 位图层存取。

**Status:** in-progress（2026-10-16）—— 编码器与单图层那条路已落地，**多图层的 16 位合成未做**。

- [x] 16 位层导出 PNG/TIFF 不先压成 8 位；普通 RGB 导出仍按既有路径工作。
      编码器：`EncodePNG16WithDPI` / `EncodeTIFF16WithDPI`（`internal/render/export16.go`），
      与 8 位路径共用 pHYs 注入与「深层栅格 → 直通 alpha NRGBA64」的换算。
      导出：`ExportAs` 的 `sixteenBit`，**只在「拼合结果就是某层的深层栅格」时可用** ——
      单图层、深层栅格、恒等变换、无蒙版/剪贴/特效、正常混合、不透明度 1、尺寸等于画布。
      普通 RGB 导出完全没动。
- [x] 导出预览、尺寸、透明度、分辨率和取消行为与最终文件一致。
      分辨率：PNG 走 pHYs、TIFF 走 XResolution/YResolution（两条都有测试）。
      尺寸：16 位**拒绝**改尺寸（重采样只有 8 位实现），在弹保存对话框**之前**就报错。
      取消：沿用既有的保存对话框路径。预览仍是 8 位 PNG —— 16 位的视觉结果只差抖动，
      与原版一样用它当预览。
- [x] 重开导出的文件验证通道深度和细节，失败不改变项目脏状态。
      测试都是**解码回来逐像素比**，不是只看文件头：夹具的四个像素只差 3（8 位往返会把它们
      压成同一个数），所以「导出后仍互不相同」就是「没有先压成 8 位」的直接证据。
      失败路径在 `ExportAs` 开头就返回，不碰文档、历史与脏标记。

## 为什么只做单图层

上游把拼合画进 16 位每通道的 CGContext，16 位合成由 CoreGraphics 代劳（ImageExporter.swift:33）。
我们这边没有 CoreGraphics，而合成器 `render.Render` 全程走 8 位 `Bitmap` —— 平行的 16 位合成器
不存在，临时搭一套又会变成「第二份像素真相」，与像素不变量相冲。

所以闸门取得很窄，**每一种不支持的情形都明确报错并说明原因**，绝不把 8 位合成结果套上 16 位
文件头 —— 那正是票面第一条禁止的「先压成 8 位」。等有了 16 位合成器，把闸门放宽即可。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `TestSixteenBitExportKeepsSixteenBitsOfPrecision` | PNG 与 TIFF 各自：导出→解码→只差低位的像素仍互不相同，且每个样本与深层对得上（±1） |
| `TestSixteenBitExportCarriesTheResolution` | 两条路的 300 DPI 都进了文件（pHYs / XResolution+YResolution） |
| `TestExportAsSixteenBitPNGKeepsSixteenBits` / `...TIFFKeepsSixteenBits` | 端到端：`ExportAs` 写出的文件重新打开核验 |
| `TestExportAsSixteenBitRefusesWhatItCannotKeep` | 多图层 → 报「单图层」 |
| `TestExportAsSixteenBitRefusesOtherFormatsAndResizes` | JPEG → 报格式；改尺寸 → 报尺寸 |
| `TestExportAsSixteenBitNeedsADeepRaster` | 普通 8 位文档 → 报「没有 16 位栅格」 |

对等矩阵：U31。原版锚点：`ImageExporter.swift`、`DeepLayerTests.swift`；提交 `f5ae5ba`、`DeepLayerTests` 16-bit export 用例。
