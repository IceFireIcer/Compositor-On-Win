package render

// SpotHeal tests — the port of HealPixels.c (spot_heal). All three modes are
// deterministic at a fixed seed, and the two behaviors SpotHealingTests.swift
// pins hold: a blemish under the brush heals into the surface while nothing
// outside the brush moves, and Content-Aware carries a curved edge through the
// spot far better than one copied patch does.

import (
	"bytes"
	"testing"
)

// blemishedBitmap is SpotHealingTests.swift's blemished(): vertical gray
// stripes 2 px wide with a 10 x 10 red blemish in the middle.
func blemishedBitmap() *Bitmap {
	const w, h = 120, 80
	b := NewBitmap(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			red := x >= 55 && x < 65 && y >= 35 && y < 45
			gray := uint8(100)
			if x%4 >= 2 {
				gray = 112
			}
			i := (y*w + x) * 4
			if red {
				b.Pix[i], b.Pix[i+1], b.Pix[i+2] = 230, 20, 20
			} else {
				b.Pix[i], b.Pix[i+1], b.Pix[i+2] = gray, gray, gray
			}
			b.Pix[i+3] = 255
		}
	}
	return b
}

// blemishCoverage is the brush's coverage: a 24 px dab at (60, 40), the stroke
// SpotHealingTests.swift draws.
func blemishCoverage(w, h int) []uint8 {
	stroke := NewHealStroke(w, h, HealStrokeSettings{Diameter: 24, Hardness: 1, Zoom: 1})
	stroke.Append(Point{X: 60, Y: 40})
	stroke.Append(Point{X: 60.5, Y: 40})
	stroke.Finish()
	return stroke.Coverage()
}

func TestSpotHealIsDeterministicPerMode(t *testing.T) {
	for _, mode := range []int{0, 1, 2} {
		base := blemishedBitmap()
		coverage := blemishCoverage(base.W, base.H)
		first := base.Clone()
		if !SpotHeal(first, coverage, 1, mode, 99) {
			t.Fatalf("mode %d: SpotHeal reported no coverage", mode)
		}
		for run := 0; run < 2; run++ {
			again := base.Clone()
			if !SpotHeal(again, coverage, 1, mode, 99) {
				t.Fatalf("mode %d: SpotHeal reported no coverage on a repeat run", mode)
			}
			if !bytes.Equal(first.Pix, again.Pix) {
				t.Fatalf("mode %d run %d differs: SpotHeal is not deterministic at a fixed seed", mode, run)
			}
		}
	}
}

func TestSpotHealBlemishUnderTheBrushAndNothingElse(t *testing.T) {
	for _, mode := range []int{0, 1, 2} {
		base := blemishedBitmap()
		before := base.Clone()
		coverage := blemishCoverage(base.W, base.H)
		if !SpotHeal(base, coverage, 1, mode, 7) {
			t.Fatalf("mode %d: SpotHeal reported no coverage", mode)
		}
		for _, p := range [][2]int{{60, 40}, {56, 36}, {64, 44}} {
			i := (p[1]*base.W + p[0]) * 4
			got := base.Pix[i : i+4]
			if int(got[0])-int(got[1]) >= 30 || got[1] < 80 || got[1] > 130 || got[3] != 255 {
				t.Errorf("mode %d: (%d, %d) is %v, not healed into the gray surface", mode, p[0], p[1], got)
			}
		}
		// Away from the brush, nothing moves.
		for _, p := range [][2]int{{10, 10}, {90, 40}, {30, 40}, {60, 10}, {60, 70}} {
			i := (p[1]*base.W + p[0]) * 4
			if !bytes.Equal(base.Pix[i:i+4], before.Pix[i:i+4]) {
				t.Errorf("mode %d: (%d, %d) changed outside the brush: %v -> %v",
					mode, p[0], p[1], before.Pix[i:i+4], base.Pix[i:i+4])
			}
		}
	}
}

// SpotHealingTests.swift: "Content-Aware rebuilds a spot pixel by pixel from
// patches all around it (PatchPixels.c), so the edge of a hill carries on
// through it as a curve. Proximity Match, which copies one nearby patch, can't
// follow it."
func TestSpotHealContentAwareBeatsProximityOnACurvedEdge(t *testing.T) {
	truth := ridgeBitmap(false)
	center := Point{X: 160, Y: ridgeCenterY}
	errors := map[int]float64{}
	for _, mode := range []int{0, 2} {
		damaged := ridgeBitmap(true)
		stroke := NewHealStroke(damaged.W, damaged.H, HealStrokeSettings{Diameter: 56, Hardness: 1, Zoom: 1})
		stroke.Append(center)
		stroke.Append(Point{X: center.X + 0.5, Y: center.Y})
		stroke.Finish()
		if !SpotHeal(damaged, stroke.Coverage(), 1, mode, 0) {
			t.Fatalf("mode %d: SpotHeal reported no coverage", mode)
		}
		errors[mode] = meanChannelError(damaged, truth, center.X, center.Y, 22)
	}
	t.Logf("mean error over the hidden disc: Content-Aware %.2f levels, Proximity Match %.2f levels",
		errors[0], errors[2])
	if errors[0] >= 11.5 {
		t.Errorf("Content-Aware is off by %.2f levels; want under 11.5 (measured 10.10)", errors[0])
	}
	// Proximity Match copies one patch, the engine this replaces.
	if errors[0]*2 >= errors[2] {
		t.Errorf("Content-Aware (%.2f) is not far closer than one copied patch (%.2f)", errors[0], errors[2])
	}
}
