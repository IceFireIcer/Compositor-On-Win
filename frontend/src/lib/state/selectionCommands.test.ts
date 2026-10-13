import { describe, expect, it } from "vitest";
import { selectionFromMask, type Selection } from "./selection";
import {
  contractSelection,
  copyStamp,
  cutStamp,
  deselect,
  expandSelection,
  featherSelection,
  invertSelection,
  isEmptySelection,
  pasteStamp,
  selectAll,
  transformSelection,
} from "./selectionCommands";

function rect(w: number, h: number, x0: number, y0: number, x1: number, y1: number): Selection {
  const mask = new Uint8Array(w * h);
  for (let y = y0; y <= y1; y++) for (let x = x0; x <= x1; x++) mask[y * w + x] = 255;
  return selectionFromMask(mask, w, h);
}

function solidBitmap(w: number, h: number, r: number, g: number, b: number): Uint8Array {
  const pix = new Uint8Array(w * h * 4);
  for (let i = 0; i < w * h; i++) {
    pix[i * 4] = r;
    pix[i * 4 + 1] = g;
    pix[i * 4 + 2] = b;
    pix[i * 4 + 3] = 255;
  }
  return pix;
}

function countMask(s: Selection | null): number {
  if (!s) return 0;
  let n = 0;
  for (const v of s.mask) if (v === 255) n++;
  return n;
}

function bruteMorph(
  mask: Uint8Array,
  width: number,
  height: number,
  radius: number,
  mode: "max" | "min",
): Uint8Array {
  const out = new Uint8Array(mask.length);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      let value = mode === "max" ? 0 : 255;
      for (let dy = -radius; dy <= radius; dy++) {
        for (let dx = -radius; dx <= radius; dx++) {
          const nx = x + dx;
          const ny = y + dy;
          if (nx < 0 || ny < 0 || nx >= width || ny >= height) continue;
          const sample = mask[ny * width + nx];
          value = mode === "max" ? Math.max(value, sample) : Math.min(value, sample);
        }
      }
      out[y * width + x] = value;
    }
  }
  return out;
}

describe("selection existence gating", () => {
  it("deselect only fires when a selection exists", () => {
    expect(deselect(null)).toBeNull();
    expect(deselect(rect(4, 4, 0, 0, 1, 1))).toBeNull(); // null = no selection
  });

  it("expand/contract/copy ignore missing or empty selections", () => {
    expect(expandSelection(null, 2)).toBeNull();
    const empty = { width: 4, height: 4, mask: new Uint8Array(16), loops: [], active: false };
    expect(expandSelection(empty, 2)).toBeNull();
    expect(copyStamp(new Uint8Array(64), 4, empty)).toBeNull();
  });

  it("treats a zero-coverage loaded mask as explicitly empty and refuses boundary edits", () => {
    const empty = selectionFromMask(new Uint8Array(16), 4, 4);
    expect(isEmptySelection(empty)).toBe(true);
    expect(expandSelection(empty, 2)).toBeNull();
    expect(contractSelection(empty, 2)).toBeNull();
    expect(featherSelection(empty, 2)).toBeNull();
    expect(transformSelection(empty, { a: 1, b: 0, c: 0, d: 1, e: 0, f: 0 }, 4, 4)).toBeNull();
  });

  it("selectAll covers the canvas; invert of everything deselects", () => {
    const all = selectAll(4, 4);
    expect(countMask(all)).toBe(16);
    expect(invertSelection(all)).toBeNull();
    expect(deselect(all)).toBeNull();
  });

  it("invert complements within the canvas and keeps feather", () => {
    const inv = invertSelection(rect(4, 4, 0, 0, 1, 1));
    expect(inv).not.toBeNull();
    expect(countMask(inv)).toBe(12);
    expect(inv?.feather).toBe(0);
  });

  it("invert complements partial coverage instead of binarizing it", () => {
    const mask = new Uint8Array([128, 0]);
    const current = selectionFromMask(mask, 2, 1);
    const inv = invertSelection(current);
    expect(inv?.mask).toEqual(new Uint8Array([127, 255]));
  });
});

