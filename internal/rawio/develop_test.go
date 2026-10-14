package rawio

import (
	"math"
	"testing"

	"compositor-win/internal/render"
)

func TestKelvinToGainsDirection(t *testing.T) {
	// 增益中和场景照明：3000K（钨丝灯，偏橙）→ 增益蓝重。
	wr, wg, wb := kelvinToGains(3000, 0)
	if !(wb > wr && wb >= wg) {
		t.Fatalf("3000K 增益应蓝重：r=%v g=%v b=%v", wr, wg, wb)
	}
	cr, cg, cb := kelvinToGains(9000, 0)
	if !(cr > cb) {
		t.Fatalf("9000K 增益应红重：r=%v g=%v b=%v", cr, cg, cb)
	}
	// 6500K 近似中性：没有通道压倒性。
	nr, ng, nb := kelvinToGains(6500, 0)
	if math.Abs(nr-nb) > 0.2 {
		t.Fatalf("6500K 应近中性：r=%v b=%v", nr, nb)
	}
	// Tint 推绿/品红——比较绿相对红蓝的份额（增益按顶通道归一，
	// 绝对值会随归一化漂移）。
	greenShare := func(g, r, b float64) float64 { return g / ((r + b) / 2) }
	pr, pg, pb := kelvinToGains(6500, 30)
	if greenShare(pg, pr, pb) <= greenShare(ng, nr, nb) {
		t.Fatalf("tint>0 应升绿的相对份额")
	}
	mr, mg, mb := kelvinToGains(6500, -30)
	if greenShare(mg, mr, mb) >= greenShare(ng, nr, nb) {
		t.Fatalf("tint<0 应降绿的相对份额")
	}
}

func TestGainsKelvinRoundTrip(t *testing.T) {
	for _, k := range []float64{3000, 5000, 6500, 8000} {
		r, g, b := kelvinToGains(k, 0)
		back := gainsToKelvinEstimate(r, g, b)
		if math.Abs(back-k)/k > 0.15 {
			t.Errorf("%vK 往返得到 %.0fK", k, back)
		}
		_ = g
	}
}

func TestCameraAsShot(t *testing.T) {
	caps := DevelopCaps{NoiseReduction: true, HighlightRecovery: true}
	s := CameraAsShot(1.8, 1.0, 1.2, caps)
	if s.AsShotTemperature != s.Temperature || s.AsShotTint != s.Tint {
		t.Fatalf("asShot 应回填温度与色调: %+v", s)
	}
	if !s.WhiteBalanceIsAsShot() {
		t.Fatal("新设置应处于 asShot 白平衡")
	}
	if s.Boost != 1 || s.Exposure != 0 {
		t.Fatalf("boost/exposure 默认: %+v", s)
	}
	if s.AsShotTemperature < 1000 || s.AsShotTemperature > 40000 {
		t.Fatalf("asShot Kelvin 越界: %v", s.AsShotTemperature)
	}
	// 红增益高 = 相机补了大量红 = 场景偏冷 → 估计温度高于中性 6500。
	if s.AsShotTemperature <= 6500 {
		t.Fatalf("红增益 1.8 应估出冷色温: %v", s.AsShotTemperature)
	}
	// 蓝增益高 = 场景偏暖（橙光）→ 估计温度低于中性 6500。
	warm := CameraAsShot(1.0, 1.0, 1.8, caps)
	if warm.AsShotTemperature >= 6500 {
		t.Fatalf("蓝增益 1.8 应估出暖色温: %v", warm.AsShotTemperature)
	}
	zero := CameraAsShot(0, 0, 0, caps)
	if zero.AsShotTemperature != 5000 {
		t.Fatalf("零增益应回落 5000K: %v", zero.AsShotTemperature)
	}
}

