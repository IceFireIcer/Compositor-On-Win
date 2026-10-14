# 64 — RAW 显影降噪与高光修复

**What to build:** 用户导入 RAW 时可在现有显影流程内调整降噪、镜头校正和高光恢复，渐变不会因 8bit 舍入出现明显色带。

**Blocked by:** 41 — RAW/HEIC 导入与 develop。

**Status:** done（1.4.6–1.4.10 增量候选；第三条验收项因缺真实 RAW 样本未满足，见下）

- [x] 显影面板提供新增参数，预览与最终导入使用一致的设置；等待状态延续到图层真正落地。
- [x] RAW 浮点结果在量化到 8bit 时采用细噪声抖动；透明和色彩边界保持预乘约束。
- [ ] 降噪、镜头校正和高光恢复有真实文件样本及可重复的像素/性能基准。
      **未满足**：仓库不含任何 RAW 样本（体积与版权），镜头校正更是 LibRaw 不支持（见「已知差异」）。
      已提供可重复的替代基准：`dither_quantize16` 的 C 夹具（`internal/render/testdata/`，
      并由黄金 harness 的 `quantize16-ramp` case 覆盖）与 `BenchmarkApplyDevelop`
      （2000×1500 帧 0.81 s / 14.8 MB/s，本机 Ryzen 9 7945HX）。真实相机文件的显影基准
      需要 CI 提供素材，属票 91（上游资产 CI）。
- [x] 参数重置、批量导入和取消操作不丢失队列项。

对等矩阵：U11。原版锚点：`RawImporter.swift`、`RawDevelopSheet.swift`、`RawDitherTests.swift`；提交 `6345432`、`196342c`、`248d4d4`。

---

## 实现

| 上游提交 | 改动 | Go 位置 |
| --- | --- | --- |
| `6345432` | RAW 的 16bit 结果量化到 8bit 时加细噪声（两次均匀抽样），梯度不再出色带 | `internal/render/ditherquantize.go` 的 `Quantize16` |
| `196342c` | 逐像素量化搬进 C（`dither_quantize16`），快 25 倍 | `tests/golden/c/DitherPixels.{c,h}` 增量 + 上面的 Go 移植 |
| `248d4d4` | 显影新增 Noise Reduction / Lens Correction / Highlight Recovery，各自带 asShot 起点与能力位 | `internal/rawio/{develop.go,session.go,rawbridge.*}`、`internal/bridge/raw_develop.go`、前端 `rawdevelop.ts` / `RawDevelopSheet.svelte` |

### 量化抖动

`Quantize16(wide []uint16, width, height int) []uint8` 逐位复现 C：每通道
`value = wide[c]*(255/65535) + hash_noise(x,y,c) + hash_noise(x,y,c+3) - 1`，
四舍五入后夹到 `[0, alpha]`，alpha 也按同一比例取整——预乘约束因此保持
（噪声不会把颜色抬过自身 alpha）。全程 float32 + `math.Round`（C 的 `lroundf`）。

### 显影参数

- 降噪 → LibRaw `params.threshold`（dcraw `-n` 的小波降噪，滑杆 0–1 × 1000，0 = 关）；
- 高光恢复 → LibRaw `params.highlight`（dcraw `-H`，0–9）；
- `rb_open` 新增 `RB_Caps*` 出参报告能力位，`asShot` 因此带上相机自身的起点；
- 相机自身白平衡的判定改为只比 Kelvin/tint（原先用 `IsAsShot()`，加了新参数后会被
  降噪/高光恢复误判为非 asShot 而关掉相机白平衡）。

## 验证

| 项 | 结果 |
| --- | --- |
| 抖动（上游 `RawDitherTests` 语义） | 0.30→0.32 的 400 行渐变：行均值误差 < 0.25（普通四舍五入 > 0.45）；平坦 0.5 灰落在 126..129、均值与 127.5 差 < 0.1、alpha 恒 255；同输入两次逐字节相同 |
| 与 C 逐字节一致 | `TestQuantize16MatchesC`：32×24 夹具（含预乘夹取段）与 `dither_quantize16` 的输出零差异；另有黄金 case `quantize16-ramp`（ε=0），由 `gen` 用 C 驱动重新生成，CI 可复现 |
| 显影帧形状 | `TestApplyDevelopIsRepeatable` 钉住 16→8bit 抖动的 (x,y) 哈希：同一帧按 8×8 展开与按一整行展开必须给出不同的噪声场（票 64 评审修掉的坑） |
| 性能基准 | `BenchmarkApplyDevelop`（2000×1500 帧 0.81 s / 14.8 MB/s） |
| 显影设置语义 | `IsAsShot`/`Reset`（含能力位恒 false）、无能力位时不发参数、`ApplyDevelop` == `Quantize16(ApplyDevelopWide)` |
| 不色带 | 渐变行均值误差 < 0.5，级数 ≥ 100 |
| 前端 | vitest 7 条（能力位置灰/不发参数、Reset 不改入参、预览与导入同一份 payload、坏文件不卡队列、busy 延续到图层落地）+ 真实 Edge 浏览器测试 2 条 |
| 既有 dither 参考图 | **逐字节未变** |

## 已知差异

- **镜头校正恒不可用**：原版靠 Apple RAW 引擎读文件内的镜头配置文件（畸变 + 暗角模型）；
  LibRaw 只有 EXIF 镜头名，没有可用的畸变/暗角模型。因此 `SupportsLensCorrection` 恒为 false，
  面板置灰并提示「此文件没有镜头配置文件」。这是已登记的原版差异，不伪造数值。
- 降噪/高光恢复映射到 LibRaw 的 dcraw 参数，与原版 Apple 引擎的具体数值不同；
  面板语义（asShot 起点、能力位、Reset）与上游一致。
- `dither_quantize16` 的输入是 uint16，不在 harness 的 8bit RGBA 输入管线里：driver 的
  `quantize16` 命令把 8bit 输入按 ×257 精确展成 16bit 再调用它，因此黄金 case 仍可由
  `go run ./tests/golden/gen` 重新生成（ε=0）。另有一份真正的 16bit 夹具
  （`internal/render/testdata/quantize16_*.raw`，含预乘夹取段）由 `TestQuantize16MatchesC`
  逐字节比对，配方见 `internal/render/testdata/quantize16.md`。
- `tests/golden/c/DitherPixels.{c,h}` 是**增量**拷贝而非整文件替换：整份替换会让
  1.4.8 拆分扫描线之前的 `dither-scanlines` 基准变化（票 60 已用 `ScanlinesPixels` 承担
  拆分后的真值），详见 `tests/golden/c/README.md`。

## 顺带修复

- 批量导入中某个文件打不开会把整个队列卡死：改为跳过并 `console.warn`，队列继续。
