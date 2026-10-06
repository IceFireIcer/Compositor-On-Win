package render

import "compositor-win/internal/domain"

// Adjustment dispatch for the CPU truth path, mirroring how the macOS layer
// pipeline applies LayerAdjustment (LayerAdjustment.apply, and the GPU
// layer path in GPUAdjustment.apply). Each supported kind generates its
// lookup once per settings and applies it to the whole raster in place.
// The per-pixel kernels (Grain, Add Noise, Black & White, Color Balance,
// Invert) live in kernels.go; the kinds without a kernel yet (the blurs)
// still pass through untouched until their ticket lands. An adjustment
// whose per-kind settings block was never stored renders untouched — the
// document carries no values to apply (Swift's editor supplies defaults
// only once the user edits, and the render path's pass-through contract
// pins this).

// applyAdjustment adjusts `b` in place; it reports whether the kind has a
// kernel yet, so callers can pass unsupported kinds through untouched.
func applyAdjustment(a *domain.Adjustment, b *Bitmap) bool {
	switch a.Kind {
	case domain.AdjustmentLevels:
		// LevelsFilter.run leaves an identity settings image untouched.
		if levelsIdentity(a.Levels) {
			return true
		}
		ApplyLUT(b, BuildLevelsLUT(a.Levels, 256))
		return true
	case domain.AdjustmentCurves:
		ApplyLUT(b, BuildCurvesLUT(a.Curves, 256))
		return true
	case domain.AdjustmentExposure:
		ApplyLUT(b, BuildExposureLUT(exposureOrDefault(a), 256))
		return true
	case domain.AdjustmentHueSaturation:
		ApplyCube(b, BuildHueSaturationCube(a))
		return true
	case domain.AdjustmentGradientMap:
		ApplyGradientMap(b, BuildGradientMapTable(gradientMapOrDefault(a)))
		return true
	case domain.AdjustmentGrain:
		if a.GrainSettings == nil {
			return true
		}
		s := *a.GrainSettings
		// Whole-raster apply: origin zero, one unit per pixel — the Swift
		// GrainSettings.apply defaults, so the pattern is document-anchored
		// at 1:1.
		ApplyGrain(b, s.Amount, s.Size, s.Roughness, s.Seed, 0, 0, 1)
		return true
	case domain.AdjustmentAddNoise:
		if a.NoiseAmount == nil {
			return true
		}
		gaussian, monochromatic := false, false
		if a.NoiseGaussian != nil {
			gaussian = *a.NoiseGaussian
		}
		if a.NoiseMonochromatic != nil {
			monochromatic = *a.NoiseMonochromatic
		}
		var seed uint32
		if a.NoiseSeed != nil {
			seed = *a.NoiseSeed
		}
		ApplyAddNoise(b, float32(*a.NoiseAmount), gaussian, monochromatic, seed)
		return true
	case domain.AdjustmentBlackWhite:
		if a.BlackWhiteSettings == nil {
			return true
		}
		s := *a.BlackWhiteSettings
		// The Swift caller (BlackWhiteSettings.apply) divides by 100 before
		// the kernel, in its order: red, yellow, green, cyan, blue, magenta.
		var weights [6]float32
		for i, v := range [6]float64{s.Reds, s.Yellows, s.Greens, s.Cyans, s.Blues, s.Magentas} {
			weights[i] = float32(v / 100)
		}
		ApplyBlackWhite(b, weights, s.Tint, s.TintHue, s.TintSaturation/100)
		return true
	case domain.AdjustmentColorBalance:
		if a.ColorBalanceSettings == nil {
			return true
		}
		s := *a.ColorBalanceSettings
		// The Swift caller divides the −100…100 sliders by 100, in its
		// all-order: shadows, midtones, highlights (cyan-red, magenta-green,
		// yellow-blue each).
		scaled := func(r, g, bl float64) [3]float32 {
			return [3]float32{float32(r / 100), float32(g / 100), float32(bl / 100)}
		}
		shadows := scaled(s.ShadowCyanRed, s.ShadowMagentaGreen, s.ShadowYellowBlue)
		midtones := scaled(s.MidCyanRed, s.MidMagentaGreen, s.MidYellowBlue)
		highlights := scaled(s.HighlightCyanRed, s.HighlightMagentaGreen, s.HighlightYellowBlue)
		// ColorBalanceSettings.apply leaves an all-zero shift untouched.
		if shadows == [3]float32{} && midtones == [3]float32{} && highlights == [3]float32{} {
			return true
		}
		ApplyColorBalance(b, shadows, midtones, highlights, s.PreserveLuminosity)
		return true
	case domain.AdjustmentInvert:
		ApplyInvert(b)
		return true
	default:
		return false
	}
}

// exposureOrDefault ports LayerAdjustment.exposure: the stored settings or
// the Swift struct's defaults (identity gamma).
func exposureOrDefault(a *domain.Adjustment) domain.ExposureSettings {
	if a.ExposureSettings != nil {
		return *a.ExposureSettings
	}
	return domain.ExposureSettings{Exposure: 0, Offset: 0, Gamma: 1}
}

// gradientMapOrDefault ports LayerAdjustment.gradientMap: the stored
// settings or the Swift struct's defaults (black shadows, white highlights).
func gradientMapOrDefault(a *domain.Adjustment) domain.GradientMapSettings {
	if a.GradientMapSettings != nil {
		return *a.GradientMapSettings
	}
	return domain.GradientMapSettings{Shadows: domain.RGB{}, Highlights: domain.RGB{Red: 1, Green: 1, Blue: 1}}
}
