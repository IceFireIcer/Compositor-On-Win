package render

// PatchFill tests — the port of PatchPixels.c. SpotHealingTests.swift pins the
// behaviors the multi-scale fill has to reproduce: Content-Aware carries a
// curved edge through a spot (within 12 levels of the hidden background, where
// one copied patch is off by more than twice that), busy texture runs on into
// what it fills instead of flattening, and a plain image gets no junk. The
// rest covers determinism, the transparent-outside rule, and a large hole's
// budget.

import (
	"bytes"
	"math"
	"runtime"
	"testing"
	"time"
)

// ridgeCenterY is where SpotHealingTests.swift puts the red disc on the curve.
var ridgeCenterY = 110 + 28*math.Sin(160.0/37)

// ridgeBitmap is SpotHealingTests.swift's ridge(): a grainy sky over a curved
// hilltop, optionally with a red disc on the curve (the damage to heal).
func ridgeBitmap(disc bool) *Bitmap {
	const w, h = 320, 200
	b := NewBitmap(w, h)
	seed := uint32(7)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			seed = seed*1664525 + 1013904223
			grain := float64(int(seed>>28)-8) * 2
			sky := float64(y) < 110+28*math.Sin(float64(x)/37)
			dx := float64(x) - 160
			dy := float64(y) - ridgeCenterY
			red := disc && dx*dx+dy*dy < 22*22
			var color [3]float64
			switch {
			case red:
				color = [3]float64{220, 30, 30}
			case sky:
				color = [3]float64{120 + float64(y)*0.5, 160 + float64(y)*0.3, 230}
			default:
				color = [3]float64{60 + grain, 110 + grain, 50 + grain}
			}
			i := (y*w + x) * 4
			for c := 0; c < 3; c++ {
				b.Pix[i+c] = uint8(maxInt(0, minInt(int(color[c]), 255)))
			}
			b.Pix[i+3] = 255
		}
	}
	return b
}

// patchTestTexture is a deterministic busy surface: 4-pixel blocks of random
// tone with a fine checker over them, the stand-in for the broken glass that
// 39d1257 is about.
func patchTestTexture(w, h int) *Bitmap {
	b := NewBitmap(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := int(healHash(uint32(x/4)*73856093^uint32(y/4)*19349663^0x9e3779b9) >> 24)
			if (x+y)%2 == 0 {
				v = v*3/4 + 40
			}
			i := (y*w + x) * 4
			b.Pix[i] = uint8(maxInt(0, minInt(v, 255)))
			b.Pix[i+1] = uint8(maxInt(0, minInt(v+24, 255)))
			b.Pix[i+2] = uint8(maxInt(0, minInt(v-24, 255)))
			b.Pix[i+3] = 255
		}
	}
	return b
}

// rectangleCoverage marks the half-open rectangle [x0, x1) × [y0, y1).
func rectangleCoverage(w, h, x0, y0, x1, y1 int) []uint8 {
	mask := make([]uint8, w*h)
	for y := maxInt(y0, 0); y < minInt(y1, h); y++ {
		for x := maxInt(x0, 0); x < minInt(x1, w); x++ {
			mask[y*w+x] = 255
		}
	}
	return mask
}

// discCoverage marks a hard disc of the given radius around (cx, cy).
func discCoverage(w, h int, cx, cy, radius float64) []uint8 {
	mask := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if dx*dx+dy*dy <= radius*radius {
				mask[y*w+x] = 255
			}
		}
	}
	return mask
}

// meanChannelError is the mean absolute per-channel difference over the disc,
// the error measure SpotHealingTests.swift uses.
func meanChannelError(got, want *Bitmap, cx, cy, radius float64) float64 {
	sum, count := 0.0, 0.0
	for y := 0; y < got.H; y++ {
		for x := 0; x < got.W; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			if dx*dx+dy*dy >= radius*radius {
				continue
			}
			i := (y*got.W + x) * 4
			for c := 0; c < 3; c++ {
				sum += math.Abs(float64(got.Pix[i+c]) - float64(want.Pix[i+c]))
				count++
			}
		}
	}
	return sum / count
}

