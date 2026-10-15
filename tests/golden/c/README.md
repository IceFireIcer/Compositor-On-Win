# C 内核拷贝件与驱动（黄金基准 harness）

拷贝自 `reference/Swift/Compositor/Rendering/`。reference 区保持只读；
本目录的 .c/.h 拷贝件与源文件逐字节一致（只多文件头的来源说明注释，可用 diff
校验），AGENTS.md 不变量 #4。

## 两份来源提交

| 拷贝件 | 来源提交 |
| --- | --- |
| LevelsPixels、BrushPixels、LensPixels、NoisePixels、WandPixels | `865e4675d09de64c3e4ad90aeacee8997542f4ae`（docs 基线） |
| ScanlinesPixels（票 60） | `e8c9e29701528413cc0e608d61187c1c61790add`（v1.5.1-8-ge8c9e29，子模块当前 pin） |
| AdjustPixels（票 63）、HealPixels + PatchPixels（票 65） | `e8c9e29701528413cc0e608d61187c1c61790add` |
| DeepPixels（票 68 拷贝，票 71 接入驱动） | `e8c9e29701528413cc0e608d61187c1c61790add` |

`865e467` 早于这些内核的最后一次改动，而 `e8c9e29` 是子模块的当前 pin：
两组拷贝因此各自注明自己的来源提交。票 63/64/65 把改过的内核整体搬到 pin，
所以 harness 比较的是**当前上游的算法真值**，而不是 1.4.5 的行为。

## 票 60 新增：ScanlinesPixels.{c,h}

1.4.8 把 Scanlines 从 Dither 风格拆成独立滤镜，新增了 `ScanlinesParams` 与
`scanlines_apply`；`DitherPixels.{c,h}` 在票 64 已随 pin 更新，但两者仍是
不同文件。`driver_main.c` 有 `scanlines` 命令分支驱动它。

## 票 63：AdjustPixels.{c,h} 搬到 pin

1.4.9 起 `adjust_camera_raw` 改为消费一张**合成查表**（`CameraRawStage` +
`camera_raw_compose`，测量数据来自 `CameraRawTables.bin`），`camera_raw_statistics`、
`camera_raw_scope`、`camera_raw_table_curves` 等随之新增；`adjust_camera_raw_curve_color`
多了 `mixerTable`/`gradeTable`/`grid`，`adjust_camera_raw_optics` 多了
`exposureTables`/`tableSize`，`effects_vignette` 与去雾/纹理/清晰度/锐化/色彩降噪重写。

- 曲线、混色、分级、光学四个命令按新签名传 **NULL 表**：源码里 `if (mixerTable)`、
  `if (gradeTable)`、`!exposure` 都是空表即原路径，因此这些 case 仍逐字节可比。
- `camera_raw`（Light/Color 组）**无法再驱动** —— 它现在必须有一张测量表。该 case
  与参考图退役到 `../legacy/`，数值变化记录在 `../legacy/README.md`。
- 镜头暗角校正现在读曝光查表（票 78 的数据），`camera-raw-optics` 因此把暗角滑块保持 0。
- 去雾/纹理/清晰度/锐化/色彩降噪的参考图**已按新算法重新生成**，Go 端口逐字节对齐。

## 票 64：DitherPixels.{c,h} 增量（**不是**整文件替换）

1.4.10 起 RAW 的 16→8bit 量化搬到 C：新增 `dither_quantize16`（哈希噪声 + 两次均匀
抽样 + 夹到自身 alpha）。

这里与其余拷贝件不同：**整文件替换会破坏既有基准**。现行拷贝是 1.4.8 把 Scanlines
拆出去**之前**的版本，内含内联的 `DITHER_SCANLINES`（`cases/dither-scanlines.json` 的
style=10 正是它）；换成当前 reference 会让该枚举整体前移、参考图必然变化，而票 60 的
`ScanlinesPixels.{c,h}` 已经承担了拆分后的扫描线真值。因此本文件只**增量插入**
`hash_noise` + `dither_quantize16`（含头文件原型），插入块与 reference 逐字节一致，
其余部分仍是 `865e467` 的那份；文件头写明了这一点。既有 dither 参考图**逐字节未变**。

`driver_main.c` 另有 `quantize16` 命令：它把 harness 的 8bit 输入按 ×257 精确展成 16bit
再调 `dither_quantize16`，所以 `quantize16-ramp` 这个 case 仍由 `gen` 生成、ε=0。
真正的 16bit 夹具（含预乘夹取段）另存 `internal/render/testdata/quantize16_*.raw`，
由 `TestQuantize16MatchesC` 逐字节比对，配方见 `internal/render/testdata/quantize16.md`。

