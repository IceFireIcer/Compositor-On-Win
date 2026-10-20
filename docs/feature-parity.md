# 功能对等矩阵（Feature Parity Matrix）

> **本文件是"一个功能都不能落下"的唯一权威清单**。以原版源码为锚点逐项核对：菜单命令面（`reference/Swift/Compositor/CompositorApp.swift`）、工具枚举（`EditorSession.NavigationTool`）、调整种类（`LayerAdjustment.AdjustmentKind`）、滤镜种类（`Filters.FilterKind`）、图层特效（`LayerEffects.LayerEffectKind`）、形状（`ShapeTool.ShapeKind`）、混合模式（`LayerAppearance.LayerBlendMode`）。
> 状态：☐ 计划中 → ◐ 实现中 → ☑ 已验收（对照 testing.md 缝合点）。拆票（`/to-tickets`）时逐行核对本矩阵；新增功能必须先在此登记。

## A. 工具（16，`NavigationTool`）

| # | 功能 | 原版锚点 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| T01 | 移动/变换 (V) | `.move` | 33-35 | M3 | ☑ 票17：会话接线在 `state/transform.ts:75`（⌘T 开）/`:88`（⏎ 提交，走 `ApplyLayerTransform`）/`:112`（Esc），画布 V 工具拖图层与 8 把手（`CanvasSurface.svelte:522,867`），Go 非破坏写 `layer.transform`（`internal/bridge/layer_transform.go:30`，`layer_transform_test.go`）。自由扭曲（⌘ 拖角）不支持 |
| T02 | 选框 (M)：矩形/椭圆双模式 | `.marquee` | 26 | M3 | ☑ 矩形/椭圆 region 纯函数 + 测试（`state/selection.ts:210,229`、`selection.test.ts`）；画布分支在 `CanvasSurface.svelte:1122`（`beginSelection` 收手势，`:760-770` 出 region），双模式切换在工具选项栏（`ToolOptionsBar.svelte:49-55` 的 rect/ellipse select）。**此前的「没有 marquee 分支、双模式未接」已过期** |
| T03 | 套索 (L)：自由手/多边形双模式 | `.lasso` | 26 | M3 | ☑ 自由手 region 纯函数 + 测试（`state/selection.ts:252`）；画布分支在 `CanvasSurface.svelte:1122`，**多边形模式已接**（`:789-795` 开多边形、`:819` 双击闭合、`:1380` 预览），模式切换在 `ToolOptionsBar.svelte:61-67`。**此前的「无分支、多边形未接」已过期** |
| T04 | 魔棒 (W)：颜色魔棒/对象选择，Tab 切换 | `.wand` | 27 | M3/M6 | ☑ 票15 颜色魔棒 + 票37 对象选择（内核 + 黄金基准 `golden_test.go:71`）。**2026-10-19 核实：原记录的「画布无魔棒分支、Tab 切换未注册」两条都已过时** —— `CanvasSurface.svelte:1114` 的魔棒分支与 `pickSelection`（`SelectWandAt`/`SelectObjectAt`、读 `wandSettings`、区分 add/subtract/intersect）都在；Tab 也已注册（`chords.ts:148` + `actions.ts:832`，`actions.test.ts:75` 有测试）。**真缺口是这条接线没有测试**，已补 `CanvasSurface.browser.test.mjs` 的魔棒点击用例（点击 → 后端被调用 → 选区装进 store），去掉分支即失败 |
| T05 | 裁剪 (C)：比例/对称/选区起点 | `.crop` | 36 | M3 | ☑ 票18 尾项：CropState/比例/对称/选区起点 + 43 测试（`state/crop.ts:488`、`crop.test.ts`），覆盖层挂在画布上（`CanvasSurface.svelte:1628`），提交走 `commitCropSession` → `commitCropToDocument` → Go `Crop`（`crop_test.go` 5 例），C 键工具已注册（`commands/entries.ts:114`）。**此前的「零生产消费者、Go 无 Crop RPC」已过期** |
| T06 | 笔刷 (B) / 橡皮 (E)：大小/硬度/不透明度/平滑、Shift 直线 | `.brush` + `BrushStroke.swift` | 18-19 | M4 | ☑ 笔刷本体（`CanvasSurface.svelte` PAINT_TOOLS + `internal/render/brush.go` 14 测试）；**橡皮有**：E 键 `Canvas & Layers:Eraser`（`table.ts:122`）→ `setBrushMode("erase")`（`actions.ts:813`），工具选项栏有 paint/erase 模式（`ToolOptionsBar.svelte:226-229`），笔画带 `erase` 标志（`canvasDrawing.ts:107`）。**参数与选项栏也有**：`[`/`]` 改大小、Shift+`[`/`]` 改硬度（`table.ts:141-142,159-160` → `actions.ts:860-863`），数字键改不透明度，工具选项栏在。**此前的「橡皮不存在、参数硬编码、[ / ] 对画笔无效、无笔刷选项栏」整条都是错的** |
| T07 | 污点修复 (J)：内容感知/创建纹理/临近匹配 | `.spotHealing` + `HealPixels.c` | 21 | M6 | ☑ 票35 |
| T08 | 仿制图章 (S)：对齐开关、单层/全层取样、⌥点设源 | `.cloneStamp` | 20 | M4 | ☑ 内核 `CloneStamp` + 13 测试（`internal/render/stampfill.go:60`、`stampfill_test.go`）；**RPC 是 `BeginRetouchStroke` 的 kind `clone`**（`retouch_session.go:122`，`cloneResultFromCoverage` 折深层）；画布分支 `CanvasSurface.svelte:1150`，**⌥ 点设源**在 `:1152`（`setCanvasCloneSource`），取样范围在 `retouch_session.go` 的 `SampleAllLayers` |
| T09 | 涂抹 (R)：模糊/涂抹/液化三模式 | `.blur` + `MetalWarp` | 22 | M4 | ☑ 内核 `WarpStroke` 三模式 + 测试（`internal/render/smudge.go:40,111`、`smudge_test.go`）；**RPC 是 `BeginRetouchStroke` 的 kind `blur`/`smudge`/`liquify`**（`retouch_session.go:79,109,111`）；画布分支 `CanvasSurface.svelte:1150`（`$activeTool === "blur"`，`:1162` 的失败提示就叫「涂抹失败」） |
| T10 | 渐变 (G)：可再编辑渐变层 | `.gradient` | 23 | M4 | ☑ `render.GradientFill`（`gradient.go:76`）有调用者：`gradient_session.go:71` 的交互式会话（拖动预览 + 松手一步提交）与 `DrawGradient`（`drawing.go:213`）两条路；前端 `state/canvasDrawing.ts:21` 接 RPC，画布有渐变拖拽分支。票面自记的差异：**不产生可再编辑的渐变层**，落成栅格 |
| T11 | 形状 (U)：矩形(圆角)/椭圆/线，Shift-U 切换，保持可编辑 | `.shape` + `ShapeKind` | 24 | M4 | ☑ 形状渲染已接合成（`internal/layerrender/shape.go`、`internal/render/composite.go:203`）；**创建也有**：Go `DrawShape`（`drawing.go:41`，矩形/椭圆/线 + 圆角/线宽校验，落成带 `ShapeStyle` 的可编辑矢量层）+ 前端 `canvasDrawing.ts:202` 的拖拽手势，Shift-U 切换在 `table.ts:149` |
| T12 | 文字 (T)：段落框/内联多行/IME | `.type` + `TypeTool.swift` | 43-45 | M8 | ☑ 票44+45：创建/编辑/提交/取消/IME/合成/往返 + 逐 run 样式与段落框缩放 |
| T13 | 吸管 (I)：取样环（可开关） | `.eyedropper` + `showsSampleRing` | 25 | M3 | ☑ `state/color.ts:180` samplePixel + `color.test.ts`；画布有吸管分支（`CanvasSurface.svelte:1107`）、取样环（`:1467` 读 `$showsSampleRing`）、滤镜吸管吞点击（`:1055` `eyedropperClick`）；取样环开关在工具选项栏（`ToolOptionsBar.svelte:355` → `setShowsSampleRing`）。**此前的「零消费者、0 命中」已过期** |
| T14 | 抓手 (H) / 空格平移 | `.hand` | 4 | M3 | ☑ 空格/中键/抓手三条平移路径（`CanvasSurface.svelte:651`）+ 视口 `state/viewport.ts` 与 `viewport.test.ts`；H 键已接 |
| T15 | 缩放 (Z)：点击/拖拽 | `.zoom` | 3 | M3 | ☑ 缩放工具点击/Alt 反向（`CanvasSurface.svelte:658`）+ 滚轮锚点缩放 + ⌘0/⌘1/=/−（`table.ts:62-65`、`viewport.test.ts`） |
| T16 | 无工具 (A)：画布点击无操作 | `.idle` | — | M3 | ☑ A 键选 idle（`actions.ts` 工具字母），画布对 idle 不落任何分支（`CanvasSurface.svelte:612-698`）＝点击无操作 |

## B. 菜单命令（`CompositorApp.swift` 全量）

**File**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C01 | New Canvas…（欢迎屏） | ⌘N→Ctrl+N | 9 | M0 | ☑ 票02：欢迎屏 + 新建画布表单（`App.svelte:174`、`components/NewCanvasSheet.svelte`），限额来自 Go（`internal/domain/limits.go:40`、`limits_test.go`）；文件菜单与 ⌘N 可达 |
| C02 | Open Project… / 拖放打开 | ⌘O | 1 | M1/M9 | ☑ 打开对话框/最近/双击关联/命令行参数都通（`state/shell.ts:50 openPath`、`internal/bridge/project_io.go:94`）；**拖放也会开项目**：`App.svelte` 的 `onDroppedPaths` 走 `routeDroppedPaths`（`state/workspace.ts:317`），它按 `isCompProjectPath` 分流 —— `.comp` 走 `OpenPathRPC`（需要转换时回落确认表单），其余才是图像导入。**此前的「把 .comp 拖到窗口走图像导入」已过期**（引用的 `App.svelte:112 importDroppedPaths` 早已改道） |
| C03 | Open Recent（含清除） | — | 53 | M9 | ☑ 票49：文件菜单逐条列出最近项目（完整路径在 tooltip）+ 清除最近记录；列表上限 10、读取时过滤已移动/删除的文件（RecentProjects.refresh 语义），与 Windows 跳转列表共用一份 |
| C04 | Import Images… | — | 46 | M7 | ☑ 票40（多选批次，RAW/HEIC 随票41） |
| C05 | Save / Save As | ⌘S / ⇧⌘S | 49 | M1 | ☑ 票49：保存/另存为共用 `saveProject()`（`state/workspace.ts:128`）→ Go `SaveProjectDialog`（`project_io.go`），原子保存与往返测试（`project/store_test.go`、`document_test.go:671`） |
| C06 | Export PNG… | ⇧⌘E | 48 | M7 | ☑ 票42 |
| C07 | Export JPEG…（实时预览内可缩放） | ⇧⌥⌘S | 48 | M7 | ☑ 票42 |
| C08 | Close Project / 退出逐标签确认 | ⌘W | 2 | M1 | ☑ 关闭标签有脏确认（`actions.ts` closeActiveProject + `state/confirm.ts`）；**退出应用也有**：`Service.OnBeforeClose`（`close.go:12`，Wails 的窗口关闭钩子）遍历**所有**标签，脏的或有在飞编辑/保存的都算，弹原生 Yes/No 且默认 No，只有明确 Yes 才放弃工作；弹框前先放掉工作区锁，免得挡住正在完成的编辑与保存。`close_test.go` 6 条覆盖（干净工作区不弹、脏的要明确 Yes、弹框失败/未知答案时 fail-closed、保护在飞编辑与保存、无 context 时 fail-closed、弹框期间不持锁）。**此前的「全仓无 OnBeforeClose」是错的** |
| C09 | Check for Updates… | — | 53 | M9 | ◐ 票50：`帮助 ▸ 检查更新…` → `CheckForUpdates`（feed 比版本）→ `UpdateSheet`（版本/发行说明/进度）→ 下载并校验（sha256 + Ed25519）→ 启动静默安装器；启动时静默检查一次。链路每环有测试，但**没有在真实 Release 上跑过**（首版发布才能确认），故记 ◐ |

