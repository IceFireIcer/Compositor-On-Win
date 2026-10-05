package render

import (
	"math"

	"compositor-win/internal/domain"
)

// LUT generation, ported from the macOS adjustment records: Levels.swift
// (LevelRange.apply / LevelsFilter.run), Curves.swift (CurvesSettings.value)
// and ImageAdjustments.swift (ExposureSettings.table / GradientMapSettings).
// The same builders feed both tracks — the CPU truth path asks for 256
// entries (the size the C kernels interpolate over) and the GPU track for
// 1024 (GPUAdjustment.apply), so a LUT's size is a parameter.

// LUT is a per-channel lookup: three tables (red, green, blue) of Size
// entries each, entry i the straight sRGB output (0–1) for channel input
// i/(Size-1). Both render tracks share one instance of this struct.
type LUT struct {
	Size   int
	Tables [3][]float32
}

// normalizeLevelRange ports LevelRange.normalized: every field clamped into
// its accepted range, white kept above black, non-finite values replaced by
// the identity defaults.
func normalizeLevelRange(r domain.LevelRange) domain.LevelRange {
	clamp := func(n, lo, hi, fallback float64) float64 {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return fallback
		}
		return math.Min(hi, math.Max(lo, n))
	}
	r.Black = clamp(r.Black, 0, 254, 0)
	r.White = clamp(r.White, r.Black+1, 255, 255)
	r.Gamma = clamp(r.Gamma, 0.1, 9.99, 1)
	r.OutputBlack = clamp(r.OutputBlack, 0, 255, 0)
	r.OutputWhite = clamp(r.OutputWhite, 0, 255, 255)
	return r
}

// applyLevelRange ports LevelRange.apply: input stretched between the black
// and white points, gamma bent, output stretched back.
func applyLevelRange(r domain.LevelRange, value float64) float64 {
	s := normalizeLevelRange(r)
	input := math.Min(1, math.Max(0, (value*255-s.Black)/(s.White-s.Black)))
	return (s.OutputBlack + math.Pow(input, 1/s.Gamma)*(s.OutputWhite-s.OutputBlack)) / 255
}

// applyLevels ports LevelsSettings.apply: the per-channel range runs first,
// the composite RGB range (ranges[0]) after it.
func applyLevels(s domain.LevelsSettings, value float64, channel int) float64 {
	return applyLevelRange(s.Ranges[0], applyLevelRange(s.Ranges[channel], value))
}

// identityLevelRange is the normalized identity: the macOS LevelRange
// struct's defaults.
var identityLevelRange = domain.LevelRange{Black: 0, Gamma: 1, White: 255, OutputBlack: 0, OutputWhite: 255}

// levelsIdentity ports LevelsSettings.isIdentity: every normalized range
// equals the default.
func levelsIdentity(s domain.LevelsSettings) bool {
	for _, r := range s.Ranges {
		if normalizeLevelRange(r) != identityLevelRange {
			return false
		}
	}
	return true
}

// BuildLevelsLUT ports the table LevelsFilter.run builds — per channel
// (red, green, blue), every input step through the two-stage range apply.
// Size 256 matches the CPU truth; GPUAdjustment uses 1024. The table index
// maps to the Levels channel one step up: ranges[0] is the composite RGB
// range, ranges[1…3] are red, green, blue.
func BuildLevelsLUT(s domain.LevelsSettings, size int) *LUT {
	return buildChannelLUT(size, func(value float64, channel int) float64 {
		return applyLevels(s, value, channel+1)
	})
}

