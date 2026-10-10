# 审计总览：票 01–51 实现完整性与接线收口

> 本文件是两份逐票审计报告的**索引与汇总**，不替代它们：
> - [A-tickets-01-27.md](./A-tickets-01-27.md) — 票 01–27 逐票逐验收项对照（366 行）
> - [B-tickets-28-51.md](./B-tickets-28-51.md) — 票 28–52 逐票判定（163 行）
> - [C-runtime-findings.md](./C-runtime-findings.md) — 运行时验证发现（若存在）
>
> 审计基线：提交 `679d5d8`。**基线之后又落了一批接线修复**（见下"已收口"），
> 所以两份报告里标 ❌/⚠️ 的条目有一部分已经作废——以本文件与当前工作树为准。

## 1. 基线状态（审计当时的客观事实）

| 检查 | 结果 |
| --- | --- |
| `go build ./...` | ✅ 通过 |
| `go test ./...` | ✅ 全绿 |
| `npx svelte-check` | ✅ 0 错误 0 警告 |
| `npx vitest run` | ✅ 684 测试全绿 |
| 应用启动 | ✅ 进程存活、WebView2 环境创建成功、启动到就绪 **0.849s** |

**结论：仓库不是"半成品代码库"——它构建、测试、启动都正常。** 问题全部集中在
**"内核齐备但用户不可达"**这一类：模块有完整实现和单测，却没有 store / RPC / 菜单
入口把它们接到用户面前。

## 2. 审计发现的核心问题（按严重度）

### 2.0 【最严重｜已修】画布从来没有显示过文档内容

**这一条是"应用能不能用"的分界线，其余所有问题都在它之下。**

标签 ID 与文档 ID 是**两个独立生成的标识**：

| | 生成处 | 形态 |
| --- | --- | --- |
| 标签 ID | `internal/bridge/workspace.go` 的 `newDocID()` | `doc-<16 位十六进制>` |
| 文档 ID | `internal/bridge/session.go` 的 `newUUID()` | RFC 4122 UUID（大写带连字符） |

前端有两处按"文档 ID"配对，两处都因此永远不成立：

1. `App.svelte:190` 的标签同步 effect 调 `loadDocument(current.id)`，
   而 `current.id` 是**标签 ID**；`applyDocumentSnapshot` 的
   `expectedDocId` 守卫（`document.ts:357`）拿它与快照里的**文档 ID** 比较 →
   不相等 → **快照被静默丢弃**。
2. `CanvasSurface.svelte:352` 的 `docState.docId === doc.id` → 永远为假 →
   `renderSrc` 恒为 `null` → **画布从不显示 Go 合成出来的图像**。

**用户可见后果**：画布永远是一块棋盘格。画笔、填充、滤镜、调整、图层操作
全都在 Go 侧真实执行、rev 也真实推进，但**一个像素都不会显示出来**。
票 02 的验收项"新建后画布显示棋盘格"恰好被这个 bug 满足，于是它一路蒙混过关。

**定位方式**（值得记下来）：先用运行时诊断确认画笔链路本身是好的
（`brush-begin-ok` → 201 次 pointermove → `brush-end`，全部成功），
再用"抓手拖拽能平移"证明指针事件确实送达，最后用一条临时诊断打印 id 比较结果：

```
FRONTEND_INFO: docload | expected=undefined            actual=35C59DD1-…  match=true
FRONTEND_INFO: docload | expected=doc-a9afc945989ceaad actual=35C59DD1-…  match=false
```

**修法**：让标签 ID **就是**文档 ID（`internal/bridge` 三处创建点统一用
`doc.DocumentID`）。`sessionByIDLocked` 本来就有"按标签 ID 或文档 ID 都能找到
会话"的兜底循环，说明两种 ID 混用是既有的模糊地带；统一之后：
- App 的标签同步 effect 能正确接受快照；
- `renderSrc` 守卫成立，画布显示真实合成；
- 删掉了已无用的 `newDocID()`。

回归测试 `internal/bridge/tabid_test.go` 钉住这个不变量
（含"重复打开同一 .comp 时 ID 不变"）。

