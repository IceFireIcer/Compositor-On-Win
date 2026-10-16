# 84 — 16 位 PNG/TIFF 导出

**What to build:** 用户可从 Export As 导出保留 16 位通道精度的 PNG 或 TIFF，并继续使用普通 8 位导出。

**Blocked by:** 59 — 统一 Export As 与 PDF/TIFF 导出; 68 — 决策门槛与 .comp v12 / 16 位图层存取。

**Status:** done（2026-10-16）—— 编码器、单图层快路与**多图层 16 位合成**都已落地。

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
- [x] **多图层也做完了**（见下「多图层 16 位合成」）：`render.CompositeDeep` 把 8 位普通路径
      （place → 缩 alpha → source-over）搬到 16 位样本上，不另写一套合成器。
- [x] 重开导出的文件验证通道深度和细节，失败不改变项目脏状态。
      测试都是**解码回来逐像素比**，不是只看文件头：夹具的四个像素只差 3（8 位往返会把它们
      压成同一个数），所以「导出后仍互不相同」就是「没有先压成 8 位」的直接证据。
      失败路径在 `ExportAs` 开头就返回，不碰文档、历史与脏标记。

## 多图层 16 位合成（2026-10-16，U31 剩余已补）

上游把拼合画进 16 位每通道的 CGContext，16 位合成由 CoreGraphics 代劳（ImageExporter.swift:33）。
我们这边没有 CoreGraphics，但也**没有另写一套合成器**：

`render.CompositeDeep` 把 8 位合成器那条普通路径原样搬到 16 位样本上 —— `Place`（对应
`DrawTransformedDeep`）→ 按图层不透明度缩 alpha → source-over。它**不重新决定**图层怎么合，
所以不是「第二份像素真相」；公式的权威仍是 `Render`，由 `TestDeepCompositeMatchesTheEightBitOne`
执行这条约束（量化后最大差 **2 级**，而故意破坏公式会差到 **118 级**）。

闸门因此放宽了一档：多图层的**普通图像图层 + 正常混合**现在能导出 16 位，位移/旋转/翻转/缩放
也支持（走 `DrawTransformedDeep`，采样方式取自图层本身，与 `Place` 一致）。

仍然明确拒绝、且说明是哪一层哪条属性的：组、调整图层、蒙版、剪贴蒙版、图层特效、非「正常」
混合、没有 16 位栅格。**绝不把 8 位合成结果套上 16 位文件头** —— 那正是票面第一条禁止的。

### 单图层那条快路还在

拼合结果本身就是某一层的深层栅格时（单图层、恒等变换、无蒙版/特效/非正常混合/不透明度 1、
尺寸等于画布），仍然直接返回那份栅格 —— 一个像素都不用重算，也就没有任何舍入。


## 验收证据审计（2026-10-16，第二轮）

沿用票 87 那套办法：**改坏实现，看测试会不会失败**。只查「有没有提到这句话」不够 ——
「改坏了会不会失败」才是同一件事。

| 变异 | 结果 |
| --- | --- |
| `deepNRGBA64` 不做去预乘（导出带预乘值） | ✓ 被抓住 |
| 16 位 TIFF 不写分辨率 | ✓ 被抓住 |
| 16 位导出**不再拒绝 HDR 栅格** | ✗ **没被抓住 —— 真缺口** |

### 查出并补上的缺口

把 `sixteenBitExportRaster` 里那段 HDR 拒绝去掉，**全部测试照样通过**：也就是说「HDR 栅格不能走
16 位 PNG/TIFF」这条规则此前**没有任何测试**。它的后果不是报错，而是**超白值被无声裁到 1.0** ——
正是票面第一条禁止的那种静默降级。

新增 `TestExportAsSixteenBitRefusesAnHDRRasterButEXRAcceptsIt`：同一份带超白值（half 0x4C00 = 4.0）
的 HDR 栅格，走 16 位必须被拒绝并说明去 EXR，走 EXR 必须成功。**两侧都钉** —— 否则「拒绝」可能
悄悄变成「谁也导不出」。加上之后重跑变异：立刻失败 ✓。
## 验证（新增）

| 用例 | 覆盖 |
| --- | --- |
| `render.TestDeepCompositeMatchesTheEightBitOne` | 两层（一层偏移 + 半透明）合成后量化，与 8 位合成最大差 2 级；破坏公式立刻差 118 级 |
| `render.TestDeepCompositeKeepsBitsTheEightBitPathCannot` | 两层的 8 位副本相同、16 位只差一个低位单位，合出来仍保留差别 |
| `bridge.TestExportAsSixteenBitCompositesMultipleLayers` | 端到端：两层文档导出 PNG，解码回来两端像素仍互不相同 |
| `bridge.TestExportAsSixteenBitRefusesAnHDRRasterButEXRAcceptsIt` | HDR 栅格走 16 位被拒绝、走 EXR 成功（**变异审计查出的缺口**） |
| `bridge.TestExportAsSixteenBitRefusesWhatTheCompositeCannotDescribe` | 蒙版 / 非正常混合 / 组各自被拒绝并说明是哪一层哪条属性 |

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
