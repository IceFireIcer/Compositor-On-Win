package render

// Ticket 43 tests: the canvas-size anchor math, the Canvas Extension fill
// layer, the trim rect (all three bases) and ResampleLayer.

import (
	"testing"

	"compositor-win/internal/domain"
)

func TestAnchorOffset(t *testing.T) {
	// 100×100 → 120×140: expanding puts the extra on the right/bottom.
	cases := []struct {
		anchor       Anchor
		newW, newH   int
		oldW, oldH   int
		wantX, wantY float64
	}{
		{4, 120, 140, 100, 100, 10, 20}, // centre
		{0, 120, 140, 100, 100, 0, 0},   // top-left
		{1, 120, 140, 100, 100, 10, 0},  // top
		{2, 120, 140, 100, 100, 20, 0},  // top-right
		{3, 120, 140, 100, 100, 0, 20},  // left
		{5, 120, 140, 100, 100, 20, 20}, // right
		{6, 120, 140, 100, 100, 0, 40},  // bottom-left
		{7, 120, 140, 100, 100, 10, 40}, // bottom
		{8, 120, 140, 100, 100, 20, 40}, // bottom-right
	}
	for _, c := range cases {
		x, y := AnchorOffset(c.newW, c.newH, c.oldW, c.oldH, c.anchor)
		if x != c.wantX || y != c.wantY {
			t.Errorf("anchor %d：得到 (%v,%v)，想要 (%v,%v)", c.anchor, x, y, c.wantX, c.wantY)
		}
	}
	// Shrinking around the centre removes from the left/top (floor).
	x, y := AnchorOffset(80, 80, 100, 100, 4)
	if x != -10 || y != -10 {
		t.Fatalf("缩小居中：得到 (%v,%v)", x, y)
	}
	// Odd difference: floor puts the extra pixel on the right/bottom.
	x, y = AnchorOffset(101, 101, 100, 100, 4)
	if x != 0 || y != 0 {
		t.Fatalf("奇数差居中：得到 (%v,%v)", x, y)
	}
}

func TestCanvasExtensionFill(t *testing.T) {
	b := CanvasExtension(120, 120, 100, 100, 10, 10, 255, 0, 0)
	if b.W != 120 || b.H != 120 {
		t.Fatalf("尺寸 %dx%d", b.W, b.H)
	}
	// The margin is the fill colour, opaque.
	corner := b.Pix[0:4]
	if corner[0] != 255 || corner[1] != 0 || corner[3] != 255 {
		t.Fatalf("外圈应为红: %v", corner)
	}
	// The old-canvas intersection stays transparent.
	inside := b.Pix[(60*120+60)*4 : (60*120+60)*4+4]
	if inside[3] != 0 {
		t.Fatalf("旧画布交集应透明: %v", inside)
	}
	// The boundary pixel (offset 10) is inside the hole.
	edge := b.Pix[(10*120+10)*4 : (10*120+10)*4+4]
	if edge[3] != 0 {
		t.Fatalf("洞的左上角应透明: %v", edge)
	}
	justOutside := b.Pix[(9*120+9)*4 : (9*120+9)*4+4]
	if justOutside[3] != 255 {
		t.Fatalf("洞外应为填充色: %v", justOutside)
	}
}

func TestTrimTransparent(t *testing.T) {
	b := NewBitmap(10, 10)
	// Opaque 4×3 block at (2,3).
	for y := 3; y < 6; y++ {
		for x := 2; x < 6; x++ {
			i := (y*10 + x) * 4
			b.Pix[i], b.Pix[i+3] = 200, 255
		}
	}
	x, y, w, h, ok := TrimRect(b, TrimOptions{BasedOn: TrimTransparentPixels, Top: true, Bottom: true, Left: true, Right: true})
	if !ok || x != 2 || y != 3 || w != 4 || h != 3 {
		t.Fatalf("透明修剪：得到 (%d,%d %dx%d) ok=%v", x, y, w, h, ok)
	}
	// Edge selection: only trim the right edge.
	_, _, w2, _, ok := TrimRect(b, TrimOptions{BasedOn: TrimTransparentPixels, Right: true})
	if !ok || w2 != 6 {
		t.Fatalf("只修剪右边：宽 %d ok=%v", w2, ok)
	}
	// Fully transparent: nothing remains.
	empty := NewBitmap(4, 4)
	if _, _, _, _, ok := TrimRect(empty, TrimOptions{BasedOn: TrimTransparentPixels, Top: true}); ok {
		t.Fatal("全透明应无可修剪内容")
	}
	// No edges requested.
	if _, _, _, _, ok := TrimRect(b, TrimOptions{BasedOn: TrimTransparentPixels}); ok {
		t.Fatal("未选择边应失败")
	}
}

