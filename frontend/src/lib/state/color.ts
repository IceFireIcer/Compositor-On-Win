/**
 * Color logic for the eyedropper, swatches and HSV picker — ticket 19's
 * pure layer. All semantics are ported from the macOS original (read-only
 * truth) `reference/Swift/Compositor/Document/ColorPalette.swift`.
 *
 * Fixed dimensions (matching Swift exactly):
 * - RGB: sRGB channels 0...1, straight (non-premultiplied), as Swift
 *   `PaletteColor` (ColorPalette.swift:4-18). Bitmaps themselves are 8-bit
 *   premultiplied RGBA (repo pixel invariant); only `samplePixel` touches
 *   that form, converting to straight on the way out.
 * - HSV: hue in degrees 0...360, saturation and brightness (value) 0...1,
 *   as Swift `PickerHSB` ("Hue in degrees, saturation and brightness
 *   0...1", ColorPalette.swift:302-303).
 */

/** A straight sRGB color, channels 0...1 (Swift PaletteColor). */
export interface Rgb {
  r: number;
  g: number;
  b: number;
}

/** A straight sRGB color with alpha, all channels 0...1. */
export interface Rgba {
  r: number;
  g: number;
  b: number;
  a: number;
}

/** HSV with hue in degrees 0...360, saturation and value 0...1 (Swift PickerHSB). */
export interface Hsv {
  h: number;
  s: number;
  v: number;
}

/** Swift `.rounded()` is round-half-away-from-zero; JS Math.round is half-up. */
function roundHalfAwayFromZero(value: number): number {
  return value < 0 ? -Math.round(-value) : Math.round(value);
}

/**
 * HSV to RGB — a direct port of Swift `PickerHSB.rgb`
 * (ColorPalette.swift:317-332): hue normalized with
 * `(h % 360 + 360) % 360`, then the c/x/m sextant decomposition.
 * Hue outside 0...360 wraps; any saturation/value are accepted as given.
 */
export function hsvToRgb(h: number, s: number, v: number): Rgb {
  const hue = ((((h % 360) + 360) % 360) / 60);
  const c = v * s;
  const x = c * (1 - Math.abs((hue % 2) - 1));
  const m = v - c;
  // Swift switches on Int(h) with a `default` sextant; hue < 6 makes floor
  // the same truncation, so sextant is always 0...5.
  const sextant = Math.floor(hue);
  let r = 0;
  let g = 0;
  let b = 0;
  switch (sextant) {
    case 0:
      r = c;
      g = x;
      break;
    case 1:
      r = x;
      g = c;
      break;
    case 2:
      g = c;
      b = x;
      break;
    case 3:
      g = x;
      b = c;
      break;
    case 4:
      r = x;
      b = c;
      break;
    default:
      r = c;
      b = x;
      break;
  }
  return { r: r + m, g: g + m, b: b + m };
}

/**
 * RGB to HSV — a port of Swift `PickerHSB.setRGB` (ColorPalette.swift:336-349).
 * `previous` carries the picker's working HSV so the documented quirks hold:
 * hue survives dragging through grays (delta == 0 keeps it) and saturation
 * survives black (high == 0 keeps it), "matching how Photoshop's field
 * behaves" (Swift lines 334-341). Without `previous`, achromatic colors fall
 * back to hue 0 / saturation 0. Tie-breaking on the max channel follows
 * Swift's red, then green, then blue branch order. The returned hue is
 * always in 0...360.
 */
export function rgbToHsv(color: Rgb, previous?: Hsv): Hsv {
  const high = Math.max(color.r, color.g, color.b);
  const low = Math.min(color.r, color.g, color.b);
  const delta = high - low;
  const v = high;
  // Swift: `if high > 0 { saturation = delta / high }` — black keeps the
  // previous saturation (and grays compute 0).
  const s = high > 0 ? delta / high : (previous?.s ?? 0);
  // Swift: `guard delta > 0 else { return }` — grays keep the previous hue.
  let h = previous?.h ?? 0;
  if (delta > 0) {
    let raw: number;
    if (high === color.r) raw = (color.g - color.b) / delta;
    else if (high === color.g) raw = (color.b - color.r) / delta + 2;
    else raw = (color.r - color.g) / delta + 4;
    raw *= 60;
    h = raw < 0 ? raw + 360 : raw;
  }
  return { h, s, v };
}

/**
 * Snaps channels to the 8-bit values painting and export actually store —
 * Swift `PaletteColor.quantized` (ColorPalette.swift:353-356):
 * `(v * 255).rounded() / 255`. Input channels are expected in 0...1.
 */
export function quantizeRgb(color: Rgb): Rgb {
  return {
    r: roundHalfAwayFromZero(color.r * 255) / 255,
    g: roundHalfAwayFromZero(color.g * 255) / 255,
    b: roundHalfAwayFromZero(color.b * 255) / 255,
  };
}

