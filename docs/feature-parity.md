# 功能对等矩阵（Feature Parity Matrix）

> **本文件是"一个功能都不能落下"的唯一权威清单**。以原版源码为锚点逐项核对：菜单命令面（`reference/Swift/Compositor/CompositorApp.swift`）、工具枚举（`EditorSession.NavigationTool`）、调整种类（`LayerAdjustment.AdjustmentKind`）、滤镜种类（`Filters.FilterKind`）、图层特效（`LayerEffects.LayerEffectKind`）、形状（`ShapeTool.ShapeKind`）、混合模式（`LayerAppearance.LayerBlendMode`）。
> 状态：☐ 计划中 → ◐ 实现中 → ☑ 已验收（对照 testing.md 缝合点）。拆票（`/to-tickets`）时逐行核对本矩阵；新增功能必须先在此登记。

## A. 工具（16，`NavigationTool`）

| # | 功能 | 原版锚点 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| T01 | 移动/变换 (V) | `.move` | 33-35 | M3 | ☐ |
| T02 | 选框 (M)：矩形/椭圆双模式 | `.marquee` | 26 | M3 | ☐ |
| T03 | 套索 (L)：自由手/多边形双模式 | `.lasso` | 26 | M3 | ☐ |
| T04 | 魔棒 (W)：颜色魔棒/对象选择，Tab 切换 | `.wand` | 27 | M3/M6 | ◐ 票15 颜色魔棒 + 票37 对象选择 |
| T05 | 裁剪 (C)：比例/对称/选区起点 | `.crop` | 36 | M3 | ☐ |
| T06 | 笔刷 (B) / 橡皮 (E)：大小/硬度/不透明度/平滑、Shift 直线 | `.brush` + `BrushStroke.swift` | 18-19 | M4 | ☐ |
| T07 | 污点修复 (J)：内容感知/创建纹理/临近匹配 | `.spotHealing` + `HealPixels.c` | 21 | M6 | ☑ 票35 |
| T08 | 仿制图章 (S)：对齐开关、单层/全层取样、⌥点设源 | `.cloneStamp` | 20 | M4 | ☐ |
| T09 | 涂抹 (R)：模糊/涂抹/液化三模式 | `.blur` + `MetalWarp` | 22 | M4 | ☐ |
| T10 | 渐变 (G)：可再编辑渐变层 | `.gradient` | 23 | M4 | ☐ |
| T11 | 形状 (U)：矩形(圆角)/椭圆/线，Shift-U 切换，保持可编辑 | `.shape` + `ShapeKind` | 24 | M4 | ☐ |
| T12 | 文字 (T)：段落框/内联多行/IME | `.type` + `TypeTool.swift` | 43-45 | M8 | ☑ 票44+45：创建/编辑/提交/取消/IME/合成/往返 + 逐 run 样式与段落框缩放 |
| T13 | 吸管 (I)：取样环（可开关） | `.eyedropper` + `showsSampleRing` | 25 | M3 | ☐ |
| T14 | 抓手 (H) / 空格平移 | `.hand` | 4 | M3 | ☐ |
| T15 | 缩放 (Z)：点击/拖拽 | `.zoom` | 3 | M3 | ☐ |
| T16 | 无工具 (A)：画布点击无操作 | `.idle` | — | M3 | ☐ |

## B. 菜单命令（`CompositorApp.swift` 全量）

**File**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C01 | New Canvas…（欢迎屏） | ⌘N→Ctrl+N | 9 | M0 | ☐ |
| C02 | Open Project… / 拖放打开 | ⌘O | 1 | M1/M9 | ☐ |
| C03 | Open Recent（含清除） | — | 53 | M9 | ☑ 票49：文件菜单逐条列出最近项目（完整路径在 tooltip）+ 清除最近记录；列表上限 10、读取时过滤已移动/删除的文件（RecentProjects.refresh 语义），与 Windows 跳转列表共用一份 |
| C04 | Import Images… | — | 46 | M7 | ☑ 票40（多选批次，RAW/HEIC 随票41） |
| C05 | Save / Save As | ⌘S / ⇧⌘S | 49 | M1 | ☐ |
| C06 | Export PNG… | ⇧⌘E | 48 | M7 | ☑ 票42 |
| C07 | Export JPEG…（实时预览内可缩放） | ⇧⌥⌘S | 48 | M7 | ☑ 票42 |
| C08 | Close Project / 退出逐标签确认 | ⌘W | 2 | M1 | ☐ |
| C09 | Check for Updates… | — | 53 | M9 | ☐ |

