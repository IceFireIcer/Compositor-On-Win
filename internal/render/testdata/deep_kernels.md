# deep/HDR 内核夹具（票 71、82）

`TestDeepKernelsMatchC` 把 `DeepPixels.c` 的五个内核与 Go 移植逐字节对拍；
`TestDeepRunKernelsMatchC` 对拍票 82 的 `deep_split` / `deep_merge`（原版 `DeepLayer.run` 的两个内核）。
夹具由 C 驱动生成，不手写；配方在这里，重新生成见下。

## 内核与夹具

| C 内核 | Go | 输入 | 期望输出 |
| --- | --- | --- | --- |
| `deep_take_painted` | `render.DeepTakePainted` | `deep_base.raw` + `deep_painted.raw` | `deep_take_painted_expected.raw` |
| `deep_take_painted_hdr` | `render.DeepTakePaintedHDR` | 同上 | `deep_take_painted_hdr_expected.raw` |
| `deep_hdr_normalize` | `render.HDRNormalize` | `hdr_input.raw` | `hdr_normalize_expected.raw` + `hdr_scales.raw` |
| `deep_hdr_restore` | `render.HDRRestore` | `hdr_normalize_expected.raw` + `hdr_scales.raw` | `hdr_restore_expected.raw` |
| `deep_hdr_exposure` | `render.HDRExposure` | `hdr_input.raw`（scale 0.25） | `hdr_exposure_expected.raw` |
| `deep_split` | `render.DeepSplit` | `deep_run_input.raw`（step 0、channel −1） | `deep_split_expected.raw` |
| `deep_roundtrip`（`RunDeep` 非混色） | `render.RunDeep` | `deep_run_input.raw` | `deep_roundtrip_expected.raw` |
| `deep_roundtrip`（`RunDeep` 混色） | `render.RunDeep` | `deep_run_input.raw` | `deep_roundtrip_mixed_expected.raw` |

16 位夹具是小端 `uint16`，4 个一份（HDR 的是 half-float 位型）；`hdr_scales.raw` 是小端 `float32`。
画布 32×24。

## 输入怎么造的

- `deep_base.raw`：8 位预乘 RGBA 斜坡，alpha 随位置变化（覆盖带 alpha 的路径）。
- `deep_painted.raw`：`deep_base.raw` 上盖一块矩形（模拟笔触：改过的像素不同，其余相同）。
- `hdr_input.raw`：half-float，值 0.25 起每列 +0.15（最高约 4.9，即高于白），每 5 个像素有一个 alpha 0.5。
- `deep_run_input.raw`：16 位预乘，颜色值 8 位放不下，alpha 随位置变化（覆盖 `room` 的钳制分支）。
  驱动对 deep 命令仍要求一个 8 位输入，所以同目录下也写了 `deep_run_base.raw`。

三者都由 `deep_golden_test.go` 里的 `deepGoldenBase` / `deepGoldenPainted` / `deepGoldenHDR` 生成，
所以 Go 与 C 从同一份输入出发。

## 重新生成

```
gcc -O2 -std=gnu99 -fblocks -I tests/golden/c -o tests/golden/c/golden_driver.exe tests/golden/c/*.c
GOLDEN_REGENERATE=1 go test ./internal/render/ -run 'TestDeepKernelsMatchC|TestDeepRunKernelsMatchC'
```

平时跑测试只比对已提交的夹具，不需要驱动也不需要编译器。

**注意**：重生成函数在没有 `GOLDEN_REGENERATE` 时是 `return` 而不是 `t.Skip`——`t.Skip` 会中止
整个测试，比对就永远不跑。这个坑真的踩过：在修掉之前，`TestDeepKernelsMatchC` 一直是 `--- SKIP`，
所谓「逐字节对拍」一次都没执行。

## 已知偏差

`HDRNormalize`/`HDRExposure` 的曲线用 Go 的 `math.Pow`（float64），C 用 `powf`；常量也是按 float32
拼写的。当前夹具上两者逐字节一致，但极端输入仍可能有 1 ULP 级差异——`deeppixels.go` 的文件头
记了这一点。
