package render

import (
	"math"
	"slices"

	"compositor-win/internal/domain"
)

// Histogram, auto-levels and eyedropper data for the Levels tool, ported
// from LevelsPixels.c (levels_histogram) and Levels.swift /
// LevelsAutomatic.swift (display scaling, auto adjustments, sampling).
// The bins layout matches the C kernel's flat 1024-entry array cut into
// four pages: bins[0] is the composite RGB channel — the mean of the three
// color histograms, not a luminance histogram (Levels.swift:85) — and
// bins[1…3] are red, green, blue.

// LevelsHistogram ports levels_histogram (LevelsPixels.c:17-28): over the
// premultiplied bitmap, each non-transparent pixel contributes its alpha
// as weight, multiplied by the optional selection coverage's byte/255
// (the mask image PixelAdjust.coverage produces, Levels.swift:90-96). The
// straight channel byte — recovered exactly like levels_apply does,
// rounded and clamped to 0-255 — lands in its channel's 256 bins, and the
// same weight/3 lands in the composite RGB bin (LevelsPixels.c:21-26).
// coverage may be nil for the whole layer; when non-empty it is indexed
// per pixel like the C kernel, so it must be W*H bytes (any other length
// is ignored). Deliberately serial: the C kernel accumulates in scan
// order and float addition is not associative, so per-worker bins would
// drift from the golden reference.
func LevelsHistogram(bmp *Bitmap, coverage []uint8) [4][256]float64 {
	var bins [4][256]float64
	useCoverage := len(coverage) == bmp.W*bmp.H
	for i := 0; i < bmp.W*bmp.H; i++ {
		p := bmp.Pix[i*4:]
		alpha := p[3]
		if alpha == 0 {
			continue
		}
		weight := float64(alpha) / 255.0
		if useCoverage {
			weight *= float64(coverage[i]) / 255.0
		}
		for channel := 0; channel < 3; channel++ {
			value := int(math.Min(255, math.Round(float64(p[channel])*255.0/float64(alpha))))
			bins[channel+1][value] += weight
			bins[0][value] += weight / 3.0
		}
	}
	return bins
}

// HistogramScale ports LevelsHistogramDisplay.scale (Levels.swift:48-56),
// the display-only vertical scaling for one channel's 256 bins. Keep the
// linear bin ratios, but cap isolated spikes at four times the 95th
// percentile of the interior bins (first and last excluded), so large
// solid backgrounds cannot flatten the useful tonal distribution.
func HistogramScale(bins []float64) float64 {
	peak := 0.0
	for _, v := range bins {
		if v > peak && !math.IsInf(v, 1) { // isFinite && > 0
			peak = v
		}
	}
	if peak <= 0 {
		return 0
	}
	var interior []float64
	if len(bins) >= 2 { // dropFirst().dropLast()
		for _, v := range bins[1 : len(bins)-1] {
			if v > 0 && !math.IsInf(v, 1) {
				interior = append(interior, v)
			}
		}
	}
	if len(interior) == 0 {
		return peak
	}
	slices.Sort(interior)
	typicalPeak := interior[int(float64(len(interior)-1)*0.95)]
	return math.Min(peak, typicalPeak*4)
}

// AutoLevelsMode names the three auto-levels kinds (LevelsAuto in
// LevelsAutomatic.swift:4-5).
type AutoLevelsMode string

const (
	AutoLevelsContrast AutoLevelsMode = "Contrast"
	AutoLevelsColor    AutoLevelsMode = "Color"
	AutoLevelsNeutral  AutoLevelsMode = "Color + neutral midtones"
)

// histogramEndpoints ports the endpoints() closure inside
// LevelsAuto.settings (LevelsAutomatic.swift:8-16): the 0.1% clip points
// of one channel's bins — the first bin where the running sum from either
// end exceeds 0.1% of the total (0 for low / 255 for high when the sum
// never gets there). ok is false for an empty histogram or when the
// interval would be empty (low >= high).
func histogramEndpoints(bins [256]float64) (low, high float64, ok bool) {
	var total float64
	for _, v := range bins {
		total += v
	}
	if !(total > 0) {
		return 0, 0, false
	}
	sum := 0.0
	l, h := 0, 255
	for i := 0; i < 256; i++ {
		sum += bins[i]
		if sum > total*0.001 {
			l = i
			break
		}
	}
	sum = 0
	for i := 255; i >= 0; i-- {
		sum += bins[i]
		if sum > total*0.001 {
			h = i
			break
		}
	}
	if l < h {
		return float64(l), float64(h), true
	}
	return 0, 0, false
}

// autoLevelRange is LevelRange(black:white:) with the memberwise
// initializer's defaults (Levels.swift:8-13): gamma 1, full 0-255 output.
func autoLevelRange(low, high float64) domain.LevelRange {
	return domain.LevelRange{Black: low, Gamma: 1, White: high, OutputBlack: 0, OutputWhite: 255}
}

