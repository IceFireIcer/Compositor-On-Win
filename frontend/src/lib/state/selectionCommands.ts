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
  return s !== null && s.active;
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
    if (mask[i] === 0) {
      inverted[i] = 255;
      any = true;
    }
  }
  if (!any) return null; // everything was selected → Photoshop deselects
  return { ...selectionFromMask(inverted, width, height), feather: current.feather ?? 0 };
}

/** One round of 8-connected dilation (rounded corners, as a round-joined stroke). */
function dilate(mask: Uint8Array, width: number, height: number): Uint8Array {
  const out = new Uint8Array(mask.length);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const i = y * width + x;
      if (mask[i] === 255) {
        out[i] = 255;
        continue;
      }
      let near = false;
      for (let dy = -1; dy <= 1 && !near; dy++) {
        for (let dx = -1; dx <= 1; dx++) {
          const nx = x + dx, ny = y + dy;
          if (nx < 0 || ny < 0 || nx >= width || ny >= height) continue;
          if (mask[ny * width + nx] === 255) {
            near = true;
            break;
          }
        }
      }
      out[i] = near ? 255 : 0;
    }
  }
  return out;
}

function erode(mask: Uint8Array, width: number, height: number): Uint8Array {
  const inverted = new Uint8Array(mask.length);
  for (let i = 0; i < mask.length; i++) inverted[i] = mask[i] === 255 ? 0 : 255;
  const dilated = dilate(inverted, width, height);
  const out = new Uint8Array(mask.length);
  for (let i = 0; i < mask.length; i++) out[i] = dilated[i] === 255 ? 0 : 255;
  return out;
}

function morph(
  current: Selection,
  amount: number,
  step: (m: Uint8Array) => Uint8Array,
): Selection | null {
  if (!canModifySelection(current) || amount < 1 || amount > EXPAND_CONTRACT_LIMIT) return null;
  let mask = current.mask;
  for (let i = 0; i < amount; i++) mask = step(mask);
  const next = selectionFromMask(mask, current.width, current.height);
  const active = mask.some((v) => v === 255);
  return { ...next, active, feather: current.feather ?? 0 };
}

/** Expand Selection: grows the outline by `amount` pixels, clipped to the canvas. */
export function expandSelection(current: Selection | null, amount: number): Selection | null {
  if (current === null || !canModifySelection(current)) return null;
  return morph(current, amount, (m) => dilate(m, current.width, current.height));
}

/** Contract Selection: shrinks by `amount`; past the middle → explicit empty. */
export function contractSelection(current: Selection | null, amount: number): Selection | null {
  if (current === null || !canModifySelection(current)) return null;
  return morph(current, amount, (m) => erode(m, current.width, current.height));
}

/**
 * Feather Selection: softens the edge by `amount` — a scalar on the
 * selection (√(f²+a²) ≤ 250), reapplied it stacks, as the Swift comment says.
 */
export function featherSelection(current: Selection | null, amount: number): Selection | null {
  if (current === null || !canModifySelection(current) || amount < 1 || amount > FEATHER_LIMIT) return null;
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
  const det = t.a * t.d - t.b * t.c;
  if (Math.abs(det) < 1e-12) return null;
  const mask = new Uint8Array(width * height);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      // inverse affine on the destination pixel center
      const dx = x + 0.5 - t.e;
      const dy = y + 0.5 - t.f;
      const sx = Math.floor((t.d * dx - t.b * dy) / det);
      const sy = Math.floor((-t.c * dx + t.a * dy) / det);
      if (sx < 0 || sy < 0 || sx >= current.width || sy >= current.height) continue;
      if (current.mask[sy * current.width + sx] === 255) mask[y * width + x] = 255;
    }
  }
  return selectionFromMask(mask, width, height);
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

/** Copy: the selected pixels, cropped to the selection's bounding box. */
export function copyStamp(bmp: Uint8Array, width: number, s: Selection | null): SelectionStamp | null {
  if (s === null || !canModifySelection(s)) return null;
  let minX = s.width, minY = s.height, maxX = -1, maxY = -1;
  for (let y = 0; y < s.height; y++) {
    for (let x = 0; x < s.width; x++) {
      if (s.mask[y * s.width + x] === 255) {
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
      if (s.mask[sy * s.width + sx] !== 255) continue;
      const src = (sy * width + sx) * 4;
      const dst = (y * w + x) * 4;
      pix[dst] = bmp[src];
      pix[dst + 1] = bmp[src + 1];
      pix[dst + 2] = bmp[src + 2];
      pix[dst + 3] = bmp[src + 3];
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
      if (s.mask[y * s.width + x] !== 255) continue;
      const i = (y * width + x) * 4;
      out[i] = out[i + 1] = out[i + 2] = out[i + 3] = 0;
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
  return s !== null && !s.active;
}
