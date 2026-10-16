# 91 — 上游资源与截图 CI 的 Windows 等价方案

**What to build:** 对上游新增图标、截图方案、测试脚本和更新 feed 资产做平台归类，并为 Windows 建立等价的资源检查，不把 macOS 工程文件直接移植进产品。

**Blocked by:** 03 — CI 门禁与 NSIS 安装包骨架; 50 — 安装包与自动更新。

**Status:** done（2026-10-16）

- [x] 记录 app icon、截图测试、CI 并行测试和 feed 生成在 Windows 的等价入口或明确排除理由。

| 上游资产 | Windows 等价 | 理由 |
| --- | --- | --- |
| `Assets.xcassets` app icon | `scripts/genicon`（生成 `.ico`） | 图标是矢量重画 + ICO 封装，不搬 Xcode 资源目录 |
| `ScreenshotTests.swift` | `*.browser.test.mjs`（真实浏览器 headless） | 截图对比换成语义断言；CI 里装 Playwright 用 runner 自带 Edge |
| `.github/workflows/verify.yml` 并行测试 | 同一个 `verify` job 串行跑前端与 Go | windows-latest 上并行收益小于 vcpkg 缓存的重用 |
| appcast.xml / feed 生成 | `scripts/genfeed` | 自建 feed，不消费上游 appcast |
| Xcode 工程文件 | **无等价物，明确排除** | macOS 专属；Windows 侧由 wails + NSIS 承担（`release.yml`） |

- [x] 构建检查资源存在、尺寸和许可证，不修改 `reference/Swift`。
      `internal/assetcheck`（`go test ./...` 会跑到，CI 因此自动覆盖，无需改 workflow）：

| 检查 | 内容 |
| --- | --- |
| 嵌入表 | `CameraRawTables.bin` 存在、尺寸与 SHA-256 对上、带 `CRT3` 魔数；子模块在时**与上游逐字节相同** |
| 拷贝来源 | `tests/golden/c` 里每个 `.c/.h`（除我们自写的 driver/dispatch）都要在顶部 20 行内注明 `reference/Swift/...` 源文件与 40 位提交号，且那个源文件**真的存在** |
| 整文件拷贝 | 注释声称是整个文件的，注释之下必须**仍与上游一致**（比较前归一化行尾）；声明 `INCREMENT` 的增量除外 |
| README 清单 | 每个拷贝件都要出现在 `tests/golden/c/README.md` |
| 许可证 | 根 `LICENSE` 与 `reference/Swift/LICENSE` 存在且非空 |
| macOS 专属 | `internal`、`frontend/src`、`scripts`、workflows 的源码里不得出现 `.xcodeproj`/`.xcassets`/`Assets.xcassets`/`ScreenshotTests` |

- [x] 资产检查失败给出可定位日志；不把 macOS 专属 Xcode 工程作为 Windows 运行时依赖。
      每条问题是 `路径: 说明`，失败的测试把清单打出来。

## 检查跑起来后抓到的真问题

1. **10 个拷贝件完全没有来源注释**（`LevelsPixels`、`BrushPixels`、`LensPixels`、`NoisePixels`、`WandPixels`
   各自的 `.c/.h`，票 08/60 的基线批次）。AGENTS.md 不变量 #4 要求「注明拷贝来源提交」，这一批漏了。
   已逐个补上，并**先核实**它们与当前 pin 的上游逐字节相同，所以引用 pin 是准确且可复验的。
2. **`DeepPixels` 的注释声称「注释之下与上游逐字节相同」**，检查一开始报不符。查下来是**行尾**差异：
   我们的副本是 LF、上游检出是 CRLF（`core.autocrlf` 的产物），内容完全相同。检查因此改成比较前归一化
   行尾 —— 否则每次检出策略变化都会误报。
3. **`DitherPixels` 确实是增量**（票 64），两个来源提交不同是设计如此。它的注释加了一行机器可读的
   `INCREMENT:` 标记；检查跳过整文件比较，但**没有该标记的同名拷贝仍然必须逐字节一致**。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `TestRepositoryAssetsAreConsistent` | 真实仓库跑一遍，0 问题 |
| `TestVerifyReportsAMissingTree` | 空目录上必须有判别力（报缺资产、缺许可证） |
| `TestProvenanceRegionJoinsWrappedLinesAndStopsAtTheWindow` | 跨行路径接得起来（`ScanlinesPixels.h` 的注释在 include guard **之后**）、窗口之外不算来源 |
| `TestVerifyCatchesADriftedEmbeddedTable` | 改一个字节的嵌入表必须被摘要抓住 |

改了 C 文件之后重新编译过黄金驱动（`gcc -O2 -std=gnu99 -fblocks`），对拍仍然逐字节通过。

对等矩阵：U38。原版锚点：`.github/workflows/verify.yml`、`ScreenshotTests.swift`、`Assets.xcassets`；提交 `5181d0b`、`3081719`、`ce5ea67`。
