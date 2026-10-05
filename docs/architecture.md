# 目标架构 — Compositor for Windows

> 决策依据见 [adr/](adr/)。原版对应关系贯穿全文；Swift 文件路径均指 `reference/Swift/` 下的只读参考代码。

## 1. 系统总览

```
┌─ Svelte 5 + TS（WebView2 进程内）────────────────────────────┐
│  UI 层：标签条 / 工具栏 / 工具头 / 图层面板 / 浮动面板 / 表单    │
│  画布层：WebGPU 合成器 + 视口 + 覆盖层 + 工具指针交互           │
│  状态层：视图状态真源（zoom/pan/工具/选区框）；文档树只读镜像    │
│  桥接层：类型化绑定客户端 + 事件订阅 + 位图缓存 + 快捷键表       │
└──────────────▲────────────────────────────────────────────────┘
               │ ① Wails 绑定：类型化命令（控制面，小 JSON）
               │ ② Wails 事件：推送（外部变更/进度/层修订）
               │ ③ asset-server HTTP：大块像素（ImageBitmap 直取）
┌──────────────▼────────────────────────────────────────────────┐
│  Go 后端（文档唯一真源）                                        │
│  bridge    —— 绑定/事件装配、应用生命周期                        │
│  domain    —— 文档模型、限额、几何                              │
│  project   —— .comp v11 读写/校验/原子保存/监视/digest           │
│  history   —— 快照撤销（结构共享）                               │
│  pixel     —— 9 个 C 内核的 Go 移植（并行）                      │
│  render    —— CPU 全文档合成（导出真值 + GPU 兜底）              │
│  psd       —— PSD/PSB 解析器移植                                │
│  importer/exporter —— 编解码矩阵                                │
└───────────────────────────────────────────────────────────────┘
```

镜像原版的双轨设计：**WGSL 管线 = Metal/CoreImage 快路径**；**Go CPU 合成 = Core Graphics 真值路径**。两轨互证，CPU 轨同时承担导出与 GPU 不可用兜底。

## 2. Swift → Windows 模块映射

| 原版（reference/Swift） | Windows 版归宿 | 说明 |
| --- | --- | --- |
| `Document/`（54 文件：模型、工具逻辑、选区、调整、撤销） | Go `internal/{domain,history,pixel}` | 逻辑与算法；选区算法在 `pixel` |
| `EditorSession.swift`（982 行会话对象） | Go `bridge` 会话服务 + 前端状态镜像 | 会话门控（canEditLayers 等）在 Go，UI 态在前端 |
| `DocumentHistory.swift` | Go `history` | 快照式 + 结构共享原样保留 |
| `ProjectStore / ProjectWatcher / ProjectDigest` | Go `project` + Go `watch` | manifest v11、fsnotify、sha256 |
| `PSD/*`（手写解析器，PSDText 682 行） | Go `psd` | 字节级行为移植，测试夹具同源 |
| `ImageImporter / ImageExporter / RawImporter` | Go `importer/exporter` | HEIC/RAW 走 cgo（libheif/LibRaw） |
| `Rendering/EditorCanvas.swift`（3,124 行 NSView） | 前端 `lib/canvas` + 组件 | 指针/滚轮/键盘交互全在 Web 事件模型 |
| `CanvasViewport.swift`（85 行纯结构） | 前端 `lib/canvas/viewport` | 几乎逐行移植，Vitest 覆盖 |
| `GPUCanvas.swift`（CI over Metal） | 前端 `lib/gpu/compositor` | WGSL 合成器 |
| `MetalLayerEffects / MetalBrushCoverage / MetalWarp / GPUNoise` | 前端 `lib/gpu/*.wgsl` | 算法逐内核对齐 |
| `LayerRenderer / TiledLayerRenderer / SeparableBlend / DownsampleCache` | Go `render` | CPU 真值与降采样 |
| `Rendering/*.c`（9 内核 2,528 行） | Go `pixel` | 黄金基准验证移植 |
| `UI/`（46 文件 SwiftUI + NativeLayerList 1,382 行 AppKit） | Svelte 组件 | NSTableView → 虚拟列表；浮动面板 → DOM 窗口 |
| `KeyboardShortcuts.swift`（114 条表） | 前端 `lib/shortcuts` | Cmd→Ctrl 映射 |
| Sparkle + appcast.xml | go-selfupdate 风格 + GitHub Releases | EdDSA 签名 feed |
| Apple Vision（主体/对象/背景） | ONNX Runtime + 开源分割模型 | 见 ADR-0005 |
| QuickLook 包内预览 | 保留写入（跨平台通用），无 shell 钩子 | M9 复评缩略图处理器 |

## 3. IPC 契约

### 3.1 控制面（Wails 绑定命令）