func TestApplyDevelopIdentity(t *testing.T) {
	// 中灰：boost 曲线的中点不动（sCurve(0.5)=0.5）。
	s := DevelopSettings{Temperature: 6500, AsShotTemperature: 6500, Boost: 1}
	pix := developFlat([]uint16{32768}, []uint16{32768}, []uint16{32768}, s)
	// srgbEncode(32768/65535) ≈ 0.7354 → 188；boost 曲线中点不动。票 64 起
	// 8bit 量化带约一级的抖动噪声，所以是 ±1 而不是精确值。
	for c := 0; c < 3; c++ {
		if pix[c] < 187 || pix[c] > 189 {
			t.Fatalf("中灰应得 188±1：得到 %v", pix[0:3])
		}
	}
	if pix[3] != 255 {
		t.Fatal("RAW 帧应不透明")
	}
}

func TestApplyDevelopExposureAndClamp(t *testing.T) {
	s := DevelopSettings{Exposure: 1, Temperature: 6500, AsShotTemperature: 6500, Boost: 1}
	pix := developFlat([]uint16{32768}, []uint16{65535}, []uint16{0}, s)
	// 推满到 255 后，抖动噪声是 ±1 级（C 的 255/65535 定标也一样），
	// 所以是 254..255 而不是精确的 255。
	if pix[0] < 254 {
		t.Fatalf("+1 档应把中灰推满：得到 %v", pix[0])
	}
	if pix[2] > 1 {
		t.Fatalf("零输入仍应约为零: %v", pix[2])
	}
}

func TestApplyDevelopBoostBlends(t *testing.T) {
	// x=0.25：flat ≈ 0.537，full ≈ 0.493 —— boost 0 与 1 必须不同。
	gray := uint16(16384)
	flat := developFlat([]uint16{gray}, []uint16{gray}, []uint16{gray},
		DevelopSettings{Temperature: 6500, AsShotTemperature: 6500, Boost: 0})
	full := developFlat([]uint16{gray}, []uint16{gray}, []uint16{gray},
		DevelopSettings{Temperature: 6500, AsShotTemperature: 6500, Boost: 1})
	if flat[0] == full[0] {
		t.Fatalf("boost 应改变色调：flat=%d full=%d", flat[0], full[0])
	}
	half := developFlat([]uint16{gray}, []uint16{gray}, []uint16{gray},
		DevelopSettings{Temperature: 6500, AsShotTemperature: 6500, Boost: 0.5})
	if half[0] == flat[0] || half[0] == full[0] {
		t.Fatalf("boost 0.5 应介于两端之间: flat=%d half=%d full=%d", flat[0], half[0], full[0])
	}
}

func TestApplyDevelopWhiteBalance(t *testing.T) {
	// asShot 相同 → 不再施加白平衡（相机增益已在解码里跑过）。
	same := DevelopSettings{Temperature: 5000, AsShotTemperature: 5000, Boost: 1}
	// 抖动噪声逐通道不同，单像素会差一级；取一片像素的均值看是否仍中性。
	const n = 256
	rs := make([]uint16, n)
	gs := make([]uint16, n)
	bs := make([]uint16, n)
	for i := range rs {
		rs[i], gs[i], bs[i] = 32768, 32768, 32768
	}
	neutral := developFlat(rs, gs, bs, same)
	var sumR, sumB float64
	for i := 0; i < n; i++ {
		sumR += float64(neutral[i*4])
		sumB += float64(neutral[i*4+2])
	}
	if math.Abs(sumR-sumB)/n > 0.2 {
		t.Fatalf("asShot 下不应再着色：R %.2f B %.2f", sumR/n, sumB/n)
	}
	// 拉到 3000K（场景其实偏暖）→ 增益补蓝中和，画面变冷：蓝通道超过红。
	cooler := DevelopSettings{Temperature: 3000, AsShotTemperature: 6500, Boost: 1}
	out := developFlat([]uint16{32768}, []uint16{32768}, []uint16{32768}, cooler)
	if out[2] <= out[0] {
		t.Fatalf("低于 asShot 的色温应把画面调冷: %v", out[0:3])
	}
	hot := DevelopSettings{Temperature: 10000, AsShotTemperature: 6500, Boost: 1}
	hotOut := developFlat([]uint16{32768}, []uint16{32768}, []uint16{32768}, hot)
	if hotOut[0] <= hotOut[2] {
		t.Fatalf("高于 asShot 的色温应把画面调暖: %v", hotOut[0:3])
	}
}

