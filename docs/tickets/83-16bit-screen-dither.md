# 83 — 16 位与 HDR 的屏幕抖动预览

**What to build:** 16 位和 HDR 图层在 8 位屏幕上平滑显示，显示副本的抖动不反向修改文档像素。

**Blocked by:** 68 — 决策门槛与 .comp v12 / 16 位图层存取; 70 — 决策门槛与 .comp v13 / HDR 图层导入; 11 — WebGPU 合成器。

**Status:** done（2026-10-16）—— 三条验收**此前已由票 64/68/70 落地**，本轮逐条核对并补上缺的一条钉子。

（票面原标 `blocked-by-decision`；三个阻塞票 68/70/11 均已完成，ADR-0007 已定 16 位/HDR 像素，
本轮核对下来没有发现待决事项。）

- [x] 16 位 sRGB 与 extended-sRGB HDR 各使用受控的 8 位显示副本；高光映射可重复。
      16 位：`DeepRaster.ToBitmap` → `Quantize16`（`deep.go:132-145`）；
      HDR：`Wide16()` 把超过白的高光夹到白、并按自身 alpha 钳制（`TestHDRDisplayCopyClipsToWhite`）。
      两者都是**纯函数**，同一份深层两次推导逐字节相同。
- [x] 缩放、平移和调整层预览不出现明显条带或闪烁，显示副本不会写回源层。
      噪声是 `quantizeHashNoise(x, y, c)` —— **位置哈希**而不是随机数，所以是固定图案、
      反复推导不闪（`TestQuantize16IsFineAndRepeatable`）；无条带由
      `TestQuantize16DitheredGradientFollowsTheTonesNotTheSteps` 覆盖。
      「不写回」本轮**补了一条钉子**：`TestTheDisplayCopyNeverWritesBackIntoTheDeepRaster`。
- [x] 以 `DeepLayerTests` 的 screen dither 和 HDR full preview 语义建立黄金样本。
      screen dither：`TestQuantize16MatchesC`（对 `DitherPixels.c` 的 `dither_quantize16` 逐字节）；
      HDR：`hdr_normalize` / `hdr_restore` / `hdr_exposure` 三份 C 黄金（`deep_golden_test.go`）。

## 本轮补的一条

`TestDeepToBitmapIsTheDitheredCopy` 只钉了「副本是抖动过的」，**没钉住「不写回源层」**——
而那是验收 2 明确要求的一条。新增的测试断言四件事：推导不动深层、两次推导逐字节相同、
改副本不回写深层、副本本身不挂 `Deep`（8 位消费者拿不到深层，也就无从写回）。

反向验证过：给副本挂上 `Deep` 后测试立刻失败。

对等矩阵：U30。原版锚点：`DeepLayers.swift`、`GPUCanvas.swift`、`DeepLayerTests.swift`；提交 `905efc5`、`5a2e57f`。
