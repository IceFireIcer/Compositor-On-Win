# D. 真机 GUI 验收（从 0 重新构建 + Windows MCP computer use）

> 基线：工作树（commit `46d13cf` + 本轮未提交改动）。方法：`wails build` 全新构建 →
> 带 stdout/stderr 日志启动 → **逐项用 Windows MCP（UIA 树 + 截图 + 点击/按键）操作真实窗口**，
> 以"日志行 + 界面状态 + 落盘产物 + 像素"为证据，不靠读代码推断。
> 本文档按轮次累积（§2 起为第一轮，§4.5 之后逐轮追加）。**八轮合计**：确证缺陷 10 个
> （其中致命 4 个：保存/打开/导出全废、画笔不可撤销、分割链路整条不可用、文字 effect 死循环），
> 已修 10 个并回归通过；另有 2 项"已定性未修/未取证"（内容感知填充、Camera Raw 提交）与
> 1 个上游偶发崩溃（go-webview2 的 errorCallback 直接 os.Exit(1)）。
> 每轮的原始证据（命令、日志行、截图）都留在对应小节里。

## 0. 构建与日志模式

| 步骤 | 命令 | 结果 |
| --- | --- | --- |
| 清理 | 删 `build/bin`、`frontend/dist` | — |
| 依赖 | `npm ci`（frontend） | 80 包，12s |
| 前端 | `npm run build` | 194 模块，dist 287.57 kB |
| 应用 | `wails build` | `build/bin/Compositor.exe` 12.3–12.8s |
| 启动 | `Start-Process … -RedirectStandardOutput logs/run-NN.out.log -RedirectStandardError …err.log` | `LAUNCH_TO_READY_SECONDS: 0.576–0.797` |

**日志模式（本轮新增，`frontend/src/main.ts`）**：正式版 WebView2 的 console 不可见，
而应用里"用户可见的失败"绝大多数走 `console.warn("保存项目失败", err)` 这类分支 ——
界面只是没反应，日志里一行都没有。新增 console tap：`console.warn`/`console.error`
转发到 Go 的 `ReportFrontendError`（warn → `FRONTEND_INFO: console.warn | …`，
error → `FRONTEND_ERROR:`），上限 200 条、原生 console 行为不变。
**本轮 3 个缺陷里有 2 个是靠这条通道才被看见的。**

回归基线（修复后全绿）：`go build ./...`、`go test ./...`（20 包）、
`npx svelte-check`（0 错 0 警）、`npx vitest run`（713 测试）。

## 1. 逐项结果

