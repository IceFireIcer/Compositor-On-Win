import { describe, expect, it } from "vitest";
import {
  hsvToRgb,
  parseHex,
  quantizeRgb,
  rgbToHex,
  rgbToHsv,
  samplePixel,
  initialPalette,
  setForeground,
  setBackground,
  swapPalette,
  resetPalette,
  type Hsv,
  type Rgb,
} from "./color";

const closeTo = (actual: number, expected: number, eps = 1e-6) =>
  expect(Math.abs(actual - expected)).toBeLessThanOrEqual(eps);

const expectRgb = (actual: Rgb, r: number, g: number, b: number, eps = 1e-6) => {
  closeTo(actual.r, r, eps);
  closeTo(actual.g, g, eps);
  closeTo(actual.b, b, eps);
};

describe("hsvToRgb (PickerHSB.rgb, ColorPalette.swift:317-332)", () => {
  it("maps the six primary/secondary hues", () => {
    // c = v*s with s=v=1, so m = 0 and each sextant is a pure pair.
    expectRgb(hsvToRgb(0, 1, 1), 1, 0, 0); // red
    expectRgb(hsvToRgb(60, 1, 1), 1, 1, 0); // yellow
    expectRgb(hsvToRgb(120, 1, 1), 0, 1, 0); // green
    expectRgb(hsvToRgb(180, 1, 1), 0, 1, 1); // cyan
    expectRgb(hsvToRgb(240, 1, 1), 0, 0, 1); // blue
    expectRgb(hsvToRgb(300, 1, 1), 1, 0, 1); // magenta
  });

  it("black is v=0 whatever the hue and saturation; white is s=0, v=1", () => {
    expectRgb(hsvToRgb(217, 0.8, 0), 0, 0, 0);
    expectRgb(hsvToRgb(999, 0, 1), 1, 1, 1);
  });

  it("scales brightness and saturation linearly (c = v*s, m = v - c)", () => {
    // h=210, s=0.5, v=0.8: sextant 3 (0,x,c) with c=0.4, x=0.2, m=0.4.
    expectRgb(hsvToRgb(210, 0.5, 0.8), 0.4, 0.6, 0.8);
  });

  it("normalizes hue outside 0...360 ((h % 360 + 360) % 360, Swift line 318)", () => {
    expectRgb(hsvToRgb(360, 1, 1), 1, 0, 0);
    expectRgb(hsvToRgb(420, 1, 1), 1, 1, 0);
    expectRgb(hsvToRgb(-60, 1, 1), 1, 0, 1);
  });
});

describe("rgbToHsv (PickerHSB.setRGB, ColorPalette.swift:336-349)", () => {
  it("recovers hue, saturation and value of chromatic colors", () => {
    const hsb = rgbToHsv({ r: 0.4, g: 0.6, b: 0.8 });
    closeTo(hsb.h, 210);
    closeTo(hsb.s, 0.5);
    closeTo(hsb.v, 0.8);
  });

  it("keeps the previous hue for grays and the previous saturation for black (Swift lines 334-341)", () => {
    // Gray: delta = 0, so hue stays as passed in; saturation collapses to 0.
    const gray = rgbToHsv({ r: 0.5, g: 0.5, b: 0.5 }, { h: 123, s: 0.9, v: 0.4 });
    closeTo(gray.h, 123);
    closeTo(gray.s, 0);
    closeTo(gray.v, 0.5);
    // Black: high = 0, so saturation too keeps its previous value.
    const black = rgbToHsv({ r: 0, g: 0, b: 0 }, { h: 300, s: 0.8, v: 0.2 });
    closeTo(black.h, 300);
    closeTo(black.s, 0.8);
    closeTo(black.v, 0);
    // Without a previous HSV, achromatic colors fall back to hue 0, saturation 0.
    const bare = rgbToHsv({ r: 0.5, g: 0.5, b: 0.5 });
    expect(bare).toEqual({ h: 0, s: 0, v: 0.5 });
  });

  it("breaks hue ties in Swift's red-then-green-then-blue order", () => {
    // r == g > b: Swift checks `high == red` first, giving hue 60, not 120.
    closeTo(rgbToHsv({ r: 1, g: 1, b: 0.5 }).h, 60);
    // g == b > r: green branch wins, hue 180.
    closeTo(rgbToHsv({ r: 0.5, g: 1, b: 1 }).h, 180);
  });

  it("returns hue in 0...360 (negative h gets +360, Swift line 348)", () => {
    const hsb = rgbToHsv({ r: 1, g: 0, b: 0.2 });
    expect(hsb.h).toBeGreaterThanOrEqual(0);
    expect(hsb.h).toBeLessThan(360);
    closeTo(hsb.h, 348); // (0 - 0.2) / 1 * 60 + 360
  });
});

