/**
 * Crop: the crop frame session, ratio presets, snapping and the commit
 * math — pure document-space logic (no viewport, no stores, no I/O).
 * Ticket 18; the feature row is T05 (story 36).
 *
 * Semantics ported from the macOS original (read-only truth):
 * - `reference/Swift/Compositor/Document/Crop.swift` — CropGeometry
 *   (snapped/valid/create), the cropRect lifecycle on EditorSession
 *   (selectTool(.crop) starts from the canvas or the selection bounds,
 *   changeCropRatio keeps the width, commitCrop feeds CanvasResizer) and
 *   CropSnap (edges snap, centers never, Control vetoes, Option is
 *   symmetric).
 * - `reference/Swift/CompositorTests/CropTests.swift` — the drag-geometry,
 *   commit-offset and move-snap assertions are ported verbatim.
 * - `reference/Swift/Compositor/IO/CanvasResizer.swift` — a crop resizes the
 *   canvas to the frame and shifts every layer/guide by contentOffset =
 *   -frame.origin; `reference/Swift/docs/project-format.md` (v8): "Guides
 *   survive Canvas Size and Crop by offsetting with the canvas."
 * - `reference/Swift/Compositor/Document/DocumentLimits.swift` — maxSide
 *   30000; Crop.swift's valid() caps |origin| at 1,000,000.
 * - `reference/Swift/Compositor/Rendering/EditorCanvas.swift` —
 *   cropSnapDistance is 8 screen points, divided by pointsPerPixel before
 *   use; here the tolerance is a parameter in document pixels (see
 *   ./snap.ts) and the caller scales it.
 *
 * Documented deviations, per the ticket spec:
 * - setRect/commitCrop clamp the frame into the canvas. The original lets
 *   the frame reach outside the canvas (a crop can grow the canvas via a
 *   negative contentOffset); expansion is out of scope for this layer, so a
 *   frame must intersect the canvas with positive area and lands clamped.
 * - offsetGuides drops guides that land outside the new canvas; the
 *   original CanvasResizer keeps every offset guide, even off-canvas ones.
 * - Crop snap targets exclude canvas/layer centers (Swift
 *   cropSnapTargets() → alignmentSnapTargets(includeCenters: false)).
 *   snap.ts always emits centers for docBounds/layerBounds, so the edge
 *   positions are expanded into guide-position targets instead and the
 *   docBounds/layerBounds toggles default off for crop snapping.
 * - Resize-handle drag math (TransformDrag/LayerTransform) belongs to the
 *   transform ticket: this layer accepts any rect via setRect, so the
 *   interaction layer reshapes the frame and hands it over.
 *
 * Commit/undo contract: commitCrop returns plain data — the new document
 * size plus the offset of the new origin in old coordinates. Everything
 * translates by -offset (a layer at old p sits at p - offset after the
 * crop; CropTests expects the layer origin to go from 0 to (-8,-4) for the
 * frame (8,4,32,16)), so a history entry only needs the previous size plus
 * this object to undo.
 */

import { DEFAULT_SNAP_TOGGLES, snap, type Rect, type SnapOptions, type SnapTargets, type SnapToggles } from "./snap";
import type { Guide } from "./guides";

/** A document size (the crop session's canvas). */
export interface CropDoc {
  width: number;
  height: number;
}

/** A document-space point (drag endpoints, frame centers). */
export interface Point {
  x: number;
  y: number;
}

/** What commitCrop hands to the caller (and the history package). */
export interface CropCommit {
  /** New document size = the frame's size. */
  width: number;
  height: number;
  /**
   * The frame's top-left in old document coordinates: the new coordinate
   * origin sits at old (offsetX, offsetY), so everything translates by
   * -offset (layers, guides).
   */
  offsetX: number;
  offsetY: number;
}

// --- Limits (DocumentLimits.swift / Crop.swift valid()) ---

/** Longest side, in pixels, of any canvas (DocumentLimits.maxSide). */
export const CROP_MAX_SIDE = 30_000;

/** |origin| cap from CropGeometry.valid. */
export const CROP_MAX_POSITION = 1_000_000;

// --- Ratio presets (CropControls picker / cropRatio) ---

export type CropRatioChoice = "free" | "original" | "1:1" | "4:3" | "3:4" | "16:9" | "9:16";

/** The picker's entries, in the original's order ("Free" first). */
export const CROP_RATIO_CHOICES: readonly CropRatioChoice[] = [
  "free",
  "original",
  "1:1",
  "4:3",
  "3:4",
  "16:9",
  "9:16",
];

