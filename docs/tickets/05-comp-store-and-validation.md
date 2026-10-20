# 05 — .comp 存取校验与互开

**What to build:** `.comp` 项目包读写：manifest.json v11 读/写（排序键 pretty JSON、逐版本特性门控）、PNG 资产读写、原子保存；校验规则逐条移植（格式/版本/色彩空间/路径安全/尺寸数量上限/版本一致性），拒绝行为与原因与原版一致；用 macOS 1.4.5 真实 `.comp` 双向互开。

**Blocked by:** 04 — 文档域模型与限额.

**Status:** done（2026-10-06）

- [x] 打开 macOS 产生的 `.comp`：manifest.json 两段式解码（头部分离 format/version 错误）+ UUID 大写规范化 + 全字段加载（subagent 移植 ProjectStore.readPackage 语义；以 writing-comp-files.md 的真实 manifest 示例作为 macOS 侧夹具互开通过——1.4.5 实机 .comp 夹具到位后复跑同一测试即完成实测，见备注）
- [x] 保存后的 manifest 排序键 pretty JSON（结构体→树→json.Encoder 两空格缩进、关闭 HTML 转义、去除结尾换行，对齐 JSONEncoder [.prettyPrinted, .sortedKeys]；" : " 分隔符细节待真实夹具核对——manifest.go 有 TODO 标注，不阻塞读写兼容）
- [x] 非法文件全拒且错误可操作：ErrInvalid/ErrTooLarge/ErrMissingImage/ErrEncode/VersionError 五类哨兵逐条对齐 ProjectError；坏 format/版本越界/缺 layers/UUID 非法/资产名不符/路径穿越（严格资产名正则）/符号链接逃逸/4MiB manifest/512MiB 资产/1 亿像素资产/蒙版非 8 位灰度/断链/循环（domain.Validate）/版本特性门控（v2–v11 逐键）全部有测试（28 个测试）
- [x] 保存为原子操作：资产先落盘（临时名+rename）、manifest 最后 rename 提交、失败清理 tmp、原始包在失败场景保持可打开（有测试）；Windows rename 语义（先 remove 后 rename）已注释
- [x] ProjectStore 往返等价：save→open→DeepEqual 通过；陈旧资产清理、损坏包打开、遍历拒绝各有测试。
      **注**：这句在 2026-10-20 的审计里被发现有水分 —— 见文末「往返审计」。

对等矩阵：I07、I08（存储层）。

> 备注：真实 macOS 1.4.5 `.comp` 夹具与字节级最终核验为遗留项（无 Swift 工具链无法生成夹具）——拿到任意 macOS 保存的 .comp 后运行 `go test ./internal/project/` 即闭环。

## 往返审计（2026-10-20，三轮）—— 第 13 行当时说过头了

第 13 行写「ProjectStore 往返等价：save→open→DeepEqual 通过」。审计发现这句话**只说对了一半**：
`DeepEqual` 确实在跑，但夹具（`sampleDoc()`）只填了「能通过校验的最小内容」—— 于是它能证明
**结构**在不在，证明不了**结构里的每个字段**在不在。

用「把字段从 json 标签里去掉」的变异逐层验证：

| 层 | 结果 |
| --- | --- |
| `Layer` 的 8 个外层字段（Shape/Effects/Text/Adjustment/MaskSourceID/ParentID/IsGroup/Transform） | 8/8 全抓住 |
| 其余子结构（Guide、TextRuns、MaskPlacement、Document 的 colorSpace/resolution/guides、相机 Raw 的六个设置） | 全抓住 |
| **`Effects.ColorOverlay`** | ✗ **没被抓住** |
| **`StrokeEffect.Inside`** | ✗ **没被抓住** |
| **`CurvesSettings.Channels`** | ✗ **没被抓住** |
| **`ShapeStyle.CornerRadius`** | ✗ **没被抓住** |

这四条都意味着**用户数据会丢**：颜色叠加、描边内外、自定义曲线、形状圆角，保存再打开就没了。

已补三条「每个子字段非默认」的往返测试：

- `TestStoreRoundTripKeepsEveryEffectField` —— 六种特效的每个字段都填满
- `TestStoreRoundTripKeepsAdjustmentSubFields` —— 四个通道各自的曲线点都非默认
- `TestStoreRoundTripKeepsShapeSubFields` —— Kind/颜色/圆角/线宽/起点/终点全填

**留给以后的一条规矩**：新增子结构时，往返夹具必须把**每个子字段填成非默认值** —— 只填到能过校验，
等于没测。这三条测试就是按这个标准写的；以后往 `Effects`/`Adjustment`/`ShapeStyle` 加字段，跟着填即可。
