# 38 — PSD/PSB 解析器

**What to build:** 手写 PSD/PSB 解析器移植：文件头/图层记录/通道（RLE + Zip）、合成与图层像素、混合模式映射、`levl`/`curv`/`hue2` 调整数据→活动调整图层、超大层裁剪到画布；8bit RGB 限定（CMYK 拒绝并提示）。

**Blocked by:** 04 — 文档域模型与限额; 05 — .comp 存取校验与互开.

**Status:** done

- [x] 原版 PSD 内存夹具族（PSDFixture 程序化字节流）语义移植全部通过
- [x] PSB（大文件版）打开正确（PSBImportTests 语义）
- [x] PSD 调整数据转为活动调整层（PSDAdjustmentTests 语义）
- [x] 非法/不支持变体给出可操作错误而非崩溃

对等矩阵：I05（解析侧）。

**实现记录**：
- internal/psd 包：types（PSDRecord/Document/Conversion + 混合键映射 24 模式，
  Dissolve/Darker Color/Lighter Color 有意缺席→Normal+报告）/reader（光标、
  文件头、图像资源 1005 分辨率、图层记录、luni/iOpa/lsct、通道解码、自底
  向上装配）/adjustments（levl 百分比伽马 292 字节、curv v1/v4 端点补全、
  hue2→hsvSettings 原始 JSON——与 render cube 解析同编码）/builder（→
  domain.Document+资产库：编组 lsct 顺序、蒙版补丁放回层网格+默认值、剪贴
  baseForParent、组透明度 1.1.6 语义、裁剪报告）。
- PSDFixture 移植（fixture_test.go）：程序化字节流（头部/资源/图层段长度
  回填/luni/lsct/RLE 合成图/PackBits 编码），四族测试 12 项全绿——
  PSDRoundTrip（顺序/可见性/透明度/混合、编组蒙版剪贴、lsct [3,1]、超大
  拒收、spot 通道跳过、压缩 99 拒收、magic、diss→报告+Normal、sLit 直入、
  组透明度 128/255、坏头四态）；PSBImport（PSB≡PSD 内容、画布越界改写拒收、
  LMsk 大块不吞 luni Unicode 名）；PSDAdjustment（levl 百分比、hue 主区+八
  区+着色、蒙版补丁落位画布 ASCII 校验）；CropToCanvas（预算内保留越界层、
  超预算裁图像+蒙版到画布+报告）。
- 移植中抓出的错误：C 式 defer 在 Go 里函数级执行——decodeChannels 的每
  通道光标推进必须显式调用（原 defer 写法让所有通道都从首个通道头读压缩
  标记）；rawLayer.fill 默认 255（透明度=opacity×fill）；类型化 nil 指针
  赋给接口让"有调整记录"判断恒真；layerFile 夹具缺外层段长度、画布 0×0。
## 拼合 PSD（没有图层段）此前静默变成空文档（2026-10-16 修复）

`reader.go` 在图层段长度 < 4 时直接返回**零图层**，**根本不读文件尾的图像数据段**。

后果不是报错：打开一个正常的拼合 PSD，用户得到的是一个**空文档**，没有任何提示 —— 比报错更难查。
这条是上一轮做 PSD 的 ICC 配置时顺带发现的，与颜色空间无关。

修法（`internal/psd/merged.go`）：图层段为空时读图像数据段并合成一个背景层。图像数据段的布局与
图层通道不同 —— 一个压缩方式字，然后**按通道平面**排（不是交错），所以单独读，不复用
`decodeChannels`（那条与图层记录绑定）。支持无压缩与 PackBits RLE（PSB 的行字节数是 4 字节）。

| 用例 | 覆盖 |
| --- | --- |
| `TestFlattenedPSDBecomesABackgroundLayer` | 无压缩的拼合 PSD → **一个**背景层，四个像素逐个正确 |
| `TestFlattenedPSDWithRLECompression` | RLE 的拼合 PSD → 一样读得出来 |

**反向验证过**：把返回的图层列表改回 `nil`，两条立刻失败（「拼合 PSD 应当读成一个背景层，得到 0 层」）。

### 顺带修掉两处**不完整的夹具**

上一轮那两个 ICC 配置的夹具**没有图像数据段**（PSD 规范要求必须有）—— 它们此前能过，只是因为读取器
在图层段为空时提前返回，根本走不到那里。这次的改动让它们暴露出来：`ErrTruncated`。
已补全夹具。**夹具当时是「恰好能过」，不是「符合格式」** —— 这类夹具会在实现变化时突然失效，
而失效原因看起来像是新代码的错。

- 桥接线：OpenProjectDialog 文件过滤器加 .psd/.psb，扩展名分派 loadAny→
  psd.Read+Build→Workspace.OpenDocument；转换报告经 psdConversions 事件
  推给前端（App 监听打印）。文字/矢量/智能对象层按 Photoshop 存储的像素
  导入+转换说明——实时重排（PSDText/PSDVector 682+282 行）不在票 38 范围，
  随文字工具批次评估。