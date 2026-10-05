package render

import (
	"errors"
	"testing"
)

// wandImage builds a width×height RGBA buffer from per-row RGB+alpha specs
// (each row fully uniform): the fixtures the wand tests reason about.
func wandImage(width, height int, rows ...[4]byte) []byte {
	px := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			copy(px[(y*width+x)*4:], rows[y][:])
		}
	}
	return px
}

func TestWandMaskContiguousFill(t *testing.T) {
	// Top half mid-gray, bottom half darker; alpha 255 everywhere.
	rgba := wandImage(6, 8, [4]byte{100, 100, 100, 255}, [4]byte{100, 100, 100, 255},
		[4]byte{100, 100, 100, 255}, [4]byte{100, 100, 100, 255},
		[4]byte{140, 140, 140, 255}, [4]byte{140, 140, 140, 255},
		[4]byte{140, 140, 140, 255}, [4]byte{140, 140, 140, 255})
	mask := make([]byte, 6*8)

	// Seeding the gray region with tolerance 25 fills exactly its 24 pixels.
	if got := WandMask(rgba, 6, 8, 24, 0, 0, 0, 25, true, mask); got != 24 {
		t.Fatalf("contiguous fill selected %d pixels, want 24", got)
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 6; x++ {
			want := byte(255)
			if y >= 4 {
				want = 0
			}
			if mask[y*6+x] != want {
				t.Fatalf("mask(%d,%d) = %d, want %d", x, y, mask[y*6+x], want)
			}
		}
	}

	// A seed whose sampled reference leaves the seed pixel itself outside the
	// tolerance selects nothing (boundary-row seed, 3×3 window → 113).
	if got := WandMask(rgba, 6, 8, 24, 0, 3, 1, 2, true, mask); got != 0 {
		t.Fatalf("non-matching seed selected %d pixels, want 0", got)
	}
	// An out-of-bounds seed selects nothing.
	if got := WandMask(rgba, 6, 8, 24, 99, 0, 0, 25, true, mask); got != 0 {
		t.Fatalf("out-of-bounds seed selected %d pixels, want 0", got)
	}
}

func TestWandMaskNonContiguousMatchesEverywhere(t *testing.T) {
	// Row 0 and row 2 share a color, row 1 differs: non-contiguous picks both
	// rows through the gap, contiguous stops at it.
	rgba := wandImage(4, 3, [4]byte{10, 20, 30, 255}, [4]byte{200, 200, 200, 255},
		[4]byte{10, 20, 30, 255})
	mask := make([]byte, 4*3)
	if got := WandMask(rgba, 4, 3, 16, 0, 0, 0, 0, false, mask); got != 8 {
		t.Fatalf("non-contiguous selected %d pixels, want 8", got)
	}
	for x := 0; x < 4; x++ {
		if mask[x] != 255 || mask[2*4+x] != 255 || mask[4+x] != 0 {
			t.Fatalf("non-contiguous mask wrong at x=%d: %v", x, mask)
		}
	}
	if got := WandMask(rgba, 4, 3, 16, 0, 0, 0, 0, true, mask); got != 4 {
		t.Fatalf("contiguous selected %d pixels, want 4", got)
	}
}

func TestWandMaskMatchesEveryChannelIncludingAlpha(t *testing.T) {
	// Same RGB, alpha differs by 30 (> tolerance 10): must not match.
	rgba := []byte{50, 60, 70, 200, 50, 60, 70, 230}
	mask := make([]byte, 2)
	if got := WandMask(rgba, 2, 1, 8, 0, 0, 0, 10, false, mask); got != 1 {
		t.Fatalf("alpha mismatch selected %d pixels, want 1", got)
	}
	if mask[0] != 255 || mask[1] != 0 {
		t.Fatalf("mask = %v, want [255 0]", mask)
	}
}