**真机验证**：修复后新建 1920×1080 文档 → 画布显示**白色**（白色背景层，
不再是棋盘格）→ 画笔拖拽出现**黑色笔迹** → 从标尺拖出**青色参考线**。
修复前这三样全都不可能出现。

### 2.0b 【严重｜已修】几何命令不同步标签尺寸；文档变更不刷新标签脏标记

这两条与 2.0 同源：**Go 侧的文档状态变了，但前端/标签描述符那份副本没跟着变**。

**(a) 几何命令不同步标签描述符尺寸**（`internal/bridge/workspace.go`）

`Workspace.EditActive` 只更新会话里的 `domain.Document`，从不碰标签描述符
（`Snapshot.Tabs[].width/height`）。而工作区快照正是前端三处的依据：画布
`.doc` 盒子尺寸、状态栏文档尺寸、裁剪覆盖层重开会话时的起始框。

后果：裁剪 / 画布大小 / 图像大小 / 修剪提交后，文档真的变了、`/render` 也按新
尺寸出图，但界面继续按旧尺寸画框 —— 表现为"提交裁剪后画布和状态栏纹丝不动"，
裁剪框还会被重置回旧的整幅画布（看起来像提交没生效）。撤销/重做几何操作同理。

修法：新增 `Workspace.syncTabSizeLocked`，在 `EditActive` 成功路径、
`UndoActive`、`RedoActive` 三处调用。回归测试
`internal/bridge/tabsize_test.go`。

**(b) 文档变更后不刷新工作区快照 → 脏标记过期（数据丢失风险）**

标签栏的"未保存"标记与关闭确认（`confirm.ts` 的 `requestClose` 读 `doc.dirty`）
都来自**工作区快照**，而文档变更只更新 `document` store。画笔、图层操作、滤镜、
撤销/重做这些路径都不刷新工作区快照，于是：**打开一个已保存的项目 → 编辑一堆
→ 标签栏不显示未保存 → 关闭时不提示保存 → 静默丢工作**。

新建文档因为创建时就是 `dirty=true`，所以这个洞只在"打开已有项目"时暴露。

修法：在 `document.ts` 的 `applyDocumentSnapshot`（所有文档变更的唯一漏斗）
里挂一个**去抖 150ms** 的工作区快照刷新；用动态 import 避免
`document ↔ workspace` 的静态循环。回归测试
`frontend/src/lib/state/document.refresh.test.ts`（4 例，含去抖合并与失败不抛）。

### 2.1 三套完整纯类零生产消费者

`TransformSession`（票 17）、`GuidesState`（票 14）、`CropState`（票 18）都是
**逻辑完整、单测齐全**的状态机，但 `grep` 消费端只命中 `*.test.ts`：

| 类 | 位置 | 后果 |
| --- | --- | --- |
| `TransformSession` | `state/transformSession.ts:833` | ⌘T / ⌘H / ⏎ 全无效；票 17 验收项无法达成 |
| `GuidesState` | `state/guides.ts:101` | 画布不渲染参考线、无开关 store；票 14 验收项无法达成 |
| `CropState` | `state/crop.ts:488` | C 工具选中后画布无反应；票 18 只有纯函数那一半 |

### 2.2 默认工具 `move` 被当成画笔（致命可用性 bug）

`tools.ts:33` 默认 `activeTool="move"`，而 `CanvasSurface.svelte` 的
`PAINT_TOOLS = {move, brush}` 把 move 也算成画笔 —— **进应用左键一拖就画一道黑笔**。
代码注释自认 "no canvas transform is wired yet"。

### 2.3 填充族断在 RPC 层

`internal/render/stampfill.go:45/265` 早就有 `FillColor`（含选区裁剪），
但 `internal/bridge` 零调用 → ⌥⌫ / ⌘⌫ 不可达。票 23 标 done，票 47 归因
"底层未落地"**不准确**——内核在，缺的是 RPC。

### 2.4 浮动选区真缺实现

"Move selected pixels" ×8 在 Go 与前端**都没有实现**（`FloatingSelection` 只存在于
`reference/Swift/`）。票 16 标 done，但对等矩阵 S05 是 ◐。

