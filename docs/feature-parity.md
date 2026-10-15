# 功能对等矩阵（Feature Parity Matrix）

> **本文件是"一个功能都不能落下"的唯一权威清单**。以原版源码为锚点逐项核对：菜单命令面（`reference/Swift/Compositor/CompositorApp.swift`）、工具枚举（`EditorSession.NavigationTool`）、调整种类（`LayerAdjustment.AdjustmentKind`）、滤镜种类（`Filters.FilterKind`）、图层特效（`LayerEffects.LayerEffectKind`）、形状（`ShapeTool.ShapeKind`）、混合模式（`LayerAppearance.LayerBlendMode`）。
> 状态：☐ 计划中 → ◐ 实现中 → ☑ 已验收（对照 testing.md 缝合点）。拆票（`/to-tickets`）时逐行核对本矩阵；新增功能必须先在此登记。

## A. 工具（16，`NavigationTool`）

| # | 功能 | 原版锚点 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| T01 | 移动/变换 (V) | `.move` | 33-35 | M3 | ☑ 票17：会话接线在 `state/transform.ts:75`（⌘T 开）/`:88`（⏎ 提交，走 `ApplyLayerTransform`）/`:112`（Esc），画布 V 工具拖图层与 8 把手（`CanvasSurface.svelte:522,867`），Go 非破坏写 `layer.transform`（`internal/bridge/layer_transform.go:30`，`layer_transform_test.go`）。自由扭曲（⌘ 拖角）不支持 |
| T02 | 选框 (M)：矩形/椭圆双模式 | `.marquee` | 26 | M3 | ◐ 内核齐备：矩形/椭圆 region 纯函数 + 测试（`state/selection.ts:210,229`、`selection.test.ts`）；画布指针分派里没有 marquee 分支（`CanvasSurface.svelte:612-698`），矩形/椭圆双模式切换未接 |
| T03 | 套索 (L)：自由手/多边形双模式 | `.lasso` | 26 | M3 | ◐ 内核齐备：自由手 region 纯函数 + 测试（`state/selection.ts:252`）；画布无 lasso 分支，多边形模式未接 |
| T04 | 魔棒 (W)：颜色魔棒/对象选择，Tab 切换 | `.wand` | 27 | M3/M6 | ◐ 票15 颜色魔棒 + 票37 对象选择（内核 + 黄金基准 `golden_test.go:71`）；画布无魔棒分支，Tab 切换未注册（`actions.test.ts:167`） |
| T05 | 裁剪 (C)：比例/对称/选区起点 | `.crop` | 36 | M3 | ◐ 内核齐备：CropState/比例/对称/选区起点 + 43 测试（`state/crop.ts:488`、`crop.test.ts`）；零生产消费者，Go 无 Crop RPC（Service 73 个方法里没有） |
| T06 | 笔刷 (B) / 橡皮 (E)：大小/硬度/不透明度/平滑、Shift 直线 | `.brush` + `BrushStroke.swift` | 18-19 | M4 | ◐ 笔刷本体已接（`CanvasSurface.svelte:429` PAINT_TOOLS + `internal/render/brush.go` 14 测试）；橡皮不存在（E 键只 `selectTool("brush")`），参数硬编码在 `internal/bridge/brush.go:11-22`，[ / ] 对画笔无效，无笔刷选项栏 |
| T07 | 污点修复 (J)：内容感知/创建纹理/临近匹配 | `.spotHealing` + `HealPixels.c` | 21 | M6 | ☑ 票35 |
| T08 | 仿制图章 (S)：对齐开关、单层/全层取样、⌥点设源 | `.cloneStamp` | 20 | M4 | ◐ 内核齐备：`CloneStamp` + 13 测试（`internal/render/stampfill.go:60`、`stampfill_test.go`）；无 RPC，画布无分支（S 键只选中工具） |
| T09 | 涂抹 (R)：模糊/涂抹/液化三模式 | `.blur` + `MetalWarp` | 22 | M4 | ◐ 内核齐备：WarpStroke 三模式 + 测试（`internal/render/smudge.go:40,111`、`smudge_test.go`）；无 RPC，模式 store 未接（`actions.test.ts:167`） |
| T10 | 渐变 (G)：可再编辑渐变层 | `.gradient` | 23 | M4 | ◐ 只有未接线的栅格化原语 `GradientFill`（`internal/render/gradient.go:76`，无测试、无调用者）；无渐变会话/RPC/UI |
| T11 | 形状 (U)：矩形(圆角)/椭圆/线，Shift-U 切换，保持可编辑 | `.shape` + `ShapeKind` | 24 | M4 | ◐ 形状渲染已接合成（`internal/layerrender/shape.go`、`internal/render/composite.go:203`）；无创建会话/RPC，Shift-U 未注册（`table.ts:149`） |
| T12 | 文字 (T)：段落框/内联多行/IME | `.type` + `TypeTool.swift` | 43-45 | M8 | ☑ 票44+45：创建/编辑/提交/取消/IME/合成/往返 + 逐 run 样式与段落框缩放 |
| T13 | 吸管 (I)：取样环（可开关） | `.eyedropper` + `showsSampleRing` | 25 | M3 | ◐ 内核齐备（`state/color.ts:180` samplePixel + `color.test.ts`）；零生产消费者，画布无吸管分支，取样环 showsSampleRing 全仓 0 命中 |
| T14 | 抓手 (H) / 空格平移 | `.hand` | 4 | M3 | ☑ 空格/中键/抓手三条平移路径（`CanvasSurface.svelte:651`）+ 视口 `state/viewport.ts` 与 `viewport.test.ts`；H 键已接 |
| T15 | 缩放 (Z)：点击/拖拽 | `.zoom` | 3 | M3 | ☑ 缩放工具点击/Alt 反向（`CanvasSurface.svelte:658`）+ 滚轮锚点缩放 + ⌘0/⌘1/=/−（`table.ts:62-65`、`viewport.test.ts`） |
| T16 | 无工具 (A)：画布点击无操作 | `.idle` | — | M3 | ☑ A 键选 idle（`actions.ts` 工具字母），画布对 idle 不落任何分支（`CanvasSurface.svelte:612-698`）＝点击无操作 |

