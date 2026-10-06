package render

import (
	"bytes"
	"math"
	"testing"
)

// stampColors rebuilds CloneStampTests' fixture: an opaque 80×40 layer, left
// half red, right half blue, with a green square at x 10–19, y 15–24.
func stampColors() *Bitmap {
	bm := NewBitmap(80, 40)
	stampFillRect(bm, 0, 0, 40, 40, 255, 0, 0)
	stampFillRect(bm, 40, 0, 40, 40, 0, 0, 255)
	stampFillRect(bm, 10, 15, 10, 10, 0, 255, 0)
	return bm
}

func stampFillRect(bm *Bitmap, x, y, w, h int, r, g, b uint8) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			i := (yy*bm.W + xx) * 4
			bm.Pix[i], bm.Pix[i+1], bm.Pix[i+2], bm.Pix[i+3] = r, g, b, 255
		}
	}
}

func stampWantPx(t *testing.T, bm *Bitmap, x, y int, r, g, b, a uint8) {
	t.Helper()
	if got := px(bm, x, y); got != [4]uint8{r, g, b, a} {
		t.Fatalf("pixel (%d,%d) = %v, want {%d %d %d %d}", x, y, got, r, g, b, a)
	}
}

// stampFalloff restates BrushStroke.swift:104-109 independently of the
// implementation, so the profile test checks the port against the formula
// itself, not against the same code it calls.
func stampFalloff(u float64) float64 {
	const k = 2.5
	return math.Max(0, (math.Exp(-k*u*u)-math.Exp(-k))/(1-math.Exp(-k)))
}

// stampExpectFalloff checks that the pixel landed at falloff weight u (a
// white source over a black opaque destination), within the byte rounding.
func stampExpectFalloff(t *testing.T, bm *Bitmap, x, y int, u float64) {
	t.Helper()
	want := math.Round(255 * stampFalloff(u))
	if got := float64(px(bm, x, y)[0]); math.Abs(got-want) > 1 {
		t.Fatalf("pixel (%d,%d) at u=%.4f = %v, want %.1f (falloff %.4f)", x, y, u, got, want, stampFalloff(u))
	}
}

func TestSetCloneSourceGuardsAndQueries(t *testing.T) {
	var cs CloneStamp
	if _, ok := cs.Source(); ok {
		t.Fatal("a fresh CloneStamp reports a source")
	}
	cs.SetCloneSource(Point{X: 15, Y: 20})
	if s, ok := cs.Source(); !ok || s != (Point{X: 15, Y: 20}) {
		t.Fatalf("Source() = (%v, %v), want (15,20) true", s, ok)
	}
	// Non-finite points are ignored (CloneStamp.swift:15's guard).
	cs.SetCloneSource(Point{X: math.NaN(), Y: 20})
	cs.SetCloneSource(Point{X: 15, Y: math.Inf(1)})
	if s, ok := cs.Source(); !ok || s != (Point{X: 15, Y: 20}) {
		t.Fatalf("Source() after non-finite calls = (%v, %v), want (15,20) true", s, ok)
	}
}