describe("HSV -> RGB -> HSV round trip (ticket 19 acceptance)", () => {
  const hues = [0, 15, 45, 60, 90, 120, 179.9, 180, 240, 300, 359.9];
  const sats = [0.01, 0.25, 0.5, 0.75, 1];
  const vals = [0.01, 0.5, 1];

  it("returns every chromatic HSV value unchanged within tolerance", () => {
    for (const h of hues) {
      for (const s of sats) {
        for (const v of vals) {
          const rgb = hsvToRgb(h, s, v);
          // The picker passes the working HSV as `previous`, so hue and
          // saturation survive even where RGB alone cannot carry them.
          const back = rgbToHsv(rgb, { h, s, v });
          closeTo(back.h, h);
          closeTo(back.s, s);
          closeTo(back.v, v);
        }
      }
    }
  });

  it("the gray axis round-trips value exactly; hue is a convention, not a loss", () => {
    for (const v of [0, 0.25, 0.5, 1]) {
      const rgb = hsvToRgb(123, 0, v);
      expectRgb(rgb, v, v, v);
      const back = rgbToHsv(rgb, { h: 123, s: 0, v });
      closeTo(back.v, v);
      closeTo(back.s, 0);
      closeTo(back.h, 123); // preserved from previous, as the picker needs
    }
  });

  it("RGB -> HSV -> RGB always reproduces the original color, grays included", () => {
    const samples: Rgb[] = [
      { r: 0, g: 0, b: 0 },
      { r: 1, g: 1, b: 1 },
      { r: 0.5, g: 0.5, b: 0.5 },
      { r: 0.9, g: 0.1, b: 0.3 },
      { r: 0.2, g: 0.7, b: 0.4 },
      { r: 1, g: 0, b: 0.2 },
      { r: 128 / 255, g: 64 / 255, b: 255 / 255 },
    ];
    for (const rgb of samples) {
      const hsb: Hsv = rgbToHsv(rgb);
      const back = hsvToRgb(hsb.h, hsb.s, hsb.v);
      expectRgb(back, rgb.r, rgb.g, rgb.b, 1e-9);
    }
  });
});

describe("8-bit quantize and hex (PaletteColor extensions, ColorPalette.swift:352-368)", () => {
  it("quantizeRgb snaps to stored 8-bit values ((v*255).rounded()/255)", () => {
    expect(quantizeRgb({ r: 0.5, g: 0, b: 1 })).toEqual({ r: 128 / 255, g: 0, b: 1 });
    expect(quantizeRgb({ r: 0.001, g: 0.999, b: 0.5019607843137255 })).toEqual({
      r: 0,
      g: 1,
      b: 128 / 255,
    });
  });

  it("rgbToHex serializes uppercase RRGGBB without '#' from rounded 8-bit channels", () => {
    expect(rgbToHex({ r: 1, g: 0, b: 0 })).toBe("FF0000");
    expect(rgbToHex({ r: 0.5, g: 1 / 3, b: 0.2 })).toBe("805533");
    expect(rgbToHex({ r: 0, g: 0, b: 0 })).toBe("000000");
  });

  it("parseHex accepts RRGGBB or shorthand RGB, with or without '#', trimmed", () => {
    expect(parseHex("FF0000")).toEqual({ r: 1, g: 0, b: 0 });
    expect(parseHex("ff0000")).toEqual({ r: 1, g: 0, b: 0 }); // radix-16 parse is case-insensitive
    expect(parseHex("#00FF7F")).toEqual({ r: 0, g: 1, b: 127 / 255 });
    expect(parseHex("  #0F8 ")).toEqual({ r: 0, g: 1, b: 136 / 255 });
    expect(parseHex("abc")).toEqual({ r: 170 / 255, g: 187 / 255, b: 204 / 255 });
  });

  it("parseHex returns null for anything that is not 6 (or 3) hex digits", () => {
    expect(parseHex("")).toBeNull();
    expect(parseHex("12345")).toBeNull();
    expect(parseHex("1234567")).toBeNull();
    expect(parseHex("zzzzzz")).toBeNull();
    expect(parseHex("#gg0000")).toBeNull();
    expect(parseHex("ff00 00")).toBeNull();
  });

  it("hex then parse is an identity on quantized colors", () => {
    for (const rgb of [
      { r: 0.5, g: 1 / 3, b: 0.2 },
      { r: 1, g: 0, b: 0 },
      { r: 0, g: 0, b: 0 },
    ] as Rgb[]) {
      expect(parseHex(rgbToHex(rgb))).toEqual(quantizeRgb(rgb));
    }
  });
});

