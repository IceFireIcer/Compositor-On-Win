# 86 — CMYK 文件导入与配置保留

**What to build:** 用户可打开带 ICC 配置的 CMYK PSD、JPEG 或 TIFF，并在项目中保留来源配置以供校样。

**Blocked by:** 38 — PSD/PSB 解析器; 72 — CMYK ICC 校样与色域警告。

**Status:** done（2026-10-19）—— TIFF / JPEG / PSD（拼合 + 分层）四条路都通，配置都能留进会话；「打开 → 保存 `.comp` → 重开」的往返也补上了（像素逐字节一致、配置持久化经直接查文件确认）。

（票面原标 `blocked-by-decision`。核对下来它并不真的卡在票 72 上：**导入**只需要文件**内嵌**的
配置，而票 72 缺的是「随包内置的通用配置」（发行准备项）。`colorio.ConvertToRGB` 的注释里就
写着「CMYK import path needs it」，API 早已备好。）

- [x] CMYK **TIFF / JPEG / PSD** 的颜色按嵌入配置转换为可编辑 RGB 表示。
      **PSD 的两种形态都通了**（2026-10-16/19）：拼合（无图层段，读图像数据段）与分层（`decodeChannels`
      按 `unpackedCMYKChannelIDs` 切通道集合）都按 4 个油墨通道解码、**反相**后交 `colorio.ConvertToRGB`。
      JPEG：`rasterio` 的 `isCMYKJPEG`（走段标记找帧头的分量数）→ `render.DecodeCMYKJPEGWithProfile`
      （四分量读回 ICC 约定油墨，并从 APP2 的 ICC_PROFILE 分段按段号拼回配置）→ 与 TIFF 共用
      `cmykImportResult` 做边界、配置校验与 `colorio.ConvertToRGB`。导出侧也补上了配置嵌入
      （`EncodeCMYKJPEGWithProfile`），否则导出的文件回来时无配置可读。
      `internal/rasterio/cmyktiff.go`：`x/image/tiff` 对 PhotometricInterpretation=5 直接
      `UnsupportedError`（reader.go:600-671），所以自己写了一个读取器 —— 8 位 4 通道、无压缩或
      Deflate（可带水平差分预测）、连续条带，取油墨平面与 ICC 标签 34675，再交给
      `colorio.ConvertToRGB`。`rasterio.Decode` 在 tif/tiff 上先嗅探，命中就走这条；导入路径
      （`import_images.go:377`）本来就调它，所以拖进画布即可。
      **配置也留进会话了**：`colorio.AdoptEmbedded`（上游 `CMYKProfile.embedded` + `adopt`）——
      名字取自配置自己的 `desc` 标签，留存一份副本并**选为当前配置**，于是校样与 CMYK 导出回到
      那份文件当初针对的油墨。导入路径在转好像素后调用它；留存失败只记日志，不影响导入（像素
      已经转好了）。
      **PSD 那条原来漏了这一步**（2026-10-16 补上）：PSD 走 `project_io` 的 `loadPhotoshop`，**不经过**
      `rasterio.Decode`，所以采纳那一步对它不生效 —— 是写分层 CMYK 的测试时才发现的。
- [x] 缺失或不受支持配置**明确拒绝**，不静默套用错误 profile。
      没有 ICC 标签、或标签里不是 CMYK 配置，都报出具体原因（「没有内嵌 ICC 配置，套用 sRGB
      会把颜色弄错」/「内嵌的配置不是 CMYK 配置」）。上游的「让用户选一份」属于 UI，未做；
      **拒绝**是安全的那一半。