// --- 票 64：RAW 显影的降噪/镜头校正/高光恢复与 8bit 抖动 ---

func TestCameraAsShotCapabilities(t *testing.T) {
	caps := DevelopCaps{NoiseReduction: true, HighlightRecovery: true}
	asShot := CameraAsShot(1.8, 1.0, 1.2, caps)
	if !asShot.SupportsNoiseReduction || !asShot.SupportsHighlightRecovery {
		t.Fatalf("能力位应随相机读入: %+v", asShot)
	}
	if asShot.SupportsLensCorrection {
		t.Fatal("LibRaw 无镜头配置文件，镜头校正能力位必须恒为 false")
	}
	if !asShot.WhiteBalanceIsAsShot() {
		t.Fatalf("相机自己的读数应算 asShot 白平衡: %+v", asShot)
	}
}

// 引擎控件（降噪、镜头校正、高光恢复）不是白平衡：动它们不该让显影离开
// 相机自己的乘数——这正是票 64 修掉的坑（原先用上游的 isAsShot 判定）。
func TestWhiteBalanceIsAsShotIgnoresEngineControls(t *testing.T) {
	s := CameraAsShot(1.8, 1.0, 1.2, DevelopCaps{NoiseReduction: true, HighlightRecovery: true})
	s.NoiseReduction = 0.4
	s.LensCorrection = true
	s.HighlightRecovery = true
	s.Exposure, s.Boost = 1.5, 0
	if !s.WhiteBalanceIsAsShot() {
		t.Fatalf("只有色温/色调离开 asShot 才算离开: %+v", s)
	}
	s.Temperature = s.AsShotTemperature + 100
	if s.WhiteBalanceIsAsShot() {
		t.Fatal("色温离开 asShot 后不应再报 asShot")
	}
	s.Temperature = s.AsShotTemperature
	s.Tint = s.AsShotTint + 5
	if s.WhiteBalanceIsAsShot() {
		t.Fatal("色调离开 asShot 后不应再报 asShot")
	}
}

func TestCameraAsShotWithoutCapabilities(t *testing.T) {
	// 相机不支持时能力位为 false，asShot 起点仍是合法状态。
	s := CameraAsShot(2.0, 1.0, 1.5, DevelopCaps{})
	if s.SupportsNoiseReduction || s.SupportsHighlightRecovery || s.SupportsLensCorrection {
		t.Fatalf("无能力位时 supports* 应全 false: %+v", s)
	}
	if !s.WhiteBalanceIsAsShot() {
		t.Fatalf("无能力位也应处于 asShot 白平衡: %+v", s)
	}
}

func TestApplyDevelopIsRepeatable(t *testing.T) {
	// A real frame shape, not one long row: the dither hashes (x, y), so the
	// shape is part of the result and this pins that ApplyDevelop threads it
	// through instead of flattening the frame.
	const width, height = 8, 8
	settings := DevelopSettings{Temperature: 6500, AsShotTemperature: 6500, Boost: 1}
	r16 := make([]uint16, width*height)
	g16 := make([]uint16, width*height)
	b16 := make([]uint16, width*height)
	for i := range r16 {
		r16[i] = uint16(i * 1024)
		g16[i] = uint16(i * 512)
		b16[i] = uint16(65535 - i*1000)
	}
	first := ApplyDevelop(r16, g16, b16, settings, width, height)
	second := ApplyDevelop(r16, g16, b16, settings, width, height)
	if len(first) != len(r16)*4 {
		t.Fatalf("输出长度应为 n*4: %d", len(first))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("同一份显影两次应逐字节相同：下标 %d %d≠%d", i, first[i], second[i])
		}
	}
	// 抖动就是 Quantize16：先在 16bit 域生成预乘 RGBA，再逐字节量化。
	wide := ApplyDevelopWide(r16, g16, b16, settings)
	want := render.Quantize16(wide, width, height)
	for i := range first {
		if first[i] != want[i] {
			t.Fatalf("ApplyDevelop 应与 Quantize16(ApplyDevelopWide) 一致：下标 %d", i)
		}
	}
	// The frame's shape reaches the dither: the same pixels as one long row
	// must give a different noise field.
	flat := render.Quantize16(wide, width*height, 1)
	same := true
	for i := range first {
		if first[i] != flat[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("the dither must hash each pixel by its (x, y), not by the flat index")
	}
}

