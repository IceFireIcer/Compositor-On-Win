/**
 * Selection command family (ticket 16), porting the Swift Selection.swift /
 * SelectionClipboard.swift semantics onto the mask representation of
 * selection.ts:
 *  - existence gating: every command is a pure function that returns null
 *    (meaning "no change") when its precondition fails — no selection, no
 *    edit (SelectionEditsTests semantics);
 *  - expand/contract grow/shrink the outline by `amount` pixels with rounded
 *    corners (a round-joined stroke band ±2·amount wide in Swift = chamfer
 *    dilation/erosion here), clipped to the canvas; contracting past the
 *    middle leaves an explicit empty selection;
 *  - feather is a scalar property of the selection, not a mask blur: two
 *    soft edges combine as √(f²+a²), capped at 250, and stack on reapply;
 *  - transform selection reshapes the outline itself and never touches
 *    pixels — the mask resamples through the inverse affine;
 *  - the clipboard is internal: copy/cut take a premultiplied RGBA buffer
 *    plus the selection, cut clears to transparent, paste composites back.
 */
import { outlineLoops, selectionFromMask, type Point, type Selection } from "./selection";

export const EXPAND_CONTRACT_LIMIT = 500;
export const FEATHER_LIMIT = 250;

export function canModifySelection(s: Selection | null): boolean {
  return s !== null && s.active && (s.loops.some((loop) => loop.length >= 3) || s.mask.some((value) => value > 0));
}

/** Select All: the whole canvas. */
export function selectAll(width: number, height: number): Selection {
  return selectionFromMask(new Uint8Array(width * height).fill(255), width, height);
}

/** Deselect, only when a selection exists. */
export function deselect(current: Selection | null): Selection | null {
  return current === null ? null : null;
}

/** Invert: canvas minus the selection. The inverse of everything is no selection at all. */
export function invertSelection(current: Selection): Selection | null {
  const { width, height, mask } = current;
  const inverted = new Uint8Array(width * height);
  let any = false;
  for (let i = 0; i < inverted.length; i++) {
    inverted[i] = 255 - mask[i];
    if (inverted[i] > 0) any = true;
  }
  if (!any) return null; // everything was selected → Photoshop deselects
  return { ...selectionFromMask(inverted, width, height), feather: current.feather ?? 0 };
}

/**
 * Computes one centered sliding maximum/minimum over a line. The window is
 * clipped to the line, so samples outside the canvas do not act as implicit
 * zeroes (or 255s for erosion).
 */
function slidingExtremum(
  source: Uint8Array,
  sourceOffset: number,
  sourceStride: number,
  target: Uint8Array,
  targetOffset: number,
  targetStride: number,
  length: number,
  radius: number,
  findMaximum: boolean,
  deque: Int32Array,
): void {
  let head = 0;
  let tail = 0;

  const append = (index: number): void => {
    const value = source[sourceOffset + index * sourceStride];
    while (tail > head) {
      const previous = deque[tail - 1];
      const previousValue = source[sourceOffset + previous * sourceStride];
      if (findMaximum ? previousValue <= value : previousValue >= value) tail--;
      else break;
    }
    deque[tail++] = index;
  };

  for (let index = 0; index <= Math.min(length - 1, radius); index++) append(index);
  for (let center = 0; center < length; center++) {
    if (center > 0) {
      const first = center - radius;
      while (head < tail && deque[head] < first) head++;
      const incoming = center + radius;
      if (incoming < length) append(incoming);
    }
    target[targetOffset + center * targetStride] = source[sourceOffset + deque[head] * sourceStride];
  }
}

/**
 * Chebyshev dilation/erosion over a grayscale mask. A square window's
 * maximum/minimum is separable, reducing a radius-N morph from O(N²WH) to
 * O(WH) with a monotonic deque on each row and column.
 */