- [x] 打开、保存重开和校样显示与 `CMYKTests` 语义一致。
      `TestCMYKImportSurvivesSaveAndReopen`（2026-10-19）串起整条链：打开 CMYK TIFF → 保存 `.comp`
      → 全新 workspace/service 重开 → **像素逐字节一致**、图层结构一致、`cmyk.json` 里记着的仍是
      导入时采纳的那份配置（校样与 CMYK 导出都用它）。

      **两个测试缺口是被变异逼出来的，值得记**：
      1. 第一版挑的是**第一份** CMYK 配置，而 `Chosen()` 的回退恰好也回退到它 —— 「持久化了」与
         「回退了」分辨不出来。改成挑**不是回退**的那份，并在回退恰好是它时跳过。
      2. 更隐蔽的一条：`adopt` 会把配置**复制进 colorio 目录**，副本名字与采纳的一致，于是
         `Chosen().Name` 在「写了 cmyk.json」与「没写、回退到副本」两种情况下**看起来一样**。
         所以断言改成**直接查 `cmyk.json` 文件**。把 `adopt` 里两处 `SetChosen` 去掉，改前测试
         照样通过，改后立刻失败。

      —— 以下是 PSD 支持的过程记录 ——
      **PSD 的 CMYK 变体：拼合这条已通**（2026-10-16）。

      `mode 4` 通过头部闸门；拼合（没有图层段）的 CMYK PSD 按 4 个油墨通道解码，**反相**后交
      `colorio.ConvertToRGB`，用的配置来自图像资源 1039。

      **反相是照上游抄的**：`PSDReader.swift:335` 写着「Inks are stored inverted: a missing channel is
      no ink」—— 满墨存 0、纸白存 255。变异验证过：去掉反相，测试立刻失败（白变暗、黑变白）。

      **配置选择照上游**（`PSDReader.swift:73-76`）：内嵌配置 → 通用配置 → 否则拒绝。我们这边**没有
      随包通用配置**（ADR-0008 §4），所以退化到「内嵌或拒绝」，与票 86 第二条一致（宁拒绝不猜 sRGB）。

      **分层 CMYK 也通了**（2026-10-16）：照上游在 `decodeChannels` 里做 —— 通道集合按
      `unpackedCMYKChannelIDs`（`PSDReader.swift:235`：`-1,0,1,2,3,-2`）切换，缺通道默认**不上墨**
      （255），四个油墨平面反相后交 `ConvertToRGB`。配置选择同样照上游（内嵌 → 通用 → 拒绝）。
      夹具是手工拼的**分层** PSD（按 `readRecord` 的读序：矩形 → 通道表 → 混合签名/键 → 属性 →
      额外数据里的掩版长度/混合范围长度/Pascal 名字）。

      —— 上一轮记录的前提（ICC 从资源 1039 带出）本轮用上了。

      已落地并验证：`psd.Read` 现在把图像资源 **1039** 的 ICC 配置带进 `Document.ICCProfile`
      （`TestReadCarriesTheEmbeddedICCProfile` / `TestReadWithoutAnICCProfileLeavesItEmpty`）。
      这是 CMYK 支持的前提 —— 那是唯一说明「这些通道是谁的油墨」的东西。

      仍未做，且有两处实打实的障碍：

      1. **色彩模式闸门与通道管线是 RGB 形状的**：`reader.go:196` 只放行 mode 3；`assemble` 按 RGBA
         建层。要支持 CMYK，得让 mode 4 通过、按 4 个油墨通道解码、再经 `colorio.ConvertToRGB` 转
         —— 牵动整条通道解码与建层路径，不是加一个分支。
      2. **没有 CMYK PSD 夹具**。手写一份**分层** PSD（`readRecord` 要完整的图层记录 + 通道数据）
         可行但工作量不小；写一份**拼合的**更省事，但读取器走不到那里 ——
         `reader.go:268` 在图层段长度 < 4 时直接返回**零图层**，**根本不读拼合的图像数据**。
         所以「拼合 PSD」这条路本身也是缺的（与颜色空间无关）。

      如实记下，不当成「快做完了」。**部分**：配置已留进会话，校样可以用它
      （上游 `adopt` 的语义）；**JPEG 的 CMYK 变体已接进导入路径**（`isCMYKJPEG` 走段标记读帧头的
      分量数 → `DecodeCMYKJPEGWithProfile` 从 APP2 的 ICC_PROFILE 分段按段号拼回配置 → 与 TIFF 共用
      `cmykImportResult` 做边界、配置校验与 `ConvertToRGB`）；**PSD 的 CMYK 变体仍未做**（要动票 38
      的解析器）。

