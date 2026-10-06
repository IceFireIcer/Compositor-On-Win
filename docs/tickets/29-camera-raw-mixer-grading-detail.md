# 29 — Camera Raw：混色器/分级/细节

**What to build:** Camera Raw 内核第二批：HSL 混色器 8 色相、颜色分级（阴影/中间调/高光+混合/平衡）、细节（锐化+亮度盒模糊降噪）、去雾。

**Blocked by:** 28 — Camera Raw：光/色/曲线.

**Status:** done

- [x] 混色器/分级/细节与 C 黄金基准一致
- [x] 与第一批参数组合的管线顺序一致
- [x] 降噪盒模糊与锐化蒙版语义（CameraRawDetailOptics 对应移植）验证

对等矩阵：R04、R05、R06。

**实现记录**（cameraraw.go，随票 29 提交）：
- 混色器/分级走票 28 的 `ApplyCameraRawCurveColor`（同一内核），新增 mixer
  （橙 hue/绿 sat 等 8 族加权邻域）与 grading（四轮+blending 0.6+balance −0.2
  权重归一）两个 ε=0 黄金 case——注意 case 传内核打包值（hue ÷360、sat/lum ÷100）。
- `ApplyCameraRawEffects` 全量移植：质感（细尺度局部对比）/清晰度（粗尺度）/
  去雾（正深度对比+饱和抬升、负深度阴影提升）/光晕（扩散/泛光/光晕三样式，
  阈值亮度面+盒模糊+暖色增益）/暗角（Highlight Priority 高光保护）。
- `ApplyCameraRawDetail` 全量移植：亮度降噪（保边盒模糊+对比回加）→色度降噪
  （HSL 饱和度平滑）→锐化（环形边缘+蒙版阈值）；`sharpenEdgeAt` float 算术
  与 C fabsf 逐位一致。boxBlurPlane 双精度滑窗、float 存储。
- 黄金基准：dehaze/clarity/glow/detail 全 ε=0 逐位一致；vignette ε=1
  （vignette_mask 的 hypot 跨库）。scale 参数保证预览与全尺寸同径。
