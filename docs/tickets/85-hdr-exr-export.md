# 85 — HDR EXR 导出

**What to build:** 用户可将保留超白高光的 HDR 图层导出为 OpenEXR，并在再次导入时恢复高光。

**Blocked by:** 70 — 决策门槛与 .comp v13 / HDR 图层导入; 71 — HDR 高光编辑与画布预览; 59 — 统一 Export As 与 PDF/TIFF 导出。

**Status:** done（2026-10-16）

- [x] Export As 的 EXR 选项写出 half-float extended sRGB，超白值不截断。
      格式表加了 `exr`；拼合结果是某层的 **HDR 深层栅格**时直接写它（`hdrExportRaster`，
      复用票 84 的窄闸门），超白值原样出去 —— `TestExportAsEXRKeepsLightBrighterThanWhite`
      解码回来验证峰值仍约为 4，而不是被截到 1。
      没有 HDR 栅格时退回 8 位拼合结果的 half 展开，与上游的 `hdrRaster ?? raster`
      （ExportAsSheet.swift:211）一致：那份回退是诚实的（值本来就是 8 位的），不像给 8 位合成
      结果套 16 位文件头那样误导。
- [x] 无超白值的 EXR 按原版规则作为 16 位层处理；带超白值的文件重新导入为 HDR。
      **这条此前已落地**：`bitmapFromDeep` 按上游 `deep_half_peak` 规则判定
      （`hdrWithinWhite = 1.001`）—— 峰值在白色内的 EXR 走 `HalfToSixteen` 变成 16 位层，
      否则保留 HDR（`internal/bridge/project_io.go:292-304`）。
- [x] 导出、取消、保存重开和权限/路径错误有明确反馈且不污染项目。
      导出走既有的保存对话框路径（取消返回空路径、权限/路径错误由 `writeExport` 报出）；
      整条路是只读的，不碰文档、历史与脏标记。

## 验收证据审计（2026-10-16）

变异审计：把 `hdrExportRaster` 改成恒返回 nil（EXR 退回 8 位栅格）→ `TestExportAsEXRKeepsLightBrighterThanWhite` 立刻失败（「超白高光被截断了：峰值 1」）✓。这条断言是吃劲的。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `TestExportAsEXRKeepsLightBrighterThanWhite` | 左半 0.5、右半 4.0 的 HDR 栅格 → 导出的 EXR 解码回来仍是 HDR 且峰值约 4 |
| `TestExportAsEXRFallsBackToTheFlattenedRaster` | 没有 HDR 栅格的文档也能导出 EXR（8 位 half 展开） |
| `TestExportAsEXRRefusesResizingAnHDRRaster` | HDR 的 EXR 改尺寸被拒（重采样只有 8 位实现，会压掉超白） |

既有测试 `TestExportAsRejectsBadRequests` 里那条「EXR 应被拒绝」改成了仍不支持的 `webp`；
前端的格式集合测试同步加上 EXR。

对等矩阵：U32。原版锚点：`ImageExporter.swift`、`DeepLayerTests.swift`；提交 `b9186f7`。
