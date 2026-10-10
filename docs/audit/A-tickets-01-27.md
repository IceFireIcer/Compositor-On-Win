# 票 01–27 实现完整性审计（只读审计报告）

- 审计员：audit-early ｜ 共享任务：task-1 ｜ 写入域：本文件
- **审计基线：提交 `679d5d8`**（`perf(memory): 票 52 …`）。审计期间工作区存在**未提交改动**：`frontend/src/lib/components/CanvasSurface.svelte`、`frontend/src/lib/state/document.ts`（已改）、新增未跟踪 `frontend/src/lib/state/view.ts` + `view.test.ts` —— 有队友正在补**票 14 的参考线/视图开关接线**。本报告所有行号按 `679d5d8` 计；上述三处另注「施工中」。
- 方法：逐票读 `docs/tickets/NN-*.md` 的 `- [x]` 验收清单 → 在代码里定位真实实现（全部经 read/grep 亲自查看）→ 判定状态。
- 状态：**✅ 已实现**（含端到端接线）／**⚠️ 部分**（有实现但缺接线或缺一侧）／**❌ 缺失**／**🔍 无法静态验证**。

## 统计

| 判定 | 票号 | 计数 |
| --- | --- | --- |
| ✅ | 01 02 03 04 06 07 08 09 10 13 26 | 11 |
| ⚠️ | 05 15 16 19 20 21 25 27 | 8 |
| ❌ | 11 12 14 17 18 22 23 24 | 8 |

---

## 01 — 脚手架与开发链路 ✅

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| wails dev 热重载 | 🔍 | — | 需运行态，静态不可验。 |
| 深色主题外壳 | ✅ | `frontend/src/app.css:1-12` | 全套 CSS 变量（--bg-app/#1e1e20 等），头部注释即写明 ticket 01。 |
| Go 绑定往返 → 状态栏 | ✅ | `internal/bridge/app.go:24,128`；`frontend/src/App.svelte:253,425` | `Version()` 注入 status bar，失败退化为「离线」。 |
| go build / npm run build / TS strict | ✅ | — | Lead 已验（基线）。 |
| wails 绑定类型入版本控制 | ✅ | `frontend/wailsjs/go/bridge/Service.d.ts:142` | `export function Version():Promise<string>`。 |

**接线缺口**：无。

## 02 — 编辑器外壳 UI 与新建画布 ✅

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 工具栏/标签条/工具轨/状态栏/图层面板 | ✅ | `App.svelte:17-19,305,389,429`；`lib/components/TabStrip.svelte`、`NewCanvasSheet.svelte`、`CanvasSurface.svelte` | 组件齐全并已挂载。 |
| 新建画布校验来自 Go | ✅ | `internal/domain/limits.go:40`；`internal/bridge/workspace.go:71`；`limits_test.go:8` | 30,000px / 200M / 1–9600ppi，含边界值测试。 |
| 棋盘格 + 文档边界 + 状态栏信息 | ✅ | `CanvasSurface.svelte:577-579,736`（HEAD） | CSS 棋盘格 + 边界叠层。 |
| 窗口尺寸持久化 | ✅ | `internal/bridge/windowstate.go:24-60`；`App.svelte:265-284` | `window.json` + 400ms 防抖保存。 |
| 标签条新建/切换/关闭/脏确认/回欢迎屏 | ✅ | `workspace.ts:63-80`；`TabStrip.svelte:35-84`；`internal/bridge/workspace.go:148-150`；`confirm.ts` | 关闭活动标签自动激活邻居；`window.confirm` 在 WebView2 不可用，故改用自绘对话框。 |

**接线缺口**：无。

## 03 — CI 门禁与 NSIS 安装包骨架 ✅（CI 未实跑）

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| verify.yml 全链路 | ✅ | `.github/workflows/verify.yml:21-92,94-151` | verify（前端+Go+基准）与 package（Wails+NSIS+artifact）两 job，`concurrency` 取消并发。 |
| 任一步失败即红、artifact 兜底 | ✅ | `verify.yml:96-97,149` | `needs: verify` + `if-no-files-found: error`。 |
| NSIS `choco install nsis` | ✅ | `verify.yml:138-143` | 注释记录了「缺 makensis 只警告」的坑。 |
| 耗时预算 | 🔍 | `verify.yml:14-17` | 预算合理；**CI 从未真实跑过**（票内自述）。 |

**接线缺口**：workflow 未在 GitHub 上首跑 → artifact/NSIS 冒烟仍为人工待办。

