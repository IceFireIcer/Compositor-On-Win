# 90 — Camera Raw 色彩族对齐

**What to build:** Camera Raw 的白平衡、色彩混合器、曲线、分级、渐晕和校准与 Photoshop 对照结果一致。

**Blocked by:** 28 — Camera Raw：光/色/曲线; 29 — Camera Raw：混色器/分级/细节; 30 — Camera Raw：光学/几何/校准。

**Status:** done（2026-10-16）—— 七项各自的断言已逐条核对并补上缺的那一项。

- [x] 白平衡、vibrance/saturation、混色器、曲线、四轮分级、渐晕和校准分别有参考像素断言。

| 项 | 断言 |
| --- | --- |
| 白平衡 | `TestCameraRawGains`（暖/品红方向）、`TestWhiteBalanceStagePicksTheGridCell`（取到哪个网格单元） |
| vibrance | `TestVibranceGainsMoreOnAMutedColorThanOnASaturatedOne`（**本轮补**） |
| saturation | 同属 Light/Color 表；`TestTableStagesOrderAndEmptiness` 钉住它在链里的位置 |
| 混色器 | `TestMixerStagesPickTheRightRunsInOrder`、`TestApplyCameraRawCurveColorMixerShiftsHue` |
| 曲线 | `TestCameraRawToneTableParametricBends`、`TestApplyCameraRawCurveColorZeroDeltasRoundTrips`、`TestFoldingAgreesWithThePerPixelPass` |
| 四轮分级 | `TestGradingStagesPickTheWheelTables`、`TestGradingLuminanceSlidersUseTheirOwnRuns`、`TestGradingBalanceZeroesTheWeakerWheel`、`TestApplyCameraRawCurveColorGradingWheels` |
| 渐晕 | `TestCameraVignetteMaskShape`、`TestApplyCameraRawEffectsVignetteDarkensCorners` |
| 校准 | `TestApplyCameraRawCalibrationShadowTintAffectsShadows`、`TestCameraRawFilterAppliesCalibrationPrimariesAlone` |

**这七项真正的权威真值是 Photoshop 的测量表本身**（随包 `CameraRawTables.bin`），由
`TestCameraRawApplyMatchesC` / `TestCameraRawComposeMatchesC` 与各 stage builder 的表号断言钉住；
上面那些是行为方向与接线，不是数值标定。

### 一条没有写出来的断言

vibrance 与 saturation 挨在一起，很容易想写「vibrance 更保护饱和色」。**实测分不出来**：两个样本上
两根滑杆方向相同（vibrance 相对增益 0.43/0.25，saturation 0.30/0.15），因为饱和那一端本来就贴近
上限，等滑杆值比较没有意义。硬写这条断言只是拟合数字，所以只钉了可复现的那一半 —— vibrance 对
不饱和像素的相对增益高于对饱和像素的 —— 并在代码注释里把为什么不做另一半说清楚。

- [x] 透明像素不参与错误的色彩统计；调整层、滤镜预览和最终提交采用相同顺序。
      透明像素：`TestCameraRawStatisticsOfAClearImageAreNeutral`（清晰图统计为中性）、
      `TestCameraRawStatisticsMatchC`、`TestCameraRawApplyLeavesClearPixelsAlone`。
      预览与最终提交同序：`TestCameraRawEffectsPreviewMatchesFullSize`（≤2048px 预览与全尺寸走同一个
      `ApplyCameraRawFilter`，只是 `Scale` 不同）。
      **Camera Raw 在本仓不是调整层类型**（调整层是色相/饱和度、色阶、曲线、曝光、渐变映射、颗粒、
      添加杂色、高斯/动感模糊、反相、黑白、色彩平衡），所以票面那句对它是预览/提交的关系；各调整层
      自己的同序由 `internal/bridge` 的调整层测试覆盖。

- [x] 旧 1.4.5 样本的预期变化记录为版本差异，不覆盖历史基准。
      `tests/golden/legacy/camera-raw-1.4.5.{json,png}` 与 `legacy/README.md` 原样保留，票 63 把
      `camera_raw` 这个 case 与参考图退役到那里、并记录了数值变化，而不是覆盖已提交的基准。

对等矩阵：U37。原版锚点：`CameraRawTests.swift`、`CameraRawTables.swift`；提交 `57ab3bb`、`bdfb9c2`、`9dac4f1`。
