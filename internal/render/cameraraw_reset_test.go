package render

// 票 80：Camera Raw 的分区重置。

import (
	"encoding/json"
	"reflect"
	"testing"

	"compositor-win/internal/domain"
)

// cameraRawAllChanged has every field off its default, so a reset of one group
// is visible and a leak into another group shows up too.
func cameraRawAllChanged() CameraRawSettings {
	mixer := CameraRawMixerSettings{}
	for i := 0; i < 8; i++ {
		mixer.Hue[i], mixer.Saturation[i], mixer.Luminance[i] = float64(50+i), float64(60+i), float64(70+i)
	}
	return CameraRawSettings{
		WhiteBalance: "Auto", Temperature: 11, Tint: 12,
		Exposure: 13, Contrast: 14, Highlights: 15, Shadows: 16, Whites: 17, Blacks: 18,
		Vibrance: 19, Saturation: 20,
		Texture: 21, Clarity: 22, Dehaze: 23,
		Glow: 24, GlowStyle: 2, GlowRange: 25, GlowSpread: 26, GlowWarmth: 27,
		VignetteAmount: 28, VignetteStyle: 1, VignetteMidpoint: 29, VignetteRoundness: 30,
		VignetteFeather: 31, VignetteHighlights: 32,
		GrainAmount: 33, GrainSize: 34, GrainRoughness: 35,
		Curve: CameraRawCurveSettings{
			Shadows: 36, Darks: 37, Lights: 38, Highlights: 39,
			ShadowSplit: 40, DarkSplit: 41, LightSplit: 42, RefineSaturation: 43,
			RGB:   []domain.CurvePoint{{X: 0, Y: 0.1}, {X: 1, Y: 0.9}},
			Red:   []domain.CurvePoint{{X: 0, Y: 0.2}, {X: 1, Y: 0.8}},
			Green: []domain.CurvePoint{{X: 0, Y: 0.3}, {X: 1, Y: 0.7}},
			Blue:  []domain.CurvePoint{{X: 0, Y: 0.4}, {X: 1, Y: 0.6}},
		},
		Mixer: mixer,
		Grading: CameraRawGradingSettings{
			Shadows:    CameraRawGradeWheel{Hue: 10, Saturation: 11, Luminance: 12},
			Midtones:   CameraRawGradeWheel{Hue: 20, Saturation: 21, Luminance: 22},
			Highlights: CameraRawGradeWheel{Hue: 30, Saturation: 31, Luminance: 32},
			Global:     CameraRawGradeWheel{Hue: 40, Saturation: 41, Luminance: 42},
			Blending:   60, Balance: 7,
		},
		Detail: CameraRawDetailSettings{
			SharpenAmount: 51, SharpenRadius: 52, SharpenDetail: 53, SharpenMasking: 54,
			NoiseLuminance: 55, NoiseLuminanceDetail: 56, NoiseLuminanceContrast: 57,
			NoiseColor: 58, NoiseColorDetail: 59, NoiseColorSmoothness: 60,
		},
		Optics: CameraRawOpticsSettings{
			RemoveChromaticAberration: true, EnableLensProfile: true,
			ProfileDistortion: 61, ProfileVignetting: 62, Distortion: 63,
			PurpleAmount: 64, PurpleHueLow: 65, PurpleHueHigh: 66,
			GreenAmount: 67, GreenHueLow: 68, GreenHueHigh: 69,
			VignetteAmount: 70, VignetteMidpoint: 71,
		},
		Geometry: CameraRawGeometrySettings{
			Upright: "Guided", Projection: "Rectilinear",
			Vertical: 72, Horizontal: 73, Rotate: 74, Aspect: 75, Scale: 76,
			OffsetX: 77, OffsetY: 78, ConstrainCrop: true,
			Guides: []CameraRawGeometryGuide{{StartX: 0.1, StartY: 0.2, EndX: 0.8, EndY: 0.9}},
		},
		Calibration: CameraRawCalibrationSettings{
			Process: 2, ShadowTint: 79, RedHue: 80, RedSaturation: 81,
			GreenHue: 82, GreenSaturation: 83, BlueHue: 84, BlueSaturation: 85,
		},
	}
}

// 重置一个分区：该分区回到默认，**其余九个逐字段不变**。
func TestResettingOneGroupLeavesTheOthersAlone(t *testing.T) {
	changed := cameraRawAllChanged()
	defaults := DefaultCameraRawSettings()
	for _, group := range CameraRawGroups {
		reset := changed.ResetGroup(group)
		if got, want := reset.KeepingGroup(group), defaults.KeepingGroup(group); !reflect.DeepEqual(got, want) {
			t.Errorf("%s：重置后 = %+v，想要默认 %+v", group, got, want)
		}
		for _, other := range CameraRawGroups {
			if other == group {
				continue
			}
			if got, want := reset.KeepingGroup(other), changed.KeepingGroup(other); !reflect.DeepEqual(got, want) {
				t.Errorf("%s：重置波及了 %s\n得到 %+v\n想要 %+v", group, other, got, want)
			}
		}
	}
}

func TestGroupDefaultDetection(t *testing.T) {
	defaults := DefaultCameraRawSettings()
	for _, group := range CameraRawGroups {
		if !defaults.IsGroupDefault(group) {
			t.Errorf("%s：默认设置下应当算默认", group)
		}
	}
	changed := cameraRawAllChanged()
	for _, group := range CameraRawGroups {
		if changed.IsGroupDefault(group) {
			t.Errorf("%s：被改过却算默认 —— 夹具没覆盖到这个分区", group)
		}
	}
}

