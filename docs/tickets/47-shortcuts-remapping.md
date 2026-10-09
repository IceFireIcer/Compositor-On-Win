# 47 — 快捷键系统与重映射

**What to build:** 快捷键基础设施：~114 条定义表移植（Cmd→Ctrl 系统映射、修饰键位掩码）、冲突校验（保留键位）、录制器控件、重映射表单（KeyboardShortcutsSheet）、覆盖持久化；画布/文本/菜单三域分发语义（对应原版 menu/canvasEvent/textEvent 分流）。

**Blocked by:** 13 — 视口与画布导航.

**Status:** done（2026-10-10）

- [x] 定义表与原版 114 条一一对应（表驱动测试）
      —— 实测原版表是 **113 条**（Swift KeyboardShortcuts.swift:75-125：46 Menus + 58 Canvas &
      Layers + 9 Text Editing），票面的"~114"是约数；表驱动测试逐条誊写三方组（id/key/modifiers）。
- [x] 重映射 UI：录制、冲突提示、恢复默认、持久化
- [x] 三域分流：文本框内不抢画布快捷键（BlendShortcutTests 语义）
- [x] **底层命令已存在的全部接线**（原票面写"全部工具/命令挂接"；见下方"依赖登记"——其余 24 条
      的底层命令本身属于尚未完成的票，字面意义上的"全部"在本票内不可能完成，已逐条登记归属）

对等矩阵：P01、C14、故事 52。

## 实现说明（2026-10-10）

**定义表与和弦（`lib/shortcuts/table.ts`、`chords.ts`）。** 113 条定义按原版顺序（Menus 46、
Canvas & Layers 58、Text Editing 9），id 沿用 `"Group:Title"`，键名沿用原版控制字符（Delete
\u{7f}、Return \r、Esc \u{1b}、Tab \t、Space、方向键 \u{f700}…\u{f703}）；位掩码沿用原版
稳定位序（Command/Primary=1 → Windows Ctrl、Option/Alt=2、Control=4（保留位，录制器不置位）、
Shift=8）。`table.test.ts` 的期望表是从 Swift 源码**手工誊写**的 113 条三元组，不从 table.ts
生成。

**冲突校验与持久化（`settings.ts`）。** 逐条移植原版 `problem(in:)` 的顺序：单键 + 位掩码
范围 → 文本编辑组需要 Ctrl/Alt/Control 之一 → 保留键（Windows 集合：Alt+F4/Alt+Tab/Ctrl+Esc/
Alt+Esc/Ctrl+Shift+Esc/Ctrl+Alt+Del，其中 Alt+F4 的 "F4" 先被单键规则拦下）→ 重复分配；
`localStorage["keyboardShortcuts.v1"]`，坏 JSON 或校验不过的整份丢弃（默认值胜出），保存前
再校验一次。17 个测试（settings.test.ts）。

**重映射表单（`KeyboardShortcutsSheet.svelte`）。** 三分组、搜索、逐行录制按钮（捕获阶段监听，
录制中 Enter/Esc 归录制器；纯修饰键不录、非单键闪红），保存时才落盘，恢复默认只清草稿（原版
`draft = [:]`），Esc 取消、Enter 保存，改动标记与问题提示即时显示。菜单入口在 编辑 ▸
键盘快捷键…。

**三域分流（`dispatch.ts` + 22 个测试）。** menu（Menus 池，等价 AppKit key equivalent）/
canvas（Canvas & Layers 池 + 改配走旧键的吞掉语义 + 仅 Shift 和弦落到无修饰键定义上的兜底）/
text（Text Editing 池 + 原版 Escape 那条，只在文字会话打开时消费）。抑制规则：焦点在
INPUT/TEXTAREA/contentEditable（含影子 DOM 上溯）或文字会话打开时，菜单与画布池全部放行；
模态表单（滤镜/几何/导出/RAW/关闭确认/快捷键表单，以及注册进去的新建画布表单与展开的菜单）
打开时画布池让位、菜单池照常。未注册动作不抢键（否则 Tab 焦点遍历、方向键滚动会被吞），
已注册动作一律 preventDefault；keyup 只处理按住型条目（空格临时抓手）。

**动作面（`actions.ts`，89 条注册 / 113 条定义，24 条按依赖登记未接）。** 菜单命令（撤销重做/新建/打开/保存/导出/关闭/缩放/
反相/主体选择/调整/几何/图层命令族）、工具字母全部 17 个、色板交换与重置、混合模式循环
（走 LayerOp，可撤销，符合 BlendShortcutTests）、数字键不透明度（两键拼两位数，0.6s 窗口）、
方向键 Nudge（±1/±10 px）、删除、文字命令 9 条、内容感知填充、笔刷大小/硬度。
菜单栏新增 图层（C46–C53）与 视图（C18）两个菜单，提示文字直接读 `chordFor + chordLabel`，
改键后菜单即时更新；图层菜单的标题与可用性来自已测试的纯函数
（`state/layerOps.ts: mergePlan/canUngroupLayers`），菜单/快捷键/Go 三处不会各说各话。