/** Swift cropRatio: "Original" tracks the document, the rest are fixed; free is null. */
export function resolveCropRatio(choice: CropRatioChoice, doc: CropDoc): number | null {
  switch (choice) {
    case "free":
      return null;
    case "original":
      return doc.height > 0 ? doc.width / doc.height : null;
    case "1:1":
      return 1;
    case "4:3":
      return 4 / 3;
    case "3:4":
      return 3 / 4;
    case "16:9":
      return 16 / 9;
    case "9:16":
      return 9 / 16;
  }
}

// --- Geometry (CropGeometry) ---

/**
 * Swift's .rounded(): halves go away from zero (JS Math.round goes toward
 * +∞, which differs on negatives).
 */
export function roundHalfAwayFromZero(value: number): number {
  return value < 0 ? -Math.round(-value) : Math.round(value);
}

/**
 * CropGeometry.snapped: standardize (flip a negative size), round the edges
 * to whole pixels, keep at least one pixel. The drag path's helper — the
 * session/commit path uses normalizeCropRect, which rejects instead.
 */
export function pixelRoundRect(rect: Rect): Rect {
  const x0 = rect.width < 0 ? rect.x + rect.width : rect.x;
  const y0 = rect.height < 0 ? rect.y + rect.height : rect.y;
  const x = roundHalfAwayFromZero(x0);
  const y = roundHalfAwayFromZero(y0);
  return {
    x,
    y,
    width: Math.max(1, roundHalfAwayFromZero(x0 + Math.abs(rect.width)) - x),
    height: Math.max(1, roundHalfAwayFromZero(y0 + Math.abs(rect.height)) - y),
  };
}

/** CropGeometry.valid: finite, 1…maxSide per side, |origin| ≤ 1,000,000. */
export function isValidCropRect(rect: Rect): boolean {
  return (
    Number.isFinite(rect.x) &&
    Number.isFinite(rect.y) &&
    Number.isFinite(rect.width) &&
    Number.isFinite(rect.height) &&
    rect.width >= 1 &&
    rect.width <= CROP_MAX_SIDE &&
    rect.height >= 1 &&
    rect.height <= CROP_MAX_SIDE &&
    Math.abs(rect.x) <= CROP_MAX_POSITION &&
    Math.abs(rect.y) <= CROP_MAX_POSITION
  );
}

/**
 * The session/commit normalization: flip a negative width/height, round the
 * edges, clamp into the canvas (ticket spec). Returns null when the result
 * is not a frame of at least one pixel per side — a zero-area frame, a
 * sub-pixel sliver, or a rect that does not intersect the canvas with
 * positive area.
 */
export function normalizeCropRect(rect: Rect, doc: CropDoc): Rect | null {
  if (
    !Number.isFinite(rect.x) ||
    !Number.isFinite(rect.y) ||
    !Number.isFinite(rect.width) ||
    !Number.isFinite(rect.height) ||
    !Number.isFinite(doc.width) ||
    !Number.isFinite(doc.height)
  ) {
    return null;
  }
  const x0 = rect.width < 0 ? rect.x + rect.width : rect.x;
  const y0 = rect.height < 0 ? rect.y + rect.height : rect.y;
  let x1 = roundHalfAwayFromZero(x0 + Math.abs(rect.width));
  let y1 = roundHalfAwayFromZero(y0 + Math.abs(rect.height));
  const x = Math.max(0, Math.min(roundHalfAwayFromZero(x0), doc.width));
  const y = Math.max(0, Math.min(roundHalfAwayFromZero(y0), doc.height));
  x1 = Math.max(0, Math.min(x1, doc.width));
  y1 = Math.max(0, Math.min(y1, doc.height));
  if (x1 - x < 1 || y1 - y < 1) return null;
  return { x, y, width: x1 - x, height: y1 - y };
}

/**
 * CropGeometry.create: the frame of a drag from `from` to `to`. A fixed
 * ratio bends the shorter delta to match; `symmetric` (Option) grows the
 * frame out from `from` as its center.
 */
export function createCropRect(from: Point, to: Point, ratio: number | null = null, symmetric = false): Rect {
  let dx = to.x - from.x;
  let dy = to.y - from.y;
  if (ratio !== null) {
    if (Math.abs(dx) > Math.abs(dy) * ratio) {
      dy = (dy < 0 ? -1 : 1) * (Math.abs(dx) / ratio);
    } else {
      dx = (dx < 0 ? -1 : 1) * Math.abs(dy) * ratio;
    }
  }
  if (symmetric) {
    return pixelRoundRect({
      x: from.x - Math.abs(dx),
      y: from.y - Math.abs(dy),
      width: Math.abs(dx) * 2,
      height: Math.abs(dy) * 2,
    });
  }
  return pixelRoundRect({
    x: Math.min(from.x, from.x + dx),
    y: Math.min(from.y, from.y + dy),
    width: Math.abs(dx),
    height: Math.abs(dy),
  });
}

