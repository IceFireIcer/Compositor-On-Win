package golden

// The retired 1.4.5-era references. They stay in the repository as the
// historical baseline (ticket 63, 65): their kernels no longer exist in the
// pinned reference, so they cannot be driven any more, but the numbers they
// recorded are the "before" side of a documented version change.
//
// See legacy/README.md for the recorded deltas.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"compositor-win/internal/render"
)

const legacyDir = "legacy"

// loadLegacyCase reads a retired case JSON (the file name is not a live case
// name any more, so the case name is set here).
func loadLegacyCase(t *testing.T, name string) Case {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(legacyDir, name+".json"))
	if err != nil {
		t.Fatalf("legacy case %s: %v", name, err)
	}
	var c Case
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("legacy case %s: %v", name, err)
	}
	c.CaseName = name
	return c
}

// legacyDelta reports how far the current Go port has moved from a retired
// reference, per channel byte.
func legacyDelta(t *testing.T, name string, got []byte) (maxDelta, meanDelta int, changed int) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(legacyDir, name+".png"))
	if err != nil {
		t.Fatalf("legacy reference %s: %v", name, err)
	}
	want, err := UnwrapPNG(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != len(got) {
		t.Fatalf("legacy %s: size %d, got %d", name, len(want), len(got))
	}
	total := 0
	for i := range want {
		d := int(got[i]) - int(want[i])
		if d < 0 {
			d = -d
		}
		total += d
		if d > 0 {
			changed++
		}
		if d > maxDelta {
			maxDelta = d
		}
	}
	if len(want) > 0 {
		meanDelta = total / len(want)
	}
	return maxDelta, meanDelta, changed
}

// TestLegacyCameraRawBaselineDrift pins the Light and Color group's 1.4.5 →
// 1.4.9 numeric change. Upstream replaced the direct-parameter kernel with one
// composed table of measured Photoshop colors (tickets 78/90 own that data),
// so the case was retired; the Go port keeps the direct kernel with the 1.4.9
// tone curve, and this test records exactly how far that lands from the
// baseline the harness used to assert.
func TestLegacyCameraRawBaselineDrift(t *testing.T) {
	c := loadLegacyCase(t, "camera-raw-1.4.5")
	px := BuildInput(c)
	bmp := render.NewBitmap(c.Width, c.Height)
	copy(bmp.Pix, px)
	num := func(key string) float64 { return c.Params[key].(float64) }
	render.ApplyCameraRaw(bmp, num("redGain"), num("greenGain"), num("blueGain"),
		num("exposure"), num("contrast"), num("highlights"), num("shadows"),
		num("whites"), num("blacks"), num("vibrance"), num("saturation"),
		int(num("clipping")))
	maxDelta, meanDelta, changed := legacyDelta(t, "camera-raw-1.4.5", bmp.Pix)
	t.Logf("camera-raw 1.4.5 baseline vs 1.4.9 tone curve: max Δ%d, mean Δ%d, %d/%d bytes moved",
		maxDelta, meanDelta, changed, len(bmp.Pix))
	if maxDelta == 0 {
		t.Fatal("the 1.4.9 tone curve must move the 1.4.5 baseline")
	}
	// The recorded drift is the whole measurement, not just its peak: a further
	// change to the Light and Color numerics moves these three together.
	if maxDelta != legacyCameraRawMaxDelta || meanDelta != legacyCameraRawMeanDelta ||
		changed != legacyCameraRawChangedBytes {
		t.Fatalf("tone drift changed: max Δ%d mean Δ%d over %d bytes, recorded Δ%d / Δ%d / %d",
			maxDelta, meanDelta, changed,
			legacyCameraRawMaxDelta, legacyCameraRawMeanDelta, legacyCameraRawChangedBytes)
	}
}

// The drift the 1.4.5 reference shows against the 1.4.9 tone curve, measured
// on 2026-10-14 (see legacy/README.md): max Δ147, mean Δ1, 2798 of 9216 bytes
// moved. Pinned so a further silent change to the Light and Color numerics
// fails here first.
const (
	legacyCameraRawMaxDelta     = 147
	legacyCameraRawMeanDelta    = 1
	legacyCameraRawChangedBytes = 2798
)
