package render

// 票 82 的内核前提：`DeepLayer.run` 让 8 位形状的操作跑在 16 位图层上而不丢精度。
// 上游锚点：DeepLayerTests.filtersKeepSixteenBitPixels / invertKeepsSixteenBits。

import (
	"testing"
)

// 33000 不是 257 的整数倍 —— 8 位绕路会把它吸到最近的 257 倍数上。
const deepRunValue = 33000

func deepRunRamp(w int) *DeepRaster {
	d := NewDeep(w, 1, false)
	for x := 0; x < w; x++ {
		i := x * 4
		d.Pix[i], d.Pix[i+1], d.Pix[i+2] = deepRunValue, deepRunValue, deepRunValue
		d.Pix[i+3] = 65535
	}
	return d
}

func TestRunDeepKeepsSixteenBitPrecisionThroughAnEightBitOperation(t *testing.T) {
	d := deepRunRamp(4)
	got, err := RunDeep(d, false, func(*Bitmap) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if got.HDR {
		t.Fatal("16 位图层的运行结果不该变成 HDR")
	}
	// 恒等操作应把值放回原处；8 位绕路只会给出 257 的整数倍。
	if got.Pix[0]%257 == 0 {
		t.Fatalf("结果 %d 是 257 的整数倍 —— 精度已经掉到 8 位", got.Pix[0])
	}
	if diff := int(got.Pix[0]) - deepRunValue; diff > 300 || diff < -300 {
		t.Fatalf("恒等操作往返 = %d，想要 ~%d（±300）", got.Pix[0], deepRunValue)
	}
}

// 真的过一个会改变数值的操作：结果仍应比 8 位细。
func TestRunDeepThroughInvertStaysFinerThanEightBits(t *testing.T) {
	d := deepRunRamp(8)
	got, err := RunDeep(d, false, func(b *Bitmap) error {
		ApplyInvert(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 反相之后应当接近 65535-33000。
	if diff := int(got.Pix[0]) - (65535 - deepRunValue); diff > 600 || diff < -600 {
		t.Fatalf("反相往返 = %d，想要 ~%d", got.Pix[0], 65535-deepRunValue)
	}
}

// 混色路径（10 次运行）也要能跑完并且不改尺寸。
func TestRunDeepMixesChannels(t *testing.T) {
	d := deepRunRamp(4)
	calls := 0
	got, err := RunDeep(d, true, func(*Bitmap) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 10 {
		t.Fatalf("混色路径应运行 10 次，得到 %d", calls)
	}
	if got.W != 4 || got.H != 1 {
		t.Fatalf("结果尺寸 = %dx%d，想要 4x1", got.W, got.H)
	}
}

// 非混色路径是 4 次运行。
func TestRunDeepRunsFourTimesWithoutMixing(t *testing.T) {
	d := deepRunRamp(4)
	calls := 0
	if _, err := RunDeep(d, false, func(*Bitmap) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("非混色路径应运行 4 次，得到 %d", calls)
	}
}

// DeepSplit 的通道步进：channel 0 只动红。
func TestDeepSplitStepsOnlyTheNamedChannel(t *testing.T) {
	d := deepRunRamp(2)
	base := DeepSplit(d, 0, -1)
	red := DeepSplit(d, 2, 0)
	for x := 0; x < 2; x++ {
		i := x * 4
		if int(red.Pix[i])-int(base.Pix[i]) != 2 {
			t.Fatalf("红通道步进 = %d，想要 +2", int(red.Pix[i])-int(base.Pix[i]))
		}
		if red.Pix[i+1] != base.Pix[i+1] || red.Pix[i+2] != base.Pix[i+2] {
			t.Fatal("只点名的通道该动，绿蓝应不变")
		}
	}
}
