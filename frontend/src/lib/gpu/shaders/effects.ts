/**
 * Layer-effect shader passes (ticket 12): the six effects render as pass
 * chains over the layer's coverage channel —
 *   1. coverage: the layer's straight alpha as a grayscale plane,
 *   2. blur: row pass + column pass (separable Gaussian, radius = blur/size),
 *   3. ring composition: stroke/inner effects read the *inverted* coverage so
 *      they paint inside the edge; drop shadow/outer glow paint outside it,
 *   4. colorize: the effect color × the processed coverage × opacity,
 *      composited under/over the layer per effect kind.
 *
 * Parameter shapes mirror internal/domain/effects.go (StrokeEffect,
 * ShadowEffect, OverlayEffect, GlowEffect). Angle semantics: degrees
 * clockwise, 90 = straight down, matching ShadowEffect.
 */

export const EFFECTS_WGSL = /* wgsl */ `
// --- pass 1: coverage -------------------------------------------------
// The effect domain works on straight alpha, not premultiplied color.
fn coverage(rgba: vec4f) -> f32 {
  return rgba.a;
}

// --- pass 2: separable blur -------------------------------------------
// One axis of a Gaussian smoothing of the coverage plane; the compositor
// runs the same pipeline twice with a per-axis radius vector. Weights are
// the 5-tap binomial kernel (1 4 6 4 1)/16, applied at texel steps so the
// two passes compose into the 2D kernel.
fn blurRow(c0: f32, cl: f32, cr: f32, cll: f32, crr: f32) -> f32 {
  return (cll + 4.0 * cl + 6.0 * c0 + 4.0 * cr + crr) / 16.0;
}

fn blurCol(c0: f32, cu: f32, cd: f32, cuu: f32, cdd: f32) -> f32 {
  return (cuu + 4.0 * cu + 6.0 * c0 + 4.0 * cd + cdd) / 16.0;
}

// --- pass 3: ring composition ------------------------------------------
// Stroke/inner effects live on the inside of the edge: their coverage is
// the inverted plane (1 - a) blurred, *minus* the layer's own footprint.
// Drop shadow/outer glow live outside: blurred coverage minus the footprint
// for the glow's spread, or just the offset blurred plane for the shadow.
fn insideRing(blurred: f32, own: f32) -> f32 {
  return clamp(blurred - own, 0.0, 1.0);
}

fn outsideRing(blurred: f32, own: f32) -> f32 {
  return clamp(blurred * (1.0 - own), 0.0, 1.0);
}

// Shadow offset: angle degrees clockwise, 90 = straight down (y-down raster,
// matching the CPU transform convention: dx = cos, dy = sin).
fn shadowOffset(angleDeg: f32, distance: f32) -> vec2f {
  let rad = radians(angleDeg);
  return vec2f(cos(rad), sin(rad)) * distance;
}

// --- pass 4: colorize ---------------------------------------------------
// The effect color under the processed coverage and opacity, premultiplied
// back for compositing.
fn colorize(cov: f32, color: vec3f, opacity: f32) -> vec4f {
  let a = cov * opacity;
  return vec4f(color * a, a);
}
`;

export type EffectKind = "stroke" | "shadow" | "colorOverlay" | "innerShadow" | "outerGlow" | "innerGlow";
