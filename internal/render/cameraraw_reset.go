package render

// Camera Raw 的分区重置（票 80）。上游 CameraRawReset.swift：
//
//	resetting(group) —— 该分区回到默认，其余不动；
//	keeping(group)   —— 只保留该分区（用于判断「是否已是默认」）；
//	isDefault(group) —— keeping(group) == 默认值。
//
// **默认值不等于零值**：曲线的分割点（25/50/75）、辉光与暗角的样式、镜头配置的两个百分比、
// 分级的 Blending、细节的几个中点、光学的去边色相端点、校准的 Process 都有非零默认。只把字段
// 清零会丢掉它们 —— 那正是「重置之后画面变了样」的来源。默认值在下面显式列出，逐条对应上游
// 结构体的属性初始化（CameraRaw.swift:52-95、CameraRawColor.swift:20-33、
// CameraRawDetailOptics.swift:5-59、CameraRawGeometryCalibration.swift:213）。

import (
	"reflect"

	"compositor-win/internal/domain"
)

// CameraRawGroup is CameraRawGroup: the ten panel sections, each of which can
// be put back to its defaults on its own.
type CameraRawGroup string

const (
	CameraRawGroupLight       CameraRawGroup = "Light"
	CameraRawGroupColor       CameraRawGroup = "Color"
	CameraRawGroupEffects     CameraRawGroup = "Effects"
	CameraRawGroupCurve       CameraRawGroup = "Curve"
	CameraRawGroupMixer       CameraRawGroup = "Color Mixer"
	CameraRawGroupGrading     CameraRawGroup = "Color Grading"
	CameraRawGroupDetail      CameraRawGroup = "Detail"
	CameraRawGroupOptics      CameraRawGroup = "Optics"
	CameraRawGroupGeometry    CameraRawGroup = "Geometry"
	CameraRawGroupCalibration CameraRawGroup = "Calibration"
)

// CameraRawGroups is CameraRawGroup.allCases in the panel's order.
var CameraRawGroups = []CameraRawGroup{
	CameraRawGroupLight, CameraRawGroupColor, CameraRawGroupEffects, CameraRawGroupCurve,
	CameraRawGroupMixer, CameraRawGroupGrading, CameraRawGroupDetail, CameraRawGroupOptics,
	CameraRawGroupGeometry, CameraRawGroupCalibration,
}

// cameraRawLinearCurve is CameraRawCurveSettings.linear: the two points that
// make a curve a no-op.
func cameraRawLinearCurve() []domain.CurvePoint {
	return []domain.CurvePoint{{X: 0, Y: 0}, {X: 1, Y: 1}}
}

// DefaultCameraRawSettings is CameraRawSettings(): the property initialisers,
// which are not all zero.
func DefaultCameraRawSettings() CameraRawSettings {
	return CameraRawSettings{
		WhiteBalance:     "Custom",
		VignetteMidpoint: 50,
		VignetteFeather:  50,
		GrainSize:        25,
		GrainRoughness:   50,
		Curve: CameraRawCurveSettings{
			ShadowSplit: 25, DarkSplit: 50, LightSplit: 75,
			RGB: cameraRawLinearCurve(), Red: cameraRawLinearCurve(),
			Green: cameraRawLinearCurve(), Blue: cameraRawLinearCurve(),
		},
		Grading: CameraRawGradingSettings{Blending: 50},
		Detail: CameraRawDetailSettings{
			SharpenRadius: 10, SharpenDetail: 25,
			NoiseLuminanceDetail: 50, NoiseColorDetail: 50, NoiseColorSmoothness: 50,
		},
		Optics: CameraRawOpticsSettings{
			ProfileDistortion: 100, ProfileVignetting: 100,
			PurpleHueLow: 270, PurpleHueHigh: 310,
			GreenHueLow: 60, GreenHueHigh: 120,
			VignetteMidpoint: 50,
		},
		Geometry:    CameraRawGeometrySettings{Upright: "Off", Projection: "Perspective"},
		Calibration: CameraRawCalibrationSettings{Process: 6},
	}
}

// copyCameraRawGroup is CameraRawReset.copy: `group` from source into target.
func copyCameraRawGroup(group CameraRawGroup, from CameraRawSettings, to *CameraRawSettings) {
	switch group {
	case CameraRawGroupLight:
		to.Exposure, to.Contrast = from.Exposure, from.Contrast
		to.Highlights, to.Shadows = from.Highlights, from.Shadows
		to.Whites, to.Blacks = from.Whites, from.Blacks
	case CameraRawGroupColor:
		to.WhiteBalance, to.Temperature, to.Tint = from.WhiteBalance, from.Temperature, from.Tint
		to.Vibrance, to.Saturation = from.Vibrance, from.Saturation
	case CameraRawGroupEffects:
		to.Texture, to.Clarity, to.Dehaze = from.Texture, from.Clarity, from.Dehaze
		to.Glow, to.GlowStyle = from.Glow, from.GlowStyle
		to.GlowRange, to.GlowSpread, to.GlowWarmth = from.GlowRange, from.GlowSpread, from.GlowWarmth
		to.VignetteAmount, to.VignetteStyle = from.VignetteAmount, from.VignetteStyle
		to.VignetteMidpoint, to.VignetteRoundness = from.VignetteMidpoint, from.VignetteRoundness
		to.VignetteFeather, to.VignetteHighlights = from.VignetteFeather, from.VignetteHighlights
		to.GrainAmount, to.GrainSize, to.GrainRoughness = from.GrainAmount, from.GrainSize, from.GrainRoughness
	case CameraRawGroupCurve:
		to.Curve = from.Curve
	case CameraRawGroupMixer:
		to.Mixer = from.Mixer
	case CameraRawGroupGrading:
		to.Grading = from.Grading
	case CameraRawGroupDetail:
		to.Detail = from.Detail
	case CameraRawGroupOptics:
		to.Optics = from.Optics
	case CameraRawGroupGeometry:
		to.Geometry = from.Geometry
	case CameraRawGroupCalibration:
		to.Calibration = from.Calibration
	}
}

// ResetGroup is resetting: these settings with `group` back to its defaults,
// choices and midpoints included, the other sections unchanged.
func (s CameraRawSettings) ResetGroup(group CameraRawGroup) CameraRawSettings {
	copyCameraRawGroup(group, DefaultCameraRawSettings(), &s)
	return s
}

// KeepingGroup is keeping: new settings carrying only `group` from these.
func (s CameraRawSettings) KeepingGroup(group CameraRawGroup) CameraRawSettings {
	out := CameraRawSettings{}
	copyCameraRawGroup(group, s, &out)
	return out
}

// IsGroupDefault is isDefault: whether `group` is as new settings have it, so
// there is nothing to reset.
//
// Upstream compares against CameraRawSettings(), which is the *default-
// initialised* value — not a zero one. Comparing against Go's zero value would
// call every group with a non-zero default (curve dividers, styles, lens
// percentages) permanently un-default, so the reset control would never settle.
func (s CameraRawSettings) IsGroupDefault(group CameraRawGroup) bool {
	return reflect.DeepEqual(s.KeepingGroup(group), DefaultCameraRawSettings().KeepingGroup(group))
}
