# 04 — 文档域模型与限额

**What to build:** Go 端文档模型：文档（尺寸/分辨率/图层列表/参考线）、图层（变换/蒙版/编组/混合模式/不透明度/调整/特效/形状/文本）、扁平存储 + 父指针层级、版本化枚举（24 种混合模式、12 种调整、6 种特效、3 种形状）；DocumentLimits 全部限额与校验（编组 ≤64、剪贴链 ≤256、参考线 ≤1000 等）。

**Blocked by:** 01 — 脚手架与开发链路.

**Status:** done（2026-10-05）

- [x] 模型可表示原版 manifest 全部字段——schema 逐字段对照 reference/Swift/docs/project-format.md（v1–v11）与 writing-comp-files.md 的真实 manifest 示例提取：图层 20 个字段（imageFile/parentID/isGroup/opacity/blendMode/maskFile/maskEnabled/maskSourceID/adjustment/maskPlacement/maskLinked/shape/effects/text）、transform（origin:[x,y] 数组、size:[w,h]、sampling "High quality"）、levels{channel,ranges[4]}、curves{channel,channels[4]}、colorBalance 九通道+preserveLuminosity、blackWhite 九字段、gradientMap/grain/exposure、effects 六项、text 含 boxSize 与 v10/v11 colorRuns/fontRuns。唯一有意保留的弱类型：adjustment.hsvSettings 用 json.RawMessage 无损承载（逐 range 模型在票 26 类型化，已注释）。
- [x] 限额校验全部实现并有单测（含边界值）：画布边长、图层 ≤10000、编组深度 ≤64（64 组+叶通过/65 组拒绝）、剪贴链 ≤256 节点（256 通过/257 拒绝）、参考线 ≤1000 与 |position| ≤1e6、循环/缺失父级/父级非编组/编组带图像/ID 重复、opacity 有限 0–1、调整数值域（色相 ±360、饱和度/明度 ±100、blurRadius 0.1–250、motionAngle −90–90、motionDistance 1–2000、noiseAmount 0.1–400）、特效尺寸 0–500、文字 runs（UTF-16 单位、正长度、升序、不重叠、不越界，含代理对用例）
- [x] 24 种混合模式拼写与原版 LayerBlendMode 逐一一致（枚举级测试；"Linear Dodge (Add)"/"Color Burn" 等逐一断言；Darker Color 明确拒绝解析）
- [x] 模型序列化往返等价：黄金 manifest（覆盖全部字段）Unmarshal→Marshal→JSON 树 DeepEqual 通过；可选字段缺失时 omitempty 保证字节面不出现（nil 语义保真，版本门控读取依赖此性质）

对等矩阵：L01、L06（模型部分）、L08；为 I07 铺路。

> 备注：bridge 工作区的脚手架 Document 到票 05/06 才统一到本模型；文件级门控（版本特性门、路径安全、资产校验、排序键 pretty JSON 字节级输出）在票 05。
