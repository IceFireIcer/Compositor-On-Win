/**
 * Snapping: alignment of a dragged position to guides, the layout grid,
 * layer bounds and document bounds — View > Snap (master), Snap To (four
 * sub-targets) and a Ctrl bypass.
 *
 * Semantics ported from the macOS original (read-only truth):
 * - `reference/Swift/Compositor/Document/Guides.swift` —
 *   alignmentSnapTargets/snappedGuidePosition: target collection order and
 *   which values each target contributes (doc bounds 0/length/center, layer
 *   edges + centers, grid lines, guide positions).
 * - `reference/Swift/Compositor/Document/LayerTransform.swift` —
 *   TransformSnap.distance = 10 screen points is the original capture
 *   distance, converted to document pixels by dividing by pointsPerPixel.
 *   This module is pure document space, so the tolerance is a parameter in
 *   document pixels defaulting to 10; the caller scales it by
 *   1/pointsPerPixel when a viewport is available. Ties keep the earlier
 *   target, matching Swift's `abs(current - value) <= abs(target - value)`
 *   guard.
 * - Default toggles mirror EditorSession's ToolDefaults: snap/snapGuides/
 *   snapLayers/snapBounds on, snapGrid off.
 */

import type { GuideAxis } from "./guides";

/** A document-space rectangle (a layer's bounding box). */
export interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

// --- Layout grid (Swift LayoutGrid: 64 px and eight, every 8 px) ---

export interface LayoutGrid {
  /** Pixels between major lines. */
  spacing: number;
  /** Parts each major square is split into; never finer than a pixel. */
  subdivisions: number;
}

const SPACING_MIN = 2;
const SPACING_MAX = 4096;
const SUBDIVISION_MIN = 1;
const SUBDIVISION_MAX = 64;

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

/** Clamps a grid config to the Swift ranges, subdivisions never finer than a pixel. */
export function normalizeGrid(spacing: number, subdivisions: number): LayoutGrid {
  const s = clamp(Math.round(spacing), SPACING_MIN, SPACING_MAX);
  const sub = clamp(
    Math.round(subdivisions),
    SUBDIVISION_MIN,
    Math.min(SUBDIVISION_MAX, s),
  );
  return { spacing: s, subdivisions: sub };
}

/** Pixels between successive grid lines (Swift LayoutGrid.step). */
export function gridStep(grid: LayoutGrid): number {
  return grid.spacing / grid.subdivisions;
}

/**
 * Every grid line along a document edge, including subdivisions. Counted
 * from the origin rather than accumulated, so an uneven step doesn't drift
 * off the majors (Swift LayoutGrid.lines).
 */
export function gridLines(grid: LayoutGrid, length: number): number[] {
  if (length < 0) return [0];
  const step = gridStep(grid);
  const count = Math.floor(length / step + 0.001);
  const lines: number[] = [];
  for (let i = 0; i <= count; i++) {
    lines.push(Math.round(i * step));
  }
  return lines;
}

/** True for a major grid line (Swift LayoutGrid.isMajor). */
export function isMajorGridLine(grid: LayoutGrid, value: number): boolean {
  const rounded = Math.round(value);
  return Math.abs(rounded % grid.spacing) < 0.001;
}

// --- Snap targets and options ---

/** Everything the caller offers as snap targets for one gesture. */
export interface SnapTargets {
  /** Alignment guides; vertical guides contribute xs, horizontal ys. */
  guides?: ReadonlyArray<{ axis: GuideAxis; position: number }>;
  /** The layout grid; its lines snap along both axes. */
  grid?: LayoutGrid;
  /** Layer bounding boxes; edges and centers snap on both axes. */
  layerBounds?: ReadonlyArray<Rect>;
  /** Document edges and center. */
  docBounds?: { width: number; height: number };
}

/** The four Snap To switches (View > Snap To). */
export interface SnapToggles {
  guides: boolean;
  grid: boolean;
  layerBounds: boolean;
  docBounds: boolean;
}

/**
 * Defaults mirror Swift EditorSession's ToolDefaults: grid snapping starts
 * off, the other three on.
 */
export const DEFAULT_SNAP_TOGGLES: SnapToggles = {
  guides: true,
  grid: false,
  layerBounds: true,
  docBounds: true,
};

export interface SnapOptions {
  /** View > Snap master switch; off returns the position unchanged. */
  enabled?: boolean;
  /** Snap To sub-targets; defaults to DEFAULT_SNAP_TOGGLES. */
  toggles?: Partial<SnapToggles>;
  /**
   * Capture distance in document pixels. Swift's TransformSnap.distance is
   * 10 screen points; document-space callers with a viewport should pass
   * 10 / pointsPerPixel. Default: 10 document px.
   */
  tolerance?: number;
  /** Ctrl held — vetoes all snapping regardless of every switch. */
  bypass?: boolean;
}