/**
 * The starting frame when the crop tool opens with a selection (Swift
 * selectTool(.crop)): the selection bbox's integral — the smallest enclosing
 * pixel rect — clamped into the canvas, falling back to the full canvas
 * when there is no usable selection.
 */
export function cropFromSelection(bbox: Rect | null | undefined, doc: CropDoc): Rect {
  const canvas: Rect = { x: 0, y: 0, width: doc.width, height: doc.height };
  if (
    !bbox ||
    !Number.isFinite(bbox.x) ||
    !Number.isFinite(bbox.y) ||
    !Number.isFinite(bbox.width) ||
    !Number.isFinite(bbox.height) ||
    bbox.width <= 0 ||
    bbox.height <= 0
  ) {
    return canvas;
  }
  const ix = Math.floor(bbox.x);
  const iy = Math.floor(bbox.y);
  const integral: Rect = {
    x: ix,
    y: iy,
    width: Math.ceil(bbox.x + bbox.width) - ix,
    height: Math.ceil(bbox.y + bbox.height) - iy,
  };
  return normalizeCropRect(integral, doc) ?? canvas;
}

// --- Ratio application (changeCropRatio, adapted to the canvas) ---

/**
 * Applies a ratio to a frame. Without `symmetric`: the largest frame with
 * the ratio that fits inside the canvas ("最大适配框"), anchored on the
 * frame's center. With `symmetric` (Option): the frame's center is kept and
 * the size adjusts minimally — the width is kept and the height derived
 * (Swift changeCropRatio keeps the width); when that derived height would
 * not fit the canvas, the width is derived from the height instead. The
 * position always slides to stay inside the canvas. Returns null when no
 * ratio-fitting frame of at least a pixel per side exists (the session then
 * keeps its current frame).
 */
export function applyAspectRect(rect: Rect, ratio: number, doc: CropDoc, symmetric = false): Rect | null {
  const base = normalizeCropRect(rect, doc);
  if (!base || !Number.isFinite(ratio) || ratio <= 0) return null;
  const W = doc.width;
  const H = doc.height;
  const cx = base.x + base.width / 2;
  const cy = base.y + base.height / 2;
  // Positions clamp into [0, W - w] x [0, H - h]; every size below fits the
  // canvas by construction, so the clamp's upper bound is never below 0.
  const place = (w: number, h: number): Rect => ({
    x: Math.max(0, Math.min(roundHalfAwayFromZero(cx - w / 2), W - w)),
    y: Math.max(0, Math.min(roundHalfAwayFromZero(cy - h / 2), H - h)),
    width: w,
    height: h,
  });

  if (!symmetric) {
    // Canvas wider than the ratio: the height is the limiting side.
    const heightLimited = W / H >= ratio;
    const w = heightLimited ? Math.max(1, roundHalfAwayFromZero(H * ratio)) : W;
    const h = heightLimited ? H : Math.max(1, roundHalfAwayFromZero(W / ratio));
    return place(w, h);
  }

  // Symmetric: keep the width, derive the height (changeCropRatio).
  const h1 = roundHalfAwayFromZero(base.width / ratio);
  if (h1 >= 1 && h1 <= H) return place(base.width, h1);
  // Otherwise derive the width from the kept height.
  const w2 = roundHalfAwayFromZero(base.height * ratio);
  if (w2 >= 1 && w2 <= W) return place(w2, base.height);
  return null;
}

// --- Snapping (CropSnap via snap.ts) ---

/**
 * Crop snap targets with the centers stripped, mirroring Swift
 * cropSnapTargets() = alignmentSnapTargets(includeCenters: false): canvas
 * edges, layer edges, guides — the edge positions expand into
 * guide-position targets because snap.ts always emits centers for
 * docBounds/layerBounds. The returned docBounds only feeds snap.ts's grid
 * line extents (the docBounds toggle stays off — see snapCropRect).
 */
export function cropSnapTargets(targets: SnapTargets, doc: CropDoc): SnapTargets {
  const guides = [...(targets.guides ?? [])];
  guides.push(
    { axis: "vertical", position: 0 },
    { axis: "vertical", position: doc.width },
    { axis: "horizontal", position: 0 },
    { axis: "horizontal", position: doc.height },
  );
  for (const rect of targets.layerBounds ?? []) {
    guides.push(
      { axis: "vertical", position: rect.x },
      { axis: "vertical", position: rect.x + rect.width },
      { axis: "horizontal", position: rect.y },
      { axis: "horizontal", position: rect.y + rect.height },
    );
  }
  return { guides, grid: targets.grid, docBounds: { width: doc.width, height: doc.height } };
}

