package render

// 票 78 的接线：Light/Color 走 Photoshop 的测量表，解析路径只在查表不可用时兜底。
// 这条测试证明接线**真的生效** —— 既有的 Camera Raw 测试没有钉住色调数值，所以接线
// 之后它们仍然全绿，光靠它们看不出走的是哪条路。

import "testing"

func cameraRawRamp() *Bitmap {
	b := NewBitmap(16, 1)
	for x := 0; x < 16; x++ {
		i := x * 4
		v := uint8(x * 17)
		b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = v, v, v, 255
	}
	return b
}

func TestCameraRawFilterGradesWithTheMeasuredTables(t *testing.T) {
	if !CameraRawTablesAvailable() {
		t.Fatal("随包资源应当可用，否则这条测试证明不了接线")
	}
	settings := CameraRawSettings{Exposure: 1}
	got := ApplyCameraRawFilter(cameraRawRamp(), settings, CameraRawOptions{Scale: 1})

	// 解析路径把曝光当 2^exposure 的线性增益，输入 204 会直接削到 255；测量表保留
	// 高光滚降（这正是 Photoshop 的行为，也是接线的意义）。
	if got.Pix[12*4] == 255 {
		t.Fatalf("曝光 +1 把输入 204 削成了 255 —— 看起来还在走解析路径")
	}
	// 而且与直接走表逐字节一致。
	want := cameraRawRamp()
	if !applyCameraRawTable(want, settings, 0) {
		t.Fatal("表路径不可用")
	}
	for i := range want.Pix {
		if got.Pix[i] != want.Pix[i] {
			t.Fatalf("下标 %d：过滤器 %d，直接走表 %d", i, got.Pix[i], want.Pix[i])
		}
	}
}

// 一个滑杆都不设时整条过滤器短路：位图逐字节不动。
func TestCameraRawFilterLeavesAnUnadjustedPictureAlone(t *testing.T) {
	plain := cameraRawRamp()
	before := append([]uint8(nil), plain.Pix...)
	out := ApplyCameraRawFilter(plain, CameraRawSettings{}, CameraRawOptions{Scale: 1})
	for i := range before {
		if out.Pix[i] != before[i] {
			t.Fatalf("没有设置时不该改动：下标 %d %d ≠ %d", i, out.Pix[i], before[i])
		}
	}
}

// 只调校准原色（不碰 light/color）也必须生效。上游的闸门是「阶段非空」，而校准原色
// 就在阶段里；我接线时先写成只认 adjustsLight/adjustsColor，于是「只调校准」这条路上
// 原色既没进表、也没走解析内核 —— 一次都没被应用。这条是那个回归的钉子。
func TestCameraRawFilterAppliesCalibrationPrimariesAlone(t *testing.T) {
	settings := CameraRawSettings{}
	settings.Calibration.RedHue = 50
	if !settings.Calibration.Adjusts() {
		t.Fatal("夹具本身要触发校准")
	}
	if settings.adjustsLight() || settings.adjustsColor() {
		t.Fatal("夹具不该触发 light/color，否则证明不了这条路径")
	}
	got := ApplyCameraRawFilter(cameraRawRamp(), settings, CameraRawOptions{Scale: 1})
	want := cameraRawRamp()
	if !applyCameraRawTable(want, settings, 0) {
		t.Fatal("表路径不可用")
	}
	changed := false
	for i := range want.Pix {
		if got.Pix[i] != want.Pix[i] {
			t.Fatalf("下标 %d：过滤器 %d，直接走表 %d", i, got.Pix[i], want.Pix[i])
		}
		if want.Pix[i] != cameraRawRamp().Pix[i] {
			changed = true
		}
	}
	if !changed {
		t.Fatal("只调校准原色时像素完全没变 —— 原色被漏掉了")
	}
}

// 解析内核仍在（查表不可用时的兜底），而且它自己仍然能工作。
func TestAnalyticCameraRawKernelStillGrades(t *testing.T) {
	b := cameraRawRamp()
	ApplyCameraRaw(b, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0)
	if b.Pix[8*4] == 8*17 {
		t.Fatal("解析内核没有改变像素")
	}
}
