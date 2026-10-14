# 61 — Navigator 导航缩略图

**What to build:** 用户在高倍率查看画布时，可通过 Navigator 缩略图观察全图并拖动视口框快速定位。

**Blocked by:** 13 — 视口与画布导航; 09 — CPU 合成器真值。

**Status:** done（2026-10-14）

- [x] View 菜单可切换 Navigator；默认关闭且偏好可持久化，放大到 300% 及以上时在画布角落显示缩略图与当前视口框。
- [x] 缩略图包含实时图层、蒙版、剪贴和调整结果；文档变更后刷新，计算与缓存有界。
- [x] 点击或拖拽缩略图平移主画布，文档边缘及旋转/缩放时映射正确。
- [x] 窄窗口和右侧图层面板不互相遮挡；对照原版 `NavigatorTests`。

对等矩阵：U08。原版锚点：`NavigatorMinimap.swift`、`NavigatorGeometry.swift`、`NavigatorTests.swift`；提交 `6490bb3`、`3e09948`。

## 实现说明（2026-10-14）

**纯几何（`lib/state/navigator.ts`，新建）。** 逐条移植 `NavigatorGeometry.swift`（52 行）与
`NavigatorMinimap` 的三个常量，坐标空间沿用 `viewport.ts`（文档像素 / CSS 像素，左上原点）：

| 本仓库 | 原版 |
| --- | --- |
| `NAVIGATOR_LARGEST` 126 × 90、`NAVIGATOR_PICTURE_PIXELS` 300、`NAVIGATOR_ZOOM_SHOWN` 3 | `NavigatorMinimap.largest` / `picturePixels` / `zoomShown` |
| `navigatorSize(documentSize)` | `NavigatorMinimap.size(for:)`（等比放进 largest、两边 `.rounded()`） |
| `navigatorImageRect(g)` / `navigatorScale(g)` | `NavigatorGeometry.imageRect` / 私有 `scale` |
| `navigatorThumbnailRect(g, rect)` | `thumbnailRect(for:)` |
| `navigatorDocumentPoint(g, point)` | `documentPoint(for:)`（越界夹到文档边缘） |
| `intersectRects` + `navigatorViewportBox(g, visible)` | `thumbnailRect(...).intersection(imageRect)` + `isNull/isEmpty` 跳过 |
| `visibleDocumentRect(viewport, w, h)` | `CanvasViewport.visibleDocumentRect` |
| `centerView(viewport, point, w, h)` | `CanvasViewport.centerView(on:documentSize:)`（走 `translateBy`，于是不再跟 Fit） |
| `navigatorShown({enabled, zoom, documentWidth, documentHeight, canvasOnly})` | `ContentView.swift:114` 的四个条件 |
| `navigatorKeyboardTarget(visible, stepsX, stepsY)` | 原版没有（键盘可达性的最小补充，一格 = 可见区域 1/8） |

**组件（`lib/components/Navigator.svelte`，新建）。** 画布右上角 12px（标尺是画布内部覆盖层，有标尺时再让开
18px），盒子尺寸固定（≤ 126 × 90），绝对定位 —— 不参与画布区域的布局，所以窄窗口与右侧图层面板都挤不到它。
缩略图复用画布那张合成图：`src` 就是 CanvasSurface 的 `/render/{id}.png?v=rev-filterRev`（同一个 URL，
浏览器缓存共用），不另开 GPU 管线，也不按文档尺寸缓存 JS 位图。编辑停下 250ms 才换图（原版
`task(id: RenderKey)` 的 sleep），首张立刻画；隐藏时把图丢掉，所以只有显示着才真的取图。视口框 = 可见矩形
映到缩略图后裁进图片范围，宽度/高度 `max(3, …)`（原版 `frame(width: max(3, …))` 与 `strokeBorder`，故用
`box-sizing: border-box`）。指针按下即居中、拖动连续居中（原版 `DragGesture(minimumDistance: 0)`）；
pointerdown/move/up 与 keydown 都 `stopPropagation`，免得被画布的工具手势与窗口快捷键吃掉。纯画布模式收起
（读 `canvasOnly` store，原版 `ContentView.swift:114` 的 `!session.canvasOnly`）。

**接线（`CanvasSurface.svelte`）。** 只加挂载点：`<Navigator src={navigatorSrc} docWidth={doc.width}
docHeight={doc.height} />`，其中 `navigatorSrc` 不含 `$transformPreviewURL` —— 原版 `navigatorImage` 用"裸
session"画，进行中的预览不进缩略图。没有新增样式。

**偏好（`lib/state/view.ts`）。** `export const showsNavigator: Writable<boolean>`，默认 false，localStorage
键 `navigator`（原版是 `@AppStorage("navigator.visible")`，见「已知差异」1）。自定义 writable 的
`set`/`update` 先改内存再写盘，所以菜单、快捷键、命令面板任何一条路径拨动它都会持久化；存储被禁用或写盘
抛错时静默退回（`shortcuts/settings.ts` 同样的守卫）。view.ts 里既有的开关都是**不持久化**的裸 writable，
所以这里补了一对模块内的 `readStoredBool`/`storeBool`，没有改动其它开关。

## 测试

