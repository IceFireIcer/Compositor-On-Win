package render

// 结构检查（票 78）：每个构造器在滑杆取各种值时，**权重非零**的槽都必须指到真实的表。
//
// 为什么值得单独钉：表号一旦越界，`tableAt` 返回 nil，而 `cameraStageColor` 对 nil 表按
// 「直通」处理 —— 于是那个滑杆看起来有效果，实际什么都没做，而且不会有任何报错。这类静默
// 失效只能靠穷举取值点来发现。

import "testing"

func TestEveryWeightedStageSlotPointsAtARealTable(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, stage cameraRawStage) {
		t.Helper()
		for i, table := range stage.tables {
			// A zero-weight slot is never read, so a nil there is harmless.
			if stage.weights[i] == 0 {
				continue
			}
			if table == nil {
				t.Fatalf("%s：槽 %d 权重 %v 却指向 nil 表（表号越界，会静默变成直通）",
					name, i, stage.weights[i])
			}
		}
	}
	values := []float64{-100, -87.5, -75, -50, -25, -12.5, 0, 12.5, 25, 50, 75, 87.5, 100}
	brightnesses := []float64{0, 1, 53, 66, 80, 92, 104, 117, 130, 145, 160, 175, 190, 207, 224, 255}
	for _, v := range values {
		check("exposure", cameraRawExposureStage(set, v))
		check("whiteBalance", cameraRawWhiteBalance(set, v, -v))
		check("whites", cameraRawTableStage(set, cameraRawWhitesStart, v, -100, 25, 9))
		check("blacks", cameraRawTableStage(set, cameraRawBlacksStart, v, -100, 25, 9))
		check("saturation", cameraRawTableStage(set, cameraRawSaturationStart, v, -100, 25, 9))
		check("vibrance", cameraRawTableStage(set, cameraRawVibranceStart, v, -100, 25, 9))
		for _, b := range brightnesses {
			check("contrast", cameraRawAdaptive(set, cameraRawContrastStart, v, b))
			check("highlights", cameraRawAdaptive(set, cameraRawHighlightsStart, v, b))
			check("shadows", cameraRawAdaptive(set, cameraRawShadowsStart, v, b))
		}
		// 混色器与校准的六条、参数曲线的四条，都走 fifty。
		for index := 0; index < 6; index++ {
			check("calibration", cameraRawFifty(set, cameraRawCalibrationStart+index*5, v))
		}
		for index := 0; index < 4; index++ {
			check("parametric", cameraRawFifty(set, cameraRawParametricStart+index*5, v))
		}
		for kind := 0; kind < 3; kind++ {
			for color := 0; color < 8; color++ {
				check("mixer", cameraRawFifty(set, cameraRawMixerStart+(kind*8+color)*5, v))
			}
		}
	}
}

// 整条链（所有滑杆同时拉到端点）的每一槽也必须存在。
func TestFullSettingsChainHasNoDanglingSlots(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	settings := &CameraRawSettings{}
	for _, v := range []float64{-100, 100} {
		settings = &CameraRawSettings{
			Exposure: v, Contrast: v, Highlights: v, Shadows: v, Whites: v, Blacks: v,
			Vibrance: v, Saturation: v, Temperature: v, Tint: -v,
		}
		settings.Calibration = CameraRawCalibrationSettings{
			RedHue: v, RedSaturation: v, GreenHue: v, GreenSaturation: v, BlueHue: v, BlueSaturation: v,
		}
		settings.Curve = CameraRawCurveSettings{
			Shadows: v, Darks: v, Lights: v, Highlights: v,
			ShadowSplit: 25, DarkSplit: 50, LightSplit: 75,
		}
		for i := 0; i < 8; i++ {
			settings.Mixer.Hue[i], settings.Mixer.Saturation[i], settings.Mixer.Luminance[i] = v, v, v
		}
		stages := cameraRawTableStages(set, settings, cameraRawBrightness{brightest: 255, luminance: 255, logBrightest: 255})
		if len(stages) == 0 {
			t.Fatal("所有滑杆都拉满时不该是空链")
		}
		for i, stage := range stages {
			for slot, table := range stage.tables {
				if stage.weights[slot] != 0 && table == nil {
					t.Fatalf("第 %d 段槽 %d 指向 nil 表", i, slot)
				}
			}
		}
	}
}