**Edit（剪贴板/填充组）**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C10 | Undo/Redo（菜单名带操作名） | ⌘Z / ⇧⌘Z | 57 | M1 | ☑ 撤销/重做接线（`actions.ts:728-729`，菜单 `MenuBar.svelte:407-408`），**标题带操作名**：Go 的 `History.UndoName/RedoName`（`history.go:113,122`）经新 RPC `HistoryNames`（`history_names.go`）送到前端 `state/historyNames.ts`，`undoMenuLabel/redoMenuLabel` 按上游 `"Undo \(name)"` 拼（空栈退回裸标题），查询挂在每次快照上与 layerDepth 同一套序号守卫 |
| C11 | Cut / Copy / Paste（像素与文字域分流） | ⌘X/C/V | 55 | M3/M9 | ☑ 票48：选区像素拷/切（剪切留洞、可撤销）、整层复制（含编组/调整层）、外部图像粘贴、⌘V 落层丢选区；文字域由原生 textarea 自己处理（输入框内快捷键让位，菜单项同时禁用）。对照 SelectionClipboardTests 的 9 个用例：原位粘贴/剪切回填/形状选区（套索软边有测试）/选区包围盒裁剪已覆盖，变换会话那 4 个用例属票 16。**系统剪贴板的 Win32 手递手（winclip 的 C 侧）没有自动化测试**——接缝在测试里是内存假实现，纯 Go 的 CF_DIB/PNG 解码有测试 |
| C12 | Copy Merged | ⇧⌘C | 48 | M7 | ☑ 票42（CF_DIB；剪贴板像素/整层 UI 通道随票48） |
| C13 | 整层复制/粘贴/跨标签拖拽 | — | 16 | M3 | ☑ 票48：`copyLayersInto` 跨会话复制子树（新 UUID/资产、位图深拷贝、引用重映射、目标画布居中、一步撤销）+ 图层面板拖到标签栏（`TransferLayer`）+ ⌘V 的跨标签分支（`copiedLayerFor` 命中别的会话 → `copyLayersInto`，有测试）。面板内部的拖拽重排仍属票 20 |
| C14 | Keyboard Shortcuts…（重映射表单） | — | 52 | M9 | ☑ 票47：表单（录制/冲突提示/恢复默认/持久化）+ 117 条定义表 + 三域分流。**唯一一处有意偏离原版键位**：Last Filter 原版 ⌃⌘F（modifiers 5）在 Windows 上 bit 4 与 bit 1 都渲染成 Ctrl，折成同一键会写出 `Ctrl+Ctrl+F`，故取 Command\|Option = **Ctrl+Alt+F**（modifiers 3；搜索仍是 Ctrl+F，不冲突）。落在 `table.ts:89` 与 `table.test.ts:50` 的注释里 |
| C15 | Fill with Foreground/Background Color | ⌥⌫ / ⌘⌫ | 42 | M4 | ☑ 票23：`FillSelection`（`internal/bridge/selection_commands.go:59`，选区裁剪/软边/一步撤销，`fill_test.go` 7 例）+ 编辑菜单两项（`MenuBar.svelte:280-291`）+ ⌥⌫/⌘⌫（`table.ts:72-73`） |
| C16 | Clear Selection Pixels | — | 42 | M4 | ☑ 票42+47：`FillSelection(mode="clear")`（`selection_commands.go:59,73`，`fill_test.go:85` 断言选区变透明）+ 编辑菜单项「清除选区像素」（`MenuBar.svelte:448`，无选区时置灰）+ Delete 键在有选区时走它（`actions.ts:864`，`actions.test.ts` 四条覆盖）。**此前的「只有定义、无入口」已过期** |
| C17 | Content-Aware Fill… | ⇧⌫ | 42 | M6 | ☑ 票35 |

**View（画布组）**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C18 | Fit Canvas / Actual Pixels / Zoom In/Out | ⌘0/⌘1/=/- | 3 | M3 | ☑ 票13+47：视口命令 + 快捷键/视图菜单 |
| C19 | Pixel Grid 开关（≥800%） | — | 5 | M3 | ☑ 像素网格 ≥800% 且在开关打开时显示（`CanvasSurface.svelte:406` 的 `$showsPixelGrid && zoom >= PIXEL_GRID_ZOOM`）；**开关本身有 store 与视图菜单项**（`state/view.ts:32` → `MenuBar.svelte:787` 切换、`:162` 读）。**此前的「showsPixelGrid 没有 store/入口」已过期**；`Menus:Show Grid`（`actions.ts:683`）是另一件事（布局网格，见 C22） |
| C20 | Snap 总开关 | — | 7 | M3 | ☑ 票76：总开关与四个子目标都接上并持久化（`state/view.ts` 的 `snapEnabled`/`snapToggles`，键名照抄原版 ToolDefaults）；菜单「对齐」+ ⇧⌘; + 命令面板动态勾选；消费端见 V04 |
| C21 | Show Transform Controls | ⌘H | 34 | M3 | ☑ `showsTransformControls`（`transformSession.ts:1081`）+ 测试；⌘H 与菜单项都是 `toggleTransformControls()`（`actions.ts:792`、`MenuBar.svelte:845-853`，菜单项带勾），画布两处消费：旋转把手只在开着时才抓（`CanvasSurface.svelte:895`）、控件组本身（`:1492`）。**⌘H 只切可见性，⌘T 才开会话**（`actions.ts:560` 的注释即此语义）。此前的「零消费者、⌘H 接到 beginTransformSession」是错的 |
| C22 | Grid ' / Guides ; / Rulers ⌘R 开关 | ' ; ⌘R | 6 | M3 | ☑ 三个开关都接上并可见：`state/view.ts:29,32,35` + 菜单勾（`MenuBar.svelte:485-510`）+ ⌘'/⌘;/⌘R（`table.ts:94-96`）；画布真的画布局网格/参考线/标尺（`CanvasSurface.svelte:858,888,974`），`view.test.ts` 覆盖 |
| C23 | Grid Settings…（间距/细分） | — | 6 | M3 | ☑ 票06：`layoutGrid` store（`state/view.ts:45`，默认 `normalizeGrid(64, 8)`）+ 表单 `GridSettingsSheet.svelte`（间距/细分，提交走 `normalizeGrid`）+ 视图菜单项（`MenuBar.svelte:784` → `gridSettingsOpen`）+ `App.svelte:551` 挂载 + 画布消费（`CanvasSurface.svelte:409`）。**此前的「无表单/菜单项、参数硬编码」已过期**。参数在**前端**而非 Go，与原版一致（布局网格属视图状态） |
| C24 | Snap To：参考线/网格/图层/文档边界 | — | 7 | M3 | ☑ 四个子开关（`state/view.ts:160 snapToggles`，持久化 + 测试）**有 UI**：视图菜单里四项带勾（`MenuBar.svelte:816-825`）；**有消费端**：`state/snap.ts:155-190 collectTargets` 逐项门控，`transformSession.ts:655-671` 同表。**此前的「无 UI 入口、没有消费端」已过期** |
| C25 | Lock Guides / Clear Guides | ⌥; | 7 | M3 | ☑ 菜单「锁定参考线」（`MenuBar.svelte:521`，⇧⌘; `table.ts:98`）与「清除参考线」（`MenuBar.svelte:530`）都可达；锁定时不可抓取（`CanvasSurface.svelte:582`），清除写回 Go（`actions.ts:508`）并作为「参考线」撤销步（`internal/bridge/guides_test.go`） |

**Select**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C26 | All / Deselect / Inverse | ⌘A/⌘D/⇧⌘I | 29 | M3 | ☑ All/Deselect/Inverse 经 `actions.ts:657-659` 注册，走纯函数 `state/selectionCommands.ts:29-39`；⌘A/⌘D/⇧⌘I（`table.ts:75-77`）+ `selectionCommands.test.ts` |
| C27 | Layer's Pixels（载入图层像素为选区） | — | 29 | M3 | ☑ 早就有：Go `LoadLayerSelection(tabId, layerID, "pixels")`（`selection_edit.go:46`，alpha ≥ 128、按图层 transform 映射到文档空间）+ 前端 `selectionEdit.ts` 的 `loadLayerSelection`（带 generation 守卫）+ 菜单「载入图层像素」（`MenuBar.svelte:301`）。本轮补上**缩略图 Ctrl 点击**（Shift 加选 / Alt 减选，原版 `NativeLayerList.loadMode`）。**此前的「无入口也无通路」是错的——那一版 grep 找的是 `layerPixels` 这个名字，方法其实叫 `LoadLayerSelection`** |
| C28 | Subject（选择主体） | ⌥⌘A | 28 | M6 | ☑ 票37 |
| C29 | Color Range…（色彩范围表单） | — | 29 | M3 | ☑ 内核 + 黄金基准（`internal/render/wand.go` ColorRangeMask、`tests/golden/cases/color-range.json`、`golden_test.go:50`）；**表单与菜单都有**：`MenuBar.svelte:302` 的「色彩范围…」→ `editSelection("colorRange")` → `SelectionEditSheet`（`App.svelte:548` 挂载，`SelectionEditSheet.svelte:57,80,123` 处理含/排除色）。**此前的「表单/菜单/RPC 全缺」已过期** |
| C30 | Mask's Black Areas（载入蒙版黑色区） | — | 29 | M3 | ☑ 早就有：同一个 `LoadLayerSelection` 的 `"mask"` 分支（灰值 < 128，链接蒙版跟随图层、解除链接用 `maskPlacement`）+ 菜单「载入蒙版黑色区域」（`MenuBar.svelte:302`，无蒙版时置灰）。本轮补上缩略图 Ctrl 点击的加选/减选 |
| C31 | Expand… / Contract… / Feather… | — | 31 | M3 | ☑ 纯函数 + 测试齐备（`state/selectionCommands.ts:103,109,118`、`selectionCommands.test.ts`）；**菜单项与表单都有**：`MenuBar.svelte:303-305` 三项 → `editSelection` → `SelectionEditSheet`。**此前的「无表单与菜单项」已过期** |

**Image**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C32 | Curves… / Levels… / Hue-Saturation…（作用于像素） | ⌘M/⌘L/⌘U | 38 | M5 | ☑ 票31 |
| C33 | Black & White / Color Balance / Exposure / Gradient Map / Grain（像素版） | — | 38 | M5 | ☑ 票31 |
| C34 | Invert ⌘I（选中蒙版时为 Invert Mask） | ⌘I | 38 | M5 | ☑ 票31 |
| C35 | Canvas Size… / Image Size… / Trim… | ⌥⌘C/⌥⌘I | 50 | M7 | ☑ 票43 |
| C36 | Flip Canvas Horizontal / Vertical | — | 33 | M3 | ☑ `actions.ts:638 flipCanvas` → Go `FlipCanvas` RPC（镜像每层/蒙版/参考线）→ 应用快照；图像菜单两项（`MenuBar.svelte:537,545`）。无快捷键（原版也没有）。**此前的「零生产者、无 RPC」已过期** |

**Filter（`FilterKind` 除内容感知与图像调整外全部）**