// lumaStd is the standard deviation of luma over a half-open rectangle.
func lumaStd(b *Bitmap, x0, y0, x1, y1 int) float64 {
	sum, sumSquares, n := 0.0, 0.0, 0.0
	for y := maxInt(y0, 0); y < minInt(y1, b.H); y++ {
		for x := maxInt(x0, 0); x < minInt(x1, b.W); x++ {
			i := (y*b.W + x) * 4
			v := (float64(b.Pix[i]) + float64(b.Pix[i+1]) + float64(b.Pix[i+2])) / 3
			sum += v
			sumSquares += v * v
			n++
		}
	}
	mean := sum / n
	return math.Sqrt(sumSquares/n - mean*mean)
}

func TestPatchFillIsDeterministic(t *testing.T) {
	base := patchTestTexture(96, 96)
	hole := rectangleCoverage(96, 96, 40, 40, 56, 56)
	first := base.Clone()
	if !PatchFill(first, hole, 7) {
		t.Fatal("PatchFill found no source pixels in a busy 96x96 image")
	}
	for run := 0; run < 2; run++ {
		again := base.Clone()
		if !PatchFill(again, hole, 7) {
			t.Fatal("PatchFill found no source pixels on a repeat run")
		}
		if !bytes.Equal(first.Pix, again.Pix) {
			t.Fatalf("run %d differs from the first: PatchFill is not deterministic at a fixed seed", run)
		}
	}
	// Only the hole may change.
	for p := 0; p < 96*96; p++ {
		if hole[p] != 0 {
			continue
		}
		if !bytes.Equal(first.Pix[p*4:p*4+4], base.Pix[p*4:p*4+4]) {
			t.Fatalf("pixel %d outside the hole changed: %v -> %v", p, base.Pix[p*4:p*4+4], first.Pix[p*4:p*4+4])
		}
	}
}

// SpotHealingTests.swift: Content-Aware comes within a handful of levels of the
// hidden background where one copied patch is off by tens of levels.
func TestPatchFillCarriesACurvedEdgeThroughTheHole(t *testing.T) {
	truth := ridgeBitmap(false)
	damaged := ridgeBitmap(true)
	const radius = 28
	hole := discCoverage(damaged.W, damaged.H, 160, ridgeCenterY, radius)
	filled := damaged.Clone()
	if !PatchFill(filled, hole, 0) {
		t.Fatal("PatchFill found no source pixels around the ridge disc")
	}
	// The stand-in for the engine this replaces: the old ContentFill.c copied
	// one patch over the hole, so a patch shifted clear of it is what the
	// numbers below are measured against (that kernel is no longer in the
	// tree, so it cannot be called directly).
	shifted := damaged.Clone()
	for y := 0; y < damaged.H; y++ {
		for x := 0; x < damaged.W; x++ {
			if hole[y*damaged.W+x] == 0 {
				continue
			}
			s := (y*damaged.W + x + 2*radius) * 4
			d := (y*damaged.W + x) * 4
			copy(shifted.Pix[d:d+4], damaged.Pix[s:s+4])
		}
	}
	fillError := meanChannelError(filled, truth, 160, ridgeCenterY, 22)
	copyError := meanChannelError(shifted, truth, 160, ridgeCenterY, 22)
	t.Logf("mean error over the hidden disc: PatchFill %.2f levels, one copied patch %.2f levels", fillError, copyError)
	if fillError >= 11.5 {
		t.Errorf("PatchFill is off by %.2f levels; want under 11.5 (measured 10.52)", fillError)
	}
	if fillError*2 >= copyError {
		t.Errorf("PatchFill (%.2f) is not far closer than one copied patch (%.2f)", fillError, copyError)
	}
}

// 39d1257: busy texture is measured at full size and weighs in against color,
// so extending a picture of breaking glass runs the glass on instead of
// stopping at a flat line.
func TestPatchFillCarriesBusyTextureToTheEdge(t *testing.T) {
	const w, h = 128, 128
	base := patchTestTexture(w, h)
	hole := rectangleCoverage(w, h, 100, 0, 128, 128) // the right strip, out to the canvas edge
	filled := base.Clone()
	if !PatchFill(filled, hole, 3) {
		t.Fatal("PatchFill found no source pixels for the edge strip")
	}
	got := lumaStd(filled, 104, 8, 124, 120)
	want := lumaStd(base, 72, 8, 92, 120)
	t.Logf("filled strip luma std %.2f against the source texture's %.2f", got, want)
	if got < 8 {
		t.Errorf("the filled strip is nearly flat (luma std %.2f); busy texture did not carry on", got)
	}
	if got < want*0.4 {
		t.Errorf("the filled strip (std %.2f) lost most of the source texture (std %.2f)", got, want)
	}
}

