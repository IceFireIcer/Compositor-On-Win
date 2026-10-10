# 50 — 安装包与自动更新

**What to build:** 发布链路：签名 NSIS 安装包（代码签名证书流程脚本化）、自动更新（GitHub Releases + EdDSA 签名 feed、启动/手动检查、更新提示 UI、差量或全量安装器）、appcast 等价 feed 生成脚本。

**Blocked by:** 49 — Shell 集成（关联/最近/脏标记）.

**Status:** implemented（2026-10-10）—— 代码与脚本齐备，两条验收需要真机/真发布才能勾。

- [ ] 安装→升级→卸载在干净环境验证（脚本记录）
- [x] 更新 feed 生成与签名可复现（脚本化，对应 publish.sh 语义）
- [ ] 应用内"检查更新"到应用新版端到端
- [x] 无证书环境的降级构建路径（不签名）仍可用

对等矩阵：P04、C09、故事 53。

> **为什么两条不勾**（两轴评审后按事实改的，之前勾早了）：
>
> - 验收 1：`scripts/verify-install.ps1` 就绪且有真断言，但本机没有 makensis、没有两个现成
>   安装器，**从未执行**，`install-verify.txt` 不存在。需要在干净 VM 里跑一次并归档。
> - 验收 3：检查→下载→校验→启动安装器这条链每一环都有测试（httptest + 假启动器），但
>   "从 GitHub 上的真 Release 装成新版"要等第一次真发布才能确认。
>
> 另外，评审在**已勾选状态下**发现了一个会静默失效的契约错：两个发布脚本把
> `internal/update.FeedURL` 注入成了**安装器 .exe 的地址**（应该是 feed.json 的地址），
> 发布版会去 GET 一个 50MB 的 exe、解析失败、检查更新永远报 error。已修（现在注入
> `…/releases/latest/download/feed.json`，安装器地址只在 feed 的 `installer.url` 里用），
> 并在两个脚本里加了"必须以 feed.json 结尾"的断言。这条记录留在这里，因为它正好说明
> 为什么"没在真发布上跑过就不能勾"。

## 实现说明（2026-10-10）

**更新模型。** 与原版 Sparkle + appcast 对齐，但落到 Windows 的形态上：

- **feed**：GitHub Releases 上的 `feed.json`（appcast 的等价物，形状见
  `internal/update/feed.go`）：`app`/`channel`/`latest{version, publishedAt, notes,
  installer{url, size, sha256, edSignature}}`。`scripts/genfeed` 生成并**自检**（用应用侧
  同一个解析器读回来 + 验签），保证脚本与应用对 feed 的理解不会漂移。
- **可复现**：`-published-at` 可以把发布时间钉死，于是同一份输入（同密钥、同字节、同一时间）
  产出的 feed **逐字节相同**（测试比较的是两次落盘的文件字节，不是内存里 marshal 两次）。
- **签名**：Ed25519，标准库 `crypto/ed25519`。私钥只从环境变量/CI secret 来
  （`COMPOSITOR_UPDATE_KEY`，base64 的 32 字节 seed），**绝不入库**；公钥经
  `-ldflags -X compositor-win/internal/update.PublicKeyB64=<b64>` 注入应用，feed 地址同理
  （`update.FeedURL`）。同一份输入（同密钥、同字节、同时间）生成逐字节相同的 feed —— 有测试。
- **应用内流程**：`CheckForUpdates`（拉 feed 比版本）→ `DownloadUpdate`（下载 + 校验
  **大小 → sha256 → Ed25519 签名**，任何一步不过就删掉临时文件）→ `ApplyUpdate`（写一个
  "等应用退出再 `/S` 静默安装"的批处理并脱离启动，然后退出应用）。校验失败的包永远不会
  被交出去，所以不存在"装了个坏包"的中间态。
- **状态而不是异常**：检查更新有四种状态 —— `available` / `up-to-date` / `unconfigured` /
  `error`。网络不通、没配更新源都是用户该看到的**状态**，不该在控制台刷错误，也不该弹"失败"。
- **UI**：`帮助 ▸ 检查更新…`（C09）+ `UpdateSheet`（版本/发行说明/下载进度条/安装按钮）；
  启动时静默检查一次，**只有真有新版**才弹表单。下载进度经 `update:progress` 事件推给前端
  （限流到约 100ms 一次）。
- **版本注入**：`internal/bridge.BuildVersion`（发布流水线用 `-ldflags -X` 注入，源码里是
  开发版本 0.1.0），状态栏与更新检查共用它。

**发布链路。**

- `.github/workflows/release.yml`：tag `v*` 或手动触发 → 与 verify.yml 同一套环境准备
  （mingw / vcpkg / npm / wails / nsis）→ 构建 → **有证书 secret 就签名**（`signtool`，时间戳）
  → 安装器改名 `Compositor-<ver>-setup.exe` → `scripts/genfeed` 生成 feed → 创建 GitHub
  Release 并上传安装器 + feed.json。
- `scripts/release.ps1`：本地等价流程（`-Version` / `-SkipSign` / `-SkipFeed` / `-DryRun` /
  `-InstallerPath`），打印产物清单与下一步。
- **版本号同步**：安装器与 exe 的版本资源来自 `wails.json` 的 `info.productVersion`（wails v2.16
  没有覆盖它的开关），而 tag 版本经 `-ldflags` 只进 `bridge.BuildVersion`。所以本地脚本在构建期
  临时改写该字段、`finally` 恢复；CI 也在构建前临时改写（**不回写仓库**，checkout 是一次性的）。
  否则安装器属性页/卸载项会显示旧版本，与 feed 里的版本号对不上。
