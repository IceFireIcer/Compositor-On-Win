# 70 — 决策门槛与 .comp v13 / HDR 图层导入

**What to build:** 在兼容性决策获批准后，用户可导入 EXR 和 Radiance HDR，保留高于白色的光值，保存并重开可与原版 v13 互通的项目。

**Blocked by:** 68 ✅ — 决策门槛与 .comp v12 / 16 位图层存取; ADR-0007 ✅ — 已获用户确认（2026-10-15）。

**Status:** done（2026-10-15）——ADR-0007 已确认；EXR/Radiance 导入、保存重开与版本门控已落地。

- [x] 决策明确 half-float extended sRGB 预乘、v13 `.exr` 资源、与 v11/v12 的兼容和拒绝边界（ADR-0007 第 1、2、4、5 条）。
- [x] EXR/HDR 超白值在导入、图层、保存、重开后仍可恢复；完全不超白的 EXR 按上游 1.001 规则转成 16 位层。
- [x] v1–v12 声明的项目拒绝 EXR 图层；路径安全、大小限额、蒙版约束延续。
- [x] 与上游 `DeepLayerTests` 的 HDR 与版本门控用例互证。

对等矩阵：U17。原版锚点：`DeepLayers.swift`、`ProjectStore.swift`、`docs/project-format.md`；提交 `7f239dc`。
## 验证

| 用例 | 覆盖 |
| --- | --- |
| `internal/rasterio` `TestDecodeEXRKeepsHighlights`、`TestDecodeRadianceKeepsHighlights` | EXR / Radiance 超白值导入后仍高于白 |
| `internal/bridge` `TestHDRLayerSurvivesSaveAndReopen` | 保存写 `.exr`、清单版本 13、重开后高光保真 < 0.01（上游同款阈值） |
| `internal/bridge` `TestExposureBringsHighlightsIntoThePreview`、`TestExposureOnHDRIsExactNotAnEightBitApproximation` | 曝光走精确线性光内核；显示副本按新的深层像素重算，高光不被永久截断 |
| `internal/bridge` `TestLevelsDimsHDRHighlightsInProportion` | 与上游 `levelsDimsHighlightsInProportion` 互证：色阶按比例压暗高光 |
| `internal/bridge` `TestFilterOnHDRKeepsHighlightsRecoverable`、`TestBrushStrokeOnHDRKeepsUntouchedHighlights` | 滤镜与绘画后图层仍是 HDR |
| `internal/project` `deep_assets_test.go` | `.exr` 需 version ≥ 13；16 位 PNG 需 ≥ 12 且非蒙版；v11 中的 16 位资产拒绝 |

## 已知差异

1. **非 sRGB 色度的 EXR 仍按数字读取**：`internal/hdrio` 不解析 `chromaticities` 属性，EXR 里的数值被当作 sRGB 数字。上游经 CoreImage 读取，色彩管理交给系统。补齐需要一条 ICC/矩阵转换路径（与票 85 的导出侧同源）。
2. **HEIF / RAW 仍是 8 位**：上游把 `bitsPerComponent > 8` 的导入当 16 位；Windows 侧 `heicio`/`rawio` 只出 8 位。见票 78/90。
3. 画布高光预览与屏幕抖动属票 71/83。
