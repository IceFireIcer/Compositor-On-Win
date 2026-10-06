package render

import (
	"bytes"
	"math"
	"testing"
	"unsafe"
)

// Numerical parity tests for the brush engine core, mirroring the semantics
// of reference/Swift/CompositorTests/BrushTests.swift and
// BrushIntersectionTests.swift. Expected values are hand-computed from the
// ported formulas (brushFalloff, spacingFraction, walk) with k = 2.5.

// brushPattern fills a bitmap with a nonzero ramp so committed snapshots can
// be told apart from untouched shared tiles.
func brushPattern(w, h int) *Bitmap {
	b := NewBitmap(w, h)
	for i := 0; i < len(b.Pix); i += 4 {
		v := uint8((i/4)%251 + 4)
		b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = v, v, v, 255
	}
	return b
}

// brushSharedTiles counts the tile positions whose slice headers share one
// backing array between two grids (nil counts as shared with nil).
func brushSharedTiles(a, b *TileGrid) int {
	n := 0
	for i := range a.Tiles {
		if unsafe.SliceData(a.Tiles[i]) == unsafe.SliceData(b.Tiles[i]) {
			n++
		}
	}
	return n
}

// brushStroke starts a fresh stroke over grid with the given settings.
func brushStroke(t *testing.T, grid *TileGrid, s BrushSettings) *Stroke {
	t.Helper()
	st, err := NewStroke(grid, s)
	if err != nil {
		t.Fatalf("NewStroke: %v", err)
	}
	return st
}

func TestBrushFalloffMatchesOriginalFormula(t *testing.T) {
	// BrushRaster.falloff (BrushStroke.swift:106-109), k = 2.5.
	const k = 2.5
	want := func(u float64) float64 {
		return math.Max(0, (math.Exp(-k*u*u)-math.Exp(-k))/(1-math.Exp(-k)))
	}
	for _, u := range []float64{0, 0.25, 0.5, 0.75, 1} {
		if got := brushFalloff(u); math.Abs(got-want(u)) > 1e-12 {
			t.Errorf("falloff(%v) = %v, want %v", u, got, want(u))
		}
	}
	// About half strength at u = 0.5, nothing at the rim (BrushTests.swift:149-153).
	if f := brushFalloff(0.5); f < 0.48 || f > 0.51 {
		t.Errorf("falloff(0.5) = %v, want ~0.494", f)
	}
	if f := brushFalloff(1); f != 0 {
		t.Errorf("falloff(1) = %v, want 0", f)
	}
	for u := 0.0; u <= 1; u += 0.05 { // monotonic decay across the band
		if brushFalloff(u) < brushFalloff(math.Min(1, u+0.05))-1e-12 {
			t.Fatalf("falloff not decreasing at u=%v", u)
		}
	}
	// Hardness regions of tipCoverage: plateau inside radius·hardness, band to
	// the rim, nothing past it (BrushStroke.tip, BrushStroke.swift:251-268).
	if c := tipCoverage(0.5, 20, 10); c != 1 {
		t.Errorf("tipCoverage(0.5, d=inner) = %v, want 1", c)
	}
	if c := tipCoverage(0.5, 20, 15); math.Abs(c-0.493672) > 1e-4 {
		t.Errorf("tipCoverage(0.5, mid-band) = %v, want ~0.4937", c)
	}
	if c := tipCoverage(0.5, 20, 20); c != 0 {
		t.Errorf("tipCoverage(0.5, rim) = %v, want 0", c)
	}
	if c := tipCoverage(1, 20, 19); c != 1 {
		t.Errorf("hard tip inside radius = %v, want 1", c)
	}
	if c := tipCoverage(1, 20, 20); c <= 0 || c >= 1 {
		t.Errorf("hard tip rim = %v, want inside (0,1)", c)
	}
	if c := tipCoverage(1, 20, 21); c != 0 {
		t.Errorf("hard tip past rim = %v, want 0", c)
	}
}