## 票 65：PatchPixels.{c,h} 新增，HealPixels.{c,h} 搬到 pin，ContentFill 退役

1.4.10 用多尺度 PatchMatch 重建了内容感知填充：`ContentFill.c` 被删除，新增
`PatchPixels.c`（`patch_fill`），`HealPixels.c` 把 `heal_solve` 公开为 `heal_membrane`
并让 `spot_heal` 的 mode 0 走 `patch_fill`。

- 新拷贝件 `PatchPixels.{c,h}`（来源提交 `e8c9e29`）；`HealPixels.{c,h}` 同步搬到 pin。
- `driver_main.c` 的 `content_fill` 命令换成 `patch_fill maskPath seed`。
- 旧 `ContentFill.{c,h}` 与 `content-fill` 的 case/参考图退役到 `../legacy/`。
- 新 case：`patch-fill-curve`（弯曲地平线 + 圆盘洞）、`patch-fill-texture`（繁忙纹理填到
  边缘）、`patch-fill-blend`（旧样本的替代）、`heal-texture`（污点修复 mode 0 在繁忙纹理上）。
  `golden.go` 的输入生成器新增 `curve`、`texture`，蒙版新增 `maskShape: "disc"`。
- `patch_fill` 用 `expf`/`lrintf`，跨库最后一位不可复现，三个 case 声明 ε=1。

## 票 71：DeepPixels.{c,h} 接入驱动

票 68 把 `DeepPixels.{c,h}` 原样拷进来（只加来源头），但当时没有驱动。票 71 移植了
`deep_take_painted` / `deep_take_painted_hdr`，并把 HDR 内核也接上——`deep_rows` 用的
`dispatch_apply` 由既有的串行垫片承担，所以拷贝件仍是逐字节一致的原件。

`driver_main.c` 新增五个命令（`deep_take_painted`、`deep_take_painted_hdr`、`hdr_normalize`、
`hdr_restore`、`hdr_exposure`）。它们的输出不是 8 位 RGBA（16 位或 half-float 位型），
所以驱动加了 `alt_bytes`：命令自己填这份缓冲，`out.raw` 照旧拿到内核输出。
16 位输入从额外路径参数读，不走 8 位 `in.raw`。

夹具与配方在 `internal/render/testdata/deep_kernels.md`，比对由 `TestDeepKernelsMatchC` 完成；
平时跑测试只读已提交的夹具，不需要编译器。

**再生成入口与其它内核不同**（ADR-0003 §2 的落差，已登记）：`go run ./tests/golden/gen`
只覆盖 `cases/` 里那些 8 位 RGBA 进出的内核，**不会**重生成 deep/HDR 夹具——它们的输出是
16 位或 half-float 位型，harness 的 PNG 管线装不下。那批夹具走另一条命令：

```
GOLDEN_REGENERATE=1 go test ./internal/render/ -run TestDeepKernelsMatchC
```

（仍需先按上面的命令编译 `golden_driver.exe`。）

## 与源文件的差异（仅在拷贝件旁，不改源）

- `dispatch/dispatch.h` + `golden_dispatch.c`：Windows 最小 GCD 垫片。
  DitherPixels.c / PatchPixels.c 用到 `dispatch_apply`；垫片以**串行、按序**方式执行
  block（小画布下正确且可复现），并附带一个 20 行的迷你 blocks 运行时
  （llvm-mingw 不含 libBlocksRuntime；串行调用下 isa/拷贝助手不会被真正使用）。
- `driver_main.c`：极小 C 驱动——argv 传参、raw 预乘 RGBA 文件出入，无 JSON
  /图像库依赖。其中 build_levels_tables / build_exposure_table /
  build_gradient_map_table 三个表构建器是 Swift 侧表生成逻辑的 C 移植
  （C 内核只消费成品表），与 Go 侧 `internal/render/lut.go` 同源对照。

## 构建与再生成

需要 clang/gcc（`-fblocks`；llvm-mingw 已验证）。仓库根目录下：

```
gcc -O2 -std=gnu99 -fblocks -I tests/golden/c \
    -o tests/golden/c/golden_driver.exe tests/golden/c/*.c
go run ./tests/golden/gen
```

`gen` 逐 case 程序化生成输入（与 Go 测试同一份 BuildInput 代码，无输入图
入库）→ 调驱动 → 把输出 raw 逐字节包成 `ref/*.png` 提交。复核可复现性：
重新生成后 `git diff tests/golden/ref` 应为零差异。
