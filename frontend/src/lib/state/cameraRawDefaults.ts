/**
 * 面板显示用的 Camera Raw 默认值（票 80 收尾）。
 *
 * **默认值不是零值**：曲线的分割点、辉光与暗角的样式、镜头配置的两个百分比、分级的混合、
 * 校准的进程版本都有非零默认。面板的滑杆在设置里还没有该键时显示的就是这些值 —— 它们必须与
 * Go 的 `render.DefaultCameraRawSettings()` 一致，否则「面板显示 25、实际按 0 计算」。
 *
 * 这里只列**非零**的那些：零值在两边都是零，不值得各存一份。两边的绑定由
 * `cameraRawDefaults.test.ts` 对着共享夹具 `internal/bridge/testdata/camera_raw_defaults.json`
 * 逐条比对（那份夹具由 Go 生成并断言）。
 */
export const CAMERA_RAW_DEFAULTS = {
  whiteBalance: "Custom",
  vignetteMidpoint: 50,
  vignetteFeather: 50,
  grainSize: 25,
  grainRoughness: 50,
  curve: { shadowSplit: 25, darkSplit: 50, lightSplit: 75 },
  grading: { blending: 50 },
  detail: {
    sharpenRadius: 10,
    sharpenDetail: 25,
    noiseLuminanceDetail: 50,
    noiseColorDetail: 50,
    noiseColorSmoothness: 50,
  },
  optics: {
    profileDistortion: 100,
    profileVignetting: 100,
    purpleHueLow: 270,
    purpleHueHigh: 310,
    greenHueLow: 60,
    greenHueHigh: 120,
    vignetteMidpoint: 50,
  },
  geometry: { upright: "Off", projection: "Perspective" },
  calibration: { process: 6 },
} as const;