func TestBrushDabRadialProfileByHardness(t *testing.T) {
	// BrushTests.softBrushProducesPartialAlphaAndCancelPreservesDocument's
	// profile expectations (BrushTests.swift:140-159), one dab per hardness on
	// an empty 80×80 canvas, diameter 40 (radius 20), centered on the pixel
	// (39,39) so distances are exact.
	for _, tc := range []struct {
		hardness float64
		x        int
		wantMin  int // inclusive alpha bounds at (x, 39)
		wantMax  int
		note     string
	}{
		// Hardness 0: Gaussian across the whole radius — full at center,
		// ~falloff(0.5)·255 = 126 at half radius, ~falloff(0.85)·255 = 23
		// near the rim, nothing beyond it.
		{0, 39, 255, 255, "center"},
		{0, 49, 124, 128, "half radius"},
		{0, 56, 21, 25, "near rim"},
		{0, 63, 0, 0, "past rim"},
		// Hardness 0.5: plateau out to radius·0.5 = 10, then the band.
		{0.5, 39, 255, 255, "center"},
		{0.5, 44, 255, 255, "plateau"},
		{0.5, 49, 255, 255, "inner edge"},
		{0.5, 58, 12, 16, "band at u=0.9"},
		{0.5, 59, 0, 0, "rim"},
		// Hardness 1: solid inside, antialiased rim, gone past it.
		{1, 39, 255, 255, "center"},
		{1, 49, 255, 255, "inside"},
		{1, 58, 255, 255, "just inside"},
		{1, 59, 1, 254, "rim ramp"},
		{1, 61, 0, 0, "past rim"},
	} {
		grid := NewTileGrid(80, 80)
		st := brushStroke(t, grid, BrushSettings{Diameter: 40, Hardness: tc.hardness, R: 1, Opacity: 1})
		st.Append(Point{X: 39.5, Y: 39.5})
		st.Finish()
		snap := st.Commit()
		r, g, b, a := snap.Pixel(tc.x, 39)
		if int(a) < tc.wantMin || int(a) > tc.wantMax {
			t.Errorf("hardness %v %s: alpha(%d,39) = %d, want [%d,%d]",
				tc.hardness, tc.note, tc.x, a, tc.wantMin, tc.wantMax)
		}
		if tc.wantMin == 255 && (r != 255 || g != 0 || b != 0) {
			t.Errorf("hardness %v %s: pixel = (%d,%d,%d), want opaque red",
				tc.hardness, tc.note, r, g, b)
		}
	}
}

func TestBrushSpacingLaysDabsByDistance(t *testing.T) {
	// A 25%-of-diameter spacing on a 100px run with diameter 40 lays the
	// leading dab plus one dab every 10px: 11 dabs, endpoints included.
	st := brushStroke(t, NewTileGrid(200, 100),
		BrushSettings{Diameter: 40, Hardness: 1, Spacing: 0.25, Opacity: 1})
	if st.spacing != 10 {
		t.Fatalf("spacing = %v, want 10", st.spacing)
	}
	st.Line(Point{X: 10, Y: 50}, Point{X: 110, Y: 50})
	if len(st.dabs) != 11 {
		t.Fatalf("dab count = %d, want 11", len(st.dabs))
	}
	for i, want := range []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110} {
		if st.dabs[i] != (Point{X: want, Y: 50}) {
			t.Errorf("dab %d = %v, want (%v,50)", i, st.dabs[i], want)
		}
	}
	// Derived spacing (BrushStroke.swift:462): 1.5% hard, 2.5% soft, floored
	// at 0.25px (BrushStroke.swift:466).
	if got := spacingFraction(1); got != 0.015 {
		t.Errorf("spacingFraction(1) = %v, want 0.015", got)
	}
	if got := spacingFraction(0.999); got != 0.025 {
		t.Errorf("spacingFraction(0.999) = %v, want 0.025", got)
	}
	st2 := brushStroke(t, NewTileGrid(100, 100), BrushSettings{Diameter: 40, Hardness: 1, Opacity: 1})
	if st2.spacing != 0.6 {
		t.Errorf("derived hard spacing = %v, want 0.6", st2.spacing)
	}
	st3 := brushStroke(t, NewTileGrid(100, 100), BrushSettings{Diameter: 10, Hardness: 1, Opacity: 1})
	if st3.spacing != 0.25 {
		t.Errorf("floored spacing = %v, want 0.25", st3.spacing)
	}
}

