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
