package rawio

import (
	"math"
	"testing"
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
	s := CameraAsShot(1.8, 1.0, 1.2)
	if s.AsShotTemperature != s.Temperature || s.AsShotTint != s.Tint {
		t.Fatalf("asShot 应回填温度与色调: %+v", s)
	}
	if !s.IsAsShot() {
		t.Fatal("新设置应处于 asShot 状态")
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
	warm := CameraAsShot(1.0, 1.0, 1.8)
	if warm.AsShotTemperature >= 6500 {
		t.Fatalf("蓝增益 1.8 应估出暖色温: %v", warm.AsShotTemperature)
	}
	zero := CameraAsShot(0, 0, 0)
	if zero.AsShotTemperature != 5000 {
		t.Fatalf("零增益应回落 5000K: %v", zero.AsShotTemperature)
	}
}

func TestApplyDevelopIdentity(t *testing.T) {
	// 中灰：boost 曲线的中点不动（sCurve(0.5)=0.5）。
	s := DevelopSettings{Temperature: 6500, AsShotTemperature: 6500, Boost: 1}
	pix := ApplyDevelop([]uint16{32768}, []uint16{32768}, []uint16{32768}, s)
	// srgbEncode(32768/65535) ≈ 0.7354 → 188；boost 曲线中点不动。
	if pix[0] != 188 || pix[1] != 188 || pix[2] != 188 {
		t.Fatalf("中灰应得 185：得到 %v", pix[0:3])
	}
	if pix[3] != 255 {
		t.Fatal("RAW 帧应不透明")
	}
}

func TestApplyDevelopExposureAndClamp(t *testing.T) {
	s := DevelopSettings{Exposure: 1, Temperature: 6500, AsShotTemperature: 6500, Boost: 1}
	pix := ApplyDevelop([]uint16{32768}, []uint16{65535}, []uint16{0}, s)
	if pix[0] != 255 {
		t.Fatalf("+1 档应把中灰推满：得到 %v", pix[0])
	}
	if pix[2] != 0 {
		t.Fatalf("零输入仍应为零: %v", pix[2])
	}
}

func TestApplyDevelopBoostBlends(t *testing.T) {
	// x=0.25：flat ≈ 0.537，full ≈ 0.493 —— boost 0 与 1 必须不同。
	gray := uint16(16384)
	flat := ApplyDevelop([]uint16{gray}, []uint16{gray}, []uint16{gray},
		DevelopSettings{Temperature: 6500, AsShotTemperature: 6500, Boost: 0})
	full := ApplyDevelop([]uint16{gray}, []uint16{gray}, []uint16{gray},
		DevelopSettings{Temperature: 6500, AsShotTemperature: 6500, Boost: 1})
	if flat[0] == full[0] {
		t.Fatalf("boost 应改变色调：flat=%d full=%d", flat[0], full[0])
	}
	half := ApplyDevelop([]uint16{gray}, []uint16{gray}, []uint16{gray},
		DevelopSettings{Temperature: 6500, AsShotTemperature: 6500, Boost: 0.5})
	if half[0] == flat[0] || half[0] == full[0] {
		t.Fatalf("boost 0.5 应介于两端之间: flat=%d half=%d full=%d", flat[0], half[0], full[0])
	}
}

func TestApplyDevelopWhiteBalance(t *testing.T) {
	// asShot 相同 → 不再施加白平衡（相机增益已在解码里跑过）。
	same := DevelopSettings{Temperature: 5000, AsShotTemperature: 5000, Boost: 1}
	neutral := ApplyDevelop([]uint16{32768}, []uint16{32768}, []uint16{32768}, same)
	if neutral[0] != neutral[2] {
		t.Fatalf("asShot 下不应再着色: %v", neutral[0:3])
	}
	// 拉到 3000K（场景其实偏暖）→ 增益补蓝中和，画面变冷：蓝通道超过红。
	cooler := DevelopSettings{Temperature: 3000, AsShotTemperature: 6500, Boost: 1}
	out := ApplyDevelop([]uint16{32768}, []uint16{32768}, []uint16{32768}, cooler)
	if out[2] <= out[0] {
		t.Fatalf("低于 asShot 的色温应把画面调冷: %v", out[0:3])
	}
	hot := DevelopSettings{Temperature: 10000, AsShotTemperature: 6500, Boost: 1}
	hotOut := ApplyDevelop([]uint16{32768}, []uint16{32768}, []uint16{32768}, hot)
	if hotOut[0] <= hotOut[2] {
		t.Fatalf("高于 asShot 的色温应把画面调暖: %v", hotOut[0:3])
	}
}