// AutoLevels ports LevelsAuto.settings (LevelsAutomatic.swift:6-36): the
// histogram from LevelsHistogram drives new black/white points. Contrast
// stretches only the composite range with the widest interval shared by
// all three channels — "a shared interval preserves channel
// relationships" (LevelsAutomatic.swift:18); Color stretches each channel
// separately; Neutral additionally bends each channel's gamma so the
// weighted mean of its adjusted values lands on 0.5
// (LevelsAutomatic.swift:27-31). Channels without a usable interval stay
// identity, and the settings start from the LevelsSettings() defaults.
func AutoLevels(mode AutoLevelsMode, histogram [4][256]float64) domain.LevelsSettings {
	result := domain.LevelsSettings{
		Channel: domain.LevelsRGB,
		Ranges:  [4]domain.LevelRange{identityLevelRange, identityLevelRange, identityLevelRange, identityLevelRange},
	}
	if mode == AutoLevelsContrast {
		var lows, highs []float64
		for c := 1; c <= 3; c++ {
			if low, high, ok := histogramEndpoints(histogram[c]); ok {
				lows = append(lows, low)
				highs = append(highs, high)
			}
		}
		if len(lows) > 0 {
			low := slices.Min(lows)
			high := slices.Max(highs)
			if low < high {
				result.Ranges[0] = autoLevelRange(low, high)
			}
		}
		return result
	}
	for c := 1; c <= 3; c++ {
		low, high, ok := histogramEndpoints(histogram[c])
		if !ok {
			continue
		}
		r := autoLevelRange(low, high)
		if mode == AutoLevelsNeutral {
			var total float64
			for _, v := range histogram[c] {
				total += v
			}
			var mean float64
			for i, v := range histogram[c] {
				mean += applyLevelRange(r, float64(i)/255) * v
			}
			mean /= total
			if mean > 0 && mean < 1 {
				r.Gamma = math.Min(9.99, math.Max(0.1, math.Log(mean)/math.Log(0.5)))
			}
		}
		result.Ranges[c] = r
	}
	return result
}

// LevelsSampleMode names the three eyedroppers (LevelsSample in
// LevelsAutomatic.swift:3).
type LevelsSampleMode string

const (
	SampleBlack LevelsSampleMode = "Black"
	SampleGray  LevelsSampleMode = "Gray"
	SampleWhite LevelsSampleMode = "White"
)

// SampleLevelsPoint ports the pixel read of EditorSession.sampleLevels
// (LevelsAutomatic.swift:68-83): the eyedropper reads the premultiplied
// layer bitmap at one pixel and returns the straight (unpremultiplied)
// sRGB color, each channel min(1, byte/alpha) in 0-1 — the input
// LevelsSettings.sampling expects. ok is false outside the bitmap (the
// Swift side floors the mapped point and guards it before cropping,
// LevelsAutomatic.swift:71-74) or on a fully transparent pixel (guard
// bytes[3] > 0, LevelsAutomatic.swift:79).
func SampleLevelsPoint(bmp *Bitmap, x, y int) (rgb [3]float64, ok bool) {
	if x < 0 || y < 0 || x >= bmp.W || y >= bmp.H {
		return rgb, false
	}
	p := bmp.Pix[(y*bmp.W+x)*4:]
	alpha := p[3]
	if alpha == 0 {
		return rgb, false
	}
	a := float64(alpha)
	for c := 0; c < 3; c++ {
		rgb[c] = math.Min(1, float64(p[c])/a)
	}
	return rgb, true
}

// ApplyLevelsSample ports LevelsSettings.sampling(_:mode:)
// (LevelsAutomatic.swift:39-60): the black and white eyedroppers move the
// sampled channel's black/white point onto the sample, clamped to keep
// one step of separation from the opposite point; the gray eyedropper
// bends the gamma so the sample maps to 0.5 (fraction = the sample's
// position between the current points; a sample outside (0,1) changes
// nothing for that channel). The composite range resets to identity —
// "all three channels are calibrated together" (LevelsAutomatic.swift:40)
// — and the output range resets to the full 0-255. Each touched range is
// normalized exactly as the Swift version stores it.
func ApplyLevelsSample(settings domain.LevelsSettings, rgb [3]float64, mode LevelsSampleMode) domain.LevelsSettings {
	result := settings
	result.Ranges[0] = identityLevelRange
	for c := 1; c <= 3; c++ {
		r := result.Ranges[c]
		v := rgb[c-1] * 255
		switch mode {
		case SampleBlack:
			r.Black = math.Min(r.White-1, math.Max(0, v))
		case SampleWhite:
			r.White = math.Max(r.Black+1, math.Min(255, v))
		case SampleGray:
			fraction := (v - r.Black) / (r.White - r.Black)
			if !(fraction > 0 && fraction < 1) {
				continue
			}
			r.Gamma = math.Log(fraction) / math.Log(0.5)
		}
		r.OutputBlack = 0
		r.OutputWhite = 255
		result.Ranges[c] = normalizeLevelRange(r)
	}
	return result
}
