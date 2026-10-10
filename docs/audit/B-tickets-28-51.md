# B 审计报告：票 28–51 实现完整性对照（只读）

> 审计人：audit-late（task-2）｜快照：`git rev-parse HEAD` = **679d5d8**（工作树当时仅 AGENTS.md 有改动）
> 方法：逐票读验收清单 → 在代码里找实现证据（read/grep）→ 逐条判定。**未跑构建、未改代码、未碰 reference/Swift/**。
> 判定口径：✅ 验收项与代码证据相符；⚠️ 相符但有未勾项/已登记差异；❌ 验收项与代码不符或能力缺失。

## 0. 审计期间发现的并发写入（先读这一节）

审计进行中（00:04–00:06）有队友在往工作树写**未提交**的视图接线，本报告基线以 HEAD 679d5d8 为准：

- 新增（untracked）`frontend/src/lib/state/view.ts`（00:04:33）、`view.test.ts`：Show Grid/Guides/Rulers/Snap/Lock 的 store + 布局网格 + 参考线外壳。
- 修改（unstaged）`frontend/src/lib/state/document.ts`（+guides 透传）、`frontend/src/lib/components/CanvasSurface.svelte`（参考线渲染/拖拽）。
- **但 `SetGuides` RPC 在 Go 里仍不存在**：全仓 grep `SetGuides` 只命中 `view.ts:12/89` 与 `view.test.ts:116`，`internal/bridge` 无该端点。且 `shortcuts/actions.ts` 未改（mtime 14:30，git 无改动），5 条视图快捷键仍未接线。

另：00:07 又出现未跟踪的 `internal/bridge/fill_test.go`，说明填充 RPC（本报告 §2 B 类 / §6 缺口 2）也正在被并行补上。

结论：报告第 2 节里"视图开关无 store"一条在 HEAD 成立、在**当前工作树**已被部分推翻；"填充无 RPC"同理。但"写回 Go 的通道（SetGuides）缺失 + actions 未接"两条在写报告时仍成立。Lead 定稿前请以最终提交为准复核这一处。

---

## 1. 票 28–52 逐票判定

| 票 | 状态 | 判定 | 关键证据 / 缺口 |
| --- | --- | --- | --- |
| 28 Camera Raw 光/色/曲线 | done，3/3 勾 | ✅ | `internal/render/cameraraw.go:629` ApplyCameraRaw、`:759/:769` 两个表构建器、`:885` CurveColor；黄金 case `tests/golden/cases/camera-raw.json`、`camera-raw-curve.json`、`camera-raw-curve-channels.json`（ref PNG 齐） |
| 29 混色器/分级/细节 | done，3/3 勾 | ✅ | `cameraraw.go:1148` Effects、`:1327` Detail；case dehaze/clarity/glow/vignette/detail/mixer/grading 全部有 ref |
| 30 光学/几何/校准 | done，3/3 勾 | ✅ | `cameraraw.go:1512` LensDistort、`:1690` Optics、`:1735` Calibration、`:2080` Geometry、`:2171` ApplyCameraRawFilter；case lens/optics/calibration |
| 31 调整对话框与图像菜单 | done，4/4 勾 | ⚠️ | 入口：`filter_session.go:413` ApplyFilter、`:603` SampleLevelsPoint、`:689` LevelsAuto；调整层编辑 `LayersPanel.svelte:158-169` + `filter.go:371` ApplyAdjustment。**票面自认留待**：Hue/Sat 的 per-range hsvSettings 表（票 31 第 36-37 行） |
| 32 滤镜菜单核心 | done，4/4 勾 | ✅ | `render/filters.go:42/102/189/219/285` 五个内核；`bridge/filter.go:315` runFilterJob；case colored-vignette/tonal-contrast |
| 33 滤镜实时预览管线 | done，4/4 勾 | ✅ | `filter_session.go:213/262/320/396` Begin/Update/Commit/Cancel；测试 `filter_test.go:107/143/203/237`（选区限制/最后一次生效/提交=ApplyFilter/取消不碰像素） |
| 34 Camera Raw 面板与示波器 | done，**3/4 勾** | ⚠️ | `CameraRawPanel.svelte` + `App.svelte:407`；`filter_session.go:758` CameraRawScope、`:801/:829/:868` 三种取样；`filter_test.go:310` TestCameraRawPanelPipeline。**未勾项见 §4** |
| 35 内容感知填充/污点修复 | done，4/4 勾 | ✅ | `render/contentfill.go:14`、`render/heal.go:196`；`filter_session.go:926/954/970` 修复笔画端点；case content-fill（ε=0）、heal-spot |
| 36 抖动 | done，3/3 勾 | ✅ | `render/dither.go:199/523/550`、`dithersettings.go:205`；6 个 dither case；ASCII 字形用 x/image 位图字体（票面已登记观感差异） |
| 37 ML 主体/对象/移除背景 | done，4/4 勾 | ✅ | `internal/ml/subject.go`、`render/guidedmatte.go`；`filter_session.go:99/1050/1055`；`go.mod:8` onnxruntime_go；`docs/adr/0005` 在 |
| 38 PSD/PSB 解析器 | done，4/4 勾 | ✅ | `internal/psd/{reader,types,adjustments,builder}.go`；桥 `project_io.go` 扩展名分派 |
| 39 PSD 文本/矢量与报告 | done，4/4 勾 | ✅ | `psd/text.go`、`engine.go`、`vector.go`；`layerrender/{text,shape,fontindex,fontmap}.go` |
| 40 栅格导入 | done，4/4 勾 | ✅ | `internal/rasterio/decode.go`+`orientation.go`；前端 `state/svg.ts`；`bridge/import_images.go` |
| 41 RAW/HEIC develop | done，4/4 勾 | ✅ | `vcpkg.json`；`internal/rawio/{session,develop}.go`、`heicio/heif.go`；`bridge/raw_develop.go`；`docs/dependencies.md:29-30` |
| 42 导出 PNG/JPEG/拷贝合并 | done，4/4 勾 | ✅ | `render/export.go:20/36/66`；`internal/winclip`；前端 `state/export.ts` |
| 43 画布/图像尺寸/修剪 | done，3/3 勾 | ✅ | `render/canvasops.go:22/34/82/216`；`bridge/doc_geometry.go`（含 :94/:171 参考线平移） |
| 44 文本引擎 | done，4/4 勾 | ✅ | `bridge/text.go`、`layerrender/text.go`；前端 `state/text.ts`。票面自认 IME 手动走查未记录 |
| 45 文本样式逐 run | done，4/4 勾 | ✅ | `layerrender/fontindex.go:55` Resolve、`text.go:297` wrapHardLine、`fontmap.go`；前端 textRuns.ts/FontPicker。票面登记 6 条已知差异 |
| 46 PSD 文本往返 | done，3/3 勾 | ✅ | `psd/writetext.go:151` encodeTySh（记录级 codec，票面明确不属于导出链路） |
| 47 快捷键系统 | done，4/4 勾 | ⚠️ | 113 条定义 `table.ts`、91 条注册 `actions.ts`、22 条未接（`actions.test.ts:156-187` 钉住）；**票面正文数字过期**（§2、§3） |
| 48 剪贴板与拖放 | done，4/4 勾 | ✅ | `bridge/clipboard.go:78/171/211/262`；`winclip/dib.go`；`layer_transfer.go`；前端 clipboard.ts/dropimport.ts。票面自认 OS 手递手无自动化 |
| 49 Shell 集成 | done，4/4 勾 | ⚠️ | `bridge/recent.go`、`shell.go`、`internal/winchrome`、单实例锁；前端 `state/shell.ts`。无未勾项，但外壳 API 无自动化断言（§4） |
| 50 安装包与自动更新 | **implemented，2/4 勾** | ⚠️ | `internal/update/*`、`scripts/genfeed`、`release.ps1`、`verify-install.ps1`、`.github/workflows/release.yml`。两条未勾见 §4 |
| 51 代理契约端到端 | done，4/4 勾 | ⚠️ | `bridge/watch.go`、`internal/fsatomic`、`docs/writing-comp-files.md`、`docs/acceptance/agent-contract.md`、`scripts/agentdemo`。无未勾项，手工验收 B 节未跑（§4） |
| 52 性能与内存预算 | **implemented，3/4 勾** | ⚠️ | `internal/memory/*`、`domain/limits.go:93` DocumentPixelBudgetFor、`bridge/memory.go`、`bridge/launch.go:34/42`、`scripts/launchbench.ps1`、`docs/benchmarks/{launch.json,performance.md}`。未勾项见 §4 |

**统计**：25 张票（28–52）中 ✅ 18、⚠️ 7、❌ 0（按"验收项 vs 代码"判定）。⚠️ 的 7 张 = 31、34、47、49、50、51、52。所有未勾项都不阻塞"应用可跑"（Lead 已实测启动 0.849s）。

---

## 2. 重点一：票 47 的 22 条未接线快捷键，逐条归类

事实基线（已实测，非引用票面）：`actions.ts` 的 `shortcutActions` 注册 **91** 条；`table.ts` 定义 **113** 条（46 Menus + 58 Canvas & Layers + 9 Text Editing）；差 **22** 条，与 `actions.test.ts:156-187` 的期望集完全一致。

### A 类｜纯逻辑已完整，缺 store / 渲染 / RPC 接线（9 条）

| 条目 | 底层能力（已存在） | 缺什么 |
| --- | --- | --- |
| Menus:Transform Layer / Selection（⌘T） | `transformSession.ts:833` TransformSession 类、`:1055` beginTransform（1063 行，含拖拽/吸附/多选跟随） | Go 无 setTransform（`grep setTransform internal` = 0）、画布无变换会话入口 |
| Canvas & Layers:Apply current canvas operation（⏎） | 同上 + `crop.ts:488` CropState（beginCrop/setRect/commit） | 画布无裁剪/变换会话入口；crop 提交可复用 CanvasSize 的 offset（Go 侧已有） |
| Menus:Show Transform Controls（⌘H） | `transformSession.ts:1063` showsTransformControls | 该 store 零消费者（grep 只命中自身与 test） |
| Menus:Show Grid | `snap.ts:52` normalizeGrid、`:72` gridLines、`:84` isMajorGridLine | HEAD 下无布局网格 store；`CanvasSurface.svelte:289/605` 的 showGrid 是 ≥800% 像素网格，不是布局网格 |
| Menus:Show Guides | `guides.ts:101` GuidesState 完整（增删改/锁定/清除/拖拽生命周期/撤销逆变更） | HEAD 下无生产消费端（只有 `crop.ts:51`、`snap.ts:23`、`transformSession.ts:2` 的 type import），画布不渲染参考线 |
| Menus:Show Rulers | `guides.ts:53` overRuler（"丢回标尺删除"判定） | 无标尺组件/无 store |
| Menus:Snap | `snap.ts:221` snap()、`:115` DEFAULT_SNAP_TOGGLES、Ctrl bypass | 无总开关 store |
| Menus:Lock Guides | `guides.ts:139` setLocked、`:167` moveGuide/deleteGuide 的锁定门 | 无开关 store（HEAD） |
| Canvas & Layers:Toggle Levels preview（⌥P） | `filter_session.go:50-51` previewSrc/preview 已在会话里 | 无"显示/隐藏预览"字段或 RPC（grep TogglePreview\|SetPreview\|previewVisible = 0）、对话框无复选框 |

### B 类｜引擎原语已在，缺 bridge RPC + 历史包装（2 条）

| 条目 | 底层能力（已存在） | 缺什么 |
| --- | --- | --- |
| Menus:Fill with Foreground（⌥⌫） | `render/stampfill.go:45` FillForeground/FillBackground、`:265` FillColor（含选区裁剪；测试 `stampfill_test.go:352-459`） | `internal/bridge` 中 **零** FillColor 调用（grep = 0）→ 无 RPC、无历史包装、前端无调用 |
| Menus:Fill with Background（⌘⌫） | 同上 | 同上 |

> 这两条与票 23 的"done"冲突：票 23 验收第 2/3 条声称"填充仅影响选区、可撤销"已勾，但命令面在 bridge 层不可达；feature-parity `C15/C16`（第 52-53 行）仍 ☐。

### C 类｜真缺底层能力（10 条）

| 条目 | 证据 |
| --- | --- |
| Canvas & Layers:Cycle tool mode（Tab） | 前端无工具 mode 状态（`grep shapeKind\|toolMode\|cycleToolMode frontend/src` = 0） |
| Canvas & Layers:Cycle shape kind（⇧U） | 同上；`render/shapelayer.go` 只有栅格化原语，无形状会话/种类状态 |
| Move selected pixels ×8（⌘←/→/↑/↓ 与 Shift 版） | FloatingSelection 仅存在于 `reference/Swift/Compositor/Document/FloatingSelection.swift`；Go 与前端均无实现；feature-parity `:180` S05 记 ◐ |

### D 类｜不适用（1 条）

| 条目 | 结论 |
| --- | --- |
| Menus:Hide Compositor（⌥⌘H） | macOS 专属（feature-parity `:236-241` "明确不做"第 4 条），非缺口；若要等价物应登记到票 49 的窗口区 |

**归类合计**：A 9 + B 2 + C 10 + D 1 = **22**。
**回答任务的问题**：`guides.ts`/`snap.ts`/`crop.ts`/`transformSession.ts` 里的类**逻辑上完整可用**（纯函数 + 类语义齐、各有测试），它们属于 A 类"**只是没接线**"；`render.FillColor` 也是"有内核缺 RPC"（B 类）；**真正缺功能的是 C 类 10 条**（工具模式/形状种类/浮动选区）。所以 22 条里 **12 条是接线问题，10 条是真缺能力，1 条不适用**（A+B 与 C/D 分列）。

---

## 3. 重点二：`actions.ts` 顶部注释"票 48 未做"——**注释过期，非真未接**

- 注释位置：`frontend/src/lib/shortcuts/actions.ts:17-23`，第 18 行仍写"剪贴板像素/文字通道（**票 48 未做**）"。
- 反证：`actions.ts:51` 已 import `{ copySelection, cutSelection, layerViaCopy, paste } from "../state/clipboard"`；`:491-494` 已注册 `Menus:Cut/Copy/Paste`；`actions.test.ts:67-70` 把它们列入"关键条目已接线"。
- 底层也在：`bridge/clipboard.go:78` CopySelection、`:171` CutSelection、`:211` Paste、`:262` LayerViaCopy；`winclip/dib.go` 有 DIB 解码；`layer_transfer.go` 有跨项目复制。
- 注释里唯一仍成立的是"文字通道"：文字域由原生 textarea 自己处理（`actions.ts:303-305` textEditingOwnsClipboard），不是缺口。
- 附带：票 47 正文同样过期——`:47` 写"88 条注册 / 113 条定义，25 条"、`:69` 标题写"25 条"，而票 48 文档 `:71` 已更新为"25 → 22"。**feature-parity.md:225（P01）的"22 条"才是当前事实。**

**结论：纯注释/文档过期，代码没有断裂。** 建议修 `actions.ts:18` 与票 47 `:47/:69` 的数字，并把"文字通道"改写为"由 textarea 承载，非未接线"。

---

## 4. 重点三：票 49/50/51/52 的未勾项逐个说明（是否阻塞"应用可跑"）

| 票 | 未勾项 | 缺什么（实测） | 阻塞应用可跑？ |
| --- | --- | --- | --- |
| 34 | "Camera RawSliderTests 语义（滑杆行为）通过"（`:12`） | 全仓无 `CameraRawSlider` 的测试：`CameraRawSlider.svelte` 只被 `CameraRawPanel.svelte:12` 引用；前端**没有任何 svelte 组件测试**（`**/*.svelte.test.ts` = 0）；Go 侧只有 `filter_test.go:310` 的管线测试 | **不阻塞**。面板能渲染、参数能走预览；缺的是滑杆交互（双击复位/Alt 裁剪视图）的回归保护 |
| 50 | "安装→升级→卸载在干净环境验证"（`:9`） | `scripts/verify-install.ps1` 存在但**从未执行**：全仓无 `install-verify.txt`（glob = 0）。需要 makensis + 两个安装器 + 干净 VM | **不阻塞**。属发布验收证据缺失 |
| 50 | "应用内检查更新到新版端到端"（`:11`） | 链路每环有测试（`internal/update/*_test.go`、`frontend state/update.test.ts`），但真 GitHub Release 未发布过；票面 `:16-28` 还记录了"FeedURL 曾注入成安装器 exe 地址"的契约错（已修） | **不阻塞**。开发版未配更新源时短路（`unconfigured` 状态） |
| 51 | 无未勾项 | 手工验收 B 节 10 步未跑（`docs/acceptance/agent-contract.md` 留空）；OneDrive 占位符/长路径/杀软未实测 | **不阻塞**（票面已诚实登记） |
| 52 | "大文档基准（打开/合成/缩放）"（`:10`） | 只有 `BenchmarkComposite100MP`（`composite_bench_test.go:176`，100MP 单次 21.2s）；**打开与缩放没有基准**（票面 `:102-106` 自认） | **不阻塞**。属性能证据缺口 |
| 49 | 无未勾项 | `SetOverlayIcon`/`SHAddToRecentDocs` 真机跑过但无 CI 断言；未截到任务栏红点 | **不阻塞**（票面已诚实登记） |

> 结论：**28–51 范围内没有任何未勾项阻塞"应用可跑"**。未勾项全部是①真机/真发布环境依赖（票 50）②测试覆盖缺口（票 34、52）③手工视觉验收未记录（票 44/51）。

---

## 5. feature-parity.md 中归属 28–51 的行的偏差

| 行 | 矩阵写的 | 代码事实 | 判定 |
| --- | --- | --- | --- |
| C41（`:98`） | ☐ | `CameraRawPanel.svelte` + `App.svelte:407` + 票 34 done | ❌ 矩阵过期，应 ◐/☑ |
| C43（`:105`） | ☐ | 无"新建调整层"入口（grep New Adjustment\|addAdjustment = 0） | ✅ 准确 |
| C44（`:106`） | ☐ | `LayersPanel.svelte:158-169` 编辑入口 + `filter.go:371` | ❌ 矩阵过期 |
| A06–A12（`:126-132`） | ☐ | 票 31（图像菜单九种调整）+ 票 32（模糊/杂色）已落地；☐ 若指"调整层种类"则准确，若指"像素版入口"则过期 | ⚠️ 口径需拆清（"调整层种类" vs "破坏式入口"） |
| C15/C16（`:52-53`） | ☐ | 内核在（`stampfill.go:265`）、RPC 无 | ✅ 矩阵比票 23 更接近事实 |
| C19–C25（`:61-67`）、V03/V04（`:212-213`） | ☐ | 票 14 标 done，但 HEAD 下无 store/渲染/开关（§2 A 类） | ⚠️ 矩阵 ☐ 比票 14 的 done 更接近事实——**票与矩阵必须改一处** |
| T01（`:10`）、T05（`:14`） | ☐ | 票 17/18 标 done，但 TransformSession/CropState 零消费端 | ⚠️ 同上 |
| C48（`:110`） | ◐ 票 47 | `moveActiveLayerOutOfGroup` 前端纯函数、Go 无 op | ✅ 准确 |
| C11–C13（`:48-50`）、P02/P07 | ☑/◐ | 与 `bridge/clipboard.go`、`layer_transfer.go`、`dropimport.ts` 相符 | ✅ 准确 |
| L08（`:170`）、V09/V10（`:218-219`）、P10（`:234`） | ☑/◐ | 与 `domain/limits.go:93`、`memory.go`、`launch.go`、基准文件相符 | ✅ 准确 |

**系统性结论**：票 14/17/18/23 的 "done" 与 feature-parity 对应行的 "☐" 互相矛盾，且**矩阵那一边更接近代码事实**（纯类无消费端）。这不是笔误——是"纯函数/类写完就勾票"的流程问题。

---

## 6. 最严重的 3 个缺口（带文件行号）

1. **三套完整纯类零生产消费者，命令面整片不可达。**
   `transformSession.ts:833`（TransformSession）、`guides.ts:101`（GuidesState）、`crop.ts:488`（CropState）在 HEAD 下 grep 消费端只命中 `*.test.ts`（`transformSession` 仅 `transformSession.test.ts:36`；`crop` 仅 `crop.test.ts:20`；guides 只有 type import）；Go 侧 `grep setTransform internal` = **0**。后果：⌘T、⌘H、⏎、⌘;、⌘'、⌘R、⌘/ 等快捷键与裁剪工具全部无效，画布不渲染参考线/变换控件/裁剪框。对应矩阵 T01/T05/C19–C25/V03/V04 仍 ☐。
   *（审计期间 `view.ts`/`CanvasSurface.svelte` 正在补参考线渲染这一支，但 `SetGuides` RPC 仍缺、actions 仍未接。）*

2. **填充命令族断在 bridge RPC 层，且票 23 已标 done。**
   内核在 `render/stampfill.go:45`（FillForeground/FillBackground）、`:265`（FillColor，选区裁剪有测试 `stampfill_test.go:352-459`），但 `internal/bridge` 零调用。后果：⌥⌫/⌘⌫ 与 C15/C16 不可达；票 47 `:74` 把原因写成"底层命令本身还没落地"——**不准确**，缺的是 RPC + 历史包装。

3. **浮动选区在 Go/前端真缺实现。**
   Move selected pixels ×8（`table.ts:156-157`）无任何底层；`FloatingSelection` 仅存在于只读的 `reference/Swift/Compositor/Document/FloatingSelection.swift`；feature-parity `:180` S05 记 ◐，但票 16 标 done。这是 22 条里唯一的"真缺功能"大类。

**次要但需登记**：票 34 滑杆测试缺失（`CameraRawSlider.svelte` 无测试、前端无组件测试设施）；票 50 的 `verify-install.txt` 不存在、真 Release 未跑；票 52 大文档"打开/缩放"基准未做；`actions.ts:18` 与票 47 `:47/:69` 的注释/数字过期。

---

## 7. 对"是否阻塞应用可跑"的总结

不阻塞。7 张 ⚠️ 票（31/34/47/49/50/51/52）与 4 个未勾项都落在"功能不可达 / 测试与验收证据缺失"，没有一条影响启动链路；Lead 已实测 `build/bin/Compositor.exe` 存活、WebView2 就绪 0.849s，与本报告不矛盾。风险点在于**"票 done"与"功能可达"之间的偏差**：建议 Lead 在终验（票 53）前把票 14/16/17/18/23 的状态按 feature-parity 的 ☐/◐ 回调，或补齐接线，二者择一。
