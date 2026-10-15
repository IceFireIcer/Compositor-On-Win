package colorio

import (
	"errors"
	"math"
	"testing"
)

// The three lookups are 3D textures of 33^3 RGBA texels; the canvas shader
// reads them by (b,g,r). Anything else is a broken texture.
func TestProofLookupsShape(t *testing.T) {
	lut, err := ProofLookups(readFile(t, firstCMYKProfilePath(t)))
	if err != nil {
		t.Fatalf("ProofLookups: %v", err)
	}
	want := ProofDimension * ProofDimension * ProofDimension * 4
	for name, got := range map[string][]float32{"Proof": lut.Proof, "Warning": lut.Warning, "Marked": lut.Marked} {
		if len(got) != want {
			t.Fatalf("%s length = %d, want %d", name, len(got), want)
		}
		// Every texel is opaque, and the three color components are in [0,1].
		for i := 0; i < len(got); i += 4 {
			if got[i+3] != 1 {
				t.Fatalf("%s alpha at texel %d = %v, want 1", name, i/4, got[i+3])
			}
			for c := 0; c < 3; c++ {
				if got[i+c] < 0 || got[i+c] > 1 {
					t.Fatalf("%s component %d at texel %d = %v, want [0,1]", name, c, i/4, got[i+c])
				}
			}
		}
	}
}

// White paper is printable by definition: it must not be flagged, and the
// proof must show it as white.
func TestProofWhiteSurvives(t *testing.T) {
	lut, err := ProofLookups(readFile(t, firstCMYKProfilePath(t)))
	if err != nil {
		t.Fatalf("ProofLookups: %v", err)
	}
	i := LatticeIndex(ProofDimension-1, ProofDimension-1, ProofDimension-1) * 4
	if got := lut.Warning[i]; got == gamutMark {
		t.Fatalf("white was marked out of gamut (warning = %v)", got)
	}
	if got := lut.Marked[i]; got != 1 {
		t.Fatalf("marked white = %v, want 1 (the lattice value itself)", got)
	}
	for c := 0; c < 3; c++ {
		if got := lut.Proof[i+c]; math.Abs(float64(got)-1) > 0.02 {
			t.Fatalf("proof white component %d = %v, want ~1", c, got)
		}
	}
}

// The gamut warning must actually fire: a press cannot print every
// saturated sRGB primary.
func TestProofMarksAtLeastOneSaturatedPrimary(t *testing.T) {
	lut, err := ProofLookups(readFile(t, firstCMYKProfilePath(t)))
	if err != nil {
		t.Fatalf("ProofLookups: %v", err)
	}
	primaries := [][3]int{
		{ProofDimension - 1, 0, 0},
		{0, ProofDimension - 1, 0},
		{0, 0, ProofDimension - 1},
	}
	marked := 0
	for _, p := range primaries {
		if lut.Warning[LatticeIndex(p[0], p[1], p[2])*4] == gamutMark {
			marked++
		}
	}
	if marked == 0 {
		t.Fatal("no saturated primary was flagged out of gamut; the warning is dead")
	}
	t.Logf("%d of 3 saturated sRGB primaries are out of gamut for this profile", marked)
}

// Marked leaves in-gamut colors exactly as they were: it is the gamut
// warning without the proof.
func TestProofMarkedLeavesInGamutAlone(t *testing.T) {
	lut, err := ProofLookups(readFile(t, firstCMYKProfilePath(t)))
	if err != nil {
		t.Fatalf("ProofLookups: %v", err)
	}
	lattice := ProofLatticeRGB()
	// Mid gray is inside every press gamut.
	idx := LatticeIndex(ProofDimension/2, ProofDimension/2, ProofDimension/2)
	i := idx * 4
	for c := 0; c < 3; c++ {
		want := float32(lattice[idx*3+c]) / 255
		if got := lut.Marked[i+c]; got != want {
			t.Fatalf("marked mid gray component %d = %v, want %v (untouched)", c, got, want)
		}
	}
	// And where it is flagged, it is flagged with the gray upstream uses.
	for texel := 0; texel < proofTexels; texel++ {
		if lut.Warning[texel*4] == gamutMark {
			for c := 0; c < 3; c++ {
				if lut.Warning[texel*4+c] != gamutMark || lut.Marked[texel*4+c] != gamutMark {
					t.Fatalf("texel %d flagged with %v/%v, want both %v",
						texel, lut.Warning[texel*4+c], lut.Marked[texel*4+c], gamutMark)
				}
			}
			break
		}
	}
}

