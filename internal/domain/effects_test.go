package domain

import (
	"math"
	"testing"
)

// effectsTestBundle is the fixture LayerEffectsSurviveTests.swift:9 uses:
// stroke 4, shadow distance 20 / blur 10, outer glow 8.
func effectsTestBundle() *Effects {
	return &Effects{
		Stroke:    &StrokeEffect{Size: 4, Red: 1, Opacity: 1},
		Shadow:    &ShadowEffect{Angle: 90, Distance: 20, Blur: 10, Opacity: 0.5},
		OuterGlow: &GlowEffect{Size: 8, Red: 1, Green: 1, Blue: 1, Opacity: 0.75},
	}
}

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Scaled matches LayerEffects.scaled(by:) (LayerEffects.swift:142): every
// present size, distance and blur follows the factor; angle and color do not.
func TestEffectsScaledScalesEveryPresentSize(t *testing.T) {
	scaled := effectsTestBundle().Scaled(0.5)
	if scaled.Stroke == nil || !almostEqual(scaled.Stroke.Size, 2) {
		t.Fatalf("stroke size = %+v, want 2", scaled.Stroke)
	}
	if scaled.Shadow == nil || !almostEqual(scaled.Shadow.Distance, 10) || !almostEqual(scaled.Shadow.Blur, 5) {
		t.Fatalf("shadow = %+v, want distance 10 / blur 5", scaled.Shadow)
	}
	if scaled.Shadow.Angle != 90 {
		t.Fatalf("shadow angle = %v, want 90 (unchanged)", scaled.Shadow.Angle)
	}
	if scaled.OuterGlow == nil || !almostEqual(scaled.OuterGlow.Size, 4) {
		t.Fatalf("outer glow size = %+v, want 4", scaled.OuterGlow)
	}
	// Colors, opacities and the inside flag ride along untouched.
	if scaled.Stroke.Red != 1 || scaled.Stroke.Opacity != 1 {
		t.Fatalf("stroke color/opacity changed: %+v", scaled.Stroke)
	}
	if scaled.Shadow.Opacity != 0.5 || scaled.OuterGlow.Opacity != 0.75 {
		t.Fatalf("opacities changed: %+v %+v", scaled.Shadow, scaled.OuterGlow)
	}
	// The receiver is not mutated (Swift's scaled(by:) copies self).
	if effects := effectsTestBundle(); !almostEqual(effects.Stroke.Size, 4) {
		t.Fatalf("fixture check failed: %+v", effects.Stroke)
	}
}

// Inner shadow scales like the drop shadow; inner glow like the outer glow.
func TestEffectsScaledScalesInnerShadowAndInnerGlow(t *testing.T) {
	effects := &Effects{
		InnerShadow: &ShadowEffect{Angle: 90, Distance: 100, Blur: 40},
		InnerGlow:   &GlowEffect{Size: 30},
	}
	scaled := effects.Scaled(0.25)
	if scaled.InnerShadow == nil || !almostEqual(scaled.InnerShadow.Distance, 25) || !almostEqual(scaled.InnerShadow.Blur, 10) {
		t.Fatalf("inner shadow = %+v, want distance 25 / blur 10", scaled.InnerShadow)
	}
	if scaled.InnerGlow == nil || !almostEqual(scaled.InnerGlow.Size, 7.5) {
		t.Fatalf("inner glow = %+v, want size 7.5", scaled.InnerGlow)
	}
	if scaled.InnerShadow.Angle != 90 {
		t.Fatalf("inner shadow angle = %v, want 90", scaled.InnerShadow.Angle)
	}
}

