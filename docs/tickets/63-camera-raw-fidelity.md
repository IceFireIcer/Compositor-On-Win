# 63 — Camera Raw 光影与细节数值对齐

**What to build:** 用户调节 Camera Raw 曝光、对比、高光、阴影、去朦胧、纹理、清晰度、锐化和色彩降噪时，预览与最终结果按新版原版数值一致。

**Blocked by:** 28 — Camera Raw：光/色/曲线; 29 — Camera Raw：混色器/分级/细节; 30 — Camera Raw：光学/几何/校准; 34 — Camera Raw 面板与示波器。

**Status:** done（1.4.6–1.4.9 增量候选）

- [x] 对照上游 1.4.9 数值基准复核曝光、对比、高光/阴影、去朦胧、纹理、清晰度及锐化；色彩族另见 90。
- [x] 色彩降噪忽略透明像素对饱和度的污染，仅平滑色度并保留亮度；高光/阴影调整不颠倒明暗顺序。
- [x] 对应 `CameraRawTests` 用例建立可复现的参考像素和容差；旧 1.4.5 黄金样本的数值变化单独记录。
- [x] 同图同设置预览与提交一致；查表与预览性能另见 78。

对等矩阵：U10。原版锚点：`CameraRaw*.swift`、`CameraRawTests.swift`；提交 `42a559b`、`75147a2`、`0b4c56c`。色彩族与性能分见 90、79。

---

## 实现

上游三个提交把 Camera Raw 的**无需测量数据**的那部分算法改了，本票把它们逐条移植到
`internal/render/cameraraw.go`（内核真值仍是 `tests/golden/c/AdjustPixels.{c,h}`，本轮已搬到
子模块 pin `e8c9e29`，见 `tests/golden/c/README.md`）：

| 上游提交 | 改动 | Go 位置 |
| --- | --- | --- |
| `0b4c56c` | 高光/阴影改用 `tone_bump`（u(1−2u)²）：曲线斜率恒为正，黑白与中灰不动 | `camToneBump` / `camToneShadows` / `camToneHighlights` |
| `42a559b` | 色彩降噪改为「红/蓝相对亮度的色差按 alpha 加权模糊后放回，亮度不变」 | `ApplyCameraRawDetail` 的 `noiseColor` 段 |
| `75147a2` | 去雾读画面自身最亮 1% 的雾色；纹理/清晰度改用保边（guided）滤波、半径与画面边长成比例；Masking 0 时全图锐化、强度 1.9×(0.5+Detail) | `camDehazeHaze` / `camEffectsDehaze` / `guidedFilterPlane` / `ApplyCameraRawEffects` / `ApplyCameraRawDetail` |

`effects_vignette` 的 Photoshop 对齐重写（1.4.9 同一批）也随拷贝件一起落地：Highlight Priority
改为线性光下的增益/向白混合。它原本归票 90，但拷贝件搬到 pin 后 harness 的
`camera-raw-vignette` 参考图就是新算法，不同步移植会让该 case 失败。

## 验证

- `internal/render/cameraraw_test.go` 新增 5 条，逐条对应上游 `CameraRawTests`：
  明暗顺序（±100 下 ramp 单调、黑白不动、+100 抬暗部）、色彩降噪（色度降到 1/4 以下而亮度不变）、
  去雾（雾下加深/加雾提亮/单色图不动）、Masking 0 全图锐化（Masking 100 时该像素一格不动）、
  清晰度保边（不越出阶跃两侧 20 级，旧盒模糊会越 53 级）。
- 黄金基准：`camera-raw-dehaze`、`camera-raw-clarity`、`camera-raw-detail` 三张参考图按新 C 重新生成，
  Go 端口**逐字节**通过（ε=0）；`camera-raw-vignette` 因 `hypot` 跨库取 ε=1。
- 预览与提交一致：`TestCameraRawEffectsPreviewMatchesFullSize` 把 400×400 的结果降采样回 200×200，
  与直接在 200×200 上跑的预览比对——纹理/清晰度的半径按画面边长成比例，两者最大差 2 级、
  均值 0.26–0.32 级；旧的固定半径会让预览的模糊相对宽一倍。
- 旧 1.4.5 样本的数值变化：`camera-raw` case 退役到 `tests/golden/legacy/`，实测
  max Δ147 / mean Δ1 / 2798 of 9216 字节变化，由 `TestLegacyCameraRawBaselineDrift` 钉住。

## 已知差异（留给后续票）

- **自适应对比/高光/阴影**（`75147a2` 的主干）由**测量查表**实现：`CameraRawTables.bin`
  （763 张 17³ 色表，Photoshop 实测）+ `camera_raw_compose`。Go 侧仍是直接内核，
  因此对比、白平衡、白/黑、饱和/自然饱和的数值与 1.4.9 不逐位一致。该数据与合成属
  **票 78**（查表与预览性能，blocked by 本票）与**票 90**（色彩族）。
- `camera_raw_statistics`（自适应滑块的三个亮度度量）同样属于查表路径，未移植。
- **镜头暗角校正**自 1.4.9 起读曝光查表；Go 侧保留旧的公式实现，`camera-raw-optics` case
  因此把暗角滑块保持 0（否则 C 的 NULL 表路径与 Go 不等价）。票 78 补表后恢复对照。
- 预览与提交走同一个内核、同一组设置，只是 `Scale` 不同；纹理/清晰度的半径已改为按画面
  边长成比例，因此缩小的预览与全尺寸提交一致。**预览性能**与取消旧任务见票 78。