**代价：Go 命令族（`internal/bridge/layer_commands.go`，1409 行实现 + 1044 行测试）。**
⌘J/⌘G/⇧⌘G/⌥⌘G/⌘E/⌫/方向键原先只有前端纯函数，`planLayerOp` 没有 case——"接线"只到名字层，
按下必得"未知操作"。本票补齐 8 个 op：`duplicateLayer`（含子树与剪贴链重映射、位图独立拷贝）、
`groupLayers`（同级收进新文件夹，跨文件夹取共同祖先）、`ungroupLayer`（就地展开并释放失效
剪贴）、`setClippingMask`（建/解，共享基底链）、`deleteLayerMask`、`deleteEffect`、
`nudgeLayer`（改 transform.origin）、`mergeLayers`（Merge Down / Merge Group / Merge
Layers 三上下文，用 render.Render 合成参与层、被移除层的剪贴重新指向结果层、一步可撤销）。
32 个 Go 测试，每个 op 都有"校验失败不改文档"的用例。

**防"假接线"的契约测试（`internal/bridge/layerop_contract_test.go`）。** 从
`document.ts/filters.ts/actions.ts/LayersPanel.svelte` 里抓 `runLayerOp("…")` 的字面量，与
`planLayerOp` 的 case 名单双向比对：前端发了 Go 不认的 op、或 Go 有没人用的 op，都会红。
放在 Go 侧是因为 CI 与本机都跑 go test，且不需要 node 类型。

## 依赖登记：仍未接线的 24 条（每条都因为底层命令还没落地）

| 定义 | 归属 |
| --- | --- |
| Cut / Copy / Paste（⌘X/C/V） | 票 48（`state/selectionCommands.ts` 的纯函数在前端拿不到图层像素；`winclip` 只有 PutBitmap） |
| Fill with Foreground / Background（⌥⌫ / ⌘⌫） | 无票（C15/C16 ☐）：引擎 `render.FillColor/FillForeground` 已在，缺 bridge RPC + 历史包装 |
| Transform Layer / Selection（⌘T） | 票 17（`transformSession.ts` 会话只有纯函数，Go 无 setTransform，画布未接交互） |
| Apply current canvas operation（⏎） | 票 17/18（crop/transform 会话没有画布入口；crop 提交可复用 CanvasSize 的 offset） |
| Show Grid / Guides / Rulers / Snap / Lock Guides | 票 14（只有 snap.ts/guides.ts 纯函数；画布不渲染参考线/标尺，没有开关 store） |
| Toggle Levels preview（⌥P） | 票 33/31（FilterSession 没有 preview 字段，Go 的预览开关也没暴露） |
| Cycle tool mode（Tab） | 票 15/21（没有任何工具 mode 状态） |
| Cycle shape kind（⇧U） | 票 25（`shapeKind` 状态与形状会话/端点都缺） |
| Move selected pixels ×8（⌘←/…） | 票 16（S05 浮动选区 ☐；Go 无像素移位原语） |
| Hide Compositor（⌥⌘H） | 不适用：macOS 专属（feature-parity「明确不做」第 4 条）；若要等价物应登记到票 49 的窗口区 |

`actions.test.ts` 把这 24 条**逐条钉住**（多一条少一条都会失败），所以"未接线的就是这份清单"
是测试保证的，不是注释承诺。

## 已知差异 / 留待后续

1. 键盘可达性优先于原版：表里尚未实现的动作不抢键（原版在 AppKit 里吞掉画布键是为了避免系统
   提示音，浏览器里没有这个代价，吞掉 Tab/方向键反而会破坏键盘导航）。
2. 原版 `ShortcutChord.event(like:)`（把和弦合成 NSEvent 去触发菜单）在浏览器里没有对应物，
   改成动作表直调。
3. 数字键不透明度落在活动图层（`setOpacity`）而不是工具不透明度：前端还没有笔刷/工具不透明度
   store（票 21 尾项），Photoshop 在移动/选择工具激活时同样作用于图层。
4. 笔刷大小/硬度的步长语义是原版的（20% 步长 / 25% 硬度档），但设置落在 `healSettings`：
   污点修复画笔（J）会真的带进 Go；画笔/橡皮的参数仍是 Go 侧固定值，等笔刷设置 RPC 落地后共用
   同一个 store。
5. "Merge Layers"（多选合并）的上下文目前从 UI 走不到：Go 文档只有 `activeLayerID`（多选集合
   是前端 store 的事），所以 ⌘E 现在只会是 Merge Down / Merge Group。Go 的 `mergeLayers`
   已接受可选 `ids`，等图层面板的多选落地后前端补一行即可。
