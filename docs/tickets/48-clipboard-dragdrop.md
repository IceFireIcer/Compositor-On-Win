# 48 — 剪贴板与拖放

**What to build:** 系统剪贴板双向（像素进/出、接受外部图像）、"拷贝合并"（42 号票产位图）进剪贴板、整层/文件夹复制粘贴（跨项目）、文件拖放导入（文件/截图/网页图临时文件回退）、图层跨标签拖拽。

**Blocked by:** 16 — 选区命令族与色彩范围; 20 — 图层操作命令族与图层面板.

**Status:** done（2026-10-10）

- [x] 像素出/入系统剪贴板（SelectionClipboardTests 语义）
- [x] 整层复制粘贴跨项目可用
- [x] 文件拖放导入三来源（文件/截图/网页图）可用
- [x] 图层跨标签拖拽；粘贴替代对话框语义
      —— "粘贴替代对话框语义"按原版 `addPixelLayer(dropsSelection: true)` 实现：粘贴落层后
      **丢弃选区**（选区在前端 store，粘贴成功后清空）；⌘J"通过拷贝的图层"同样丢选区。
      票面没有别的对话框语义可对，若指 Photoshop 的"粘贴入"(Paste Into) 则未实现，登记在下方。

对等矩阵：C11–C13、P02、P07、故事 55。

## 实现说明（2026-10-10）

**像素出/入（Go：`internal/winclip` + `internal/bridge/clipboard.go`）。**
- `winclip` 新增读侧：`GetImage`（优先注册格式 "PNG"，回落 CF_DIB）与 `Sequence`（剪贴板序号，
  对应 `NSPasteboard.changeCount`）。C 侧 `clip_get_data`/`clip_sequence`；**解码全在纯 Go**
  （`dib.go`：BITMAPINFOHEADER/BITMAPCOREHEADER、1/4/8/16/24/32bpp、BI_RGB 与 BI_BITFIELDS、
  上下行、调色板、XRGB 全零 alpha 判定），因此格式矩阵在任意机器上都能测（`dib_test.go` 8 组）。
- `Service.CopySelection(selectionJSON)`：把活动图层按"画布所见"合成、裁到选区包围盒、按选区覆盖度
  乘预乘通道（软边保留），写入系统剪贴板（CF_DIB）并记住像素与来源原点；**没有选区**时区域是整幅
  画布，并同时记住整层（原版 `canCopyLayer` 分支）。序号在**自己写完剪贴板之后**读，否则自己的
  写入会让自己的记忆瞬间失效（原版同样在 setData 后取 changeCount）。
- `Service.CutSelection`：先拷贝再按选区抹成透明，一步"剪切"，像素经 `EditActiveRaster` 的
  rasterJournal 随撤销/重做还原（选区没盖住图层时不产生历史）。
- `Service.Paste`：三条来源按原版顺序 —— 同一工作区里拷过的整层（同标签走 `duplicateLayers`，
  跨标签走 `copyLayersInto`）、本应用拷的像素（原位）、外部图像（居中）。
- `Service.LayerViaCopy`：⌘J 有选区时把选区像素就地变成新层"通过拷贝的图层"。
- 系统剪贴板走 `systemClipboard` 接缝（生产 = winclip，测试 = 内存假实现），测试套件不碰用户
  剪贴板；Copy Merged（票 42）也改用同一接缝，删掉了重复的 `copyToClipboard`。

**整层/跨项目复制（Go：`internal/bridge/layer_transfer.go`）。** `copyLayersInto` 把图层连同
子树复制进另一个会话：新 UUID、`<新UUID>.png`/`.mask.png`、位图深拷贝、子树内 parentID/
maskSourceID 重映射、切开的根挂到目的地父级、**整体居中到目标画布**（Swift
ProjectWorkspace.swift:246-255）、目标侧一步历史（`跨标签粘贴图层`）；校验先于修改（未知 id /
同会话 / 超 10000 层 / Validate 失败都不动两端）。`Service.TransferLayer(layerID, destDocID)`
是前端拖拽的入口：复制后把活动标签切到目标并返回新 Snapshot。

**前端接线。** `state/clipboard.ts`（复制/剪切/粘贴/通过拷贝的图层，选区序列化）、
`state/dropimport.ts`（拖放判定：Wails 路径与 DOM 字节两路去重、浏览器能否解码、按位次命名）、
`workspace.ts`（`importDroppedPaths`/`importDroppedBlobs`/`transferLayer`，把既有导入批次流程
抽成 `runImportBatch` 共用）。菜单：编辑 ▸ 剪切/拷贝/粘贴/通过拷贝的图层；快捷键 ⌘X/⌘C/⌘V、
⌘J（有选区走 LayerViaCopy）。图层行 `draggable`，标签栏接手 `application/x-compositor-layer`
的 drop。

