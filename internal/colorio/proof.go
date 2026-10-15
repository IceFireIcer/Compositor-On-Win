//go:build windows

package colorio

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sync"
)

// Points along each side of the proof's color lookups; upstream
// `CMYK.dimension`.
const ProofDimension = 33

// proofTexels is one 3D texture worth of RGBA texels: 33^3.
const proofTexels = ProofDimension * ProofDimension * ProofDimension

// gamutMark is the gray upstream paints over colors the press cannot print.
const gamutMark = 0.5

// gamutThreshold is upstream's tolerance for the round trip, in 8-bit units:
// "Out of gamut: printing moves it further than its own rounding there and
// back would."
const gamutThreshold = 10

// ErrProofUnavailable reports a proof that could not be built — upstream
// `proofLookups` returns nil there. A profile that is not CMYK is refused
// with ErrNotCMYK instead, so callers can tell the two apart.
var ErrProofUnavailable = errors.New("could not build the CMYK proof lookups for this profile")

// ProofLUTs are the canvas's three color lookups, made once per profile:
// `Proof` takes each RGB color to the one printing it through the profile
// gives back; `Warning` is the same, but marks gray the colors it cannot
// print as they are; `Marked` leaves colors as they are and marks those
// gray (the gamut warning without the proof).
//
// Each slice holds 33^3 RGBA texels as float32 in [0,1], ready to upload as
// a WebGPU 3D texture. The lattice is stored red fastest, which is the order
// the shader reads.
type ProofLUTs struct {
	Proof   []float32
	Warning []float32
	Marked  []float32
}

// LatticeIndex is the texel of the sRGB lattice for the color (r,g,b), each
// in 0..32. Red runs fastest, matching upstream's `i = ((b*n + g)*n + r)`.
func LatticeIndex(r, g, b int) int {
	return (b*ProofDimension+g)*ProofDimension + r
}

// level is upstream's `level(_:)`: the 8-bit value of lattice step i,
// `round(i * 255 / 32)`.
func level(i int) uint8 {
	return uint8(math.Round(float64(i) * 255 / float64(ProofDimension-1)))
}

// ProofLatticeRGB builds the lattice of sRGB colors the lookups sample:
// 33^3 unassociated RGB triples, red fastest. Upstream stores these as
// opaque 32-bit pixels; only the color components matter to the proof, so
// this returns three bytes per texel.
func ProofLatticeRGB() []uint8 {
	lattice := make([]uint8, proofTexels*3)
	for b := 0; b < ProofDimension; b++ {
		for g := 0; g < ProofDimension; g++ {
			for r := 0; r < ProofDimension; r++ {
				i := LatticeIndex(r, g, b) * 3
				lattice[i] = level(r)
				lattice[i+1] = level(g)
				lattice[i+2] = level(b)
			}
		}
	}
	return lattice
}

// GamutOutside is upstream's gamut rule on one texel: the round trip moved
// the color further than its own rounding there and back would, so the press
// cannot print it as it is.
func GamutOutside(shown, lattice [3]uint8) bool {
	off := 0
	for c := 0; c < 3; c++ {
		d := int(shown[c]) - int(lattice[c])
		if d < 0 {
			d = -d
		}
		if d > off {
			off = d
		}
	}
	return off > gamutThreshold
}

// proofFromShown turns the round-tripped lattice into the three lookups.
// Split from the conversion so the rule can be tested without an ICC file:
// shown and lattice are w*h*3 sRGB buffers of the same length.
func proofFromShown(lattice, shown []uint8) ProofLUTs {
	out := ProofLUTs{
		Proof:   make([]float32, proofTexels*4),
		Warning: make([]float32, proofTexels*4),
		Marked:  make([]float32, proofTexels*4),
	}
	for i := 0; i < proofTexels; i++ {
		var pair, ref [3]uint8
		for c := 0; c < 3; c++ {
			pair[c] = shown[i*3+c]
			ref[c] = lattice[i*3+c]
		}
		outside := GamutOutside(pair, ref)
		j := i * 4
		for c := 0; c < 3; c++ {
			proof := float32(pair[c]) / 255
			out.Proof[j+c] = proof
			if outside {
				out.Warning[j+c] = gamutMark
				out.Marked[j+c] = gamutMark
			} else {
				out.Warning[j+c] = proof
				out.Marked[j+c] = float32(ref[c]) / 255
			}
		}
		out.Proof[j+3] = 1
		out.Warning[j+3] = 1
		out.Marked[j+3] = 1
	}
	return out
}

// proofCache holds the lookups by profile, as upstream caches them by
// profile name. The key is the profile's content, so two profiles that share
// a name but not their bytes cannot collide.
var proofCache = struct {
	sync.Mutex
	byKey map[[32]byte]ProofLUTs
}{byKey: map[[32]byte]ProofLUTs{}}

// ClearProofCache forgets every cached proof. Tests need it; a profile that
// is reloaded from disk gets a fresh entry by itself, because the key is the
// content.
func ClearProofCache() {
	proofCache.Lock()
	defer proofCache.Unlock()
	proofCache.byKey = map[[32]byte]ProofLUTs{}
}

// ProofLookups builds (or returns from the cache) the canvas lookups for a
// CMYK profile. It returns ErrNotCMYK for any other profile and
// ErrProofUnavailable when the conversions cannot be made.
//
// The three slices of a cached profile are shared between calls, as the
// original's cached Data was: upload them, do not write to them.
func ProofLookups(profile []byte) (ProofLUTs, error) {
	key := sha256.Sum256(profile)
	proofCache.Lock()
	if known, ok := proofCache.byKey[key]; ok {
		proofCache.Unlock()
		return known, nil
	}
	proofCache.Unlock()

	made, err := buildProofLookups(profile)
	if err != nil {
		return ProofLUTs{}, err
	}
	proofCache.Lock()
	proofCache.byKey[key] = made
	proofCache.Unlock()
	return made, nil
}

// buildProofLookups samples the profile once: every lattice color, a pixel
// each, printed through the profile over white paper and read back on
// screen.
func buildProofLookups(profile []byte) (ProofLUTs, error) {
	lattice := ProofLatticeRGB()

	// Through the profile and back, over white paper.
	ink, err := ConvertToInk(lattice, proofTexels, 1, profile, [3]uint8{255, 255, 255})
	if err != nil {
		if errors.Is(err, ErrNotCMYK) {
			return ProofLUTs{}, err
		}
		return ProofLUTs{}, fmt.Errorf("%w: %v", ErrProofUnavailable, err)
	}
	shown, err := ConvertToRGB(ink, proofTexels, 1, profile)
	if err != nil {
		return ProofLUTs{}, fmt.Errorf("%w: %v", ErrProofUnavailable, err)
	}
	if len(shown) != proofTexels*3 {
		return ProofLUTs{}, fmt.Errorf("%w: round trip returned %d bytes, want %d", ErrProofUnavailable, len(shown), proofTexels*3)
	}
	return proofFromShown(lattice, shown), nil
}
