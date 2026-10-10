# 票 51 — 代理契约端到端 · 验收记录

> 对应票面：[docs/tickets/51-agent-contract-e2e.md](../tickets/51-agent-contract-e2e.md)
> 契约正文：[docs/writing-comp-files.md](../writing-comp-files.md)
> 对等矩阵：P05、故事 56。本页是**可执行的验收记录**：自动化部分照抄命令，手工部分照抄步骤，结果填进「记录」表。

## 怎么跑

### A. 自动化（Go）

```powershell
# 应用层端到端（票 51 的验收主体）：真实写入 → 监视 → 原位重载
go test ./internal/bridge/ -count=1 -run 'TestExternal|TestWatchStops' -v
# 监视内核（票 07）
go test ./internal/watch/... -count=1 -v
# 官方示例脚本（代理侧参考实现，用真实加载器验证产物）
go test ./scripts/agentdemo/ -count=1
```

**应用层端到端**（`internal/bridge/watch_test.go`，"代理"由真实 `project.Store` 扮演：
PNG 先写、manifest 最后原子改名）：

| 测试 | 断言的行为 |
| --- | --- |
| `TestExternalWriteReloadsTheOpenDocumentInPlace` | 真实写入 → **一次**原位重载（实测 555ms，见下）；标签 id 不变、撤销栈清空、rev 前进（前端画布缓存键是 `(docId, rev)`） |
| `TestExternalBrokenOrderIsIgnoredThenPickedUp` | 顺序破坏（manifest 先改、PNG 还没写）→ 被忽略、旧画面不动；补上 PNG 后正常重载 |
| `TestExternalSameSizeRewriteIsNotNoticed` | 同字节大小的 PNG 替换 + manifest 一字不改 → 看不到更新（契约里写明的边界） |
| `TestExternalDirtyDocumentAsksBeforeReplacing` | 脏文档先问（`pending`），保留/放弃两条路都对；同一份磁盘内容不重复追问，新的改动会再问 |
| `TestExternalChangeWaitsWhileAStrokeIsInFlight` | 正在拖笔刷时外部改了包：同样先问（重载会把会话换掉，笔画会落空），结束后才真正载入 |
| `TestExternalConcurrentWritesMergeIntoOneReload` | 5 次连写（间隔 40ms）合并成 1 次重载 |
| `TestExternalReloadKeepsTheActiveTab` | 后台标签的文件被改：内容重载但不抢走活动标签 |
| `TestWatchStopsWhenTheTabCloses` | 关标签后监视器退休，文件再变也不产生事件 |
| `TestExternalUnreadablePackageIsIgnoredUntilFixed` | manifest 读不出来（**用"把 manifest.json 换成目录"来模拟**，不是真的云占位符/文件锁）→ 忽略；恢复后正常重载 |

**视口与选区保留**在前端有单测（`frontend/src/lib/state/external.test.ts`，9 个）：`applyExternalChange`
贴上快照与新文档，而 `viewport` 与 `currentSelection` 两个 store 逐字段不变；后台标签的重载
不会把它的文档贴到当前画布上（前端只有一份文档镜像）。这是**负断言**——"没人去动视口"，
不等于"用户肉眼看画布没跳"，那一步在手工 B 节第 2 步。

`internal/watch` 那一层覆盖**监视内核**（票 07）：

