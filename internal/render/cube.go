package render

import (
	"encoding/json"
	"math"

	"compositor-win/internal/domain"
)

// Hue/Saturation color cube generation, ported from HueSaturation.swift
// (HueSaturationFilter). The cube is 33³ entries of straight RGBA floats
// (alpha 1, red varying fastest — the order cube_apply reads), built once
// per settings: every lattice corner converted to HSL, adjusted, converted
// back. GPUAdjustment hands the same data to CIColorCube, so both render
// tracks share the Cube struct below.

// Cube is a dimension³ color lookup: Data holds Dimension³×4 float32 RGBA
// entries, red varying fastest. Shared by the CPU truth path (ApplyCube)
// and the GPU track.
type Cube struct {
	Dimension int
	Data      []float32
}

// HueCubeDimension is the lattice size, "the usual size for this kind of
// lookup: fast to build, smooth enough" (HueSaturationFilter.dimension).
const HueCubeDimension = 33

// The six color ranges plus Master, as in Photoshop's Cmd+U. Raw values are
// the macOS ColorRange spellings, the keys of hsvSettings' dictionaries.
const (
	hsvMaster   = "Master"
	hsvReds     = "Reds"
	hsvYellows  = "Yellows"
	hsvGreens   = "Greens"
	hsvCyans    = "Cyans"
	hsvBlues    = "Blues"
	hsvMagentas = "Magentas"
)

var hsvColorRanges = []string{hsvReds, hsvYellows, hsvGreens, hsvCyans, hsvBlues, hsvMagentas}

// hueBand is a hue band in degrees, wrapping at 360: full strength between
// rangeStart and rangeEnd, fading to nothing at falloffStart/falloffEnd.
type hueBand struct {
	FalloffStart float64 `json:"falloffStart"`
	RangeStart   float64 `json:"rangeStart"`
	RangeEnd     float64 `json:"rangeEnd"`
	FalloffEnd   float64 `json:"falloffEnd"`
}

// defaultBand ports ColorRange.defaultBand — Photoshop's starting hue band.
func defaultBand(colorRange string) hueBand {
	switch colorRange {
	case hsvMaster:
		return hueBand{0, 0, 360, 360}
	case hsvReds:
		return hueBand{315, 345, 15, 45}
	case hsvYellows:
		return hueBand{15, 45, 75, 105}
	case hsvGreens:
		return hueBand{75, 105, 135, 165}
	case hsvCyans:
		return hueBand{135, 165, 195, 225}
	case hsvBlues:
		return hueBand{195, 225, 255, 285}
	default: // magentas
		return hueBand{255, 285, 315, 345}
	}
}

// hueRangeAdjustment is one range's slider values.
type hueRangeAdjustment struct {
	Hue        float64 `json:"hue"`
	Saturation float64 `json:"saturation"`
	Lightness  float64 `json:"lightness"`
}

// hueSatSettings mirrors the macOS HueSaturationSettings as it serializes
// into hsvSettings. The domain keeps that JSON as a RawMessage until the
// HSV editor lands; this view parses it for rendering without touching the
// manifest encoding.
type hueSatSettings struct {
	Range       string                        `json:"range"`
	Colorize    bool                          `json:"colorize"`
	InvertRange bool                          `json:"invertRange"`
	Adjustments map[string]hueRangeAdjustment `json:"adjustments"`
	Bands       map[string]hueBand            `json:"bands"`
}

// resolvedHSV ports LayerAdjustment.resolvedHSV: the typed hsvSettings when
// present, otherwise the legacy scalar fields as a Master adjustment.
func resolvedHSV(a *domain.Adjustment) hueSatSettings {
	if len(a.HSVSettings) > 0 {
		var s hueSatSettings
		if json.Unmarshal(a.HSVSettings, &s) == nil {
			s.fillDefaults()
			return s
		}
	}
	s := hueSatSettings{
		Range:       hsvMaster,
		Colorize:    a.Colorize,
		Adjustments: map[string]hueRangeAdjustment{},
	}
	if a.Hue != 0 || a.Saturation != 0 || a.Lightness != 0 {
		s.Adjustments[hsvMaster] = hueRangeAdjustment{a.Hue, a.Saturation, a.Lightness}
	}
	// The Swift init always records the (possibly zero) adjustment; the map
	// above skips zeros only where hueResponse would skip them anyway.
	if _, ok := s.Adjustments[hsvMaster]; !ok {
		s.Adjustments[hsvMaster] = hueRangeAdjustment{}
	}
	s.fillDefaults()
	return s
}