func TestBrushHardStrokeCrossesTilesSeamlessly(t *testing.T) {
	// BrushTests.continuousStrokeCrossesTilesAndCommitsOneUndo's pixel probes
	// (BrushTests.swift:119-139): a 560px stroke crosses all three tiles of a
	// 600px canvas and reads as one solid line through both seams.
	grid := NewTileGrid(600, 80)
	st := brushStroke(t, grid, BrushSettings{Diameter: 20, Hardness: 1, R: 1, Opacity: 1})
	st.Append(Point{X: 20, Y: 40})
	st.Append(Point{X: 580, Y: 40})
	st.Finish()
	snap := st.Commit()
	touched := 0
	for _, tile := range snap.Tiles {
		if tile != nil {
			touched++
		}
	}
	if touched != 3 {
		t.Errorf("touched tiles = %d, want 3", touched)
	}
	for _, x := range []int{20, 255, 256, 257, 511, 512, 579} {
		if p := px(snap.Bitmap(), x, 40); p != [4]uint8{255, 0, 0, 255} {
			t.Errorf("pixel(%d,40) = %v, want opaque red across the seam", x, p)
		}
	}
	if a := px(snap.Bitmap(), 300, 0)[3]; a != 0 {
		t.Errorf("pixel(300,0) alpha = %d, want 0", a)
	}
}

func TestBrushOpacityCapsTheWholeStroke(t *testing.T) {
	// BrushTests.opacityCapsTheWholeStrokeEvenWhereItOverlapsItself
	// (BrushTests.swift:306-319): back and forth over the same pixels many
	// times; overlapping dabs never exceed the brush opacity.
	grid := NewTileGrid(200, 80)
	st := brushStroke(t, grid, BrushSettings{Diameter: 20, Hardness: 1, R: 1, Opacity: 0.5})
	st.Append(Point{X: 20, Y: 40})
	for _, x := range []float64{180, 20, 180, 20, 100} {
		st.Append(Point{X: x, Y: 40})
	}
	st.Finish()
	snap := st.Commit()
	p := px(snap.Bitmap(), 100, 40)
	if math.Abs(float64(p[3])-128) > 1 || math.Abs(float64(p[0])-128) > 1 || p[1] != 0 {
		t.Errorf("pixel(100,40) = %v, want [~128,0,0,~128] (opacity capped at 0.5)", p)
	}
	if a := px(snap.Bitmap(), 100, 0)[3]; a != 0 {
		t.Errorf("pixel(100,0) alpha = %d, want 0", a)
	}
}

func TestBrushSoftStrokeAccumulatesCoverageKeepingFeather(t *testing.T) {
	// BrushTests.softStrokeBuildsCoverageWhileKeepingItsFeatheredRim
	// (BrushTests.swift:320-333): a soft dab alone is ~falloff(0.5) at half
	// radius; stroking through the same pixel accumulates coverage toward the
	// cap without ever exceeding it.
	grid := NewTileGrid(200, 80)
	s := BrushSettings{Diameter: 40, Hardness: 0, R: 1, Opacity: 1}
	single := brushStroke(t, grid, s)
	single.Append(Point{X: 99.5, Y: 39.5})
	single.Finish()
	singleA := px(single.Commit().Bitmap(), 99, 49)[3]
	if singleA < 124 || singleA > 128 {
		t.Fatalf("single dab alpha = %d, want ~126", singleA)
	}
	stroke := brushStroke(t, grid, s)
	stroke.Append(Point{X: 20, Y: 39.5})
	stroke.Append(Point{X: 180, Y: 39.5})
	stroke.Finish()
	strokeA := px(stroke.Commit().Bitmap(), 99, 49)[3]
	if strokeA <= singleA+60 {
		t.Errorf("stroke alpha = %d, want > single %d + 60", strokeA, singleA)
	}
	if strokeA > 255 {
		t.Errorf("stroke alpha = %d, want <= 255", strokeA)
	}
}