/**
 * Serializes as uppercase `RRGGBB` without a leading `#`, from rounded
 * 8-bit channels — Swift `PaletteColor.hex` (ColorPalette.swift:357-359).
 * Input channels are expected in 0...1.
 */
export function rgbToHex(color: Rgb): string {
  const byte = (v: number) => roundHalfAwayFromZero(v * 255);
  const part = (v: number) => byte(v).toString(16).padStart(2, "0").toUpperCase();
  return `${part(color.r)}${part(color.g)}${part(color.b)}`;
}

/**
 * Parses `RRGGBB` or shorthand `RGB`, with or without a leading `#`,
 * whitespace-trimmed; anything else is null — Swift `PaletteColor.init?(hex:)`
 * (ColorPalette.swift:360-367). Digits are case-insensitive (Swift's
 * `UInt32(text, radix: 16)` accepts both).
 */
export function parseHex(hex: string): Rgb | null {
  let text = hex.trim();
  if (text.startsWith("#")) text = text.slice(1);
  if (text.length === 3) {
    text = Array.from(text)
      .map((c) => c + c)
      .join("");
  }
  if (!/^[0-9a-fA-F]{6}$/.test(text)) return null;
  const value = Number.parseInt(text, 16);
  return {
    r: ((value >> 16) & 0xff) / 255,
    g: ((value >> 8) & 0xff) / 255,
    b: (value & 0xff) / 255,
  };
}

/**
 * One pixel of a composited canvas for the eyedropper — the logic side of
 * Swift `EditorSession.sampleCompositeColor(at:)` (ColorPalette.swift:233-254).
 *
 * `pix` is tightly packed 8-bit premultiplied sRGB RGBA with stride
 * `width * 4` (the repo's universal raster form; Swift samples through a
 * premultipliedLast bitmap context). Height is derived from the buffer
 * length. Returns straight (un-premultiplied) 0...1 RGBA; null outside the
 * bitmap or over a fully transparent pixel (Swift guards `pixel[3] > 0`,
 * line 250). Un-premultiplication reproduces Swift line 252 exactly:
 * `(min(alpha, value) / alpha * 255).rounded() / 255` — the min clamp is
 * Swift's own guard against invalid premultiplied data.
 */
export function samplePixel(pix: Uint8Array, width: number, x: number, y: number): Rgba | null {
  if (width <= 0) return null;
  const bytesPerPixel = 4;
  const height = Math.floor(pix.length / (width * bytesPerPixel));
  if (x < 0 || y < 0 || x >= width || y >= height) return null;
  const i = (y * width + x) * bytesPerPixel;
  const alpha = pix[i + 3];
  if (alpha === 0) return null;
  const channel = (value: number) =>
    roundHalfAwayFromZero((Math.min(alpha, value) / alpha) * 255) / 255;
  return {
    r: channel(pix[i]),
    g: channel(pix[i + 1]),
    b: channel(pix[i + 2]),
    a: alpha / 255,
  };
}

// --- Foreground/background swatch state (EditorSession, ColorPalette.swift:20-56) ---

/** The two swatch colors, foreground first as in Swift's session fields. */
export interface PaletteState {
  foreground: Rgb;
  background: Rgb;
}

const BLACK: Rgb = { r: 0, g: 0, b: 0 };
const WHITE: Rgb = { r: 1, g: 1, b: 1 };

function copyRgb(color: Rgb): Rgb {
  return { r: color.r, g: color.g, b: color.b };
}

/**
 * The default palette: foreground black, background white. Matches Swift's
 * defaults — the foreground is `BrushSettings`' black (BrushStroke.swift:13-15)
 * and the background is `PaletteColor.white` (EditorSession.swift:228) — and
 * the D-key reset target (`resetPaletteColors`, ColorPalette.swift:52-56).
 */
export function initialPalette(): PaletteState {
  return { foreground: copyRgb(BLACK), background: copyRgb(WHITE) };
}

/** Sets the foreground swatch (Swift `setPaletteColor(_:background: false)`). */
export function setForeground(palette: PaletteState, color: Rgb): PaletteState {
  return { foreground: copyRgb(color), background: copyRgb(palette.background) };
}

/** Sets the background swatch (Swift `setPaletteColor(_:background: true)`). */
export function setBackground(palette: PaletteState, color: Rgb): PaletteState {
  return { foreground: copyRgb(palette.foreground), background: copyRgb(color) };
}

/**
 * Exchanges the two swatches — Swift `swapPaletteColors`
 * (ColorPalette.swift:43-51), the X key / double-arrow button.
 */
export function swapPalette(palette: PaletteState): PaletteState {
  return { foreground: copyRgb(palette.background), background: copyRgb(palette.foreground) };
}

/**
 * D-key semantics: foreground black, background white — Swift
 * `resetPaletteColors` (ColorPalette.swift:52-56).
 */
export function resetPalette(_palette: PaletteState): PaletteState {
  return initialPalette();
}