// curveValue ports CurvesSettings.value: shape-preserving monotone cubic
// Hermite interpolation between the anchors — slopes that would overshoot
// are flattened to zero, and the harmonic mean of the adjoining secants
// keeps the curve through its handles.
func curveValue(points []domain.CurvePoint, x float64) float64 {
	i := -1
	for j := len(points) - 1; j >= 0; j-- {
		if points[j].X <= x {
			i = j
			break
		}
	}
	if i < 0 {
		i = 0
	}
	i = min(len(points)-2, i)
	d := make([]float64, len(points)-1)
	for j := range d {
		d[j] = (points[j+1].Y - points[j].Y) / (points[j+1].X - points[j].X)
	}
	slope := func(j int) float64 {
		if j == 0 {
			return d[0]
		}
		if j == len(points)-1 {
			return d[len(d)-1]
		}
		if d[j-1]*d[j] <= 0 {
			return 0
		}
		return 2 / (1/d[j-1] + 1/d[j])
	}
	h := points[i+1].X - points[i].X
	t := math.Min(1, math.Max(0, (x-points[i].X)/h))
	t2, t3 := t*t, t*t*t
	y := (2*t3-3*t2+1)*points[i].Y +
		(t3-2*t2+t)*h*slope(i) +
		(-2*t3+3*t2)*points[i+1].Y +
		(t3-t2)*h*slope(i+1)
	return math.Min(255, math.Max(0, y))
}

// BuildCurvesLUT ports the table curves.apply builds: each color channel's
// curve runs first, the composite RGB curve (channels[0]) after it, so a
// master curve bends the per-channel result.
func BuildCurvesLUT(s domain.CurvesSettings, size int) *LUT {
	return buildChannelLUT(size, func(value float64, channel int) float64 {
		x := value * 255
		return curveValue(s.Channels[0], curveValue(s.Channels[channel+1], x)) / 255
	})
}

// srgbToLinear/linearToSrgb are the transfer functions the original uses
// throughout its adjustments (srgb_to_linear / linear_to_srgb in
// AdjustPixels.c, and inline in ExposureSettings.table).
func srgbToLinear(encoded float64) float64 {
	if encoded <= 0.04045 {
		return encoded / 12.92
	}
	return math.Pow((encoded+0.055)/1.055, 2.4)
}

func linearToSrgb(linear float64) float64 {
	if linear <= 0 {
		return 0
	}
	if linear >= 1 {
		return 1
	}
	if linear <= 0.0031308 {
		return linear * 12.92
	}
	return 1.055*math.Pow(linear, 1/2.4) - 0.055
}

// BuildExposureLUT ports ExposureSettings.table: Photoshop's exposure —
// decoded to linear light, scaled by 2^exposure, offset, gamma bent, and
// encoded back to sRGB. The same curve runs on every channel.
func BuildExposureLUT(s domain.ExposureSettings, size int) *LUT {
	scale := math.Pow(2, s.Exposure)
	return buildChannelLUT(size, func(value float64, _ int) float64 {
		linear := srgbToLinear(value)
		linear = math.Pow(math.Max(0, linear*scale+s.Offset), 1/s.Gamma)
		return math.Min(1, math.Max(0, linearToSrgb(linear)))
	})
}

// BuildGradientMapTable ports GradientMapSettings.apply's table: 256 entries
// of straight RGB bytes, entry i the endpoint colors interpolated at
// brightness i/255 — highlights first when reversed. Round-half-away per
// channel, as the Swift `channel(from:to:t:)` does.
func BuildGradientMapTable(s domain.GradientMapSettings) []uint8 {
	dark, light := s.Shadows, s.Highlights
	if s.Reversed {
		dark, light = s.Highlights, s.Shadows
	}
	table := make([]uint8, 256*3)
	for i := 0; i < 256; i++ {
		t := float64(i) / 255
		for c, pair := range [...][2]float64{{dark.Red, light.Red}, {dark.Green, light.Green}, {dark.Blue, light.Blue}} {
			v := pair[0] + (pair[1]-pair[0])*t
			table[i*3+c] = uint8(math.Min(255, math.Max(0, math.Round(v*255))))
		}
	}
	return table
}

// buildChannelLUT samples a per-channel curve at Size steps for the three
// color channels.
func buildChannelLUT(size int, apply func(value float64, channel int) float64) *LUT {
	step := float64(size - 1)
	lut := &LUT{Size: size}
	for c := 0; c < 3; c++ {
		lut.Tables[c] = make([]float32, size)
		for i := 0; i < size; i++ {
			lut.Tables[c][i] = float32(apply(float64(i)/step, c))
		}
	}
	return lut
}
