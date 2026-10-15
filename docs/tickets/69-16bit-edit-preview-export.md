# 69 — 16 位图层转换与几何编辑

**What to build:** 用户可在 8 位与 16 位之间转换图层；对 16 位图层缩放、裁剪、旋转或翻转时仍保留精度。

**Blocked by:** 68 ✅ — 决策门槛与 .comp v12 / 16 位图层存取; 43 ✅ — 画布尺寸/图像尺寸/修剪。

**Status:** done（2026-10-15）——位深转换、16 位图像尺寸重采样、菜单栏与图层面板右键菜单、位深标签均已落地。

- [x] 位深转换命令：8 位 ↔ 16 位，撤销 / 重做一致，保存重开一致。
- [x] 位深标签与可转换性的查询接口（`LayerDepthInfo`）。
- [x] 图层菜单「转换为 16 位 / 8 位」与图层面板的位深标签。
- [x] 图像尺寸重采样在 16 位精度进行（裁剪/旋转/翻转只改 placement，已天然保留精度）。
- [x] 8 位转换的舍入规则明确：抖降（`Quantize16`），与上游 `DeepLayer.dithered` 同一内核。

对等矩阵：U16。原版锚点：`DeepLayers.swift`、`DeepLayerTests.swift`；提交 `2fe3524`。绘画、调整、显示和导出分见 82–85。

---

## 实现

真值是 `DeepLayers.swift` 的 `EditorSession.convertLayerDepth`（`DeepLayers.swift:295-328`）。

1. `internal/bridge/layer_depth.go`：
   - `depthLabel`：`HDR` / `16-Bit` / 空，对应上游 `NativeLayerList` 的 `depthTag`。
   - `convertibleLayer`：活动图层必须是像素图层（不是编组、调整图层或可编辑文字），对应
     `canConvertLayerDepth`。**「只选一个图层」那一半留在前端**：选区是前端状态
     （AGENTS.md 不变量 5），Go 只能看 `ActiveLayerID`。
   - `ConvertLayerDepth(tabID)`：方向由「有没有深层像素」决定（与上游一致）——有深层就是转
     8 位（抖降后丢掉深层），没有就是转 16 位（现有 8 位像素原样留作派生副本，深层是它的
     精确 v×257 展宽）。空白图层（`imageFile == nil`）拿到画布大小的透明 16 位像素并设定
     placement，对应上游同一分支。
   - `LayerDepthInfo()`：回包 `{labels, activeLabel, canConvert}`，供面板画标签与菜单启用。
2. `internal/bridge/drawing.go`：`editDrawingTab` 拆出 `editDrawingTabFor`，历史名在锁内由
   session 决定——转换的方向决定这一步叫「转换为 16 位」还是「转换为 8 位」。

**为什么必须进 raster journal**：位深不是 manifest 字段（由资产 PNG 的位深表达），所以 8→16
转换后清单一个字节都没变，历史里也就没有任何东西可回滚。深层像素是像素状态，必须由
`rasterChange` 的 before/after 承载，撤销才能真正把深层像素拿走。`Bitmap.Clone` 已经会克隆
`Deep`，journal 因此自动正确。

## 验证

`internal/bridge/layer_depth_test.go`（6 条）：

| 用例 | 覆盖 |
| --- | --- |
| `TestConvertToSixteenBitKeepsThePixelsAndUndoes` | 8 位像素逐字节不变、深层是精确 v×257、撤销拿走深层、重做装回 |
| `TestConvertToEightBitDithersAndUndoes` | 转 8 位是深层像素的抖降、深层被丢掉、撤销装回深层 |
| `TestConvertDepthOnABlankLayerGivesItPixels` | 空白图层拿到画布大小的 16 位像素与 placement；撤销后重新没有资产 |
| `TestConvertDepthRefusesNonPixelLayers` | 编组与调整图层被拒 |
| `TestLayerDepthInfoReportsLabelsAndConvertibility` | 8 位无标签、转换后为 `16-Bit`、`canConvert` 正确 |
| `TestSixteenBitConversionSavesAsVersion12` | 转换后保存，清单版本为 12（与票 68 的版本策略咬合） |

## 已知差异

1. **图像尺寸只重采样图像尺寸这条路径**：`render.DrawTransformedDeep` 覆盖它；自由变换、裁剪、
   画布旋转与翻转本来就只改 placement（`doc_geometry.go` / `canvas_rotate.go`），不重采样，
   因此天然保留精度。上游用 `ImageResizer` 统一处理，这里只对齐了会重采样的那一条。
2. 上游对 HDR 图层另有一条 `convertHDRToSixteenBit`（裁到白、保持 16 位），属票 71。
3. 位深标签读的是 Go 侧的 `LayerDepthInfo`，多选时不提供转换——选区是前端状态
   （AGENTS.md 不变量 5），Go 只能看 `ActiveLayerID`。
