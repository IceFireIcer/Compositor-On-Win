package domain

// Effects is the per-layer effect bundle. A missing record means the layer
// has no effects; each sub-effect is independent and may be disabled via
// enabled (missing enabled means visible — old projects rely on that).
type Effects struct {
	Stroke       *StrokeEffect  `json:"stroke,omitempty"`
	Shadow       *ShadowEffect  `json:"shadow,omitempty"`
	ColorOverlay *OverlayEffect `json:"colorOverlay,omitempty"`
	InnerShadow  *ShadowEffect  `json:"innerShadow,omitempty"`
	OuterGlow    *GlowEffect    `json:"outerGlow,omitempty"`
	InnerGlow    *GlowEffect    `json:"innerGlow,omitempty"`
}

// StrokeEffect outlines the layer edge; Inside picks which side of the edge
// it renders on. Size runs 0–500 layer pixels.
type StrokeEffect struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Size    float64 `json:"size"`
	Red     float64 `json:"red"`
	Green   float64 `json:"green"`
	Blue    float64 `json:"blue"`
	Opacity float64 `json:"opacity"`
	Inside  bool    `json:"inside"`
}

// ShadowEffect is the drop-shadow and inner-shadow payload: angle in degrees
// (clockwise, 90 = straight down), distance and blur in layer pixels.
type ShadowEffect struct {
	Enabled  *bool   `json:"enabled,omitempty"`
	Angle    float64 `json:"angle"`
	Distance float64 `json:"distance"`
	Blur     float64 `json:"blur"`
	Red      float64 `json:"red"`
	Green    float64 `json:"green"`
	Blue     float64 `json:"blue"`
	Opacity  float64 `json:"opacity"`
}

// OverlayEffect flat-colors the layer pixels.
type OverlayEffect struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Red     float64 `json:"red"`
	Green   float64 `json:"green"`
	Blue    float64 `json:"blue"`
	Opacity float64 `json:"opacity"`
}

// GlowEffect is the outer/inner glow payload; size runs 0–500 layer pixels.
type GlowEffect struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Size    float64 `json:"size"`
	Red     float64 `json:"red"`
	Green   float64 `json:"green"`
	Blue    float64 `json:"blue"`
	Opacity float64 `json:"opacity"`
}

// Effect size ceilings, mirroring the ranges each effect's isValid accepts
// (LayerEffects.swift: StrokeEffect.maxSize, ShadowEffect.isValid,
// OuterGlowEffect.isValid). Scaled holds its results to them.
const (
	maxStrokeSize     = 500
	maxShadowDistance = 5000
	maxShadowBlur     = 500
	maxGlowSize       = 500
)

// Scaled is LayerEffects.scaled(by:) (LayerEffects.swift:142): the effects for
// an image resampled by factor, with every size, distance and blur in pixels
// scaled with it and held to the ranges isValid accepts, as Photoshop's Scale
// Styles does. A sub-effect that is absent stays absent; only a present one is
// scaled, and its other fields are copied through unchanged. factor is the
// mean of the two axis factors (sqrt(sx*sy)) when the image is stretched.
func (e *Effects) Scaled(factor float64) *Effects {
	if e == nil {
		return nil
	}
	scale := func(value *float64, limit float64) {
		*value = min(limit, *value*factor)
	}
	result := *e
	if result.Stroke != nil {
		stroke := *result.Stroke
		scale(&stroke.Size, maxStrokeSize)
		result.Stroke = &stroke
	}
	if result.Shadow != nil {
		shadow := *result.Shadow
		scale(&shadow.Distance, maxShadowDistance)
		scale(&shadow.Blur, maxShadowBlur)
		result.Shadow = &shadow
	}
	if result.InnerShadow != nil {
		inner := *result.InnerShadow
		scale(&inner.Distance, maxShadowDistance)
		scale(&inner.Blur, maxShadowBlur)
		result.InnerShadow = &inner
	}
	if result.OuterGlow != nil {
		glow := *result.OuterGlow
		scale(&glow.Size, maxGlowSize)
		result.OuterGlow = &glow
	}
	if result.InnerGlow != nil {
		glow := *result.InnerGlow
		scale(&glow.Size, maxGlowSize)
		result.InnerGlow = &glow
	}
	return &result
}