func TestStampTickAlignedOffsetFixedFromFirstTick(t *testing.T) {
	// CloneStampTests.copiesTheSourceUnderTheBrushKeepingAlignmentUntilItIsTurnedOff:
	// source (15,20), first stroke at (60,20) puts the green source under the
	// brush; the aligned next stroke keeps the offset (−45,0), so (66,20)
	// copies red from (21,20).
	src := stampColors()
	dst := stampColors()
	cs := &CloneStamp{Hardness: 1} // the test's stroke: diameter 6 → radius 3
	cs.SetCloneSource(Point{X: 15, Y: 20})

	if err := cs.StampTick(dst, nil, src, Point{X: 15, Y: 20}, Point{X: 60, Y: 20}, 3, true); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, dst, 60, 20, 0, 255, 0, 255) // the green source, under the brush
	stampWantPx(t, dst, 70, 5, 0, 0, 255, 255)  // untouched elsewhere

	if err := cs.StampTick(dst, nil, src, Point{X: 15, Y: 20}, Point{X: 66, Y: 20}, 3, true); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, dst, 66, 20, 255, 0, 0, 255)

	// The offset rounds to whole pixels once, at the anchor tick
	// (CloneStamp.swift:26's .rounded()): source (15.4, 20.6) copies from
	// (15, 21) — and the rounded offset stays fixed for the next tick.
	src2 := stampColors()
	dst2 := stampColors()
	cs2 := &CloneStamp{Hardness: 1}
	cs2.SetCloneSource(Point{X: 15.4, Y: 20.6})
	if err := cs2.StampTick(dst2, nil, src2, Point{X: 15.4, Y: 20.6}, Point{X: 60, Y: 20}, 3, true); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, dst2, 60, 20, 0, 255, 0, 255) // src (15,21), inside the green square
	if err := cs2.StampTick(dst2, nil, src2, Point{X: 15.4, Y: 20.6}, Point{X: 66, Y: 20}, 3, true); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, dst2, 66, 20, 255, 0, 0, 255) // src (21,21), red
}

func TestStampTickUnalignedSamplesAtTheSource(t *testing.T) {
	// CloneStampTests' third stroke: not aligned, every stroke starts at the
	// source again. At tick level srcPoint IS the sample center, so (66,20)
	// copies the green source pixel where an aligned tick would have copied
	// red from (21,20) — the aligned/unaligned offset difference.
	src := stampColors()
	dst := stampColors()
	cs := &CloneStamp{Hardness: 1}
	cs.SetCloneSource(Point{X: 15, Y: 20})

	if err := cs.StampTick(dst, nil, src, Point{X: 15, Y: 20}, Point{X: 66, Y: 20}, 3, false); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, dst, 66, 20, 0, 255, 0, 255)

	// (50,10) runs from the brush to the source: offset (−35, +10).
	if err := cs.StampTick(dst, nil, src, Point{X: 15, Y: 20}, Point{X: 50, Y: 10}, 3, false); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, dst, 50, 10, 0, 255, 0, 255)
}

func TestSetCloneSourceStartsANewAlignment(t *testing.T) {
	// "A new source starts a new alignment" (CloneStamp.swift:13): after a
	// second ⌥-click the offset re-fixes at the next tick.
	src := stampColors()
	dst := stampColors()
	cs := &CloneStamp{Hardness: 1}
	cs.SetCloneSource(Point{X: 15, Y: 20})
	if err := cs.StampTick(dst, nil, src, Point{X: 15, Y: 20}, Point{X: 60, Y: 20}, 3, true); err != nil {
		t.Fatal(err)
	}

	cs.SetCloneSource(Point{X: 12, Y: 18})
	if err := cs.StampTick(dst, nil, src, Point{X: 12, Y: 18}, Point{X: 66, Y: 20}, 3, true); err != nil {
		t.Fatal(err)
	}
	// The anchor re-takes at (66,20), so the sample is the source pixel itself
	// (12,18), green; the old offset (−45,0) would have copied red from (21,20).
	stampWantPx(t, dst, 66, 20, 0, 255, 0, 255)

	// The new offset (−54,−2) holds for later ticks: (76,20) copies (22,18), red.
	if err := cs.StampTick(dst, nil, src, Point{X: 12, Y: 18}, Point{X: 76, Y: 20}, 3, true); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, dst, 76, 20, 255, 0, 0, 255)

	// An ignored (non-finite) source neither changes the source nor resets the
	// alignment: (86,20) still copies with the (−54,−2) offset from (66,20) —
	// (32,18), red; a reset anchor would have copied green from (12,18).
	cs.SetCloneSource(Point{X: math.NaN(), Y: 5})
	if s, ok := cs.Source(); !ok || s != (Point{X: 12, Y: 18}) {
		t.Fatalf("Source() after a non-finite call = (%v, %v)", s, ok)
	}
	if err := cs.StampTick(dst, nil, src, Point{X: 12, Y: 18}, Point{X: 86, Y: 20}, 3, true); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, dst, 86, 20, 255, 0, 0, 255)
}