export interface CropSnapOptions {
  /** View > Snap master switch; off returns the frame unchanged. */
  enabled?: boolean;
  /** Snap To sub-targets; docBounds/layerBounds default off (centers must not attract crop edges). */
  toggles?: Partial<SnapToggles>;
  /**
   * Capture distance in document pixels. Swift's cropSnapDistance is 8
   * screen points; divide by pointsPerPixel for this parameter.
   */
  tolerance?: number;
  /** Ctrl held — vetoes all snapping (passed through to snap.ts). */
  bypass?: boolean;
  /**
   * "edges" (default): each edge snaps on its own, as when creating or
   * resizing. "move": the whole frame shifts by the best edge snap on each
   * axis and keeps its size, as when dragging the frame around.
   */
  mode?: "edges" | "move";
  /** Which edges may snap in "edges" mode; default all four. */
  edges?: { left?: boolean; right?: boolean; top?: boolean; bottom?: boolean };
}

/**
 * Far enough off-document that no real target (guides ≤ 1e6, canvas ≤ 30000,
 * grid lines ≤ canvas) can reach it: lets a one-axis probe snap only the
 * axis under test, so "did it snap" is readable from snappedTo.
 */
const SNAP_PROBE = 1e7;

/**
 * Snaps a crop frame's edges against guides, grid lines, document and layer
 * edges (CropSnap). The frame is returned as-is when snapping is off,
 * bypassed by Ctrl, or nothing is within tolerance; edge snaps that would
 * collapse the frame past its opposite edge are skipped (Swift's
 * `x < result.maxX` / `x > result.minX` guards). The result still goes
 * through normalizeCropRect at setRect time, which drops a snap whose frame
 * would be invalid.
 */
export function snapCropRect(rect: Rect, targets: SnapTargets, doc: CropDoc, opts: CropSnapOptions = {}): Rect {
  const toggles: SnapToggles = { ...DEFAULT_SNAP_TOGGLES, docBounds: false, layerBounds: false, ...opts.toggles };
  const snapOpts: SnapOptions = {
    enabled: opts.enabled,
    toggles,
    tolerance: opts.tolerance,
    bypass: opts.bypass,
  };
  const cropTargets = cropSnapTargets(targets, doc);

  // Probes one axis: the off-document coordinate on the other axis can never
  // snap, so snappedTo non-empty ⇔ this axis landed on a target.
  const snapAxis = (value: number, axis: "x" | "y"): { value: number; snapped: boolean } => {
    const probe = axis === "x" ? { x: value, y: SNAP_PROBE } : { x: SNAP_PROBE, y: value };
    const result = snap(probe, cropTargets, snapOpts);
    return axis === "x"
      ? { value: result.x, snapped: result.snappedTo.length > 0 }
      : { value: result.y, snapped: result.snappedTo.length > 0 };
  };

  if (opts.mode === "move") {
    // Swift CropSnap's move branch: the nearest edge on each axis lands on a
    // target; the size is kept.
    const shift = (edges: number[], axis: "x" | "y"): number => {
      let best: number | null = null;
      for (const edge of edges) {
        const hit = snapAxis(edge, axis);
        if (!hit.snapped) continue;
        const delta = hit.value - edge;
        if (best === null || Math.abs(delta) < Math.abs(best)) best = delta;
      }
      return best ?? 0;
    };
    return {
      x: rect.x + shift([rect.x, rect.x + rect.width], "x"),
      y: rect.y + shift([rect.y, rect.y + rect.height], "y"),
      width: rect.width,
      height: rect.height,
    };
  }

  // Edges mode (create/resize): each edge snaps on its own.
  const want = opts.edges ?? {};
  let x0 = rect.x;
  let x1 = rect.x + rect.width;
  let y0 = rect.y;
  let y1 = rect.y + rect.height;
  if (want.left !== false) {
    const hit = snapAxis(x0, "x");
    if (hit.value < x1) x0 = hit.value;
  }
  if (want.right !== false) {
    const hit = snapAxis(x1, "x");
    if (hit.value > x0) x1 = hit.value;
  }
  if (want.top !== false) {
    const hit = snapAxis(y0, "y");
    if (hit.value < y1) y0 = hit.value;
  }
  if (want.bottom !== false) {
    const hit = snapAxis(y1, "y");
    if (hit.value > y0) y1 = hit.value;
  }
  return { x: x0, y: y0, width: x1 - x0, height: y1 - y0 };
}