## 04 — 文档域模型与限额 ✅

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| manifest 全字段 | ✅ | `internal/domain/document.go:22-81` | Layer 20 字段、transform/levels/curves/effects/text；`hsvSettings` 有意留 `json.RawMessage`（票 26 类型化）。 |
| 限额校验 + 边界单测 | ✅ | `limits.go:18-21,71`；`validate.go:13-16,158-185`；`limits_test.go`、`validate_test.go:37-235` | 编组 ≤64、剪贴链 ≤256、参考线 ≤1000、pos ≤1e6 全部落地。 |
| 24 种混合模式拼写一致 | ✅ | `internal/domain/blend.go:10-33`、`:37+`；`blend_test.go:8` | 含 "Linear Dodge (Add)" / "Color Burn"。 |
| 序列化往返等价 + omitempty 语义 | ✅ | `document_test.go:180,218` | 指针字段 nil 保真。 |

**接线缺口**：无。

## 05 — .comp 存取校验与互开 ⚠️

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 打开 macOS .comp（两段式解码 + UUID 规范化） | ⚠️ | `internal/project/manifest.go:46-65`；`manifest_test.go:123-280` | 逻辑与夹具齐备，**真实 1.4.5 实机 .comp 夹具仍缺** → 双向互开未实测。 |
| 排序键 pretty JSON | ⚠️ | `internal/project/manifest.go:17`（TODO 字节级对齐） | 缩进/HTML 转义/去尾换行都对，键值分隔符未与真实夹具核对。 |
| 非法文件全拒 + 五类哨兵 | ✅ | `internal/project/errors.go:15-28`（含 `VersionError`）；`store_test.go:182-265`；`validate_test.go:66-534` | 路径穿越/符号链接逃逸/非 8 位灰度蒙版/断链/循环/版本门控都有测试。 |
| 原子保存 | ✅ | `internal/fsatomic/fsatomic.go`；`store_test.go:146` | 资产先落盘、manifest 后 rename，失败保持原包可打开。 |
| 往返等价 + 陈旧资产清理 | ✅ | `store_test.go:98,116` | — |

**接线缺口**：真实 macOS 夹具未到位（唯一未闭环项）。

## 06 — 快照式撤销历史 ✅

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| UndoName / RedoName | ✅ | `internal/history/history.go:113,122` | 语义对齐 DocumentHistory.swift。 |
| 嵌套事务 / no-op 保留 redo | ✅ | `history.go:160,177,210`；`history_test.go:216,284,314` | — |
| 修剪 100 条 / 256MB | ✅ | `history.go:313,334`；`history_test.go:448,516,664` | 字节核算用可注入估算器（票内已注明偏差）。 |
| isModified / MarkSavedRevision | ✅ | `history.go:130,140,145`；`history_test.go:579` | — |
| HistoryTests 11 例移植 | ✅ | `history_test.go:46-713`（13 个 Test 函数） | — |

**接线缺口**：无（`internal/bridge/workspace.go:377-436` 已用 hist.Begin/End/Undo/Redo）。

## 07 — 外部变更监视与原位重载 ✅

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 300ms 合并 + digest 稳定回调 | ✅ | `internal/watch/coordinator.go:12,71,141-158`；`coordinator_test.go:67` | — |
| 重载保留视口/选区、清撤销 | ✅ | `internal/bridge/watch.go:165`；`Workspace.ReloadDocument`；`frontend/src/lib/state/external.test.ts` | 票 51 已完成接线。 |
| 脏文档「放弃/保留」询问 | ✅ | `internal/bridge/watch.go:22-30,196,242`；`lib/components/ExternalChangePrompt.svelte`；`App.svelte:49-50,444` | `external:changed` pending 通路。 |
| 原子改名后监视续存 | ✅ | `internal/watch/watcher.go`；`watcher_test.go` | — |
| 忙等退避有上限终态 | ✅ | `coordinator.go:12-35`；`coordinator_test.go:111-161` | 250ms×2^n，MaxAttempts 封顶。 |

**接线缺口**：无（票 51 已补）。

## 08 — 黄金基准 harness ✅

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 9 个 C 内核编译 + 参考 PNG | ✅ | `tests/golden/c/*.c`（9 内核 + `golden_dispatch.c` / `driver_main.c`）；`c/README.md`；`tests/golden/ref/*.png` | 来源提交已注明。 |
| 参考图库 + 参数 JSON 入库可复现 | ✅ | `tests/golden/cases/*.json`（35 例）；`tests/golden/gen/main.go` | 固定种子、确定性。 |
| 比对协议：偏差 1 位即失败 | ✅ | `golden_test.go:368-385` | — |
| 后续「与 C 一致」指向本设施 | ✅ | `golden_test.go:391-437` | 未移植内核具名跳过 + 「有 port 却未比对」反查。 |

**接线缺口**：无。