### 2.5 `lib/gpu/` 零生产消费者（**未收口**）

`frontend/src/lib/gpu/` 全部产物（device 探测、`buildRenderGraph`、纹理 LRU、
WGSL 混合/调整/特效着色器）**没有任何生产导入**，只有 7 个自测文件。
`lib/gpu/README.md:37-39` 承诺"CanvasSurface 持有 WebGpuCompositor"从未兑现。

后果：票 11 / 12 / 22 的验收项（"与 CPU 真值一致""60fps""4K 回显 <16ms"）
建立在**不存在的调用链**上。当前画布走的是 Go 合成 + `/render/{id}.png` + `<img>`。

要真正接通还需要两块新东西（都不在本次范围内）：
1. **Go 侧逐层位图 HTTP 端点**（现在只有整幅 `/render/{docID}.png`，
   而 `layerBitmaps.ts` 需要按层取图）；
2. **`WebGpuCompositor` 本体**——WGSL 着色器与 `buildRenderGraph` 都在，
   但缺"把 graph 执行成 WebGPU 绘制"的那一层。

## 3. 已收口（基线之后完成的接线）

| 缺口 | 修复 | 落点 |
| --- | --- | --- |
| 2.1 参考线 | 新增视图层 store + 画布渲染 + 标尺拖出/移动/删除 + 写回 Go | `state/view.ts`、`CanvasSurface.svelte`、`document.ts`（解析 `guides`） |
| 2.1 变换会话 | 新增会话接线 + 画布变换框/把手 + ⌘T/⏎/Esc | `state/transform.ts`、`CanvasSurface.svelte` |
| 2.2 move 画黑笔 | `PAINT_TOOLS` 只留 `brush`；move 改为拖动/变换图层 | `CanvasSurface.svelte` |
| 2.3 填充族 | 新增 `FillSelection` RPC + 菜单项 | `internal/bridge/selection_commands.go`、`actions.ts`、`MenuBar.svelte` |
| 2.4 浮动选区 | 新增 `MoveSelectedPixels` RPC + 8 条方向键接线 | `internal/bridge/selection_commands.go`、`actions.ts` |
| 票 14 写回通道 | 新增 `SetGuides` RPC（原先 Go 完全没有设置参考线的入口） | `internal/bridge/guides.go` |
| 票 17 写回通道 | 新增 `ApplyLayerTransform` RPC（非破坏，只写 `layer.transform`） | `internal/bridge/layer_transform.go` |
| 视图命令不可达 | 把 5 个开关**并入原有的视图菜单**（含勾选状态）；原先这 5 条只有快捷键 | `MenuBar.svelte` |
| ⌘H 语义错 | 原先 ⌘H 被接到"开始变换会话"；实际应是切换**控件可见性**（会话由 ⌘T 开） | `actions.ts`、`CanvasSurface.svelte` |
| 菜单栏重复「视图」 | 新增时误加了第二个同名菜单；已合并进原菜单（缩放命令 + 开关） | `MenuBar.svelte` |
| 任务栏脏标记告警 | 每次启动必现的确定性竞态：主窗口"已创建但未显示"被误判成"没有主窗口" | `internal/winchrome/`（runtime-verify） |
| 前端错误不可见 | 加 `window.onerror` / `unhandledrejection` 全局兜底 + `ReportFrontendError` 绑定 | `main.ts`、`internal/bridge/frontend_error.go` |
| 2.1 裁剪工具 | 新增 `Crop` RPC + 自包含 `CropOverlay.svelte`（C 键开会话、拖框/把手/比例/吸附、Enter 提交、Esc 取消） | `internal/bridge/crop.go`、`components/CropOverlay.svelte`、`CanvasSurface.svelte` |

**未接线快捷键从 22 条降到 3 条**：`Toggle Levels preview`（需要滤镜会话的预览开关状态）、
`Cycle tool mode` / `Cycle shape kind`（需要 activeTool 之外的子模式 store）。
另有 1 条 `Hide Compositor` 属 macOS 专属，feature-parity 明确不做。