- `scripts/verify-install.ps1`：验收 1 的脚本化记录（静默安装旧版 → 探针用户数据 → 覆盖升级
  → 断言用户数据仍在 → 静默卸载 → 断言目录清掉），输出可 review 的 `install-verify.txt`。
- **降级路径（验收 4）**：没有证书 → 跳过签名并打印醒目警告；没有更新私钥 → 跳过 feed 生成并
  警告；应用侧没有注入 feed/公钥 → 明确报"这个构建没有配置更新源"。三者都不失败。
  本机已实测：不带任何证书跑 `wails build -nsis` 能出安装器（缺 makensis 时只警告）。

**没有引入第三方依赖。** 原计划用 `creativeprojects/go-selfupdate`（见 dependencies.md 原表），
实施时改为自写：该库的模型是"替换正在运行的二进制"，而票面要的是全量**安装器**（快捷方式、
`.comp` 关联、卸载项都在安装器里，票 49 刚把它们写进注册表），而 Ed25519 验证标准库就有。
已回填 `docs/dependencies.md` 与 `THIRD-PARTY-NOTICES.md`。

## 测试

- Go `internal/update`（14 组）：feed 解析（含 9 种危险 feed 的拒绝）、版本比较（17 例，含
  预发布/构建元数据/垃圾输入）、可用性检查（有新版/最新/404/坏 JSON）、下载校验（正常、
  篡改内容被 sha256 拦、错签名被 Ed25519 拦、大小不符、超上限、校验失败不留残file）、
  安装脚本（引号、`/S`、CRLF）与脱离启动（接缝）。
- Go `internal/bridge`（4 组）：四种检查状态都不抛、下载落盘并记录、应用更新需要已校验的包、
  更新信息。
- Go `scripts/genfeed`（5 组）：生成的 feed 能被应用侧解析器读回并验签、可复现、坏输入被拒、
  私钥解析（seed/完整私钥/垃圾）、发行说明读取。
- 前端 `state/update.test.ts`（8 组）：四种状态、静默 vs 手动弹窗、下载成功/失败、安装
  成功/失败、更新信息、进度订阅（无运行时空订阅、有运行时时只在下载中更新）。

## 已知差异 / 未实现（诚实登记）

1. **差量更新不做**：只有全量安装器（票面允许"差量**或**全量"）。每次升级都要下完整的
   ~18MB 安装包（实测本机产物 18.2MB）。
2. **验收 1 未执行**：`scripts/verify-install.ps1` 就绪但本机缺 makensis 与安装器；需要在
   干净 Windows 环境跑一次并归档记录。**不要**把"脚本存在"当成"验证通过"。
3. **验收 3 的"真实 Release"端到端未跑**：检查→下载→校验→启动安装器这条链有测试（httptest
   + 假启动器）覆盖，但"从 GitHub 上的真 Release 装到新版"要等第一次真发布才能确认；
   签名证书与时间戳步骤同理（本机没有证书）。
4. **没有回滚/降级保护**：只比版本号，不阻止用户装一个更旧的包（feed 是我们控制的，风险低）。
5. **更新通道只有 `stable`**：feed 里有 `channel` 字段，但没有 UI 切换 beta 通道。
6. **启动检查会联网**：发布版每次启动一次 HTTPS GET（开发版没配更新源，直接短路不联网）。
   没有"不再检查更新"的开关（原版 Sparkle 有）。
7. **安装器自身没做完整性校验之外的事**：NSIS 的 `/S` 静默升级假定安装器是我们签名的那份；
   UAC 提权由安装器自己处理（安装范围 machine，见票 03）。
8. **应用退出后才真正安装**：用户点"立即安装并重启"后应用退出，安装器等 3 秒再跑；如果用户
   在这 3 秒内手动打开了应用，安装可能失败（原版 Sparkle 也会遇到同类竞态）。
9. **装进安装包的 exe 没有 Authenticode 签名**：`wails build -nsis` 一趟完成"编译 exe +
   makensis 打包"，所以只能签安装器外壳（SmartScreen 看的是它）。要连内层 exe 也签需要两趟
   构建（先 `wails build` 签 exe，再对 `project.nsi` 跑 makensis）；CI 里已打警告。
10. **安装失败没有用户可见反馈**：应用已经退出，UAC 被拒或安装器报错时用户只看到"应用关了
   又没升级"。原版 Sparkle 有更新器窗口兜住；这里需要"状态文件 + 下次启动提示"，未做。
11. **验收脚本不隔离整机**：`verify-install.ps1` 只把 INSTDIR 指到干净前缀，卸载项与
   开始菜单/桌面快捷方式仍按产品名写删，所以本机已装 Compositor 时会破坏那套安装（用户数据
   有备份，注册表与快捷方式没有）。已加检测：发现已装就必须显式 `-Force`，建议在 VM/快照里跑。
12. **下载没有断点续传**：中断要重下整包；失败会清掉临时文件（`.part` + 校验通过才改名）。
13. **代理**：Go 的标准传输认 `HTTP_PROXY`/`HTTPS_PROXY` 环境变量，但不读 WinINET 的系统
    代理设置 —— 靠 IE 代理配置的企业网络可能连不上更新源。