func TestStampTickSoftEdgeRadialProfile(t *testing.T) {
	// The clone stamp's own tip is soft by default
	// (CloneStampTests.cloneStampKeepsItsOwnSoftBrushTip: hardness 0), feathered
	// across the whole radius with BrushRaster.falloff: full at the center,
	// about half strength at u = 0.5 (BrushTests.swift:149-153), exactly zero at
	// and past the rim.
	src := solid(64, 64, 255, 255, 255, 255)
	dst := solid(64, 64, 0, 0, 0, 255)
	cs := &CloneStamp{} // zero value: the tool's default soft tip, full strength
	cs.SetCloneSource(Point{X: 31.5, Y: 32.5})

	// A tip center on a pixel center keeps every distance on this row exact.
	if err := cs.StampTick(dst, nil, src, Point{X: 31.5, Y: 32.5}, Point{X: 31.5, Y: 32.5}, 20, true); err != nil {
		t.Fatal(err)
	}

	stampWantPx(t, dst, 31, 32, 255, 255, 255, 255) // u = 0: full strength
	stampExpectFalloff(t, dst, 36, 32, 0.25)
	stampExpectFalloff(t, dst, 41, 32, 0.5) // about half strength
	stampExpectFalloff(t, dst, 46, 32, 0.75)
	stampExpectFalloff(t, dst, 50, 32, 0.95)

	// Monotone decay across the whole radius.
	prev := uint8(255)
	for x := 32; x <= 50; x++ {
		v := px(dst, x, 32)[0]
		if v >= prev {
			t.Fatalf("profile not strictly falling at x=%d: %v after %v", x, v, prev)
		}
		prev = v
	}

	// Zero at the rim and beyond: those pixels are byte-identical to before.
	stampWantPx(t, dst, 51, 32, 0, 0, 0, 255) // u = 1
	stampWantPx(t, dst, 52, 32, 0, 0, 0, 255) // past the rim
	// The destination stays opaque: source-over of an opaque sample.
	stampWantPx(t, dst, 41, 32, px(dst, 41, 32)[0], px(dst, 41, 32)[1], px(dst, 41, 32)[2], 255)
}

func TestStampTickMaskScalesStrength(t *testing.T) {
	src := solid(64, 32, 255, 255, 255, 255)
	dst := solid(64, 32, 0, 0, 0, 255)
	mask := make([]uint8, 64*32)
	for x := 0; x < 64; x++ {
		mask[16*64+x] = 255 // full-strength band through the tip's center
		mask[17*64+x] = 128 // half-strength band
		// row 15 stays 0: the selection cuts the stamp off entirely
	}
	cs := &CloneStamp{}
	cs.SetCloneSource(Point{X: 32, Y: 16.5})

	if err := cs.StampTick(dst, mask, src, Point{X: 32, Y: 16.5}, Point{X: 32, Y: 16.5}, 10, true); err != nil {
		t.Fatal(err)
	}

	// Row 15 sits as near the tip's center as row 16 (d = 0.5 both) yet keeps
	// the untouched black: mask 0 blocks the stamp.
	stampWantPx(t, dst, 32, 15, 0, 0, 0, 255)
	stampExpectFalloff(t, dst, 32, 16, 0.05) // mask 255: the plain tip weight
	// mask 128 halves the coverage: the same falloff at half the weight.
	want := math.Round(255 * stampFalloff(1.0/10) * 0.5)
	if got := float64(px(dst, 32, 17)[0]); math.Abs(got-want) > 1 {
		t.Fatalf("mask-128 pixel = %v, want %.1f", got, want)
	}

	// A wrong-sized mask is an error, not a silently ignored selection: the
	// stamp must not run outside the clip.
	dst2 := solid(64, 32, 0, 0, 0, 255)
	cs2 := &CloneStamp{}
	cs2.SetCloneSource(Point{X: 32, Y: 16.5})
	if err := cs2.StampTick(dst2, make([]uint8, 5), src, Point{X: 32, Y: 16.5}, Point{X: 32, Y: 16.5}, 10, true); err == nil {
		t.Fatal("wrong-sized mask accepted")
	}
	if !bytes.Equal(dst2.Pix, solid(64, 32, 0, 0, 0, 255).Pix) {
		t.Fatal("rejected stamp changed the bitmap")
	}
}

