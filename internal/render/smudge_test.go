package render

import (
	"math"
	"testing"
)

// warpGradient builds the MetalWarpTests photo fixture in spirit: an opaque
// bitmap whose channels vary on every axis so any resampling shows up.
func warpGradient(w, h int) *Bitmap {
	bm := NewBitmap(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			bm.Pix[i] = uint8((x * 255) / w)
			bm.Pix[i+1] = uint8((y * 255) / h)
			if (x/8+y/8)%2 == 0 {
				bm.Pix[i+2] = 220
			} else {
				bm.Pix[i+2] = 40
			}
			bm.Pix[i+3] = 255
		}
	}
	return bm
}

// warpStripes builds 1px vertical stripes alternating two levels — the
// high-frequency pattern the liquify sharpness probe measures. One bilinear
// resample at a fractional offset mixes adjacent stripes once (values 64/191
// for the 0/255 pattern); resampling at every dab washes them toward flat gray.
func warpStripes(w, h int, even, odd uint8) *Bitmap {
	bm := NewBitmap(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := even
			if x%2 == 1 {
				v = odd
			}
			i := (y*w + x) * 4
			bm.Pix[i], bm.Pix[i+1], bm.Pix[i+2], bm.Pix[i+3] = v, v, v, 255
		}
	}
	return bm
}

// warpDot builds a dark field with one antialiased bright disc, the
// smudgeLeavesOneFadingTrail fixture: a solid fill (as CG's fillEllipse makes)
// with a one-pixel coverage edge, not a gradient cone.
func warpDot(w, h, cx, cy, radius int) *Bitmap {
	bm := NewBitmap(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			dist := math.Sqrt(float64((x-cx)*(x-cx) + (y-cy)*(y-cy)))
			cover := math.Min(1, math.Max(0, float64(radius)+0.5-dist))
			b := uint8(math.Round(26 + (255-26)*cover))
			bm.Pix[i], bm.Pix[i+1], bm.Pix[i+2], bm.Pix[i+3] = b, b, b, 255
		}
	}
	return bm
}

func TestCycleWarpMode(t *testing.T) {
	// BlurToolMode's case order is liquify, blur, smudge (SmudgeLiquify.swift:10-14)
	// and the R key advances through it, wrapping (EditorSession.cycleToolMode,
	// EditorSession.swift:393-404; bound to "r" in EditorCanvas.swift:2240).
	if got := CycleWarpMode(WarpModeLiquify); got != WarpModeBlur {
		t.Fatalf("CycleWarpMode(liquify) = %q, want blur", got)
	}
	if got := CycleWarpMode(WarpModeBlur); got != WarpModeSmudge {
		t.Fatalf("CycleWarpMode(blur) = %q, want smudge", got)
	}
	if got := CycleWarpMode(WarpModeSmudge); got != WarpModeLiquify {
		t.Fatalf("CycleWarpMode(smudge) = %q, want liquify", got)
	}
}

func TestLiquifyDabOffsetsDouble(t *testing.T) {
	// One 2px dab: each pixel inside the hardness plateau takes the whole move
	// as its source offset (warp_push: sampled - move*w, MetalWarp.swift:249).
	// The same dab again samples the field from behind the same travel and
	// subtracts the move once more: displacement doubles. The pixels themselves
	// are never re-resampled — that is what keeps liquify sharp
	// (MetalWarpTests.liquifyStaysSharp's core assertion).
	src := warpGradient(200, 100)
	s := NewWarpStroke(src, WarpModeLiquify, WarpSettings{Diameter: 50, Hardness: 0.3, Strength: 1})

	s.PushDab(98, 50, 100, 50, 25, 1)
	if ox, oy := s.offsetAt(100, 50); ox != -2 || oy != 0 {
		t.Fatalf("after one 2px dab offset = (%v, %v), want (-2, 0)", ox, oy)
	}

	s.PushDab(98, 50, 100, 50, 25, 1)
	if ox, oy := s.offsetAt(100, 50); ox != -4 || oy != 0 {
		t.Fatalf("after the same dab twice offset = (%v, %v), want (-4, 0)", ox, oy)
	}

	// Displacement scales linearly with the dab's pressure, too: two dabs at
	// pressure 0.5 stack to the same 2px a single pressure-1 dab gives.
	p := NewWarpStroke(src, WarpModeLiquify, WarpSettings{Diameter: 50, Hardness: 0.3, Strength: 1})
	p.PushDab(99, 50, 100, 50, 25, 0.5)
	p.PushDab(99, 50, 100, 50, 25, 0.5)
	if ox, oy := p.offsetAt(100, 50); ox != -1 || oy != 0 {
		t.Fatalf("two half-strength dabs give offset = (%v, %v), want (-1, 0)", ox, oy)
	}
}

