# CameraRawTables.bin — 来源与格式

**来源**：`reference/Swift/Compositor/Resources/CameraRawTables.bin`，子模块 pin
`e8c9e29701528413cc0e608d61187c1c61790add`，**逐字节拷贝**（1,247,790 字节）。
文件名与内容都不改，方便与上游比对。

**为什么必须随包带**：这是从 Photoshop 的 Camera Raw Filter 量出来的数据 —— 763 张
17³ 色表，每个滑杆在每个取值下渲染一次。本仓无法重算，重新量取需要 Photoshop 与
`CameraRawTables.swift:27-28` 里那张取值网格（七个亮度 × 七个自适应值）。

**为什么不放在 reference/ 下直接读**：`reference/Swift/` 是只读基准，产品代码不该依赖它；
`//go:embed` 要求资源在包目录内。

**格式**（`CameraRawTables.swift:36-64`）：

| 偏移 | 长度 | 含义 |
| --- | --- | --- |
| 0 | 4 | 标识 `"CRT3"` |
| 4 | 2 | size（uint16 LE）= 17 |
| 6 | 2 | count（uint16 LE）= 763 |
| 8 | 2 | step（uint16 LE）= 3 |
| 10 | 余下 | **xz 容器**（`FD 37 7A 58 5A 00` 开头）压缩的 count × size³ × 3 字节运行差值 |

解压后按「表 → 通道」分块，每块内是逐格的 int8 累加值（uint8 自然回绕）。实际取值 =
该轴的基准级 + 累加值 × step，夹到 0…255；基准级把 0…255 按 size 等分取整。索引次序红最慢、
其次绿、蓝最快。

注意 Apple 的 `NSData.decompressed(using: .lzma)` **实际解的是 xz**，尽管枚举叫 `.lzma`；
Go 侧用 `github.com/ulikunitz/xz` 的 `xz.NewReader`（不是 `xz/lzma`）。

**校验**：`internal/render/cameratables.go` 的 `loadCameraRawTables` 逐项核对标识、几何与
解压长度；`main()` 在开窗口之前调 `render.ValidateCameraRawTables()` 解一次，资源缺失或
几何不符会写进启动日志。

**解码正确性的证据**：`TestCameraRawIdentityTablesAreIdentity` —— 滑杆取 0 的那五张表
（曝光、白色、黑色、饱和度、自然饱和度）在当前资源上**恰好等于恒等**（偏离基准级 0 级）。
运行差值累加或轴映射写错，这个测试不会通过。