describe("samplePixel (sampleCompositeColor, ColorPalette.swift:233-254)", () => {
  // 2x3 premultiplied RGBA buffer, tightly packed (bytesPerRow = width * 4).
  const w = 2;
  const pix = new Uint8Array([
    // row 0: opaque red, half-alpha red
    255, 0, 0, 255, 128, 0, 0, 128,
    // row 1: transparent black (alpha 0), quarter-alpha mixed color
    0, 0, 0, 0, 64, 32, 0, 128,
    // row 2: opaque white, premultiplied clamped edge (value == alpha)
    255, 255, 255, 255, 255, 255, 255, 255,
  ]);

  it("returns the straight (un-premultiplied) 0-1 RGBA of an opaque pixel", () => {
    expect(samplePixel(pix, w, 0, 0)).toEqual({ r: 1, g: 0, b: 0, a: 1 });
    expect(samplePixel(pix, w, 0, 2)).toEqual({ r: 1, g: 1, b: 1, a: 1 });
  });

  it("un-premultiplies with Swift's formula: (min(a, v) / a * 255).rounded() / 255", () => {
    // 128,0,0 @ a=128: 128/128*255 = 255 -> r = 1; a = 128/255.
    expect(samplePixel(pix, w, 1, 0)).toEqual({ r: 1, g: 0, b: 0, a: 128 / 255 });
    // 64,32,0 @ a=128: r = round(127.5)/255 = 128/255, g = round(63.75)/255 = 64/255.
    expect(samplePixel(pix, w, 1, 1)).toEqual({ r: 128 / 255, g: 64 / 255, b: 0, a: 128 / 255 });
  });

  it("returns null over fully transparent pixels (Swift guard pixel[3] > 0)", () => {
    expect(samplePixel(pix, w, 0, 1)).toBeNull();
  });

  it("returns null outside the bitmap (Swift's document-bounds guard)", () => {
    expect(samplePixel(pix, w, -1, 0)).toBeNull();
    expect(samplePixel(pix, w, 0, -1)).toBeNull();
    expect(samplePixel(pix, w, 2, 0)).toBeNull();
    expect(samplePixel(pix, w, 0, 3)).toBeNull();
  });

  it("the sampled color equals the composited pixel: hex of the sample matches the stored 8-bit color", () => {
    // Eyedropper invariant: what you sample is what the pixel stores.
    const sample = samplePixel(pix, w, 1, 1);
    expect(sample).not.toBeNull();
    expect(rgbToHex({ r: sample!.r, g: sample!.g, b: sample!.b })).toBe("804000");
  });
});

describe("foreground/background palette (EditorSession, ColorPalette.swift:20-56)", () => {
  const red: Rgb = { r: 1, g: 0, b: 0 };
  const blue: Rgb = { r: 0, g: 0.2, b: 1 };

  it("starts foreground black, background white (BrushStroke.swift:13-15, EditorSession.swift:228)", () => {
    expect(initialPalette()).toEqual({
      foreground: { r: 0, g: 0, b: 0 },
      background: { r: 1, g: 1, b: 1 },
    });
  });

  it("setForeground and setBackground replace one side and copy the color", () => {
    let p = setForeground(initialPalette(), red);
    expect(p.foreground).toEqual(red);
    expect(p.background).toEqual({ r: 1, g: 1, b: 1 });
    p = setBackground(p, blue);
    expect(p.foreground).toEqual(red);
    expect(p.background).toEqual(blue);
    // Copies, so later mutation of the argument cannot alias into the state.
    const mutable: Rgb = { r: 1, g: 1, b: 0 };
    const q = setForeground(p, mutable);
    mutable.r = 0;
    expect(q.foreground).toEqual({ r: 1, g: 1, b: 0 });
  });

  it("swap exchanges the two (swapPaletteColors, ColorPalette.swift:43-51; X key)", () => {
    const p = setBackground(setForeground(initialPalette(), red), blue);
    const swapped = swapPalette(p);
    expect(swapped.foreground).toEqual(blue);
    expect(swapped.background).toEqual(red);
    // Swapping twice restores the original.
    expect(swapPalette(swapped)).toEqual(p);
  });

  it("reset returns to black/white (resetPaletteColors, ColorPalette.swift:52-56; D key)", () => {
    const p = setBackground(setForeground(initialPalette(), red), blue);
    expect(resetPalette(p)).toEqual(initialPalette());
  });
});
