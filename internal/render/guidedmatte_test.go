package render

import "testing"

// The guided matte has no C kernel — the numbers are pinned here: the box
// mean conserves constants, the filter follows the guide's edges, and the
// refine chain's shift/contrast behave as documented.

func TestGuidedBoxConstantStaysConstant(t *testing.T) {
	src := make([]float32, 16*16)
	for i := range src {
		src[i] = 0.3
	}
	out := GuidedBox(src, 16, 16, 3)
	for i, v := range out {
		if v < 0.2999 || v > 0.3001 {
			t.Fatalf("constant plane drifted at %d: %v", i, v)
		}
	}
}

func TestGuidedFilterFollowsGuideEdges(t *testing.T) {
	w, h := 32, 32
	guide := make([]float32, w*h) // 0 left, 1 right
	mask := make([]float32, w*h)  // the guide, softly pulled toward gray
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			guide[y*w+x] = 0
			if x >= w/2 {
				guide[y*w+x] = 1
			}
			mask[y*w+x] = guide[y*w+x]*0.8 + 0.1
		}
	}
	out := GuidedFilter(mask, guide, w, h, 2, 1e-4)
	// The filter re-derives the linear mask↔guide relationship and snaps the
	// result onto the guide's edges: the softened mask comes back sharp.
	if out[4*w+2] >= 0.11 {
		t.Fatalf("left side should track the low guide: %v", out[4*w+2])
	}
	if out[4*w+w-3] <= 0.89 {
		t.Fatalf("right side should track the high guide: %v", out[4*w+w-3])
	}
	for i, v := range out {
		if v < 0 || v > 1 {
			t.Fatalf("result out of 0–1 at %d: %v", i, v)
		}
	}
}

func TestRefineSubjectMaskShiftAndContrast(t *testing.T) {
	w, h := 32, 32
	mask := NewBitmap(w, h) // a half-plane mask
	guide := NewBitmap(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			guide.Pix[i], guide.Pix[i+1], guide.Pix[i+2], guide.Pix[i+3] = 255, 255, 255, 255
			if x < w/2 {
				v := uint8(180)
				mask.Pix[i], mask.Pix[i+1], mask.Pix[i+2], mask.Pix[i+3] = v, v, v, 255
			} else {
				mask.Pix[i], mask.Pix[i+1], mask.Pix[i+2], mask.Pix[i+3] = 40, 40, 40, 255
			}
		}
	}
	base := RefineSubjectMask(mask, guide, 2, 0, 0, 1400)
	// Shift +2 grows the mask: the midline pixel is kept in the refined mask.
	shifted := RefineSubjectMask(mask, guide, 2, 2, 0, 1400)
	if shifted.Pix[(16*w/2+w/2)*4] <= base.Pix[(16*w/2+w/2)*4] {
		t.Fatal("正偏移应当长出掩码")
	}
	// Contrast 100 is a hard cut at the middle: the 180 gray goes to 1, the 40 to 0.
	hard := RefineSubjectMask(mask, guide, 2, 0, 100, 1400)
	for y := 0; y < h; y++ {
		left := hard.Pix[(y*w+w/4)*4]
		right := hard.Pix[(y*w+3*w/4)*4]
		if left != 255 || right != 0 {
			t.Fatalf("对比 100 应二值化: 左 %v 右 %v", left, right)
		}
	}
}

func TestApplySubjectMaskScalesPremultiplied(t *testing.T) {
	b := NewBitmap(2, 2)
	for i := 0; i < len(b.Pix); i += 4 {
		b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = 200, 200, 200, 200
	}
	mask := NewBitmap(2, 2)
	for i := 0; i < len(mask.Pix); i += 4 {
		mask.Pix[i], mask.Pix[i+1], mask.Pix[i+2], mask.Pix[i+3] = 128, 128, 128, 255
	}
	ApplySubjectMask(b, mask)
	if b.Pix[0] != 100 || b.Pix[3] != 100 {
		t.Fatalf("蒙版 50%% 应把预乘值减半: %v/%v", b.Pix[0], b.Pix[3])
	}
}

func TestLargestSubjectAtKeepsComponent(t *testing.T) {
	w, h := 16, 16
	mask := NewBitmap(w, h)
	for i := 0; i < len(mask.Pix); i += 4 {
		mask.Pix[i], mask.Pix[i+1], mask.Pix[i+2], mask.Pix[i+3] = 0, 0, 0, 255
	}
	// Two blobs: (4,4) and (12,12) 3×3 each.
	for _, c := range [][2]int{{4, 4}, {12, 12}} {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				i := ((c[1]+dy)*w + c[0] + dx) * 4
				mask.Pix[i], mask.Pix[i+3] = 255, 255
			}
		}
	}
	if !LargestSubjectAt(mask, 4, 4) {
		t.Fatal("点选应命中第一个团块")
	}
	if mask.Pix[(12*w+12)*4] != 0 {
		t.Fatal("另一个团块应被清掉")
	}
	if mask.Pix[(4*w+4)*4] == 0 {
		t.Fatal("命中的团块应保留")
	}
	if LargestSubjectAt(mask, 0, 0) {
		t.Fatal("空白处点选应失败")
	}
}
