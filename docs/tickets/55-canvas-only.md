# 55 — 纯画布全屏模式

**What to build:** 用户按 F 或选菜单可在常规工作区与黑底纯画布全屏视图之间切换，退出后恢复编辑状态。

**Blocked by:** 13 — 视口与画布导航; 47 — 快捷键系统与重映射。

**Status:** done（2026-10-13）

- [x] F 切换纯画布模式，Esc 退出；输入框、对话框中按键不误触发。
- [x] 画布占满窗口、背景为黑色，工具栏与面板不遮挡画布（窗框见「已知差异」1）。
- [x] 退出后缩放、平移、工具、面板状态和未保存文档保持原样。
- [x] 与命令搜索快捷键 Ctrl+F 互不冲突（滤镜重做的 ⌘F 属票 58，尚未实现；纯 F 目前没有别处占用）。

对等矩阵：U02。原版锚点：`CompositorApp.swift`、`CompositorApplicationDelegate.swift`；提交 `ad4bddf`、`720c121`。

## 实现说明（2026-10-13）

**状态（`lib/state/canvasOnly.ts`，新建）。** `canvasOnly`（默认 false）对应原版 `EditorSession.canvasOnly`；
纯函数 `canvasOnlyAllowed(hasDocument, editingText)` 是原版 `canToggleCanvasOnly` 的核（有画布、没有正在输入的
文字），`canToggleCanvasOnly()` 与 MenuBar 的 disabled 共用这一份规则，避免两处判断漂移；`toggleCanvasOnly()`
进入前过守卫、退出不设限（文档被关掉后不该被困在纯画布），`exitCanvasOnly()` 幂等。

**快捷键（`shortcuts/table.ts`）。** 按原版 `KeyboardShortcuts.swift:116` 的顺序，在 Canvas & Layers 组开头、
`Select tool` 之前插入 `("Toggle Fullscreen: the canvas alone on black (Esc also leaves)", "f")`；Canvas 组
58 → 59、全表 114 → 115，`table.test.ts` 的誊写期望同步。裸 F 落在画布池，Ctrl+F 仍在菜单池
（`Menus:Search Commands`），两条和弦不同、互不遮蔽。

**动作（`shortcuts/actions.ts`）。** `Canvas & Layers:Toggle Fullscreen: …` → `canToggleCanvasOnly()` 守卫 +
`toggleCanvasOnly()`。Esc 沿用既有的 `Canvas & Layers:Cancel current canvas operation`：先文字会话、再变换会话；
两者都没有时纯画布模式下 `exitCanvasOnly()`（原版 `escapeHasNothingToCancel`），不在纯画布时行为与以前逐字相同
（不再多调一次 `cancelTransformSession()`，它本来就是空操作）。

**布局（`App.svelte`）。** 根 `.app` 上加 `class:canvas-only={$canvasOnly}`，只用 CSS 收起 toolbar /
ToolOptionsBar（其根元素 class 是 `.options`）/ tool-rail / right-panel / status-bar；`.app.canvas-only` 的网格
改成单行、画布区域铺满并显式 `background: #000`（不依赖 `--bg-canvas`，原版要的是"黑底上的画布"而不是主题色）。
**没有新增任何 `{#if}`**：`display: none` 不卸载组件，CanvasSurface 的 WebGPU 上下文与缩放/平移/工具/面板状态
原样保留。

标尺是 CanvasSurface 内部的覆盖层（`$showRulers`，默认开），原版 `ContentView.swift:89` 在 canvasOnly 时
一并收起，所以这里补一条 `.app.canvas-only :global(.ruler) { display: none }` —— 组件作用域的选择器点不到子组件
的类，必须用 `:global`。收起是纯显示层的事，退出后标尺照旧（状态没动）。导航器（票 61）尚未实现。

## 测试

- `state/canvasOnly.test.ts`（9）：默认关；`canvasOnlyAllowed` 四种输入；有文档进/出；无文档不能进；文字会话中
  不能进；已在纯画布时文字会话不挡退出；`exitCanvasOnly` 幂等；重复 toggle 幂等；`canToggleCanvasOnly` 与纯函数一致。
- `shortcuts/table.test.ts`（8）：115 条、47/59/9；逐条 id/key/modifiers；F 是画布池裸 F 且排在 `Select tool` 之前。
- `shortcuts/actions.test.ts`（38）：F 进/出；INPUT 里 F 不触发；几何表单打开时画布池静默；无文档时守卫拦住；
  Ctrl+F 仍开命令面板且不切纯画布；纯画布下 Esc 退出、没有纯画布时 Esc 不改状态；文字会话 / 变换会话开着时
  Esc 只取消会话、留在纯画布。
- 集成后全量 `npx vitest run`：66 文件 / 1007 passed（含票 56、57 的用例）；`npm run check` 0 错 0 警。

## 已知差异 / 未验证

1. **Wails 的窗口标题栏与边框前端改不了**：原版另开一个 borderless 覆盖窗
   （`CompositorApplicationDelegate.toggleCanvasOnly` 的 `canvasOnlyFrame`）盖住 macOS 的圆角、边缘描边与
   菜单栏/Dock，并隐藏红绿灯按钮；Windows 版只能收起页面内的栏与面板，窗口自身的标题栏、边框与任务栏仍由
   系统绘制。真正的无边框全屏要在 Wails/Go 侧改窗口样式，不在本票范围。
2. **菜单项的模态判断**：原版 `canToggleCanvasOnly` 把滤镜/调整/取色等对话框一并排除。F 走快捷键分发层时
   那些模态本来就让画布池静默（`dispatch.ts` 的 `canvasModalOpen`），行为与原版一致；菜单项的 disabled 由
   MenuBar 用 `canvasOnlyAllowed(有文档, 正在输入文字)` 加上 MenuBar 自己那份 `dialogOpen` 判定（集成时补），
   所以"对话框开着时点菜单"也不会进纯画布。
3. **没有浏览器级布局测试**：挂载 App.svelte 需要一整套 `window.go` 桩（Wails 运行时），成本高且脆，故未加。
   布局核对方式是读编译产物：用 `svelte/compiler` 编译 App.svelte，确认生成的选择器为
   `.app.canvas-only > .toolbar/.status-bar/.options`、`.app.canvas-only > .body > .tool-rail/.right-panel`、
   `.app.canvas-only > .body > .canvas-area`，且 0 警告（没有未使用选择器）。
4. **选区编辑表单打开时 Esc 不退出纯画布**：`SelectionEditSheet` 是另一层会话且有自己的取消入口，Esc 不越过它
   （原版此时 Esc 归表单所在的窗口）；它也不会被 Esc 取消 —— 这是既有行为，本票未改动。
5. **Esc 只感知全局会话**：原版 `escapeHasNothingToCancel` 把裁剪框、套索/形状/渐变草稿、参考线拖动都算进去；
   Windows 侧这些草稿是 CanvasSurface 组件内的局部状态（没有全局 store），Esc 看不到它们 —— 拖动过程中按 Esc
   会退出纯画布（拖动本身不受影响，松手照旧）。文字/变换/选区编辑三个全局会话都在守卫里。
