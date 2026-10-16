package render

// 票 78 的接线：Light/Color 走 Photoshop 的测量表，解析路径只在查表不可用时兜底。
// 这条测试证明接线**真的生效** —— 既有的 Camera Raw 测试没有钉住色调数值，所以接线
// 之后它们仍然全绿，光靠它们看不出走的是哪条路。

import (
	"math"
	"testing"
)

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
	got := ApplyCameraRawFilter(cameraRawRamp(), settings, CameraRawOptions{Scale: 1, VisualizePointColor: -1})

	// 解析路径把曝光当 2^exposure 的线性增益，输入 204 会直接削到 255；测量表保留
	// 高光滚降（这正是 Photoshop 的行为，也是接线的意义）。
	if got.Pix[12*4] == 255 {
		t.Fatalf("曝光 +1 把输入 204 削成了 255 —— 看起来还在走解析路径")
	}
	// 而且与直接走表逐字节一致。
	want := cameraRawRamp()
	if !applyCameraRawTable(want, settings, 0, false) {
		t.Fatal("表路径不可用")
	}
	for i := range want.Pix {
		if got.Pix[i] != want.Pix[i] {
			t.Fatalf("下标 %d：过滤器 %d，直接走表 %d", i, got.Pix[i], want.Pix[i])
		}
	}
}

// 票 78 收尾：曲线与混色器折进查找表（上游 foldingColor）。折进表的意义是整条 Camera Raw
// 链仍只花一次查表；风险是**折的时候漏掉某一段**，那一段就静默失效。

// 票 90：vibrance 与 saturation 的**定义性区别** —— vibrance 主要抬不饱和的颜色，对已经饱和的
// 颜色下手轻得多；saturation 一视同仁。这两根滑杆在界面上挨着，退化成一个最不容易被发现。
func TestVibranceGainsMoreOnAMutedColorThanOnASaturatedOne(t *testing.T) {
	if !CameraRawTablesAvailable() {
		t.Skip("没有随包资源时这两根滑杆走解析核，测不到 Photoshop 的行为")
	}
	// 两个像素：偏灰的蓝，和纯蓝。
	build := func() *Bitmap {
		b := NewBitmap(2, 1)
		// 偏灰的蓝，和明显饱和但**还有余量**的蓝 —— 纯蓝已经到顶，再抬也无处可去。
		copy(b.Pix[0:], []uint8{120, 140, 170, 255})
		copy(b.Pix[4:], []uint8{40, 60, 200, 255})
		return b
	}
	saturationAt := func(b *Bitmap, i int) float64 {
		r, g, bl := float64(b.Pix[i]), float64(b.Pix[i+1]), float64(b.Pix[i+2])
		maxc := math.Max(r, math.Max(g, bl))
		minc := math.Min(r, math.Min(g, bl))
		if maxc == 0 {
			return 0
		}
		return (maxc - minc) / maxc
	}
	base := build()
	apply := func(s CameraRawSettings) *Bitmap {
		return ApplyCameraRawFilter(build(), s, CameraRawOptions{Scale: 1, VisualizePointColor: -1})
	}
	vibrance := apply(CameraRawSettings{Vibrance: 50})

	if got := saturationAt(base, 0); got > 0.5 {
		t.Fatalf("夹具的第一个像素应当是不饱和的，得到 %v", got)
	}
	if got := saturationAt(base, 4); got < 0.5 {
		t.Fatalf("夹具的第二个像素应当是明显饱和的，得到 %v", got)
	}
	// **相对**增益：绝对 chroma 的变化量总是饱和色更大（它的起点就大），而「对已饱和的颜色下手
	// 更轻」说的是相对自己原来的饱和度。
	relative := func(b *Bitmap, index int) float64 {
		before := saturationAt(base, index)
		return (saturationAt(b, index) - before) / before
	}
	vMuted := relative(vibrance, 0)
	vSaturated := relative(vibrance, 4)
	if vMuted <= vSaturated {
		t.Errorf("vibrance 对不饱和像素的相对增益 %v 应当高于对饱和像素的 %v", vMuted, vSaturated)
	}

	// 这条**不**去比较 vibrance 与 saturation 的绝对强弱。实测两者在这些样本上方向相同
	// （vibrance 0.43/0.25，saturation 0.30/0.15），因为饱和的那一端本来就贴近上限 —— 等滑杆值
	// 比较分不出这两根滑杆，硬写一条「vibrance 更保护」的断言只是在拟合数字。这两根滑杆的权威
	// 真值是 Photoshop 的测量表本身，由 TestCameraRawApplyMatchesC 与各 stage builder 钉住。
}

// 只有混色器调整（Light/Color 一个滑杆都没动）时也必须生效。这一条钉的是闸门：折进表之后
// paintColor 为假，如果闸门还只认 adjustsLight/adjustsColor，混色器会被整个丢掉。
func TestAMixerOnlyAdjustmentStillApplies(t *testing.T) {
	before := cameraRawRamp()
	settings := CameraRawSettings{}
	settings.Mixer.Luminance[0] = 60 // 红通道提亮
	if settings.adjustsLight() || settings.adjustsColor() {
		t.Fatal("夹具不该触发 light/color，否则证明不了这条路径")
	}
	got := ApplyCameraRawFilter(cameraRawRamp(), settings, CameraRawOptions{Scale: 1, VisualizePointColor: -1})
	changed := false
	for i := range before.Pix {
		if got.Pix[i] != before.Pix[i] {
			changed = true
			break
		}
	}
	if !changed {
		t.Fatal("只有混色器调整时像素完全没变 —— 折进表的闸门把它漏掉了")
	}
}

