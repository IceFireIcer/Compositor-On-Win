# 74 — 从欢迎页和标签栏创建项目

**What to build:** 用户可从欢迎屏粘贴图像建画布、把图像拖到标签栏新开项目，或把图层拖进新项目并自动适配画布尺寸。

**Blocked by:** 02 — 编辑器外壳 UI 与新建画布; 20 — 图层操作命令族与图层面板; 48 — 剪贴板与拖放。

**Status:** done（2026-10-15）——三条验收均已落地。

- [x] 欢迎屏粘贴图像直接按图像大小建画布；文本仍可粘入当前输入框。
- [x] 图片落在标签栏任意位置新开项目；拖到现有画布仍是图层导入；新项目从图层建立时画布自动适配。
- [x] 关闭最后一个标签后欢迎页保持稳定，无故增加 Untitled 序号；多标签拖放与撤销有回归用例。

## 实现

1. **欢迎屏粘贴**（`App.svelte` 的 `onWelcomePaste`）：没有打开任何文档时，把剪贴板里的图
   交给既有的 blob 导入路（`importDroppedBlobs`）——没有文档时 Go 的 `commitImportedLayers`
   本来就走 `importNewDocument`，按第一张图建项目。焦点在 `input`/`textarea`/可编辑元素上时
   一概不接手，文本照常粘进输入框。
2. **标签栏拖放**：新增桥接入口 `ImportDroppedPathsAsNewProject`（与 `ImportDroppedPaths`
   同形的两种回包），前端 `importDroppedPathsAsNewProject` 与它对接。`TabStrip` 整条栏
   （含空白处）是拖放目标；桌面端同一次拖放还会惊动 Wails 的窗口级回调，所以 `OnFileDrop`
   现在用回调带的**落点坐标**判断是否落在标签栏上，落在别处仍走原来的「导入为图层」。
   两条路共用一个去重时间戳（`dropimport.ts` 的 `noteWailsPaths`/`shouldImportDomDropNow`），
   否则同一次拖放会导两遍。
3. **Untitled 序号**：新标签取**最小的空闲**序号，而不是一路递增（原版 `ProjectWorkspace.addTab`，
   上游 issue #231）。关掉中间那个标签后下一个补它的号；关掉最后一个又拿到「未命名」。
   两处命名（`appendDocument` 与 `importNewDocument`）共用一个 `nextUntitledName`。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `internal/bridge` `TestDroppingImagesOnTheTabBarOpensANewProject` | 画布按第一张图的尺寸、每张图一层、原文档不受影响 |
| `internal/bridge` `TestDroppingOnTheTabBarRefusesAnEmptyBatch` | 空批次报错 |
| `internal/bridge` `TestUntitledNumbersReuseTheLowestFreeSlot` | 上游 `untitledNumbersStayLowAndTheBlankTabStays` 的序号语义 |
| `internal/bridge` `TestUntitledNumberReadsOnlyUntitledNames` | 只有「未命名」与「未命名 N」占号 |
| `internal/bridge` `TestImportDroppedPathsCommitsAllRastersAsOneUndo`（既有） | 拖到画布仍是导入为图层、一步撤销 |
| `frontend` `dropimport.test.ts` | 两条拖放路共用去重时间戳 |

## 已知差异

1. **没有磁盘路径的内存图像已接**（`BeginBlobImportAsNewProject` + 前端的
   `importDroppedBlobsAsNewProject`）：标签栏的 DOM 拖放在文件不带 `path` 时改走上传面，同样落成
   新项目。取消批次会把「新开项目」开关一起清掉，下一批普通导入不会跑错地方。
   **仍有的差异**：桌面端 Wails 的窗口级回调只带路径，认不出「落在标签栏的内存图」，所以那种
   拖放仍按「导入为图层」处理；只有 DOM 那条路（网页里拖来的图）走新项目。原版经 NSItemProvider
   两者都覆盖。
2. 原版「最后一个空标签不关闭」这条没做：Windows 侧关掉最后一个标签直接回欢迎屏，
   语义等价（`canClose` 的判据在原版里就是为了不出现「关了又被替换」的空标签）。

现状（已核对）：

- 欢迎页在 `App.svelte:397-402`，目前只有「新建画布…」一个入口，没有粘贴处理。
- `TabStrip.svelte:57-59` 的拖放只挂在**每个标签**上，且只认 `LAYER_DRAG_MIME`（图层跨标签复制）；标签栏空白处不是拖放目标。
- 桥接有 `Workspace.NewDocument(w,h,res,name)`、`ImportDroppedPaths(paths)`、`BeginBlobImport(names)`，但**没有**「按图像尺寸新建项目并把它作为图层导入」的入口 —— 这是本票缺的那一块。

剩余工作：

1. 桥接新增一个「从拖入的图像新建项目」入口：解码首图取尺寸 → 建同尺寸画布 → 其余作为图层导入（上游 `ProjectWorkspace` 的 drop-on-tabs 行为）。
2. `TabStrip` 把整个标签栏（含空白处）作为拖放目标，图片落到任意位置都新开项目；落到现有画布仍是图层导入（现状已对）。
3. 欢迎屏粘贴图像直接按图像大小建画布；文本仍粘进当前输入框。
4. 关掉最后一个标签后欢迎页稳定、不无故增加 Untitled 序号；多标签拖放与撤销的回归用例。

（上面 1–4 是实施计划；验收清单在文首，三条**均已勾**。此处原先还留着一份未勾的**重复**清单 —— 2026-10-20 清掉，避免「标了 done 却有未勾项」的假象。图层面板揭示和窄布局分见 88、92。）
对等矩阵：U21。原版锚点：`ProjectTabs.swift`、`ProjectWorkspaceTests.swift`；提交 `2522e3b`、`d3d0ce7`、`e580107`。