func TestStampTickRejectsInvalidParameters(t *testing.T) {
	src := stampColors()
	good := stampColors()
	radius := func(r float64) error {
		dst := stampColors()
		cs := &CloneStamp{Hardness: 1}
		cs.SetCloneSource(Point{X: 15, Y: 20})
		err := cs.StampTick(dst, nil, src, Point{X: 15, Y: 20}, Point{X: 60, Y: 20}, r, true)
		if err == nil && !bytes.Equal(dst.Pix, good.Pix) {
			t.Fatalf("radius %v accepted and changed the bitmap", r)
		}
		return err
	}
	// Zero radius is rejected, as are negative and non-finite ones.
	for _, r := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if err := radius(r); err == nil {
			t.Fatalf("radius %v accepted", r)
		}
	}

	cs := &CloneStamp{Hardness: 1}
	if err := cs.StampTick(nil, nil, src, Point{}, Point{}, 3, true); err == nil {
		t.Fatal("nil destination accepted")
	}
	if err := cs.StampTick(stampColors(), nil, nil, Point{}, Point{}, 3, true); err == nil {
		t.Fatal("nil source accepted")
	}
	dst := stampColors()
	if err := cs.StampTick(dst, nil, src, Point{X: math.NaN(), Y: 0}, Point{X: 60, Y: 20}, 3, true); err == nil {
		t.Fatal("non-finite source point accepted")
	}
	if err := cs.StampTick(dst, nil, src, Point{X: 15, Y: 20}, Point{X: math.Inf(1), Y: 20}, 3, true); err == nil {
		t.Fatal("non-finite brush point accepted")
	}
	if err := cs.StampTick(dst, nil, src, Point{X: 15, Y: 20}, Point{X: 60, Y: 20}, 3, true); err != nil {
		t.Fatal(err)
	}
	// Valid runs leave no doubt the rejects above never touched the bitmap.
	if !bytes.Equal(dst.Pix, good.Pix) && px(dst, 60, 20) == px(good, 60, 20) {
		t.Fatal("sanity: valid stamp should have painted")
	}
}

func TestStampTickStopsWhereSampleDoesNotReach(t *testing.T) {
	// "Where the sample doesn't reach, there's nothing to paint"
	// (BrushStroke.swift:507-508): pixels whose source position falls outside
	// src keep what they had — no color, no clearing.
	src := solid(4, 4, 0, 255, 0, 255)
	dst := solid(40, 10, 0, 0, 0, 255)
	cs := &CloneStamp{Hardness: 1}
	cs.SetCloneSource(Point{X: 0, Y: 0})

	// Unaligned from source (0,0) to brush (8,5): offset (−8, −5), tip radius 5.
	// x = 12 would sample src x = 4 — past the edge — and stays black, while
	// x = 11 samples src x = 3 and paints.
	if err := cs.StampTick(dst, nil, src, Point{X: 0, Y: 0}, Point{X: 8, Y: 5}, 5, false); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, dst, 12, 5, 0, 0, 0, 255)
	if got := px(dst, 11, 5); got == [4]uint8{0, 0, 0, 255} {
		t.Fatalf("pixel (11,5) = %v, want painted green", got)
	}
	stampWantPx(t, dst, 3, 5, 0, 0, 0, 255) // would sample src x = −5

	// The same at the top and bottom edges, from source (2,0): offset
	// (−18, −5) puts y = 0 at src y = −5 and y = 9 at src y = 4 — both past
	// the 4px source — while (20,5) lands dead on src (2,0).
	if err := cs.StampTick(dst, nil, src, Point{X: 2, Y: 0}, Point{X: 20, Y: 5}, 5, false); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, dst, 20, 0, 0, 0, 0, 255)
	stampWantPx(t, dst, 20, 9, 0, 0, 0, 255)
	stampWantPx(t, dst, 20, 5, 0, 255, 0, 255) // full strength at the center
}

