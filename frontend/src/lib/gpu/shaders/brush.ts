/**
 * The brush coverage shader source as a string constant, so vitest can
 * validate it without a bundler pipeline; the compositor passes it to
 * device.createShaderModule verbatim.
 *
 * Every formula is ported formula-by-formula from the semantic truth,
 * reference/Swift/Compositor/Rendering/MetalBrushCoverage.swift (read-only):
 * the continuous-tip coverage kernel — paint deposition integrated by
 * distance travelled with 8-point Gauss-Legendre quadrature clipped to the
 * tip's support, optical density accumulating while coverage is
 * 1 − exp(−density), the density cap at 20 and the permanent/provisional-tail
 * double buffer (brush-performance.md). Hard tips keep the antialiased
 * silhouette; soft coverage is independent of pointer-event count.
 *
 * Port decisions (Metal → WebGPU, semantics unchanged):
 *  - The uchar preview buffer becomes an r8unorm storage texture — one 8-bit
 *    coverage byte per pixel, the same quantization as uchar(round(255·v)).
 *  - The MSL buffer indices carry over as bindings 0–3.
 *  - Metal's INFINITY seed becomes f32's finite maximum (3.4e38): sqrt and
 *    the coverage clamps read it as "no segment", so the results are exact.
 *  - The dispatch shape is unchanged: one dispatch per 256 px tile, uniforms
 *    carrying the tile's document origin (MetalBrushCoverage.render).
 */

/** One dispatch's uniforms, the 80-byte layout of BrushUniforms in the WGSL. */
export interface BrushUniforms {
  /** 2×2 pixel→document transform, row-major as Metal's CGAffineTransform (a, b, c, d). */
  mapping: [number, number, number, number];
  /** Tile origin in document space, tip radius (diameter / 2), tip hardness (0–1). */
  geometry: [number, number, number, number];
  /** Canvas width/height, antialias width, deposition spacing. */
  canvas: [number, number, number, number];
  /** Tile width/height, committed segment count, total segment count. */
  counts: [number, number, number, number];
}

/** sizeof(BrushUniforms): four 16-byte-aligned vectors. */
export const BRUSH_UNIFORMS_SIZE = 80;

/**
 * Soft-tip deposition rate, shared with the continuous GPU integral —
 * BrushStroke.spacingFraction (BrushStroke.swift:462): soft tips deposit at
 * 2.5% of the diameter and hard ones at 1.5%.
 */
export function spacingFraction(hardness: number): number {
  return hardness >= 1 ? 0.015 : 0.025;
}

/**
 * Deposition spacing in document pixels — the uniform's canvas.w
 * (MetalBrushCoverage.swift:50, BrushStroke.swift:466): the spacing floor
 * keeps degenerate diameters from dividing by zero.
 */
export function depositionSpacing(diameter: number, hardness: number): number {
  return Math.max(0.25, diameter * spacingFraction(hardness));
}

/**
 * Builds one dispatch's uniforms, mirroring the Uniforms construction in
 * MetalBrushCoverage.render (MetalBrushCoverage.swift:47-51): radius is
 * diameter / 2, the antialias width is the smaller mapping axis length with a
 * 0.001 floor (never divide by zero at extreme scales), and the deposition
 * spacing follows the hardness-derived rate.
 */
export function brushUniforms(params: {
  mapping: [number, number, number, number];
  originX: number;
  originY: number;
  tileWidth: number;
  tileHeight: number;
  diameter: number;
  hardness: number;
  canvasWidth: number;
  canvasHeight: number;
  settledCount: number;
  totalCount: number;
}): BrushUniforms {
  const [a, b, c, d] = params.mapping;
  return {
    mapping: params.mapping,
    geometry: [params.originX, params.originY, params.diameter / 2, params.hardness],
    canvas: [
      params.canvasWidth,
      params.canvasHeight,
      Math.max(0.001, Math.min(Math.hypot(a, b), Math.hypot(c, d))),
      depositionSpacing(params.diameter, params.hardness),
    ],
    counts: [params.tileWidth, params.tileHeight, params.settledCount, params.totalCount],
  };
}

/**
 * TS mirror of the WGSL const arrays below — the eight-point Gauss-Legendre
 * nodes/weights ported from MetalBrushCoverage.swift:121-122. The test suite
 * pins this mirror against the shader source so the two cannot drift apart.
 */
export const GAUSS_LEGENDRE_NODES = [0.1834346425, 0.5255324099, 0.7966664774, 0.9602898565];
export const GAUSS_LEGENDRE_WEIGHTS = [0.3626837834, 0.3137066459, 0.2223810345, 0.1012285363];