## 09 — CPU 合成器真值 ✅

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 典型文档合成像素断言 | ✅ | `internal/render/composite.go:39-305`；`render_test.go:147-278` | 恒等/不透明度/蒙版（链接+解链）/剪贴/组衰减/旋转 90°。 |
| 24 混合模式 sRGB 语义 | ✅ | `internal/render/blend.go:21-124`；`render_test.go:15,61` | 含非可分离 SetLum/SetSat/ClipColor。 |
| 黄金比对「先例形态」 | ✅ | 见票 08 设施 | 票内自述为协议先例，非全部比对。 |
| 并行 = 串行 | ✅ | `render_test.go:278`；`composite_bench_test.go` | GOMAXPROCS(1) 对比。 |
| GPU 对比协议先例 | ✅ | `internal/render/image.go`（Bitmap / EncodePNG） | — |

**接线缺口**：调整层已接（`composite.go:111`）；**特效层完全未接**（见票 12）。

## 10 — 调整渲染真值（LUT/cube）✅

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| LUT/cube 数值对齐原版 | ✅ | `internal/render/lut.go`、`internal/render/cube.go`；`lut_test.go`、`cube_test.go` | 对照 Swift 手算用例。 |
| 调整层在 CPU 合成器正确渲染 | ✅ | `composite.go:111-165` | 非破坏、按模式混合、蒙版×不透明度预乘交叉淡化。 |
| CPU/GPU 共用 LUT/cube | ⚠️ | `lut.go` / `cube.go`；`frontend/src/lib/gpu/shaders/adjust.ts` | 结构可复用，但 **GPU 侧无消费者**（票 11/12 未接线）。 |
| 33³ 边界与插值语义 | ✅ | `cube_test.go`；`golden_test.go:68-70`（cube-synthetic 逐位） | — |

**接线缺口**：GPU 调整 pass 无生产调用者。

## 11 — WebGPU 合成器 ❌

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 复杂 .comp 60fps 合成 | ❌ | `frontend/src/lib/gpu/README.md:29-33` | 只有采样**协议**，无执行体、无实测数字。 |
| 不可用时回退 CPU 且功能不缺失 | ⚠️ | `lib/gpu/device.ts`；`device.test.ts` | 探测库正确，但画布**从未调用**它；实际渲染一直是 Go 位图，回退语义无从谈起。 |
| GPU 输出与 CPU 真值一致（逐模式） | ❌ | `README.md:19-27` | 仅协议；无 Playwright 执行体。 |
| 修订号失效仅重取该层 | ⚠️ | `lib/gpu/layerBitmaps.ts` + 测试 | 库层达标，**零生产消费者**。 |
| 纹理 LRU + 内存压力 | ⚠️ | `lib/gpu/textureCache.ts` + 测试 | 同上。 |

**接线缺口（严重）**：`frontend/src/lib/gpu/` 全部模块（device/renderGraph/layerBitmaps/textureCache/shaders）**仅被自己的测试引用**；生产代码里搜不到任何 `lib/gpu` 导入。`README.md:37-39` 承诺「`CanvasSurface` 持有 `WebGpuCompositor`」，但 `CanvasSurface.svelte` 只渲染 Go 返回的位图并走桥接笔刷 —— 该承诺未兑现。

## 12 — WGSL 特效与 GPU 调整 ❌

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 6 种特效 GPU 渲染且与 CPU 真值一致 | ❌ | `lib/gpu/shaders/effects.ts`、`lib/gpu/effectPass.ts`；`internal/render/` 全树 grep 无 shadow/glow/stroke 渲染 | **CPU 特效真值根本不存在**（`domain/effects.go` 只有数据模型；`bridge/layer_commands.go:1010-1085` 只有增删改名），"与 CPU 一致"无从成立。 |
| 调整层 GPU 应用与 CPU 一致 | ❌ | `shaders/adjust.ts` | 无消费者、无对照执行。 |
| 特效 + 调整组合一致性 | ❌ | — | 同上。 |
| 特效属性变更实时反映 | ❌ | `effectPass.ts` 缓存键控 | 纯函数级；UI 无特效行（见票 20）。 |
| 内存压力下逐出且不闪白 | ⚠️ | `effectPass.ts` stale-while-revalidate + 测试 | 库层达标，无生产接入。 |

**接线缺口（严重）**：特效轨道 = 前端纯库 + Go 数据模型，中间**没有任何渲染实现**；`LayersPanel.svelte` 无特效行、无右键入口。