func TestStampTickPremultipliedSourceOver(t *testing.T) {
	// The sample is already premultiplied; source-over at weight a mixes
	// src·a + dst·(1−a) channel-uniformly (BrushStroke.swift:506-525 at
	// setAlpha(opacity)). A half-covered pixel of a half-alpha source stays
	// premultiplied-correct over opaque black and over transparency alike.
	src := solid(8, 4, 128, 64, 32, 128) // premultiplied half-alpha orange
	black := solid(40, 10, 0, 0, 0, 255)
	clear0 := NewBitmap(40, 10)
	cs := &CloneStamp{Hardness: 1}
	cs.SetCloneSource(Point{X: 2, Y: 2.5})

	// Brush at (10.5, 6.5): the pixel (10,6) sits dead center (coverage 1),
	// pixel (12,6) at distance 2 of radius 2 (coverage exactly 0.5). The
	// offset rounds to (−9, −4), so both sample the same source row.
	if err := cs.StampTick(black, nil, src, Point{X: 2, Y: 2.5}, Point{X: 10.5, Y: 6.5}, 2, true); err != nil {
		t.Fatal(err)
	}
	if err := cs.StampTick(clear0, nil, src, Point{X: 2, Y: 2.5}, Point{X: 10.5, Y: 6.5}, 2, true); err != nil {
		t.Fatal(err)
	}

	// Full strength copies the premultiplied sample verbatim — over anything.
	stampWantPx(t, black, 10, 6, 128, 64, 32, 128)
	stampWantPx(t, clear0, 10, 6, 128, 64, 32, 128)
	// Half strength: src·0.5 + dst·0.5, channels and alpha alike.
	stampWantPx(t, black, 12, 6, 64, 32, 16, 192)
	// Over transparency the result is the sample halved — still premultiplied
	// (r, g, b ≤ a), never straight-alpha garbage.
	stampWantPx(t, clear0, 12, 6, 64, 32, 16, 64)
}

func TestFillColorWholeImage(t *testing.T) {
	// fillSelection with no selection fills the whole layer
	// (SelectionEdits.swift:51): foreground and background differ only in the
	// color the caller passes; an opaque fill replaces what was there.
	bm := NewBitmap(8, 8)
	stampFillRect(bm, 0, 0, 4, 8, 255, 0, 0) // left half red, right half transparent
	if err := FillColor(bm, nil, bm.W, bm.H, Color{R: 0, G: 0, B: 1, A: 1}, FillForeground); err != nil {
		t.Fatal(err)
	}
	for _, at := range [][2]int{{0, 0}, {3, 7}, {4, 0}, {7, 7}} {
		stampWantPx(t, bm, at[0], at[1], 0, 0, 255, 255)
	}

	bm2 := NewBitmap(8, 8)
	if err := FillColor(bm2, nil, bm2.W, bm2.H, Color{R: 0, G: 1, B: 0, A: 1}, FillBackground); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, bm2, 5, 5, 0, 255, 0, 255)

	// Components clamp like CGColor's would.
	bm3 := NewBitmap(2, 2)
	if err := FillColor(bm3, nil, bm3.W, bm3.H, Color{R: 2, G: -1, B: 0.5, A: 3}, FillForeground); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, bm3, 1, 1, 255, 0, 128, 255)
}