func TestWandMaskSamplingRadiusChangesReference(t *testing.T) {
	// Rows 0-3 hold gray 100, rows 4-7 gray 140. A seed on the boundary row 3
	// averages different windows: the 3×3 window (radius 1) lands on 113 —
	// only the light region matches within 25 — while the 5×5 window
	// (radius 2) lands on 116 and both grays match.
	rgba := wandImage(6, 8, [4]byte{100, 100, 100, 255}, [4]byte{100, 100, 100, 255},
		[4]byte{100, 100, 100, 255}, [4]byte{100, 100, 100, 255},
		[4]byte{140, 140, 140, 255}, [4]byte{140, 140, 140, 255},
		[4]byte{140, 140, 140, 255}, [4]byte{140, 140, 140, 255})
	mask := make([]byte, 6*8)

	if got := WandMask(rgba, 6, 8, 24, 0, 3, 1, 25, true, mask); got != 24 {
		t.Fatalf("radius 1 (3×3 sampling) selected %d pixels, want 24", got)
	}
	if got := WandMask(rgba, 6, 8, 24, 0, 3, 2, 25, true, mask); got != 48 {
		t.Fatalf("radius 2 (5×5 sampling) selected %d pixels, want 48", got)
	}
}

func TestColorRangeMask(t *testing.T) {
	// Three columns: pure red, pure green, and a transparent pixel.
	rgba := make([]byte, 3*4)
	copy(rgba[0:], []byte{255, 0, 0, 255})
	copy(rgba[4:], []byte{0, 255, 0, 255})
	copy(rgba[8:], []byte{90, 90, 90, 0}) // transparent, straight color irrelevant
	mask := make([]byte, 3)
	red := []byte{255, 0, 0}

	// Include red: only the opaque red pixel matches.
	if got := ColorRangeMask(rgba, 3, 1, 12, red, nil, 0, false, mask); got != 1 || mask[0] != 255 || mask[1] != 0 || mask[2] != 0 {
		t.Fatalf("include: got %d %v, want 1 [255 0 0]", got, mask)
	}
	// Exclude red inside the fuzziness window: nothing matches.
	if got := ColorRangeMask(rgba, 3, 1, 12, red, []byte{250, 0, 0}, 10, false, mask); got != 0 {
		t.Fatalf("exclude: selected %d pixels, want 0", got)
	}
	// Fuzziness reaches the darker red but not green.
	if got := ColorRangeMask(rgba, 3, 1, 12, []byte{240, 10, 0}, nil, 20, false, mask); got != 1 || mask[0] != 255 {
		t.Fatalf("fuzziness: got %d %v, want 1 [255 …]", got, mask)
	}
	// Transparent never matches, even inside fuzziness of the include color.
	if got := ColorRangeMask(rgba, 3, 1, 12, []byte{90, 90, 90}, nil, 5, false, mask); got != 0 {
		t.Fatalf("transparent matched: %d, want 0", got)
	}
	// Invert flips everything, transparent pixels included (as in the C kernel).
	if got := ColorRangeMask(rgba, 3, 1, 12, red, nil, 0, true, mask); got != 2 || mask[0] != 0 || mask[1] != 255 || mask[2] != 255 {
		t.Fatalf("invert: got %d %v, want 2 [0 255 255]", got, mask)
	}
}

func TestColorRangeMaskUnpremultiplies(t *testing.T) {
	// Half-alpha red straightens to 255,0,0: ((128*255 + 127) / 255) = 128 per
	// channel pre-straightening… the kernel recovers (r*255 + a/2)/a per
	// channel, so premultiplied (128,0,0,128) reads as straight red.
	rgba := make([]byte, 8)
	copy(rgba, []byte{128, 0, 0, 128})
	mask := make([]byte, 1)
	if got := ColorRangeMask(rgba, 1, 1, 4, []byte{255, 0, 0}, nil, 0, false, mask); got != 1 {
		t.Fatalf("half-alpha red not recognized: selected %d, want 1", got)
	}
}

// shoelace returns twice the signed area of a loop in screen coordinates
// (y down): positive = clockwise, negative = counterclockwise.
func shoelace(points []int32) int {
	sum := 0
	n := len(points) / 2
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		sum += int(points[i*2])*int(points[j*2+1]) - int(points[j*2])*int(points[i*2+1])
	}
	return sum
}