// fillDefaults gives missing maps the Swift defaults: no adjustments, the
// Photoshop bands.
func (s *hueSatSettings) fillDefaults() {
	if s.Range == "" {
		s.Range = hsvMaster
	}
	if s.Adjustments == nil {
		s.Adjustments = map[string]hueRangeAdjustment{}
	}
	if s.Bands == nil {
		s.Bands = map[string]hueBand{}
	}
	for _, r := range append([]string{hsvMaster}, hsvColorRanges...) {
		if _, ok := s.Bands[r]; !ok {
			s.Bands[r] = defaultBand(r)
		}
	}
}

// forward returns degrees from `from` forward to `to`, always 0…360.
func bandForward(from, to float64) float64 {
	delta := math.Mod(to-from, 360)
	if delta < 0 {
		delta += 360
	}
	return delta
}

// weight ports HueBand.weight: how strongly the band claims a hue — 1 inside
// the range, linear ramps through each falloff shoulder, 0 outside.
func (b hueBand) weight(hue float64) float64 {
	span := bandForward(b.FalloffStart, b.FalloffEnd)
	if span <= 0 {
		return 1 // Master covers everything.
	}
	position := bandForward(b.FalloffStart, hue)
	if position > span {
		return 0
	}
	rampIn := bandForward(b.FalloffStart, b.RangeStart)
	plateauEnd := bandForward(b.FalloffStart, b.RangeEnd)
	if position < rampIn {
		if rampIn > 0 {
			return position / rampIn
		}
		return 1
	}
	if position <= plateauEnd {
		return 1
	}
	rampOut := span - plateauEnd
	if rampOut > 0 {
		return (span - position) / rampOut
	}
	return 1
}

// weightOf ports HueSaturationSettings.weight: Master everywhere; other
// ranges through their band, inverted for the selected range when
// invertRange is on.
func (s *hueSatSettings) weightOf(colorRange string, hue float64) float64 {
	if colorRange == hsvMaster {
		return 1
	}
	band, ok := s.Bands[colorRange]
	if !ok {
		band = defaultBand(colorRange)
	}
	w := band.weight(hue)
	if s.InvertRange && colorRange == s.Range {
		return 1 - w
	}
	return w
}

// hueResponse samples, once per degree, how much every range shifts a hue —
// the table that keeps the cube cheap (HueSaturationFilter.hueResponse).
type hueResponseEntry struct {
	shift, saturation, lightness float64
}

func hueResponse(s *hueSatSettings) []hueResponseEntry {
	out := make([]hueResponseEntry, 361)
	// Fixed range order (Master, then the wheel): the Swift dictionary
	// iterates unordered, but a deterministic order keeps the float sums —
	// and therefore the cube bytes — stable across runs.
	for _, name := range append([]string{hsvMaster}, hsvColorRanges...) {
		adj, ok := s.Adjustments[name]
		if !ok || adj == (hueRangeAdjustment{}) {
			continue
		}
		for degree := 0; degree <= 360; degree++ {
			w := s.weightOf(name, float64(degree))
			if w <= 0 {
				continue
			}
			out[degree].shift += adj.Hue * w
			out[degree].saturation += adj.Saturation * w
			out[degree].lightness += adj.Lightness * w
		}
	}
	return out
}

// adjustedSaturation ports HueSaturationFilter.adjustedSaturation —
// Photoshop's saturation: below 0 scales toward gray, above 0 divides by
// what's left (+50 doubles, +100 saturates fully). Multiplicative both ways,
// so neutral grays stay neutral.
func adjustedSaturation(saturation, amount float64) float64 {
	amount = math.Min(1, math.Max(-1, amount))
	if amount <= 0 {
		return math.Max(0, saturation*(1+amount))
	}
	if amount >= 1 {
		if saturation > 0 {
			return 1
		}
		return 0
	}
	return math.Min(1, saturation/(1-amount))
}