func TestBrushSelfCrossingScreensLikeTwoSeparateStrokes(t *testing.T) {
	// BrushIntersectionTests.selfCrossingsBlendInsteadOfTakingTheStrongestEdge
	// (BrushIntersectionTests.swift:37-53): where a stroke crosses itself the
	// coverage screens — 1−(1−Cv)(1−Ch) — instead of taking the strongest
	// edge. In alpha space at opacity 1 that is exactly 255−(255−a)(255−b)/255.
	mk := func(path []Point) *TileGrid {
		grid := NewTileGrid(800, 800)
		st := brushStroke(t, grid, BrushSettings{Diameter: 120, Hardness: 0, R: 1, G: 1, B: 1, Opacity: 1})
		st.Append(path[0])
		for _, p := range path[1:] {
			st.Append(p)
		}
		st.Finish()
		return st.Commit()
	}
	v := mk([]Point{{X: 400, Y: 100}, {X: 400, Y: 700}})
	h := mk([]Point{{X: 700, Y: 400}, {X: 100, Y: 400}})
	c := mk([]Point{{X: 400, Y: 100}, {X: 400, Y: 700}, {X: 700, Y: 700},
		{X: 700, Y: 400}, {X: 100, Y: 400}})
	vb, hb, cb := v.Bitmap(), h.Bitmap(), c.Bitmap()
	for _, off := range []int{45, 48, 51} {
		a := px(vb, 400+off, 400+off)[3]
		b := px(hb, 400+off, 400+off)[3]
		actual := px(cb, 400+off, 400+off)[3]
		expected := 255 - (255-int(a))*(255-int(b))/255
		if int(actual) <= int(max(a, b))+10 {
			t.Errorf("offset %d: crossing alpha %d, want > max(%d,%d)+10", off, actual, a, b)
		}
		if math.Abs(float64(int(actual)-expected)) > 4 {
			t.Errorf("offset %d: crossing alpha %d, want ~%d (screen of %d,%d)", off, actual, expected, a, b)
		}
	}
}

func TestBrushAccumulationDependsOnDistanceNotEventCount(t *testing.T) {
	// BrushIntersectionTests.accumulationDependsOnDistanceNotEventCount
	// (BrushIntersectionTests.swift:55-67): the walk carries its dab phase
	// across pointer events, so 2 events and 137 events rasterize the same.
	for _, diameter := range []float64{12, 120, 520} {
		paint := func(step float64) *Bitmap {
			grid := NewTileGrid(800, 800)
			st := brushStroke(t, grid, BrushSettings{Diameter: diameter, Hardness: 0, R: 1, G: 1, B: 1, Opacity: 1})
			st.Append(Point{X: 60, Y: 400})
			const length = 680.0
			count := int(math.Ceil(length / step))
			for i := 1; i <= count; i++ {
				tt := float64(i) / float64(count)
				st.Append(Point{X: 60 + length*tt, Y: 400})
			}
			st.Finish()
			return st.Commit().Bitmap()
		}
		sparse, dense := paint(1000), paint(5)
		for y := 400; y < min(800, 400+int(diameter/2)); y++ {
			a := px(sparse, 400, y)[3]
			b := px(dense, 400, y)[3]
			if math.Abs(float64(int(a)-int(b))) > 2 {
				t.Errorf("diameter %v, row %d: sparse %d vs dense %d", diameter, y, a, b)
			}
		}
	}
}

func TestBrushEraseDecrementsAlphaWithTheSamePipeline(t *testing.T) {
	// Erasing runs the same coverage pipeline with destination-out semantics
	// (BrushStroke.swift:534-539): alpha drops by coverage × opacity, capped
	// within a stroke, compounding across strokes.
	base := solid(200, 80, 255, 0, 0, 255)

	// Full-opacity erase wipes to transparent.
	st1 := brushStroke(t, TileGridFromBitmap(base), BrushSettings{Diameter: 20, Hardness: 1, Opacity: 1, Erase: true})
	st1.Line(Point{X: 20, Y: 40}, Point{X: 180, Y: 40})
	st1.Finish()
	snap1 := st1.Commit()
	if p := px(snap1.Bitmap(), 100, 40); p != [4]uint8{} {
		t.Errorf("full erase: pixel(100,40) = %v, want transparent", p)
	}
	if p := px(snap1.Bitmap(), 100, 10); p != [4]uint8{255, 0, 0, 255} {
		t.Errorf("full erase: pixel(100,10) = %v, want untouched opaque red", p)
	}

	// Half opacity: one pass halves the alpha; repeated dabs within the same
	// stroke cannot go past the cap.
	st2 := brushStroke(t, TileGridFromBitmap(base), BrushSettings{Diameter: 20, Hardness: 1, Opacity: 0.5, Erase: true})
	st2.Line(Point{X: 20, Y: 40}, Point{X: 180, Y: 40})
	st2.Append(Point{X: 20, Y: 40})
	st2.Append(Point{X: 180, Y: 40})
	st2.Finish()
	snap2 := st2.Commit()
	if p := px(snap2.Bitmap(), 100, 40); p != [4]uint8{128, 0, 0, 128} {
		t.Errorf("half erase: pixel(100,40) = %v, want [128,0,0,128]", p)
	}

	// A second stroke compounds: 128 → 64.
	st3 := brushStroke(t, snap2, BrushSettings{Diameter: 20, Hardness: 1, Opacity: 0.5, Erase: true})
	st3.Line(Point{X: 20, Y: 40}, Point{X: 180, Y: 40})
	st3.Finish()
	if p := px(st3.Commit().Bitmap(), 100, 40); p != [4]uint8{64, 0, 0, 64} {
		t.Errorf("second half erase: pixel(100,40) = %v, want [64,0,0,64]", p)
	}
}