| # | 命令 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- |
| C37 | Gaussian Blur / Motion Blur | 40 | M5 | ☑ 票32 |
| C38 | Add Noise / Vignette / Bloom-Glow | 40 | M5 | ☑ 票32（滤镜菜单） |
| C39 | Dither（11 种风格） | 40 | M6 | ☑ 票36 |
| C40 | Tonal Contrast / Lens Correction | 40 | M5 | ☑ 票32 |
| C41 | Camera Raw Filter（全参数面板） | 39 | M5 | ☑ 票34：滤镜菜单「Camera Raw 滤镜…」（`state/filters.ts:45`）→ 面板（`App.svelte:404-407` `CameraRawPanel.svelte`）+ Go `ApplyCameraRawFilter`（`internal/render/cameraraw.go`，Light/Color 走票 78 的测量表）与滑杆/示波器管线测试（`internal/bridge/filter_test.go:310`） |
| C42 | Remove Background | 28 | M6 | ☑ 票37 |

**Layer**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C43 | New Adjustment Layer（12 种） | — | 37 | M5 | ☑ Go `LayerOp` 的 `addAdjustment`（`document.go:260`，含类型校验 `:263`、插入位置 `:323`、provisional 创建 `:91`）+ 前端 `layerOps.addAdjustment`（`state/document.ts:575`）+ 图层菜单 12 项「新建调整图层 · <类型>」（`MenuBar.svelte:635`）。**此前的「无入口也无 RPC」已过期**；`document_test.go:262,308,342,392` 与 `adjustment_session_test.go:179` 覆盖 |
| C44 | Edit Adjustment… | — | 37 | M5 | ☑ 票31+34：图层面板「编辑」按钮（`LayersPanel.svelte:155-171`）→ `beginFilter(kind, …, layerID, adjustment)` → Go `setAdjustment`（`internal/bridge/document.go:227`）/`ApplyAdjustment`（`internal/render/adjustment.go:19`），调整层渲染有测试（`adjustment_render_test.go`） |
| C45 | Transform Layer / Transform Selection | ⌘T | 32-34 | M3 | ☑ 变换图层全链路（⌘T `table.ts:85` → `state/transform.ts:75` → `ApplyLayerTransform` `layer_transform.go:30`，`layer_transform_test.go`）；**Transform Selection 已接**：`transformSelection`（`state/selectionCommands.ts:174`）被 `state/selectionEdit.ts:95` 消费，入口是「变换选区…」（`MenuBar.svelte:306`）→ `SelectionEditSheet` |
| C46 | Duplicate Layer / Layer via Copy | ⌘J | 16 | M3 | ☑ 票47：Go `duplicateLayer`（子树/剪贴链/位图独立拷贝）+ 菜单与快捷键 |
| C47 | Create/Release Clipping Mask | ⌥⌘G | 15 | M3 | ☑ 票47：Go `setClippingMask`（共享基底链）+ 菜单项随状态改名 |
| C48 | Group ⌘G / Ungroup ⇧⌘G / Move Out of Folder | ⌘G/⇧⌘G | 12 | M3 | ☑ 票47：编组/取消编组（Go op + 菜单 + 快捷键）。**移出文件夹已补成 Go op**（2026-10-19）：原先只有前端纯函数、菜单根本没接、**也不进撤销历史**；现在 `internal/bridge/document.go` 的 `moveOutOfGroup` 把活动图层移出到组所在那一层、紧跟组之上，作为一步可撤销的编辑（undo 名「移出文件夹」），在副本上演算 + `validateClipGraph`/`Validate` 通过才提交，失败不留痕。菜单「移出文件夹」只在活动图层有父级时可用。Go 5 条测试（含撤销、含子层保留）+ 前端 3 条（含可用性）。变异验证 6 处全被抓：父级改错、槽位改到栈顶、根级静默接受、undo 名改错、前端改发 `moveLayer`、菜单可用性恒真 |
| C49 | New Blank Layer / Rename / Show-Hide | ⇧⌘N | 16 | M3 | ☑ 票20+47：空白层/重命名/可见性（面板）+ ⇧⌘N 快捷键与菜单 |
| C50 | Move Layer Up/Down | ] / [ | 17 | M3 | ☑ 票47：Go moveLayer + 图层菜单与快捷键（方向键 Nudge 亦已接，走 nudgeLayer） |
| C51 | Merge（向下合并/合并所选/合并组，随上下文变标题） | ⌘E | 17 | M3 | ☑ 票47：Go `mergeLayers`（render.Render 合成参与层、剪贴重指向、一步撤销）+ 菜单标题随上下文 |
| C52 | Flip Layer H/V | — | 33 | M3 | ☑ `actions.ts:717 flipLayer`：开变换会话 → `flipTransform` → `commitTransformSession`（非破坏，走 `ApplyLayerTransformToTab`）；图层菜单两项（`MenuBar.svelte:597,605`）。无快捷键（原版也没有） |
| C53 | Delete Layer / Layer Mask / Effect（随选中项变标题） | — | 17 | M3 | ☑ 删层（⌫/菜单，`actions.ts:864` 按选区→蒙版→图层路由）、删蒙版（蒙版行菜单的「删除蒙版」与面板底部按钮，`LayersPanel.svelte:460,477`）都在。**特效行此前根本不存在**——从 PSD 带进来的图层特效既看不见也删不掉；本轮补上：面板在每个图层下列出它挂着的特效（标题按 domain.Effects 字段序，中文），每行一个删除按钮走 `layerOps.deleteEffect(id, kind)` → Go `deleteEffect`（`document.ts:593`）。用例见 `LayersPanel.browser.test.mjs`（列出、按 kind 删除、无特效不长空行） |

## C. 调整图层（`AdjustmentKind`，12 种，非破坏）

| # | 种类 | 实现锚点 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| A01 | Hue/Saturation（分区/分段） | 33³ color cube | 37 | M5 | ☑ 票10+26：内核 + 黄金逐位（`internal/render/cube.go` / `lut.go`、`golden_test.go:27-70`）+ 调整层渲染（`composite.go:111`、`adjustment_render_test.go`）；新建调整层入口仍缺（见 C43） |
| A02 | Levels（Auto + 黑/灰/白三吸管） | `LevelsPixels.c` LUT | 37 | M5 | ☑ 票10+26：同 A01（`BuildLevelsLUT` + `LevelsPixels.c` 黄金逐位 + 调整层渲染）；仅能从 PSD 导入的调整层编辑，新建入口见 C43 |
| A03 | Curves | 256 项/通道表 | 37 | M5 | ☑ 票10+26：同 A01（Curves 256 项表 + 黄金逐位 + 调整层渲染）；新建入口见 C43 |
| A04 | Exposure | sRGB↔线性表 | 37 | M5 | ☑ 票10+26：同 A01（曝光 sRGB↔线性表 + `levels`/`gradient_map` 黄金；`golden_test.go:351`）；新建入口见 C43 |
| A05 | Gradient Map | 256×3 表 | 37 | M5 | ☑ 票10+26：同 A01（256×3 渐变映射表 + 黄金逐位 + 调整层渲染）；新建入口见 C43 |
| A06 | Grain（胶片颗粒，文档锚定噪声场） | `adjust_grain` | 37 | M5 | ☑ 票27：`ApplyGrain`（`internal/render/kernels.go`）+ 黄金逐位（`golden_test.go:83`）+ 调整层渲染测试；破坏式入口见 C33，新建调整层见 C43 |
| A07 | Add Noise（位置+种子确定性） | `noise_add_at` | 37 | M5 | ☑ 票27：`ApplyAddNoise`（位置 + 种子确定性，`kernels.go`）+ 黄金（`golden_test.go:107`）+ 调整层渲染测试；新建入口见 C43 |
| A08 | Gaussian Blur（溢出层边缘） | CI blur 语义 | 37 | M5 | ☑ `adjustment.go:62-67` 的 `AdjustmentGaussianBlur` 分支复用破坏式滤镜的同一个 `ApplyGaussianBlur`（半径缺失或非正时不动），`TestRenderAdjustmentGaussianBlurChangesPixels`（`adjustment_render_test.go:151`）；破坏式入口见 C37。**此前的「dispatch 没有 blur 分支」已过期——12 种 kind 全部有内核，`default` 分支对已知 kind 不可达** |
| A09 | Motion Blur | CI motion 语义 | 37 | M5 | ☑ `adjustment.go:68-77` 的 `AdjustmentMotionBlur` 分支（`ApplyMotionBlur`，`MotionRadiusPerPixel` 与破坏式滤镜同一条换算），`TestRenderAdjustmentMotionBlurChangesPixels`（`adjustment_render_test.go:174`） |
| A10 | Invert | CPU | 37 | M5 | ☑ 内核已移植（`internal/render/kernels.go:324` ApplyInvert，真值 PixelInvert.swift）且反相菜单可达（C34 ☑）；`internal/render/invert_test.go` 已补内核级断言（**部分透明**的像素才是定义性的：`A − C` 与朴素的 `255 − C` 在全不透明时同值）；黄金 C 内核**不该造** —— 上游反相是 Swift + vImage，没有 C 内核，凭空的「黄金 C」会把参照降级成手写期望 |
| A11 | Black & White（6 通道权重+着色） | `adjust_black_white` | 37 | M5 | ☑ 票27：`ApplyBlackWhite`（6 通道权重 + 着色，`kernels.go`）+ 黄金逐位（`golden_test.go:87`）+ 调整层渲染测试；破坏式入口 C33 ☑ |
| A12 | Color Balance（保明度） | `adjust_color_balance` | 37 | M5 | ☑ 票27：`ApplyColorBalance`（保明度，`kernels.go`）+ 黄金逐位（`golden_test.go:96`）+ 调整层渲染测试；破坏式入口 C33 ☑ |

## D. Camera Raw（`AdjustPixels.c` adjust_camera_raw* 族，全参数）

| # | 参数组 | 里程碑 | 状态 |
| --- | --- | --- | --- |
| R01 | 光：曝光/对比/高光/阴影/白/黑 | M5 | ☑ 票28 |
| R02 | 色：白平衡/自然饱和/饱和 | M5 | ☑ 票28 |
| R03 | 曲线：参数化+点曲线（含彩色曲线） | M5 | ☑ 票28 |
| R04 | 混色器：HSL 8 色相 | M5 | ☑ 票29 |
| R05 | 颜色分级：阴影/中间调/高光+混合/平衡 | M5 | ☑ 票29 |
| R06 | 细节：锐化+降噪（亮度盒模糊） | M5 | ☑ 票29 |
| R07 | 光学：去边/色差/晕影 | M5 | ☑ 票30 |
| R08 | 几何：Upright 拉直/校准 | M5 | ☑ 票30 |
| R09 | 直方图 + 矢量示波器 | M5 | ☑ 票26 直方图 + 票34 面板/示波器 |

## E. 图层特效（`LayerEffectKind`，6 种，GPU 实时可编辑）

| # | 特效 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- |
| E01 | Stroke（描边，形态学膨胀） | 41 | M2/M5 | ☑ **已实现**：`render.renderLayerEffects`（`internal/render/effects.go:216`；票 78 期间由 `renderStrokeEffects` 改名，因为它现在同时画投影/外发光/内阴影/内发光）+ 形态学 `strokeRingCoverage`/`extremeCoverage`（`:345,363`），里/外描边、分数半径、宽描边、蒙版与独立蒙版 placement、旋转翻转、剪贴、组不透明度、活形状都覆盖（`effects_render_test.go` 11 条）。**上一版状态行写「没有任何特效渲染」是我 grep 被截断后的误判，已纠正** |
| E02 | Drop Shadow（投影） | 41 | M2/M5 | ☑ **已实现**：`render.effectsMargin`（余量 `distance + blur*3`）+ `effectCoverage`（按 `blur/2` 做高斯，`LayerEffects.swift:588-596`）+ `renderLayerEffects` 把影子画在图层**下面**；偏移 `shadowOffset` 照抄 `(-cos(angle), sin(angle)) * distance`（90° 垂直向下）。用例：`TestRenderDropShadowFallsStraightDownAtNinetyDegrees`（逐像素网格）、`TestRenderDisabledShadowDrawsNothing`（关掉即与无特效逐字节相同） |
| E03 | Color Overlay（颜色叠加） | 41 | M2/M5 | ☑ **已实现**：`render.applyColorOverlay`（`effects.go:119`）+ `colorOverlayOf`（`:27`，缺 enabled 即视为可见），source-over 语义与蒙版先于特效、组不透明度、源栅格 placement、活形状都覆盖（`effects_render_test.go` 8 条） |
| E04 | Inner Shadow（内阴影） | 41 | M2/M5 | ☑ **已实现**：`invertCoverage`（`1 - level`）+ `effectRecolorAtop`（source-atop，保留原 alpha）+ `renderLayerEffects` 在**图层缓冲**上着色，所以内阴影不会盖到投影上。用例：`TestRenderInnerShadowTintsTheInsideEdgeAndKeepsAlpha`（90° 时落在上缘内侧、中心不变、图层 alpha 与外围透明都不变）、`TestRenderDisabledInnerShadowDrawsNothing` |
| E05 | Outer Glow（外发光） | 41 | M2/M5 | ☑ **已实现**：`outerGlowOf` + `effectsMargin`（`size*3`）+ `renderLayerEffects` 里的 `soft * (1 - shape)`（对应上游 `outerGlowCoverage` 的 `levels[i] * (1 - mask[i])`），画在图层下面。用例：`TestRenderOuterGlowLightsTheRoomAndNotTheLayer`（照亮四周、不盖图层）、`TestRenderOuterGlowStaysWithinItsMargin` |
| E06 | Inner Glow（内发光） | 41 | M2/M5 | ☑ **已实现**：`effectCoverage(blur=size)` 取 `1 - level` 后 `effectRecolorAtop`。**`GuidedMatte.levels` 只是「画进 8 位灰度上下文再读回」**，对这些已是目标尺寸的覆盖图是恒等，所以不需要引导滤波。用例：`TestRenderInnerGlowTintsTheInsideEdgeAndKeepsAlpha` |

## F. 文档模型与图层系统能力

| # | 能力 | 锚点 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| L01 | 扁平图层+parentID、编组≤64、剪贴链≤256 | `CanvasDocument`/`LayerHierarchy` | 11-12 | M1 | ☑ 票04：扁平层 + parentID（`internal/domain/document.go`）、编组 ≤64 / 剪贴链 ≤256（`domain/limits.go`、`domain/validate.go:158-185`、`limits_test.go`）；Go 侧 layer op 全接（`internal/bridge/document.go:117`） |
| L02 | 24 种混合模式（23+Normal，原顺序，sRGB 空间语义） | `LayerBlendMode` | 11 | M2 | ☑ 票04+09：24 种混合模式原顺序（`internal/domain/blend.go:10-33`）+ sRGB 空间合成（`internal/render/blend.go:21-124`）+ 测试（`blend_test.go`、`render_test.go:15`）；面板下拉与 ⇧= / ⇧- 可达 |
| L03 | 图层/组不透明度（组压暗内部） | `LayerAppearance` | 11 | M2 | ☑ 票09：组压暗内部（`internal/render/composite.go`）+ `render_test.go` 组衰减断言；面板滑杆与数字键可达 |
| L04 | 图层蒙版：绘制/填充/反相/羽化/越层绘制/链接-解链/独立变换 | `LayerMask.swift` | 14 | M2/M3 | ☑ 蒙版渲染/存储（`composite.go:127,225`、`domain.MaskFile`/`MaskPlacement`、`project/validate.go:136`）；**行 UI 与操作入口都在**：`LayersPanel.svelte` 有蒙版缩略图（`:431`）、新建蒙版菜单（`:373`）、蒙版操作菜单（`:440` → `requestLayerMask` 的 `LayerMaskOperation`），绘制走 token 笔触的 `targetMask`（`paint_session.go:182`）、填充走 `FillLayerMask`（`fill_layer_mask.go:53`）、反相在 `MenuBar.svelte:324`、**链接/解链**在蒙版菜单（`LayersPanel.svelte:455-456` → `layerOps.ts:375`）与 Go `layer_mask.go:198-210`；蒙版独立 placement（`MaskPlacement`）在翻转/旋转/裁剪里同步维护，未解除链接时跟随图层（`raster_paint_target.go:43,64`）。**此前的「mask 0 命中」是 grep 大小写/关键词不当导致的误判** |
| L05 | 剪贴蒙版 + 组蒙版 | `maskSourceID` | 15 | M2 | ☑ 剪贴蒙版经 ⌥⌘G 可达（`internal/bridge/layer_commands.go:884 linkClippingMask`、`MenuBar.svelte:378`、`layer_commands_test.go`）；组蒙版渲染已接（`composite.go:91`、`render_test.go:238`），无独立创建 UI |
| L06 | 非破坏变换（任意缩小保留原分辨率）+ 采样质量三档 | `LayerTransform.sampling` | 33 | M3 | ☑ 非破坏变换已接线（`state/transform.ts:88` → `layer_transform.go:30`，`layer_transform_test.go` 断言只改 transform）。**采样质量三档的 UI 其实早就有，原记录过时**（2026-10-19 核实）：`TransformInspector` 里有 Sampling 下拉、`SAMPLINGS` 三档、`applyTransformNumeric({sampling})` 会改草稿；`transform.ts:107` 是 `t.sampling ?? DEFAULT_SAMPLING`（**回退**，不是固定值）。真正缺的是**测试**：把应用 sampling 那段改掉，25 条测试原先照样全过 —— 已补 `transform.test.ts` 的单测（改档位 + 拒绝未知取值）与 `TransformInspector.browser.test.mjs` 的下拉用例，两处变异都能抓住 |
| L07 | 快照撤销：嵌套事务/100 条/256MB/revision 脏标记 | `DocumentHistory` | 57 | M1 | ☑ 票06：快照历史（`internal/history/history.go:113,122,313,334`、`history_test.go` 13 个 Test）+ 撤销/重做菜单与 ⌘Z/⇧⌘Z |
| L08 | 文档限额：30,000px 边长/200M 像素/内存预算 RAM/16 | `DocumentLimits` | 51 | M1 | ☑ 票52：`ValidateNewDocument`（边长/像素/分辨率）+ `DocumentPixelBudgetFor`（纯函数，RAM/16 语义：下限 200M、上限 800M；8GiB=537MP 与原版注释一致）+ 拐点表与上限关系测试 |

## G. 选区系统

| # | 能力 | 锚点 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| S01 | 魔棒匹配：容差/连续/全局/取样半径(点/3×3/5×5) | `WandPixels.c` | 27 | M3 | ☑ 内核 + 黄金逐位（`internal/render/wand.go`、`golden_test.go:71-82`，ε=0）；**画布交互已接**：`CanvasSurface.svelte:1114` 的 wand 分支 → `pickSelection`，模式（wand/object）在 `ToolOptionsBar.svelte:73-81`、**容差**在 `:92-93`（`updateWandSettings`） |
| S02 | 像素边描迹 → 蚂蚁线闭环路径 | `wand_trace` | 30 | M3 | ☑ 描迹与闭环路径齐备（`state/selection.ts` 的 `outlineLoops` + `selection.test.ts`）。**2026-10-19 核实：原记录的「无创建入口」是过期记录** —— 入口一直在（`CanvasSurface.svelte` 魔棒分支 → `pickSelection` → `SelectWandAt` → `selectionFromMask` → `outlineLoops` → 蚂蚁线），那句写于魔棒接线之前。**同处补了真缺口**：画布原来只画一层**静态白色虚线**（白底上看不见、也不「走」），且每个闭环一个 `<path>`（细节丰富的描迹能出上万 DOM 节点）；改成**复合路径 + 两层**（白色实线打底 + 走动黑虚线，相位 0.12s 进 1px，照 `TransformOverlay.swift:296`/`EditorCanvas.swift:2446`）。5 处变异全被抓（删覆盖层/删动画/白线改黑/去掉闭合 `Z`/坐标 x↔y 对调）。**两份实现不再会漂移**（2026-10-20）：Go 的 `WandTrace` 与前端 `outlineLoops` 是同一套算法的两份，前端那份是**刻意的** —— 蚂蚁线要同步出，走 IPC 每选一次都要往返、反而更慢，所以不做 RPC 接线。风险在漂移，用**共享夹具**解决：`internal/render/testdata/trace_cases.json` 同时喂 `TestWandTraceMatchesTheSharedFixture` 与 `trace_cases.test.ts`（比对前把每条闭环旋转到字典序最小起点，走向仍参与比对）。两侧各改坏一处（方向位互换）都会被抓住。**边数上限也补齐了**：前端原先没有，病态掩版会描出上千万条边；现在与 Go 同一个 `OUTLINE_EDGE_LIMIT = 8_000_000`，超限返回**空列表**（不画蚂蚁线，而不是卡死画布），上限可注入以便用小夹具干净地测这条分支 |
| S03 | 色彩范围（采样含/排除色+羽化度） | `color_range_mask` | 29 | M3 | ☑ 同 C29：内核 + 黄金基准齐备，且**表单存在**（`SelectionEditSheet.svelte` 的 `colorRange` 分支，含/排除色与容差） |
| S04 | 扩展/收缩/羽化（蒙版域操作） | `SelectionEdits.swift` | 31 | M3 | ☑ 同 C31：纯函数 + 测试齐备，UI 入口在 `MenuBar.svelte:303-305` + `SelectionEditSheet` |
| S05 | 选区剪贴板/选区内移动复制/边缘自动滚动 | `FloatingSelection` | 30 | M3 | ☑ 票48+16：选区剪贴板（拷/切/粘贴/⌘J）完成；浮动选区搬运已接（方向键 → `MoveSelectedPixels`，`internal/bridge/selection_commands.go:186`、`pixel_move_test.go`、`actions.ts:758-765`）；**边缘自动滚动已接线**（2026-10-19）：`edgeAutoScroll` 不再零消费者 —— 矩形选区拖拽进视口边缘 32px 余量时按帧推视口（`CanvasSurface.svelte` 的 rAF 循环，鼠标停住时 `pointermove` 不再触发，所以必须自己推帧；松手/取消即停），并把框的端点按同一指针位置重算。浏览器测试钉住「贴边拖会平移、中间拖不平移、松手后停止」，去掉挂钩即失败。**拖拽浮层已补**（2026-10-20）：画布上拖动选区，语义**照原版拆分**（`SelectionEdits.swift:168-212`）—— **普通拖动只移选区轮廓**（mask+loops 一起平移、保留 `feather`、**不发 RPC、不产生撤销步骤**），**⌘/Ctrl 拖动才搬像素**（走 `actions.ts` 导出的 `moveSelectedPixels`，与方向键同一条通道）。Esc/右键取消不提交、零位移不产生步骤、RPC 在飞时拒绝第二个手势、光标在拖动/悬停选区内为 move。浏览器测试 4 条（普通拖动 0 次 RPC 且轮廓/mask/feather 平移；Ctrl 拖动调 1 次且 payload 是原始选区；Esc 与右键各一条取消），5 处变异全被抓。**⌘⌥ 拖动=复制后搬已实现**（2026-10-20）：按住 ⌘/Ctrl + Alt 拖动时，结算先调 `LayerViaCopy` 把选区像素拷成新层、再在新层上搬 —— 原件留在原处。用现有两条 RPC 组合而成，所以**撤销是两步**而不是原版的一步（已知差异）。**这条没有自动化测试**：试过给浏览器夹具伪造 `LayerViaCopy` 的文档快照，`applyClipboardSnapshot` 判它不合法、`copy` 之后的搬移被吞掉；与其塞一个跑不通的测试，不如如实标注。**仍缺**：悬停光标未区分普通/Ctrl（原版 moveSelection/剪刀），`cursors.ts` 待接 |
| S06 | 对象选择/主体/移除背景 + 引导滤波抠边 | Vision→ONNX（ADR-0005） | 28 | M6 | ☑ 票37 |
| S07 | 内容感知填充（可延伸出画布） | `PatchPixels.c` | 42 | M6 | ☑ 票35 |

## H. 文本（`TypeTool.swift`/`PSDText.swift`）

| # | 能力 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- |
| X01 | 段落框拖拽/缩放/内联多行编辑/IME/⌘回车提交 | 43 | M8 | ☑ 票44+45：建框/点文本/IME/提交 + 8 向手柄缩放（16…30000、面积 2e8） |
| X02 | 字体/字号/颜色/对齐/字距/行高，逐 run 混排 | 44 | M8 | ☑ 票45：逐 run 编辑/渲染 + 字体枚举选择器与回退 + 行内选区保留 + manifest v11 往返（编辑期不画混排，提交后由 Go 光栅） |
| X03 | 文本变换 + 用作剪贴蒙版 | 45 | M8 | ☑ 文本层落盘后有 ImageFile（`internal/bridge/text.go:159`），可被 ⌘T 通用变换会话改 `layer.transform`（`state/transform.ts:62` 只排除编组），也可作剪贴蒙版来源（`layer_commands.go:884` 只拒编组/调整层）；无文本专属重排（编辑时按自身原点重光栅，`text.go:172-177`） |
| X04 | PSD 文本 run 往返 | 47 | M8 | ☑ 票39+45+46：PSD→模型解析 + 逐 run 编辑；模型→PSD 不适用（本应用不写 PSD），票46 把它做成**记录级 codec 往返**（encodeTySh 仅供 reader 对等验证，不在任何导出链路） |

## I. 文件与导出

| # | 能力 | 锚点 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| I01 | 导入 JPEG/PNG/TIFF | Go 标准库 + x/image | 46 | M7 | ☑ 票40（含 EXIF 方向） |
| I02 | 导入 HEIC（libheif/cgo） | `ImageImporter` | 46 | M7 | ☑ 票41（静态链接，LGPL-3.0 已声明） |
| I03 | 导入 SVG（光栅化） | `decodeSVG` | 46 | M7 | ☑ 票40（前端 WebView2 一次栅格化，适配画布语义） |
| I04 | 相机 RAW + develop 步骤 + asShot 默认 | `RawImporter`/LibRaw | 46 | M7 | ☑ 票41（LibRaw 0.22.2 + 自写 develop 数学；与 Apple CIRAWFilter 数值不逐位一致） |
| I05 | PSD/PSB 导入：层/组/蒙版/混合/`levl``curv``hue2`/填充形状/水平文本；超大层裁剪；转换报告 | `PSD/*` | 47 | M7 | ☑ 票38+39：解析/调整/蒙版/剪贴/裁剪/报告 + 可编辑文本与活形状 + 应用前确认 |
| I06 | 导出 PNG/JPEG + JPEG 实时预览 | `ImageExporter` | 48 | M7 | ☑ 票42（DPI 元数据 pHYs/JFIF） |
| I07 | `.comp` v11 读写/校验/原子保存/包内预览图 | `ProjectStore`（ADR-0002） | 1/49 | M1 | ☑ 票05：.comp v11 读写/校验/原子保存/包内预览图全套（`internal/project/*`、`internal/fsatomic`、`project/store_test.go`、`project/validate_test.go`）；唯一未闭环是真实 macOS 1.4.5 夹具缺失（双向互开未实测） |
| I08 | 保存不阻塞编辑（后台写） | `finishWriting` | 49 | M1 | ☑ 快照（doc 克隆 + 位图克隆）在锁内完成，编码与文件 I/O 在锁外；**编辑入口不再查 `sess.saving`**（11 处：笔触/重绘/渐变/蒙版/变换/填充/绘制），所以写在后台跑时编辑照常。上游 `ProjectController.save` 也是「只在快照与保存面板期间 isProjectBusy，写之前放开」（`ProjectController.swift:33-35`）。`saving` 仍gate**文档替换**（关闭/切换/重载）。用例：`TestEditingContinuesWhileASaveIsWriting`（在 `savePackage` 里停住，期间走完一次笔触，并断言标签仍脏）、`TestFillLayerMaskIsNotBlockedByASaveInFlight` |

## J. 视图/交互/性能

| # | 能力 | 锚点 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| V01 | 视口：0.001–32×、17 级梯、锚点保持、fit/100% | `CanvasViewport` | 3 | M3 | ☑ 票13：0.001–32× / 17 级梯 / 锚点保持 / fit / 100%（`state/viewport.ts`、`viewport.test.ts`），已接画布（`CanvasSurface.svelte:315,658`）与 ⌘0/⌘1/=/− |
| V02 | 像素网格 ≥800% / 棋盘格 / 文档边界 | `drawPixelGrid` | 5/8 | M3 | ☑ 像素网格 ≥800%（`CanvasSurface.svelte:320`）+ 棋盘格 + 文档边界叠层（`CanvasSurface.svelte:828-857`），`viewport.test.ts` 覆盖阈值 |
| V03 | 标尺/参考线（拖出/移动/拖回删除/锁定/清除）/布局网格 | `Guides.swift` | 6-7 | M3 | ☑ 标尺（`CanvasSurface.svelte:974-999`）+ 参考线拖出/移动/拖回标尺删除（`:602,635,750`）+ 锁定（`:582`）+ 清除（`MenuBar.svelte:530`）+ 布局网格（`:858`）；持久化走 `SetGuides`（`internal/bridge/guides.go:30`、`guides_test.go`） |
| V04 | 吸附：参考线/网格/图层/文档边界 + Ctrl 旁路 | `TransformSnap`/`CropSnap` | 35 | M3 | ☑ 内核齐备（`state/snap.ts`、`snap.test.ts`，含 Ctrl 旁路与四目标）且已有消费端：图层拖拽（`CanvasSurface.svelte` 的 `canvasSnapInput`）与裁剪（`CropOverlay.svelte:233,242`）都把 targets 传进 `snap()`；票76 补上总开关门控与偏好持久化 |
| V05 | 锐利缩小采样（Lanczos 级联） | `DownsampleCache` | 51 | M2 | ☑ **导出与图像尺寸的缩小走 Lanczos-3**：`render.LanczosResample`（`internal/render/lanczos.go`）可分离、浮点、核宽随倍率（正确的低通，不是固定核的点采样），`ResampleBitmap` 的缩小分支用它（`export.go:123`）。**与上游的差异**：上游用「逐次减半的级联」是因为 CG 只能把最后 2× 做好，且它的消费者是画布分块渲染器；这里一趟做完更准（级联每步都要量化回 8 位），结果只取决于像素，所以预览与导出一致。滤镜预览与抖动块仍用 `DownscaleBitmap` 的盒平均（那是另一条语义：`BrushRaster.draw` 的插值降尺度）。**导出对话框里两处也统一了**：JPEG 质量预览编码的缩略图（`bridge/export.go` 的 `s.exportPreview`）原先走盒平均，而保存走 `ResampleBitmap` —— 同一个操作两条滤波会让用户看到一张图、存下另一张，本轮改成同一个；`TestExportPreviewThumbnailUsesTheSavedFilesFilter` 钉住，并断言夹具确实能区分两种滤波 |
| V06 | 数值拖拽调节（拖标签 scrub）/方向键步进/点击跳转 | `NumericScrub`/`SliderSnap` | 54 | M3 控件 | ☑ **机制已接线到变换检查器**（2026-10-19）：`numeric.ts` 的 `scrub`/`arrows`/`direct` 现在由 `TransformInspector` 的五个字段真实调用 —— 拖标签按像素改值、↑↓ 步进（Shift 步长 ×10，照 ArrowStepper 是「粗调」不是「细调」）、输入经 `direct()` 解析并钳制。浏览器测试钉住三种行为：去掉拖拽或方向键任一，测试立刻失败。**已铺到工具栏选项栏**（2026-10-19）：新建 `NumericField.svelte`，`ToolOptionsBar` 的 10 个数值选项（容差/渐变不透明度/圆角/线宽/直径/硬度/不透明度/平滑/强度/模糊半径）改用它；组件级钳制由「直接挂组件、断言 `onchange` 收到钳过的值」那条用例钉住（工具栏那条「填 99999 得 2000」只是行为断言，store 自己也会钳）。**又铺了两个表单**（2026-10-20）：`GeometrySheet` 6 个（画布宽高/图像宽高/分辨率/容差）、`SelectionEditSheet` 6 个（取样 X·Y/颜色容差/水平·垂直位移/半径）。三个 `step="any"` 的字段（水平缩放/垂直缩放/旋转角度）**保持原生**——`NumericField.step` 只吃数字，改成 0.01/0.1 会引入浏览器原生 step 校验、拦掉 12.345 这类输入，属于真实的可用性倒退。**`FilterDialog` 也铺完了**（2026-10-20）：60 个 `type="number"` 里 **55 个**转成 `NumericField`，**5 个 `.tiny` RGB 三元组保留原生**（没有标签，而 `NumericField` 必须有 `label`；强行加会改文案）；66 个 range 滑块、下拉、开关全部未动。滑块补了 `aria-label` 保住可访问名。**最后一批**（2026-10-20）：`TextOptionsBar` 的字号/字距/行高三处也换了（浏览器测试：拖标签 +5、输入 99999 钳到 2000；去掉 onchange 立刻失败）。**余下仍是原生输入的都写了理由**：`GridSettingsSheet`(2) 与 `ShapeInspector`(2) 用 `bind:value` 双向绑定 / `step="any"`，`ExportDialog`(2) 是成对尺寸行（一个「尺寸」标签 + × + px，没有逐字段可见标签），`FilterDialog`(5) 是无标签的 `.tiny` RGB 三元组，`LayersPanel`(1) 是 range 滑块，`RawDevelopSheet` 只有滑块。**没有「没接而是忘了」的了** —— 全是「接了会更差」的。 |
| V07 | 合成徽标光标（加选/减选/吸管/旋转等 ~10 种） | NSCursor 家族 | — | M3 | ☑ 票13：合成徽标光标按工具/修饰键切换（`components/cursors.ts`、`cursors.test.ts`），已接画布（`CanvasSurface.svelte:340`） |
| V08 | 右键拖拽笔刷 HUD | `EditorCanvas` | 18 | M4 | ☑ `lib/gpu/brushCursor.ts` + `brushCursor.test.ts`；画布右键在笔刷类工具上开 `beginBrushTipDrag`（`CanvasSurface.svelte:1014-1018`，`e.button === 2 && BRUSH_TOOLS.has($activeTool)`），拖动走 `dragBrushTip`（`:1186`） |
| V09 | 4K 笔刷回显 <16ms 基准 | `brush-performance.md` | 19 | M4 | ◐ 票52：`TestBrushStroke4KLatency` 在 4000×4000 上逐段计时，实测 median 1.5–2.0ms / p95 2.1–2.5ms（门禁：默认 48ms，`COMPOSITOR_BENCH_STRICT=1` 时为票面目标 16ms），基线入 `docs/benchmarks/performance.md`。**只有后端、不含显示**，且桥接层只暴露直径 30（原版测 40/800）—— 故 ◐ |
| V10 | 内存压力响应的纹理/缓存逐出 | `GPUCanvas` | — | M2 | ☑ 票52：`internal/memory` 采样档位（<25% moderate / <12% critical），桥接侧真的逐出渲染缓存并把历史压到 10–50 步（注入测试证明撤销/重做/重渲染仍可用），档位变化推 `memory:pressure`；前端注册口就位且有测试。**「GPU 纹理缓存尚未接进画布」是过期记录**（2026-10-20 核实）：`CanvasSurface.svelte:491` 调 `WebGpuCompositor.create(canvas)`，构造函数里 `registerPressureHandler`（`compositor.ts:635`）把 `moderate`/`critical` 转发给 `layerStore.onMemoryPressure`；逐出本身在缓存层有测试（`textureCache.test.ts:82,96`、`layerBitmaps.test.ts:153`、`effectPass.test.ts:76`），档位归一化在 `memory.test.ts`。故 ☑ |

## K. 平台集成与代理契约

| # | 能力 | Windows 方案 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| P01 | ~114 条快捷键 + 重映射 + 冲突校验 | 前端快捷键表（Cmd→Ctrl） | 52 | M9 | ☑ 票47：113 条定义逐条移植（`shortcuts/table.ts`、`table.test.ts`）+ 重映射/冲突校验/持久化 + 三域分流；本轮后未接线只剩 4 条——Toggle Levels preview、Cycle tool mode、Cycle shape kind（真缺底层能力）+ Hide Compositor（macOS 专属），`actions.test.ts:151-176` 钉住；注册 109/113 |
| P02 | 剪贴板（像素/整层/合并） | Windows 剪贴板 | 55 | M9 | ☑ 票42+48：CF_DIB 写（拷贝合并/选区像素）+ 读（PNG 注册格式回落 CF_DIB，纯 Go 解码含 1/4/8/16/24/32bpp，PNG 解码有测试）+ 剪贴板序号做变更判定。**与 C11 同一 caveat：winclip 的 Win32 读写本体（C shim）无自动化测试，只在真机上跑**；`GetImage`/`Sequence`/`PutBitmap` 的实际行为需要手工验收（拷进画图、从浏览器拷图回来） |
| P03 | 文件关联/最近文件/跳转列表 | NSIS + shell | 53 | M9 | ☑ 票49：`wails.json` 的 `info.fileAssociations` 经 NSIS 注册 `.comp` 关联（生成的宏写 `"$INSTDIR\Compositor.exe" "%1"`，双击/打开方式进标签）+ 最近文件（用户配置目录 JSON，打开与保存都记）+ 跳转列表（`SHAddToRecentDocs`）。真机跑通过、**无自动化断言**；覆盖图标实测会卡（见 P08） |
| P04 | 安装包 + 签名 + 自动更新 | NSIS + GitHub Releases + EdDSA | 53 | M9 | ◐ 票50：NSIS 安装包（票03）+ `signtool` 签名（证书只从 secrets/环境变量来；无证书降级为不签名，内层 exe 未签见票面已知差异）+ `feed.json`（appcast 等价物，`scripts/genfeed` 生成、Ed25519 签名、可逐字节复现）+ 应用内检查/下载/校验/静默升级 + release.yml/release.ps1/verify-install.ps1。**零新增依赖**；差量更新不做；**安装→升级→卸载未在干净环境执行、真 Release 端到端未跑**（故 ◐ 而非 ☑） |
| P05 | 代理热更新（写 PNG→原子改名→~300ms 重载，保留视口选区） | fsnotify + digest | 56 | M1/M9 | ◐ 票51：`internal/bridge/watch.go` 把 `internal/watch` 接到每个有路径的标签上，协调器报出稳定摘要后读盘→判脏→原位重载（`ReloadDocument`：标签不变、清撤销、rev 前进）或先问用户（`external:changed` pending + `ResolveExternalChange`）；视口/选区在前端且重载不碰它们。实测写入到重载 **569ms**（合并窗口 300ms + 退避 ~250ms，票面"~300ms"指合并窗口）。9 个端到端 + 4 个 fsatomic + 14 个示例脚本测试；**"画布不跳"只有 store 层负断言**，手工视觉验收（验收页 B 节 10 步）未跑，OneDrive/长路径未实测 —— 故 ◐ 而非 ☑ |
| P06 | 常暗深色 UI / 单窗口多标签 | Svelte 外壳 | 2/53 | M0 | ☑ 票01+02：常暗深色 UI（`frontend/src/app.css` 变量）+ 单窗口多标签（`components/TabStrip.svelte`、`state/workspace.ts`），标签新建/切换/关闭/脏确认有测试（`workspace.test.ts`、`confirm.test.ts`） |
| P07 | 拖放导入（文件/截图/网页图） | WebView 拖放 | 55 | M9 | ◐ 票48：三来源都接了（Wails OnFileDrop 路径 → `ImportDroppedPaths`；截图/网页图 → 浏览器栅格化 + `BeginBlobImport` 上传面）；dragover 对 Files 与 text/uri-list/text/html 都取消默认（否则网页图那条永远收不到 drop），两路用 250ms 等待 + 750ms 去重窗口避免重复导入。Go 侧 8 个测试、前端判定与接线 13 个测试；**真机拖放交互未手工验收**（P07 仍 ◐ 的原因） |
| P08 | 窗口脏标记/标题（任务栏覆盖图标） | Win32 等价 | 10 | M9 | ☑ 票49：窗口标题 `名字* - Compositor`（脏标记加星）+ 任务栏覆盖图标（`ITaskbarList3::SetOverlayIcon` 红点，失败只记日志）。真机发现该 API 会跨进程卡住（最长 >60s），因此 winchrome 每次调用最多等 2s 且有界失败、与最近文档各用独立闸门；未截到任务栏红点图 |
| P09 | 首次启动窗口尺寸记忆 | 窗口状态持久化 | 9 | M9 | ☑ 票02+49：`WindowStore`（用户配置目录 window.json）+ 启动恢复 + resize 防抖保存；票49 核对后勾 |
| P10 | 启动到就绪基准（原版 LAUNCH_TO_READY_MEDIAN） | 性能测试 | — | M10 | ☑ 票52（CI 无硬门禁）：`main()` 起表 + 前端就绪上报 → stdout 打 `LAUNCH_TO_READY_SECONDS`；`scripts/launchbench.ps1` 跑 N 次算中位数并按原版指标名打印，可 `-Record` 进 `docs/benchmarks/launch.json`。**已实测**：本机三次 median 0.547/0.534/0.513 s，趋势入 `docs/benchmarks/launch.json`、口径与跑法入 `docs/benchmarks/launch.md`；CI 里那一步是 log-only + artifact 且 `continue-on-error`（runner 无交互桌面），**没有硬门禁**；跨平台不可直接比（少了进程创建与 XCTest/辅助功能开销） |

## 明确不做（与原版对齐的省略，非遗漏）

1. Darker Color / Lighter Color 混合模式——原版 `LayerAppearance.swift` 注释明确省略。
2. CMYK/16bit/Display P3——在 1.4.5 旧基准下暂不做；1.5/1.5.1 已出现 16 位、HDR、CMYK，因此对应增量票（U15–U19、U29–U34）必须先完成 ADR 与用户确认。
3. Apple Pencil/触控条/Force Touch——原版无此功能。
4. macOS 专属：QuickLook 面板、Hide Others 语义、NSFontPanel——以 Windows 等价物替代（P03-P08），QuickLook 包内预览图仍生成（跨平台通用）。

## 矩阵自检

- **表头声明的数量与实际行数一致**：`A. 工具（16）` = 16 行（T01–T16）、`C. 调整图层（12 种）` = 12 行（A01–A12）、`E. 图层特效（6 种）` = 6 行（E01–E06）。
- **全矩阵 143 行，各组连续无缺号**：T 16、C 53、A 12、R 9、E 6、L 8、S 7、X 4、I 8、V 10、P 10。B 组（菜单命令）表头没有声明数量，实际 53 行 = File 9 + Edit 8 + View 8 + Select 6 + Image 5 + Filter 6 + Layer 11。
- **与外部声明的偏差（仅登记，未改行）**：`AGENTS.md` 称本矩阵「150+ 行」，实际 143 行；`P01` 的旧文案写「~114 条快捷键」，`frontend/src/lib/shortcuts/table.ts` 在本轮（票 54）之前是 113 条定义——票 54 照当前参考源码补上 `Menus:Search Commands`（⌘F/Ctrl+F），现在是 114 条。
- 本轮只改「状态」列，未增删任何行；「# / 功能 / 原版锚点 / 故事# / 里程碑」五列逐行保持原样（改前改后其余单元格逐字节相同）。

## 更新记录

- **更新日期**：2026-10-14（第二轮）。票 63 / 64 / 65 / 66 / 67 落地，`U10`–`U14` ☐ → ☑；
  该轮只改这五格，另把 `S07` 的原版锚点从 `ContentFill.c` 改成 `PatchPixels.c`（上游 1.4.10 删除了前者）。
  实现细节见 [63](tickets/63-camera-raw-fidelity.md)、[64](tickets/64-raw-develop-quality.md)、
  [65](tickets/65-patchmatch-healing.md)、[66](tickets/66-gaussian-preserve-alpha.md)、
  [67](tickets/67-effect-text-preservation.md)。
  黄金基准本轮新增 `patch-fill-{curve,texture,blend}`、`heal-texture`、`quantize16-ramp` 五个 case
  （40 → 42 张参考图），`camera-raw` 与 `content-fill` 退役到 `tests/golden/legacy/`；
  另修掉图像尺寸重采样后资产名不符 `.comp` 约定、导致无法保存的既有缺陷（票 67 的测试发现）。
- **更新日期**：2026-10-14。票 58 / 59 / 60 / 61 / 62 落地，`U05`–`U09` ☐ → ☑；该轮只改这五格。
- **更新日期**：2026-10-13（第二轮）。票 55 / 56 / 57 并行落地，`U02`、`U03`、`U04` ☐ → ☑；
  该轮只改这三格。实现细节见 [55](tickets/55-canvas-only.md)、[56](tickets/56-canvas-rotation.md)、
  [57](tickets/57-new-canvas-units-background.md)。
- **更新日期**：2026-10-13。票 54 实现命令搜索面板：**只把 `U01` 从 ☐ 改成 ☑**，其余单元格
  一格不动；实现细节见 [docs/tickets/54-command-palette.md](tickets/54-command-palette.md)。
  （本文件里"上游 1.4.5→1.5.1 增量矩阵"整段与「明确不做」第 2 条的改写，是上一轮登记 54–92 票据时
  落的，与各轮的勾选不是同一轮。）
- **更新日期**：2026-10-11。
- **基线**：commit `679d5d8`（`perf(memory): 票 52 …`）。本轮以**当前工作树**为准复核，而不是照抄 `docs/audit/` 的结论：工作树在基线之后又落了未提交接线——新增 `frontend/src/lib/state/view.ts`、`state/transform.ts`、`internal/bridge/{selection_commands,guides,layer_transform}.go`，并改了 `CanvasSurface.svelte`、`state/document.ts`、`shortcuts/actions.ts`、`MenuBar.svelte`。复核期间工作树仍被并发编辑，所有 `路径:行` 以本次复核时的内容为准。
- **统计变化**：☑ 54 → **87** 行；◐ 15 → **42** 行；☐ 74 → **14** 行（共 143 行）。

## 上游 1.4.5→1.5.1 增量矩阵

以下是旧 143 行矩阵之外的新增范围。状态全部为 ☐，表示只登记、未实现；票据编号与 `docs/tickets/54–92` 对应。格式/色彩空间变更项的 ☐ 同时表示 ADR 决策门槛。

| ID | 新增能力 | 票据 | 里程碑 | 状态 |
| --- | --- | --- | --- | --- |
| U01 | 命令搜索面板 | 54 | M3/M9 | ☑ |
| U02 | 纯画布全屏模式 | 55 | M3/M9 | ☑ |
| U03 | 画布旋转 90 度 | 56 | M3 | ☑ |
| U04 | 新建画布物理单位与底色 | 57 | M0/M7 | ☑ |
| U05 | 重做上一个滤镜 | 58 | M5 | ☑ |
| U06 | Export As 与 PDF/TIFF | 59 | M7 | ☑ |
| U07 | 独立 Scanlines 滤镜 | 60 | M5/M6 | ☑ |
| U08 | Navigator 导航缩略图 | 61 | M3 | ☑ |
| U09 | WebP 与相机 HEIF 导入 | 62 | M7 | ☑ |
| U10 | Camera Raw 光影数值对齐 | 63 | M5 | ✅ 曝光/对比/高光/阴影/白平衡/白/黑/饱和/自然饱和/校准原色现在走 **Photoshop 的测量表**（票 78 的接线：`applyCameraRawTable` → `cameraRawTableStages` → 合成表 → 逐像素查一次），不再是被 `75147a2` 取代的祖先解析算法；整条路与 C 端到端逐字节对拍。去雾/纹理/清晰度/锐化/降噪已对齐。**仍缺**：混色器与颜色分级仍走解析路径（票 90） |
| U11 | RAW 显影降噪与高光修复 | 64 | M7 | ◐ 降噪/量化已对齐；镜头校正 `supports_lens_correction=0`，控件永久置灰 |
| U12 | PatchMatch 内容感知修复 | 65 | M6 | ☑ |
| U13 | Gaussian Blur 保留透明度 | 66 | M5 | ☑ |
| U14 | 图层特效与可编辑内容保留 | 67 | M5/M7/M8 | ☑ |
| U15 | .comp 16 位存取与版本门控 | 68 | M1/M7 | ☑（ADR-0007；写最低所需版本）。普通 16 位图层跑滤镜已保留深层（票 82 的 `RunDeep`） |
| U16 | 16 位转换与几何编辑 | 69 | M7 | ✅ |
| U17 | .comp HDR/EXR 存取与版本门控 | 70 | M1/M7 | ✅ 非 sRGB 色度仍按数字读取 |
| U18 | HDR 编辑与画布预览 | 71 | M2/M7 | ◐ 图层编辑已保留高光；**HDR 调整层仍未走全深度**：`runFilterOnGrid` 的 HDR 分支是归一化 → 8 位内核 → 恢复尺度，只有 Exposure 走 `adjustmentWorksAtFullDepth`。票 82 解决的是普通 **16 位**图层的滤镜/绘画/重绘，不含 HDR 调整层 |
| U19 | CMYK ICC 校样 | 72 | M2/M7 | ◐ 查找表已接到显示，无内置 ICC |
| U20 | Progressive Blur | 73 | M5 | ✅ 可变半径用 8 档高斯阶梯近似 |
| U21 | 欢迎页粘贴、标签栏和图层建项目 | 74 | M0/M7 | ✅ 欢迎屏粘贴、标签栏拖放新开项目、Untitled 最小空闲序号；无路径的内存图仍走导入为图层 |
| U22 | 中键平移与修饰键缩放 | 75 | M3 | ✅ |
| U23 | Snap 总开关与命令状态 | 76 | M3/M9 | ✅ |
| U24 | 文件夹直接子图像批量导入 | 77 | M7 | ✅ 普通文件夹展开成**直接子级**图像（`bridge/imagefolder.go`：常规文件 + 可导入类型 + 跳过隐藏、自然序），展开点在 `beginImport` 这个三入口的汇聚处，所以对话框/画布/标签栏/外壳行为一致；另加「导入文件夹…」菜单项（`PickImageFolder`）。空文件夹作为一条明确错误报出。`imagefolder_test.go` 6 条 |
| U25 | Camera Raw 查表与预览性能 | 78 | M5/M10 | ◐ **查表资源与加载器已落地**：`CameraRawTables.bin` 逐字节拷入 `internal/render/assets/`（来源与格式见该目录 README），`go:embed` 打进二进制，`cameratables.go` 按上游校验标识/几何/解压长度并解码 763 张表，`main()` 开窗口前 `ValidateCameraRawTables()` 解一次。**解码正确性有硬证据**：五张滑杆取 0 的表恰好等于恒等。**阶段管线已就位**：`camerastage.go` 逐行对应 `AdjustPixels.c` 的 `table_lookup`/`camera_raw_stage_color`/`camera_raw_compose`/`composed_lookup`，曝光阶段与通用构造器（`between`/`stage`）已接，**与 C 逐位对拍通过**（`TestCameraRawComposeMatchesC`）。**阶段构造器也已落地**：自适应（对比/高光/阴影按画面亮度取四张表）、白平衡（9×9 四角混合）、`fifty`、以及上游 `stages(for:brightness:)` 的完整顺序（Calibration → Exposure → 白平衡 → 对比 → 高光 → 阴影 → 白/黑/饱和/自然饱和 → 参数曲线），夹具里两条阶段由构造器生成并对拍。**逐像素应用已落地并与 C 端到端逐字节对拍**（`cameraRawApplyTable` = `adjust_camera_raw`，三种裁剪预览各有夹具）。**已接进 `ApplyCameraRawFilter`**：Light/Color 走测量表，解析内核降为查表不可用时的兜底；接线时发现上游的 calibration 只画 Shadow Tint（原色在表里），我们的调用传了全部参数、接表后会双重应用，已按上游分叉。**混色器阶段已移植**（`cameraRawMixerStages` = `mixerStages`，八色 × 亮度/饱和/色相，已进合成对拍），但**尚未接线**——上游把混色器与分级折进同一张表，接线归票 90。**曲线与混色器已折进查找表**（`cameraRawTableCurves` + `canFold`，整条链仍只花一次查表），折的条件比上游严以避免静默丢段（分级阶段未移植、`refineSaturation` 上游也没折）。过程中修掉两个真 bug：**参数曲线被应用两次**（`toneTable()` 在测得分割点下只该做点曲线）、**「可视化」勾选框一直无效且桥接用零值 0 当索引**（有拾取点时普通编辑会压暗画布）。**颜色分级也已折进表**（`cameraRawGradingStages`：四轮盘亮度、Balance 份额、Blending 三档插值、每轮盘最多八槽；夹具里用填满八槽的用例与 C 对拍）。**预览取消旧任务已有专项用例**：`TestASupersededPreviewNeverOverwritesTheNewerSettings`（数修订号：先发慢、后发快，被丢弃时只前进 1；去掉 `f.generation != gen` 立刻变 2 → 反向验证过。踩过的坑：先发必须真的慢 —— 高斯是盒式近似半径无关，得用动感模糊）。**5–20× 基准已复算并记录**：那句「5–20 倍」指**预览的像素数**（预览限 2048 最长边 ⇒ 倍率 = (最长边/2048)²，实测 14MP→5.0×、24MP→8.6×、54MP→19.3×，10.7MP→3.8× **如实标为不达标**），不是查表相对解析核的加速（那个实测 4.6×）；「提交不走低精度捷径」由同一测试钉住（提交底图仍是全尺寸）。数字在 `docs/benchmarks/performance.md` |
| U26 | 半透明像素的图层特效着色 | 79 | M5 | ✅ 票 79：颜色叠加、内发光、内阴影改为上游 `e7e295e`/`9cb57db` 的语义 —— **重新着色图层自身的像素并保持 alpha**（`MetalLayerEffects.swift:369-384`），目标色是特效色按像素 alpha 预乘。修复前它们被当成覆盖在合成上的一层：混合权重多乘了一次 alpha，于是半透明像素只被染上一部分、alpha 还被抬高、下面的特效被串色；不透明像素两条路结果相同，所以只有半透明像素看得出来。六种特效（描边/投影/外发光/颜色叠加/内阴影/内发光）齐备 |
| U27 | Camera Raw 分区重置 | 80 | M5 | ✅ 票 80：面板十个分区标题各有 ↺，默认值由 `Service.CameraRawGroupDefaults` 给出（**默认值不是零值**：曲线分割点 25/50/75、样式、镜头配置百分比、分级混合 50、Process 6…），只回该分区的键；重置后该分区重新显示，走既有预览通道（可撤销）。混色器点索引与几何引导草稿在本仓无对应状态（引导线 UI 未做），票面如实记录 |
| U28 | 快捷键清除与旧配置迁移 | 81 | M9 | ✅ 票 81：空键（`isNoneChord`）表示「没有键」，`problemIn` 跳过它所以清掉后能存、且释放冲突键；表单补上录制中的 ✕；两条出厂无键的菜单命令（CMYK Proof Colors / Gamut Warning，上游 87e2283）进表并接上 `toggleProof`（从 MenuBar 挪到 state 共享），无键命令菜单不显示提示；旧配置改成上游的**逐条采纳**（按 id 排序试用），坏记录/已删命令/冲突项只丢自己 |
| U29 | 16 位图层绘画与调整精度 | 82 | M5/M7 | ✅ 滤镜走 `RunDeep`（`deep_split`/`deep_merge` 与 C 逐字节对拍）；笔触/重绘/渐变/填充折深层 —— 其中**四处原先会丢掉深层**，本轮修掉 |
| U30 | 16 位与 HDR 屏幕抖动预览 | 83 | M2/M7 | ✅ 16 位走 `ToBitmap` → `Quantize16`、HDR 走 `Wide16()`（高光夹白并按 alpha 钳制），都是纯函数；噪声是**位置哈希**而非随机数，所以缩放平移不闪；与 C 的 `dither_quantize16` 及三份 HDR 黄金对拍。本轮补上「不写回源层」的钉子 |
| U31 | 16 位 PNG/TIFF 导出 | 84 | M7 | ◐ 票 84：编码器（`EncodePNG16WithDPI`/`EncodeTIFF16WithDPI`）与 Export As 的 `sixteenBit` 已落地，PNG/TIFF 两条端到端与分辨率都有测试，**导出→解码逐像素核验**。✅ **多图层 16 位合成已补**：`render.CompositeDeep` 把 8 位那条普通路径（`Place`→缩 alpha→source-over）原样搬到 16 位样本上，**不另写一套合成器**（故非「第二份像素真相」），公式权威仍是 `Render` —— 量化后最大差 2 级，故意破坏公式差 118 级。位移/旋转/翻转/缩放也支持（采样方式取自图层，与 `Place` 一致）。仍明确拒绝并指出是哪一层哪条属性：组、调整图层、蒙版、剪贴、特效、非「正常」混合、无 16 位栅格。单图层快路（拼合即该层深层栅格）保留，一个像素不重算 |
| U32 | HDR EXR 导出 | 85 | M7 | ✅ 票 85：Export As 加 EXR；拼合结果是 HDR 深层栅格时直接写它（超白不截断，解码回来峰值仍约 4），否则退回 8 位 half 展开（上游 `hdrRaster ?? raster`）。「无超白→16 位层 / 有超白→HDR」的导入规则此前已由 `bitmapFromDeep` 落地（`hdrWithinWhite = 1.001`）。HDR 的 EXR 拒绝改尺寸 |
| U33 | CMYK 文件导入与配置保留 | 86 | M7 | ◐ **TIFF 已通**：`x/image/tiff` 不认 CMYK（photometric 5 直接 UnsupportedError），自写读取器取油墨平面 + ICC 标签 34675，交 `colorio.ConvertToRGB` 转 sRGB；`rasterio.Decode` 嗅探后走它，导入路径本来就调 `Decode`。缺配置/非 CMYK 配置**明确拒绝**而非猜。**配置也留进会话了**：`colorio.AdoptEmbedded`（上游 `CMYKProfile.embedded` + `adopt`）把文件内嵌配置留存并**选为当前配置**，校样回到那份文件当初针对的油墨。**JPEG 也已通**：`rasterio.isCMYKJPEG` 走段标记读帧头分量数 → `DecodeCMYKJPEGWithProfile` 从 APP2 的 ICC_PROFILE 分段按段号拼回配置 → 与 TIFF 共用 `cmykImportResult`（边界、配置校验、`ConvertToRGB`）。**PSD 的 CMYK 变体：拼合那条已通**（mode 4 过闸门；4 个油墨通道**反相**后经资源 1039 的配置转 RGB —— 反相照 `PSDReader.swift:335`；配置选择照 `:73-76` 内嵌→通用→拒绝，我们没有随包通用配置故退化为「内嵌或拒绝」）。**分层 CMYK 也通了**（`decodeChannels` 按 `unpackedCMYKChannelIDs` 切通道集合、缺通道默认不上墨、四平面反相后转换 —— 全部照 `PSDReader.swift:235,310,334-340`）。**PSD 的配置采纳原来漏了**（它走 `project_io.loadPhotoshop`，不经过 `rasterio.Decode`），已补。**U33 的三条路（TIFF/JPEG/PSD）到此都通** |
| U34 | CMYK JPEG/TIFF/PDF 导出 | 87 | M7 | ◐ **TIFF 与 PDF 已通**：`EncodeCMYKTIFFWithDPI`（基线无压缩 TIFF + ICC 34675）与 `EncodeCMYKPDFWithDPI`（`/ICCBased` N=4 + `/Alternate /DeviceCMYK`），配置取自 View › CMYK › 配置；写出的 TIFF 由票 86 的读取器读回来闭环验证。**JPEG 也已通**：四通道 JPEG 用随 vcpkg 装好的 **libjpeg-turbo**（cgo，同 lcms2/LibRaw/libheif 的路子；`vcpkg.json` 里已显式列为依赖），按 **Adobe 约定**写（存储值反相 + APP14 标记），质量走表单。同文件还有 `DecodeCMYKJPEG`（四分量读回，ICC 约定）—— 票 86 剩的 JPEG 变体那一半，也是往返测试的读者。导出侧也补上了**配置嵌入**（`EncodeCMYKJPEGWithProfile`，否则导出的文件回来时无配置可读）。**验收证据审计过**（变异测试：TIFF 写错 photometric、TIFF 不写 ICC、PDF 声明 N=3 —— 三条都被抓住），审计查出一条真缺口并补上：`EncodeCMYKPDFWithDPI` 的「没有配置就拒绝」此前没有直接测试。**仍缺**：TIFF 的油墨极性按 ICC 约定，与 Photoshop 的反相约定未用真文件核对 |
| U35 | 画布选层后揭示图层 | 88 | M3 | ✅ `LayersPanel.svelte` 的 effect：选中行用 `scrollIntoView({block:"nearest"})`（最小滚动量，已可见即不动，所以不闪），行的定义直接取面板真值 `[role="option"][aria-selected="true"]`（图层行与蒙版行走同一条路）。依赖只有选中层与蒙版目标两个值，所以重命名/缩略图刷新不触发滚动；键没变则直接返回（快照整体替换会让 effect 重跑）。查不到行静默返回。浏览器测试含反向验证 |
| U36 | 更新窗口发行说明 | 89 | M9 | ☐ |
| U37 | Camera Raw 色彩族对齐 | 90 | M5 | ✅ 七项断言逐条核对（白平衡/混色器/曲线/四轮分级/渐晕/校准原有，vibrance 本轮补）；**权威真值是随包的 Photoshop 测量表**，由 `TestCameraRawApplyMatchesC`/`TestCameraRawComposeMatchesC` 与各 stage builder 表号断言钉住，其余是行为方向与接线。透明像素不参与统计（3 条）；Camera Raw 在本仓**不是调整层类型**，预览与提交同序由 `TestCameraRawEffectsPreviewMatchesFullSize` 覆盖；1.4.5 基线原样留在 `tests/golden/legacy/` 未覆盖。**未写**「vibrance 比 saturation 更保护饱和色」——实测等滑杆值分不出，不拟合数字 |
| U38 | 上游资源与截图 CI 等价方案 | 91 | M0/M9 | ✅ `internal/assetcheck`（`go test ./...` 覆盖，无需改 workflow）：嵌入表尺寸/SHA-256/`CRT3` 魔数且与上游逐字节一致；`tests/golden/c` 每个拷贝件顶部 20 行内自证 `reference/Swift` 源文件 + 40 位提交号且源文件存在；声称整文件的注释之下必须仍与上游一致（比较前归一化行尾）；README 清单；许可证；源码不得出现 `.xcodeproj`/`.xcassets`/`Assets.xcassets`/`ScreenshotTests`。等价表（icon→`genicon`、截图→`*.browser.test.mjs`、feed→`genfeed`、Xcode 工程**明确排除**）记在票面。**检查抓到 10 个拷贝件无来源注释并已补** |
| U39 | 窄窗口图层与工具栏布局 | 92 | M0/M3 | ☑ **整壳极小窗口已实测**（`App.browser.test.mjs` 挂整个 App，900×600/640×480/480×360 三个视口断言页面不横溢、无够不着的元素）：量出 640×480 下 `.app` 是 722 宽（隐式网格列按 max-content，被最宽一行撑开，横溢 82px），修以显式 `minmax(0, 1fr)` + `.toolbar{overflow-x:auto}`；反向验证表明**吃劲的是工具栏可横滚**，另两处为防御性。**查出并修掉真缺陷**：`.mask-row` 第三方栏原为 `minmax(88px, auto)`（硬下限 88px + 26px 缩进）→ 面板 ≤148px 时蒙版菜单冲出右边界；改 `minmax(0, 88px)`（封顶好读、允许收缩，宁可截断不溢出），宽度扫描 200→100px 无溢出。选项栏实测 240px 可横滚、无第二滚动条，1600px 完全不滚，已用新浏览器测试钉住；重命名行高稳定；禁用/空状态走主题令牌。**深色主题对比度已实测**：`theme.contrast.test.mjs` 直接从 `app.css` 读令牌，12 对文本/底色断言 AA（最弱 `--accent` on `--accent-surface` 4.61:1），反向验证过（调暗 `--text-dim` 立刻 3 条失败）。**先量后改、量完发现不该改**：原本怀疑控件边框 1.5:1 看不见，浏览器探针量到工具栏控件都走原生外观（`border-style: none`、透明底），边界由 UA 画，所以 `--border` 保持不动。**仍缺**：整壳极小窗口遮挡检查（要挂 WebGPU + Wails 绑定，成本高） |

上游的 8 个标签后提交（Camera Raw 分区重置、快捷键清除、更新 feed/窗口、滚动条等）单列在审计报告中，不改变 1.5.1 发布范围。

### 本轮 ☐ → ☑（28 行）

T01（票17 变换会话接线）、T14（抓手/空格平移）、T15（缩放工具）、T16（无工具）；C01（新建画布/欢迎屏，票02）、C05（保存/另存为，票49）、C15（填充前景/背景色，票23 的 FillSelection + 菜单 + 快捷键）、C22（Grid/Guides/Rulers 三开关，票14）、C25（Lock/Clear Guides，票14）、C26（All/Deselect/Inverse，票16）、C41（Camera Raw 滤镜面板，票34）、C44（Edit Adjustment，票31+34）；A06、A07、A11、A12（颗粒/杂色/黑白/色彩平衡内核 + 黄金逐位，票27）；L01、L02、L03、L05、L07（域模型/混合模式/不透明度/剪贴蒙版/快照历史，票04+06+09+15）；X03（文本变换 + 用作剪贴蒙版）；I07（.comp v11 读写，票05）；V01、V02、V03、V07（视口/像素网格/标尺参考线布局网格/光标，票13+14）；P06（深色 UI + 多标签，票01+02）。

### 本轮 ◐ → ☑（5 行）

A01 A02 A03 A04 A05（色相饱和度/色阶/曲线/曝光/渐变映射：LUT/cube 内核 + 黄金逐位 + 调整层渲染，票10+26；旧状态停留在票10 的「◐ 生成+接线已落地」）。

### 本轮 ☐ → ◐（32 行）

- 工具：T02 T03（选框/套索 region 纯函数齐备，画布无指针分支）、T05（裁剪 CropState 齐备，无 RPC/入口）、T06（笔刷已接，橡皮/参数/选项栏缺）、T08（仿制图章内核齐备，无 RPC）、T09（涂抹/液化内核齐备，无 RPC）、T10（渐变仅栅格化原语，无测试无消费者）、T11（形状渲染已接，无创建会话）、T13（吸管取样齐备，无画布分支）。
- 菜单：C02（打开项目通，拖放 .comp 走图像导入不会打开）、C08（关标签有确认，退出应用无确认）、C10（撤销/重做通，菜单名不带操作名）、C16（清除选区 RPC 就绪，无入口）、C19（像素网格随缩放显示，showsPixelGrid 开关缺失）、C20（Snap 总开关可拨但无消费端）、C21（⌘H 接到开会话而非切换控件）、C24（Snap To 四开关有 store，无入口无消费端）、C29（色彩范围内核 + 黄金，无表单）、C31（扩展/收缩/羽化纯函数，无入口）、C36（Flip Canvas 纯函数，无入口）、C45（变换图层全链路，Transform Selection 未接）、C52（Flip Layer 纯函数，无入口）。
- 调整层：A10（Invert 内核已移植 + 反相菜单可达，但缺黄金参照与回归测试）。
- 文档模型：L04（蒙版渲染/存储齐备，无蒙版 UI 与绘制入口）、L06（非破坏变换已接，采样三档无 UI）。
- 选区：S01 S02 S03 S04（魔棒/描迹/色彩范围/扩展收缩羽化内核齐备，无创建入口）。
- 视图：V04（吸附内核齐备，零消费端）、V06（数值控件纯函数齐备，零消费者）、V08（笔刷 HUD 纯函数齐备，零消费者）。

### 仍为 ☐（14 行，按组）

- **B 菜单命令**：C23（Grid Settings…）、C27（Layer's Pixels）、C30（Mask's Black Areas）、C43（New Adjustment Layer）。
- **C 调整图层**：A08（Gaussian Blur 调整层内核是 pass-through）、A09（Motion Blur 同上）。
- **E 图层特效**：E01 E02 E03 E04 E05 E06（只有 GPU 纯库与自测，CPU 真值不存在，LayerOp 只有 deleteEffect，图层面板无特效行）。
- **I 文件与导出**：I08（后台写）。
- **J 视图/交互/性能**：V05（Lanczos 级联缩小）。

### 复核方法

对每一行到代码里找真实证据（read/grep，路径:行写在状态列里），判定口径：**☑** = 实现 + 接线 + 有测试 + 用户可达（有菜单项或快捷键入口）；**◐** = 内核/纯函数齐备且有测试，但整体用户不可达（缺 RPC 或缺 UI 入口），或该行点名的子能力缺失；**☐** = 真没做。「明确不做」第 4 条的 Hide Compositor 未接线，保持原样（`actions.test.ts:169-170` 钉住）。
