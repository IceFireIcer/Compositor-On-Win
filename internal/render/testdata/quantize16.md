# Quantize16 fixture（票 64）

`quantize16_wide.raw` 是 32×24 的 16bit 预乘 RGBA 输入（紧凑排列，
6144 字节，小端），`quantize16_expected.raw` 是 C 内核
`dither_quantize16` 对它的输出（3072 字节）。

来源：`tests/golden/c/DitherPixels.c`——逐字节拷贝自
`reference/Swift/Compositor/Rendering/DitherPixels.c`（来源提交
`e8c9e29701528413cc0e608d61187c1c61790add`，v1.5.1-8-ge8c9e29）里的
`hash_noise` / `dither_quantize16`。

## 复现

本机（llvm-mingw）用临时探针驱动 C 内核；探针本身不入库，步骤如下：

1. 编译一个只有 `main` 的探针，链接 `tests/golden/c/DitherPixels.c`
   与 `tests/golden/c/golden_dispatch.c`（后者的 `dispatch_apply` 是
   串行垫片，`dither_quantize16` 只用它分带，串行输出与并行逐字节相同）：

   ```
   gcc -O2 -std=gnu99 -fblocks -I tests/golden/c/dispatch -I tests/golden/c \
       -o probe.exe probe.c tests/golden/c/golden_dispatch.c tests/golden/c/DitherPixels.c
   ```

2. 生成输入：24 行分三段——0–7 行是水平渐变（含 R/G/B 全范围），
   8–15 行是 32768 平坦灰，16–23 行是逐像素哈希随机、且颜色值大于自身
   alpha（考预乘夹取）。写小端 uint16。
3. `probe.exe wide.raw expected.raw 32 24`。

`TestQuantize16MatchesC` 读这两份夹具逐字节比对 Go 的
`render.Quantize16`。夹具入库是为了让比对不依赖本机 C 工具链。
