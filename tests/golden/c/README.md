# C 内核拷贝件与驱动（黄金基准 harness）

拷贝自 `reference/Swift/Compositor/Rendering/`（LevelsPixels、AdjustPixels、
BrushPixels、ContentFill、DitherPixels、HealPixels、LensPixels、NoisePixels、
WandPixels 九个内核），**来源提交 `865e4675d09de64c3e4ad90aeacee8997542f4ae`
（docs 基线，reference 自入库后未改动）**。reference 区保持只读；本目录的
.c/.h 拷贝件与源文件逐字节一致（可用 diff 校验），AGENTS.md 不变量 #4。

## 与源文件的差异（仅在拷贝件旁，不改源）

- `dispatch/dispatch.h` + `golden_dispatch.c`：Windows 最小 GCD 垫片。
  DitherPixels.c 唯一用到 `dispatch_apply`（`#include <dispatch/dispatch.h>`）；
  垫片以**串行、按序**方式执行 block（小画布下正确且可复现），并附带一个
  20 行的迷你 blocks 运行时（llvm-mingw 不含 libBlocksRuntime；串行调用下
  isa/拷贝助手不会被真正使用）。
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