describe("expand / contract / feather", () => {
  it("rejects non-finite or fractional feather amounts without corrupting the selection", () => {
    const selection = rect(4, 4, 1, 1, 2, 2);
    for (const amount of [Number.NaN, Infinity, 1.5]) expect(featherSelection(selection, amount)).toBeNull();
    expect(selection.feather).toBeUndefined();
  });
  it("uses grayscale max/min coverage instead of binarizing soft masks", () => {
    const partial = selectionFromMask(new Uint8Array([128]), 1, 1);
    const expanded = expandSelection(partial, 1);
    const contracted = contractSelection(partial, 1);
    expect(expanded?.mask).toEqual(new Uint8Array([128]));
    expect(expanded?.active).toBe(true);
    expect(contracted?.mask).toEqual(new Uint8Array([128]));
    expect(contracted?.active).toBe(true);

    const grown = expandSelection(selectionFromMask(new Uint8Array([0, 64, 0]), 3, 1), 1);
    expect(grown?.mask).toEqual(new Uint8Array([64, 64, 64]));
    const shrunk = contractSelection(selectionFromMask(new Uint8Array([128, 64, 128]), 3, 1), 1);
    expect(shrunk?.mask).toEqual(new Uint8Array([64, 64, 64]));
  });

  it("matches the brute-force Chebyshev max/min reference on randomized gray masks", () => {
    let seed = 0x51e1ec7;
    const nextByte = (): number => {
      seed = (seed * 1664525 + 1013904223) >>> 0;
      return (seed >>> 24) & 0xff;
    };
    const width = 5;
    const height = 4;
    for (let sample = 0; sample < 12; sample++) {
      const mask = Uint8Array.from({ length: width * height }, () => nextByte());
      mask[0] = 128; // Keep every generated input visibly soft and active.
      const radius = (sample % 3) + 1;
      const current = selectionFromMask(mask, width, height);
      const expanded = expandSelection(current, radius);
      const contracted = contractSelection(current, radius);
      expect(expanded?.mask).toEqual(bruteMorph(mask, width, height, radius, "max"));
      expect(contracted?.mask).toEqual(bruteMorph(mask, width, height, radius, "min"));
      expect(expanded?.mask.every(Number.isFinite)).toBe(true);
      expect(contracted?.mask.every(Number.isFinite)).toBe(true);
    }
  });

  it("validates integer amounts and ignores outside-canvas samples", () => {
    const partial = selectionFromMask(new Uint8Array([128]), 1, 1);
    for (const amount of [0, -1, 1.5, Number.NaN, Number.POSITIVE_INFINITY, 501]) {
      expect(expandSelection(partial, amount)).toBeNull();
      expect(contractSelection(partial, amount)).toBeNull();
    }
    const all = selectAll(2, 2);
    expect(contractSelection(all, 500)?.mask).toEqual(new Uint8Array([255, 255, 255, 255]));
  });

  it("expand grows the outline by amount on every side", () => {
    // 8-connected dilation = Chebyshev metric: a square core grows to a
    // square; the Swift round-join corner softening is a mask-domain
    // approximation documented in the module header.
    const grown = expandSelection(rect(10, 10, 4, 4, 5, 5), 2);
    expect(countMask(grown)).toBe(36); // 6×6
    expect(grown?.mask[4 * 10 + 2]).toBe(255); // grew 2 left of the core
  });

  it("expand clips to the canvas", () => {
    const grown = expandSelection(rect(6, 6, 0, 0, 1, 1), 3);
    expect(grown?.mask[0]).toBe(255);
    expect(grown?.mask[5 * 6 + 5]).toBe(0); // chamfer-exceeding far corner stays out
    expect(countMask(grown)).toBe(25); // Chebyshev: 5×5 window
  });

  it("contract shrinks by amount and can leave an explicit empty selection", () => {
    const shrunk = contractSelection(rect(10, 10, 2, 2, 7, 7), 2);
    expect(countMask(shrunk)).toBe(4); // 6×6 eroded 2 per side → 2×2
    const gone = contractSelection(rect(10, 10, 2, 2, 7, 7), 5);
    expect(gone).not.toBeNull();
    expect(gone?.active).toBe(false); // explicit empty, not null
  });

  it("feather stacks as √(f²+a²) capped at 250", () => {
    let s = rect(8, 8, 1, 1, 5, 5);
    s = featherSelection(s, 30)!;
    expect(s?.feather).toBeCloseTo(30, 9);
    s = featherSelection(s, 40)!;
    expect(s?.feather).toBeCloseTo(50, 9); // √(900+1600)
    s = featherSelection(s, 250)!;
    expect(s?.feather).toBe(250); // capped
    expect(featherSelection(s, 0)).toBeNull(); // amount must be ≥1
    expect(featherSelection(s, 251)).toBeNull(); // menu gating rejects >250
  });
});