// Building the lookups twice for the same profile must yield the same
// content, and must come from the cache the second time.
func TestProofLookupsCache(t *testing.T) {
	ClearProofCache()
	profile := readFile(t, firstCMYKProfilePath(t))
	first, err := ProofLookups(profile)
	if err != nil {
		t.Fatalf("ProofLookups: %v", err)
	}
	second, err := ProofLookups(profile)
	if err != nil {
		t.Fatalf("ProofLookups (cached): %v", err)
	}
	for name, pair := range map[string][2][]float32{
		"Proof":   {first.Proof, second.Proof},
		"Warning": {first.Warning, second.Warning},
		"Marked":  {first.Marked, second.Marked},
	} {
		if len(pair[0]) != len(pair[1]) {
			t.Fatalf("%s length changed: %d then %d", name, len(pair[0]), len(pair[1]))
		}
		for i := range pair[0] {
			if pair[0][i] != pair[1][i] {
				t.Fatalf("%s[%d] = %v then %v", name, i, pair[0][i], pair[1][i])
			}
		}
	}

	// Clearing the cache must not change what gets built.
	ClearProofCache()
	third, err := ProofLookups(profile)
	if err != nil {
		t.Fatalf("ProofLookups (rebuilt): %v", err)
	}
	for i := range first.Proof {
		if first.Proof[i] != third.Proof[i] {
			t.Fatalf("rebuilt proof[%d] = %v, want %v", i, third.Proof[i], first.Proof[i])
		}
	}
}

func TestProofLookupsRejectsNonCMYK(t *testing.T) {
	_, err := ProofLookups(readFile(t, firstSRGBProfilePath(t)))
	if !errors.Is(err, ErrNotCMYK) {
		t.Fatalf("ProofLookups with an sRGB profile: err = %v, want ErrNotCMYK", err)
	}
}

// The three lookups differ exactly as upstream defines them: proof is what
// printing shows, warning is proof with gray marks, marked is the lattice
// with gray marks.
func TestProofRuleFromSyntheticShown(t *testing.T) {
	n := ProofDimension
	count := n * n * n
	lattice := ProofLatticeRGB()

	shown := make([]uint8, count*3)
	// Every texel round trips unchanged except a deliberately drifted one.
	for i := 0; i < count; i++ {
		shown[i*3] = lattice[i*3]
		shown[i*3+1] = lattice[i*3+1]
		shown[i*3+2] = lattice[i*3+2]
	}
	drifted := LatticeIndex(1, 2, 3)
	shown[drifted*3] += 11 // off by 11: outside

	lut := proofFromShown(lattice, shown)

	for _, tc := range []struct {
		name string
		idx  int
	}{
		{"untouched", LatticeIndex(5, 6, 7)},
		{"drifted", drifted},
	} {
		i := tc.idx * 4
		outside := tc.name == "drifted"
		for c := 0; c < 3; c++ {
			wantProof := float32(shown[tc.idx*3+c]) / 255
			if got := lut.Proof[i+c]; got != wantProof {
				t.Errorf("%s proof component %d = %v, want %v", tc.name, c, got, wantProof)
			}
			if outside {
				if got := lut.Warning[i+c]; got != gamutMark {
					t.Errorf("%s warning component %d = %v, want %v", tc.name, c, got, gamutMark)
				}
				if got := lut.Marked[i+c]; got != gamutMark {
					t.Errorf("%s marked component %d = %v, want %v", tc.name, c, got, gamutMark)
				}
			} else {
				if got, want := lut.Warning[i+c], lut.Proof[i+c]; got != want {
					t.Errorf("%s warning component %d = %v, want proof %v", tc.name, c, got, want)
				}
				wantMarked := float32(lattice[tc.idx*3+c]) / 255
				if got := lut.Marked[i+c]; got != wantMarked {
					t.Errorf("%s marked component %d = %v, want lattice %v", tc.name, c, got, wantMarked)
				}
			}
			if lut.Proof[i+3] != 1 || lut.Warning[i+3] != 1 || lut.Marked[i+3] != 1 {
				t.Fatalf("%s alpha = %v/%v/%v, want 1", tc.name, lut.Proof[i+3], lut.Warning[i+3], lut.Marked[i+3])
			}
		}
	}

	// A drift of exactly 10 must not be flagged.
	shown2 := make([]uint8, count*3)
	copy(shown2, lattice)
	edge := LatticeIndex(4, 4, 4)
	shown2[edge*3] += 10
	lut2 := proofFromShown(lattice, shown2)
	if got := lut2.Warning[edge*4]; got == gamutMark {
		t.Fatalf("an offset of exactly 10 was flagged: warning = %v", got)
	}
}