| 项 | 操作（Windows MCP） | 证据 | 判定 |
| --- | --- | --- | --- |
| C01 新建画布 | 欢迎屏 1920×1080/72 → 创建 | 状态栏 `1920 × 1080 @ 72ppi`；白底；标签"未命名" | ✅ |
| T06 笔刷 | 选 B → 画布拖拽 | 黑色笔迹；标签转脏；窗口标题加 `*` | ✅ |
| C15 填充前景色 | Alt+Delete | 整层变黑 | ✅ |
| C10 撤销/重做 | 填充后 Ctrl+Z；笔迹后 Ctrl+Z | 均正确回退；图层合并撤销可还原图层 | ✅（修 §2.1 后） |
| C34 反相 | Ctrl+I | 白底黑笔 → 黑底白笔 | ✅ |
| C18/V01 缩放 | Ctrl+1 / Ctrl+= / Ctrl+- / Ctrl+0 | 工具头读数 100 → 125 → 100 → 43% | ✅ |
| T05 裁剪 | C 工具拖框 → Enter | 覆盖层含比例/尺寸/提交；1920×1080 → 1215×540；撤销回 1920×1080，重做再回 1215×540 | ✅（矩阵应升 ☑） |
| C02 打开项目 | Ctrl+O → 原生对话框 | 对话框可开（修 §2.3 后）；但 `.comp` 是**目录包**，标准文件对话框会"进入文件夹"而不是选中它 | ⚠️ 见 §4 |
| C02 命令行/关联打开 | `Compositor.exe <path.comp>` | 单实例转发正常：前台实例存活、第二实例退出、进程数=1 | ✅（§4 的崩溃是另一个前提） |
| C05 保存 | Ctrl+S → 原生另存为 → 保存 | `demo1.comp/`（manifest.json 765B + images/*.png）；manifest v11、1920×1080、sRGB、图层"背景"；标题 `demo1 - Compositor`（脏标记消失） | ✅（修 §2.3 后） |
| C06 导出 PNG | Ctrl+Shift+E → 原生另存为 | `export1.png` 53169B；**像素校验**：1920×1080、32bppARGB、采样 928 暗 / 41556 白 | ✅ |
| C37 高斯模糊 | 滤镜菜单 → 高斯模糊… → 拖半径到最大 → 确定 | 对话框（破坏性徽标 + 半径滑杆 + 取消/确定）；提交后画布像素整体改变（大半径铺开） | ✅ |
| T12/X01 文本 | T 工具拖框 → 输入 → Ctrl+Enter | 文本层入面板、画布渲染出文字；选项栏（字体/字号/颜色/对齐/字距/行高） | ⚠️ 功能通，但触发 effect 死循环 → §2.2（已修） |
| C46/C48/C49/C50/C51 图层族 | Ctrl+Shift+N / Ctrl+J / Ctrl+G / Ctrl+E / 混合模式下拉 | 面板逐次变为 `[背景]` → `[图层 1, 背景]` → `[图层 1 copy, 图层 1, 背景]` → 加 `Folder 1`；合并 4→3 层；混合模式下拉 24 项且 Multiply 落到面板 | ✅ |
| P08 窗口标题/脏标记 | 保存前后看标题 | `未命名* - Compositor` → `demo1 - Compositor` | ✅（修 §2.3 后） |
| C22 网格/参考线/标尺 | Ctrl+' / Ctrl+; / Ctrl+R | 画布出现布局网格（在深色笔迹上可见交叉纹理）+ 顶部标尺刻度（500/1000/1500） | ✅ |
| C14 快捷键表单 | 编辑菜单"键盘快捷键…" | 本轮点击未命中（菜单项坐标随菜单内容变化），**未验证** | ⏳ 下一轮 |
| 菜单可达性 | 逐个打开 8 个顶层菜单读 UIA 项 | 选择 2 / 文件 8 / 编辑 9 / 图像 14 / 图层 6 / 帮助 1 / 视图 11 / 滤镜 10（条件禁用项 UIA 不列出，已与 `MenuBar.svelte` 源码逐条核对） | ✅ |
| T02 选框 / T03 套索 / T04 魔棒 / T13 吸管 / T10 渐变 / T11 形状 | 各自选中工具后在画布拖拽 | 画布**无任何反应**（无蚂蚁线/无选区/无取样/无渐变/无形状） | ❌ 与矩阵 ◐ 一致 |
| 合并空层 | 对空编组/空层 Ctrl+E | 日志：`合并结果为空，没有可合并的像素` / `下方没有同级图层`（校验正确；Photoshop 会产出空层，本实现拒绝） | ⚠️ 语义差异，非缺陷 |

## 2. 本轮修掉的缺陷

### 2.1 【致命】画笔落笔不可撤销（`internal/bridge/workspace.go`）

`EndStroke` 把笔迹写回内存位图库、推 rev、标脏，**但没有进历史**：`history.End` 对
"文档 JSON 未变"的编辑判为 no-op 而不记录，而画笔只改位图不改文档模型
（`render.NewStroke` 走 `TileGridFromBitmap` 逐 tile 拷贝，基础位图全程不被就地改写）。
于是 Ctrl+Z 面对空栈报"没有可撤销的操作"，前端 `runUndoRedo` 又静默吞掉 ——
用户看到的就是"笔迹永远擦不掉"。

**修法**：走滤镜提交同一套机制 —— `hist.Begin("画笔")` → 换位图 → `hist.EndForced`
→ `journalRasters(before, after)`；像素逐字节相同（空笔）时完全不产生条目、不推 rev。
回归测试 `internal/bridge/brush_undo_test.go`（落笔 → 撤销还原 → 重做恢复）。

### 2.2 【高】文字会话触发 Svelte effect 死循环（`frontend/src/lib/components/CanvasSurface.svelte`）

日志实证（每敲一个字一条）：`FRONTEND_ERROR: window.error | …/effect_update_depth_exceeded`。

根因两处叠加：
1. 选区写回 effect 直接读 `$textSession?.content` → 订阅的是**整个会话对象**，
   `selection` 一变就叫醒它；
2. `setSelection` 里 `textSession.update(s => …return s)` 的"没变就返回原对象"守卫
   **挡不住通知**：svelte/store 的 `safe_not_equal` 对对象恒判"不等"，照样 notify。

于是 `setSelection → notify → effect → syncSelection → setSelection` 无限递归。

**修法**：effect 依赖收进 `$derived($textSession?.content)`（只有内容真变才重跑）；
`setSelection` 先 `get(textSession)` 比对，值没变就**根本不写 store**。

### 2.3 【致命】Wails 运行时上下文从未注入 → 所有原生对话框全废（`main.go`）

日志实证：`FRONTEND_INFO: console.warn | 保存项目失败 保存文件对话框失败: 运行时尚未就绪`。

`Service.Startup(ctx)` 定义了却**没人调用**：Wails 只调 `options.OnStartup` 这一个钩子，
不会因为绑定结构体上有个 `Startup` 方法就自动调用它（官方模板里 `app.startup` 也是
main.go 手写 `OnStartup: app.startup` 接上去的）。`s.ctx` 恒为 nil，`runtimeCtx()`
的每一个消费端一起静默降级：

| 消费端 | 用户可见后果 |
| --- | --- |
| `project_io.go` Open/Save | 保存 / 另存为 / 打开项目 全部报"运行时尚未就绪" |
| `import_images.go` | 导入图像对话框失败 |
| `export.go` | 导出 PNG/JPEG 对话框失败 |
| `shell.go` | 窗口标题恒为 "Compositor"，脏标记与文档名从不出现 |
| `filter_session.go` | 滤镜实时预览 `EventsEmit` 被跳过 |
| `watch.go` | 外部变更 `external:changed` 事件不发 |
| `memory.go` | 内存压力事件不发 |
| `update.go` | 检查更新/下载对话框与事件不发 |

**修法**：`OnStartup` 第一句 `service.Startup(ctx)`。回归：保存落盘、标题、导出全部恢复。

## 3. 上游崩溃（未修，已定位）

**现象**：前台实例刚用 Esc 关掉原生对话框后，第二次启动（`Compositor.exe <path>`，
即双击 .comp / 从资源管理器打开）会**让前台实例当场退出**：

```
logs/run-02.out.log: [WebView2 Error] The parameter is incorrect.
logs/run-02.err.log: 1: go-webview2/pkg/edge.(*Chromium).errorCallback  ← chromium.go:151
                    2: …(*Chromium).Focus                                   ← chromium.go:575
                    3: …windows.(*Frontend).onFocus                         ← frontend.go:1043
                   17: …(*Frontend).WindowShow → SetForegroundWindow        ← frontend.go:1036
```

**根因**：`go-webview2` 的 `errorCallback` 末尾是 `os.Exit(1)`（`chromium.go:150-153`），
任何 WebView2 调用报错都会直接终止进程；触发点是 `shell.RaiseWindow` 无条件调
`runtime.WindowShow` → `SetForegroundWindow` → 焦点事件 → `Chromium.Focus` 返回
E_INVALIDARG（对话框刚销毁时的窗口状态）。

**干净复现对照**：无对话框时连开两次实例 → 前台实例存活、第二实例退出、进程数=1（正常）。
所以这是"特定窗口状态 + 上游 os.Exit"的组合，不是单实例逻辑本身错。

**第二轮复现尝试（未复现）**：按"启动 → Ctrl+O 开原生对话框 → Esc 关掉 → 点进窗口 → 再起第二实例"
逐步 bisect，四步全部存活、进程数始终为 1、日志无 WebView2 错误。崩溃那次的现场是**高频对话框
自动化**（另存为对话框里输入过非法文件名、弹过"文件名无效"消息框、反复点"打开"让资源管理器
进目录）之后。因此这是"上游 os.Exit + 罕见窗口状态"的偶发组合，**不是确定性 bug**。

**处置**：不基于不可复现的证据改 `RaiseWindow` 的行为（贸然去掉 `WindowShow` 会牺牲"双击 .comp
把既有窗口提到前台"这条正常路径）。真要根治应在上游：给 go-webview2 提交 `errorCallback` 不应
`os.Exit(1)` 的补丁，或换用暴露 `AdditionalBrowserArgs` 的 Wails 版本。

## 4. 其他观察

- `.comp` 是目录包，Windows 标准文件对话框无法"选中文件夹"作为打开目标：
  Ctrl+O 里输入完整 `.comp` 路径会进入该文件夹而不是打开项目。
  双击 / 命令行 / 最近项目三条路不受影响。若要修，应改用文件夹选择器或自绘列表。
- 菜单里条件禁用的项（取消编组 / 剪贴蒙版 / 合并 / 清除参考线 / 最近项目）在 UIA 树里
  **不出现**，看起来像"菜单缺项"——本轮已逐条与 `MenuBar.svelte` 比对确认是禁用而非缺失。
- 保存对话框里 `Ctrl+A` 经 MCP 发送**不会**选中文件名文本（导致路径被追加成非法名）；
  用 `Type(clear:true)` 或先点选字段再输入即可。

## 4.5 第二轮（round 2）结果

| 项 | 操作（Windows MCP） | 证据 | 判定 |
| --- | --- | --- | --- |
| V03 参考线 | 从顶部标尺拖出 | 画布出现青色水平参考线 | ✅ |
| C25 清除参考线 | 拖出参考线后开视图菜单 | **修复前**：菜单里根本没有"清除参考线"（见 §4.6）；**修复后**：出现 → 点击 → 参考线消失且菜单项随之消失 | ✅（修 §4.6 后） |
| C26 全选 / C11 剪切 | Ctrl+D（无选区）→ Ctrl+X；再 Ctrl+A → Ctrl+X | 无选区时剪切是 no-op（画布保持白）；全选后剪切把画布变透明（棋盘格），**并可见右侧蚂蚁线** | ✅ |
| T01/C45 变换会话 | Ctrl+T → 拖右侧把手 → Enter | 变换框 + 8 把手；拖动实时缩放图层（出现透明边），Enter 提交后保持 | ✅ |
| C02 打开（命令行/关联） | `Compositor.exe <path.comp>`（第二实例） | 前台实例新开标签 "demo1"，第二实例退出，进程数=1 | ✅ |
| C32 曲线… | 图像 ▸ 曲线… → 拖白场锚点 → 确定 | 对话框（RGB/Red/Green/Blue 页签 + identity 曲线 + 提示）；提交后画布由白变灰 | ✅（修 §4.7 后） |
| C14 快捷键表单 | 编辑 ▸ 键盘快捷键… | 表单：搜索框 + 「菜单（46）」逐条（Undo Ctrl+Z … Save As Ctrl+Shift+S …）并可录制 | ✅ |
| V10 内存压力事件 | 观察日志 | `内存压力 "moderate"/"critical"` 随阈值切换（ctx 修好后 EventsEmit 才通） | ✅ |

### 4.6 【缺陷·已修】View 菜单的勾与"清除参考线"永远冻在挂载那一刻（`MenuBar.svelte`）

`const viewState = $derived(viewToggleState())`：`viewToggleState()` 内部是 `get(store)` 和普通类字段
（`guidesState.guides.length`）—— Svelte 5 不会把这些登记成 `$derived` 的依赖，于是这份核对表
"算一次就永不更新"。真机表现：画布上明明拖出了参考线，视图菜单里**没有"清除参考线"**
（`disabled: !viewState.hasGuides` 恒为真），五个开关的勾也永远停在初始值。

**修法**：MenuBar 直接用 `$showGrid / $showGuides / $showRulers / $snapEnabled / $lockGuides /
$showsTransformControls / $visibleGuides` 逐个响应式读取（`$visibleGuides` 每次改动都 refresh，
正好当信号源）；无人再用的 `viewToggleState()` 从 actions.ts 删除，避免后来者照着抄。

### 4.7 【缺陷·已修】图像菜单的九条调整一条都打不开（Go 侧默认值缺失）

真机表现（日志实证）：

```
FRONTEND_INFO: console.warn | 无法开始滤镜会话 调整会话缺少 adjustment 参数
```

菜单路径 `beginFilter(kind, label, true)` 只发 kind、`adjustment` 为 null，而 `BeginFilterEdit`
要求调整类会话必须带 adjustment，于是曲线/色阶/色相饱和度/曝光/渐变映射/颗粒/添加杂色/黑白/色彩平衡
全部不可达（矩阵 C32/C33 记 ☑ 是按"调整层内核 + 黄金基准"判的，用户路径其实断的）。

**修法**：Go 侧按 kind 补 identity 默认值（`fillAdjustmentDefaults`）—— 菜单不需要知道每种调整的
完整参数表。两处不能只填 kind：`Curves` 的空通道会让 `curveValue` 走 `make([]float64, -1)`
**panic**（已加 len<2 → identity 的守卫 + 回归测试），`Levels` 的全零 range 会被 normalize 成
black=0/white=1/gamma=0.1（一张过曝图，不是 identity），故补 `domain.IdentityLevelRange()`。
回归测试 `internal/bridge/adjustment_session_test.go`（12 种调整逐条从菜单载荷开启）、
`internal/render/curve_empty_test.go`。

## 4.8 第三轮（round 3）结果 —— 无新缺陷

前两轮的修复在本轮回归中全部保持；本轮 8 项全部通过，未发现新问题。

| 项 | 操作（Windows MCP） | 证据 | 判定 |
| --- | --- | --- | --- |
| C35 画布大小… | 图像 ▸ 画布大小… → 宽 2400 → 确定 | 表单含宽/高/9 向锚点/「扩展画布时填充」；状态栏 `1920 × 1080` → `2400 × 1080` | ✅ |
| C35 图像大小… | Ctrl+Alt+I → 宽 1200 → 确定 | 表单含宽/高/分辨率/「保持长宽比」；`2400 × 1080` → `1200 × 540`（高度跟随） | ✅ |
| C07 导出 JPEG… | Ctrl+Alt+Shift+S → 质量滑杆/缩放 → 导出… → 另存为 | 对话框有实时预览（`1200×540`、估算 11 KB）；落盘 `export1.jpg` **10811 B**、1200×540、`ImageFormat.Jpeg` | ✅ |
| C12 拷贝合并 | Ctrl+Shift+C → 读 Windows 剪贴板 | **CF_DIB 2,592,040 B，BITMAPINFOHEADER = 1200 × -540 @32bpp**（尺寸与文档一致）；采样像素 (600,270) 白不透 / (10,10) 全透明（画布扩边留下的透明带） | ✅ |
| P02 粘贴回文档 | Ctrl+V | 图层面板 `[背景]` → `[图层 1, 背景]` | ✅ |
| C08 关闭标签脏确认 | 脏文档 Ctrl+W | 表单「"未命名"有未保存的更改，仍要关闭吗？」+ 取消/关闭；取消后标签保留 | ✅ |
| C03 打开最近 | 文件菜单 | 逐条「打开最近：demo1.comp <完整路径>」+「清除最近记录」 | ✅ |
| C04/I01 导入图像 | 文件 ▸ 导入图像… → 选 export1.png | 图层 `[export1, 图层 1, 背景]` | ✅ |

> C12/P02 这一条此前在矩阵里挂着"winclip 的 Win32 手递手没有自动化测试，只在真机上跑"的
> caveat —— 本轮用真机剪贴板读回做了逐字段核对（格式、字节数、DIB 头宽高与位深、像素采样），
> 双向（写 + 读回文档）都通。

## 4.9 第四轮（round 4）：分割/抠图链路整条是死的（3 个缺陷，已修）

矩阵把 S06（对象选择/主体/移除背景）记为 ☑，依据是"内核 + 黄金基准 + 单测"。真机点下去发现：
**这条链路一次都没成功跑过** —— 而且是三个独立缺陷叠在一起，每个都足以让它失败。

### 4.9.1 【缺陷】`ml.EnsureModel()` 零调用者：UI 承诺的自动下载从未发生

滤镜面板白纸黑字写着「首次使用会下载 4.6 MB 分割模型（u2netp，Apache-2.0）」，而
`internal/ml/subject.go` 里唯一负责下载+校验（SHA-256、大小下限、临时文件改名）的
`EnsureModel()` **没有任何调用者**（全仓 grep 只有它自己的声明）。于是首次使用直接失败，
错误文案还让用户去运行一个**根本不存在的**「下载分割模型」命令。

**修法**：新增 `EnsureReady()`（先 `EnsureModel()` 再 `setup()`），`RunSubjectMaskRaw` 改用它。
真机证据：首次点「选择主体」后 `%APPDATA%\Compositor\models\u2netp.onnx` 出现，
**4,574,861 字节**。

### 4.9.2 【缺陷】硬编码的 ONNX 输出名，推理必然失败

模型加载后报：

```
选择失败
分割推理失败: Error running network: Invalid output name: output.0
```

会话是用写死的 `[]string{"input.1"}, []string{"output.0"}` 建的。用 `ort.GetInputOutputInfo`
读出这个固定版本 u2netp 的真实接口：

```
INPUT  "input.1" dims=[1 3 320 320]
OUTPUT "1959" … "1966"   （8 个同形状输出 [1 1 320 320]，张量名取自生产节点）
```

**修法**：建会话前读图接口，用 `inputs[0].Name` / `outputs[0].Name`（d0 就是 rembg 取的那张显著图）。

### 4.9.3 【缺陷】初始化失败被 `sync.Once` 永久缓存 + 错误只进 console

`setup()` 用 `initOnce` 把"模型缺失"这个失败也缓存住了 —— 即使模型随后下载成功，
本次进程内也永远不会再初始化（`initErr` 恒为 `ErrModelMissing`）。改成 mutex+bool，
**只缓存成功**。

另外 `actions.ts` 的 `runSelect` /`filters.ts` 的 `beginFilter` 失败只 `console.warn`：
用户点「选择主体」看到的是"什么都没发生"。现在弹出提示框，并且错误拆成两条可操作信息：
`ErrRuntimeMissing`（把 onnxruntime.dll 放到 exe 同级）/ `ErrModelMissing`（首次使用会自动下载）。

### 4.9.4 真机验收（修复后）

| 项 | 操作 | 证据 |
| --- | --- | --- |
| S06/C28 选择主体 | 生成"噪声底 + 红色椭圆"合成图 → 导入 → 选择 ▸ 选择主体 | 画布上出现**贴着椭圆轮廓的白色蚂蚁线**（模型真的找对了显著物） |
| C42 移除背景 | 滤镜 ▸ 移除背景… → 确定 | 噪声背景被抠掉（该层只剩椭圆，透明处露出下层），蚂蚁线仍在 |
| 模型自动下载 | 首次使用 | `%APPDATA%\Compositor\models\u2netp.onnx` 4,574,861 B |
| 优雅降级 | 缺 runtime 时 | 弹框 + 日志给出准确原因（不再是"运行不存在的命令"） |

> **分发待办**：`onnxruntime.dll` 必须随安装包发布（本轮为了真机验证，把 Go 模块
> `onnxruntime_go@v1.36.0/test_data/onnxruntime.dll`（16,149,344 B）拷进了 `build/bin/`；
> `build/bin` 已 gitignore，不进库）。发布链路需要把运行时纳入 NSIS 载荷，否则用户拿到的是
> 一个"模型会自动下载但装了也跑不起来"的包。

## 4.10 第五轮（round 5）：污点修复笔刷（J）两个叠在一起的缺陷

矩阵 T07「污点修复 (J)」记 ☑（票 35：内核 + 黄金基准）。真机按下 J 在画布上拖，得到的是

```
FRONTEND_ERROR: unhandledrejection | 没有进行中的修复笔刷
```

而且画布**纹丝不动**。挖下去是两个独立缺陷：

### 4.10.1 【缺陷】BeginHealStroke 与 HealPoint/EndHealStroke 竞速

`CanvasSurface.svelte` 的指针流是"down 时 `void BeginHealStroke(...)`，move 时 `void HealPoint(...)`，
up 时 `void EndHealStroke()`" —— 三个都是**不等结果的异步 RPC**。一次正常速度的拖拽里，
move/up 的调用几乎必然先于 Begin 的往返落地，Go 侧此时 `sess.heal == nil`，于是
HealPoint/EndHealStroke 双双报"没有进行中的修复笔刷"，**整笔修复被丢掉**，会话还可能残留
（残留后下一次 Begin 会报"已有进行中的修复笔刷"）；两个 rejection 都没人接，
冒到 `window.onunhandledrejection` 才被日志抓到。

**修法**：加一条 promise 链把三步排成一队（`queueHeal`），每步挂 catch。
（画笔那条路因为 move 走 rAF 合并、且首个采样点在 BeginStroke 内，天然躲过了这个竞速。）

### 4.10.2 【缺陷】修复笔刷提交后不刷新画布

排完序之后错误没了，但**画布依旧不变**：`EndHealStroke` 只把结果写在 Go 侧并推进 rev，
前端必须重新取图才会看到 —— 画笔在 `finally` 里调了 `reloadDocument()`，修复笔刷这条**漏了**。
补上之后：实心黑斑 → 拖一笔 → 变成被周围白色插值掉的柔和残影（未覆盖到的部分留着）。

### 4.10.3 同轮确认（与矩阵一致，非缺陷）

| 项 | 操作 | 结果 |
| --- | --- | --- |
| T08 仿制图章 (S) | 选中 S 后画布拖拽 | 无任何反应（矩阵 ◐："内核齐备、无 RPC、画布无分支"） |
| T09 涂抹 (R) | 选中 R 后画布拖拽 | 无任何反应（矩阵 ◐ 同上） |

## 4.11 第六轮（round 6）：滤镜扫一遍 + 内容感知填充的定性结论

### 4.11.1 滤镜族（逐个打开，日志无报错）

| 滤镜 | 结果 |
| --- | --- |
| 运动模糊 / 添加杂色 / 晕影 / 辉光 / 色调对比 / 镜头校正 | 对话框全部正常打开（无失败日志） |
| 抖动（C39） | 打开正常：风格下拉 **11 项**（与"11 种风格"一致）+ 像素大小/色调数/扩散/密度/对比度/网格大小/角度/文字大小(字符集)/行间距/辉光/珠点 |
| Camera Raw | 不弹标准对话框，而是右侧停靠面板（预期行为） |
| 高斯模糊 / 移除背景 | 前几轮已各自验证过"提交改像素" |

抖动提交后的可见性**未能取证**：测试图是"白底 + 黑笔迹"双色图，低像素大小下抖动近似恒等
（内核另有 `tests/golden/cases/dither-*.json` 黄金逐位覆盖，全绿）。记为"未取证"，不记为通过。

### 4.11.2 内容感知填充（⇧⌫ / C17 / S07）：结果明显不对，**已定性、本轮未修**

真机步骤：新建 1920×1080 → 导入"噪声底 + 红色椭圆"合成图 → 选择 ▸ 选择主体 → ⇧⌫。
结果与预期相反：**未被选中的照片纹理变成了选区主体的红色**，而选区内部（椭圆）保持原样。

逐层排除：

1. **内核没问题**。新增回归测试 `internal/render/contentfill_polarity_test.go`：64×64 绿底 +
   中央 16×16 红斑，mask 只标中央 → 填完中央变绿、角落不变 → `ContentFill` 填的确实是
   "被标记的像素"，与 `tests/golden/c/ContentFill.c` 的 `target[p]=mask!=0` 一致。
2. **极性也没接反**。临时插桩（已移除）打印实际到达内核的蒙版：

   ```
   DIAG contentfill: selW=1920 selH=1080 selMask=284576/2073600 (13.7%)
                    layerMask=284576/2073600 (13.7%) grown=1920x1080 selRect=0,0,1920,1080
   ```

   selMask 与 layerMask 数量一致（映射没丢），且 **13.7% ≈ 导入照片面积（640×480 = 307200，
   占画布 14.8%）的 93%**。
3. **真正的分歧在选区本身**：u2netp 把"贴在白底上的那张照片"整体当成了显著物，而不是照片里的
   红椭圆（蚂蚁线看着像贴着椭圆，是因为照片边缘紧贴噪点、虚线不明显）。于是"要填的目标"几乎
   覆盖整层，**几乎没有取样源**；而 `ContentFill` 在 `donorCount == 0` 时是直接返回的 no-op，
   可画面又确实变了 —— 说明后续 `blendThroughSelection` / 裁剪回写那一段还有第二个因素。

**结论（不夸大）**：内核正确、蒙版映射正确；问题在"选择主体把整张照片当主体"与"填充目标≈整层
导致无取样源"这条链路上。要给出确定的修法，需要专门一轮把 `grown` 网格来源（层 vs 画布）、
`blendThroughSelection` 的极性、以及主体掩码在"照片贴在画布上"场景下的语义一起钉死。
本轮**不改** —— 按"不主观推断"的原则，没有定位到根因就不动代码。

## 4.12 第七轮（round 7）：图层族 + Camera Raw 面板

| 项 | 操作（Windows MCP） | 证据 | 判定 |
| --- | --- | --- | --- |
| C47 剪贴蒙版 | 图层菜单 ▸ 创建剪贴蒙版 → 再打开菜单 | 菜单项变成 **「释放剪贴蒙版 Ctrl+Alt+G」**；再点一次变回「创建剪贴蒙版」 | ✅ |
| L03 图层不透明度 | 图层面板滑杆点一下 | 读数 100 → **11**；画布上该层笔迹随即变淡（与下层黑色笔迹对比明显） | ✅ |
| C36 翻转画布 / C52 翻转图层 | 逐个打开 图像 / 图层 菜单找"翻转" | 两个菜单里**都没有**任何 翻转/Flip 条目 | ❌（与矩阵 ◐"无菜单/快捷键"一致） |
| C41 Camera Raw | 滤镜 ▸ Camera Raw 滤镜… | 右侧停靠面板：标题「Camera Raw 实时预览」+ 直方图/阴影裁剪/高光裁剪 + 分组 光·颜色·特效·细节·光学·几何·校准（可折叠）；展开后可见 全局:明度/混合/平衡、质感/清晰度/去雾/光晕(样式·范围·扩散·暖度)/暗角(样式·中点·圆度·羽化·高光保护)/颗粒(大小·粗糙度) 等滑杆 | ✅ 面板与实时预览 |
| C41 提交 | 找面板底部 取消/确定 | **未取证**：UIA 树里底部只暴露了提示文字「预览 ≤2048px · 确定按全尺寸提交」，两个按钮元素没出现；两次坐标裁剪截图也没拍到按钮。不排除是"窄面板 + 长提示文字把按钮挤出可视区"，但**没有确证**，因此不记为缺陷，只记"提交未取证" | ⏳ |

> 附带确认（预期行为，非缺陷）：滤镜会话打开期间菜单栏被模态守卫挡住（点「图像」不弹菜单），
> 与"滤镜对话框是模态"的设计一致。

## 4.13 第八轮（round 8）：对本批修复做两轴代码评审 → 逐条修复

用 code-review 技能把 HEAD(46d13cf)..工作树 的改动交给两个并行子代理（Standards 轴 / Spec 轴），
两轴共报 18 条。**除"范围外"的 1 条外全部修掉**：

### 已修（Spec 轴）

1. **调整默认值只补了 2/4 个入口**（最严重）。第一版只改 `BeginFilterEdit` 与 `ApplyFilter`，
   而 `UpdateFilterPreview` 与 `CommitFilter` 仍走裸 `normalized` —— 对话框自己发的载荷是"部分"的：
   色阶吸管只给 3 个 range（`[4]LevelRange` 第 4 个零值 → white=1/gamma=0.1 的过曝图），
   `setCurvePoints` 只给一个通道。修法：抽出 `normalizeAdjustment(kind, *filterParams)`，
   **四个入口全部调用**。新增 `TestAdjustmentPreviewAndCommitFillDefaults`，
   并做变异验证（去掉预览入口那一行 → 测试确实 FAIL，加回 → PASS）。
2. **修复笔刷失败只进 console**。`queueHeal` 的 catch 补 `window.alert`（与选择主体/滤镜一致）。
3. **修复笔刷提交失败不刷新画布**。`EndHealStroke` 外面包 `try/finally`，与画笔同款
   （画笔在 `finally` 里调 `reloadDocument`，修复笔刷这条漏了）。
4. **日志 tap 会静默丢消息**：旧版用 `reporting` 布尔挡重入，**上报在途期间产生的 warn 全被丢掉**；
   200 条到顶也无声。改成有界 FIFO 串行 drain（上限 500，满了丢最旧），到顶补一行汇总并计数。
5. **测试可能永远为真**：`TestEmptyBrushStrokeRecordsNothing` 所有断言都在 `if bytes.Equal(...)` 里，
   等于没测。重写成 `TestBrushStrokeOutsideLayerRecordsNothing`（落点在图层外 → 覆盖为空 →
   断言像素不变、撤销栈为 0、rev 不推进），**真的走到了早退分支**。

### 已修（Standards 轴）

6. `ml/subject.go` 过时注释（"ErrModelMissing reports the model or runtime"）→ 改成两条错误各自的语义。
7. `render/lut.go` 注释谎称"原版把缺失的表当 identity" —— 实际 Swift 的 Curves 校验要求 2–32 个锚点、
   否则抛 `ProjectError.invalid`。改成**显式标注 DIVERGENCE 并说明为什么必须分叉**（这里可达：
   菜单会话只发 kind、对话框只填一个通道，不守就会 `make([]float64,-1)` panic）。
8. `CanvasSurface.svelte` 注释中英混排"把 three 个调用" → "把三个调用"。
9. `main.ts` 新增的两段英文注释 → 中文（与文件其余注释一致）。
10. `docs/audit/D-gui-acceptance.md` 头部"本轮共发现 4 个缺陷"与后文八轮记录矛盾 → 改成八轮汇总。
11. `domain.IdentityLevelRange()` 与 `render.identityLevelRange` 两份权威 identity → render 改为调用 domain。
12. `MenuBar` 的 `hasGuides` 原读渲染列表 `$visibleGuides`（注释自认"正好可以当信号源"）→
    改读**文档真源** `$document.guides`；`visibleGuides` 导入随之删除。
13. ONNX 输出按**位置**取（`outputs[0]`）且注释断言"就是 d0" → 改成**按形状挑**（1×1×H×W，
    与输入 H/W 对齐），注释不再断言导出顺序。
14. 重复的 8 行"kind + 默认值"块（两处）→ 合并进 `normalizeAdjustment`。
15. `workspace.go` 注释"empty or fully transparent dab"与实现不符 → 改成"提交后像素逐字节相同"。

### 未修（评审判断，保留）

- `.gitignore` 增加 `logs/`：评审标为"与修复无关的范围蔓延"。**保留**——真机验收的启动日志与截图
  都在 `logs/`，不忽略就会持续污染 `git status`；仓库本来就忽略了 `app-*.log`。

回归：`go build` / `go vet` / `go test ./...`（20 包）全绿；`svelte-check` 0 错 0 警；`vitest` 713 通过。

## 5. 下一轮待测

图层蒙版/特效入口、调整族（曲线/色阶/色相饱和度/曝光/渐变映射/颗粒/添加杂色/黑白/色彩平衡）、
画布大小/图像大小/修剪、选择族（全选/取消/反选/主体/对象）、视图族（网格/参考线/标尺/对齐/变换控件）、
导出 JPEG、剪贴板（拷贝合并/选区像素）、快捷键表单、外部变更提示、内存压力事件。