- `state/navigator.test.ts`（新建，21 条）：
  - `documentFitsTheBoxCentered` / `thumbnailRect` / `documentPoint` / 越界夹边 / 空文档空盒子 —— 逐条对照
    `NavigatorTests` 的数值（400 × 200 文档放进 200 × 200 盒子）。
  - `minimapFitsTheDocument`：4000 × 3000 → 120 × 90；6000 × 2000 → 126 × 42；零尺寸 → 零。
  - `visibleRectFollowsZoomAndPan`：800 × 600 点在 4× 下显示 200 × 150 像素、中心是 (200, 150)。
  - `centeringKeepsZoomAndStopsFollowingFit`：居中后可见矩形中心 = (50, 60)、缩放不变、之后 `resize` 不再重新 fit。
  - 文档边缘：居中到 (0, 0) 时可见矩形中心贴着文档左上角；可见矩形越出文档边缘时数值正确。
  - 缩略图 ↔ 文档往返：视口框中心映射回可见矩形中心；画布旋转 90°（宽高互换）后比例与映射仍正确；
    视口框裁进图片范围（显示超过整幅 → 框住整幅；完全在外/零面积相接 → 不给框）。
  - 显示条件：默认关、2.99× 不显示、3× 显示、纯画布收起、无文档不显示。
  - 键盘：方向键按 1/8 可见区域平移目标点。
  - 持久化：默认 false；`set`/`update` 写盘并能往返读回；`loadShowsNavigator`/`storeShowsNavigator` 是同一份
    读写；存储缺失、读抛错、写抛错都不抛异常（内存里仍然生效）。
- 全量 `npx vitest run`：**69 文件 / 1042 passed**（其中本票新增 21 条；同工作树还有并行票在改，总数会浮动）。
- `npm run check`：**0 errors / 0 warnings**。
- 挂载核对（无浏览器测试，见「已知差异」4）：用 `svelte/compiler` 在内存里编译两个组件核对产物选择器 ——
  Navigator.svelte 产出 `<div class="navigator svelte-xpkkqv" role="application" aria-label="导航器"
  title="点击或拖拽移动视图（方向键微调）" tabindex="0">`、`<div class="viewport-box svelte-xpkkqv">`、
  `<img class="picture svelte-xpkkqv">` 与 `.navigator.svelte-xpkkqv { position: absolute; right: 12px;
  z-index: 7; … }`，0 警告；CanvasSurface.svelte 产出 `import Navigator from "./Navigator.svelte"` 与
  `Navigator(node_23, { src: $.get(navigatorSrc), docWidth, docHeight })`，0 警告。
  坐标换算逐条读过：`documentRect`/`documentPoint`（viewport.ts）→ `visibleDocumentRect` →
  `navigatorThumbnailRect`（乘 `navigatorScale`）→ `navigatorDocumentPoint`（除 `navigatorScale` + 夹边）
  → `centerView`（`pan = 文档中心×ppp − 目标点×ppp`），与 `CanvasViewport.swift:18-51` 一一对应。

## 已知差异 / 未验证

1. **localStorage 键用 `navigator`**：票面接口契约指定用 ToolDefaults 风格的短键；原版是
   `@AppStorage("navigator.visible")`（`CompositorApp.swift:8` / `ContentView.swift:7`），ToolDefaults 自己
   的前缀是 `tool.` 且不含这一项。要跟 macOS 逐字兼容时改成 `navigator.visible` 只是一处常量。
2. **缩略图是 Go 的整幅合成 PNG，不是原版 `navigatorImage(maxSide:)`**：原版把文档按比例缩小再合成一次
   （"adjustment / clipping surface 会按画的东西取尺寸"）。Windows 侧合成在 Go，重开一条缩略图管线要么加 Go
   接口（超出本票写范围）、要么在前端复制像素不变量，所以直接复用画布那张合成图（同一 URL，缓存命中），由
   浏览器缩到 ≤ 126px。视觉等价；代价是大文档首次显示要等整幅 PNG 到达（原版等的是 300px 的小图）。
3. **原版 `NavigatorTests` 有 5 条测不了**：`pictureFitsTheLongerSideAndShowsTheComposite`、
   `smallDocumentsAreNotEnlarged`、`hugeDocumentsWithAdjustmentsStillShow`、`blursShrinkWithThePicture`、
   `selectionChangesDoNotRedrawThePicture` 测的是 CoreGraphics 合成的像素与 `RenderKey`。Windows 侧对应的是
   Go 的合成测试（票 09 覆盖 adjustment/mask/clipping），前端只负责"URL 只随 docId/rev/filterRev 变"，
   已由 `navigatorSrc` 的形状与 CanvasSurface 现有渲染测试保证。
4. **没有浏览器级测试**：挂 CanvasSurface 需要 WebGPU 适配器与一整套 Wails 桩（仓库里
   `*.browser.test.mjs` 走的是 LayersPanel / CommandPalette 这类无 WebGPU 的组件），故按票面要求不做；
   映射与挂载用上面的编译产物选择器 + 纯函数单测核对。
5. **菜单项由 Lead 接线（已完成）**：`MenuBar.svelte` 的 视图 菜单在「缩小」之后加了「导航器（300% 及以上）」
  一条（`checked: $showsNavigator`，run 里 toggle，位置对齐原版 View 组的 Navigator）。组件自己判断何时显示，
   菜单只拨开关；偏好持久化在 store 里，所以菜单/命令面板/将来的快捷键都走同一条。
6. **键盘支持是最小集**：只有方向键微调（可见区域 1/8）；导航器拿着焦点时方向键归它（拦冒泡），所以此时
   画布的图层微调快捷键不触发 —— 焦点在别处时行为不变。