## B. 菜单命令（`CompositorApp.swift` 全量）

**File**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C01 | New Canvas…（欢迎屏） | ⌘N→Ctrl+N | 9 | M0 | ☑ 票02：欢迎屏 + 新建画布表单（`App.svelte:174`、`components/NewCanvasSheet.svelte`），限额来自 Go（`internal/domain/limits.go:40`、`limits_test.go`）；文件菜单与 ⌘N 可达 |
| C02 | Open Project… / 拖放打开 | ⌘O | 1 | M1/M9 | ◐ 打开对话框/最近/双击关联/命令行参数都通（`state/shell.ts:50 openPath`、`internal/bridge/project_io.go:94`）；但把 .comp 拖到窗口走图像导入（`App.svelte:112 importDroppedPaths` → `internal/bridge/import_images.go:283 rasterio.Decode`），拖放不会打开项目 |
| C03 | Open Recent（含清除） | — | 53 | M9 | ☑ 票49：文件菜单逐条列出最近项目（完整路径在 tooltip）+ 清除最近记录；列表上限 10、读取时过滤已移动/删除的文件（RecentProjects.refresh 语义），与 Windows 跳转列表共用一份 |
| C04 | Import Images… | — | 46 | M7 | ☑ 票40（多选批次，RAW/HEIC 随票41） |
| C05 | Save / Save As | ⌘S / ⇧⌘S | 49 | M1 | ☑ 票49：保存/另存为共用 `saveProject()`（`state/workspace.ts:128`）→ Go `SaveProjectDialog`（`project_io.go`），原子保存与往返测试（`project/store_test.go`、`document_test.go:671`） |
| C06 | Export PNG… | ⇧⌘E | 48 | M7 | ☑ 票42 |
| C07 | Export JPEG…（实时预览内可缩放） | ⇧⌥⌘S | 48 | M7 | ☑ 票42 |
| C08 | Close Project / 退出逐标签确认 | ⌘W | 2 | M1 | ◐ 关闭标签有脏确认（`actions.ts` closeActiveProject + `state/confirm.ts`、`workspace.test.ts`）；退出应用无确认——全仓无 `OnBeforeClose`（grep = 0），关窗即退出 |
| C09 | Check for Updates… | — | 53 | M9 | ◐ 票50：`帮助 ▸ 检查更新…` → `CheckForUpdates`（feed 比版本）→ `UpdateSheet`（版本/发行说明/进度）→ 下载并校验（sha256 + Ed25519）→ 启动静默安装器；启动时静默检查一次。链路每环有测试，但**没有在真实 Release 上跑过**（首版发布才能确认），故记 ◐ |