## 4. Mac 键位 → Windows 键位（用户特别提到）

**结论：绑定路径本来就是干净的，无需改键位映射。**

| 环节 | 位置 | 行为 |
| --- | --- | --- |
| 事件 → 存储位 | `shortcuts/chords.ts:109-115` | `e.ctrlKey \|\| e.metaKey` 都映射到 `MOD_COMMAND`，即 **Cmd → Ctrl** |
| 标签显示 | `chords.ts:137-142` | 显示 `"Ctrl"`，不显示 ⌘ |
| 修饰键归一 | `state/modifiers.ts:131-132` | `MetaLeft/MetaRight → Ctrl` |

剩下的 `⌘` 只出现在**中文注释与内部命名**里（例如 `actions.ts` 的"⌘J：复制活动图层"），
属于"描述原版对应命令"的说明文字，不影响实际按键。

**一处待办（低危）**：`state/filters.ts:52-54` 的 `placeholder: "⌘M/⌘L/⌘U"` 是死数据；
`MenuBar.svelte` 在定义表查不到时会把它渲染成**用户可见文本**。当前因为
`table.ts:79-81` 有对应定义所以不会显示，但一旦定义表变动就会露出 ⌘ 字样。

## 4.1 `main.go` 的代理绕过是死代码（runtime-verify 实证）

`main.go:28-40` 的 `init()` 设置
`WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--proxy-bypass-list=<-loopback>`，
注释说这是为了修"系统代理把 wails.localhost 拦成 502"。

**这个环境变量到不了 WebView2 浏览器进程**，实证链路：

1. go-webview2 主动把它覆盖成"选项里传的值"：
   `webviewloader/native_module.go:160` → `os.Setenv(..., additionalBrowserArgs)`；
   `env_create.go:169` 干脆置空。注释写明这是为了**压掉注册表覆盖**
   （"webview2loader checks for existence and not for empty value"）。
2. Wails 调 `edge.NewChromium()`（无参，`frontend.go:164`），
   而 `pkg/options/windows/windows.go` 的 `Options` **没有**暴露
   `AdditionalBrowserArgs` 字段（go-webview2 的 `edge.Chromium` 有这个字段，
   Wails 只用来塞 `--disable-gpu` 与 `--disable-features`）。

真机证据（runtime-verify）：手动带上该环境变量启动后，
`msedgewebview2.exe` 命令行里只有 Wails 自己加的 `--disable-features=…`，
没有 `--proxy-bypass-list`；注册表覆盖同样无效。

**影响**：如果用户仍能复现 502，照现在的代码改注释/改参数都不会有用。
可选处置（需要产品决策）：① 用 `go.mod` 的 `replace` 换成仓库内补丁版
go-webview2（让"环境变量里已有的参数追加而非覆盖"）；② 升级到提供该出口的
Wails 版本；③ 不依赖浏览器参数，从网络层另解，并把注释改成诚实的"当前无效"。

## 5. 仍未完成

> 说明：以下都是**已确认的能力缺口**，不是"没做完的活"——每一项都写清了缺什么、
> 卡住哪些对等矩阵行，便于下一轮直接开工。

| 项 | 原因 | 影响 |
| --- | --- | --- |
| `lib/gpu/` 接通（票 11/12/22） | 需要新增 Go 逐层位图端点 + WebGpuCompositor 本体 | 渲染走 CPU 轨；GPU 验收项未达成 |
| `Cycle tool mode` / `Cycle shape kind` | 需要子模式 store（票 15/25 只有纯函数） | 2 条快捷键未接线 |
| `Toggle Levels preview` | 需要滤镜会话暴露预览开关 | 1 条快捷键未接线 |
| `main.go` 代理绕过是死代码 | 见下节 | 用户报的 502 未被真正修好 |
| 票 50 两条验收 | 需要真实 Release 与干净环境 | 应用内"检查更新"未端到端验证 |
| 票 52 大文档基准 | 需要真机 4K 对照 | 仅有启动到就绪基准 |
| `docs/feature-parity.md` 状态列 | 系统性低估完成度 | 见该文件更新记录 |