func TestLiquifyRepeatedDabsStaySharp(t *testing.T) {
	// 19 dabs of a 0.25px move stack the field to -4.75px at the center; the
	// pen-up commit resamples the untouched original once, at a fractional
	// offset, so the 1px stripes keep their single-resample contrast (64/191).
	// Resampling the pixels at every dab — the CPU path the MetalWarp comment
	// describes softening — would run 19 bilinear passes and wash them toward
	// flat gray (~127).
	src := warpStripes(200, 100, 0, 255)
	s := NewWarpStroke(src, WarpModeLiquify, WarpSettings{Diameter: 50, Hardness: 0.98, Strength: 1})
	for i := 0; i < 19; i++ {
		s.PushDab(99.75, 50, 100, 50, 25, 1)
	}
	if ox, oy := s.offsetAt(100, 50); ox != -4.75 || oy != 0 {
		t.Fatalf("19 quarter-pixel dabs give offset = (%v, %v), want (-4.75, 0)", ox, oy)
	}
	out := s.Commit()
	lo, hi := 255, 0
	for x := 90; x <= 110; x++ {
		v := int(out.Pix[(50*out.W+x)*4])
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	if lo > 70 || hi < 185 {
		t.Fatalf("stripe contrast after 19 dabs is [%d, %d], want a single resample's [64, 191]", lo, hi)
	}
}

func TestLiquifyCommitKeepsSizeAndIsPure(t *testing.T) {
	// The pen-up readback returns a new bitmap the size of the canvas, and it
	// is pure: the session stores it as one undo step (the original's
	// finishWarp, SmudgeLiquify.swift:217-246, paints the result into the layer
	// as a single snapshot-backed edit; the Go history takes the bitmap
	// directly, so no per-dab replay is needed).
	src := warpGradient(200, 100)
	s := NewWarpStroke(src, WarpModeLiquify, WarpSettings{Diameter: 50, Hardness: 0.3, Strength: 0.7})
	s.Append(60, 50)
	for x := 64.0; x <= 160; x += 4 {
		s.Append(x, 50)
	}
	first, second := s.Commit(), s.Commit()
	if first.W != src.W || first.H != src.H {
		t.Fatalf("commit is %dx%d, want %dx%d", first.W, first.H, src.W, src.H)
	}
	for i := range first.Pix {
		if first.Pix[i] != second.Pix[i] {
			t.Fatalf("commit is not pure: byte %d differs between calls", i)
		}
	}
	// Pixels the stroke never reached keep the layer as it was.
	for y := 0; y < src.H; y++ {
		for x := 0; x < 20; x++ {
			i := (y*src.W + x) * 4
			if first.Pix[i] != src.Pix[i] || first.Pix[i+3] != src.Pix[i+3] {
				t.Fatalf("untouched pixel (%d,%d) changed", x, y)
			}
		}
	}
}

func TestSmudgeSamePointDabIsIdempotent(t *testing.T) {
	// The brush carries what it just left, and nothing older (WarpStroke.smudge,
	// SmudgeLiquify.swift:146-148): the second dab over the same spot finds
	// under == carried and lays down nothing — no ghost re-stamp of the pick-up.
	src := warpDot(300, 100, 100, 50, 8)
	s := NewWarpStroke(src, WarpModeSmudge, WarpSettings{Diameter: 40, Hardness: 0.5, Strength: 0.6})
	s.Append(100, 50) // the stroke's first point only picks the color up
	s.SmudgeDab(100, 50, 20, 0.6)
	after := s.work.Clone()
	s.SmudgeDab(100, 50, 20, 0.6)
	for i := range after.Pix {
		if after.Pix[i] != s.work.Pix[i] {
			t.Fatalf("repeated same-point smudge dab moved byte %d (%d -> %d)", i, after.Pix[i], s.work.Pix[i])
		}
	}
}

func TestSmudgeLeavesOneFadingTrail(t *testing.T) {
	// A bright dot smudged sideways leaves one trail that fades, not a row of
	// ghost copies of itself (MetalWarpTests.smudgeLeavesOneFadingTrail).
	src := warpDot(300, 100, 60, 50, 8)
	s := NewWarpStroke(src, WarpModeSmudge, WarpSettings{Diameter: 40, Hardness: 0.5, Strength: 0.6})
	for i := 0; i <= 60; i++ {
		s.Append(float64(60+3*i), 50)
	}
	out := s.Commit()
	row := make([]int, 0, 170)
	for x := 70; x < 240; x++ {
		row = append(row, int(out.Pix[(50*out.W+x)*4]))
	}
	// A ghost is a bump: brighter than a little way either side of it.
	peaks := 0
	for i := 2; i < len(row)-2; i++ {
		if row[i] > row[i-2]+2 && row[i] > row[i+2]+2 {
			peaks++
		}
	}
	if peaks != 0 {
		t.Fatalf("the trail repeats itself: %d ghost peaks along %v", peaks, row)
	}
	if row[0] <= row[len(row)-1]+20 {
		t.Fatalf("there is no trail: %d at the dot against %d at the end", row[0], row[len(row)-1])
	}
}

func TestWarpMaskLimitsDabs(t *testing.T) {
	// A selection clip scales every dab's weight by its coverage: pixels outside
	// the selection are untouched, a feathered edge moves about half as far.
	src := warpGradient(200, 100)
	settings := WarpSettings{Diameter: 50, Hardness: 0.3, Strength: 1}

	half := make([]uint8, 200*100)
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			half[y*200+x] = 255
		}
	}
	s := NewWarpStroke(src, WarpModeLiquify, settings)
	s.Mask = half
	s.PushDab(100, 50, 102, 50, 25, 1)
	out := s.Commit()
	for y := 0; y < 100; y++ {
		for x := 100; x < 200; x++ {
			i := (y*200 + x) * 4
			if out.Pix[i] != src.Pix[i] || out.Pix[i+1] != src.Pix[i+1] {
				t.Fatalf("pixel (%d,%d) outside the selection changed", x, y)
			}
		}
	}
	// Inside, the dab moved the gradient two pixels left (the sample two pixels
	// behind is worth ~2.5 levels here).
	if got, want := int(out.Pix[(50*200+98)*4]), int(src.Pix[(50*200+96)*4]); got != want {
		t.Fatalf("inside the selection pixel (98,50) = %d, want the shifted %d", got, want)
	}

	// A half-covered selection moves the same pixel about half as far.
	soft := make([]uint8, 200*100)
	for i := range soft {
		soft[i] = 128
	}
	f := NewWarpStroke(src, WarpModeLiquify, settings)
	f.Mask = soft
	f.PushDab(100, 50, 102, 50, 25, 1)
	fo := f.Commit()
	full := int(src.Pix[(50*200+98)*4]) - int(out.Pix[(50*200+98)*4])
	feather := int(src.Pix[(50*200+98)*4]) - int(fo.Pix[(50*200+98)*4])
	if feather <= 0 || feather >= full {
		t.Fatalf("feathered selection moved pixel (98,50) by %d, want strictly between 0 and the full %d", feather, full)
	}
}