func TestApplyDevelopGradientDoesNotBand(t *testing.T) {
	const width, height = 64, 256
	r16 := make([]uint16, width*height)
	g16 := make([]uint16, width*height)
	b16 := make([]uint16, width*height)
	ideal := make([]float64, height)
	for y := 0; y < height; y++ {
		// 0.10…0.90 的 16bit 渐变。显影把线性值经 sRGB 编码 + boost 曲线送到
		// 8bit，所以理想值是编码后的值：普通四舍五入只给约 205 级，抖动后
		// 每一行都该落在自己的理想值附近。
		tone := 0.10 + 0.80*(float64(y)+0.5)/float64(height)
		ideal[y] = srgbEncode(sCurve(tone)) * 255
		v := uint16(tone*65535 + 0.5)
		for x := 0; x < width; x++ {
			i := y*width + x
			r16[i], g16[i], b16[i] = v, v, v
		}
	}
	settings := DevelopSettings{Temperature: 6500, AsShotTemperature: 6500, Boost: 1}
	pix := ApplyDevelop(r16, g16, b16, settings, width, height)
	levels := map[uint8]bool{}
	for y := 0; y < height; y++ {
		sum := 0
		for x := 0; x < width; x++ {
			v := pix[(y*width+x)*4]
			sum += int(v)
			levels[v] = true
		}
		mean := float64(sum) / float64(width)
		// 行均值跟住渐变（单像素量化误差 ±0.5，64 像素平均后远小于 0.5）。
		if math.Abs(mean-ideal[y]) > 0.5 {
			t.Fatalf("第 %d 行均值偏离理想值：%v vs %v", y, mean, ideal[y])
		}
	}
	// 平滑渐变的量化结果不应只剩几级（那正是色带）。
	if len(levels) < 100 {
		t.Fatalf("渐变应产生大量中间级，实际 %d 级", len(levels))
	}
}

// developFlat runs the develop's last step on a one-row frame. The dither's
// noise is hashed from each pixel's (x, y), so a frame handed over flat gets a
// different field than the camera's grid; tests whose subject is the develop
// math rather than the noise field use this, and the shape-dependent ones call
// ApplyDevelop directly.
func developFlat(r, g, b []uint16, s DevelopSettings) []uint8 {
	return ApplyDevelop(r, g, b, s, len(r), 1)
}

// BenchmarkApplyDevelop is the Go-side develop cost per frame: exposure, white
// balance and the boost curve over the 16-bit render, then the dithered round
// to 8 bits. The LibRaw decode that precedes it needs a camera file and the
// repository ships none (ticket 91 owns upstream assets for CI), so this is
// the half of the import that can be measured here.
func BenchmarkApplyDevelop(b *testing.B) {
	const width, height = 2000, 1500
	r16 := make([]uint16, width*height)
	g16 := make([]uint16, width*height)
	blue16 := make([]uint16, width*height)
	for i := range r16 {
		r16[i], g16[i], blue16[i] = uint16(i*7), uint16(i*5), uint16(i*11)
	}
	settings := DevelopSettings{Temperature: 6500, AsShotTemperature: 6500, Boost: 1}
	b.SetBytes(int64(width * height * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ApplyDevelop(r16, g16, blue16, settings, width, height)
	}
}