func TestPatchFillOnAPlainImageAddsNoJunk(t *testing.T) {
	const w, h = 64, 64
	base := NewBitmap(w, h)
	for i := 0; i < len(base.Pix); i += 4 {
		copy(base.Pix[i:i+4], []byte{20, 90, 200, 255})
	}
	hole := discCoverage(w, h, 32, 32, 10)
	filled := base.Clone()
	if !PatchFill(filled, hole, 5) {
		t.Fatal("PatchFill found no source pixels in a plain image")
	}
	for p := 0; p < w*h; p++ {
		if hole[p] == 0 {
			continue
		}
		if got := filled.Pix[p*4 : p*4+4]; !bytes.Equal(got, []byte{20, 90, 200, 255}) {
			t.Fatalf("plain image pixel %d came out %v, not the plain color", p, got)
		}
	}
}

func TestPatchFillLeavesTransparentOutsideAlone(t *testing.T) {
	const w, h = 64, 64
	base := NewBitmap(w, h)
	// An opaque green square with transparent magenta around it: a fill that
	// matched or copied the transparent pixels would come out magenta.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			if x >= 12 && x < 52 && y >= 12 && y < 52 {
				copy(base.Pix[i:i+4], []byte{0, 180, 0, 255})
			} else {
				copy(base.Pix[i:i+4], []byte{255, 0, 255, 0})
			}
		}
	}
	hole := rectangleCoverage(w, h, 12, 12, 24, 24) // touching the square's corner
	filled := base.Clone()
	if !PatchFill(filled, hole, 11) {
		t.Fatal("PatchFill found no source pixels beside the transparent margin")
	}
	for p := 0; p < w*h; p++ {
		i := p * 4
		if hole[p] != 0 {
			got := filled.Pix[i : i+4]
			if got[0] > 32 || got[2] > 32 || got[3] != 255 {
				t.Fatalf("hole pixel %d came out %v; transparent magenta leaked into the fill", p, got)
			}
			continue
		}
		if !bytes.Equal(filled.Pix[i:i+4], base.Pix[i:i+4]) {
			t.Fatalf("pixel %d outside the hole changed: %v -> %v", p, base.Pix[i:i+4], filled.Pix[i:i+4])
		}
	}
}

// A 256 x 256 image with a 120 x 120 hole. The time and allocation budgets are
// deliberately loose — this is a "does not explode" check, not a benchmark, and
// CI machines vary — so they only catch a change of order, not a slow run.
func TestPatchFillLargeHoleWithinBudget(t *testing.T) {
	const w, h = 256, 256
	base := patchTestTexture(w, h)
	hole := rectangleCoverage(w, h, 68, 68, 188, 188)
	filled := base.Clone()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	if !PatchFill(filled, hole, 1) {
		t.Fatal("PatchFill found no source pixels around a 120x120 hole")
	}
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("256x256 with a 120x120 hole took %v and allocated %d MiB", elapsed, allocated>>20)
	if elapsed > 60*time.Second {
		t.Errorf("PatchFill took %v for a 120x120 hole; the budget is 60s", elapsed)
	}
	if allocated > 1<<30 {
		t.Errorf("PatchFill allocated %d MiB for a 120x120 hole; the budget is 1 GiB", allocated>>20)
	}
	// The fill must land near the image's own tone rather than on garbage.
	holeLuma, aroundLuma := 0.0, 0.0
	for y := 68; y < 188; y++ {
		for x := 68; x < 188; x++ {
			i := (y*w + x) * 4
			holeLuma += (float64(filled.Pix[i]) + float64(filled.Pix[i+1]) + float64(filled.Pix[i+2])) / 3
		}
	}
	for y := 8; y < 248; y++ {
		for x := 8; x < 60; x++ {
			i := (y*w + x) * 4
			aroundLuma += (float64(filled.Pix[i]) + float64(filled.Pix[i+1]) + float64(filled.Pix[i+2])) / 3
		}
	}
	holeLuma /= 120 * 120
	aroundLuma /= 240 * 52
	if math.Abs(holeLuma-aroundLuma) > 40 {
		t.Errorf("the filled hole (luma %.1f) does not match the surrounding image (%.1f)", holeLuma, aroundLuma)
	}
}