func TestWandTraceSquareIsOneClockwiseLoop(t *testing.T) {
	// A 3×3 block of selected pixels inside a 5×5 mask.
	mask := make([]byte, 25)
	for y := 1; y <= 3; y++ {
		for x := 1; x <= 3; x++ {
			mask[y*5+x] = 255
		}
	}
	points, loops, err := WandTrace(mask, 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(loops) != 1 || loops[0] != 4 {
		t.Fatalf("loops = %v, want [4]", loops)
	}
	got := points[:2*int(loops[0])]
	want := []int32{1, 1, 4, 1, 4, 4, 1, 4}
	if len(got) != len(want) {
		t.Fatalf("points = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("points = %v, want %v", got, want)
		}
	}
	if shoelace(got) <= 0 {
		t.Fatalf("outer boundary must run clockwise, shoelace %d", shoelace(got))
	}
	// No stray points after the single loop.
	if len(points) != 8 {
		t.Fatalf("points has %d coordinates, want 8", len(points))
	}
}

func TestWandTraceHoleRunsCounterclockwise(t *testing.T) {
	// 5×5 fully selected except a single-pixel hole at (2,2): one outer loop
	// clockwise plus one hole loop counterclockwise, sharing no vertices.
	mask := make([]byte, 25)
	for i := range mask {
		mask[i] = 255
	}
	mask[2*5+2] = 0
	points, loops, err := WandTrace(mask, 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(loops) != 2 || loops[0] != 4 || loops[1] != 4 {
		t.Fatalf("loops = %v, want [4 4]", loops)
	}
	outer := points[:8]
	wantOuter := []int32{0, 0, 5, 0, 5, 5, 0, 5}
	for i := range wantOuter {
		if outer[i] != wantOuter[i] {
			t.Fatalf("outer loop = %v, want %v", outer, wantOuter)
		}
	}
	hole := points[8:16]
	wantHole := []int32{2, 2, 2, 3, 3, 3, 3, 2}
	for i := range wantHole {
		if hole[i] != wantHole[i] {
			t.Fatalf("hole loop = %v, want %v", hole, wantHole)
		}
	}
	if shoelace(outer) <= 0 || shoelace(hole) >= 0 {
		t.Fatalf("winding wrong: outer %d (want >0), hole %d (want <0)", shoelace(outer), shoelace(hole))
	}
}

func TestWandTracePlusShapeAndDiagonalLoops(t *testing.T) {
	// A plus of five pixels outlines to one 12-corner loop.
	mask := make([]byte, 9)
	mask[1] = 255 // (1,0)
	mask[3] = 255 // (0,1)
	mask[4] = 255 // (1,1)
	mask[5] = 255 // (2,1)
	mask[7] = 255 // (1,2)
	points, loops, err := WandTrace(mask, 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(loops) != 1 || loops[0] != 12 {
		t.Fatalf("plus: loops = %v, want [12]", loops)
	}
	if len(points) != 24 {
		t.Fatalf("plus: %d coordinates, want 24", len(points))
	}

	// Two diagonal pixels share the corner vertex (1,1); turning right keeps
	// their outlines apart: two 4-corner loops.
	diag := make([]byte, 4)
	diag[0] = 255 // (0,0)
	diag[3] = 255 // (1,1)
	points, loops, err = WandTrace(diag, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(loops) != 2 || loops[0] != 4 || loops[1] != 4 {
		t.Fatalf("diagonal: loops = %v, want [4 4]", loops)
	}
	if len(points) != 16 {
		t.Fatalf("diagonal: %d coordinates, want 16", len(points))
	}
}

func TestWandTraceEdgeCases(t *testing.T) {
	if points, loops, err := WandTrace(nil, 0, 0); err != nil || points != nil || loops != nil {
		t.Fatalf("empty mask: %v %v %v", points, loops, err)
	}
	if points, loops, err := WandTrace(make([]byte, 6), 3, 2); err != nil || len(points) != 0 || len(loops) != 0 {
		t.Fatalf("all-zero mask: %v %v %v", points, loops, err)
	}

	// A checkerboard with more than wandEdgeLimit pixel edges is refused
	// (2001² selects 2002001 pixels × 4 edges = 8008004 > 8000000).
	const n = 2001
	mask := make([]byte, n*n)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if (x+y)%2 == 0 {
				mask[y*n+x] = 255
			}
		}
	}
	_, _, err := WandTrace(mask, n, n)
	if !errors.Is(err, ErrOutlineTooDetailed) {
		t.Fatalf("dense outline err = %v, want ErrOutlineTooDetailed", err)
	}
}
