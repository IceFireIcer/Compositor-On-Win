# 46 — PSD 文本往返

**What to build:** PSD 文本层导出/导入往返：从样式对象重建 PSD 文本记录、字体名跨平台映射（Windows 字体 ↔ Photoshop 字体名）、与 39 号票的导入侧对齐测试。

**Blocked by:** 45 — 文本样式（逐 run）; 39 — PSD 文本/矢量与转换报告.

**Status:** done（2026-10-10）

- [x] 导入→导出→再导入的文本语义等价（逐 run 保持）
- [x] 字体缺失时的回退与报告提示
- [x] PSDRoundTripTests 文本族语义通过

对等矩阵：X04、I05（文本导出侧）。

## 实现说明（2026-10-10）

**"导出"在本应用里的含义（先把范围钉死）。** 对等矩阵 X04 的原话是"模型→PSD 不适用
（本应用不写 PSD）"，原版 `reference/Swift/Compositor/IO/PSD` 也只有 reader。所以本票的导出侧
交付的是**记录级 codec**：`internal/psd/writetext.go` 的 `encodeTySh` 从
`domain.TextStyle` 重建 Photoshop 的 TySh 文本记录（2×3 矩阵、versioned descriptor、
EngineData 的 StyleRun/RunArray、RunLengthArray、FontSet、段落 Justification、bounds/
boundingBox 段落框、Ornt/Txt），再用自家 reader 读回。文件头已写明它**只被 roundtrip 测试
使用、不属于任何导出链路**——不去编造一个生产调用方来消灭"死代码"告警。

**真·往返（验收 1 的字面含义）。** `TestTextRoundTripImportExportImport`：Photoshop 形状的
多 run TySh（段落框 224×104、三个 StyleSheet、相邻同值 run、内容 `汉字🌏abc` 7 个 UTF-16
单元）→ parse → encodeTySh → 再 parse，断言两次样式全字段等价、锚点与段落框标志一致、
`MapToWindows(SegoeUI) == "Segoe UI"`（家族没丢）。三条**已文档化的归一化**显式写在
`normalizeRoundTripStyle` 里并由 5 个子用例单独钉住：字体名写成 PostScript 拼写（Segoe UI →
SegoeUI）、覆盖 unit 0 的 run 折进基样式、相邻同值 run 合并。tracking 是浮点往返
（`t = size × tracking / 1000`），测试用误差界断言（1e-9）而不是精确相等，并证明第二次往返
不再漂移；`sameTextStyle` 相应改成带容差比较。

**逐 run 保持的边界（票 39 之后新增的 op）。** 写侧：相邻同值合并、与基样式相同的冗余 run
不落字、run 起点落在代理对中间时整 rune 取值（不产出非法 run）、RunLengthArray 短于 RunArray
时回退 StyleSheet 的 RunLength。导入侧：相邻同值 run **不**合并（钉住票 39 的已验收行为，注释
说明与写侧不对称）、越界 run 丢弃、"与基础样式相同则不产生 run"的门。

**读取侧对齐（`internal/psd/text.go`）。** `textStyleValid` 现在复用
`domain.TextStyle.ValidateRuns`（TypeTool.swift:35-62 的 colorRuns/fontRuns 有效性），内容上限
用 `domain.TextUTF16Len`（与 project/validate.go 同单位）——此前畸形 PSD（run 字体名 201 字符、
含换行、10 万 emoji）能导入却存不下；现在整层保持栅格。`engineColor` 补齐原版
PSDText.swift:222 的两值灰分支（`[0.25 1]` → 灰 0.25）。`internal/layerrender/text.go` 的渲染
门禁同样改用 UTF-16 计数。

**字体缺失的回退与报告。** `builder.go: substituteFaces` 只替换**绘制副本**，存储样式保留
Photoshop 名字（与原版一致，未改文件语义）；`missingFontNotes` 覆盖基础字体与每个 FontRun
（去重），未映射 → "字体"X"未安装，文字以系统字体绘制。"（逐义对应
PSDText.missingFontNote），有映射 → "已按"Y"替代绘制"（原版没有映射表，这是超集）。图层仍然
可编辑、原名保留，两条路径都有不依赖本机字体的测试。`fontmap.go` 正向映射补到 32 条、
反向 PostScript 名补到 10 个家族（目标逐个用本机 425 个 face 人工核对，这一步没有测试落点；
测试只保证"每个 target 在期望表里且有条目"），
`TestMapToPostScriptCoversEveryTarget` 从空断言换成显式期望表，新增
`TestMapToPostScriptIsIdempotent`。

**PSDRoundTripTests 文本族（验收 3）。** 参考 10 条用例一一对应，无缺失：点文本可编辑、
字号按矩阵缩放而不按文档分辨率、首段样式 + 备注、仅 leading/横向缩放差异的备注、段落框
boxSize、超大段落框保持像素、warp/faux 备注、竖排/损坏保持像素、缺字体报告但保持可编辑、
旋转角度；其中 oversized 段落框的断言并入段落框用例，另补了 300 PPI 全链路与
"未映射字体仍可编辑"。

## 留待后续（诚实登记）

1. `encodeTySh` 是记录级 codec：将来若真要做"导出 PSD"，入口是新增的导出功能，不是本票。
2. 字体索引把字体文件字节常驻内存（票 45 已登记）。
3. 导入侧 run 字体索引越界/名字非法时静默丢该 run 的字体（不产生备注）；行为已用注释写明。
