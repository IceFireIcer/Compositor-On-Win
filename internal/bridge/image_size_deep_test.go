package bridge

import "testing"

// TestImageSizeResamplesSixteenBitLayers is ticket 69: scaling a 16-bit layer
// must resample the deep raster, not the dithered 8-bit copy. Widening the
// resampled copy back would invent steps the 16-bit samples never had.
func TestImageSizeResamplesSixteenBitLayers(t *testing.T) {
	svc, _ := newTestService(t, 4, 4)
	sess := activeSessionOf(t, svc)
	key := attachDeep(t, sess)
	deep := sess.bitmaps[key].Deep
	// A value an 8-bit copy cannot hold: 100 sits between two 8-bit steps.
	for i := 0; i < len(deep.Pix); i += 4 {
		deep.Pix[i] = 100
		deep.Pix[i+1] = 100
		deep.Pix[i+2] = 100
		deep.Pix[i+3] = 65535
	}
	derived := deep.ToBitmap()
	derived.Deep = deep
	sess.bitmaps[key] = derived

	if _, err := svc.ImageSize(`{"width":8,"height":8,"resolution":72,"sampling":"Nearest"}`); err != nil {
		t.Fatalf("图像尺寸: %v", err)
	}
	layer := sess.doc.Layers[0]
	got := sess.bitmaps[*layer.ImageFile]
	if got == nil || got.Deep == nil || got.Deep.HDR {
		t.Fatal("16 位图层缩放后应仍是 16 位")
	}
	if got.Deep.W != 8 || got.Deep.H != 8 {
		t.Fatalf("深层尺寸 %d×%d，想要 8×8", got.Deep.W, got.Deep.H)
	}
	// The field is flat, so every sample of the resample — whichever source
	// pixel the box rounding lands on — is 100. An 8-bit detour would come
	// back as v*257, which 100 is not.
	for i := 0; i < len(got.Deep.Pix); i += 4 {
		if got.Deep.Pix[i] != 100 || got.Deep.Pix[i+3] != 65535 {
			t.Fatalf("重采样后样本 [%d] = %d，想要 100", i, got.Deep.Pix[i])
		}
	}
}