func TestWarpZeroStrengthMovesNothing(t *testing.T) {
	// Pressure 0: no displacement in liquify, no lay-down in smudge or blur —
	// the canvas comes back exactly as it went in.
	src := warpDot(300, 100, 100, 50, 8)
	cases := []struct {
		mode WarpMode
		dab  func(s *WarpStroke)
	}{
		{WarpModeLiquify, func(s *WarpStroke) { s.PushDab(100, 50, 104, 50, 20, 0) }},
		{WarpModeSmudge, func(s *WarpStroke) { s.SmudgeDab(100, 50, 20, 0) }},
		{WarpModeBlur, func(s *WarpStroke) { s.BlurDab(100, 50, 20, 0) }},
	}
	for _, c := range cases {
		s := NewWarpStroke(src, c.mode, WarpSettings{Diameter: 40, Hardness: 0.5, Strength: 0, BlurRadius: 5})
		c.dab(s)
		out := s.Commit()
		for i := range src.Pix {
			if out.Pix[i] != src.Pix[i] {
				t.Fatalf("%s dab at pressure 0 changed byte %d (%d -> %d)", c.mode, i, src.Pix[i], out.Pix[i])
			}
		}
	}
}

func TestBlurDabConvergesOnSnapshot(t *testing.T) {
	// Blur paints the stroke-start softened sample through the tip
	// (BlurTool.blurSample, BlurTool.swift:10-17): repeated dabs over one spot
	// converge on that same snapshot instead of compounding the blur, and only
	// a new stroke — which takes a fresh sample — softens the area further.
	src := warpDot(300, 100, 100, 50, 4)
	s := NewWarpStroke(src, WarpModeBlur, WarpSettings{Diameter: 40, Hardness: 0.5, Strength: 1, BlurRadius: 5})
	probe := (50*300 + 106) * 4 // just outside the dot, inside the blur's spread
	snapshot := s.blur.Pix[probe]
	if snapshot <= 0 || snapshot >= 255 {
		t.Fatalf("blur snapshot at the probe is %d, want softened strictly between 0 and 255", snapshot)
	}
	s.BlurDab(106, 50, 20, 1)
	if got := s.work.Pix[probe]; got != snapshot {
		t.Fatalf("a full-strength dab at the probe laid down %d, want the snapshot's %d", got, snapshot)
	}
	s.BlurDab(106, 50, 20, 1)
	if got := s.work.Pix[probe]; got != snapshot {
		t.Fatalf("repeated blur dabs compound: %d, want the snapshot's %d", got, snapshot)
	}
	// A new stroke samples the softened canvas and softens further.
	next := NewWarpStroke(s.work.Clone(), WarpModeBlur, WarpSettings{Diameter: 40, Hardness: 0.5, Strength: 1, BlurRadius: 5})
	next.BlurDab(106, 50, 20, 1)
	if got := next.work.Pix[probe]; got == snapshot {
		t.Fatalf("a second stroke left the probe at %d, want it further softened", got)
	}
}