func TestFillColorRespectsSelectionMask(t *testing.T) {
	// With a selection only mask > 0 pixels change, and the mask value scales
	// the fill — an antialiased selection edge lands proportionally
	// (BrushStroke.paintCanvas's selectionClip).
	bm := solid(16, 8, 100, 100, 100, 255)
	mask := make([]uint8, 16*8)
	for y := 2; y < 6; y++ {
		for x := 8; x < 12; x++ {
			mask[y*16+x] = 255
		}
		mask[y*16+7] = 128 // a half-covered edge column
	}
	if err := FillColor(bm, mask, bm.W, bm.H, Color{R: 1, G: 0, B: 0, A: 1}, FillForeground); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, bm, 9, 3, 255, 0, 0, 255)   // inside: opaque fill replaces
	stampWantPx(t, bm, 7, 3, 178, 50, 50, 255) // mask 128: half the fill over gray 100
	stampWantPx(t, bm, 0, 0, 100, 100, 100, 255)
	stampWantPx(t, bm, 13, 3, 100, 100, 100, 255) // mask 0: untouched

	// An all-zero mask — the empty selection — fills nothing.
	bm2 := solid(4, 4, 100, 100, 100, 255)
	empty := make([]uint8, 16)
	if err := FillColor(bm2, empty, bm2.W, bm2.H, Color{R: 1, G: 0, B: 0, A: 1}, FillForeground); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, bm2, 2, 2, 100, 100, 100, 255)
}

func TestFillColorClearsOnlySelection(t *testing.T) {
	// clearSelectedPixels is destination-out (BrushStroke.swift:714-718):
	// existing pixels scale toward transparent by the coverage; full coverage
	// clears to zero; everything outside the selection stays put.
	bm := solid(16, 8, 100, 100, 100, 255)
	mask := make([]uint8, 16*8)
	for y := 2; y < 6; y++ {
		for x := 8; x < 12; x++ {
			mask[y*16+x] = 255
		}
		mask[y*16+7] = 128
	}
	if err := FillColor(bm, mask, bm.W, bm.H, Color{}, FillClear); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, bm, 9, 3, 0, 0, 0, 0) // cleared to transparent
	// Half-covered: every premultiplied channel scales by 1 − 128/255 —
	// alpha included, which is what destination-out means.
	stampWantPx(t, bm, 7, 3, 50, 50, 50, 127)
	stampWantPx(t, bm, 0, 0, 100, 100, 100, 255)
	stampWantPx(t, bm, 13, 3, 100, 100, 100, 255)

	// Without a mask the whole layer clears.
	bm2 := solid(4, 4, 100, 100, 100, 255)
	if err := FillColor(bm2, nil, bm2.W, bm2.H, Color{}, FillClear); err != nil {
		t.Fatal(err)
	}
	stampWantPx(t, bm2, 2, 2, 0, 0, 0, 0)
	stampWantPx(t, bm2, 0, 0, 0, 0, 0, 0)
}

func TestFillColorRejectsInvalid(t *testing.T) {
	bm := solid(8, 8, 100, 100, 100, 255)
	keep := bm.Clone()
	mismatch := make([]uint8, 5)

	if err := FillColor(nil, nil, 8, 8, Color{R: 1}, FillForeground); err == nil {
		t.Fatal("nil bitmap accepted")
	}
	if err := FillColor(bm, nil, 9, 8, Color{R: 1}, FillForeground); err == nil {
		t.Fatal("width mismatch accepted")
	}
	if err := FillColor(bm, nil, 8, 9, Color{R: 1}, FillForeground); err == nil {
		t.Fatal("height mismatch accepted")
	}
	if err := FillColor(bm, mismatch, bm.W, bm.H, Color{R: 1}, FillForeground); err == nil {
		t.Fatal("wrong-sized mask accepted")
	}
	if err := FillColor(bm, nil, bm.W, bm.H, Color{R: 1}, "colorize"); err == nil {
		t.Fatal("unknown mode accepted")
	}
	if err := FillColor(bm, nil, bm.W, bm.H, Color{R: math.NaN()}, FillForeground); err == nil {
		t.Fatal("non-finite color accepted")
	}
	if !bytes.Equal(bm.Pix, keep.Pix) {
		t.Fatal("a rejected fill changed the bitmap")
	}
}