// 只有曲线调整时同样必须生效（曲线是折进表的另一段）。
func TestACurveOnlyAdjustmentStillApplies(t *testing.T) {
	before := cameraRawRamp()
	settings := CameraRawSettings{}
	// 分割点摆成 Photoshop 测得的那组（面板的默认），参数曲线由测量表画，曲线这一遍只做点曲线。
	settings.Curve = CameraRawCurveSettings{Shadows: 50, ShadowSplit: 25, DarkSplit: 50, LightSplit: 75}
	got := ApplyCameraRawFilter(cameraRawRamp(), settings, CameraRawOptions{Scale: 1, VisualizePointColor: -1})
	changed := false
	for i := range before.Pix {
		if got.Pix[i] != before.Pix[i] {
			changed = true
			break
		}
	}
	if !changed {
		t.Fatal("只有曲线调整时像素完全没变 —— 折进表把它漏掉了")
	}
}

// 颜色分级**没有**折进表（它的阶段还没移植），所以它必须走逐像素那一遍 —— 一旦折了，
// 分级会被静默丢掉。canFold 里那条 !s.Grading.Adjusts() 就是为它写的。
func TestGradingIsNotLostWhenFoldingIsPossible(t *testing.T) {
	before := cameraRawRamp()
	settings := CameraRawSettings{}
	settings.Grading.Midtones = CameraRawGradeWheel{Saturation: 80, Hue: 120}
	got := ApplyCameraRawFilter(cameraRawRamp(), settings, CameraRawOptions{Scale: 1, VisualizePointColor: -1})
	changed := false
	for i := range before.Pix {
		if got.Pix[i] != before.Pix[i] {
			changed = true
			break
		}
	}
	if !changed {
		t.Fatal("分级调整时像素完全没变 —— 折进表把它漏掉了")
	}
}

// 折叠与逐像素两条路应当给出接近的结果：折进表会把曲线量化到 33³ 的网格上，所以不逐字节
// 相同，但不该差出可见的量级。
func TestFoldingAgreesWithThePerPixelPass(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	_ = set
	settings := CameraRawSettings{}
	// 用一条**平缓**的曲线：折进表会把曲线量化到 33³ 的网格上，曲线越陡这条比较越没有
	// 意义（陡到每 1/32 步就移动十几级时，两条路本来就会差那么多）。
	settings.Curve.Highlights = -20
	folded := ApplyCameraRawFilter(cameraRawRamp(), settings, CameraRawOptions{Scale: 1, VisualizePointColor: -1})
	perPixel := cameraRawRamp()
	curve := settings.Curve
	tone := BuildCameraRawToneTable(curve)
	red := BuildCameraRawChannelTable(curve.Red)
	green := BuildCameraRawChannelTable(curve.Green)
	blue := BuildCameraRawChannelTable(curve.Blue)
	ApplyCameraRawCurveColor(perPixel, tone[:], red[:], green[:], blue[:],
		curve.RefineSaturation/100, make([]float32, 24), 0, nil, make([]float32, 12), 0.5, 0, -1)
	worst := 0
	for i := range folded.Pix {
		if d := int(folded.Pix[i]) - int(perPixel.Pix[i]); d > worst {
			worst = d
		} else if -d > worst {
			worst = -d
		}
	}
	if worst > 6 {
		t.Fatalf("折叠与逐像素相差 %d 级 —— 折进表可能漏了或错了某一段", worst)
	}
}

// 票 78 收尾时发现的真 bug：CameraRawPointColor 原先没有 Visualize 字段，前端那个「可视化」
// 勾选框的值在桥接处被丢掉；而桥接又用 CameraRawOptions 的零值当索引 —— 0 读作「可视化第 0 个
// 拾取点」，于是有拾取点时一次普通编辑会把画布压暗，且 paintColor 永远为真、折进表永不生效。
func TestVisualizeIndexIsMinusOneUnlessAPointAsksForIt(t *testing.T) {
	var empty CameraRawMixerSettings
	if got := empty.VisualizeIndex(); got != -1 {
		t.Fatalf("没有拾取点时 = %d，想要 -1（-1 才是「关闭」）", got)
	}
	// 有拾取点但都没勾：仍然是 -1。这一条钉的就是那个零值陷阱。
	withPoints := CameraRawMixerSettings{Points: []CameraRawPointColor{{Hue: 10}, {Hue: 20}}}
	if got := withPoints.VisualizeIndex(); got != -1 {
		t.Fatalf("没勾可视化时 = %d，想要 -1", got)
	}
	// 勾了第二个：索引是 1。
	withPoints.Points[1].Visualize = true
	if got := withPoints.VisualizeIndex(); got != 1 {
		t.Fatalf("勾了第二个 = %d，想要 1", got)
	}
}

// 一个滑杆都不设时整条过滤器短路：位图逐字节不动。
func TestCameraRawFilterLeavesAnUnadjustedPictureAlone(t *testing.T) {
	plain := cameraRawRamp()
	before := append([]uint8(nil), plain.Pix...)
	out := ApplyCameraRawFilter(plain, CameraRawSettings{}, CameraRawOptions{Scale: 1, VisualizePointColor: -1})
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
	got := ApplyCameraRawFilter(cameraRawRamp(), settings, CameraRawOptions{Scale: 1, VisualizePointColor: -1})
	want := cameraRawRamp()
	if !applyCameraRawTable(want, settings, 0, false) {
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