## 13 — 视口与画布导航 ✅

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 缩放梯 + 锚点保持数学 | ✅ | `lib/state/viewport.ts:14-16,161,284`；`viewport.test.ts` | 0.001–32×、17 级；已接入 `CanvasSurface.svelte:289,605`（HEAD）。 |
| 像素网格 / 棋盘格 / 文档边界 | ✅ | `CanvasSurface.svelte:7,289,605,736`（HEAD；`PIXEL_GRID_ZOOM=8` ← `viewport.ts:32`） | ≥800% 显示。 |
| 光标按工具/修饰键切换 | ✅ | `lib/components/cursors.ts:50-170`；`CanvasSurface.svelte:291`（HEAD） | 徽标光标为 SVG 近似（票内自述「视觉冒烟级」）。 |
| 修饰键事件位可被工具层消费 | ✅ | `lib/state/modifiers.ts:131-132`；`CanvasSurface.svelte:17,421`（HEAD） | MetaLeft/Right → Ctrl 位正确。 |

**接线缺口**：视口本身无缺口；但**工具层只消费了 hand/zoom/type/spotHealing/brush**（见票 15/17/18）。

## 14 — 标尺/参考线/网格/吸附 ❌（施工中）

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 拖出/移动/删除参考线 + 锁定/清除 | ❌ | `lib/state/guides.ts`（纯函数 + `guides.test.ts`） | 基线时**无 store、无标尺 UI、无画布渲染**；`guides.ts` 的消费者只有同样未接线的 `crop.ts` / `snap.ts` / `transformSession.ts`。 |
| Snap To 四目标 + 总开关 + Ctrl 旁路 | ❌ | `lib/state/snap.ts`；`snap.test.ts` | 纯函数达标，零生产消费者。 |
| 网格设置表单（间距/细分） | ❌ | 无 GridSettingsSheet 类组件 | `snap.ts:52` 有 `normalizeGrid`，无表单。 |
| 参考线随文档持久化、可撤销 | ⚠️ | `internal/domain/document.go:81`（guides 字段）；`project/validate.go`（v8 门控） | 模型/存储往返在，**但基线时没有任何写回 RPC**（`wailsjs` 与 `internal/bridge` 都搜不到 SetGuides）。 |

**接线缺口**：整票的 UI 层缺失；`actions.test.ts:166-171` 把 5 条视图命令（Show Grid/Guides/Rulers/Snap/Lock Guides）显式钉为未接线。
**施工中**：审计时有队友新增 `frontend/src/lib/state/view.ts` 并在改 `CanvasSurface.svelte`（标尺拖出 / 参考线命中测试）——若已完成，本票应从 ❌ 上调，但**须核实是否同时补上 SetGuides 回写与网格设置表单**。

## 15 — 选区工具（选框/套索/魔棒）⚠️

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 魔棒与 C 黄金基准逐位一致 | ✅ | `internal/render/wand.go`；`golden_test.go:71-82`；wand case ε=0 | 含 3×3/5×5 取样与描迹。 |
| 选框/套索/魔棒创建-加减-移动-复制闭环 | ❌ | `lib/state/selection.ts`；`CanvasSurface.svelte:379,474`（HEAD：`PAINT_TOOLS = {move, brush}`） | 画布指针分派里**没有** marquee/lasso/wand 分支 → 用户无法在画布上创建选区。 |
| 蚂蚁线动画与选区框视觉 | ⚠️ | `CanvasSurface.svelte:40,601`（HEAD：`loopPath` 画 SVG path） | 静态路径可画，但无 CSS 动画；且因上一条，只有 Go 侧选区（主体/对象/色阶吸管）才会出线。 |
| 边缘自动滚动 | ❌ | `selection.ts:316`（纯函数）；`CanvasSurface.svelte` 无调用 | 函数存在，零调用者。 |

**接线缺口**：选框/套索/魔棒工具的画布交互、蚂蚁线动画、自动滚动。

## 16 — 选区命令族与色彩范围 ⚠️

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 全部命令可用 + 存在性门控 | ⚠️ | `lib/state/selectionCommands.ts:102-115`；`actions.ts:477-479` | 仅 All/Deselect/Inverse 有菜单与快捷键；**Expand/Contract/Feather/Load Layer Pixels/Mask Black Areas/Transform Selection 无任何 UI**。 |
| 色彩范围与 C 一致 + 表单 | ⚠️ | `internal/render/wand.go`（ColorRangeMask）；`tests/golden/cases/color-range.json`；`golden_test.go:50-67` | 内核 ε=0 一致 ✅；**色彩范围表单不存在**（无组件、无菜单项、无 RPC）。 |
| 变换选区不改像素 | ❌ | `selectionCommands.ts`（纯函数） | 无 ⌘T 会话接线（`actions.test.ts:164` 钉住）。 |
| 选区内剪切/复制/粘贴往返 | ✅ | `lib/state/clipboard.ts:35-74`；`actions.ts:51,307-323`；`internal/bridge/clipboard.go`；`clipboard_test.go:324` | Go 侧 CopySelection/CutSelection/Paste/LayerViaCopy 齐全。 |