**Edit（剪贴板/填充组）**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C10 | Undo/Redo（菜单名带操作名） | ⌘Z / ⇧⌘Z | 57 | M1 | ☐ |
| C11 | Cut / Copy / Paste（像素与文字域分流） | ⌘X/C/V | 55 | M3/M9 | ☑ 票48：选区像素拷/切（剪切留洞、可撤销）、整层复制（含编组/调整层）、外部图像粘贴、⌘V 落层丢选区；文字域由原生 textarea 自己处理（输入框内快捷键让位，菜单项同时禁用）。对照 SelectionClipboardTests 的 9 个用例：原位粘贴/剪切回填/形状选区（套索软边有测试）/选区包围盒裁剪已覆盖，变换会话那 4 个用例属票 16。**系统剪贴板的 Win32 手递手（winclip 的 C 侧）没有自动化测试**——接缝在测试里是内存假实现，纯 Go 的 CF_DIB/PNG 解码有测试 |
| C12 | Copy Merged | ⇧⌘C | 48 | M7 | ☑ 票42（CF_DIB；剪贴板像素/整层 UI 通道随票48） |
| C13 | 整层复制/粘贴/跨标签拖拽 | — | 16 | M3 | ☑ 票48：`copyLayersInto` 跨会话复制子树（新 UUID/资产、位图深拷贝、引用重映射、目标画布居中、一步撤销）+ 图层面板拖到标签栏（`TransferLayer`）+ ⌘V 的跨标签分支（`copiedLayerFor` 命中别的会话 → `copyLayersInto`，有测试）。面板内部的拖拽重排仍属票 20 |
| C14 | Keyboard Shortcuts…（重映射表单） | — | 52 | M9 | ☑ 票47：表单（录制/冲突提示/恢复默认/持久化）+ 113 条定义表 + 三域分流 |
| C15 | Fill with Foreground/Background Color | ⌥⌫ / ⌘⌫ | 42 | M4 | ☐ |
| C16 | Clear Selection Pixels | — | 42 | M4 | ☐ |
| C17 | Content-Aware Fill… | ⇧⌫ | 42 | M6 | ☑ 票35 |

**View（画布组）**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C18 | Fit Canvas / Actual Pixels / Zoom In/Out | ⌘0/⌘1/=/- | 3 | M3 | ☑ 票13+47：视口命令 + 快捷键/视图菜单 |
| C19 | Pixel Grid 开关（≥800%） | — | 5 | M3 | ☐ |
| C20 | Snap 总开关 | — | 7 | M3 | ☐ |
| C21 | Show Transform Controls | ⌘H | 34 | M3 | ☐ |
| C22 | Grid ' / Guides ; / Rulers ⌘R 开关 | ' ; ⌘R | 6 | M3 | ☐ |
| C23 | Grid Settings…（间距/细分） | — | 6 | M3 | ☐ |
| C24 | Snap To：参考线/网格/图层/文档边界 | — | 7 | M3 | ☐ |
| C25 | Lock Guides / Clear Guides | ⌥; | 7 | M3 | ☐ |

**Select**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C26 | All / Deselect / Inverse | ⌘A/⌘D/⇧⌘I | 29 | M3 | ☐ |
| C27 | Layer's Pixels（载入图层像素为选区） | — | 29 | M3 | ☐ |
| C28 | Subject（选择主体） | ⌥⌘A | 28 | M6 | ☑ 票37 |
| C29 | Color Range…（色彩范围表单） | — | 29 | M3 | ☐ |
| C30 | Mask's Black Areas（载入蒙版黑色区） | — | 29 | M3 | ☐ |
| C31 | Expand… / Contract… / Feather… | — | 31 | M3 | ☐ |

**Image**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C32 | Curves… / Levels… / Hue-Saturation…（作用于像素） | ⌘M/⌘L/⌘U | 38 | M5 | ☑ 票31 |
| C33 | Black & White / Color Balance / Exposure / Gradient Map / Grain（像素版） | — | 38 | M5 | ☑ 票31 |
| C34 | Invert ⌘I（选中蒙版时为 Invert Mask） | ⌘I | 38 | M5 | ☑ 票31 |
| C35 | Canvas Size… / Image Size… / Trim… | ⌥⌘C/⌥⌘I | 50 | M7 | ☑ 票43 |
| C36 | Flip Canvas Horizontal / Vertical | — | 33 | M3 | ☐ |

**Filter（`FilterKind` 除内容感知与图像调整外全部）**