| 测试 | 断言的行为 |
| --- | --- |
| `internal/watch/watcher_test.go` `TestWatcherCoalescesWriteBurstIntoOneCallback` | 一次"先 PNG 后改名 manifest"的写入突发只报告一次 |
| `internal/watch/watcher_test.go` `TestWatcherSurvivesAtomicRenameOverManifest` | 原子改名覆盖 manifest 后监视重挂，后续改动仍被看到 |
| `internal/watch/watcher_test.go` `TestWatcherNoticesPackageCreatedIncrementally` | `images\` 目录后出现时被补挂 |
| `internal/watch/digest_test.go` `TestComputeStableForIdenticalContent` | 相同字节重写 → digest 不变（不报告） |
| `internal/watch/digest_test.go` `TestComputeChangesWhenContentChanges` | manifest 字节变化 / 图像大小变化 / 增删图像 → digest 变化 |
| `internal/watch/digest_test.go` `TestComputeMissingManifestIsDistinctError` | 缺 manifest → `ErrNoManifest` |
| `internal/watch/coordinator_test.go` | 稳定后才报告一次、退避 250ms×2ⁿ、上限 8 次后终结、`Notify` 合并 |
| `internal/project/*_test.go` | manifest 校验与包校验的拒绝类别（`ErrInvalid` / `ErrMissingImage` / `ErrTooLarge` / `VersionError`） |

**官方示例脚本** `scripts/agentdemo`（代理侧参考实现，Go + 只用标准库）：

```powershell
# 第 1 步建包（背景 + 光斑 1），之后每多一步追加一个光斑
go run ./scripts/agentdemo -dir C:\Users\you\Desktop\demo.comp -steps 1
go run ./scripts/agentdemo -dir C:\Users\you\Desktop\demo.comp -steps 1
# 一步到位（3 步之间默认不等待；-pause 400ms 可以让人看清每次更新）
go run ./scripts/agentdemo -dir C:\Users\you\Desktop\demo.comp -steps 3 -pause 400ms
```

参数：`-dir`（必填）、`-steps`（默认 1）、`-pause`（步间等待）、`-width`/`-height`（默认 960×540）、
`-seed`（默认固定值，保证同一 seed 逐字节可复现）。再次运行会**保留 documentID 与已有图层 id**，
只追加/覆盖，正是契约里"改已有项目"的样子。

### B. 手工验收（必须做，自动化不能替代）

前置：`wails dev`（或已构建的 exe）打开 `C:\Users\you\Desktop\demo.comp`，窗口与画布可见。

| # | 步骤 | 期望 |
| --- | --- | --- |
| 1 | 用 `scripts/agentdemo` 写入第 1 步（新增一个图层） | 画布在约 300ms 内出现新图层；只重载一次，不闪多次 |
| 2 | 把画布缩放到 200%、平移到某个角落、拉一个选区，再写入第 2 步 | 缩放/平移/选区**原样保留**，画面内容更新 |
| 3 | 连续两次写入（间隔 < 300ms） | 合并为一次重载（画面直接到最终状态，不出现中间态停留） |
| 4 | 在应用里画一笔（产生未保存修改），再写入第 3 步 | 弹"放弃我的修改 / 保留我的"询问；选"保留我的"→ 画布不被覆盖；选"放弃"→ 采用代理版本 |
| 5 | 写入一个**半成品**（manifest 引用了尚未写入的 PNG），随后修正 | 坏状态被忽略、旧画面不动、不弹错；修正后正常重载 |
| 6 | 用**字节大小相同**的另一张 PNG 覆盖某图层，manifest 一字不改 | **看不到更新**（预期行为） |
| 7 | 接着把该图层的 `name` 改掉再写 manifest | 这次看到更新 |
| 8 | 在一个**后台标签**的项目里写入 | 活动标签不被抢走；**当前画布不被换成后台标签的文档**；切回去时后台标签已是新内容 |
| 9 | 重载后按 Ctrl+Z | 撤销栈为空（重载等同重新打开文件） |
| 10 | 写入一个 manifest 带 UTF-8 BOM 的包 | 被静默拒绝；去掉 BOM 重写后恢复 |

## 记录

> 时间填本地时间；环境填 OS 版本 + 构建方式（`wails dev` / 安装版）；结论填"通过 / 不通过 / 未实测"，不通过时在观察里写清现象。

| 时间 | 环境 | 步骤 | 观察 | 结论 |
| --- | --- | --- | --- | --- |
|  |  |  |  |  |
|  |  |  |  |  |
|  |  |  |  |  |
|  |  |  |  |  |
|  |  |  |  |  |

## 本轮自动化的结果

本轮（2026-10-10，Windows 11 / Go 1.26.0 windows-amd64）实际跑过：

```
# 下面的 `--- PASS` 与耗时行摘自实际输出（省掉了 === RUN 行，其余原样）
$ go test ./internal/bridge/ -count=1 -run 'TestExternal|TestWatchStops' -v
    watch_test.go:243: 外部写入到重载完成：568ms
--- PASS: TestExternalWriteReloadsTheOpenDocumentInPlace (0.59s)
--- PASS: TestExternalBrokenOrderIsIgnoredThenPickedUp (1.89s)
--- PASS: TestExternalSameSizeRewriteIsNotNoticed (1.22s)
--- PASS: TestExternalDirtyDocumentAsksBeforeReplacing (1.85s)
--- PASS: TestExternalChangeWaitsWhileAStrokeIsInFlight (0.58s)
--- PASS: TestExternalConcurrentWritesMergeIntoOneReload (1.46s)
--- PASS: TestExternalReloadKeepsTheActiveTab (0.58s)
--- PASS: TestWatchStopsWhenTheTabCloses (1.22s)
--- PASS: TestExternalUnreadablePackageIsIgnoredUntilFixed (4.07s)
ok  compositor-win/internal/bridge  13.551s

$ go test ./internal/watch/... -count=1
ok  compositor-win/internal/watch  4.977s

$ go test ./scripts/agentdemo/ -count=1
ok  compositor-win/scripts/agentdemo  0.791s（14 个测试）

$ cd frontend && npx vitest run src/lib/state/external.test.ts
Tests  9 passed (9)
```

**实测重载时序：555–569ms**（四次运行 555/567/568/569）（合并窗口 300ms + coordinator 首次比较前的一次退避 250ms 左右）。
票面写的"~300ms"是**合并窗口**；从写入到画面更新完是 0.5–0.6s，这里按实测记录。
端到端脚本 `scripts/agentdemo` 的真实运行也核对过：3 步 → 4 个图层（背景 + 光斑 1/2/3），
再次运行保留 documentID 与全部图层 id，包内无 `.tmp` 残留。

**手工 B 节 10 步仍未执行**（需要人工看着画布），「记录」表留空。

## 未验证项

以下各项**没有**实测证据，不要当成已验证：

1. **OneDrive / 云占位符**：只有云端的占位文件在未下载时读取会失败（依赖 provider）。本机没有 OneDrive 目录，未实测；应用"把这种读失败当作这次不重载"的行为同样未实测。
2. **长路径（>260 字符）**：未实测。不要在文档或脚本里断言其行为。
3. **真实杀毒软件 / 索引器干扰**：刚写完的 PNG 被瞬态锁住、改名重试是否足够，未在装有第三方杀毒软件的真实环境实测。
4. **"视口/选区保留"只在 store 层断言**：`external.test.ts` 证明外部变更不会碰 `viewport`/`currentSelection`，
   但"用户肉眼看画布没有跳"仍需手工 B 节第 2 步。
5. **`scripts/agentdemo` 生成物没有用 macOS 原版打开过**：只对齐了 `internal/project` 的规则与真实加载器。
6. **Windows 文件锁**：结论（打开时改名 `Access is denied`、删除被打开文件 `being used by another process`、
   目标存在但未打开时改名成功并替换）来自本机实测，并已固化成 `internal/fsatomic` 的测试
   （`TestWriteFileKeepsTheOldContentWhenTheTargetIsLocked`、`TestWriteFileRetriesBeforeGivingUp`）；
   应用自身 `Store.Save` 现在走同一套"一次改名 + 重试"。**真实杀毒软件占用下的表现仍未实测。**
7. **QuickLook**：Windows 版不写 `QuickLook\`、读到即忽略，有单测佐证 digest 会跳过 `images\` 下的子目录（`TestComputeIgnoresSubdirectoriesInImages`）；"从 macOS 带来的包在应用里打开"这一条未手工验证。
8. **真实 macOS `.comp` 互开**：票 05 的字节级对齐仍挂着 `TODO(字节级对齐)`（`internal/project/manifest.go`），跨平台互开的实测证据不在本页范围内。

## 待办 / 本轮已完成

- [x] `internal/bridge`：`watch.Watcher` + `watch.Coordinator` 已接到会话（`internal/bridge/watch.go`），
      外部变更端到端测试 9 个（见上表）。
- [x] `scripts/agentdemo`：已落地（`-dir`/`-steps`/`-pause`/`-width`/`-height`/`-seed`），
      14 个测试用真实加载器验证产物。
- [x] `internal/project/store.go`：`Save` 的 manifest 提交改成**一次 rename 覆盖 + 短重试**
      （`internal/fsatomic`），错误注释一并删除；`internal/bridge/recent.go` 同步修掉同样的写法。
- [ ] 手工跑完上面 B 节 10 步并填写「记录」表（需要人看着画布，本轮未做）。
