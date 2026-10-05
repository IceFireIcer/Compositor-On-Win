/**
 * GPU adjustment shaders (ticket 12), consuming the LUT/cube data structures
 * built by internal/render/lut.go + cube.go (ticket 10) — one data source,
 * both tracks. Lookup semantics mirror ApplyLUT/ApplyCube, including the
 * top-edge clamp (byte 255 resolves to the last entry) via clamp-to-edge
 * sampling at texel-center-offset coordinates.
 */

export const LUT_WGSL = /* wgsl */ `
// Per-channel 1D LUT: three Size-entry tables (red, green, blue) in one
// RGBA texture (a unused). Entry i is the straight sRGB output for input
// i/(Size-1). Mirrors ApplyLUT: position = x*(Size-1)/255 with the top edge
// clamped to the last texel; the CPU truth builds Size=256, GPU 1024.
@group(0) @binding(0) var lutSampler: sampler;
@group(0) @binding(1) var lutTexture: texture_2d<f32>;

fn applyLUT(c: vec3f) -> vec3f {
  var out: vec3f;
  let channelCoords = vec3f(c.r, c.g, c.b);
  // One texture row per channel; u = position scaled by table size.
  let size = f32(textureDimensions(lutTexture).x);
  let scale = (size - 1.0) / 255.0;
  out.r = textureSampleLevel(lutTexture, lutSampler, vec2f(channelCoords.r * scale + 0.5 / size, 0.5), 0.0).r;
  out.g = textureSampleLevel(lutTexture, lutSampler, vec2f(channelCoords.g * scale + 0.5 / size, 1.5 / size), 0.0).g;
  out.b = textureSampleLevel(lutTexture, lutSampler, vec2f(channelCoords.b * scale + 0.5 / size, 2.5 / size), 0.0).b;
  return out;
}
`;

export const CUBE_WGSL = /* wgsl */ `
// 33³ color cube in a texture_3d with linear filtering and clamp-to-edge.
// Mirrors cube_apply: position = straightByte * (dim-1)/255 per channel;
// sampling at (position + 0.5)/dim puts texel centers at integer lattice
// points, so hardware trilinear reproduces the kernel's interpolation, and
// the byte-255 top edge (position = dim-1) clamps exactly onto the last
// entry — the same boundary semantics the CPU path tests explicitly.
@group(0) @binding(0) var cubeSampler: sampler;
@group(0) @binding(1) var cubeTexture: texture_3d<f32>;

fn applyCube(c: vec3f) -> vec3f {
  let dim = f32(textureDimensions(cubeTexture).x);
  let scale = (dim - 1.0) / 255.0;
  let pos = vec3f(c.r, c.g, c.b) * scale;
  let uvw = (pos + 0.5) / dim;
  return textureSampleLevel(cubeTexture, cubeSampler, uvw, 0.0).rgb;
}
`;