| # | 命令 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- |
| C37 | Gaussian Blur / Motion Blur | 40 | M5 | ☑ 票32 |
| C38 | Add Noise / Vignette / Bloom-Glow | 40 | M5 | ☑ 票32（滤镜菜单） |
| C39 | Dither（11 种风格） | 40 | M6 | ☑ 票36 |
| C40 | Tonal Contrast / Lens Correction | 40 | M5 | ☑ 票32 |
| C41 | Camera Raw Filter（全参数面板） | 39 | M5 | ☐ |
| C42 | Remove Background | 28 | M6 | ☑ 票37 |

**Layer**

| # | 命令 | 快捷键 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C43 | New Adjustment Layer（12 种） | — | 37 | M5 | ☐ |
| C44 | Edit Adjustment… | — | 37 | M5 | ☐ |
| C45 | Transform Layer / Transform Selection | ⌘T | 32-34 | M3 | ☐ |
| C46 | Duplicate Layer / Layer via Copy | ⌘J | 16 | M3 | ☑ 票47：Go `duplicateLayer`（子树/剪贴链/位图独立拷贝）+ 菜单与快捷键 |
| C47 | Create/Release Clipping Mask | ⌥⌘G | 15 | M3 | ☑ 票47：Go `setClippingMask`（共享基底链）+ 菜单项随状态改名 |
| C48 | Group ⌘G / Ungroup ⇧⌘G / Move Out of Folder | ⌘G/⇧⌘G | 12 | M3 | ◐ 票47：编组/取消编组（Go op + 菜单 + 快捷键）；移出文件夹仍是前端纯函数（`moveActiveLayerOutOfGroup`），Go 无 op |
| C49 | New Blank Layer / Rename / Show-Hide | ⇧⌘N | 16 | M3 | ☑ 票20+47：空白层/重命名/可见性（面板）+ ⇧⌘N 快捷键与菜单 |
| C50 | Move Layer Up/Down | ] / [ | 17 | M3 | ☑ 票47：Go moveLayer + 图层菜单与快捷键（方向键 Nudge 亦已接，走 nudgeLayer） |
| C51 | Merge（向下合并/合并所选/合并组，随上下文变标题） | ⌘E | 17 | M3 | ☑ 票47：Go `mergeLayers`（render.Render 合成参与层、剪贴重指向、一步撤销）+ 菜单标题随上下文 |
| C52 | Flip Layer H/V | — | 33 | M3 | ☐ |
| C53 | Delete Layer / Layer Mask / Effect（随选中项变标题） | — | 17 | M3 | ◐ 票47：删层（⌫/菜单）与 Go `deleteLayerMask`/`deleteEffect` 已就绪；面板还没有蒙版/特效行的右键入口 |

## C. 调整图层（`AdjustmentKind`，12 种，非破坏）

| # | 种类 | 实现锚点 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| A01 | Hue/Saturation（分区/分段） | 33³ color cube | 37 | M5 | ◐ 票10：LUT/cube 生成+接线已落地，M5 内核票 37 出口 |
| A02 | Levels（Auto + 黑/灰/白三吸管） | `LevelsPixels.c` LUT | 37 | M5 | ◐ 票10：LUT/cube 生成+接线已落地，M5 内核票 37 出口 |
| A03 | Curves | 256 项/通道表 | 37 | M5 | ◐ 票10：LUT/cube 生成+接线已落地，M5 内核票 37 出口 |
| A04 | Exposure | sRGB↔线性表 | 37 | M5 | ◐ 票10：LUT/cube 生成+接线已落地，M5 内核票 37 出口 |
| A05 | Gradient Map | 256×3 表 | 37 | M5 | ◐ 票10：LUT/cube 生成+接线已落地，M5 内核票 37 出口 |
| A06 | Grain（胶片颗粒，文档锚定噪声场） | `adjust_grain` | 37 | M5 | ☐ |
| A07 | Add Noise（位置+种子确定性） | `noise_add_at` | 37 | M5 | ☐ |
| A08 | Gaussian Blur（溢出层边缘） | CI blur 语义 | 37 | M5 | ☐ |
| A09 | Motion Blur | CI motion 语义 | 37 | M5 | ☐ |
| A10 | Invert | CPU | 37 | M5 | ☐ |
| A11 | Black & White（6 通道权重+着色） | `adjust_black_white` | 37 | M5 | ☐ |
| A12 | Color Balance（保明度） | `adjust_color_balance` | 37 | M5 | ☐ |

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
| L01 | 扁平图层+parentID、编组≤64、剪贴链≤256 | `CanvasDocument`/`LayerHierarchy` | 11-12 | M1 | ☐ |
| L02 | 24 种混合模式（23+Normal，原顺序，sRGB 空间语义） | `LayerBlendMode` | 11 | M2 | ☐ |
| L03 | 图层/组不透明度（组压暗内部） | `LayerAppearance` | 11 | M2 | ☐ |
| L04 | 图层蒙版：绘制/填充/反相/羽化/越层绘制/链接-解链/独立变换 | `LayerMask.swift` | 14 | M2/M3 | ☐ |
| L05 | 剪贴蒙版 + 组蒙版 | `maskSourceID` | 15 | M2 | ☐ |
| L06 | 非破坏变换（任意缩小保留原分辨率）+ 采样质量三档 | `LayerTransform.sampling` | 33 | M3 | ☐ |
| L07 | 快照撤销：嵌套事务/100 条/256MB/revision 脏标记 | `DocumentHistory` | 57 | M1 | ☐ |
| L08 | 文档限额：30,000px 边长/200M 像素/内存预算 RAM/16 | `DocumentLimits` | 51 | M1 | ☐ |

## G. 选区系统

| # | 能力 | 锚点 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| S01 | 魔棒匹配：容差/连续/全局/取样半径(点/3×3/5×5) | `WandPixels.c` | 27 | M3 | ☐ |
| S02 | 像素边描迹 → 蚂蚁线闭环路径 | `wand_trace` | 30 | M3 | ☐ |
| S03 | 色彩范围（采样含/排除色+羽化度） | `color_range_mask` | 29 | M3 | ☐ |
| S04 | 扩展/收缩/羽化（蒙版域操作） | `SelectionEdits.swift` | 31 | M3 | ☐ |
| S05 | 选区剪贴板/选区内移动复制/边缘自动滚动 | `FloatingSelection` | 30 | M3 | ◐ 票48：选区剪贴板（拷/切/粘贴、⌘J 通过拷贝的图层）完成；浮动选区移动与边缘自动滚动仍属票 16 |
| S06 | 对象选择/主体/移除背景 + 引导滤波抠边 | Vision→ONNX（ADR-0005） | 28 | M6 | ☑ 票37 |
| S07 | 内容感知填充（可延伸出画布） | `ContentFill.c` | 42 | M6 | ☑ 票35 |

## H. 文本（`TypeTool.swift`/`PSDText.swift`）

| # | 能力 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- |
| X01 | 段落框拖拽/缩放/内联多行编辑/IME/⌘回车提交 | 43 | M8 | ☑ 票44+45：建框/点文本/IME/提交 + 8 向手柄缩放（16…30000、面积 2e8） |
| X02 | 字体/字号/颜色/对齐/字距/行高，逐 run 混排 | 44 | M8 | ☑ 票45：逐 run 编辑/渲染 + 字体枚举选择器与回退 + 行内选区保留 + manifest v11 往返（编辑期不画混排，提交后由 Go 光栅） |
| X03 | 文本变换 + 用作剪贴蒙版 | 45 | M8 | ☐ |
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
| I07 | `.comp` v11 读写/校验/原子保存/包内预览图 | `ProjectStore`（ADR-0002） | 1/49 | M1 | ☐ |
| I08 | 保存不阻塞编辑（后台写） | `finishWriting` | 49 | M1 | ☐ |

## J. 视图/交互/性能

| # | 能力 | 锚点 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| V01 | 视口：0.001–32×、17 级梯、锚点保持、fit/100% | `CanvasViewport` | 3 | M3 | ☐ |
| V02 | 像素网格 ≥800% / 棋盘格 / 文档边界 | `drawPixelGrid` | 5/8 | M3 | ☐ |
| V03 | 标尺/参考线（拖出/移动/拖回删除/锁定/清除）/布局网格 | `Guides.swift` | 6-7 | M3 | ☐ |
| V04 | 吸附：参考线/网格/图层/文档边界 + Ctrl 旁路 | `TransformSnap`/`CropSnap` | 35 | M3 | ☐ |
| V05 | 锐利缩小采样（Lanczos 级联） | `DownsampleCache` | 51 | M2 | ☐ |
| V06 | 数值拖拽调节（拖标签 scrub）/方向键步进/点击跳转 | `NumericScrub`/`SliderSnap` | 54 | M3 控件 | ☐ |
| V07 | 合成徽标光标（加选/减选/吸管/旋转等 ~10 种） | NSCursor 家族 | — | M3 | ☐ |
| V08 | 右键拖拽笔刷 HUD | `EditorCanvas` | 18 | M4 | ☐ |
| V09 | 4K 笔刷回显 <16ms 基准 | `brush-performance.md` | 19 | M4 | ☐ |
| V10 | 内存压力响应的纹理/缓存逐出 | `GPUCanvas` | — | M2 | ☐ |

## K. 平台集成与代理契约

| # | 能力 | Windows 方案 | 故事# | 里程碑 | 状态 |
| --- | --- | --- | --- | --- | --- |
| P01 | ~114 条快捷键 + 重映射 + 冲突校验 | 前端快捷键表（Cmd→Ctrl） | 52 | M9 | ☑ 票47：原版实测 113 条逐条移植（表驱动测试）+ 重映射/冲突校验/持久化 + 三域分流；票48 接上 Cut/Copy/Paste 后未接线 22 条，已逐条登记归属 |
| P02 | 剪贴板（像素/整层/合并） | Windows 剪贴板 | 55 | M9 | ☑ 票42+48：CF_DIB 写（拷贝合并/选区像素）+ 读（PNG 注册格式回落 CF_DIB，纯 Go 解码含 1/4/8/16/24/32bpp，PNG 解码有测试）+ 剪贴板序号做变更判定。**与 C11 同一 caveat：winclip 的 Win32 读写本体（C shim）无自动化测试，只在真机上跑**；`GetImage`/`Sequence`/`PutBitmap` 的实际行为需要手工验收（拷进画图、从浏览器拷图回来） |
| P03 | 文件关联/最近文件/跳转列表 | NSIS + shell | 53 | M9 | ☑ 票49：`wails.json` 的 `info.fileAssociations` 经 NSIS 注册 `.comp` 关联（生成的宏写 `"$INSTDIR\Compositor.exe" "%1"`，双击/打开方式进标签）+ 最近文件（用户配置目录 JSON，打开与保存都记）+ 跳转列表（`SHAddToRecentDocs`）。真机跑通过、**无自动化断言**；覆盖图标实测会卡（见 P08） |
| P04 | 安装包 + 签名 + 自动更新 | NSIS + GitHub Releases + EdDSA | 53 | M9 | ☐ |
| P05 | 代理热更新（写 PNG→原子改名→~300ms 重载，保留视口选区） | fsnotify + digest | 56 | M1/M9 | ☐ |
| P06 | 常暗深色 UI / 单窗口多标签 | Svelte 外壳 | 2/53 | M0 | ☐ |
| P07 | 拖放导入（文件/截图/网页图） | WebView 拖放 | 55 | M9 | ◐ 票48：三来源都接了（Wails OnFileDrop 路径 → `ImportDroppedPaths`；截图/网页图 → 浏览器栅格化 + `BeginBlobImport` 上传面）；dragover 对 Files 与 text/uri-list/text/html 都取消默认（否则网页图那条永远收不到 drop），两路用 250ms 等待 + 750ms 去重窗口避免重复导入。Go 侧 8 个测试、前端判定与接线 13 个测试；**真机拖放交互未手工验收**（P07 仍 ◐ 的原因） |
| P08 | 窗口脏标记/标题（任务栏覆盖图标） | Win32 等价 | 10 | M9 | ☑ 票49：窗口标题 `名字* - Compositor`（脏标记加星）+ 任务栏覆盖图标（`ITaskbarList3::SetOverlayIcon` 红点，失败只记日志）。真机发现该 API 会跨进程卡住（最长 >60s），因此 winchrome 每次调用最多等 2s 且有界失败、与最近文档各用独立闸门；未截到任务栏红点图 |
| P09 | 首次启动窗口尺寸记忆 | 窗口状态持久化 | 9 | M9 | ☑ 票02+49：`WindowStore`（用户配置目录 window.json）+ 启动恢复 + resize 防抖保存；票49 核对后勾 |
| P10 | 启动到就绪基准（原版 LAUNCH_TO_READY_MEDIAN） | 性能测试 | — | M10 | ☐ |

## 明确不做（与原版对齐的省略，非遗漏）

1. Darker Color / Lighter Color 混合模式——原版 `LayerAppearance.swift` 注释明确省略。
2. CMYK/16bit/Display P3——原版 sRGB 8bit 单色彩空间（ADR-0002）。
3. Apple Pencil/触控条/Force Touch——原版无此功能。
4. macOS 专属：QuickLook 面板、Hide Others 语义、NSFontPanel——以 Windows 等价物替代（P03-P08），QuickLook 包内预览图仍生成（跨平台通用）。