// min(limit, value*factor) (LayerEffects.swift:143): an enlargement past a
// ceiling stops at the ceiling, never above it.
func TestEffectsScaledClampsToEffectCeilings(t *testing.T) {
	effects := &Effects{
		Stroke:      &StrokeEffect{Size: 400},
		Shadow:      &ShadowEffect{Distance: 4000, Blur: 400},
		InnerShadow: &ShadowEffect{Distance: 4000, Blur: 400},
		OuterGlow:   &GlowEffect{Size: 400},
		InnerGlow:   &GlowEffect{Size: 400},
	}
	scaled := effects.Scaled(4)
	if scaled.Stroke.Size != 500 {
		t.Fatalf("stroke size = %v, want 500 (limit)", scaled.Stroke.Size)
	}
	if scaled.Shadow.Distance != 5000 || scaled.Shadow.Blur != 500 {
		t.Fatalf("shadow = distance %v / blur %v, want 5000 / 500", scaled.Shadow.Distance, scaled.Shadow.Blur)
	}
	if scaled.InnerShadow.Distance != 5000 || scaled.InnerShadow.Blur != 500 {
		t.Fatalf("inner shadow = distance %v / blur %v, want 5000 / 500", scaled.InnerShadow.Distance, scaled.InnerShadow.Blur)
	}
	if scaled.OuterGlow.Size != 500 || scaled.InnerGlow.Size != 500 {
		t.Fatalf("glows = %v / %v, want 500 each", scaled.OuterGlow.Size, scaled.InnerGlow.Size)
	}
	// A ceiling holds even when the scaled value lands exactly on it.
	exact := (&Effects{Stroke: &StrokeEffect{Size: 250}}).Scaled(2)
	if exact.Stroke.Size != 500 {
		t.Fatalf("stroke size = %v, want 500", exact.Stroke.Size)
	}
}

// A nil bundle stays nil, and a missing sub-effect is not invented by scaling.
func TestEffectsScaledKeepsNilSemantics(t *testing.T) {
	var absent *Effects
	if got := absent.Scaled(2); got != nil {
		t.Fatalf("nil effects scaled to %+v, want nil", got)
	}
	scaled := (&Effects{Stroke: &StrokeEffect{Size: 4}}).Scaled(2)
	if scaled.Shadow != nil || scaled.InnerShadow != nil || scaled.OuterGlow != nil ||
		scaled.InnerGlow != nil || scaled.ColorOverlay != nil {
		t.Fatalf("absent sub-effects were invented: %+v", scaled)
	}
	if scaled.Stroke.Size != 8 {
		t.Fatalf("stroke size = %v, want 8", scaled.Stroke.Size)
	}
	// An empty bundle stays empty.
	empty := (&Effects{}).Scaled(2)
	if empty == nil || empty.Stroke != nil || empty.Shadow != nil || empty.ColorOverlay != nil ||
		empty.InnerShadow != nil || empty.OuterGlow != nil || empty.InnerGlow != nil {
		t.Fatalf("empty effects changed: %+v", empty)
	}
}

// Color Overlay has no size, distance or blur, so it passes through untouched
// (Swift's scaled(by:) only writes the fields that exist and have a size).
func TestEffectsScaledLeavesColorOverlayAlone(t *testing.T) {
	overlay := &OverlayEffect{Red: 0.2, Green: 0.4, Blue: 0.6, Opacity: 0.5}
	effects := &Effects{ColorOverlay: overlay}
	scaled := effects.Scaled(3)
	if scaled == effects {
		t.Fatal("Scaled must return a new bundle")
	}
	if scaled.ColorOverlay == nil || *scaled.ColorOverlay != *overlay {
		t.Fatalf("color overlay = %+v, want %+v", scaled.ColorOverlay, overlay)
	}
}

// The result of scaling a valid bundle stays valid for every effect it holds.
func TestEffectsScaledResultStaysValid(t *testing.T) {
	effects := &Effects{
		Stroke:      &StrokeEffect{Size: 400, Opacity: 1},
		Shadow:      &ShadowEffect{Angle: 90, Distance: 4000, Blur: 400, Opacity: 0.5},
		InnerShadow: &ShadowEffect{Angle: 90, Distance: 4000, Blur: 400, Opacity: 0.5},
		OuterGlow:   &GlowEffect{Size: 400, Opacity: 0.75},
		InnerGlow:   &GlowEffect{Size: 400, Opacity: 0.75},
	}
	scaled := effects.Scaled(10)
	if scaled.Stroke.Size > 500 || scaled.Shadow.Distance > 5000 || scaled.Shadow.Blur > 500 ||
		scaled.InnerShadow.Distance > 5000 || scaled.InnerShadow.Blur > 500 ||
		scaled.OuterGlow.Size > 500 || scaled.InnerGlow.Size > 500 {
		t.Fatalf("scaled effects escape their ceilings: %+v", scaled)
	}
}
