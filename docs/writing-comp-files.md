# 编写 Compositor 项目（给 AI 代理与脚本）

> **本文是 Windows 版契约**，描述 Compositor for Windows 的实际行为。macOS 原版契约在
> [reference/Swift/docs/writing-comp-files.md](../../reference/Swift/docs/writing-comp-files.md)（**只读**，不要改它）。
> 两者结构对齐，**凡行为不同处本文都显式标出**；格式权威是
> [reference/Swift/docs/project-format.md](../../reference/Swift/docs/project-format.md)。

一个 Compositor 项目（`.comp`）就是一个文件夹：若干 PNG 图层图 + 一份 `manifest.json`。
任何能写文件的程序都能建或改它，Compositor 会在文件变化时更新已打开的画布。不需要插件，也没有 API。

## 试试看

1. 在 Compositor for Windows 里打开一个项目（新建画布后保存到某处，例如 `C:\Users\you\Desktop\demo.comp`），**保持它开着**。
2. 让一个能写文件的 AI 代理（Claude Code、Codex 之类）：

   > 读 docs/writing-comp-files.md，然后在 C:\Users\you\Desktop\demo.comp 里设计一个夜色场景。一步一步来，一次一到两个图层。

3. 盯着画布。代理每次写完项目，Compositor 就会重载它，通常半秒内。

得到的就是普通图层：可以选中、改不透明度和混合模式、在蒙版上绘制、保存。

## 包结构

```
C:\Users\you\Desktop\demo.comp\
├── manifest.json
└── images\
    ├── 6F1D3C2A-0B7E-4E8A-9C4D-2A1B3C4D5E6F.png        图层像素
    └── 6F1D3C2A-0B7E-4E8A-9C4D-2A1B3C4D5E6F.mask.png   该图层的蒙版（可选）
```

一份最小 manifest：单个铺满画布的图像图层。

```json
{
  "format": "com.compositor.project",
  "version": 11,
  "colorSpace": "sRGB",
  "documentID": "0C5E7A91-3B2D-4F6A-8E1C-9D0B7A6F5E4D",
  "width": 1920,
  "height": 1080,
  "resolution": 72,
  "activeLayerID": "6F1D3C2A-0B7E-4E8A-9C4D-2A1B3C4D5E6F",
  "layers": [
    {
      "id": "6F1D3C2A-0B7E-4E8A-9C4D-2A1B3C4D5E6F",
      "name": "Background",
      "imageFile": "6F1D3C2A-0B7E-4E8A-9C4D-2A1B3C4D5E6F.png",
      "isVisible": true,
      "isGroup": false,
      "opacity": 1,
      "blendMode": "Normal",
      "transform": {
        "origin": [0, 0],
        "size": [1920, 1080],
        "rotation": 0,
        "flipX": false,
        "flipY": false,
        "sampling": "High quality"
      }
    }
  ]
}
```

- `layers` **自下而上**排列：最后一个图层画在最上面。
- 编辑已有项目时，`documentID` 保持原样别改。
- `transform` 用文档像素摆放图层：`origin` 是左上角，`size` 是宽高，`rotation` 是顺时针角度。图像会被拉伸到 `size`，所以图层可以比画布小（用 `origin` 摆一个抠图），也可以放大。
- `sampling` 只能是 `"High quality"`、`"Smooth"`、`"Nearest"`（注意 `High quality` 的 q 是小写）。
- `opacity` 取值 0 到 1。
- 图像以 UTF-8 无 BOM 写出；JSON 里的 CRLF 也能正常解析，但应用自己写的是 LF（见"Windows 差异"）。

## 必须遵守的规则

破坏任意一条，Compositor 会**整包拒绝且不给任何提示**：打开的画布保持原样。如果什么都没更新，先查这几条。