**接线缺口**：色彩范围表单与 Go 通路、扩展/收缩/羽化入口、变换选区会话。

## 17 — 变换系统 ❌

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 非破坏保留原分辨率 | ⚠️ | `lib/state/transformSession.ts:1055` + `transformSession.test.ts` | 纯函数级：73 测试全绿。 |
| 自由扭曲 / 锁轴 / 整组变换 | ⚠️ | 同上 | 纯函数级。 |
| 数值输入与方向键同量纲 | ⚠️ | 同上 | 纯函数级。 |
| 吸附提示 + Flip Canvas/Layer | ⚠️ | `transformSession.ts`（flip、snap hint） | 纯函数级。 |

**接线缺口（严重）**：`transformSession.ts` **零生产消费者**（全仓 grep 只命中自身与测试）。无布局控件、无 ⌘T 入口、无 Go RPC；`actions.test.ts:160-165` 显式钉住。更糟的是**默认工具 move 被当作画笔**：`tools.ts:33` `activeTool = "move"` + `CanvasSurface.svelte:379,474`（HEAD）→ 用户一进画布拖拽就画黑笔（`CanvasSurface.svelte:377` 注释自认 "no canvas transform is wired yet"）。

## 18 — 裁剪 ❌

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 裁剪计算 / 比例 / 对称语义 | ⚠️ | `lib/state/crop.ts`（`snapCropRect:362`、`offsetGuides:463`）；`crop.test.ts` | 43 测试全绿，纯函数级。 |
| 选区起点裁剪 + 吸附 | ⚠️ | `crop.ts:308-329` | 纯函数级。 |
| 提交后文档尺寸/manifest 正确且可撤销 | ❌ | 无 Crop RPC（服务方法清单里没有） | 无法从 UI 触达；`crop.ts` **零生产消费者**。 |

**接线缺口**：裁剪工具的画布交互、Go 提交路径、与 history 的接线。

## 19 — 吸管/色板/颜色选择器/数值控件 ⚠️

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 吸管取样与像素一致；取样环开/关 | ❌ | `lib/state/color.ts:180`（samplePixel） | `samplePixel` **只被测试调用**；取样环（showsSampleRing）全仓 0 命中；吸管工具无画布分支（`CanvasSurface` 里 `eyedropperClick` 是**色阶吸管**，非 T13 吸管工具）。 |
| HSV↔RGB 往返 | ✅ | `color.ts`；`color.test.ts` | 灰轴保 hue、纯黑保 s。 |
| 数值控件三输入收敛 | ⚠️ | `lib/components/numeric.ts:65,100`；`numeric.test.ts` | 实现与测试都在，**`numeric.ts` 零生产消费者**（各表单仍用原生 input）。 |

**接线缺口**：吸管工具、取样环覆盖层、scrub/stepper 组件的实际使用。

## 20 — 图层操作命令族与图层面板 ⚠️

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 面板操作接入撤销且操作名正确 | ✅ | `lib/components/LayersPanel.svelte:42,83-85,135,143-153`；`lib/state/layerOps.ts`；`document.ts`（LayerOp RPC） | 改名/可见性/不透明度/混合模式走 Go。 |
| 合并族三上下文与 CPU 合成一致 | ✅ | `internal/bridge/layer_commands.go`；`layer_commands_test.go`；`feature-parity.md:113` | render.Render 参与层合成。 |
| 重排/嵌套/重命名/可见性…即时反映 | ⚠️ | `LayersPanel.svelte:69-78` 只有 `ondragstart`（跨标签复制） | **面板内拖放重排无 ondrop** → 重排/嵌套拖拽未实现。 |
| 右键菜单随选中项变化 | ❌ | `layerOps.ts:1081`（menuTitle）纯函数；`LayersPanel.svelte` 无 oncontextmenu | 无图层右键菜单；MenuBar 是顶部菜单。 |

**接线缺口**：面板内拖放重排、图层右键菜单、**蒙版行与特效行**（`LayersPanel.svelte` 里 mask/effect 0 命中，与 `feature-parity.md:115` 的 ◐ 一致）。

