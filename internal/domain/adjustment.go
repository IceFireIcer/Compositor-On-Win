package domain

import "encoding/json"

// AdjustmentKind is one of the 12 non-destructive adjustment kinds. Raw
// values are the macOS AdjustmentKind spellings; the pixel-sampling kinds
// (Gaussian Blur / Motion Blur / Add Noise) require manifest version 9.
type AdjustmentKind string

const (
	AdjustmentHueSaturation AdjustmentKind = "Hue/Saturation"
	AdjustmentLevels        AdjustmentKind = "Levels"
	AdjustmentCurves        AdjustmentKind = "Curves"
	AdjustmentExposure      AdjustmentKind = "Exposure"
	AdjustmentGradientMap   AdjustmentKind = "Gradient Map"
	AdjustmentGrain         AdjustmentKind = "Grain"
	AdjustmentAddNoise      AdjustmentKind = "Add Noise"
	AdjustmentGaussianBlur  AdjustmentKind = "Gaussian Blur"
	AdjustmentMotionBlur    AdjustmentKind = "Motion Blur"
	AdjustmentInvert        AdjustmentKind = "Invert"
	AdjustmentBlackWhite    AdjustmentKind = "Black & White"
	AdjustmentColorBalance  AdjustmentKind = "Color Balance"
)

// AllAdjustmentKinds lists every kind.
var AllAdjustmentKinds = []AdjustmentKind{
	AdjustmentHueSaturation,
	AdjustmentLevels,
	AdjustmentCurves,
	AdjustmentExposure,
	AdjustmentGradientMap,
	AdjustmentGrain,
	AdjustmentAddNoise,
	AdjustmentGaussianBlur,
	AdjustmentMotionBlur,
	AdjustmentInvert,
	AdjustmentBlackWhite,
	AdjustmentColorBalance,
}

// Adjustment is the adjustment-layer payload. Scalar fields and the
// levels/curves blocks are non-optional in the macOS record and always
// serialize; the per-kind settings blocks are pointers. hsvSettings is kept
// as raw JSON until ticket 26 types the per-range Hue/Saturation model —
// json.RawMessage round-trips it losslessly.
type Adjustment struct {
	Kind        AdjustmentKind  `json:"kind"`
	Hue         float64         `json:"hue"`
	Saturation  float64         `json:"saturation"`
	Lightness   float64         `json:"lightness"`
	Colorize    bool            `json:"colorize"`
	HSVSettings json.RawMessage `json:"hsvSettings,omitempty"`

	Levels LevelsSettings `json:"levels"`
	Curves CurvesSettings `json:"curves"`

	ExposureSettings     *ExposureSettings     `json:"exposureSettings,omitempty"`
	GradientMapSettings  *GradientMapSettings  `json:"gradientMapSettings,omitempty"`
	GrainSettings        *GrainSettings        `json:"grainSettings,omitempty"`
	BlackWhiteSettings   *BlackWhiteSettings   `json:"blackWhiteSettings,omitempty"`
	ColorBalanceSettings *ColorBalanceSettings `json:"colorBalanceSettings,omitempty"`

	// Version 9 pixel-sampling kinds.
	BlurRadius         *float64 `json:"blurRadius,omitempty"`
	MotionAngle        *float64 `json:"motionAngle,omitempty"`
	MotionDistance     *float64 `json:"motionDistance,omitempty"`
	NoiseAmount        *float64 `json:"noiseAmount,omitempty"`
	NoiseGaussian      *bool    `json:"noiseGaussian,omitempty"`
	NoiseMonochromatic *bool    `json:"noiseMonochromatic,omitempty"`
	NoiseSeed          *uint32  `json:"noiseSeed,omitempty"`
}

// LevelsChannel is one of the four channel selectors ("RGB", "Red", "Green",
// "Blue").
type LevelsChannel string

const (
	LevelsRGB   LevelsChannel = "RGB"
	LevelsRed   LevelsChannel = "Red"
	LevelsGreen LevelsChannel = "Green"
	LevelsBlue  LevelsChannel = "Blue"
)

// LevelRange is one channel's input/output mapping
// (black/gamma/white → outputBlack/outputWhite). Identity: 0/1/255/0/255.
type LevelRange struct {
	Black       float64 `json:"black"`
	Gamma       float64 `json:"gamma"`
	White       float64 `json:"white"`
	OutputBlack float64 `json:"outputBlack"`
	OutputWhite float64 `json:"outputWhite"`
}

// LevelsSettings carries the four channel ranges (RGB, then red, green,
// blue).
type LevelsSettings struct {
	Channel LevelsChannel `json:"channel"`
	Ranges  [4]LevelRange `json:"ranges"`
}

// CurvePoint is one curve anchor (x and y in 0…255).
type CurvePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// CurvesSettings carries the four channel point lists (RGB, then red, green,
// blue).
type CurvesSettings struct {
	Channel  LevelsChannel   `json:"channel"`
	Channels [4][]CurvePoint `json:"channels"`
}

// ExposureSettings is exposure in stops, black offset and gamma.
type ExposureSettings struct {
	Exposure float64 `json:"exposure"`
	Offset   float64 `json:"offset"`
	Gamma    float64 `json:"gamma"`
}

// RGB is a plain 0–1 float color triplet (gradient map endpoints).
type RGB struct {
	Red   float64 `json:"red"`
	Green float64 `json:"green"`
	Blue  float64 `json:"blue"`
}

// GradientMapSettings maps luminance between two endpoint colors.
type GradientMapSettings struct {
	Shadows    RGB  `json:"shadows"`
	Highlights RGB  `json:"highlights"`
	Reversed   bool `json:"reversed"`
}

// GrainSettings is the film-grain field: amount (0–100), grain size in
// pixels and roughness, seed-anchored so the pattern is session-stable.
type GrainSettings struct {
	Amount    float64 `json:"amount"`
	Size      float64 `json:"size"`
	Roughness float64 `json:"roughness"`
	Seed      uint32  `json:"seed"`
}

// BlackWhiteSettings carries the six channel weights plus optional tint.
type BlackWhiteSettings struct {
	Reds           float64 `json:"reds"`
	Yellows        float64 `json:"yellows"`
	Greens         float64 `json:"greens"`
	Cyans          float64 `json:"cyans"`
	Blues          float64 `json:"blues"`
	Magentas       float64 `json:"magentas"`
	Tint           bool    `json:"tint"`
	TintHue        float64 `json:"tintHue"`
	TintSaturation float64 `json:"tintSaturation"`
}

// ColorBalanceSettings carries the three tonal-range color shifts (−100…100)
// and the luminosity preservation flag.
type ColorBalanceSettings struct {
	ShadowCyanRed         float64 `json:"shadowCyanRed"`
	ShadowMagentaGreen    float64 `json:"shadowMagentaGreen"`
	ShadowYellowBlue      float64 `json:"shadowYellowBlue"`
	MidCyanRed            float64 `json:"midCyanRed"`
	MidMagentaGreen       float64 `json:"midMagentaGreen"`
	MidYellowBlue         float64 `json:"midYellowBlue"`
	HighlightCyanRed      float64 `json:"highlightCyanRed"`
	HighlightMagentaGreen float64 `json:"highlightMagentaGreen"`
	HighlightYellowBlue   float64 `json:"highlightYellowBlue"`
	PreserveLuminosity    bool    `json:"preserveLuminosity"`
}