- **图像文件按图层命名。** `"id": "6F1D…"` 的图层必须用 `"imageFile": "6F1D….png"`，蒙版用 `"maskFile": "6F1D….mask.png"`，**ID 一律大写**，与 manifest 里写的一致。一个图层一个 ID，项目内唯一。
- **图像是 8 位 PNG**，放在 `images\` 下。图层图像是 RGBA；蒙版是 8 位灰度（白显示图层，黑隐藏）。
- **混合模式拼写必须精确**（24 种）：`Normal`, `Darken`, `Multiply`, `Color Burn`, `Linear Burn`, `Lighten`, `Screen`, `Color Dodge`, `Linear Dodge (Add)`, `Overlay`, `Soft Light`, `Hard Light`, `Vivid Light`, `Linear Light`, `Pin Light`, `Hard Mix`, `Difference`, `Exclusion`, `Subtract`, `Divide`, `Hue`, `Saturation`, `Color`, `Luminosity`。
- **manifest 点名的每个图层，它的图像都要就位**，且 manifest 是合法 JSON。

## 项目开着的时候怎么写才安全

Compositor 一发现项目变化就去读，所以永远不要留下写了一半的状态：

1. 先把新增或改动的 PNG 写进 `images\`。
2. 再把 manifest 写到包内的临时文件（例如 `.manifest.json.tmp`），然后**一次改名**覆盖 `manifest.json`。

改一个已有图层：保留它的 `id`，覆盖它的 PNG，再重写 manifest。图层就地更新，堆叠位置不变。

manifest 不再引用的图像，等 manifest 提交之后再删。

**Windows 上不要"先删 manifest 再改名"**：见下一节第 2 条，那是会丢整个项目的写法。

## Windows 差异

以下都是本机实测出来的；与 macOS 原版契约不同处已标出。

1. **文件被打开时改名会失败。** `os.Rename(tmp, target)` 在 target 已被某进程打开（即使只是只读打开）时返回 `Access is denied`；对一个被打开的文件直接 `os.Remove` 也会失败（`being used by another process`）。目标不存在时改名正常；**目标存在但没被打开时改名也会成功并替换**（MoveFileEx 语义）。
   → **怎么做才对**：改 manifest 失败就**重试几次**（应用只在读的那一瞬间持有文件）；不要改成"先删旧的再改名"。
2. **先删再改名是危险的。** 如果 `os.Remove(manifest)` 成功了、随后的改名却失败（进程被杀、文件被锁），**整个项目就没了**。永远走"写临时文件 → 一次改名覆盖"。
   > 应用自己也按这条规则写：`Store.Save`（`internal/project/store.go`）通过
   > `internal/fsatomic.WriteFile` 提交 manifest —— 临时文件 + **一次 rename 覆盖** + 遇到瞬态锁短暂重试。
   > 所以"应用保存"和"代理写入"在同一个包上交替进行也不会出现缺清单的窗口。
3. **manifest 必须是 UTF-8 且不带 BOM。** 实测 Go 的 `encoding/json` 遇到 BOM 直接报 `invalid character 'ï'`，于是整个包被静默拒绝。用记事本"另存为 UTF-8"或用 PowerShell `Out-File`/`Set-Content` 时注意别写出 BOM。
4. **CRLF 合法，但改换行会让应用重载。** 实测 JSON 里的 CRLF 正常解析（应用自己写的是 LF）。
   注意 digest 是**按 manifest 的原始字节**算的（`internal/watch/digest.go`），所以"只把 LF 换成 CRLF、
   其余内容不变"会被当成一次真的改动而触发重载 —— 不影响正确性，但会让画布白刷一次。
5. **文件名大小写。** manifest 里的 `imageFile` 必须写成**大写 UUID**：`internal/project/store.go` 的 `assetNameRe` 是 `^[0-9A-F]{8}-…(\.mask)?\.png$`，小写会被拒。NTFS 大小写不敏感，所以磁盘上放小写文件名在 Windows 还能找到，但**同一个包在 macOS 上会失败**——一律用大写。
6. **OneDrive / 云占位符。** 只有云端的占位文件在未下载时读取会失败（取决于 provider）；应用把这种读失败当作"这次不重载"。代理应确保文件已真正落地，或让用户把包目录设为"始终保留在此设备上"。**未实测**（本机没有 OneDrive 目录）。
7. **杀毒/索引器瞬态锁。** 刚写完的 PNG 可能被短暂锁住；改名要重试，应用侧会被忽略到下一次变更。
8. **QuickLook。** 那是 macOS/Finder 的东西，位于**包根目录**（`<包>/QuickLook/Preview.jpg`）。Windows 版
   **不写**它；digest 只看 `manifest.json` 与 `images\` 下的普通文件（`internal/watch/digest.go`），
   所以根目录的 `QuickLook\` 既不影响摘要也不影响加载 —— **Windows 上不需要删它**
   （原版契约要求每次改动后删掉，Windows 无此要求；留着也不会让应用误判为"有改动"）。
9. **长路径。** 未实测；不要在文档或脚本里断言 260 字符限制的行为。

## 打开着的应用会做什么

- 写入停止后大约三分之一秒重载。连续的多次写入会合并成一次更新，所以想让观看者看到每一步，就在步骤之间稍停一下。
- 重载**保留缩放、平移和选区**（这些是前端视图状态），但**清空撤销栈**，等同于重新打开文件。标签 id 与位置不变，活动标签也不变——后台标签的改动不会把你从当前标签拽走。
- 如果用户自己有未保存的修改，Compositor 会问他是"放弃我的修改"还是"保留我的"，绝不会静默覆盖他的工作。
- **载入失败会被忽略**，直到下一次变更：半成品或不合法（含 manifest 缺失）的包只是不被采用，旧画面保持不动，也不弹错。所以你写错了再修好，仍然会显示出来。
- 变更的判定来自 **manifest 的原始字节** 加上 **每个图像的`名字`与`字节大小`**，不看写入时间、不看目录、不读图像内容。用不同像素重写一张 PNG 实际上总会改变大小。若你替换的图像**字节大小完全相同**，就必须同时在 manifest 上做点改动（例如改图层名）；把**一模一样的 manifest 字节**再写一遍是不够的。

## 看不到更新时怎么排查

按 digest 规则逐条排查：

1. **PNG 同名同大小。** 新图和旧图字节数一样，manifest 又一字未改 → 不会触发重载。**在 manifest 上也做点改动**（改图层名、调 `opacity`、加一条 `guides` 都行）。
2. **manifest 有 BOM。** 整个包被静默拒绝，画布不动。把文件重存为"UTF-8 无 BOM"。
3. **ID 不是大写。** `imageFile` / `maskFile` 里出现小写 UUID → `assetNameRe` 拒绝整包。
4. **只改了元数据 / 原样重写。** 只动文件时间、或把相同字节再写一遍 → digest 不变，不算变更。
5. **顺序错了导致包半成品。** 先改名 manifest 再写 PNG，应用可能正好读到"manifest 已引用、PNG 还没到"的状态 → 整包被拒，直到下一次变更。永远 PNG 先、manifest 后。
6. **重名 / 缺图。** 图层 ID 重复、`imageFile` 指向不存在的文件、PNG 不是合法 8 位 PNG、蒙版不是 8 位灰度 → 整包被拒。
7. **只把 LF 换成 CRLF 也算一次改动。** digest 按 manifest 的原始字节算，所以换行变化会让应用
   重载一次（内容没变，画面不会变，但会白刷一次）。
8. **还是不动**：确认应用打开的就是那个路径的项目；确认没有另一个进程（编辑器、同步客户端）还占着文件；然后随便改一个图层名再写一次。

## 蒙版

给任意图层加蒙版：`"maskFile": "<id>.mask.png"` 配 `"maskEnabled": true`。蒙版覆盖图层自己的像素，所以像素尺寸与图层图像一致。柔和的灰色给出柔和边缘。

## 调整图层

调整图层有 `adjustment` 对象、**没有** `imageFile`，影响它下面的一切。每种类型都带同一套恒等 `levels` 与 `curves` 块，外加自己的设置。一个暖色的曲线层：

```json
{
  "id": "A1B2C3D4-E5F6-4A7B-8C9D-0E1F2A3B4C5D",
  "name": "Warm Grade",
  "isVisible": true,
  "isGroup": false,
  "opacity": 1,
  "blendMode": "Normal",
  "transform": { "origin": [0, 0], "size": [1920, 1080], "rotation": 0, "flipX": false, "flipY": false, "sampling": "High quality" },
  "adjustment": {
    "kind": "Curves",
    "hue": 0, "saturation": 0, "lightness": 0, "colorize": false,
    "levels": { "channel": "RGB", "ranges": [
      { "black": 0, "gamma": 1, "white": 255, "outputBlack": 0, "outputWhite": 255 },
      { "black": 0, "gamma": 1, "white": 255, "outputBlack": 0, "outputWhite": 255 },
      { "black": 0, "gamma": 1, "white": 255, "outputBlack": 0, "outputWhite": 255 },
      { "black": 0, "gamma": 1, "white": 255, "outputBlack": 0, "outputWhite": 255 } ] },
    "curves": { "channel": "RGB", "channels": [
      [ { "x": 0, "y": 0 }, { "x": 255, "y": 255 } ],
      [ { "x": 0, "y": 0 }, { "x": 120, "y": 147 }, { "x": 255, "y": 255 } ],
      [ { "x": 0, "y": 0 }, { "x": 100, "y": 114 }, { "x": 255, "y": 255 } ],
      [ { "x": 0, "y": 0 }, { "x": 115, "y": 97 }, { "x": 255, "y": 238 } ] ] }
  }
}
```

- `ranges` 与 `channels` 依次是 RGB、红、绿、蓝。曲线点的 x 从 0 到 255，x 递增。
- `kind` 是 `Hue/Saturation`、`Levels`、`Curves`、`Exposure`、`Gradient Map`、`Grain`、`Invert`、`Black & White`、`Color Balance`、`Gaussian Blur`、`Motion Blur`、`Add Noise` 之一。
- Hue/Saturation 直接在 adjustment 上设 `hue`、`saturation`、`lightness`。Color Balance 用 `colorBalanceSettings` 对象（`shadowCyanRed`、`shadowMagentaGreen`、`shadowYellowBlue`，`mid` 与 `highlight` 同理，各自 −100 到 100，外加 `preserveLuminosity`）。
- 其他类型最省事的做法是：在 Compositor 里加一个、保存，然后从那个项目的 manifest 里照抄。

## 更多

- 文件夹、文字图层、图层特效以及格式里的其他东西：[project-format.md](../../reference/Swift/docs/project-format.md)。
- 限额：画布每边最多 30,000 像素；单个图层图像与蒙版各 1 亿像素；最多 10,000 个图层；manifest 4 MiB；单个资产 512 MiB。
- 验收记录与"未实测"清单：[docs/acceptance/agent-contract.md](../acceptance/agent-contract.md)。
