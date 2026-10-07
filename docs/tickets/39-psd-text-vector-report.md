# 39 — PSD 文本/矢量与转换报告

**What to build:** PSD 文本层（`PSDText` 682 行语义：attributed runs、字体名映射、横排文本保留可编辑）与矢量数据（填充矩形/椭圆保持可编辑、其余矢量栅格化）；导入转换报告 UI（逐项列出：保留/降级为像素，应用前确认）。

**Blocked by:** 38 — PSD/PSB 解析器.

**Status:** done（2026-10-07）

实现注记：internal/psd/text.go（TySh 描述符读取器：版本/2×3 变换/描述符/变形描述符 + EngineData PostScript 子集解析器 engine.go + 样式提取：字号/字体索引/填充色[alpha r g b]/字距/行高/两端对齐提示/多 run 签名比较；竖排与剪切/非均匀缩放保持栅格）+ vector.go（vogk 活形状：矩形/圆角/椭圆；vmsk 四锚点尖锐矩形兜底；无存储像素时按路径重绘填充+描边）。internal/layerrender：Windows 字体索引（WINDIR\Fonts + LOCALAPPDATA，PostScript/家族/归一化名 + 系统字体回落）与文本排版光栅（按词折行、固定行高、基线=lineHeight−descent、对齐锚点；x/image sfnt+vector 光栅化——与 AppKit 的排版细节为语义对等而非逐位一致）、形状光栅（矩形/圆角/椭圆/线）、矢量路径光栅（三次贝塞尔展平 + 填充与描边两遍）。builder：文字图层保留 domain.TextStyle 并渲染像素，活形状保留 domain.ShapeStyle；报告文案与原版逐条对应（warp 省略/伪粗斜体省略/仅首个样式/两端对齐回落/字体缺失/描边省略/矢量已栅格化）。bridge：待确认 PSD 导入（pendingPSSDOpen + Confirm/Cancel 端点），前端转换报告表单在应用前确认（替换原 psdConversions 事件）。测试：11 个新用例（引擎样式/竖排拒绝/剪切拒绝/旋转保留/提示语/段落框 BoxSize/活形状/圆角/路径重绘/描边提示/椭圆/可编辑元数据/字体缺失报告/空内容回落）。对等矩阵 I05/X04。

- [x] 文本 run 解析与字体映射测试（PSDTextTests 语义分批；TySh 描述符 + EngineData 字典 + 多 run 签名比较出"仅保留首个样式"提示）
- [x] 填充矩形/椭圆保留为形状层；其余矢量栅格化（vogk 活形状 + vmsk 尖锐矩形兜底 + 无像素层按路径填充/描边重绘）
- [x] 转换报告列出全部降级项并在应用前确认（OpenProjectDialog 回 pending+conversions，前端 PsdConversionSheet 确认后才应用）
- [x] 横排文本可编辑、竖排文本栅格化（Ornt=Vrtc 拒绝；剪切/非均匀缩放同样保持栅格）

对等矩阵：I05（文本/矢量/报告）、X04（解析侧）。
