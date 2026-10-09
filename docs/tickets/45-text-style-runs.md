# 45 — 文本样式（逐 run）

**What to build:** 文本样式系统：字体/字号/颜色/对齐/字距/行高；工具头设置区与行内选中范围（逐 run 字体与颜色混排）；Windows 字体目录枚举与字体选择器； PSD 字体名映射表。

**Blocked by:** 44 — 文本引擎.

**Status:** done（2026-10-10）

- [x] 逐 run 样式（colorRuns/fontRuns）编辑与渲染测试（TypeToolTests 样式语义）
- [x] 字距/行高（TextKit leading/kern 语义）与原版排版对齐（抽样对照）
- [x] 字体枚举/选择器/回退字体策略
- [x] 样式随 manifest v11 往返

对等矩阵：X02、故事 44。

## 实现说明（2026-10-10）

**逐 run 语义（TypeTool.swift:65-186）。** 纯函数在 `frontend/src/lib/state/textRuns.ts`：
`setRunColor`/`setRunFont`（空段或整段 → 改基础字段并清 runs；逐 UTF-16 码元展开后合并相等
邻居）、`uniformColorIn`/`uniformFontIn`（选区取值，混排返回 null → 选项栏显示"多种字体"）、
`reconcileRunsForEdit`（编辑后 run 跟随，含代理对与"提升为基础样式"）、`dropRuns`。44 个测试
（textRuns.test.ts）逐条对应 TypeToolTests.swift:354-497。Go 侧 `internal/domain/text.go:
ValidateRuns` + `internal/project/validate.go` + `internal/bridge/text.go` 三处同规则校验，
渲染在 `internal/layerrender/text.go`（每个 rune 用自己的面孔测宽/画字，颜色逐字形蒙版），
测试 `text_runs_test.go`。

**选项栏与选区（本票修掉的阻断缺陷）。** 选项栏的字体选择器/颜色控件一夺焦，textarea 的高亮
就塌陷，`onTextBlur` 随即把选区写成 null —— 逐 run 编辑端到端不可用。现在按原版
`InlineTextEditor.swift:26-36` 的 `holdsSelection`/`keepSelection` 保留失焦瞬间的选区
（`text.ts: heldSelection` + 4 个测试），真塌陷仍由编辑器内的 select/mouseup/keyup 写回。
提交失败也保留会话（对齐 `applyText` 的 draft 回滚，TypeTool.swift:271-281）。

**排版。** `lineHeight = leading > 0 ? leading : fontSize × 1.2`、`kern = tracking`（逐字符含
末字符）、对齐按 `width − 2×padding` 的容器宽，均与 TypeTool.swift:18/415/463-468 逐式一致
（`text_runs_test.go` 钉住 144/174/384/474 的行高样例）。**换行**原本只在 ASCII 空格断行，
中文段落会顶出框外：现在按 AppKit byWordWrapping 语义断行——空格处断、宽字符（CJK/假名/谚文/
全角）之间逐字断、单个超长词按字符边界回退（`layerrender/text.go: wrapHardLine`，
`textwrap_test.go` 5 个测试）。行内编辑器的 padding/letter-spacing/text-align 与光栅端
`TextPadding = 12` 对齐，提交前后换行位置一致。

**字体枚举/选择器/回退。** `internal/layerrender/fontindex.go` 枚举 `%WINDIR%\Fonts` 与
`%LOCALAPPDATA%\Microsoft\Windows\Fonts`（ttf/otf/ttc，TTC 取多面），按 family→subfamily→
PostScript 名排序、按 PS 名去重；`Resolve` 三级回退（PS 名 → 家族 → 归一化 → "Family-Style"
拆家族 → Segoe UI → 任意已装面孔）。`Service.ListFonts` → `state/fonts.ts`（惰性一次加载、
失败可重试）→ `FontPicker.svelte`（家族分组、搜索、方向键/回车/Esc）与
`TextOptionsBar.svelte` 的"未安装"徽标。PSD 字体名映射表见票 46（`fontmap.go`）。

**段落框缩放（X01 的尾项）。** `text.ts: resizeTextBox` 实现 8 向手柄几何（动的边跟手、对边
固定、16 下限、30000/2e8 上限、取整），画布上 8 个手柄 div 拖拽写回
`boxSize`/`editorAt`/`anchor`；点文本第一次拖手柄先按当前显示尺寸物化成段落框
（InlineTextEditor.swift:450-456）。8 个纯函数测试（text.test.ts）。

**manifest v11 往返（含修掉的一个阻断缺陷）。** colorRuns(v10)/fontRuns(v11) 的版本门、
校验、读写都在（`store_text_runs_test.go`、`bridge/text_runs_test.go`）。原先**保存不升级
版本**：打开 v9/v10 的 .comp 再给选区设逐 run 字体，保存会被自己的版本门拒绝
（`project/validate.go` 按文档声明的版本判）。现在 `project.Store.Save` 与保存路径一致地
盖章当前版本（`domain.FormatVersion`，对齐原版"每次保存都写 current"：
ProjectStore.swift:15,21），Open 仍按文件声明版本校验；回归测试
`TestStoreSaveStampsTheCurrentVersion`（v1/v5/v9/v10 + 新 run → 保存成功、读回 v11、调用方
文档不被改写）。

## 已知差异 / 留待后续（诚实登记）

1. **编辑期不是逐字符所见即所得**：行内是原生 textarea，画不出混排颜色/字体（浏览器限制），
   只画基础样式 + letter-spacing/text-align；提交后由 Go 按 colorRuns/fontRuns 画完整结果
   （原版每帧重光栅，EditorCanvas.swift:31-33）。
2. **字体选择器悬停预览缺失**（原版 TypeTool.swift:365-381 的 previewFont/endFontPreview）。
3. **"已安装"判定两套**：前端 `isInstalled` 只精确匹配 family/PS 名，Go `Resolve` 还会归一化
   与拆 "Family-Style" —— 例如 "Helvetica-Bold" 前端报未安装而 Go 能解析。
4. **像素操作不清 `layer.Text`**：原版会失效可编辑文字（TypeTool.swift:216-221），Go 侧笔刷/
   滤镜/几何都不清，.comp 里会留下"已被重绘却仍标可编辑"的文字元数据。
5. 字体索引把每个字体文件字节常驻内存（fontindex.go:191），全机字体多时占用可观。
6. `domain/validate.go` 的 `validateText` 是 `domain/text.go: ValidateRuns` 的弱化重复
   （当前不可达，但会分叉）。
