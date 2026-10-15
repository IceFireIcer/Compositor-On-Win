package render

import (
	"testing"

	"compositor-win/internal/domain"
)

// TestExposureOnHDRKeepsHighlightsRecoverable is ticket 71: an exposure
// adjustment on an HDR layer must not clip light brighter than white. Turning
// it down brings a highlight into view; the unadjusted raster still holds it.
func TestExposureOnHDRKeepsHighlightsRecoverable(t *testing.T) {
	deep := NewDeep(2, 2, true)
	for i := 0; i < len(deep.Pix); i += 4 {
		deep.Pix[i] = FloatToHalf(4)
		deep.Pix[i+3] = FloatToHalf(1)
	}
	bmp := deep.ToBitmap()
	bmp.Deep = deep
	original := deep.Clone()

	ok := ApplyAdjustment(&domain.Adjustment{
		Kind:             domain.AdjustmentExposure,
		ExposureSettings: &domain.ExposureSettings{Exposure: -2, Offset: 0, Gamma: 1},
	}, bmp)
	if !ok {
		t.Fatal("曝光调整应被识别")
	}
	if bmp.Deep == nil || !bmp.Deep.HDR {
		t.Fatal("曝光后图层应仍是 HDR")
	}
	got := HalfToFloat(bmp.Deep.Pix[0])
	// 4 is the encoded value, not linear light. Two stops down must shrink
	// it without clipping it to white.
	if !(got > 1 && got < 4) {
		t.Fatalf("降两档后高光 = %g，应小于 4 且仍高于白", got)
	}
	if HalfToFloat(original.Pix[0]) < 3 {
		t.Fatal("调整不应改掉调用方仍持有的原栅格")
	}
}
