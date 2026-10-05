package domain

import "testing"

// The 24 spellings must match the macOS LayerBlendMode raw values exactly —
// the .comp manifest stores them verbatim (writing-comp-files.md "Blend modes
// are spelled exactly").
func TestBlendModeSpellings(t *testing.T) {
	want := []BlendMode{
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
	if len(AllBlendModes) != len(want) {
		t.Fatalf("AllBlendModes has %d entries, want %d", len(AllBlendModes), len(want))
	}
	for i, mode := range want {
		if AllBlendModes[i] != mode {
			t.Fatalf("AllBlendModes[%d] = %q, want %q", i, AllBlendModes[i], mode)
		}
	}
	if got := string(BlendLinearDodge); got != "Linear Dodge (Add)" {
		t.Fatalf(`Linear Dodge spelling = %q, want "Linear Dodge (Add)"`, got)
	}
	if got := string(BlendColorBurn); got != "Color Burn" {
		t.Fatalf(`Color Burn spelling = %q, want "Color Burn"`, got)
	}
}

func TestBlendModeRoundTrip(t *testing.T) {
	for _, mode := range AllBlendModes {
		parsed, ok := ParseBlendMode(string(mode))
		if !ok || parsed != mode {
			t.Fatalf("ParseBlendMode(%q) = %q, %v", mode, parsed, ok)
		}
	}
	if _, ok := ParseBlendMode("Darker Color"); ok {
		t.Fatal("Darker Color is deliberately omitted in the original — must not parse")
	}
}

func TestAdjustmentKinds(t *testing.T) {
	want := []AdjustmentKind{
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
	if len(AllAdjustmentKinds) != len(want) {
		t.Fatalf("AllAdjustmentKinds has %d entries, want %d", len(AllAdjustmentKinds), len(want))
	}
	for i, kind := range want {
		if AllAdjustmentKinds[i] != kind {
			t.Fatalf("AllAdjustmentKinds[%d] = %q, want %q", i, AllAdjustmentKinds[i], kind)
		}
	}
}

func TestEffectAndShapeAndSamplingSpellings(t *testing.T) {
	if SamplingHighQuality != Sampling("High quality") || SamplingSmooth != Sampling("Smooth") || SamplingNearest != Sampling("Nearest") {
		t.Fatal("sampling spellings diverge from the original")
	}
	for _, kind := range []ShapeKind{ShapeRectangle, ShapeEllipse, ShapeLine} {
		if kind != "Rectangle" && kind != "Ellipse" && kind != "Line" {
			t.Fatalf("unexpected shape kind %q", kind)
		}
	}
	if TextAlignmentLeft != TextAlignment("Left") || TextAlignmentCenter != TextAlignment("Center") || TextAlignmentRight != TextAlignment("Right") {
		t.Fatal("text alignment spellings diverge from the original")
	}
	if GuideAxisHorizontal != GuideAxis("horizontal") || GuideAxisVertical != GuideAxis("vertical") {
		t.Fatal("guide axis spellings diverge from the original")
	}
}
