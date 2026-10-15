package colorio

import (
	"math"
	"testing"
)

// The proof's lookups are sampled on a 33-point grid per side; upstream
// `CMYK.dimension`.
func TestProofDimension(t *testing.T) {
	if ProofDimension != 33 {
		t.Fatalf("ProofDimension = %d, want 33 (upstream CMYK.dimension)", ProofDimension)
	}
}

// Red must run fastest, because that is the order the canvas shader reads
// the 3D texture in: index = (b*33 + g)*33 + r.
func TestProofLatticeOrderAndValues(t *testing.T) {
	lattice := ProofLatticeRGB()
	if len(lattice) != ProofDimension*ProofDimension*ProofDimension*3 {
		t.Fatalf("lattice length = %d, want %d", len(lattice), ProofDimension*ProofDimension*ProofDimension*3)
	}

	// Along red the lattice must be the rounded 0..255 ramp.
	for r := 0; r < ProofDimension; r++ {
		want := uint8(math.Round(float64(r) * 255 / float64(ProofDimension-1)))
		if got := lattice[LatticeIndex(r, 0, 0)*3]; got != want {
			t.Fatalf("lattice[r=%d,g=0,b=0].r = %d, want %d", r, got, want)
		}
		if r > 0 && lattice[LatticeIndex(r, 0, 0)*3] <= lattice[LatticeIndex(r-1, 0, 0)*3] {
			t.Fatalf("red is not the fastest axis at r=%d", r)
		}
	}
	for g := 0; g < ProofDimension; g++ {
		want := uint8(math.Round(float64(g) * 255 / float64(ProofDimension-1)))
		if got := lattice[LatticeIndex(0, g, 0)*3+1]; got != want {
			t.Fatalf("lattice[r=0,g=%d,b=0].g = %d, want %d", g, got, want)
		}
	}
	for b := 0; b < ProofDimension; b++ {
		want := uint8(math.Round(float64(b) * 255 / float64(ProofDimension-1)))
		if got := lattice[LatticeIndex(0, 0, b)*3+2]; got != want {
			t.Fatalf("lattice[r=0,g=0,b=%d].b = %d, want %d", b, got, want)
		}
	}

	// Every corner must land on the sRGB cube's corners.
	corners := [][3]int{{0, 0, 0}, {32, 0, 0}, {0, 32, 0}, {0, 0, 32}, {32, 32, 32}}
	for _, c := range corners {
		i := LatticeIndex(c[0], c[1], c[2]) * 3
		want := [3]uint8{}
		for k, v := range c {
			if v == 32 {
				want[k] = 255
			}
		}
		if lattice[i] != want[0] || lattice[i+1] != want[1] || lattice[i+2] != want[2] {
			t.Fatalf("lattice%v = %v, want %v", c, lattice[i:i+3], want)
		}
	}
}

// Index arithmetic must agree with the (b,g,r) triple loop upstream fills
// the lattice with.
func TestLatticeIndexMatchesUpstreamLoopOrder(t *testing.T) {
	for b := 0; b < ProofDimension; b++ {
		for g := 0; g < ProofDimension; g++ {
			for r := 0; r < ProofDimension; r++ {
				want := (b*ProofDimension+g)*ProofDimension + r
				if got := LatticeIndex(r, g, b); got != want {
					t.Fatalf("LatticeIndex(%d,%d,%d) = %d, want %d", r, g, b, got, want)
				}
			}
		}
	}
}

// The gamut rule is upstream's `off = max_c |shown[c] - lattice[c]|; outside
// = off > 10`: printing must move a color further than its own rounding
// there and back would.
func TestGamutOutsideRule(t *testing.T) {
	cases := []struct {
		name    string
		shown   [3]uint8
		lattice [3]uint8
		want    bool
	}{
		{"identical", [3]uint8{10, 20, 30}, [3]uint8{10, 20, 30}, false},
		{"off by ten is still inside", [3]uint8{0, 0, 0}, [3]uint8{10, 10, 10}, false},
		{"off by eleven is outside", [3]uint8{0, 0, 0}, [3]uint8{11, 11, 11}, true},
		{"one channel over the line is enough", [3]uint8{200, 100, 100}, [3]uint8{200, 100, 89}, true},
		{"sign of the offset does not matter", [3]uint8{255, 255, 255}, [3]uint8{244, 244, 244}, true},
		{"largest channel decides", [3]uint8{0, 5, 9}, [3]uint8{1, 6, 10}, false},
	}
	for _, c := range cases {
		if got := GamutOutside(c.shown, c.lattice); got != c.want {
			t.Errorf("%s: GamutOutside(%v, %v) = %v, want %v", c.name, c.shown, c.lattice, got, c.want)
		}
	}
}