func TestTrimColor(t *testing.T) {
	b := NewBitmap(8, 8)
	// Solid blue background with an opaque white 2×2 block at (3,3).
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			i := (y*8 + x) * 4
			b.Pix[i], b.Pix[i+2], b.Pix[i+3] = 0, 255, 255
		}
	}
	for y := 3; y < 5; y++ {
		for x := 3; x < 5; x++ {
			i := (y*8 + x) * 4
			b.Pix[i], b.Pix[i+1], b.Pix[i+2] = 255, 255, 255
		}
	}
	x, y, w, h, ok := TrimRect(b, TrimOptions{BasedOn: TrimTopLeftPixelColor, Top: true, Bottom: true, Left: true, Right: true})
	if !ok || x != 3 || y != 3 || w != 2 || h != 2 {
		t.Fatalf("左上取样色修剪：得到 (%d,%d %dx%d) ok=%v", x, y, w, h, ok)
	}
	// Tolerance: 10 counts the near-white block pixels as background too,
	// so nothing trims (all "matches").
	near := NewBitmap(4, 4)
	for i := 0; i < len(near.Pix); i += 4 {
		near.Pix[i], near.Pix[i+1], near.Pix[i+2], near.Pix[i+3] = 100, 100, 100, 255
	}
	near.Pix[0], near.Pix[1], near.Pix[2] = 105, 105, 105
	if _, _, _, _, ok := TrimRect(near, TrimOptions{BasedOn: TrimTopLeftPixelColor, Top: true, Tolerance: 10}); ok {
		t.Fatal("容差内应视为同色（无可修剪内容）")
	}
	// Bottom-right sampling.
	x, y, w, h, ok = TrimRect(b, TrimOptions{BasedOn: TrimBottomRightPixelColor, Top: true, Bottom: true, Left: true, Right: true})
	if !ok || x != 3 || y != 3 || w != 2 || h != 2 {
		t.Fatalf("右下取样色修剪：得到 (%d,%d %dx%d) ok=%v", x, y, w, h, ok)
	}
}

func TestLayerBox(t *testing.T) {
	// Axis-aligned: origin 10,10 size 20×30 at 2× → left 20, top 20, 40×60.
	tr := domain.Transform{Origin: [2]float64{10, 10}, Size: [2]float64{20, 30}, Sampling: domain.SamplingSmooth}
	left, top, w, h := LayerBox(tr, 2, 2)
	if left != 20 || top != 20 || w != 40 || h != 60 {
		t.Fatalf("轴对齐包围盒 (%v,%v %dx%d)", left, top, w, h)
	}
	// 90° rotation of a 20×30 box about its centre → 30×20 box.
	rot := domain.Transform{Origin: [2]float64{0, 0}, Size: [2]float64{20, 30}, Rotation: 90, Sampling: domain.SamplingSmooth}
	left, top, w, h = LayerBox(rot, 1, 1)
	// cos(90°) is 6.1e-17 rather than 0, so the ceil() leg can add one row —
	// the original's Int(ceil(max) - floor(min)) has exactly the same
	// half-pixel artifact; the box is 30 wide and 20–21 tall.
	if w != 30 || (h != 20 && h != 21) {
		t.Fatalf("旋转包围盒 %dx%d", w, h)
	}
	if left != -5 || (top != 5 && top != 4) {
		t.Fatalf("旋转包围盒原点 (%v,%v)", left, top)
	}
}