export interface SnapResult {
  x: number;
  y: number;
  /** Categories the position landed on, e.g. "guides", "grid", "layerBounds", "docBounds". */
  snappedTo: string[];
}

/**
 * The original capture distance: "How close, in screen points, a guide comes
 * before it snaps" (TransformSnap.distance, LayerTransform.swift).
 */
export const SNAP_TOLERANCE_PX = 10;

interface AxisTargets {
  xs: Array<{ value: number; from: string }>;
  ys: Array<{ value: number; from: string }>;
}

function collectTargets(targets: SnapTargets, toggles: SnapToggles, docWidth: number, docHeight: number): AxisTargets {
  const out: AxisTargets = { xs: [], ys: [] };
  const push = (values: number[], from: string, axis: "x" | "y") => {
    for (const value of values) {
      (axis === "x" ? out.xs : out.ys).push({ value, from });
    }
  };

  // Collection order mirrors Swift alignmentSnapTargets; ties keep the earlier
  // target, so this order is the tie-break priority.
  if (toggles.docBounds && targets.docBounds) {
    const w = targets.docBounds.width;
    const h = targets.docBounds.height;
    push([0, w / 2, w], "docBounds", "x");
    push([0, h / 2, h], "docBounds", "y");
  }
  if (toggles.layerBounds && targets.layerBounds) {
    for (const rect of targets.layerBounds) {
      push(
        [rect.x, rect.x + rect.width / 2, rect.x + rect.width].map(Math.round),
        "layerBounds",
        "x",
      );
      push(
        [rect.y, rect.y + rect.height / 2, rect.y + rect.height].map(Math.round),
        "layerBounds",
        "y",
      );
    }
  }
  if (toggles.grid && targets.grid) {
    push(gridLines(targets.grid, docWidth), "grid", "x");
    push(gridLines(targets.grid, docHeight), "grid", "y");
  }
  if (toggles.guides && targets.guides) {
    for (const guide of targets.guides) {
      if (guide.axis === "vertical") out.xs.push({ value: guide.position, from: "guides" });
      else out.ys.push({ value: guide.position, from: "guides" });
    }
  }
  return out;
}

/** Nearest target within tolerance; null when none qualifies. */
function nearest(
  axisTargets: Array<{ value: number; from: string }>,
  value: number,
  tolerance: number,
): { value: number; from: string } | null {
  let best: { value: number; from: string } | null = null;
  for (const target of axisTargets) {
    const distance = Math.abs(target.value - value);
    if (distance > tolerance) continue;
    // `<=` keeps the earlier target on ties, matching Swift's shift().
    if (best && Math.abs(best.value - value) <= distance) continue;
    best = target;
  }
  return best;
}

/**
 * Snaps a document-space position against the offered targets. Each axis is
 * resolved on its own; `snappedTo` lists the categories the final position
 * landed on (x's category first when both axes snap). The master switch off,
 * Ctrl bypass, or all four Snap To toggles off return the position unchanged.
 */
export function snap(
  position: { x: number; y: number },
  targets: SnapTargets,
  opts: SnapOptions = {},
): SnapResult {
  const unchanged = { x: position.x, y: position.y, snappedTo: [] as string[] };
  if (opts.bypass || opts.enabled === false) return unchanged;

  const toggles: SnapToggles = { ...DEFAULT_SNAP_TOGGLES, ...opts.toggles };
  const anyTargetOn =
    (toggles.guides && targets.guides !== undefined) ||
    (toggles.grid && targets.grid !== undefined) ||
    (toggles.layerBounds && targets.layerBounds !== undefined) ||
    (toggles.docBounds && targets.docBounds !== undefined);
  if (!anyTargetOn) return unchanged;

  // Grid lines need a document length; when the caller offered no doc bounds,
  // size the span generously around the position so nearby lines exist.
  const width = targets.docBounds?.width ?? Math.ceil(Math.max(position.x, 0)) + SNAP_TOLERANCE_PX * 2;
  const height = targets.docBounds?.height ?? Math.ceil(Math.max(position.y, 0)) + SNAP_TOLERANCE_PX * 2;

  const { xs, ys } = collectTargets(targets, toggles, width, height);
  const tolerance = opts.tolerance ?? SNAP_TOLERANCE_PX;

  const snappedTo: string[] = [];
  let { x, y } = position;
  const hitX = nearest(xs, position.x, tolerance);
  if (hitX) {
    x = hitX.value;
    if (!snappedTo.includes(hitX.from)) snappedTo.push(hitX.from);
  }
  const hitY = nearest(ys, position.y, tolerance);
  if (hitY) {
    y = hitY.value;
    if (!snappedTo.includes(hitY.from)) snappedTo.push(hitY.from);
  }
  return { x, y, snappedTo };
}