## 21 — 笔刷引擎核心 ⚠️

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 硬度/间距/平滑/不透明度累积 | ✅ | `internal/render/brush.go`；`brush_test.go:48-482`（14 测试） | 含 falloff k=2.5、spacing 下限、累积封顶。 |
| 抬笔像素与预览逐位一致 | ✅ | `brush_test.go:389`；`internal/bridge/workspace.go:531` | EndStroke 提交 + rev bump。 |
| 未动瓦片共享 + 撤销恢复 | ✅ | `brush_test.go:356`；`workspace.go:377-383` | unsafe.SliceData 共享断言。 |
| 瓦片边界无缝 | ✅ | `brush_test.go:177` | — |

**接线缺口**：桥接虽通，但**笔刷参数被硬编码**——`internal/bridge/brush.go:11-22` `defaultBrushSettings()`（直径 30、硬度 0.5、黑色、不透明 1、无平滑、**无擦除**）。`tools.ts:98-107` 的 changeBrushSize/Hardness 只写 `healSettings`（只有污点修复读它），所以 [ / ] 对画笔**没有任何可见效果**；无笔刷选项栏。

## 22 — WGSL 笔刷覆盖与延迟管线 ❌

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 4K 回显 <16ms | ⚠️ | `internal/render/brush_test.go`（4K 计时）；`feature-parity.md:218` | 只有**后端**计时（median 1.5–2.0ms），不含显示；`brushLatency.ts` 的 120 帧协议无执行体。 |
| 临时尾/永久缓冲切换、抬笔一致 | ⚠️ | `lib/gpu/shaders/brush.ts`、`lib/gpu/brushLatency.ts` | 纯库 + 测试，**零生产消费者**。 |
| 笔刷圈光标与右键 HUD | ❌ | `lib/gpu/brushCursor.ts`（无消费者）；`CanvasSurface.svelte:550`（HEAD，onContextMenu 仅 preventDefault） | 右键拖拽调大小 HUD 不存在。 |
| 笔触后特效/预览即时重建 | ❌ | — | 特效轨道整体不存在（见票 12）。 |

**接线缺口**：GPU 笔刷覆盖层、光标圈、右键 HUD；当前画笔走 Go 位图往返。

## 23 — 橡皮/仿制图章/填充命令 ❌

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 仿制图章对齐/取样源语义 | ⚠️ | `internal/render/stampfill.go`；`stampfill_test.go:53-321` | Go 实现 + 13 测试齐备，**无 RPC**（服务方法清单无 CloneStamp），画布无 cloneStamp 分支。 |
| 填充/清除仅影响选区内像素 | ⚠️ | `stampfill.go`（FillColor）；`stampfill_test.go:379,408` | 函数级正确，**无 RPC**；`actions.ts:157-159` 未接线。 |
| 全部操作可撤销 | ❌ | `stampfill.go` 注释「调用方快照入 history」 | 没有调用方 → 无可撤销路径。 |
| 橡皮选项头 | ❌ | `internal/bridge/brush.go:11-22`（`Erase:false` 硬编码） | 无选项栏、无擦除开关。 |

**接线缺口**：`actions.test.ts:157-159` 明确记录「票 23 的填充命令还没接到 Go（没有 fill RPC）」；仿制图章同样整条链路缺失。

## 24 — 涂抹与液化 ❌

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 三模式交互与 R 键轮转 | ❌ | `internal/render/smudge.go`（CycleWarpMode）；`smudge_test.go:64` | 轮转是 Go 纯函数；**无模式 store**，`actions.test.ts:174` 钉住 "Cycle tool mode" 未接线。 |
| 液化偏移场不累积模糊 | ✅ | `smudge.go`；`smudge_test.go:79,109,140` | Go 侧算法达标。 |
| 抬笔回读一致、可撤销 | ❌ | 无 Smudge RPC | 无桥接、无回读路径。 |

**接线缺口**：整条 UI/桥接链路；只有 Go 内核 + 10 个测试。

## 25 — 渐变与形状工具 ⚠️

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 渐变层可再编辑 | ⚠️ | `internal/render/gradient.go`（GradientFill 线性/径向 + 多色标 + 选区 mask） | 票内已论证 .comp v11 **无渐变 manifest 记录**，故做成无状态原语；**无工具交互、无 RPC**。 |
| 形状为可编辑样式对象；大半径 = 胶囊 | ✅ | `internal/render/shapelayer.go`；`composite.go:203-209`；`internal/layerrender/shape.go` | ShapeStyle 优先于 imageFile 重绘。 |
| 两类图层参与合成 | ⚠️ | `composite.go:203`（形状 ✅） | 渐变原语**未被 composite 调用**（只作为提交期栅格化原语存在）。 |

**接线缺口**：渐变工具拖拽会话、形状工具创建会话与 RPC；`actions.test.ts:173-175` 钉住 "Cycle shape kind" 未接线。