**Edit（剪贴板/填充组）**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C10 | Undo/Redo（菜单名带操作名） | ⌘Z / ⇧⌘Z | 57 | M1 | ◐ 撤销/重做已接线且有操作名数据（Go `internal/history/history.go:113,122`，菜单 `MenuBar.svelte:252-253`，`actions.ts:637-638`）；但菜单名不带操作名——前端没有 UndoName RPC（frontend grep = 0） |
| C11 | Cut / Copy / Paste（像素与文字域分流） | ⌘X/C/V | 55 | M3/M9 | ☑ 票48：选区像素拷/切（剪切留洞、可撤销）、整层复制（含编组/调整层）、外部图像粘贴、⌘V 落层丢选区；文字域由原生 textarea 自己处理（输入框内快捷键让位，菜单项同时禁用）。对照 SelectionClipboardTests 的 9 个用例：原位粘贴/剪切回填/形状选区（套索软边有测试）/选区包围盒裁剪已覆盖，变换会话那 4 个用例属票 16。**系统剪贴板的 Win32 手递手（winclip 的 C 侧）没有自动化测试**——接缝在测试里是内存假实现，纯 Go 的 CF_DIB/PNG 解码有测试 |
| C12 | Copy Merged | ⇧⌘C | 48 | M7 | ☑ 票42（CF_DIB；剪贴板像素/整层 UI 通道随票48） |
| C13 | 整层复制/粘贴/跨标签拖拽 | — | 16 | M3 | ☑ 票48：`copyLayersInto` 跨会话复制子树（新 UUID/资产、位图深拷贝、引用重映射、目标画布居中、一步撤销）+ 图层面板拖到标签栏（`TransferLayer`）+ ⌘V 的跨标签分支（`copiedLayerFor` 命中别的会话 → `copyLayersInto`，有测试）。面板内部的拖拽重排仍属票 20 |
| C14 | Keyboard Shortcuts…（重映射表单） | — | 52 | M9 | ☑ 票47：表单（录制/冲突提示/恢复默认/持久化）+ 113 条定义表 + 三域分流 |
| C15 | Fill with Foreground/Background Color | ⌥⌫ / ⌘⌫ | 42 | M4 | ☑ 票23：`FillSelection`（`internal/bridge/selection_commands.go:59`，选区裁剪/软边/一步撤销，`fill_test.go` 7 例）+ 编辑菜单两项（`MenuBar.svelte:280-291`）+ ⌥⌫/⌘⌫（`table.ts:72-73`） |
| C16 | Clear Selection Pixels | — | 42 | M4 | ◐ 内核/RPC 就绪：`FillSelection(mode="clear")`（`selection_commands.go:59,73`，`fill_test.go:85` 断言选区变透明）；前端 `fill("clear")` 只有定义、无注册/菜单/快捷键（`actions.ts:564`） |
| C17 | Content-Aware Fill… | ⇧⌫ | 42 | M6 | ☑ 票35 |