func TestWarpAppendSpacingAndPoints(t *testing.T) {
	// Dabs space along the path at max(1, diameter * 0.005) for smudge and
	// max(1, diameter * 0.025) otherwise (SmudgeLiquify.swift:88), each
	// segment divided into ceil(distance/spacing) steps (lines 84-101). The
	// stroke's first point only primes the state (pick-up for smudge), and a
	// shorter-than-spacing move records nothing.
	src := warpGradient(300, 100)

	s := NewWarpStroke(src, WarpModeLiquify, WarpSettings{Diameter: 60, Hardness: 0.3, Strength: 0.7})
	s.Append(10, 50)
	if len(s.Points()) != 0 {
		t.Fatalf("liquify's first append dabbed %d times, want none", len(s.Points()))
	}
	s.Append(20, 50) // 10px at spacing 1.5 -> ceil(10/1.5) = 7 dabs
	if len(s.Points()) != 7 {
		t.Fatalf("liquify laid %d dabs, want 7", len(s.Points()))
	}
	s.Append(20.5, 50) // under spacing: nothing
	if len(s.Points()) != 7 {
		t.Fatalf("sub-spacing move laid %d extra dabs", len(s.Points())-7)
	}

	m := NewWarpStroke(src, WarpModeSmudge, WarpSettings{Diameter: 40, Hardness: 0.5, Strength: 0.6})
	m.Append(10, 50)
	if len(m.carried) == 0 {
		t.Fatal("smudge's first append did not pick the color up")
	}
	m.Append(20, 50) // 10px at spacing 1 -> 10 dabs
	if len(m.Points()) != 10 {
		t.Fatalf("smudge laid %d dabs, want 10", len(m.Points()))
	}

	b := NewWarpStroke(src, WarpModeBlur, WarpSettings{Diameter: 40, Hardness: 0.5, Strength: 1, BlurRadius: 5})
	b.Append(10, 50)
	b.Append(20, 50)
	if len(b.Points()) != 10 {
		t.Fatalf("blur laid %d dabs, want 10", len(b.Points()))
	}
}

// offsetAt reads the liquify offset field at a pixel (tests sit in the package
// to inspect the field directly; sessions only ever see Commit's bitmap).
func (s *WarpStroke) offsetAt(x, y int) (float32, float32) {
	return s.offsets[(y*s.w+x)*2], s.offsets[(y*s.w+x)*2+1]
}