// --- Commit and guide translation ---

/**
 * Computes the crop result: the frame normalized (flip, pixel round, canvas
 * clamp), validated, then turned into pure history data — the new document
 * size and the offset of the new origin in old coordinates. Null when the
 * frame is unusable (zero area, no positive-area canvas intersection,
 * beyond DocumentLimits).
 */
export function commitCrop(rect: Rect, doc: CropDoc): CropCommit | null {
  const normalized = normalizeCropRect(rect, doc);
  if (!normalized || !isValidCropRect(normalized)) return null;
  return {
    width: normalized.width,
    height: normalized.height,
    offsetX: normalized.x,
    offsetY: normalized.y,
  };
}

/**
 * Guides after the crop: each shifts by -offset (the new origin sits at old
 * (offsetX, offsetY), so a guide keeps its content), and the ones landing
 * outside the new canvas are dropped (ticket spec; the original keeps every
 * offset guide — see the module header). A guide exactly on the new edge
 * stays.
 */
export function offsetGuides(
  guides: readonly Guide[],
  offsetX: number,
  offsetY: number,
  newWidth: number,
  newHeight: number,
): Guide[] {
  return guides
    .map((guide) => ({
      ...guide,
      position: guide.position - (guide.axis === "vertical" ? offsetX : offsetY),
    }))
    .filter((guide) => {
      const length = guide.axis === "vertical" ? newWidth : newHeight;
      return guide.position >= 0 && guide.position <= length;
    });
}

// --- Session (the cropRect lifecycle on EditorSession) ---

/**
 * The crop frame session: begin (from the canvas or a selection) → reshape
 * (setRect / applyAspect, snapping composed by the caller via snapCropRect)
 * → commit or cancel. Pure document-space state, like GuidesState.
 */
export class CropState {
  #doc: CropDoc | null = null;
  #rect: Rect | null = null;

  /** The current frame; null once cancelled or committed. */
  get rect(): Rect | null {
    return this.#rect;
  }

  /** The document the session began against. */
  get doc(): CropDoc | null {
    return this.#doc;
  }

  /** Swift visibleCropRect: the frame, or the whole canvas when none is set. */
  get visibleRect(): Rect | null {
    if (!this.#doc) return null;
    return this.#rect ?? { x: 0, y: 0, width: this.#doc.width, height: this.#doc.height };
  }

  #fullCanvas(): Rect {
    return { x: 0, y: 0, width: this.#doc!.width, height: this.#doc!.height };
  }

  /** Begins a session; `initialRect` (e.g. cropFromSelection's result) is normalized, falling back to the canvas. */
  beginCrop(doc: CropDoc, initialRect?: Rect | null): boolean {
    if (!Number.isFinite(doc.width) || !Number.isFinite(doc.height) || doc.width < 1 || doc.height < 1) {
      return false;
    }
    this.#doc = { width: doc.width, height: doc.height };
    this.#rect = (initialRect ? normalizeCropRect(initialRect, doc) : null) ?? this.#fullCanvas();
    return true;
  }

  /**
   * Reshapes the frame. The rect is normalized (negative size flips, pixel
   * round, canvas clamp); an unusable rect is refused and the previous
   * frame kept — Swift only assigns when CropGeometry.valid(next).
   */
  setRect(rect: Rect): boolean {
    if (!this.#doc) return false;
    const normalized = normalizeCropRect(rect, this.#doc);
    if (!normalized) return false;
    this.#rect = normalized;
    return true;
  }

  /**
   * Applies a ratio preset (null = "free", a no-op). See applyAspectRect
   * for the max-fit and symmetric semantics; a failed application keeps
   * the current frame.
   */
  applyAspect(ratio: number | null, symmetric = false): boolean {
    if (!this.#doc || !this.#rect || ratio === null || !Number.isFinite(ratio) || ratio <= 0) return false;
    const next = applyAspectRect(this.#rect, ratio, this.#doc, symmetric);
    if (!next) return false;
    this.#rect = next;
    return true;
  }

  /** Commits: pure history data out, frame cleared — Swift clears cropRect and applies the new document size. */
  commit(): CropCommit | null {
    if (!this.#doc || !this.#rect) return null;
    const result = commitCrop(this.#rect, this.#doc);
    if (result) this.#rect = null;
    return result;
  }

  /** Cancels: the frame is dropped, the document untouched. Returns null for assignment-style callers. */
  cancelCrop(): null {
    this.#rect = null;
    return null;
  }
}