**View（画布组）**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C18 | Fit Canvas / Actual Pixels / Zoom In/Out | ⌘0/⌘1/=/- | 3 | M3 | ☑ 票13+47：视口命令 + 快捷键/视图菜单 |
| C19 | Pixel Grid 开关（≥800%） | — | 5 | M3 | ◐ 像素网格 ≥800% 自动显示（`CanvasSurface.svelte:320,852`，`PIXEL_GRID_ZOOM` ← `state/viewport.ts:32`）；原版的 showsPixelGrid 开关没有 store/入口，`Menus:Show Grid`（`actions.ts:683`）绑的是布局网格（原版 Show ▸ Grid，属 C22） |
| C20 | Snap 总开关 | — | 7 | M3 | ◐ 开关已接（`state/view.ts:38` + 菜单「对齐」`MenuBar.svelte:512` + ⇧⌘; `table.ts:97`），但零消费端：`snapOptions()`/`guideTargets()`（`view.ts:162,171`）只被测试引用，画布拖拽（`CanvasSurface.svelte:540`）与裁剪都没把目标传进 `snap()` |
| C21 | Show Transform Controls | ⌘H | 34 | M3 | ◐ store 与测试齐备（`transformSession.ts:1063` showsTransformControls）但零消费者；⌘H 与菜单项接到 `beginTransformSession`（`actions.ts:690`、`MenuBar.svelte:538`），画布只在会话存在时画控件（`CanvasSurface.svelte:867`）——按 ⌘H 得到的是开会话，不是切换控件 |
| C22 | Grid ' / Guides ; / Rulers ⌘R 开关 | ' ; ⌘R | 6 | M3 | ☑ 三个开关都接上并可见：`state/view.ts:29,32,35` + 菜单勾（`MenuBar.svelte:485-510`）+ ⌘'/⌘;/⌘R（`table.ts:94-96`）；画布真的画布局网格/参考线/标尺（`CanvasSurface.svelte:858,888,974`），`view.test.ts` 覆盖 |
| C23 | Grid Settings…（间距/细分） | — | 6 | M3 | ☐ 无 Grid Settings 表单/菜单项，Go 侧也没有 gridSettings（internal grep = 0）；布局网格参数硬编码 normalizeGrid(64, 8)（`state/view.ts:47`） |
| C24 | Snap To：参考线/网格/图层/文档边界 | — | 7 | M3 | ◐ 四个子开关的 store 与默认值齐备且有测试（`state/view.ts:44`、`view.test.ts`）；无 UI 入口（View 菜单没有 Snap To 子菜单），且与 C20 一样没有消费端 |
| C25 | Lock Guides / Clear Guides | ⌥; | 7 | M3 | ☑ 菜单「锁定参考线」（`MenuBar.svelte:521`，⇧⌘; `table.ts:98`）与「清除参考线」（`MenuBar.svelte:530`）都可达；锁定时不可抓取（`CanvasSurface.svelte:582`），清除写回 Go（`actions.ts:508`）并作为「参考线」撤销步（`internal/bridge/guides_test.go`） |

**Select**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C26 | All / Deselect / Inverse | ⌘A/⌘D/⇧⌘I | 29 | M3 | ☑ All/Deselect/Inverse 经 `actions.ts:657-659` 注册，走纯函数 `state/selectionCommands.ts:29-39`；⌘A/⌘D/⇧⌘I（`table.ts:75-77`）+ `selectionCommands.test.ts` |
| C27 | Layer's Pixels（载入图层像素为选区） | — | 29 | M3 | ☐ 无入口也无通路：Go 无 layerPixels/SelectLayerPixels（grep = 0），前端无动作、无 RPC |
| C28 | Subject（选择主体） | ⌥⌘A | 28 | M6 | ☑ 票37 |
| C29 | Color Range…（色彩范围表单） | — | 29 | M3 | ◐ 内核 + 黄金基准齐备（`internal/render/wand.go` ColorRangeMask、`tests/golden/cases/color-range.json`、`golden_test.go:50`）；色彩范围表单/菜单/RPC 全缺（frontend grep = 0） |
| C30 | Mask's Black Areas（载入蒙版黑色区） | — | 29 | M3 | ☐ 无入口也无通路（Go grep MaskBlackAreas = 0，前端无动作） |
| C31 | Expand… / Contract… / Feather… | — | 31 | M3 | ◐ 纯函数 + 测试齐备（`state/selectionCommands.ts:103,109,118`、`selectionCommands.test.ts`）；无扩展/收缩/羽化表单与菜单项 |

**Image**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C32 | Curves… / Levels… / Hue-Saturation…（作用于像素） | ⌘M/⌘L/⌘U | 38 | M5 | ☑ 票31 |
| C33 | Black & White / Color Balance / Exposure / Gradient Map / Grain（像素版） | — | 38 | M5 | ☑ 票31 |
| C34 | Invert ⌘I（选中蒙版时为 Invert Mask） | ⌘I | 38 | M5 | ☑ 票31 |
| C35 | Canvas Size… / Image Size… / Trim… | ⌥⌘C/⌥⌘I | 50 | M7 | ☑ 票43 |
| C36 | Flip Canvas Horizontal / Vertical | — | 33 | M3 | ◐ 纯函数 + 测试（`transformSession.ts:369 flipCanvasTransforms`）；零生产者、无 RPC、无菜单/快捷键（actions.ts 里 Flip 0 命中） |

**Filter（`FilterKind` 除内容感知与图像调整外全部）**