function morphMask(
  mask: Uint8Array,
  width: number,
  height: number,
  radius: number,
  findMaximum: boolean,
): Uint8Array {
  const horizontal = new Uint8Array(mask.length);
  const out = new Uint8Array(mask.length);
  const deque = new Int32Array(Math.max(1, width, height));

  for (let y = 0; y < height; y++) {
    slidingExtremum(mask, y * width, 1, horizontal, y * width, 1, width, radius, findMaximum, deque);
  }
  for (let x = 0; x < width; x++) {
    slidingExtremum(horizontal, x, width, out, x, width, height, radius, findMaximum, deque);
  }
  return out;
}

function morph(
  current: Selection,
  amount: number,
  findMaximum: boolean,
): Selection | null {
  if (
    !canModifySelection(current) ||
    !Number.isInteger(amount) ||
    amount < 1 ||
    amount > EXPAND_CONTRACT_LIMIT
  ) {
    return null;
  }
  const mask = morphMask(current.mask, current.width, current.height, amount, findMaximum);
  const next = selectionFromMask(mask, current.width, current.height);
  const active = mask.some((v) => v > 0);
  return { ...next, active, feather: current.feather ?? 0 };
}

/** Expand Selection: grows the outline by `amount` pixels, clipped to the canvas. */
export function expandSelection(current: Selection | null, amount: number): Selection | null {
  if (current === null || !canModifySelection(current)) return null;
  return morph(current, amount, true);
}

/** Contract Selection: shrinks by `amount`; past the middle → explicit empty. */
export function contractSelection(current: Selection | null, amount: number): Selection | null {
  if (current === null || !canModifySelection(current)) return null;
  return morph(current, amount, false);
}

/**
 * Feather Selection: softens the edge by `amount` — a scalar on the
 * selection (√(f²+a²) ≤ 250), reapplied it stacks, as the Swift comment says.
 */
export function featherSelection(current: Selection | null, amount: number): Selection | null {
  if (current === null || !canModifySelection(current) || !Number.isInteger(amount) || amount < 1 || amount > FEATHER_LIMIT) return null;
  const f = current.feather ?? 0;
  return { ...current, feather: Math.min(FEATHER_LIMIT, Math.sqrt(f * f + amount * amount)) };
}

export interface Affine {
  a: number;
  b: number;
  c: number;
  d: number;
  e: number;
  f: number;
}

/**
 * Transform Selection (⌘T selection mode): the outline itself scales and
 * rotates; pixels never change. The new mask resamples the old one through
 * the inverse affine (nearest, matching the crisp-outline convention).
 */
export function transformSelection(current: Selection | null, t: Affine, width: number, height: number): Selection | null {
  if (current === null || !canModifySelection(current)) return null;
  if (![t.a, t.b, t.c, t.d, t.e, t.f, width, height].every(Number.isFinite) ||
      width < 1 || height < 1 || !Number.isInteger(width) || !Number.isInteger(height)) return null;
  const det = t.a * t.d - t.b * t.c;
  if (Math.abs(det) < 1e-12) return null;
  const mask = new Uint8Array(width * height);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      // inverse affine on the destination pixel center
      const dx = x + 0.5 - t.e;
      const dy = y + 0.5 - t.f;
      const sx = Math.floor((t.d * dx - t.c * dy) / det);
      const sy = Math.floor((-t.b * dx + t.a * dy) / det);
      if (sx < 0 || sy < 0 || sx >= current.width || sy >= current.height) continue;
      const coverage = current.mask[sy * current.width + sx];
      if (coverage > 0) mask[y * width + x] = coverage;
    }
  }
  // Keep the outline in document space even where its rasterized mask is
  // clipped. Raster coverage remains clipped to the canvas; the ants path
  // retains the full transformed boundary.
  const loops = current.loops.map((loop) => loop.map((point) => ({
    x: t.a * point.x + t.c * point.y + t.e,
    y: t.b * point.x + t.d * point.y + t.f,
  })));
  return { ...selectionFromMask(mask, width, height), loops, feather: current.feather ?? 0 };
}

export interface SelectionStamp {
  width: number;
  height: number;
  /** Top-left of the stamp's bounding box in document pixels. */
  x: number;
  y: number;
  /** Premultiplied RGBA of the masked region. */
  pix: Uint8Array;
}

