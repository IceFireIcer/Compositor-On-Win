# 性能与内存预算：基准

票 52 的基准部分。这里记录 CPU 合成器（`internal/render`）与笔刷路径
（`internal/bridge`）的耗时基线、跑法，以及怎么在 CI 里跟踪趋势。

对应代码：

- `internal/render/composite_bench_test.go` — `BenchmarkComposite4K`、
  `BenchmarkComposite4KWithMask`、`BenchmarkComposite100MP`
- `internal/bridge/brush_bench_test.go` — `TestBrushStroke4KLatency`、
  `BenchmarkBrushStroke4K`

## 怎么跑

`Benchmark*` **不会**被 `go test ./...` 触发（基准要 `-bench`）。但注意：
**只有 `-short` 才会跳过全部基准与那条 4K 笔刷测量** —— 不加 `-short` 的
`go test ./internal/bridge`（含 CI 现在的 `go test ./...`）会真跑
`TestBrushStroke4KLatency`，代价约 0.5–1 s（可接受，它带 48 ms 的宽容门禁）。
100MP 那条额外要求 `COMPOSITOR_BENCH_HEAVY=1`，否则打印跳过原因。

```sh
# 日常全量测试（基准与 4K 测量自动跳过，毫秒级）
go test ./... -count=1

# 显式 -short：同样跳过（CI 的快速档）
go test ./... -short -count=1

# 4K 笔刷测量：默认就会跑（本机约 0.5 s），只跳过基准
go test ./internal/bridge -run TestBrushStroke4KLatency -v -count=1

# 合成基准（4K 与 4K+蒙版，各 10 个样本）
go test ./internal/render -run XXX -bench "BenchmarkComposite4K$|BenchmarkComposite4KWithMask" \
    -benchmem -benchtime 10x -count=1

# 笔刷基准（一整趟 120 段笔刷含提交，走桥接层真实路径）
go test ./internal/bridge -run XXX -bench BenchmarkBrushStroke4K -benchmem -count=1

# 100MP 重负载：默认跳过，必须显式打开；一次约 21 s、3.8 GB 分配
COMPOSITOR_BENCH_HEAVY=1 go test ./internal/render -run XXX \
    -bench BenchmarkComposite100MP -benchmem -benchtime 1x -count=1
```

环境变量：

| 变量 | 作用 |
| --- | --- |
| `COMPOSITOR_BENCH_HEAVY=1` | 打开 100MP 合成基准（默认跳过） |
| `COMPOSITOR_BENCH_STRICT=1` | 把 4K 笔刷测量的 median 门禁从 48 ms 收紧到票面目标 16 ms |

`-short` 会把所有基准和 4K 笔刷测量都跳过，所以 `go test -run XXX ./...`
这类调用在任何模式下都不会失败。

## 本机基线

采集环境（`go version` / `Get-CimInstance Win32_OperatingSystem` /
`Win32_Processor` / `Win32_ComputerSystem`）：

| 项 | 值 |
| --- | --- |
| Go | go1.26.0 windows/amd64 |
| 操作系统 | Microsoft Windows 11 专业版 10.0.26200 (64 位) |
| CPU | AMD Ryzen 9 7945HX with Radeon Graphics，16 核 / 32 线程，2.5 GHz |
| 内存 | 15.2 GiB（16,299,335,680 字节） |
| 机型 | Micro-Star International Co., Ltd. Crosshair A16 HX D7WFKG |

文档限额（`internal/domain/limits.go`）：`MaxSide = 30_000`、
`MaxSurfacePixels = 200_000_000`（单个 surface），
`DocumentPixelBudget()` 是整篇文档的像素预算（RAM/16，200M–800M）。

### 合成（`internal/render`）

场景：1 个背景 + 2 个带水平渐变蒙版的图层 + 1 个裁剪到下方图层覆盖率的
图层；像素源在计时区之外构造并复用，只测 `render.Render` 本身。

| 基准 | 画布 | ns/op | ms/op | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: | ---: |
| `BenchmarkComposite4K` | 3840×2160（8.3MP，4 图层） | 1,606,831,060 | 1606.8 | 315,270,915 | 511 |
| `BenchmarkComposite4KWithMask` | 3840×2160（8.3MP，单蒙版图层） | 556,552,610 | 556.6 | 149,324,606 | 170 |
| `BenchmarkComposite100MP` | 10000×10000（100MP，4 图层） | 21,197,900,500 | 21197.9 | 3,800,307,552 | 717 |

命令：

```sh
go test ./internal/render -run XXX -bench "BenchmarkComposite4K$|BenchmarkComposite4KWithMask" -benchmem -benchtime 10x -count=1
COMPOSITOR_BENCH_HEAVY=1 go test ./internal/render -run XXX -bench BenchmarkComposite100MP -benchmem -benchtime 1x -count=1
```

