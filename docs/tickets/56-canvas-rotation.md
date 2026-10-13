# 56 — 画布顺逆时针旋转 90 度

**What to build:** 用户通过 Image 菜单将整个画布顺时针或逆时针旋转 90 度，并能撤销、保存和重开。

**Blocked by:** 17 — 变换系统; 43 — 画布尺寸/图像尺寸/修剪。

**Status:** done（2026-10-13）

- [x] 两个方向均交换画布宽高，并正确变换图层、蒙版、选区及参考线（选区在前端，见「已知差异」1）。
- [x] 不重采样图层像素；图层、组和独立蒙版 placement 的变换位置在导出、保存、重开后与预览一致。
- [x] 一次操作对应一次撤销；连续四次同向旋转回到原状。
- [x] 与原版 `CanvasRotationTests` 的边界及形状用例对照。

对等矩阵：U03。原版锚点：`CanvasRotation.swift`、`CanvasRotationTests.swift`；提交 `754f188`。

## 实现说明（2026-10-13）

**桥（`internal/bridge/canvas_rotate.go`，新建）。** `Service.RotateCanvas(tabID, direction)` 走
`s.ws.editTransformTab(tabID, nil, 历史名, fn)`（与 `canvas_flip.go` 同构）：一次操作恰好一条撤销，
已切换 / 已关闭 / 已重载 / 忙碌（笔刷、滤镜、修复、保存中）的会话在打开历史之前被拒；`direction` 只接受
`"clockwise"` / `"counterclockwise"`，其它值在动文档之前报中文错误。历史名「旋转画布 90°（顺时针）」
/「（逆时针）」。

**几何（`quarterTurnedTransform` / `quarterTurnedGuide` / `rotateCanvasDocument`）。** 逐行对齐
`CanvasRotation.swift`：`center = origin + size/2`，顺时针把中心送到 `(canvasH − cy, cx)`、逆时针送到
`(cy, canvasW − cx)`，原点回到 `middle − size/2`，尺寸不变、旋转角 ±90 后对 360 取余（`math.Mod`，
与原版 `truncatingRemainder` 同符号）。**只改 placement，位图指针与字节都不动**，所以不重采样、整像素仍在
整像素上；编组在 domain 里就是扁平列表的普通行，遍历所有行天然带上子层。参考线按原版四分支换轴/改位置
（竖线顺时针只换轴、逆时针 `position = width − position`；横线顺时针 `position = height − position`、逆时针
只换轴）。宽高在最后互换，之前的 placement 与参考线数学都用转之前的画布尺寸。

**标签尺寸同步。** `editTransformTab` 不像 `EditActive` 那样自动 `syncTabSizeLocked`，所以旋转回调在
工作区锁内补调一次 `s.ws.syncTabSizeLocked(tabID, sess.doc)`——否则前端画布盒子/状态栏/裁剪覆盖层会继续
按旧尺寸画框（`tabsize_test.go` 的回归）；撤销/重做路径本来就同步。

**前端（`frontend/src/lib/state/canvasRotate.ts`，新建）。** 照 `shortcuts/actions.ts` 的 `flipCanvas`
模式：取活动标签 → `RotateCanvas(tab.id, direction)`（带发起时的 tab id）→ 落地前复查
`workspace.activeId` 与 `document.docId` 仍是发起标签，再用 `applyDocumentSnapshot(raw, tab.id)` 应用；
回复属于别的标签（或 rev 未前进）时整条丢弃。随后把前端 `currentSelection` 的蒙版按同一方向转 90°
（纯函数 `rotateSelection`：宽高互换、蒙版按像素中心 `(x,y)→(H−1−y,x)` / `(y,W−1−x)` 重排、外框点按像素边
坐标 `(H−y,x)` / `(y,W−x)` 变换，羽化与 active 保留），最后 `fitViewport(newW, newH)`（原版
`viewport.fit`）。RPC 失败只 `console.warn`，文档 / 选区 / 视口一律不动。

**绑定（`frontend/wailsjs/go/bridge/Service.js` / `.d.ts`）。** 按既有条目样式在字母序位置
（`RetouchStrokePoint` 与 `SampleColorRange` 之间）插入 `RotateCanvas(arg1, arg2)`。

## 测试

- `internal/bridge/canvas_rotate_test.go`（新建，8 个顶层用例 / 11 个子用例）：3 × 2 逐像素（顺时针
  `[40,10,50,20,60,30]`、逆时针 `[30,60,20,50,10,40]`，与原版 `CanvasRotationTests` 逐位一致）；
  宽高互换 + 标签尺寸同步 + 历史名 + 单条撤销；顺逆各一次与四次同向都精确回到原状（含 placement 逐字段相等）；
  一次撤销恢复整份文档与像素；100 × 80 夹具里编组 / 子层 / 独立蒙版 placement / 三条参考线在两个方向下的
  逐字段期望 + 位图不动 + 一次撤销恢复；`quarterTurnedGuide` 四分支纯函数；非法方向、无文档、切标签、
  关标签、忙碌笔刷的拒绝且不留历史。
- `frontend/src/lib/state/canvasRotate.test.ts`（新建，12）：RPC 收到活动 tab id 与方向；快照落地；视口按
  新尺寸重 fit；两个方向的选区蒙版重排 + 宽高互换 + 外框点；无选区 / 全零选区跳过；回复属于别的标签整条丢弃；
  等待期间切标签的迟到回复不改状态；失败只 warn 且文档 / 选区 / 视口不动；`rotateSelection` 纯函数三例。
- 证据：`go vet ./...` 0 错；`go test ./...` 全绿；`npm run check` 0 错 0 警告；`npx vitest run`
  66 文件 / 1007 测试全过。

## 已知差异 / 未验证

1. **选区不在 `domain.Document` 里**：Windows 侧选区是前端 store（`state/selection.ts`），所以 Go 只转
   图层 / 蒙版 / 参考线 / 宽高，选区旋转在前端同一个 RPC 回复之后单独做。语义与原版
   `CGAffineTransform`（顺时针 `a:0,b:1,c:-1,d:0,tx:height`、逆时针 `a:0,b:-1,c:1,d:0,ty:width`）一致，
   但两者不在同一个原子事务里：极端情况下（应用在两者之间退出）可能出现文档已转、选区未转的中间态。
   **撤销同样只回滚 Go 侧**：撤销后文档回正、前端选区仍是转过的那一份（原版选区属于 document，
   与旋转在同一条历史里）。要让选区跟着撤销，得先把选区搬进 `domain`（超出本票范围）。
2. **全零（present-but-empty）选区跳过不转**：按约定无选区与全零选区都跳过；全零选区因此保留旧宽高，消费方
   （滤镜 / 剪贴板）都把它当"空"处理，不会误用。原版对空路径也会做一次变换（结果不变）。
3. **原版 `rotateCanvas` 开头的 `commitTransform()` / `cancelCrop()` / `finishOpacityEdit()` 未接**：
   Windows 侧的变换草稿是前端会话（`state/transform.ts`），本票的文件范围不含它，MenuBar 入口也直接调
   `rotateCanvas`。若变换会话开着时旋转，草稿会在转过的画布上提交——需要 Lead 决定是否在菜单入口先提交 /
   取消变换会话（`flipCanvas` 有同样的既有行为）。
4. **参考线只由 Go 转一次**：前端 `applyDocumentSnapshot → syncGuidesFromDocument` 重建镜像，前端不再
   `offsetGuides` 二次旋转（与 `crop.go` 的约定相同）。