## 26 — 调整内核 A：色阶/曲线/色相饱和度 ✅

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| levels_apply / levels_histogram / cube_apply 与 C 一致 | ✅ | `internal/render/lut.go`、`cube.go`、`apply.go`、`histogram.go`；`golden_test.go:27-70` | 逐位一致。 |
| 色阶自动 + 三吸管计算 | ✅ | `histogram.go`（AutoLevels）；`internal/bridge/filter_session.go`；`FilterDialog.svelte:78,165` | 已接 UI。 |
| 色相/饱和度分区/分段 | ✅ | `cube.go`（HueBand / hueResponse）；`cube_test.go` | — |
| 直方图数据可供 UI 消费 | ✅ | `BridgeService.LayerHistogram()` ← `FilterDialog.svelte:92` | 已接。 |

**接线缺口**：无。

## 27 — 调整内核 B ⚠️

| 验收项 | 状态 | 证据 | 说明 |
| --- | --- | --- | --- |
| 七个内核全部与 C 黄金基准一致 | ⚠️ | `internal/render/kernels.go:76,144,185,282,324`；`golden_test.go:83-110`（grain/black_white/color_balance/noise 逐位） | 曝光/渐变映射在票 10/26 已逐位 ✅；**Invert 无 C 参照内核**（`tests/golden/c/` 无 invert，真值是 PixelInvert.swift）→ 该项措辞过强。 |
| 颗粒/杂色位置 + 种子确定性 | ✅ | `kernels.go:76,144`；`golden_test.go:83,107` | 哈希场确定性。 |
| 接入渲染真值与 GPU | ⚠️ | `internal/render/adjustment.go:27-107`（CPU dispatch 全接） | CPU ✅；**GPU 应用无消费者**（见票 11/12）。 |

**接线缺口**：GPU 调整 pass；Invert 缺黄金基准（建议补 C 侧，或明确登记为「以 Swift 为真值」）。

---

## 专项 A：⌘/Meta 语义是否泄漏到 Windows 按键绑定

**结论：按键绑定路径干净；发现 1 处「用户可见但当前未渲染」的遗留 ⌘ 文案（潜在风险）。**

- ✅ 正确：`lib/shortcuts/chords.ts:109-115` `modifiersOf` 把 `ctrlKey || metaKey` 都映到 `MOD_COMMAND`；`chords.ts:137-142` 标签表把 `MOD_COMMAND` 显示为 **"Ctrl"**（注释「原版 ⌘ → Windows Ctrl」）。`settings.ts:47-54` 的保留键集合已换成 Windows 实际保留键（Alt+F4 / Ctrl+Esc / …）。
- ✅ 正确：`lib/state/modifiers.ts:131-132` `MetaLeft/MetaRight → Modifier.Ctrl`。
- ✅ 各处 `metaKey` 用法（`CanvasSurface.svelte:324` 等）都是「接受 Ctrl 或 Win 键」的**放宽**，不是把 Ctrl 硬绑成 Meta。
- ⚠️ **潜在泄漏**：`lib/state/filters.ts:52-54` 把 `placeholder: "⌘M"/"⌘L"/"⌘U"` 写进**图像菜单的 hint 数据**；`MenuBar.svelte:287` 在查不到快捷键定义时直接用它当 `hint`，`MenuBar.svelte:501-503` 渲染成用户可见文本。当前 Curves/Levels/Hue 都在 `table.ts:79-81` 有定义，所以走 hintFor(definition)（显示 Ctrl+M/L/U），**暂时看不到 ⌘**；但一旦定义表改名/被删，菜单会直接给用户显示 ⌘M —— 属于死数据，建议删除或改走 hintFor。
- 其余 79 处 ⌘ 全部位于中文代码注释，属参考说明，不构成泄漏。

## 专项 B：`docs/feature-parity.md` 归属 01–27 的行 vs 代码

**总体：矩阵对 01–27 的成果系统性低估（大片 ☐ 实际已有实现）。** 逐行核对（括号为矩阵行号）：