func TestBrushCommitSharesUntouchedTiles(t *testing.T) {
	// BrushStroke.swift:119-121: snapshots copy at most the touched 256px
	// tiles. One dab touches one tile; the other len-1 tiles must share the
	// base grid's backing arrays.
	base := TileGridFromBitmap(brushPattern(600, 80))
	st := brushStroke(t, base, BrushSettings{Diameter: 20, Hardness: 1, R: 1, Opacity: 1})
	st.Append(Point{X: 40, Y: 40})
	st.Finish()
	snap := st.Commit()
	if got := brushSharedTiles(snap, base); got != 2 {
		t.Errorf("shared tiles = %d, want 2 (len-1)", got)
	}
	if unsafe.SliceData(snap.Tiles[0]) == unsafe.SliceData(base.Tiles[0]) {
		t.Error("touched tile 0 must not share the base slice")
	}
	if p := px(snap.Bitmap(), 40, 40); p != [4]uint8{255, 0, 0, 255} {
		t.Errorf("pixel(40,40) = %v, want opaque red", p)
	}

	// The same holds for an all-transparent base: untouched tiles stay nil.
	blank := NewTileGrid(600, 80)
	st2 := brushStroke(t, blank, BrushSettings{Diameter: 20, Hardness: 1, R: 1, Opacity: 1})
	st2.Append(Point{X: 40, Y: 40})
	st2.Finish()
	snap2 := st2.Commit()
	if got := brushSharedTiles(snap2, blank); got != 2 {
		t.Errorf("blank base: shared tiles = %d, want 2", got)
	}
	if snap2.Tiles[0] == nil {
		t.Error("blank base: touched tile must be allocated")
	}
}

func TestBrushCommitMatchesPreviewAndUndoRestores(t *testing.T) {
	// BrushTests semantics (BrushTests.swift:126-137): the live preview and
	// the committed snapshot agree bit for bit; undo (the base grid) restores
	// the pre-stroke pixels; redo replays the committed snapshot.
	base := TileGridFromBitmap(brushPattern(600, 80))
	before := base.Bitmap().Pix
	st := brushStroke(t, base, BrushSettings{Diameter: 20, Hardness: 1, R: 1, Opacity: 1})
	st.Append(Point{X: 20, Y: 40})
	st.Append(Point{X: 580, Y: 40})
	st.Finish()
	preview := st.View().Bitmap()
	committed := st.Commit()
	if !bytes.Equal(preview.Pix, committed.Bitmap().Pix) {
		t.Error("committed snapshot differs from the preview")
	}
	if !bytes.Equal(base.Bitmap().Pix, before) {
		t.Error("the stroke must never mutate its base grid (undo state)")
	}
	// Undo = the base grid; redo = the committed grid.
	if !bytes.Equal(base.Bitmap().Pix, before) {
		t.Error("undo (base grid) no longer matches the pre-stroke state")
	}
	for _, x := range []int{20, 300, 579} {
		if p := px(committed.Bitmap(), x, 40); p != [4]uint8{255, 0, 0, 255} {
			t.Errorf("redo: pixel(%d,40) = %v, want opaque red", x, p)
		}
	}
}

