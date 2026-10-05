/**
 * The blend/composite shader source as a string constant, so vitest can
 * validate it without a bundler pipeline; the compositor passes it to
 * device.createShaderModule verbatim.
 *
 * Every formula mirrors internal/render/blend.go exactly — sRGB semantics
 * (deliberately not linear-light), ADR-0004: the PDF 32000 formulas for the
 * 16 separable modes, Photoshop's standard formulas for the 8 extensions,
 * and the PDF non-separable Hue/Saturation/Color/Luminosity via SetLum/
 * SetSat/ClipColor. The composite equation is BlendPixel's.
 */

export const BLEND_WGSL = /* wgsl */ `
struct Uniforms {
  mode: u32,
  opacity: f32,
  pad0: f32,
  pad1: f32,
};

@group(0) @binding(0) var dstSampler: sampler;
@group(0) @binding(1) var dstTexture: texture_2d<f32>;
@group(0) @binding(2) var srcSampler: sampler;
@group(0) @binding(3) var srcTexture: texture_2d<f32>;
@group(0) @binding(4) var maskSampler: sampler;
@group(0) @binding(5) var maskTexture: texture_2d<f32>;
@group(0) @binding(6) var<uniform> uniforms: Uniforms;

fn clamp01(v: f32) -> f32 {
  return min(1.0, max(0.0, v));
}

// vividLight unclamped, shared by HardMix.
fn vividLight(d: f32, s: f32) -> f32 {
  if (s <= 0.0) { return 0.0; }
  if (s < 0.5) { return 1.0 - (1.0 - d) / (2.0 * s); }
  if (s >= 1.0) { return 1.0; }
  return d / (2.0 * (1.0 - s));
}

// PDF soft-light's D(d).
fn pinLightD(d: f32) -> f32 {
  if (d <= 0.25) { return ((16.0 * d - 12.0) * d + 4.0) * d; }
  return sqrt(d);
}

// blendChannel mirrors BlendChannel for the 20 separable modes (Normal plus
// the 19 blends that reduce to per-channel math). Non-separable modes are
// handled pixel-wise in blendPixel.
fn blendChannel(mode: u32, d: f32, s: f32) -> f32 {
  switch mode {
    case 0u:  { return s; }                                  // Normal
    case 1u:  { return min(d, s); }                          // Darken
    case 2u:  { return d * s; }                              // Multiply
    case 3u:  {                                              // Color Burn
      if (s <= 0.0) { return 0.0; }
      return clamp01(1.0 - (1.0 - d) / s);
    }
    case 4u:  { return clamp01(d + s - 1.0); }               // Linear Burn
    case 5u:  { return max(d, s); }                          // Lighten
    case 6u:  { return d + s - d * s; }                      // Screen
    case 7u:  {                                              // Color Dodge
      if (s >= 1.0) { return 1.0; }
      return clamp01(d / (1.0 - s));
    }
    case 8u:  { return clamp01(d + s); }                     // Linear Dodge (Add)
    case 9u:  {                                              // Overlay
      if (d <= 0.5) { return 2.0 * d * s; }
      return 1.0 - 2.0 * (1.0 - d) * (1.0 - s);
    }
    case 10u: {                                              // Soft Light
      if (s <= 0.5) { return d - (1.0 - 2.0 * s) * d * (1.0 - d); }
      return d + (2.0 * s - 1.0) * (pinLightD(d) - d);
    }
    case 11u: {                                              // Hard Light
      if (s <= 0.5) { return 2.0 * s * d; }
      return 1.0 - 2.0 * (1.0 - s) * (1.0 - d);
    }
    case 12u: {                                              // Vivid Light
      if (s <= 0.0) { return 0.0; }
      if (s < 0.5) { return clamp01(1.0 - (1.0 - d) / (2.0 * s)); }
      if (s >= 1.0) { return 1.0; }
      return clamp01(d / (2.0 * (1.0 - s)));
    }
    case 13u: { return clamp01(d + 2.0 * s - 1.0); }         // Linear Light
    case 14u: {                                              // Pin Light
      if (s < 0.5) { return min(d, 2.0 * s); }
      return max(d, 2.0 * s - 1.0);
    }
    case 15u: {                                              // Hard Mix
      if (vividLight(d, s) < 0.5) { return 0.0; }
      return 1.0;
    }
    case 16u: { return abs(d - s); }                         // Difference
    case 17u: { return d + s - 2.0 * d * s; }                // Exclusion
    case 18u: { return clamp01(d - s); }                     // Subtract
    case 19u: {                                              // Divide
      if (s <= 0.0) { return 1.0; }
      return clamp01(d / s);
    }
    default:  { return s; }                                  // unreachable for separable modes
  }
}

// Non-separable helpers (PDF 32000 11.3.5), mirroring lum/clipColor/setLum/
// sat/setSat in blend.go.
fn lum(c: vec3f) -> f32 {
  return 0.3 * c.r + 0.59 * c.g + 0.11 * c.b;
}

fn clipColor(c: vec3f) -> vec3f {
  let l = lum(c);
  var r = c.r; var g = c.g; var b = c.b;
  let n = min(r, min(g, b));
  let x = max(r, max(g, b));
  if (n < 0.0) {
    r = l + (r - l) * l / (l - n);
    g = l + (g - l) * l / (l - n);
    b = l + (b - l) * l / (l - n);
  }
  if (x > 1.0) {
    r = l + (r - l) * (1.0 - l) / (x - l);
    g = l + (g - l) * (1.0 - l) / (x - l);
    b = l + (b - l) * (1.0 - l) / (x - l);
  }
  return vec3f(r, g, b);
}

fn setLum(c: vec3f, l: f32) -> vec3f {
  return clipColor(c + (l - lum(c)));
}

fn sat(c: vec3f) -> f32 {
  return max(c.r, max(c.g, c.b)) - min(c.r, min(c.g, c.b));
}

fn setSat(c: vec3f, s: f32) -> vec3f {
  var comp = array<f32, 3u>(c.r, c.g, c.b);
  var minIdx = 0u;
  var maxIdx = 0u;
  for (var i: u32 = 1u; i < 3u; i++) {
    if (comp[i] < comp[minIdx]) { minIdx = i; }
    if (comp[i] > comp[maxIdx]) { maxIdx = i; }
  }
  let midIdx = 3u - minIdx - maxIdx;
  var out = array<f32, 3u>(0.0, 0.0, 0.0);
  if (comp[maxIdx] > comp[minIdx]) {
    out[midIdx] = (comp[midIdx] - comp[minIdx]) * s / (comp[maxIdx] - comp[minIdx]);
    out[maxIdx] = s;
  }
  return vec3f(out[0], out[1], out[2]);
}

// blendPixel mirrors BlendPixel: non-separable modes reduce pixel-wise, then
// every mode runs the PDF composite equation on straight colors.
fn blendPixel(mode: u32, dC: vec3f, dA: f32, sC: vec3f, sA: f32) -> vec3f {
  var blended: vec3f;
  if (mode == 20u) {                 // Hue: sat of source, lum of backdrop
    blended = setLum(setSat(sC, sat(dC)), lum(dC));
  } else if (mode == 21u) {          // Saturation: sat of source, hue+lum of backdrop
    blended = setLum(setSat(dC, sat(sC)), lum(dC));
  } else if (mode == 22u) {          // Color: hue+sat of source, lum of backdrop
    blended = setLum(sC, lum(dC));
  } else if (mode == 23u) {          // Luminosity: lum of source, hue+sat of backdrop
    blended = setLum(dC, lum(sC));
  } else {
    blended = vec3f(blendChannel(mode, dC.r, sC.r), blendChannel(mode, dC.g, sC.g), blendChannel(mode, dC.b, sC.b));
  }
  let outA = sA + dA * (1.0 - sA);
  if (outA <= 0.0) { return vec3f(0.0); }
  // blendWeighted: (sA*Blended + dA*(1-sA)*Cd) / outA
  return (sA * blended + dA * (1.0 - sA) * dC) / outA;
}

@fragment
fn main(@location(0) uv: vec2f) -> @location(0) vec4f {
  let d = textureSample(dstTexture, dstSampler, uv);
  let s = textureSample(srcTexture, srcSampler, uv);
  // Mask semantics follow maskCoverage: coverage is the mask's red channel,
  // not its alpha; the source fades to nothing where coverage is 0.
  let mask = textureSample(maskTexture, maskSampler, uv);
  let opacity = uniforms.opacity * mask.r;
  let sA = s.a * opacity;
  let dA = d.a;
  if (sA <= 0.0) { return d; }
  // Straight colors: the textures store premultiplied RGBA8, so unpremultiply
  // before the formulas and re-premultiply after, exactly like BlendBitmap.
  var dC = vec3f(0.0);
  if (dA > 0.0) { dC = d.rgb / d.a; }
  let sC = s.rgb / max(s.a, 0.0001);
  let outC = blendPixel(uniforms.mode, dC, dA, sC, sA);
  let outA = sA + dA * (1.0 - sA);
  return vec4f(outC * outA, outA);
}
`;
