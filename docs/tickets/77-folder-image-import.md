# 77 — 文件夹直接子图像批量导入

**What to build:** 用户选择或拖放普通文件夹时，将其直接子级图像按自然文件名顺序导入为图层。

**Blocked by:** 40 — 栅格导入; 48 — 剪贴板与拖放。

**Status:** done（2026-10-16）

- [x] 只读取直接子级 regular image files，忽略隐藏项、子目录、`.comp` 包和不支持文件；`2` 排在 `10` 前。
- [x] 空文件夹、部分失败和取消有明确结果；批量导入可作为一次撤销操作。
- [x] Import Image、画布、标签栏和 Dock 的文件夹入口行为一致。

## 实现

`internal/bridge/imagefolder.go`：

- `isPlainFolder` 对应上游 `ImageFolder.matches`（ImageImporter.swift:26-30）：是目录、且不是包；
  `.comp` 也是磁盘上的目录，但它按项目打开而不是当图源。
- `folderImagePaths` 对应 `ImageFolder.images`（:32-42）：`os.ReadDir` 的**直接子级**、`IsRegular()`
  的常规文件、扩展名在可导入集合内；隐藏项按 macOS 的 `.` 前缀**与** Windows 的
  `FILE_ATTRIBUTE_HIDDEN` 双重判断（上游的 `skipsHiddenFiles` 在 Windows 上语义不同）。
- `naturalLess` 对应 `localizedStandardCompare`：数字段按数值比，所以 `2 Two` 在 `10 Ten` 前。
- 可导入集合**由导入对话框自己的过滤器派生**（`imageFilter.Pattern`），两处不可能列出不同格式；
  有一条测试专门钉这一点。

展开点选在 `beginImport`（`import_images.go`）——`BeginImageImport`、`ImportDroppedPaths` 与
`ImportDroppedPathsAsNewProject` 都走它，所以**导入对话框、画布拖放、标签栏拖放与外壳**四条入口
对文件夹的行为天然一致（验收 3），不会出现「对话框认、拖放不认」的分裂。展开后若某文件夹一张图都
没有，就作为一条 `status:error` 条目报出（与「文件读不了」同一形状），混合批次里其余文件照常导入。

入口：新增 `Service.PickImageFolder`（`runtime.OpenDirectoryDialog`）与文件菜单的「导入文件夹…」，
它和「导入图像…」共用同一条批次路径与同一个错误上报。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `TestFolderImportTakesOnlyTheImagesDirectlyInsideIt` | 上游 `folderImportsTheImagesDirectlyInsideIt` 的形状：子目录/`.comp`/隐藏/`.txt` 都不进，只剩 `[2 Two, 10 Ten]` |
| `TestFolderImportSkipsTheHiddenAttribute` | Windows 的隐藏**属性**（不只是点号前缀） |
| `TestEmptyFolderIsReported` | 上游 `folderWithoutImagesSaysSo`：明确报错而不是静默 |
| `TestFolderAndFilesImportTogether` | 混合批次里调用方的次序在前，文件夹内的按自然序接上 |
| `TestNaturalLessOrdersNumbersAsNumbers` | 数字段按数值；前导零的平局只钉确定性（上游的平局方向未核实） |
| `TestFolderScanAcceptsTheDialogsFormats` | 文件夹扫描与对话框过滤器不会分叉 |
| 前端 `importFolder (ticket 77)` | 取消返回 null 不过桥；选到目录就交给同一条 `BeginImageImport` |

## 已知差异

上游的 `localizedStandardCompare` 是**本地化**比较（大小写、重音、按语言排序），这里的
`naturalLess` 只做「数字段按数值、其余按码点」。对 `2 Two` / `10 Ten` 这类名字结果一致；
对依赖语言排序的名字（例如中文文件名、重音字母）次序可能与 Finder 不同。

对等矩阵：U24。原版锚点：`ImageImporter.swift`、`ImageImportTests.swift`；提交 `b2a0c45`。