func TestDrawTransformedAxisAligned(t *testing.T) {
	src := NewBitmap(4, 4)
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = 255, 128, 0, 255
	}
	// Layer at document (10,10) 4×4, scaled 2×: the new grid is 8×8 at (20,20).
	tr := domain.Transform{Origin: [2]float64{10, 10}, Size: [2]float64{4, 4}, Sampling: domain.SamplingSmooth}
	left, top, w, h := LayerBox(tr, 2, 2)
	dst := NewBitmap(w, h)
	DrawTransformed(src, tr, 2, 2, dst, left, top, domain.SamplingSmooth)
	mid := dst.Pix[(4*dst.W+4)*4 : (4*dst.W+4)*4+4]
	if mid[0] != 255 || mid[1] != 128 || mid[3] != 255 {
		t.Fatalf("缩放绘制应保色: %v", mid)
	}
	// Axis-aligned: the grid IS the layer's bounding box, so every pixel is
	// covered (the layer is 4×4 at (10,10), scaled to 8×8).
	covered := 0
	for i := 0; i < w*h; i++ {
		if dst.Pix[i*4+3] > 0 {
			covered++
		}
	}
	if covered != w*h {
		t.Fatalf("轴对齐应完全覆盖：%d/%d", covered, w*h)
	}
}

func TestDrawTransformedRotationBakes(t *testing.T) {
	// A 4×2 source rotated 90° must land in a 2×4 box fully covered.
	src := NewBitmap(4, 2)
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i], src.Pix[i+3] = 200, 255
	}
	tr := domain.Transform{Origin: [2]float64{0, 0}, Size: [2]float64{4, 2}, Rotation: 90, Sampling: domain.SamplingSmooth}
	left, top, w, h := LayerBox(tr, 1, 1)
	// The float artifact can widen the grid by a row/column (see TestLayerBox).
	if w < 2 || w > 3 || h < 3 || h > 4 {
		t.Fatalf("旋转盒 %dx%d", w, h)
	}
	dst := NewBitmap(w, h)
	DrawTransformed(src, tr, 1, 1, dst, left, top, domain.SamplingSmooth)
	covered := 0
	for i := 0; i < w*h; i++ {
		if dst.Pix[i*4+3] > 0 {
			covered++
		}
	}
	// The core 2×4 is fully covered; the artifact row/column beyond it is
	// partially covered (its pixels map outside the rotated rect), exactly
	// as the original's ceil()-pad grid does.
	if covered < 8 {
		t.Fatalf("旋转后核心区应完全覆盖：%d/%d", covered, w*h)
	}
}

func TestDrawMaskTransformed(t *testing.T) {
	// 2×1 mask: left bright, right dark. Nearest sampling at 1× keeps them.
	gray := []uint8{255, 0}
	tr := domain.Transform{Origin: [2]float64{0, 0}, Size: [2]float64{2, 1}, Sampling: domain.SamplingNearest}
	out := DrawMaskTransformed(gray, 2, 1, tr, 1, 1, 2, 1, 0, 0, domain.SamplingNearest)
	if out[0] != 255 || out[1] != 0 {
		t.Fatalf("最近邻掩码映射: %v", out)
	}
	// Bilinear across the same pair lands in between.
	mid := DrawMaskTransformed(gray, 2, 1, tr, 1, 1, 2, 1, 0, 0, domain.SamplingSmooth)
	if mid[0] < 100 || mid[0] > 155 {
		t.Fatalf("双线性掩码映射: %v", mid)
	}
	// The grid beyond the layer box stays zero (trimmed coverage).
	out2 := DrawMaskTransformed(gray, 2, 1, tr, 1, 1, 4, 1, 0, 0, domain.SamplingNearest)
	if out2[2] != 0 || out2[3] != 0 {
		t.Fatalf("框外应为零: %v", out2)
	}
	// Uniform 1×1 masks are handled by the caller; here the guard returns a
	// zero grid for degenerate input rather than panicking.
	if got := DrawMaskTransformed(nil, 0, 0, tr, 1, 1, 2, 1, 0, 0, domain.SamplingSmooth); len(got) != 2 {
		t.Fatalf("退化输入尺寸 %d", len(got))
	}
}
