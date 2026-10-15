# 58 — 重做上一个滤镜

**What to build:** 用户可将最近一次成功应用的滤镜及其参数直接再次应用于当前可编辑图层。

**Blocked by:** 32 — 滤镜菜单核心; 33 — 滤镜实时预览管线; 47 — 快捷键系统与重映射。

**Status:** done（2026-10-14）

- [x] Filter 菜单提供 Last Filter；原版最终为 Control+Command+F，而搜索为 Command+F。Windows 映射须指定两个不同的和弦并可重映射，不能把两个原版修饰键折叠成同一个 Ctrl。
- [x] 只记住成功提交且可重复的滤镜；内容感知填充和图像调整不成为 Last Filter。重做沿用参数和渐进模糊定位，不弹预览面板。
- [x] 无上次滤镜、会话冲突时命令禁用；失败不留下悬挂预览。**「不可编辑图层」只由 Go 侧拒绝**——前端没有图层可编辑性判据（票面自己的已知差异 2），所以这一条是部分满足。
- [x] 每次重做可单独撤销，输出与手动使用相同参数一致。

对等矩阵：U05。原版锚点：`Filters.swift`、`CompositorApp.swift`；提交 `0415aa4`、`6b81d4a`。

## 实现说明（2026-10-14）

**记住什么（`frontend/src/lib/state/lastFilter.ts`，新建）。** 原版
`EditorSession.lastFilter` 与 `FilterKind.repeatsAsLastFilter`（Filters.swift:28-29、
:866）的移植：只有**成功提交**的滤镜会被记下，且只有 Filter 菜单自己的那些 ——
`repeatsAsLastFilter(kind) = kind !== "contentAwareFill" && !kind.startsWith("adjust:")`，
所以内容感知填充（走 ApplyFilter）与图像调整（`adjust:*` 会话）都到不了这里。记录里带
kind / 菜单标签 / destructive / **提交时的完整参数对象**，重做时原样带上 —— 原版特意保留
的渐进模糊定位（progressiveStart/End）就是靠"参数原样"这一条。记录只活在内存里（原版也是
会话状态，不落盘）。`repeatAllowed(record, hasLayer, sessionOpen)` 是菜单与动作共用的
可用性判据（原版 canRepeatLastFilter），`lastFilterLabel` 对应原版
`"Last Filter: " + kind.rawValue`。

**重做（`frontend/src/lib/state/filters.ts` 的 `repeatLastFilter`）。** 刻意**不复用**
`beginFilter`/`commitFilter`：那两个会写 `filterSession` store，而 App 是按这个 store 挂
FilterDialog / CameraRawPanel 的 —— 重做必须不弹面板。它直接调同一对 RPC
（`BeginFilterEdit` → `CommitFilter`，同一份带 `filterSession:{tabId,seed,requestId}` 的
参数 JSON），走同一条 Go 提交路径，所以"每次重做是一次独立提交、各自可撤销、输出与手动用
同样参数一致"是结构保证而不是巧合。参数里的两项预览专用字段（Option 拖拽的剪贴视图
`clipping`、锐化蒙版叠加 `sharpenMask`）提交前清掉。失败时调 `CancelFilterSession`
收掉 Go 会话，绝不留下悬挂预览（原版 `repeatLastFilter` 结尾的 `cancelFilter`）。跨标签的
迟到回复整条丢弃（与 filters.ts 既有的 `applySessionSnapshot` 同一守卫）。

**接线。** 定义表按原版顺序在 `Select Subject` 与 `Curves` 之间加 `Menus:Last Filter`；
Filter 菜单在滤镜列表**之前**加这一条，标题跟着上次滤镜改名（「重做上一个滤镜：晕影」），
没有上次滤镜 / 图层不可编辑 / 有滤镜会话或对话框开着时禁用。动作表注册
`Menus:Last Filter` → `repeatLastFilter()`（不可用时它自己返回 false，快捷键路径同样安全）。
命令搜索面板会自动收录这条菜单项（票 54 的同源菜单模型）。

**和弦映射（票面的硬要求）。** 原版是 ⌃⌘F（modifiers 5 = Control|Command），但 Windows 的
位序里 bit 4（Control）与 bit 1（Command）**都**渲染成 Ctrl —— 直接照抄会得到
"Ctrl+Ctrl+F"。票面要求两个原版修饰键映射成两个不同的和弦，所以表里取
`Command|Option` = **Ctrl+Alt+F**（搜索仍是 Ctrl+F）；两者在画布/菜单池里互不遮蔽，且都可
在「键盘快捷键…」里重映射（录制器从不置 bit 4，所以改键不会写出折叠的和弦）。

## 测试

- `state/lastFilter.test.ts`（6）：可重复 kind 全表；内容感知填充/图像调整不算；提交才记录且
  记下参数本身（同一对象引用）；不可重复的提交不覆盖已有的；可用性判据四种输入；菜单标题改名。
- `state/repeatLastFilter.test.ts`（5）：没有上次滤镜什么都不做；按原参数重跑（BeginFilterEdit
  收到 kind/layerId/参数/seed，CommitFilter 收到**同一份** JSON，参数里的预览专用字段被清掉，
  `filterSession` store 保持 null —— 不弹面板）；`adjust:*` 不重做；提交失败时
  `CancelFilterSession(tabId, seed)` 且文档不变；会话冲突时不重做。
- `shortcuts/table.test.ts`（8）：117 条、49/59/9（票 59 的 Export As 也在同一轮加进表里），
  逐条三元组（含 Last Filter 的 3）。
- `shortcuts/actions.test.ts`（39）：关键条目含 `Menus:Last Filter`；Ctrl+Alt+F 被菜单池消费、
  不开命令面板、不进纯画布，Ctrl+F 仍是搜索。
- 集成后全量 `npx vitest run` 与 `npm run check` 见提交说明。

## 已知差异 / 留待后续

1. **和弦是 Windows 映射**：⌃⌘F → Ctrl+Alt+F（理由见上）。原版表里的 5 在
   `table.test.ts` 的誊写期望里写成 3，并在注释里说明 —— 这是本票唯一一处**有意偏离**
   原版数值的地方，票面明确要求如此。
2. **"不可编辑图层"判据**：菜单/动作只判"有活动图层 + 没有滤镜会话/对话框"（与 Filter 菜单
   其余条目同一套），真正的可编辑性由 Go 拒绝（既有行为）。原版 `canRepeatLastFilter` 还区分
   晕影需要 `canVignette`，Windows 侧没有对应状态。
3. **渐进模糊定位**：本票只保证"参数原样带回"；Progressive Blur 滤镜本身由票 73 落地，届时
   它的位置参数会自然被带上（记录的是提交时的整个参数对象）。
4. 记录不落盘：重启应用后没有"上次滤镜"（与原版 `EditorSession.lastFilter` 一致）。