- **文档生命周期**：`NewDocument` / `OpenProject(path)` / `SaveProject` / `SaveProjectAs` / `CloseTab` / `QuitConfirm`。
- **编辑事务**：`BeginEdit(name)` / `EndEdit()` / `Undo` / `Redo`——对应原版嵌套事务语义。
- **图层操作**：增删改/重排/编组/蒙版/特效/调整/文本样式（纯元数据操作，小 JSON）。
- **工具事务**：`StrokeCommit`（笔刷瓦片补丁）、`SelectionApply`、`TransformCommit`、`CropCommit` 等——像素重活在 Go 执行。
- **导入导出**：`ImportFiles` / `ExportImage` / `ImportPSD`（返回转换报告）。
- **查询**：文档树快照、直方图、示波器数据、字体列表。

### 3.2 事件面（Go → 前端推送）

- `external:changed`（外部变更已重载，附新树 + 保留视口指令）
- `layer:revised`（层修订号递增 → 前端失效对应 ImageBitmap）
- `task:progress`（导出/滤镜后台进度）
- `session:gate`（canEditLayers / canUseHistory 门控变化）

### 3.3 像素面（HTTP）

- asset-server 中间件挂 `/pixel/<layerID>/<revision>.png`：完整层、蒙版、合成结果按需取回为 ImageBitmap。
- 缓存失效键 = 层修订号；`layer:revised` 事件触发重取。
- **红线**：任何位图禁止以 base64 走 JSON 桥（原版教训的对称移植：原版走零拷贝 CGDataProvider，等价物是 HTTP 零拷贝路径）。

## 4. 关键数据流

### 4.1 一笔笔刷（延迟敏感路径）

```
pointerdown → 前端笔触缓冲（瓦片化）
  → WGSL 笔刷覆盖核（永久/临时尾双缓冲）→ 覆盖层合成 → 每帧 rAF 呈现  【<16ms 本地回显】
pointerup → 二进制瓦片补丁 → Go 写层瓦片 + 生成新快照
  → layer:revised 事件 → 前端失效对应 ImageBitmap → 重取 → 常规重合成
```

覆盖层（光标圈、蚂蚁线、变换框、渐变/裁剪覆盖）独立于合成循环——对齐原版"覆盖层重绘不触发全画布重合成"。

### 4.2 外部代理写 `.comp`（热重载契约）

```
代理写 PNG → 原子改名 manifest.json
  → fsnotify 事件 300ms 合并 → 原子重命名后重挂
  → sha256 digest 比对（忙等退避 250ms×2ⁿ）→ 原位重载（保留 zoom/pan/选区，清撤销）
  → 脏文档 → 弹"放弃/保留"询问
```

### 4.3 导出（真值路径）

```
前端请求导出 → Go CPU 合成器全文档渲染（混合模式/蒙版/特效/调整）
  → 编码 PNG/JPEG → 写文件；JPEG 实时预览同源渲染（≤预览尺寸）
```

## 5. 并发与内存模型

- **Go**：文档操作单 writer 串行（对位原版 `@MainActor`）；重像素活走 goroutine 池；IO/保存后台化（保存期间可继续编辑，对位原版 finishWriting 任务）。
- **前端**：rAF 合成循环；重像素操作（滤镜预览）由 Go 后台完成，前端只收结果——对位原版 `Task.detached` + `previewLimit`。
- **内存**：移植 DocumentLimits 预算（边长 30,000px / 单面 200M 像素 / 文档 min(800M, RAM/16)）；前端纹理缓存 LRU 上限 + `navigator.storage`/内存压力响应；Go 侧撤销修剪 100 条/256MB。

## 6. 仓库布局（目标终态）

```
reference/Swift/          只读原版（功能基准 + 算法真值）
docs/                     执行文档（本目录）
internal/
  bridge/                 Wails 装配、生命周期、事件
  domain/                 文档模型与限额
  project/                .comp 存取、校验、原子保存、PNG 资产
  watch/                  外部变更监视（fsnotify 合并/重挂）、digest、退避协调
  history/                快照撤销
  pixel/                  C 内核 Go 移植（wand/heal/fill/adjust/levels/noise/lens/dither/brush）
  render/                 CPU 合成、混合模式、特效、降采样
  psd/                    PSD/PSB 解析
  importer/ exporter/     编解码
frontend/
  src/lib/gpu/            WebGPU 设备、WGSL 内核、纹理缓存
  src/lib/canvas/         视口、合成循环、覆盖层、工具交互
  src/lib/state/          Svelte stores（视图态 + 文档镜像）
  src/lib/bridge/         绑定客户端、位图缓存、事件
  src/lib/shortcuts/      114 条快捷键表 + 重映射
  src/components/         面板/表单/工具头/图层面板
build/                    appicon、manifest、NSIS 资产
tests/golden/             黄金基准 harness（C 内核拷贝件 + 参考 PNG）
.github/workflows/        windows-latest CI
```