/** Scale one premultiplied byte the same way as the Go raster kernels. */
function scalePremultiplied(value: number, coverage: number): number {
  return Math.floor((value * coverage) / 255);
}

/** Copy: the selected pixels, cropped to the selection's bounding box. */
export function copyStamp(bmp: Uint8Array, width: number, s: Selection | null): SelectionStamp | null {
  if (s === null || !canModifySelection(s)) return null;
  let minX = s.width, minY = s.height, maxX = -1, maxY = -1;
  for (let y = 0; y < s.height; y++) {
    for (let x = 0; x < s.width; x++) {
      if (s.mask[y * s.width + x] > 0) {
        if (x < minX) minX = x;
        if (x > maxX) maxX = x;
        if (y < minY) minY = y;
        if (y > maxY) maxY = y;
      }
    }
  }
  if (maxX < 0) return null;
  const w = maxX - minX + 1;
  const h = maxY - minY + 1;
  const pix = new Uint8Array(w * h * 4);
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const sx = minX + x, sy = minY + y;
      const coverage = s.mask[sy * s.width + sx];
      if (coverage === 0) continue;
      const src = (sy * width + sx) * 4;
      const dst = (y * w + x) * 4;
      if (coverage === 255) {
        pix[dst] = bmp[src];
        pix[dst + 1] = bmp[src + 1];
        pix[dst + 2] = bmp[src + 2];
        pix[dst + 3] = bmp[src + 3];
      } else {
        for (let c = 0; c < 4; c++) pix[dst + c] = scalePremultiplied(bmp[src + c], coverage);
      }
    }
  }
  return { width: w, height: h, x: minX, y: minY, pix };
}

/** Cut: copy, then clear the selected pixels to transparent in place. */
export function cutStamp(
  bmp: Uint8Array,
  width: number,
  s: Selection,
): { stamp: SelectionStamp | null; bmp: Uint8Array } {
  const stamp = copyStamp(bmp, width, s);
  if (!stamp) return { stamp: null, bmp };
  const out = Uint8Array.from(bmp);
  for (let y = 0; y < s.height; y++) {
    for (let x = 0; x < s.width; x++) {
      const coverage = s.mask[y * s.width + x];
      if (coverage === 0) continue;
      const keep = 255 - coverage;
      const i = (y * width + x) * 4;
      if (keep === 0) {
        out[i] = out[i + 1] = out[i + 2] = out[i + 3] = 0;
      } else {
        for (let c = 0; c < 4; c++) out[i + c] = scalePremultiplied(out[i + c], keep);
      }
    }
  }
  return { stamp, bmp: out };
}

/** Paste: composites the stamp over the bitmap at its stored position (Normal). */
export function pasteStamp(stamp: SelectionStamp, bmp: Uint8Array, width: number, height: number): Uint8Array {
  const out = Uint8Array.from(bmp);
  for (let y = 0; y < stamp.height; y++) {
    for (let x = 0; x < stamp.width; x++) {
      const dx = stamp.x + x, dy = stamp.y + y;
      if (dx < 0 || dy < 0 || dx >= width || dy >= height) continue;
      const src = (y * stamp.width + x) * 4;
      const sa = stamp.pix[src + 3] / 255;
      const dst = (dy * width + dx) * 4;
      const da = out[dst + 3] / 255;
      const outA = sa + da * (1 - sa);
      if (outA <= 0) continue;
      for (let c = 0; c < 3; c++) {
        // PDF over on premultiplied values: out = src + dst·(1−sA)
        const v = stamp.pix[src + c] + out[dst + c] * (1 - sa);
        out[dst + c] = Math.min(255, Math.max(0, Math.round(v)));
      }
      out[dst + 3] = Math.round(outA * 255);
    }
  }
  return out;
}

/** The ants' outlines for a selection (re-derived after any mask change). */
export function loopsFor(s: Selection): Point[][] {
  if (!s.active) return [];
  return s.loops.length > 0 ? s.loops : outlineLoops(s.mask, s.width, s.height);
}

/** Marks a contract that erased everything as explicitly empty, not null. */
export function isEmptySelection(s: Selection | null): boolean {
  return s !== null && !canModifySelection(s);
}