export const BRUSH_WGSL = /* wgsl */ `
struct BrushUniforms {
  mapping: vec4f,   // a, b, c, d
  geometry: vec4f,  // document origin of tile, radius, hardness
  canvas: vec4f,    // width, height, antialias width, deposition spacing
  counts: vec4u,    // tile width, height, committed segment count, total segment count
};

// Binding 0: accumulated permanent paint, one f32 per tile pixel — optical
// density for soft tips (capped at 20), coverage for hard tips.
@group(0) @binding(0) var<storage, read_write> permanent: array<f32>;
// Binding 1: the 8-bit grayscale preview the compositor shows mid-stroke; the
// WebGPU spelling of Metal's uchar buffer (r8unorm quantizes like round(255·v)).
@group(0) @binding(1) var previewTex: texture_storage_2d<r8unorm, write>;
@group(0) @binding(2) var<uniform> u: BrushUniforms;
// Binding 3: the dispatch's segments (x0, y0, x1, y1) — committed pieces first,
// then the provisional tail.
@group(0) @binding(3) var<storage> segments: array<vec4f>;

fn segmentDistanceSquared(p: vec2f, segment: vec4f) -> f32 {
  let v = segment.zw - segment.xy;
  let t = clamp(dot(p - segment.xy, v) / max(dot(v, v), 1e-12), 0.0, 1.0);
  let delta = p - (segment.xy + t * v);
  return dot(delta, delta);
}

fn brushCoverage(distanceSquared: f32, u: BrushUniforms) -> f32 {
  let distance = sqrt(distanceSquared);
  let radius = u.geometry.z;
  if (u.geometry.w >= 1.0) {
    return clamp((radius - distance) / u.canvas.z + 0.5, 0.0, 1.0);
  }
  let t = clamp((distance / radius - u.geometry.w) / (1.0 - u.geometry.w), 0.0, 1.0);
  return max(0.0, (exp(-2.5 * t * t) - exp(-2.5)) / (1.0 - exp(-2.5)));
}

// Integrate paint deposition by distance travelled, not pointer-event count or
// spline subdivision count. Optical density adds; coverage is 1 - exp(-density).
// This is the continuous form of source-over soft dabs at the shared deposition spacing.
fn tipDensity(distanceSquared: f32, u: BrushUniforms) -> f32 {
  return -log(max(1.0 - brushCoverage(distanceSquared, u), 0.001));
}

fn segmentDensity(p: vec2f, segment: vec4f, u: BrushUniforms) -> f32 {
  let v = segment.zw - segment.xy;
  let segmentLength = length(v);
  if (segmentLength < 1e-6) { return tipDensity(dot(p - segment.xy, p - segment.xy), u); } // initial click
  let direction = v / segmentLength;
  let projection = dot(p - segment.xy, direction);
  let perpendicular = p - segment.xy - projection * direction;
  let perpendicularSquared = dot(perpendicular, perpendicular);
  let radiusSquared = u.geometry.z * u.geometry.z;
  if (perpendicularSquared >= radiusSquared) { return 0.0; }
  let reach = sqrt(radiusSquared - perpendicularSquared);
  let lo = max(0.0, projection - reach);
  let hi = min(segmentLength, projection + reach);
  if (hi <= lo) { return 0.0; }
  let midpoint = (lo + hi) * 0.5;
  let halfLength = (hi - lo) * 0.5;
  // Eight-point Gauss-Legendre quadrature, clipped to the tip's support.
  // Long sparse events and short dense events produce the same paint coverage.
  const gaussNodes = array<f32, 4>(0.1834346425, 0.5255324099, 0.7966664774, 0.9602898565);
  const gaussWeights = array<f32, 4>(0.3626837834, 0.3137066459, 0.2223810345, 0.1012285363);
  var integral = 0.0;
  for (var i = 0u; i < 4u; i++) {
    let a = midpoint - halfLength * gaussNodes[i] - projection;
    let b = midpoint + halfLength * gaussNodes[i] - projection;
    integral += gaussWeights[i] * (tipDensity(perpendicularSquared + a * a, u)
                                 + tipDensity(perpendicularSquared + b * b, u));
  }
  return integral * halfLength / u.canvas.w;
}

// Permanent paint and the replaceable tail are separate. Tail previews are never
// accumulated into permanent paint, including at self-intersections.
@compute @workgroup_size(16, 16)
fn continuousBrush(@builtin(global_invocation_id) pixel: vec3u) {
  if (pixel.x >= u.counts.x || pixel.y >= u.counts.y) { return; }
  let index = pixel.y * u.counts.x + pixel.x;
  let local = vec2f(f32(pixel.x) + 0.5, f32(pixel.y) + 0.5);
  let p = u.geometry.xy + local.x * u.mapping.xy + local.y * u.mapping.zw;
  if (any(p < vec2f(0.0)) || any(p >= u.canvas.xy)) {
    textureStore(previewTex, pixel.xy, vec4f(0.0));
    return;
  }
  if (u.geometry.w >= 1.0) {
    // Hard tips already have a solid interior. Preserve pixel-edge antialiasing.
    // Metal seeds these minima with INFINITY; 3.4e38 is f32's finite stand-in —
    // sqrt and the coverage clamps read it as "no segment".
    var settled = 3.4e38;
    var tail = 3.4e38;
    for (var i = 0u; i < u.counts.z; i++) { settled = min(settled, segmentDistanceSquared(p, segments[i])); }
    for (var i = u.counts.z; i < u.counts.w; i++) { tail = min(tail, segmentDistanceSquared(p, segments[i])); }
    let value = max(permanent[index], brushCoverage(settled, u));
    permanent[index] = value;
    textureStore(previewTex, pixel.xy, vec4f(max(value, brushCoverage(tail, u))));
  } else {
    var value = permanent[index];
    var tail = 0.0;
    for (var i = 0u; i < u.counts.z; i++) { value = value + segmentDensity(p, segments[i], u); }
    for (var i = u.counts.z; i < u.counts.w; i++) { tail = tail + segmentDensity(p, segments[i], u); }
    permanent[index] = min(value, 20.0);
    textureStore(previewTex, pixel.xy, vec4f(1.0 - exp(-min(value + tail, 20.0))));
  }
}
`;