| 行 | 矩阵状态 | 代码事实 | 判定 |
| --- | --- | --- | --- |
| C10 Undo/Redo 带操作名 (47) | ☐ | `history.go:113,122` + `actions.ts:106-113` + 菜单 | 应至少 ◐ |
| C15/C16 Fill/Clear (52-53) | ☐ | `stampfill.go` 有 Go 实现，无 RPC | 与代码相符（应注明「内核已就绪」） |
| C19 Pixel Grid (61) | ☐ | `CanvasSurface.svelte:289,605` | 应 ☑ |
| C20/C22/C23/C24/C25 (62-67) | ☐ | 纯函数在，UI 缺 | 与代码相符 |
| C26/C27/C29/C30/C31 (73-78) | ☐ | `selectionCommands.ts` 5 条纯函数，仅 3 条接线 | 应 ◐ 并注明 |
| C36 Flip Canvas (88) | ☐ | `transformSession.ts` flip（无消费者） | 相符（未接线） |
| C43/C44 New Adjustment Layer / Edit (105-106) | ☐ | `internal/bridge` 无 addAdjustmentLayer op | 相符 |
| C45 Transform (107) | ☐ | `transformSession.ts` 无入口 | 相符 |
| C52 Flip Layer (114) | ☐ | 同上 | 相符 |
| E01–E06 特效 (152-157) | ☐ | 只有 GPU 纯库，无 CPU 真值、无 UI | 相符（但票 12 标 done → 票与矩阵矛盾，以矩阵为准） |
| A01–A05 (121-125) | ◐ | `lut.go` / `cube.go` / `adjustment.go` 全就位 | 应 ☑（票 10/26 已 done） |
| A06/A07/A10/A11/A12 (126,127,130,131,132) | ☐ | `kernels.go:76,144,185,282,324` + 黄金逐位 | 应 ☑（票 27 已 done）→ **矩阵明显滞后** |
| L01/L02/L03/L05/L07 (163-167,169) | ☐ | `domain/*`、`render/blend.go`、`render/composite.go`、`history/*` 全部就位并接线 | 应 ☑ → 滞后 |
| I07 .comp v11 读写 (203) | ☐ | `internal/project/*` 全套 + 测试 | 应 ☑（除真实夹具项） |
| I08 后台写 (204) | ☐ | `project_io.go` 无 goroutine / 后台写 | 与代码相符 |
| P06 深色 UI / 多标签 (230) | ☐ | `app.css` + `TabStrip` 已 done | 应 ☑ → 滞后 |
| S01–S04 (176-179) | ☐ | `wand.go` / `selectionCommands.ts` 纯函数 + 黄金 | 应 ◐ |
| T01–T16 (10-25) | 全 ☐ | marquee/lasso/wand/crop/gradient/shape/eyedropper/cloneStamp/blur 工具全部**无画布分支** | 与代码相符（其「内核」可另注 ◐） |
| V01/V02/V07 (210,211,216) | ☐ | `viewport.ts` / `cursors.ts` 已接 CanvasSurface | 应 ☑ → 滞后 |
| V03/V04/V05/V06/V08 (212-215,217) | ☐ | 参考线/吸附/数值控件/Lanczos/笔刷 HUD 全缺或未接 | 与代码相符 |

---

## 最严重的 3 个缺口（供 Lead 决策）

1. **`frontend/src/lib/gpu/` 全部产物没有任何生产消费者** —— 票 11/12/22 的「GPU 合成器、特效通道、笔刷覆盖层」在仓库里只有库 + 自测（7 个 `*.test.ts`），生产代码 grep 不到一次 `lib/gpu` 导入；`lib/gpu/README.md:37-39` 承诺「CanvasSurface 持有 WebGpuCompositor」未兑现。票 11/12/22 的一切「与 CPU 真值一致 / 60fps / <16ms 回显」验收都建立在**不存在的调用链**上。
2. **默认工具 move 被当成画笔，且变换系统零接线** —— `tools.ts:33`（activeTool="move"）+ `CanvasSurface.svelte:379,474`（HEAD，PAINT_TOOLS = {move, brush}）；`CanvasSurface.svelte:377` 注释自认 "no canvas transform is wired yet"。结果：进入应用后左键拖拽即画黑笔，而 `lib/state/transformSession.ts`（票 17 全部产物）**零消费者**。
3. **八条「有实现、无通路」的工具/命令链** —— Go 侧已有实现与测试但桥接层没有 RPC、UI 也没有入口：填充/清除（`internal/render/stampfill.go` ↔ `actions.test.ts:157-159`）、仿制图章（`stampfill.go`）、涂抹/液化（`smudge.go`）、渐变（`gradient.go`）、裁剪（`crop.ts`）、变换选区（`selectionCommands.ts`）、扩展/收缩/羽化、色彩范围表单。配套还有 `numeric.ts`、`crop.ts`、`transformSession.ts`、`samplePixel`、`brushCursor.ts`、`brushLatency.ts` 六个「孤儿模块」。

**次级（值得单独列条）**：票 14 的参考线/视图开关在审计时只到纯函数层（`actions.test.ts:166-171` 钉住 5 条未接线视图命令）；票 20 的图层面板缺拖放重排、右键菜单、蒙版/特效行；票 21 的笔刷参数在 `internal/bridge/brush.go:11-22` 硬编码导致 [ / ] 无效。
