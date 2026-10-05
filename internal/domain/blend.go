package domain

// BlendMode is one of the 24 Photoshop blend modes. The manifest stores the
// raw strings verbatim — spellings are the macOS LayerBlendMode raw values
// and must not be changed (Darker Color / Lighter Color are deliberately
// absent, matching the original).
type BlendMode string

const (
	BlendNormal      BlendMode = "Normal"
	BlendDarken      BlendMode = "Darken"
	BlendMultiply    BlendMode = "Multiply"
	BlendColorBurn   BlendMode = "Color Burn"
	BlendLinearBurn  BlendMode = "Linear Burn"
	BlendLighten     BlendMode = "Lighten"
	BlendScreen      BlendMode = "Screen"
	BlendColorDodge  BlendMode = "Color Dodge"
	BlendLinearDodge BlendMode = "Linear Dodge (Add)"
	BlendOverlay     BlendMode = "Overlay"
	BlendSoftLight   BlendMode = "Soft Light"
	BlendHardLight   BlendMode = "Hard Light"
	BlendVividLight  BlendMode = "Vivid Light"
	BlendLinearLight BlendMode = "Linear Light"
	BlendPinLight    BlendMode = "Pin Light"
	BlendHardMix     BlendMode = "Hard Mix"
	BlendDifference  BlendMode = "Difference"
	BlendExclusion   BlendMode = "Exclusion"
	BlendSubtract    BlendMode = "Subtract"
	BlendDivide      BlendMode = "Divide"
	BlendHue         BlendMode = "Hue"
	BlendSaturation  BlendMode = "Saturation"
	BlendColor       BlendMode = "Color"
	BlendLuminosity  BlendMode = "Luminosity"
)

// AllBlendModes lists every mode in Photoshop order.
var AllBlendModes = []BlendMode{
	BlendNormal,
	BlendDarken,
	BlendMultiply,
	BlendColorBurn,
	BlendLinearBurn,
	BlendLighten,
	BlendScreen,
	BlendColorDodge,
	BlendLinearDodge,
	BlendOverlay,
	BlendSoftLight,
	BlendHardLight,
	BlendVividLight,
	BlendLinearLight,
	BlendPinLight,
	BlendHardMix,
	BlendDifference,
	BlendExclusion,
	BlendSubtract,
	BlendDivide,
	BlendHue,
	BlendSaturation,
	BlendColor,
	BlendLuminosity,
}

var blendModeSet = func() map[BlendMode]struct{} {
	set := make(map[BlendMode]struct{}, len(AllBlendModes))
	for _, m := range AllBlendModes {
		set[m] = struct{}{}
	}
	return set
}()

// ParseBlendMode reports whether s is one of the 24 known modes.
func ParseBlendMode(s string) (BlendMode, bool) {
	mode := BlendMode(s)
	_, ok := blendModeSet[mode]
	return mode, ok
}
