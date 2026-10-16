# 86 — CMYK 文件导入与配置保留

**What to build:** 用户可打开带 ICC 配置的 CMYK PSD、JPEG 或 TIFF，并在项目中保留来源配置以供校样。

**Blocked by:** 38 — PSD/PSB 解析器; 72 — CMYK ICC 校样与色域警告。

**Status:** in-progress（2026-10-16）—— **TIFF 那条已通**；PSD 与 JPEG 的 CMYK 变体未做。

（票面原标 `blocked-by-decision`。核对下来它并不真的卡在票 72 上：**导入**只需要文件**内嵌**的
配置，而票 72 缺的是「随包内置的通用配置」（发行准备项）。`colorio.ConvertToRGB` 的注释里就
写着「CMYK import path needs it」，API 早已备好。）

- [x] CMYK **TIFF** 的颜色按嵌入配置转换为可编辑 RGB 表示。
      `internal/rasterio/cmyktiff.go`：`x/image/tiff` 对 PhotometricInterpretation=5 直接
      `UnsupportedError`（reader.go:600-671），所以自己写了一个读取器 —— 8 位 4 通道、无压缩或
      Deflate（可带水平差分预测）、连续条带，取油墨平面与 ICC 标签 34675，再交给
      `colorio.ConvertToRGB`。`rasterio.Decode` 在 tif/tiff 上先嗅探，命中就走这条；导入路径
      （`import_images.go:377`）本来就调它，所以拖进画布即可。
      **配置也留进会话了**：`colorio.AdoptEmbedded`（上游 `CMYKProfile.embedded` + `adopt`）——
      名字取自配置自己的 `desc` 标签，留存一份副本并**选为当前配置**，于是校样与 CMYK 导出回到
      那份文件当初针对的油墨。导入路径在转好像素后调用它；留存失败只记日志，不影响导入（像素
      已经转好了）。
- [x] 缺失或不受支持配置**明确拒绝**，不静默套用错误 profile。
      没有 ICC 标签、或标签里不是 CMYK 配置，都报出具体原因（「没有内嵌 ICC 配置，套用 sRGB
      会把颜色弄错」/「内嵌的配置不是 CMYK 配置」）。上游的「让用户选一份」属于 UI，未做；
      **拒绝**是安全的那一半。
- [ ] 打开、保存重开和校样显示与 `CMYKTests` 语义一致。**部分**：配置已留进会话，校样可以用它
      （上游 `adopt` 的语义）；**JPEG 的 CMYK 变体已可用** —— `render.DecodeCMYKJPEG`（票 87 的 U34
      剩余）能按四分量读回 ICC 约定的油墨，但**还没接进导入路径**，也还没从 APP2 的 ICC_PROFILE
      分段拼回内嵌配置；**PSD 的 CMYK 变体仍未做**（要动票 38 的解析器）。

## 支持范围（刻意写死并逐条报错）

| 项 | 支持 | 不支持时的行为 |
| --- | --- | --- |
| 通道 | 8 位 × 4（CMYK） | 报「只支持每通道 8 位」/「SamplesPerPixel 只支持 4」 |
| 压缩 | 无压缩、Deflate（8/32946） | 报「压缩方式 N 尚未支持」 |
| 预测器 | 1、2（水平差分） | 报「Predictor N 尚未支持」 |
| 条带 | 连续条带 | 报条带表不一致或越界 |
| 配置 | 内嵌 ICC（34675） | 报缺配置，不猜 |

绝不猜着解：解出垃圾比报错更糟。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `TestCMYKTIFFImportsThroughItsEmbeddedProfile` | 手工拼一份带真实系统配置的 CMYK TIFF → `Decode` → 白墨转成接近白、满黑墨明显偏暗 |
| `TestCMYKTIFFWithoutAProfileIsRefused` | 没有 ICC 标签 → 拒绝且错误里说明缺的是 ICC |
| `TestCMYKTIFFWithUnsupportedCompressionIsRefused` | 把 Compression 改成 LZW → 明确报错，而不是解出垃圾 |

夹具是**手工拼**的：`x/image/tiff` 的 writer 不写 CMYK，所以测试按 TIFF 布局逐段拼一份最小的
无压缩 CMYK TIFF。转换值按 ADR-0008 §5 只做结构性断言（满黑墨的具体值取决于所用配置的黑点）。

对等矩阵：U33。原版锚点：`PSDReader.swift`、`CMYK.swift`、`CMYKTests.swift`；提交 `896d5c8`。