func TestBrushSmoothingTrailsPointerOnAString(t *testing.T) {
	// EditorSession.smoothed (EditorSession+Brush.swift:103-113): with the
	// string taut the brush follows; while slack, samples never reach the
	// stroke; on Finish the raw pointer closes the gap.
	grid := NewTileGrid(400, 100)
	st := brushStroke(t, grid, BrushSettings{Diameter: 20, Hardness: 1, R: 1, Opacity: 1, Smoothing: 50})
	st.Append(Point{X: 10, Y: 50})
	if len(st.dabs) != 1 {
		t.Fatalf("first sample must paint once, got %d dabs", len(st.dabs))
	}
	st.Append(Point{X: 30, Y: 50}) // within the string's reach of 50
	if len(st.dabs) != 1 {
		t.Errorf("slack sample produced dabs (%d), want none", len(st.dabs))
	}
	st.Append(Point{X: 200, Y: 50}) // pulls the string taut: anchor ratchets to 150
	if math.Abs(st.anchor.X-150) > 1e-9 || st.anchor.Y != 50 {
		t.Errorf("anchor = %v, want (150,50)", st.anchor)
	}
	if len(st.dabs) <= 1 {
		t.Error("taut pull produced no dabs")
	}
	before := len(st.dabs)
	st.Finish() // the stroke ends where the hand did, at the raw pointer
	if len(st.dabs) <= before {
		t.Error("Finish must close the string's gap to the pointer")
	}
	last := st.dabs[len(st.dabs)-1]
	if last.X <= 199 || last.X > 200.5 {
		t.Errorf("last dab x = %v, want just short of the pointer at 200", last.X)
	}

	// The string's length is in screen points: at zoom 2 a smoothing of 50
	// spans 25 document pixels.
	grid2 := NewTileGrid(400, 100)
	st2 := brushStroke(t, grid2, BrushSettings{Diameter: 20, Hardness: 1, R: 1, Opacity: 1, Smoothing: 50})
	st2.Zoom = 2
	st2.Append(Point{X: 10, Y: 50})
	st2.Append(Point{X: 40, Y: 50}) // 30 > 25: moves to 10 + (30-25) = 15
	if math.Abs(st2.anchor.X-15) > 1e-9 {
		t.Errorf("zoomed anchor = %v, want 15", st2.anchor.X)
	}
}

func TestBrushNewStrokeRejectsInvalidSettings(t *testing.T) {
	// Guards of BrushStroke.init (BrushStroke.swift:223-227).
	grid := NewTileGrid(10, 10)
	for _, s := range []BrushSettings{
		{Diameter: 0, Hardness: 1, Opacity: 1},
		{Diameter: 3000, Hardness: 1, Opacity: 1},
		{Diameter: 40, Hardness: -0.1, Opacity: 1},
		{Diameter: 40, Hardness: 1.5, Opacity: 1},
		{Diameter: 40, Hardness: 1, Opacity: 0.001},
		{Diameter: 40, Hardness: 1, Opacity: 1.5},
		{Diameter: math.NaN(), Hardness: 1, Opacity: 1},
	} {
		if _, err := NewStroke(grid, s); err != ErrInvalidBrushSettings {
			t.Errorf("NewStroke(%+v) error = %v, want ErrInvalidBrushSettings", s, err)
		}
	}
	if _, err := NewStroke(nil, BrushSettings{Diameter: 40, Hardness: 1, Opacity: 1}); err != ErrInvalidBrushSettings {
		t.Errorf("NewStroke(nil) error = %v, want ErrInvalidBrushSettings", err)
	}
}

func TestBrushTileGridSplittingAndRoundTrip(t *testing.T) {
	// Edge tiles clip (BrushStroke.swift:646); nil tiles read transparent;
	// the bitmap round-trip is exact.
	b := brushPattern(300, 20)
	g := TileGridFromBitmap(b)
	if g.Cols != 2 || g.Rows != 1 {
		t.Fatalf("grid = %dx%d tiles, want 2x1", g.Cols, g.Rows)
	}
	if tw, th := g.tileSize(1, 0); tw != 44 || th != 20 {
		t.Errorf("edge tile = %dx%d, want 44x20", tw, th)
	}
	for _, p := range [][2]int{{0, 0}, {255, 19}, {256, 0}, {299, 19}} {
		want := px(b, p[0], p[1])
		if got := px(g.Bitmap(), p[0], p[1]); got != want {
			t.Errorf("pixel(%d,%d) = %v, want %v", p[0], p[1], got, want)
		}
	}
	empty := NewTileGrid(600, 80)
	if tw, _ := empty.tileSize(2, 0); tw != 88 {
		t.Errorf("blank grid edge tile width = %d, want 88", tw)
	}
	if r, g2, b2, a := empty.Pixel(100, 100); r != 0 || g2 != 0 || b2 != 0 || a != 0 {
		t.Errorf("nil tile pixel = (%d,%d,%d,%d), want transparent", r, g2, b2, a)
	}
	if r, _, _, _ := empty.Pixel(-1, 0); r != 0 {
		t.Error("out-of-bounds read must be transparent")
	}
}