100MP 只跑了 1 个样本：单次就分配 3.8 GB、耗时 21 s，多跑样本只会让内存
压力主导结果。它的意义是"这条路能跑通且量级可测"，不是稳定回归基线。

### 4K 笔刷（`internal/bridge`）

`TestBrushStroke4KLatency`（两趟 120 段，逐段计时）：

```
BRUSH4K diameter=30 pass=1 median_ms=1.50 p95_ms=2.51 end_ms=32.53
BRUSH4K diameter=30 pass=2 median_ms=2.00 p95_ms=2.51 end_ms=31.53
```

（同一台机器上重复 4 次：median 1.50–2.00 ms、p95 2.11–2.51 ms、
end 29.1–50.8 ms；`end_ms` 的第一趟偶尔冲到 50 ms 上下，是提交时的
GC/分配抖动。）

`BenchmarkBrushStroke4K`（一整趟 120 段含 `EndStroke` 提交）：

```
BenchmarkBrushStroke4K-32    	       5	 235191220 ns/op	135095705 B/op	     709 allocs/op
```

命令：

```sh
go test ./internal/bridge -run TestBrushStroke4KLatency -v -count=1
go test ./internal/bridge -run XXX -bench BenchmarkBrushStroke4K -benchmem -count=1
```

读法：每段指针更新是 1.5–2.0 ms（p95 2.1–2.5 ms），mouse-up
（`EndStroke`，含把笔刷网格提交回位图库）约 29–36 ms（首趟偶尔到 50 ms
上下），占整趟 235 ms 的绝大部分——瓶颈在提交，不在逐段绘制。

**门禁**：`TestBrushStroke4KLatency` 默认只在 median 超过 **48 ms** 时失败
（CI 机器慢/负载高时不误报）；`COMPOSITOR_BENCH_STRICT=1` 时改用票面目标
**16 ms**。这条写死在 `brushMedianThreshold()` 与测试注释里。

### 桥接层的笔刷直径限制（重要）

原版场景测的是直径 40 与 800。桥接层目前只暴露固定的默认笔刷设置
（`internal/bridge/brush.go` 的 `defaultBrushSettings`，直径 30），没有设置
直径的入口，所以这里的 `diameter=30` 是桥接层真实可达的那一档，**不是**
原版的 40/800。要按原版直径测桥接路径，必须先给 bridge 加设置入口——那要改
`internal/bridge/brush.go`，超出本票允许新建的路径，所以没做。下面的原版
对照因此只作量级参考。

## Camera Raw：查表路径与预览倍率（票 78）

对应代码：`internal/render/cameraraw_bench_test.go`。

```sh
go test ./internal/render -run '^$' -bench 'CameraRaw' -benchtime=3x -count=1
go test ./internal/bridge -run TestPreviewRendersFewerPixelsThanTheCommitByTheExplainedFactor -v -count=1
```

### 查表 vs 解析核（2048²，Light/Color 共享子集）

```
BenchmarkCameraRawTablePath-32       	       3	 358273133 ns/op	17215626 B/op	       6 allocs/op
BenchmarkCameraRawAnalyticPath-32    	       3	1643763433 ns/op	16777216 B/op	       1 allocs/op
```

**约 4.6×**：查表每像素一次三线性采样，解析核每像素重算一遍色调数学。4.6× 是这台机器上
**实测**的值 —— 上游笔记里那个 5–20× 说的不是这件事，见下。

### 5–20× 指的是预览的像素数

上游把预览限在 **2048 最长边**（`filter_session.go` 的 `previewLimit`），所以倍率是纯算术：
**(最长边 / 2048)²** —— 与滑杆、与滤镜都无关。实测（`TestPreview...ExplainedFactor`）：

| 文档 | 像素 | 预览 | 倍率 |
| --- | --- | --- | --- |
| 4000×2666 | 10.7MP | 2.8MP | **3.8×（够不着 5×）** |
| 4600×3066 | 14.1MP | 2.8MP | 5.0× |
| 6000×4000（常见照片） | 24.0MP | 2.8MP | 8.6× |
| 9000×6000 | 54.0MP | 2.8MP | 19.3× |

读法：5–20× 对应最长边约 **4600–9200px**（3:2 下约 14–54MP）。比这小的照片倍率更低，
比这大的更高 —— 断言里把 4000px 那条**如实标成不达标**，不四舍五入成「达标」。

**提交不走捷径**：同一个测试断言提交用的底图仍是全尺寸那张（`grown`），只有预览源被降采样。

### 与上游笔记的对照（只作量级参考）

