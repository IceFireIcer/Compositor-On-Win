// Package domain holds the document model and its invariants.
// Ticket 02 starts with the document limits; the full model lands in
// ticket 04 around these constants.
package domain

import "fmt"

// DocumentLimits, ported from the macOS DocumentLimits:
// max side 30,000px, max single surface 200M pixels,
// resolution 1–9600ppi (manifest range), default 72ppi.
const (
	MaxSide          = 30_000
	MaxSurfacePixels = 200_000_000
	MinResolution    = 1
	MaxResolution    = 9600
)

// DefaultResolution is used when the new-canvas form leaves it empty.
const DefaultResolution = 72

// LimitsError is a user-facing validation failure; its message is shown
// directly in the new-canvas form.
type LimitsError struct{ msg string }

func (e *LimitsError) Error() string { return e.msg }

func limitsErrf(format string, args ...any) *LimitsError {
	return &LimitsError{msg: fmt.Sprintf(format, args...)}
}

// ValidateNewDocument checks a new-canvas request against the document
// limits. Sides are checked before the surface product so absurd inputs
// report 边长 rather than overflowing.
func ValidateNewDocument(width, height, resolution int) *LimitsError {
	if width < 1 || height < 1 {
		return limitsErrf("宽高必须为正数（得到 %d × %d）", width, height)
	}
	if width > MaxSide {
		return limitsErrf("边长超限：宽 %d 超过 %d px", width, MaxSide)
	}
	if height > MaxSide {
		return limitsErrf("边长超限：高 %d 超过 %d px", height, MaxSide)
	}
	if pixels := width * height; pixels > MaxSurfacePixels {
		return limitsErrf("像素超限：%d × %d = %.1fM，超过 %.0fM 上限",
			width, height, float64(pixels)/1e6, float64(MaxSurfacePixels)/1e6)
	}
	if resolution < MinResolution || resolution > MaxResolution {
		return limitsErrf("分辨率超出 %d–%d ppi", MinResolution, MaxResolution)
	}
	return nil
}