// rgbToHSL ports HueSaturationFilter.toHSL.
func rgbToHSL(r, g, b float64) (h, s, l float64) {
	high := math.Max(r, math.Max(g, b))
	low := math.Min(r, math.Min(g, b))
	l = (high + low) / 2
	delta := high - low
	if delta <= 0 {
		return 0, 0, l
	}
	s = delta / (1 - math.Abs(2*l-1))
	if high == r {
		h = (g - b) / delta
	} else if high == g {
		h = (b-r)/delta + 2
	} else {
		h = (r-g)/delta + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, math.Min(1, s), l
}

// hslToRGB ports HueSaturationFilter.toRGB.
func hslToRGB(h, s, l float64) (r, g, b float64) {
	if s <= 0 {
		return l, l, l
	}
	chroma := (1 - math.Abs(2*l-1)) * s
	sector := h / 60
	second := chroma * (1 - math.Abs(math.Mod(sector, 2)-1))
	base := l - chroma/2
	switch int(sector) { // Int() truncates, as Swift's Int(sector) does.
	case 0:
		r, g, b = chroma, second, 0
	case 1:
		r, g, b = second, chroma, 0
	case 2:
		r, g, b = 0, chroma, second
	case 3:
		r, g, b = 0, second, chroma
	case 4:
		r, g, b = second, 0, chroma
	default:
		r, g, b = chroma, 0, second
	}
	return clamp01(r + base), clamp01(g + base), clamp01(b + base)
}

// hsAdjust ports HueSaturationFilter.adjust: one lattice corner (or pixel)
// color through the settings, using the per-degree response table.
func hsAdjust(r, g, b float64, s *hueSatSettings, response []hueResponseEntry) (float64, float64, float64) {
	hue, saturation, lightness := rgbToHSL(r, g, b)
	var lightnessAmount float64
	if s.Colorize {
		hue = math.Mod(s.hue(), 360)
		saturation = math.Min(1, math.Max(0, s.saturation()/100))
		lightnessAmount = s.lightness() / 100
	} else {
		sampled := response[int(math.Min(float64(len(response)-1), math.Max(0, math.Round(hue))))]
		lightnessAmount = sampled.lightness / 100
		hue = math.Mod(hue+sampled.shift, 360)
		if hue < 0 {
			hue += 360
		}
		saturation = adjustedSaturation(saturation, sampled.saturation)
	}
	// Lightness pulls toward white above 0 and toward black below, reaching
	// either at ±100.
	amount := math.Min(1, math.Max(-1, lightnessAmount))
	if amount >= 0 {
		lightness += (1 - lightness) * amount
	} else {
		lightness *= 1 + amount
	}
	return hslToRGB(hue, saturation, math.Min(1, math.Max(0, lightness)))
}

// hue/saturation/lightness read the selected range's sliders, as the Swift
// computed properties do.
func (s *hueSatSettings) hue() float64 {
	if adj, ok := s.Adjustments[s.Range]; ok {
		return adj.Hue
	}
	return 0
}

func (s *hueSatSettings) saturation() float64 {
	if adj, ok := s.Adjustments[s.Range]; ok {
		return adj.Saturation
	}
	return 0
}

func (s *hueSatSettings) lightness() float64 {
	if adj, ok := s.Adjustments[s.Range]; ok {
		return adj.Lightness
	}
	return 0
}

// BuildHueSaturationCube ports HueSaturationFilter.buildCube: every lattice
// corner through hsAdjust, blue outermost and red fastest (the order
// cube_apply and CIColorCube read).
func BuildHueSaturationCube(a *domain.Adjustment) *Cube {
	s := resolvedHSV(a)
	response := hueResponse(&s)
	n := HueCubeDimension
	step := float64(n - 1)
	cube := &Cube{Dimension: n, Data: make([]float32, n*n*n*4)}
	i := 0
	for b := 0; b < n; b++ {
		for g := 0; g < n; g++ {
			for r := 0; r < n; r++ {
				rr, gg, bb := hsAdjust(float64(r)/step, float64(g)/step, float64(b)/step, &s, response)
				cube.Data[i] = float32(rr)
				cube.Data[i+1] = float32(gg)
				cube.Data[i+2] = float32(bb)
				cube.Data[i+3] = 1
				i += 4
			}
		}
	}
	return cube
}