上游的 5–20× 是本仓无法直接复现的外部基线（没有原版的机器、原版也没有可跑的 Windows 构建）。
这里能给的是**可解释**的两件事：查表相对解析核实测约 4.6×，以及预览相对提交的倍率就是
`(最长边/2048)²`。把两者相乘才是「常见照片预览 vs 全尺寸逐像素重算」的总账，且两项都可复算。

## 与原版 4K 笔刷笔记的对照

原版数字来自 `reference/Swift/docs/brush-performance.md`（2026-09-12 采集）
与 `reference/Swift/CompositorTests/BrushPerformanceTests.swift`：4000×4000
文档、直径 40 与 800、hardness 0、每次 120 段、两趟，opaque 与空白层各测
一遍。

| 场景 | 原版（macOS，含视图显示） | 本机 Go（仅后端，直径 30） |
| --- | ---: | ---: |
| 800 px 笔刷 median 更新 | 2.80 ms（修正后 3.08–3.12 ms） | 1.50–2.00 ms |
| 800 px 笔刷 p95 更新 | 5.38 ms | 2.11–2.51 ms |
| 800 px 笔刷 mouse-up | 5.2–6.2 ms（修正后 6.3–9.7 ms） | 29.1–50.8 ms |
| 40 px 笔刷 median 更新 | 0.36–0.47 ms | 1.50–2.00 ms |

**这些数字不可直接比较。** 三个原因：

1. **原版包含视图显示开销**：它的计时覆盖模型更新加
   `CanvasView.synchronizeDisplay()` / `displayIfNeeded()`；Go 侧只测后端
   （`StrokePoint` / `EndStroke`），没有任何绘制或上屏成本。
2. **直径不同**：原版是 40/800，Go 侧是桥接层唯一的 30（见上一节）。
3. **mouse-up 语义不同**：原版 mouse-up 含 flush、commit、history 与随后的
   一次显示；Go 侧 `EndStroke` 是 `Commit()` 加把网格写回位图库。

所以本表只用于看量级与找异常方向，不构成"Go 比原版快/慢"的结论。真正的
跨平台对比要在 WebGPU 渲染路径与前端显示接上之后，用同样的"含显示"口径
再测一次。

## CI 趋势怎么记录

现有 CI 在 `.github/workflows/verify.yml` 的 `Go checks (vet + test)` 步骤里
跑 `go vet ./...` 与 `go test ./...`。**本票不改该文件**，加步骤由另一个人
负责。给那一节的设计：

1. **每次 CI 跑哪条命令**：新增一步，只跑 4K 档（不跑 100MP）：

   ```sh
   go test ./internal/render -run XXX \
       -bench "BenchmarkComposite4K$|BenchmarkComposite4KWithMask" \
       -benchmem -benchtime 10x -count=1 -v | tee bench-composite.txt
   go test ./internal/bridge -run TestBrushStroke4KLatency -v -count=1 | tee bench-brush.txt
   ```

2. **数值看哪里**：`Benchmark...` 行里的 `ns/op` 与 `B/op`，以及
   `BRUSH4K ... median_ms=... p95_ms=... end_ms=...` 行。两处输出都用
   `tee` 落到文件，再用 `actions/upload-artifact` 作为构建产物留存，这样
   每次 CI 都有一条可 grep 的历史曲线。

3. **为什么不用硬阈值卡门禁**：GitHub 托管 runner 是共享、异构的机器，频率
   与邻居负载都会变；同一份代码在 `windows-latest` 上跑出两三倍差异是常态。
   用绝对阈值卡门禁只会制造随机红灯，然后被大家忽略。所以：

   - CI 只**记录并上传**数字，跑出趋势；
   - 唯一进测试门禁的是 `TestBrushStroke4KLatency` 那条宽容断言（默认
     48 ms），它拦的是"数量级退化"（比如提交路径从 32 ms 掉到秒级），
     不是"慢了 20%"；
   - 真要看回归，就对比两次 CI 的 artifact，或用同一台机器上的前后两次
     `-count=1` 结果做相对比较，而不是拿绝对毫秒数跨机器比。

4. **100MP 不进 CI**：单次 3.8 GB 分配、21 s 耗时，会拖长流水线并可能触发
   runner 内存压力。需要时在本地或专门的机器上用
   `COMPOSITOR_BENCH_HEAVY=1` 手动跑，把数字补进上面的表。

## 未跑项

- **原版直径 40 / 800 的桥接路径**：桥接层没有设置直径的入口，跑不了（原因
  见上文）。本机 `diameter=30` 的数字已给出。
- **opaque / 空白层两种场景**：原版分别测了不透明层与空白层；Go 侧桥接装置
  的文档自带一张不透明白背景层，且两趟画在同一层上，只覆盖了"已有像素"这
  一种。要分别测需要改测试装置，本票未做。
- **100MP 多样本**：只跑了 1 个样本（见上文原因）。
- **GPU / 前端显示路径**：尚未接线，没有可测对象。