**拖放导入三来源。**
1. **磁盘文件**：Wails `OnFileDrop` → `Service.ImportDroppedPaths`（复用 BeginImageImport 的解码
   主体；全栅格时一次提交、一步撤销；出现 SVG/RAW/错误时回落到既有导入表单流程）。
2. **截图位图 / 3. 网页图**（没有磁盘路径）：DOM `drop` → 浏览器 `createImageBitmap` 栅格化成
   PNG → `BeginBlobImport` 发上传 token → PUT 到既有 `/pixel/upload/<token>` 面 → Finish。
   网页图（`text/uri-list`）先 fetch 再走同一条路。
   两条路可能同时收到一次拖放，且谁先到不确定：DOM 侧先等 250ms（`DROP_SETTLE_MS`）让
   Wails 的窗口级回调落地，再用 750ms 去重窗口（`DROP_DEDUP_MS` / `shouldImportDomDrop`）
   判定"这次拖放已经按路径导过了"，避免同一批文件导入两遍。

## 测试

- Go：`internal/winclip/dib_test.go`（8 组，DIB 格式矩阵）、`winclip/png_test.go`（2 组，PNG
  解码与预乘）、`internal/bridge/clipboard_test.go`（18 组：原位粘贴/剪切留洞与撤销/整层复制/
  编组整层复制/套索软边选区/选区包围盒裁剪/外部图像居中/他人拷贝使记忆失效/跨标签粘贴/子树只
  复制一次/剪切一步撤销/选区未覆盖不算历史）、`layer_transfer_test.go`（12 组）、
  `import_drop_test.go`（8 组）。
- 前端：`state/clipboard.test.ts`（9 组）、`state/dropimport.test.ts`（7 组）、
  `state/workspace.test.ts` 的拖放/搬运接线（6 组）、`shortcuts/actions.test.ts` 的接线清单
  （Cut/Copy/Paste 已接线，未接线 25 → 22）。

## 已知差异 / 未实现（诚实登记）

1. **系统剪贴板的 OS 手递手没有自动化测试**：winclip 的 Win32 读写（C shim）只在真机上跑，CI 里
   被接缝换成内存假实现；纯解码（CF_DIB 格式矩阵、PNG 预乘）与语义层有测试。手工验收要点：
   拷贝 → 粘进"画图"；从浏览器拷图 → 应用里 ⌘V 居中落层。
   C 侧 `clip_get_data` 的契约是"cap 之外一个字节都不写"：尺寸不够时只回传所需大小（返回码 3），
   Go 侧重新分配后再读一次；`GlobalSize` 可能比真实 DIB 大，多出的尾部字节由 Go 的头部自校验
   （stride/调色板越界检查）忽略。
2. **没有蒙版选区**：原版 `isMaskSelected` 时拷贝的是蒙版灰度，本应用还没有蒙版选区入口（票 20
   的蒙版行/票 39 的活蒙版之外），故只处理图层像素。
   另外 Go 的文档模型没有多选集合（选择在前端），所以"整层复制"一次只带**活动图层**及其子树；
   原版 `copiedLayerIDs()` 会把多选集合一起复制。多选落地后 (`copyLayersInto` 已接受 ids
   列表) 前端补一个 ids 即可。
3. **层外剪贴不烘焙**：跨项目复制时指向子树外的 `maskSourceID` 直接置 nil（原版会先
   `LiveMaskBaker.bake` 把剪贴外观烘进像素）。
4. **没有"粘贴到画布落点"**：跨标签拖拽一律居中（原版带落点时可落在指针处）；也没有
   "Paste Into（粘贴入选区）"这一 Photoshop 命令。
5. **拖放三来源未做真机交互验收**：Go 侧与前端判定都有测试，但 Wails `OnFileDrop` 与 WebView2
   的 DOM drop 在真实拖放下的配合（尤其是"一次拖放是否只导一次"）需要手工过一遍。
   另外**内存图像（blob）没有像素预算前置校验**：尺寸要等上传才知道（SVG 能查声明尺寸），
   目前只受上传面 512MB 字节上限约束；严格预算要在 `storeUploadedRaster` 里补（留待后续）。
6. **剪贴板像素不做 PNG 写出**：系统剪贴板只写 CF_DIB（Windows 应用都认），原版写 PNG 给 macOS
   应用用。
   另外系统剪贴板的"读"优先取注册格式 PNG、回落 CF_DIB；CF_BITMAP（老应用只放 DDB）没接，
   遇到时按"没有位图"报错。
7. **图层面板内的拖拽排序还没接**：本次让图层行 `draggable`（跨标签复制的起点），但面板内部的
   拖拽重排（原版 drag-reorder，`state/layerOps.ts: reorder` 已有纯函数）仍属票 20 的 UI 收尾；
   拖到面板内目前不会有动作，拖到标签上才会复制过去。
7. **撤销后副本位图留在库中**：与仓库既有约定一致（history 只存清单）。