| # | 命令 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- |
| C37 | Gaussian Blur / Motion Blur | 40 | M5 | ☑ 票32 |
| C38 | Add Noise / Vignette / Bloom-Glow | 40 | M5 | ☑ 票32（滤镜菜单） |
| C39 | Dither（11 种风格） | 40 | M6 | ☑ 票36 |
| C40 | Tonal Contrast / Lens Correction | 40 | M5 | ☑ 票32 |
| C41 | Camera Raw Filter（全参数面板） | 39 | M5 | ☑ 票34：滤镜菜单「Camera Raw 滤镜…」（`state/filters.ts:45`）→ 面板（`App.svelte:404-407` `CameraRawPanel.svelte`）+ Go `ApplyCameraRawFilter`（`internal/render/cameraraw.go:2171`）与滑杆/示波器管线测试（`internal/bridge/filter_test.go:310`） |
| C42 | Remove Background | 28 | M6 | ☑ 票37 |

**Layer**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C43 | New Adjustment Layer（12 种） | — | 37 | M5 | ☐ 无「新建调整层」入口也无 RPC：Go LayerOp 表（`internal/bridge/document.go:117-303`）没有 addAdjustmentLayer，前端 grep `New Adjustment` = 0 |
| C44 | Edit Adjustment… | — | 37 | M5 | ☑ 票31+34：图层面板「编辑」按钮（`LayersPanel.svelte:155-171`）→ `beginFilter(kind, …, layerID, adjustment)` → Go `setAdjustment`（`internal/bridge/document.go:227`）/`ApplyAdjustment`（`internal/render/adjustment.go:19`），调整层渲染有测试（`adjustment_render_test.go`） |
| C45 | Transform Layer / Transform Selection | ⌘T | 32-34 | M3 | ◐ 变换图层全链路（⌘T `table.ts:85` → `state/transform.ts:75` → `ApplyLayerTransform` `layer_transform.go:30`，`layer_transform_test.go`）；Transform Selection 分支未接（`state/selectionCommands.ts:138` 零消费者） |
| C46 | Duplicate Layer / Layer via Copy | ⌘J | 16 | M3 | ☑ 票47：Go `duplicateLayer`（子树/剪贴链/位图独立拷贝）+ 菜单与快捷键 |
| C47 | Create/Release Clipping Mask | ⌥⌘G | 15 | M3 | ☑ 票47：Go `setClippingMask`（共享基底链）+ 菜单项随状态改名 |
| C48 | Group ⌘G / Ungroup ⇧⌘G / Move Out of Folder | ⌘G/⇧⌘G | 12 | M3 | ◐ 票47：编组/取消编组（Go op + 菜单 + 快捷键）；移出文件夹仍是前端纯函数（`moveActiveLayerOutOfGroup`），Go 无 op |
| C49 | New Blank Layer / Rename / Show-Hide | ⇧⌘N | 16 | M3 | ☑ 票20+47：空白层/重命名/可见性（面板）+ ⇧⌘N 快捷键与菜单 |
| C50 | Move Layer Up/Down | ] / [ | 17 | M3 | ☑ 票47：Go moveLayer + 图层菜单与快捷键（方向键 Nudge 亦已接，走 nudgeLayer） |
| C51 | Merge（向下合并/合并所选/合并组，随上下文变标题） | ⌘E | 17 | M3 | ☑ 票47：Go `mergeLayers`（render.Render 合成参与层、剪贴重指向、一步撤销）+ 菜单标题随上下文 |
| C52 | Flip Layer H/V | — | 33 | M3 | ◐ 纯函数 + 测试（`transformSession.ts:359 flipLayerTransform`）；无菜单/快捷键/RPC（frontend grep Flip 只命中 session 自身与测试） |
| C53 | Delete Layer / Layer Mask / Effect（随选中项变标题） | — | 17 | M3 | ◐ 票47：删层（⌫/菜单）与 Go `deleteLayerMask`/`deleteEffect` 已就绪；面板还没有蒙版/特效行的右键入口 |

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
| A08 | Gaussian Blur（溢出层边缘） | CI blur 语义 | 37 | M5 | ☐ 调整层内核缺失：`internal/render/adjustment.go:26-111` 的 dispatch 没有 blur 分支（注释自认「kinds without a kernel yet (the blurs) still pass through」）；破坏式高斯模糊滤镜已实现（票32，见 C37） |
| A09 | Motion Blur | CI motion 语义 | 37 | M5 | ☐ 同 A08：调整层 dispatch 无 motion blur 分支；破坏式运动模糊滤镜已实现（票32，见 C37） |
| A10 | Invert | CPU | 37 | M5 | ◐ 内核已移植（`internal/render/kernels.go:324` ApplyInvert，真值 PixelInvert.swift）且反相菜单可达（C34 ☑）；但 `tests/golden/c/` 无 invert 内核、render 测试里也没有 ApplyInvert 断言——缺参照/回归测试 |
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
| E01 | Stroke（描边，形态学膨胀） | 41 | M2/M5 | ☐ |
| E02 | Drop Shadow（投影） | 41 | M2/M5 | ☐ |
| E03 | Color Overlay（颜色叠加） | 41 | M2/M5 | ☐ |
| E04 | Inner Shadow（内阴影） | 41 | M2/M5 | ☐ |
| E05 | Outer Glow（外发光） | 41 | M2/M5 | ☐ |
| E06 | Inner Glow（内发光） | 41 | M2/M5 | ☐ |

## F. 文档模型与图层系统能力

| # | 能力 | 锚点 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| L01 | 扁平图层+parentID、编组≤64、剪贴链≤256 | `CanvasDocument`/`LayerHierarchy` | 11-12 | M1 | ☑ 票04：扁平层 + parentID（`internal/domain/document.go`）、编组 ≤64 / 剪贴链 ≤256（`domain/limits.go`、`domain/validate.go:158-185`、`limits_test.go`）；Go 侧 layer op 全接（`internal/bridge/document.go:117`） |
| L02 | 24 种混合模式（23+Normal，原顺序，sRGB 空间语义） | `LayerBlendMode` | 11 | M2 | ☑ 票04+09：24 种混合模式原顺序（`internal/domain/blend.go:10-33`）+ sRGB 空间合成（`internal/render/blend.go:21-124`）+ 测试（`blend_test.go`、`render_test.go:15`）；面板下拉与 ⇧= / ⇧- 可达 |
| L03 | 图层/组不透明度（组压暗内部） | `LayerAppearance` | 11 | M2 | ☑ 票09：组压暗内部（`internal/render/composite.go`）+ `render_test.go` 组衰减断言；面板滑杆与数字键可达 |
| L04 | 图层蒙版：绘制/填充/反相/羽化/越层绘制/链接-解链/独立变换 | `LayerMask.swift` | 14 | M2/M3 | ◐ 蒙版渲染/存储齐备（`composite.go:127,225`、`domain.MaskFile`/`MaskPlacement`、`project/validate.go:136`），反相蒙版有菜单入口（`MenuBar.svelte:324`）；但无蒙版行 UI、无绘制/填充/羽化/越层绘制/独立变换入口（`LayersPanel.svelte` 里 mask 0 命中） |
| L05 | 剪贴蒙版 + 组蒙版 | `maskSourceID` | 15 | M2 | ☑ 剪贴蒙版经 ⌥⌘G 可达（`internal/bridge/layer_commands.go:884 linkClippingMask`、`MenuBar.svelte:378`、`layer_commands_test.go`）；组蒙版渲染已接（`composite.go:91`、`render_test.go:238`），无独立创建 UI |
| L06 | 非破坏变换（任意缩小保留原分辨率）+ 采样质量三档 | `LayerTransform.sampling` | 33 | M3 | ◐ 非破坏变换已接线（`state/transform.ts:88` → `layer_transform.go:30`，`layer_transform_test.go` 断言只改 transform）；采样质量三档无 UI（`transform.ts:57` 固定 DEFAULT_SAMPLING） |
| L07 | 快照撤销：嵌套事务/100 条/256MB/revision 脏标记 | `DocumentHistory` | 57 | M1 | ☑ 票06：快照历史（`internal/history/history.go:113,122,313,334`、`history_test.go` 13 个 Test）+ 撤销/重做菜单与 ⌘Z/⇧⌘Z |
| L08 | 文档限额：30,000px 边长/200M 像素/内存预算 RAM/16 | `DocumentLimits` | 51 | M1 | ☑ 票52：`ValidateNewDocument`（边长/像素/分辨率）+ `DocumentPixelBudgetFor`（纯函数，RAM/16 语义：下限 200M、上限 800M；8GiB=537MP 与原版注释一致）+ 拐点表与上限关系测试 |

## G. 选区系统

| # | 能力 | 锚点 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| S01 | 魔棒匹配：容差/连续/全局/取样半径(点/3×3/5×5) | `WandPixels.c` | 27 | M3 | ◐ 内核 + 黄金逐位（`internal/render/wand.go`、`golden_test.go:71-82`，ε=0）；无魔棒 RPC/画布交互，容差/连续/全局/取样半径无 UI |
| S02 | 像素边描迹 → 蚂蚁线闭环路径 | `wand_trace` | 30 | M3 | ◐ 描迹与闭环路径齐备（Go `wand_trace` + `state/selection.ts:117 outlineLoops`、`selection.test.ts`），画布也画蚂蚁线（`CanvasSurface.svelte:846`）；但无创建入口（同 S01） |
| S03 | 色彩范围（采样含/排除色+羽化度） | `color_range_mask` | 29 | M3 | ◐ 同 C29：内核 + 黄金基准齐备；采样含/排除色 + 羽化度的表单不存在 |
| S04 | 扩展/收缩/羽化（蒙版域操作） | `SelectionEdits.swift` | 31 | M3 | ◐ 同 C31：扩展/收缩/羽化纯函数 + 测试齐备，无 UI 入口 |
| S05 | 选区剪贴板/选区内移动复制/边缘自动滚动 | `FloatingSelection` | 30 | M3 | ◐ 票48+16：选区剪贴板（拷/切/粘贴/⌘J）完成；浮动选区搬运已接（方向键 → `MoveSelectedPixels`，`internal/bridge/selection_commands.go:186`、`pixel_move_test.go`、`actions.ts:758-765`）；边缘自动滚动仍是零消费者的纯函数（`state/selection.ts:322`），无拖拽浮层 |
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
| I08 | 保存不阻塞编辑（后台写） | `finishWriting` | 49 | M1 | ☐ 保存仍是同步前台写（`internal/bridge/project_io.go` / SaveProjectDialog 无 goroutine），无后台写 |

## J. 视图/交互/性能

| # | 能力 | 锚点 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| V01 | 视口：0.001–32×、17 级梯、锚点保持、fit/100% | `CanvasViewport` | 3 | M3 | ☑ 票13：0.001–32× / 17 级梯 / 锚点保持 / fit / 100%（`state/viewport.ts`、`viewport.test.ts`），已接画布（`CanvasSurface.svelte:315,658`）与 ⌘0/⌘1/=/− |
| V02 | 像素网格 ≥800% / 棋盘格 / 文档边界 | `drawPixelGrid` | 5/8 | M3 | ☑ 像素网格 ≥800%（`CanvasSurface.svelte:320`）+ 棋盘格 + 文档边界叠层（`CanvasSurface.svelte:828-857`），`viewport.test.ts` 覆盖阈值 |
| V03 | 标尺/参考线（拖出/移动/拖回删除/锁定/清除）/布局网格 | `Guides.swift` | 6-7 | M3 | ☑ 标尺（`CanvasSurface.svelte:974-999`）+ 参考线拖出/移动/拖回标尺删除（`:602,635,750`）+ 锁定（`:582`）+ 清除（`MenuBar.svelte:530`）+ 布局网格（`:858`）；持久化走 `SetGuides`（`internal/bridge/guides.go:30`、`guides_test.go`） |
| V04 | 吸附：参考线/网格/图层/文档边界 + Ctrl 旁路 | `TransformSnap`/`CropSnap` | 35 | M3 | ◐ 内核齐备（`state/snap.ts:221`、`snap.test.ts`，含 Ctrl 旁路与四目标）；零消费端：`state/view.ts:162 snapOptions()` 只被测试引用，画布拖拽（`CanvasSurface.svelte:540`）未传 targets |
| V05 | 锐利缩小采样（Lanczos 级联） | `DownsampleCache` | 51 | M2 | ☐ 全仓无 Lanczos/降采样实现（internal/render grep = 0），缩放靠 CSS img |
| V06 | 数值拖拽调节（拖标签 scrub）/方向键步进/点击跳转 | `NumericScrub`/`SliderSnap` | 54 | M3 控件 | ◐ 纯函数 + 测试齐备（`components/numeric.ts:68,102,116`、`numeric.test.ts`）；零生产消费者（各表单仍用原生 input） |
| V07 | 合成徽标光标（加选/减选/吸管/旋转等 ~10 种） | NSCursor 家族 | — | M3 | ☑ 票13：合成徽标光标按工具/修饰键切换（`components/cursors.ts`、`cursors.test.ts`），已接画布（`CanvasSurface.svelte:340`） |
| V08 | 右键拖拽笔刷 HUD | `EditorCanvas` | 18 | M4 | ◐ 纯函数 + 测试（`lib/gpu/brushCursor.ts`）；零生产消费者，右键只有 preventDefault（`CanvasSurface.svelte:797`） |
| V09 | 4K 笔刷回显 <16ms 基准 | `brush-performance.md` | 19 | M4 | ◐ 票52：`TestBrushStroke4KLatency` 在 4000×4000 上逐段计时，实测 median 1.5–2.0ms / p95 2.1–2.5ms（门禁：默认 48ms，`COMPOSITOR_BENCH_STRICT=1` 时为票面目标 16ms），基线入 `docs/benchmarks/performance.md`。**只有后端、不含显示**，且桥接层只暴露直径 30（原版测 40/800）—— 故 ◐ |
| V10 | 内存压力响应的纹理/缓存逐出 | `GPUCanvas` | — | M2 | ◐ 票52：`internal/memory` 采样档位（<25% moderate / <12% critical），桥接侧真的逐出渲染缓存并把历史压到 10–50 步（注入测试证明撤销/重做/重渲染仍可用），档位变化推 `memory:pressure`；前端注册口就位且有测试。**GPU 纹理缓存尚未接进画布**（票 11 的库层今天没有生产端注册）—— 故 ◐ |

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
| U10 | Camera Raw 光影数值对齐 | 63 | M5 | ☑ |
| U11 | RAW 显影降噪与高光修复 | 64 | M7 | ☑ |
| U12 | PatchMatch 内容感知修复 | 65 | M6 | ☑ |
| U13 | Gaussian Blur 保留透明度 | 66 | M5 | ☑ |
| U14 | 图层特效与可编辑内容保留 | 67 | M5/M7/M8 | ☑ |
| U15 | .comp 16 位存取与版本门控 | 68 | M1/M7 | ☑（ADR-0007；写最低所需版本） |
| U16 | 16 位转换与几何编辑 | 69 | M7 | ✅ |
| U17 | .comp HDR/EXR 存取与版本门控 | 70 | M1/M7 | ✅ 非 sRGB 色度仍按数字读取 |
| U18 | HDR 编辑与画布预览 | 71 | M2/M7 | ◐ 图层编辑已保留高光，调整图层待 82 |
| U19 | CMYK ICC 校样 | 72 | M2/M7 | ◐ 查找表已接到显示，无内置 ICC |
| U20 | Progressive Blur | 73 | M5 | ☐ |
| U21 | 欢迎页粘贴、标签栏和图层建项目 | 74 | M0/M7 | ☐ |
| U22 | 中键平移与修饰键缩放 | 75 | M3 | ☐ |
| U23 | Snap 总开关与命令状态 | 76 | M3/M9 | ☐ |
| U24 | 文件夹直接子图像批量导入 | 77 | M7 | ☐ |
| U25 | Camera Raw 查表与预览性能 | 78 | M5/M10 | ☐ |
| U26 | 半透明像素的图层特效着色 | 79 | M5 | ☐ |
| U27 | Camera Raw 分区重置 | 80 | M5 | ☐ |
| U28 | 快捷键清除与旧配置迁移 | 81 | M9 | ☐ |
| U29 | 16 位图层绘画与调整精度 | 82 | M5/M7 | ☐ 决策门槛 |
| U30 | 16 位与 HDR 屏幕抖动预览 | 83 | M2/M7 | ☐ 决策门槛 |
| U31 | 16 位 PNG/TIFF 导出 | 84 | M7 | ☐ 决策门槛 |
| U32 | HDR EXR 导出 | 85 | M7 | ☐ 决策门槛 |
| U33 | CMYK 文件导入与配置保留 | 86 | M7 | ☐ 决策门槛 |
| U34 | CMYK JPEG/TIFF/PDF 导出 | 87 | M7 | ☐ 决策门槛 |
| U35 | 画布选层后揭示图层 | 88 | M3 | ☐ |
| U36 | 更新窗口发行说明 | 89 | M9 | ☐ |
| U37 | Camera Raw 色彩族对齐 | 90 | M5 | ☐ |
| U38 | 上游资源与截图 CI 等价方案 | 91 | M0/M9 | ☐ |
| U39 | 窄窗口图层与工具栏布局 | 92 | M0/M3 | ☐ |

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