// jsonValueIsZero reports whether a decoded JSON value is its type's zero, so a
// group's touched fields can be read off a marshalled struct (which always
// carries every key).
func jsonValueIsZero(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case bool:
		return !t
	case float64:
		return t == 0
	case string:
		return t == ""
	case []any:
		for _, item := range t {
			if !jsonValueIsZero(item) {
				return false
			}
		}
		return true
	case map[string]any:
		for _, item := range t {
			if !jsonValueIsZero(item) {
				return false
			}
		}
		return true
	}
	return false
}

// 键清单必须与 copyCameraRawGroup 实际复制的字段一致。夹具把**每个**字段都改离默认，
// 所以 KeepingGroup 里非零的那些键就是该分区真正拥有的字段 —— 与清单对不上就说明一边加了
// 字段另一边没加，重置会悄悄漏掉或多重置。
func TestGroupJSONKeysMatchTheCopiedFields(t *testing.T) {
	changed := cameraRawAllChanged()
	for _, group := range CameraRawGroups {
		blob, err := json.Marshal(changed.KeepingGroup(group))
		if err != nil {
			t.Fatal(err)
		}
		var all map[string]any
		if err := json.Unmarshal(blob, &all); err != nil {
			t.Fatal(err)
		}
		touched := map[string]bool{}
		for key, value := range all {
			if !jsonValueIsZero(value) {
				touched[key] = true
			}
		}
		listed := map[string]bool{}
		for _, key := range cameraRawGroupJSONKeys[group] {
			listed[key] = true
		}
		for key := range touched {
			if !listed[key] {
				t.Errorf("%s：copyCameraRawGroup 复制了 %q，键清单里没有", group, key)
			}
		}
		for key := range listed {
			if !touched[key] {
				t.Errorf("%s：键清单里有 %q，copyCameraRawGroup 却没复制（或夹具没改到它）", group, key)
			}
		}
	}
}

// 回包只带该分区的键，且零值也要在（调用方需要看到「现在曝光是 0」，而不是「没提曝光」）。
func TestGroupDefaultFieldsCarryOnlyThatGroup(t *testing.T) {
	fields, err := CameraRawGroupDefaultFields(CameraRawGroupLight)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"exposure": true, "contrast": true, "highlights": true, "shadows": true, "whites": true, "blacks": true}
	if len(fields) != len(want) {
		t.Fatalf("Light 回包有 %d 个键，想要 %d：%v", len(fields), len(want), fields)
	}
	for key := range want {
		if _, ok := fields[key]; !ok {
			t.Errorf("Light 回包缺少 %q", key)
		}
	}
	if _, err := CameraRawGroupDefaultFields("Nope"); err == nil {
		t.Fatal("未知分区应当报错")
	}
}

// 默认值不是零值：这些必须逐个钉住，否则有人「顺手简化」成零值就会让重置改变画面。
func TestDefaultCameraRawSettingsKeepTheirNonZeroDefaults(t *testing.T) {
	d := DefaultCameraRawSettings()
	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{"曲线阴影分割", d.Curve.ShadowSplit, 25},
		{"曲线暗部分割", d.Curve.DarkSplit, 50},
		{"曲线亮部分割", d.Curve.LightSplit, 75},
		{"暗角中点", d.VignetteMidpoint, 50},
		{"暗角羽化", d.VignetteFeather, 50},
		{"颗粒大小", d.GrainSize, 25},
		{"颗粒粗糙度", d.GrainRoughness, 50},
		{"分级混合", d.Grading.Blending, 50},
		{"锐化半径", d.Detail.SharpenRadius, 10},
		{"锐化细节", d.Detail.SharpenDetail, 25},
		{"亮度降噪细节", d.Detail.NoiseLuminanceDetail, 50},
		{"色彩降噪细节", d.Detail.NoiseColorDetail, 50},
		{"色彩降噪平滑", d.Detail.NoiseColorSmoothness, 50},
		{"配置畸变", d.Optics.ProfileDistortion, 100},
		{"配置暗角", d.Optics.ProfileVignetting, 100},
		{"紫边色相下", d.Optics.PurpleHueLow, 270},
		{"紫边色相上", d.Optics.PurpleHueHigh, 310},
		{"绿边色相下", d.Optics.GreenHueLow, 60},
		{"绿边色相上", d.Optics.GreenHueHigh, 120},
		{"光学暗角中点", d.Optics.VignetteMidpoint, 50},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s 默认 = %v，想要 %v", c.name, c.got, c.want)
		}
	}
	if d.WhiteBalance != "Custom" {
		t.Errorf("白平衡默认 = %q，想要 Custom", d.WhiteBalance)
	}
	if d.Calibration.Process != 6 {
		t.Errorf("校准 Process 默认 = %d，想要 6", d.Calibration.Process)
	}
	if d.Geometry.Upright != "Off" || d.Geometry.Projection != "Perspective" {
		t.Errorf("几何默认 = %q/%q", d.Geometry.Upright, d.Geometry.Projection)
	}
	if len(d.Curve.RGB) != 2 || d.Curve.RGB[0] != (domain.CurvePoint{X: 0, Y: 0}) || d.Curve.RGB[1] != (domain.CurvePoint{X: 1, Y: 1}) {
		t.Errorf("曲线默认应当是直线两点，得到 %+v", d.Curve.RGB)
	}
}
