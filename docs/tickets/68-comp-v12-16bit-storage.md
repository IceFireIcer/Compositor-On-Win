# 68 — 决策门槛与 .comp v12 / 16 位图层存取

**What to build:** 在兼容性决策获批准后，用户可把 16 位 PNG/TIFF/HEIF/RAW 导入为保留原精度的图层，并保存、重开；Windows 写出的项目格式按批准的 ADR 与原版 v12/v13 规则确定。

**Blocked by:** 05 ✅ — .comp 存取校验与互开; 41 ✅ — RAW/HEIC 导入与 develop; **ADR-0007（2026-10-15 用户确认）**。

**Status:** done（2026-10-15；1.5 增量）

- [x] 先明确 8bit RGBA 现有不变量的扩展边界、旧版兼容策略和 v11→v12 版本门控，获用户确认后再实现。
- [x] 输入与项目资源保留 16bit sRGB 预乘像素；屏幕 8bit 副本不反向覆盖原始像素（**HDR 图层**已成立：显示副本始终派生自深层栅格）。**普通 16 位图层已成立**（票 82 落地）：滤镜/调整走 `render.RunDeep`（原版 `DeepLayer.run`），16 位栅格被保留而不是丢掉；重绘工具走 `takePainted`。没有深层栅格的 8 位图层照旧只跑 8 位。蒙版仍为 8bit。
- [x] 原版 v12 允许 16 位 PNG；v1–v11 遇 16 位资产按原版规则拒绝。**已核实原版新保存实际为 v13**（见下），Windows 按 ADR-0007 §4 写「内容所需的最低版本」。
- [x] 导入、保存与无损往返由测试验证（**新写的 Go 用例**，不是 `DeepLayerTests` 的逐条移植；与它的对应关系见票 69/70 的验证表，其余用例分见票 82/83/84）。真实相机 RAW 样本见票 91。

对等矩阵：U15。原版锚点：`DeepLayers.swift`、`ProjectStore.swift`、`docs/project-format.md`；提交 `d9cdb81`。

---

## 决策：ADR-0007（2026-10-15 用户确认）

票面要求「先写 ADR 并获用户确认」，本轮完成。核实 `reference/Swift`（`e8c9e29`）后确认的关键事实：

1. `ProjectManifest.current = 13`，`supported = 1...13`。
2. **上游每次保存都写 v13**：`EditorSession.projectSnapshot()` 重建 `ProjectManifest(...)`，
   `version` 取结构体默认值 `current`；加载到的版本不随文档保留（`installProject` 只取
   documentID/width/height/resolution/guides）。多处测试以
   `#expect(loaded.manifest.version == ProjectManifest.current)` 钉住。
3. 因此 `project-format.md` 开头「version `12` for new saves」是**过期描述**，与代码不符——
   票面要求核实的正是这一点。
4. 资产种类由文件自身表达，**manifest schema 零变更**：16 位层 = `<uuid>.png`（16 位 PNG）；
   HDR 层 = `<uuid>.exr`；蒙版恒为 8 位 PNG。

用户批准：像素不变量扩展为「8 位 / 16 位 / HDR half-float」三种；**写出版本取内容所需的最低版本**
（11/12/13），读取接受 1–13。这是对上游「恒写 13」的有意偏差，目的是让不含新特性的 Windows
文档仍能被 macOS 1.4.5 打开。

## 实现

1. `internal/render/deep.go` — `DeepRaster`：16 位或 half-float 预乘 RGBA，native-endian
   `uint16`、行紧密排列、左上原点，与 macOS `DeepLayer` 布局一致。`DeepFromBitmap`（v×257 精确
   展宽）、`DeepFromImage`（走 `color.Color.RGBA()`，16 位 PNG 一位不丢）、`WorkingBitmap`
   （宽图的 8 位派生副本）、`ToBitmap`（纯派生，**不**附带 Deep）、`Peak`、`EncodePNG16`、`DeepFromPNG`。
2. `internal/render/deeppixels.go` — `DeepPixels.c` 的 Go 移植：`HalfToFloat` / `FloatToHalf`
   （binary16，就近偶数舍入）、`HalfPeak`、`HDRNormalize` / `HDRRestore`、`HDRExposure`、
   `DeepLinear` / `DeepEncoded`。float32 全程对齐 C。
3. `internal/render/image.go` — `Bitmap` 增加 `Deep *DeepRaster`：深层像素是图层的真源，8 位位图
   是它的派生副本（对应上游 `ImportedImage.image` / `.deep`）。这同时白送上游那条规则：任何
   产出新位图的操作天然没有 Deep，图层从此就是 8 位（同文字栅格化）。
4. `internal/domain/document.go` — `FormatVersion = 13`，新增 `VersionBase = 11` /
   `Version16Bit = 12` / `VersionHDR = 13`。
5. `internal/project` — `requiredVersion` 取内容所需的最低版本；`assetNameRe` 允许 `.exr`；
   `checkAsset` 分派 PNG / EXR；16 位 PNG 需 v12、EXR 需 v13（拒绝类别照抄上游的
   `ProjectError.missingImage`）；蒙版恒为 8 位灰度 PNG，`.mask.exr` 显式拒绝；`parseEXRHeader`
   只读头部（与 PNG 一样，解码留给编解码器）。
6. `internal/bridge` — 载入走 `render.WorkingBitmap`；保存走 `encodeAsset`（深层优先，
   写 `EncodePNG16`）；`internal/rasterio` 的导入同样经 `WorkingBitmap`。
7. `scripts/agentdemo` 改声明 `VersionBase`：它只写 8 位 RGBA，按 ADR-0007 §4 就该是 v11。

## 验证

- `internal/render/deep_test.go`（11 条）：half-float 已知值 / 就近偶数舍入 / 往返、v×257 展宽、
  派生副本走 `Quantize16`、`Peak` 去预乘、`HalfPeak` 跳过 Inf、HDR 归一化往返高光（< 0.01）、
  曝光把 linear 4 降到 linear 2（上游同款期望值）、透明像素不动、16 位 PNG 往返 ≤1 级、
  不透明像素往返精确、HDR 显示副本夹到白且不越过 alpha。
- `internal/project/deep_assets_test.go`（8 条）：三种内容的写出版本（11/12/13）、v11 里的 16 位
  资产被拒（`ErrMissingImage`）、v12 里的 `.exr` 被拒（`ErrInvalid`）、16 位蒙版被拒、`.mask.exr` 被拒、
  `parseEXRHeader` 正常与三类坏输入。
- `internal/bridge/deep_layer_test.go`：半透明 16 位斜坡 → 保存 → 全新 workspace 重开；断言清单
  版本 12、资产是 16 位 PNG、深层像素逐样本 ≤1 级、8 位副本是抖动的（**不等于**普通截断）。

## 已知差异

1. **写出版本**：上游恒写 13，Windows 写最低所需版本。读取范围一致（1–13），双向可读；
   差异只在保存字节的 `version` 字段，已在 ADR-0007 记录并经用户确认。
2. **16 位 PNG 的预乘往返**允许 ≤1 级舍入（PNG 无预乘，CoreGraphics 同样要除回去），
   以测试钉住；不透明像素精确往返。
3. 真实相机 RAW / HEIF 的 16 位样本不在仓库内（票 91 负责上游资源），本轮以构造样本验证。
4. 上游 `DeepLayerTests` 的其余用例分属票 69（转换）、82（绘画与调整精度）、83（屏幕抖动）、
   84（16 位导出）。
