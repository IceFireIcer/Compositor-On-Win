# 28 — Camera Raw：光/色/曲线

**What to build:** Camera Raw 内核第一批（`adjust_camera_raw*` 族）：曝光/对比/高光/阴影/白/黑、白平衡/自然饱和度/饱和度、参数化+点曲线；≤2048px 预览与全尺寸提交同源。

**Blocked by:** 26 — 调整内核 A.

**Status:** done

- [x] 光/色/曲线参数族与 C 黄金基准一致（CameraRawTests 语义分批）
- [x] 参数族可独立组合（管线顺序与原版一致）
- [x] 高光/阴影修正的浮点 ε 用例显式标注

对等矩阵：R01、R02、R03。

**实现记录**（cameraraw.go，随票 28 提交）：
- 设置模型全家族落地（Curve/Mixer/Grading/Detail/Optics/Geometry/Calibration，
  Normalized/Adjusts/Gains 镜像 CameraRaw*.swift；零值≠Swift 默认——归一化按
  ImageAdjustmentPixels.clamp 语义把 0 压到区间下限，与 Swift 一致）。
- `ApplyCameraRaw` 逐行移植（白平衡增益→曝光→对比→高光/阴影/白/黑→自然饱和
  /饱和 + clipping 1/2 视图）；既有 camera-raw case（ε=1）首次接入比对。
- 曲线：cameraBend 参数化四区（33 锚点过单调三次 Hermite 平滑）+ 四条点曲线
  （复用 curveValue 的 0…255 标度路由）；`BuildCameraRawToneTable`/
  `BuildCameraRawChannelTable`。
- `ApplyCameraRawCurveColor` 全量移植（tone/通道 LUT + refineSaturation 双向
  + 混色器 8 族 + 点色 + 分级四轮 + visualize）。
- 黄金基准：新增 `camera_raw_curve` 驱动命令（driver 内镜像表构建器）与两个
  case——曲线组 ε=1（bend 跨库 pow），彩色通道曲线 ε=0 逐位（含 HSL 零移位
  往返）。管线顺序由票 30 的 ApplyCameraRawFilter 定型并测试。