## 支持范围（刻意写死并逐条报错）

| 项 | 支持 | 不支持时的行为 |
| --- | --- | --- |
| 通道 | 8 位 × 4（CMYK） | 报「只支持每通道 8 位」/「SamplesPerPixel 只支持 4」 |
| 压缩 | 无压缩、Deflate（8/32946） | 报「压缩方式 N 尚未支持」 |
| 预测器 | 1、2（水平差分） | 报「Predictor N 尚未支持」 |
| 条带 | 连续条带 | 报条带表不一致或越界 |
| 配置 | 内嵌 ICC（34675） | 报缺配置，不猜 |

绝不猜着解：解出垃圾比报错更糟。


## 验收证据审计（2026-10-16，第三轮）

| 变异 | 结果 |
| --- | --- |
| `isCMYKTIFF` 恒为假（CMYK TIFF 走普通路径） | ✓ 被抓住 |
| `isCMYKJPEG` 接受任意分量数 | ✓ 被抓住 |
| 导入**不再校验**内嵌配置是不是 CMYK 配置 | ✗ **没被抓住 —— 真缺口** |

### 查出并补上的缺口

把 `cmykImportResult` 里那道 `IsCMYKProfile` 校验去掉，全部测试照样通过 —— 也就是说「内嵌的配置
必须真的是 CMYK 配置」这条规则**没有任何测试**。

行为其实没坏：更深一层的 `ConvertToRGB` 会拦住它。但**错误分类与提示都退化了**，实测失败信息是：

```
无法读取该图像：文件可能已损坏或不可用: That profile isn't a CMYK output profile.
```

一个**完全正常**、只是内嵌配置不对的文件，被报成「可能已损坏」——用户换个配置就能用，却被引向
「文件坏了」。所以补 `TestCMYKTIFFWithANonCMYKProfileIsRefused`：断言错误是 `ErrUnsupported`
（不是 `ErrUnreadable`）且提示里说明「不是 CMYK 配置」。加上之后重跑变异：立刻失败 ✓。

### 这一轮的方法收获

变异审计有两类产出：一类是「规则完全没测试」（会转出垃圾、会静默降级），另一类是这种「**行为对、
但错误分类错了**」——后者不看变异结果根本发现不了，因为它不会让任何断言失败。
## 验证

| 用例 | 覆盖 |
| --- | --- |
| `TestCMYKTIFFImportsThroughItsEmbeddedProfile` | 手工拼一份带真实系统配置的 CMYK TIFF → `Decode` → 白墨转成接近白、满黑墨明显偏暗 |
| `TestCMYKTIFFWithoutAProfileIsRefused` | 没有 ICC 标签 → 拒绝且错误里说明缺的是 ICC |
| `TestCMYKJPEGImportsThroughItsEmbeddedProfile` | 带配置的 CMYK JPEG 走真实导入路径：纸白转成接近白、满黑墨明显偏暗、配置随结果回来 |
| `TestCMYKJPEGWithoutAProfileIsRefused` | 没有 APP2 配置 → 拒绝且错误里说明缺的是 ICC |
| `TestCMYKTIFFWithANonCMYKProfileIsRefused` | 内嵌 RGB 配置 → 报成 `ErrUnsupported` 且说明「不是 CMYK 配置」（**变异审计查出的缺口**） |
| `TestCMYKTIFFWithUnsupportedCompressionIsRefused` | 把 Compression 改成 LZW → 明确报错，而不是解出垃圾 |

夹具是**手工拼**的：`x/image/tiff` 的 writer 不写 CMYK，所以测试按 TIFF 布局逐段拼一份最小的
无压缩 CMYK TIFF。转换值按 ADR-0008 §5 只做结构性断言（满黑墨的具体值取决于所用配置的黑点）。

对等矩阵：U33。原版锚点：`PSDReader.swift`、`CMYK.swift`、`CMYKTests.swift`；提交 `896d5c8`。