describe("transform selection", () => {
  it("retains feather and the unclipped outline when transforming beyond the canvas", () => {
    const s = { ...selectionFromMask(new Uint8Array([128, 0, 0, 0]), 2, 2), feather: 4 };
    const moved = transformSelection(s, { a: 1, b: 0, c: 0, d: 1, e: 3, f: 0 }, 2, 2)!;
    expect(moved.feather).toBe(4);
    expect(moved.loops).toHaveLength(1);
    expect(moved.loops.flat().every((p) => p.x >= 3)).toBe(true);
    expect(moved.mask).toEqual(new Uint8Array(4));
    expect(s.mask[0]).toBe(128);
  });

  it("rejects non-finite affine coefficients instead of replacing the selection with an empty mask", () => {
    const s = selectionFromMask(new Uint8Array([255]), 1, 1);
    expect(transformSelection(s, { a: 1, b: 0, c: 0, d: 1, e: Number.NaN, f: 0 }, 1, 1)).toBeNull();
  });
  it("reshapes the outline without touching pixels", () => {
    const s = rect(8, 8, 2, 2, 5, 5);
    const moved = transformSelection(s, { a: 1, b: 0, c: 0, d: 1, e: 2, f: 1 }, 8, 8);
    expect(countMask(moved)).toBe(16);
    // the 4×4 block now starts at (4,3)
    expect(moved?.mask[3 * 8 + 4]).toBe(255);
    expect(moved?.mask[2 * 8 + 2]).toBe(0);
  });

  it("rejects singular affines and missing selections", () => {
    expect(transformSelection(null, { a: 1, b: 0, c: 0, d: 1, e: 0, f: 0 }, 8, 8)).toBeNull();
    expect(transformSelection(rect(8, 8, 0, 0, 1, 1), { a: 0, b: 0, c: 0, d: 0, e: 0, f: 0 }, 8, 8)).toBeNull();
  });

  it("keeps partial coverage when the selection is transformed", () => {
    const current = selectionFromMask(new Uint8Array([128]), 1, 1);
    const moved = transformSelection(current, { a: 1, b: 0, c: 0, d: 1, e: 0, f: 0 }, 1, 1);
    expect(moved?.mask).toEqual(new Uint8Array([128]));
  });
});

describe("selection clipboard", () => {
  it("copy crops to the bounding box of selected pixels", () => {
    const bmp = solidBitmap(6, 6, 200, 100, 50);
    const stamp = copyStamp(bmp, 6, rect(6, 6, 2, 1, 4, 3));
    expect(stamp?.width).toBe(3);
    expect(stamp?.height).toBe(3);
    expect(stamp?.x).toBe(2);
    expect(stamp?.y).toBe(1);
    expect(stamp?.pix[0]).toBe(200);
  });

  it("copies partial coverage inside the crop and scales premultiplied pixels", () => {
    const bmp = solidBitmap(4, 3, 200, 100, 50);
    const mask = new Uint8Array(12);
    mask[1 * 4 + 2] = 128;
    const stamp = copyStamp(bmp, 4, selectionFromMask(mask, 4, 3));
    expect(stamp).toMatchObject({ width: 1, height: 1, x: 2, y: 1 });
    expect(stamp?.pix).toEqual(new Uint8Array([100, 50, 25, 128]));
  });

  it("cut clears the selected pixels to transparent; paste composites back", () => {
    const bmp = solidBitmap(6, 6, 200, 100, 50);
    const { stamp, bmp: cut } = cutStamp(bmp, 6, rect(6, 6, 1, 1, 2, 2));
    expect(cut[0]).toBe(200); // outside the selection: untouched
    expect(cut[(1 * 6 + 1) * 4 + 3]).toBe(0); // inside: transparent
    const restored = pasteStamp(stamp!, cut, 6, 6);
    expect(restored[(1 * 6 + 1) * 4]).toBe(200);
    expect(restored[(1 * 6 + 1) * 4 + 3]).toBe(255);
    expect(restored[(1 * 6 + 1) * 4 + 1]).toBe(100);
  });

  it("cut attenuates premultiplied pixels by the remaining soft-mask coverage", () => {
    const bmp = solidBitmap(1, 1, 200, 100, 50);
    const selection = selectionFromMask(new Uint8Array([128]), 1, 1);
    const { stamp, bmp: cut } = cutStamp(bmp, 1, selection);
    expect(stamp?.pix).toEqual(new Uint8Array([100, 50, 25, 128]));
    expect(cut).toEqual(new Uint8Array([99, 49, 24, 127]));
  });

  it("paste clips the stamp at canvas edges", () => {
    const bmp = new Uint8Array(4 * 4 * 4);
    const stamp = { width: 2, height: 2, x: 3, y: 3, pix: new Uint8Array(2 * 2 * 4).fill(255) };
    const out = pasteStamp(stamp, bmp, 4, 4);
    expect(out[(3 * 4 + 3) * 4 + 3]).toBe(255); // only the top-left stamp pixel lands
  });

  it("cut with no usable selection is a no-op", () => {
    const bmp = solidBitmap(4, 4, 1, 2, 3);
    const { stamp, bmp: out } = cutStamp(bmp, 4, rect(4, 4, 0, 0, 0, 0));
    expect(stamp).not.toBeNull();
    expect(out).not.toBe(bmp);
    const empty = { width: 4, height: 4, mask: new Uint8Array(16), loops: [], active: false };
    expect(cutStamp(bmp, 4, empty).stamp).toBeNull();
  });

  it("isEmptySelection distinguishes explicit-empty from null", () => {
    expect(isEmptySelection(null)).toBe(false);
    const contracted = contractSelection(rect(6, 6, 1, 1, 2, 2), 5);
    expect(isEmptySelection(contracted)).toBe(true);
  });
});
