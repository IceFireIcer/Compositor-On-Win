# 57 — 新建画布物理单位与底色

**What to build:** 用户在新建画布时可输入像素、英寸、厘米或毫米，选 72/300 DPI 与透明/白/黑底，预览最终像素尺寸。

**Blocked by:** 02 — 编辑器外壳 UI 与新建画布; 04 — 文档域模型与限额。

**Status:** done（2026-10-13）

- [x] 物理单位按分辨率换算为整数像素，切换单位保留实际画布尺寸；本地化小数输入和限额校验一致。
- [x] 白底、黑底创建可编辑的 Background 图层；透明底保持空白图层。
- [x] 分辨率写入项目并进入导出元数据；新建默认仍是透明、像素、72 DPI。
- [x] 覆盖原版 `NewCanvasUnitTests` 的单位往返和底色像素断言。

对等矩阵：U04。原版锚点：`NewCanvasSheet.swift`、`NewCanvasUnitTests.swift`；提交 `b0b4f01`、`ab9779a`、`a00d8b6`。

## 实现说明（2026-10-13）

**单位与底色的纯函数（`frontend/src/lib/state/newCanvas.ts`）。** 原版
`NewCanvasUnit` / `NewCanvasBackground`（NewCanvasSheet.swift:6-56）的逐行移植，无
Svelte 依赖：`NewCanvasUnit.inches.pixels("8.5", 300)` 这种调用形状照抄原版，所以测试
能逐条誊写 `NewCanvasUnitTests`。`pixels` 先 `trim` 再把逗号换成小数点（`"2,54"` =
2.54）、必须 > 0；像素单位必须是整数，打印单位四舍五入后落在 1…30000。`text` 对像素单位
给整数、对打印单位最多两位小数（POSIX 小数点、无千分位）。边限直接复用裁剪的
`CROP_MAX_SIDE`（DocumentLimits.maxSide = 30000），没有第二个常量。另有
`nextResolution`（72 ↔ 300）与 `pixelPreview`（"72 DPI: 1920 × 1080 像素"）。
`newCanvas.test.ts` 10 条：前 3 条逐条照抄原版（打印尺寸换算、单位往返与 pill 循环
px → in → cm → mm → px、底色循环 transparent → white → black → transparent），其余是
本地化小数、非法输入、1/30000/30001 边限、打印文本位数、DPI 循环与预览。

**表单（`frontend/src/lib/components/NewCanvasSheet.svelte`）。** props 不变
（`open` / `onclose`），单位、DPI、底色都是组件内部状态。宽/高改存字符串并按当前单位
显示；换单位时先把像素尺寸按新单位写回输入框（原版 switchUnit：换的是写法，画布不变）。
三颗 pill 点击循环（底色 / 单位 / DPI），打印单位显示像素预览，非法输入给出橙色提示并
禁用「创建」。默认仍是像素、72 DPI、透明、1920 × 1080。

**Go 侧底色（`internal/bridge/session.go`、`workspace.go`）。**
`Workspace.NewDocument(width, height, resolution int, background string)` —— 新建画布**唯一**
的公开入口：`""`/`"transparent"` 建空白「图层 1」，`"white"`/`"black"` 建不透明「Background」
层（整张画布一个颜色，白 255 / 黑 0，alpha 255），其它值报错且不落标签。命名、建会话、激活
收在 `appendDocument` 一处。

**接线（`frontend/src/lib/state/workspace.ts`、`frontend/wailsjs/go/bridge/Workspace.*`）。**
`newDocument(width, height, resolution, background = "transparent")` 多带一个底色，前端调
`Workspace.NewDocument`（4 参）。**wailsjs 的绑定与 Go 的导出方法一一对应**：集成时把原先
"前端调 `NewDocument`、让它转发到 `NewDocumentWithBackground`"的写法改掉了 —— `wails build`
会按 Go 的导出方法重新生成 `wailsjs`，转发写法在重新生成后会变成 3 参，4 参调用打到 3 参
方法上，新建画布在打包版里静默失效。票 57 之前那个建白底「背景」的 3 参入口只剩既有测试在
用，已改成不导出的 `newDocumentWithWhiteBackground`（见下），所以公开面就是一条方法。

**分辨率进 manifest 与导出元数据。** `TestWorkspaceNewDocumentResolutionReachesManifest` 用
2550 × 3300 @300 DPI 建文档 → `SaveActive` → `project.Store.Open` 断言 manifest 里
`resolution == 300`、尺寸与「图层 1」都在；`TestWorkspaceNewDocumentResolutionReachesExportMetadata`
再从同一条新建路径导出 PNG，断言 pHYs 写的是 300 DPI（11811 ppm）——导出编码本身是票 42 的
既有实现，本票只证明新建时的 DPI 确实一路走到了它。

## 已知差异 / 有意取舍

1. **透明底的图层带一张全透明位图，而不是原版的 `asset == nil`。** 原版
   `firstLayer` 对透明底只给一个没有资产的空图层（NewCanvasUnitTests 断言
   `asset == nil`）；Windows 这边的绘制目标（`resolveRasterPaintTarget`）要求图层有资产
   才能落笔，否则新建的透明画布根本画不上。所以透明底建「图层 1」+ 全 0 位图：合成结果
   与原版一致（什么都不画），但用户可以立刻落笔。测试相应断言"整张位图全 0"。
2. **既有测试的白底夹具改成了不导出的 `newDocumentWithWhiteBackground`。**
   document_test / export_test / fill_test / text_test 等既有测试按"不透明白「背景」层"这个形状
   断言，所以那份夹具保留；但它只给测试用，因此不导出（不在 Wails 绑定面上），测试调用点
   由集成时统一改名。用户可见的默认是透明，白/黑底的图层名照原版是 `Background`，夹具仍是
   既有的「背景」。
3. **透明底占掉「图层 1」这个名字。** 原版按"下一个没被用过的 Layer N"取名；这边
   `session.layerSeq` 从 1 起，随后新建的图层是「图层 2」，不会与初始图层重名。
4. **DPI 仍只在 pill 上循环 72/300**（原版如此），没有自由输入；`domain` 允许 1–9600，
   RPC 不限制这两个值。
5. 单位切换不会把非法输入"修好"：原版在像素尺寸非法时只换单位、不动输入框，这边一致。
