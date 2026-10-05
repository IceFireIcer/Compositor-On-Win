package render

import "compositor-win/internal/domain"

// Adjustment dispatch for the CPU truth path, mirroring how the macOS layer
// pipeline applies LayerAdjustment (LayerAdjustment.apply, and the GPU
// layer path in GPUAdjustment.apply). Each supported kind generates its
// lookup once per settings and applies it to the whole raster in place.
// The M5 kernel kinds (Grain, Add Noise, blurs, and the per-pixel Black &
// White / Color Balance / Invert) pass through untouched until their
// tickets land.

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
