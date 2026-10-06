import { writable } from "svelte/store";
import type { GuideAxis } from "./guides";
import { DEFAULT_SNAP_TOGGLES, SNAP_TOLERANCE_PX, gridLines, type SnapTargets, type SnapToggles } from "./snap";

/**
 * Transform session: the pure logic layer behind non-destructive layer
 * transforms — move / resize / rotate / distort (⌘-dragged corners), group
 * transforms, arrow-key nudges, numeric input and snap hints. The transform
 * only ever rewrites origin / size / rotation / flips / sampling: the layer's
 * asset resolution is never touched (non-destructive, TransformTests
 * "renderingRotatesScalesAndFlipsWithoutReplacingPixels").
 *
 * Semantics ported from the macOS original (read-only truth):
 * - `reference/Swift/Compositor/Document/LayerTransform.swift` —
 *   LayerTransform (isValid/rounded/point/contains/scalePercent/scaled/
 *   placing/following, lines 17-99), TransformDrag.updated (lines 153-214:
 *   move axis lock, rotate 15° Shift steps, resize anchors with mirror-flip
 *   overdrag, `lockRatio != shift`), TransformDrag.corners (136-151), and
 *   TransformSnap.offset/shift (226-245: smallest box-edge move, ties keep
 *   the earlier target).
 * - `reference/Swift/Compositor/Document/EditorSession.swift` —
 *   beginTransform (412), previewTransform (428: invalid values are vetoed),
 *   commitTransform (451: group carry, no-op on invalid draft),
 *   cancelTransform (493), nudgeLayer (537: 1 px, Shift 10 px, one undo step
 *   when no edit is open), locksTransformRatio default true (197).
 * - `reference/Swift/Compositor/Document/LayerFlip.swift` —
 *   mirrored(horizontally:across:) (6-17: flag toggles, angle negates, the
 *   middle crosses the axis), flipCanvas (55: every layer across the document
 *   middle as one step).
 * - `reference/Swift/Compositor/Document/Crop.swift` — snappedMove (122-137)
 *   and snappedResizePoint (143-182: upright only, the dragged edge snaps,
 *   proportional keeps only the nearer edge).
 * - `reference/Swift/Compositor/Document/Guides.swift` —
 *   alignmentSnapTargets (247-284: doc bounds 0/length/center, layer
 *   min/mid/max, grid lines, guides — in that order, so ties resolve early).
 * - `reference/Swift/Compositor/Rendering/EditorCanvas.swift` — the drag
 *   pipeline (1960-1993: resize snaps the pointer, move snaps the rounded
 *   draft, rotation is never snapped, Ctrl bypasses) and arrow-key nudge
 *   steps (2194-2198: 1 px, Shift 10 px).
 * - `reference/Swift/Compositor/UI/TransformInspector.swift` — numeric fields
 *   (22-43: X/Y ±30,000, W/H 1…30,000, Scale 0.1…30,000 % about the center,
 *   ° wraps into ±360, Flip H/V buttons) and resize() (89-100: W keeps the
 *   top-left origin, the other side follows when the ratio is locked).
 * - `reference/Swift/Compositor/Document/BrushStroke.swift` —
 *   BrushRaster.pixelToDocument (110-116), the flipped/rotated unit mapping.
 * - `reference/Swift/Compositor/Document/Distort.swift` — DistortWarp.corners
 *   (10-12: TL, TR, BR, BL in handle order).
 *
 * Shapes mirror `internal/domain/transform.go` (origin/size as [x, y] /
 * [w, h] arrays, rotation in degrees clockwise, sampling strings spelled
 * exactly like the macOS LayerSampling raw values).
 */

/** Resampling quality; raw values are the macOS LayerSampling spellings. */
export type Sampling = "Nearest" | "Smooth" | "High quality";

export const SAMPLINGS: readonly Sampling[] = ["Nearest", "Smooth", "High quality"];

/** Default matches Go's domain.SamplingHighQuality / Swift .high. */
export const DEFAULT_SAMPLING: Sampling = "High quality";

/**
 * Places a layer in document pixels — the TS twin of
 * `internal/domain/transform.go` Transform / Swift LayerTransform.
 * Origin is the top-left corner of the unrotated bounds; rotation is
 * degrees clockwise around their center.
 */
export interface Transform {
  origin: [number, number];
  size: [number, number];
  rotation: number;
  flipX: boolean;
  flipY: boolean;
  sampling: Sampling;
}

/** A document-space point. */
export interface Vec2 {
  x: number;
  y: number;
}

/** 2×3 affine (CGAffineTransform layout): maps p → (a·x + c·y + tx, b·x + d·y + ty). */
export interface Affine {
  a: number;
  b: number;
  c: number;
  d: number;
  tx: number;
  ty: number;
}

/** The eight handles in Swift order: corners at even indices, edge midpoints odd. */
export const HANDLES: readonly Vec2[] = [
  { x: 0, y: 0 }, { x: 0.5, y: 0 }, { x: 1, y: 0 },
  { x: 1, y: 0.5 }, { x: 1, y: 1 }, { x: 0.5, y: 1 },
  { x: 0, y: 1 }, { x: 0, y: 0.5 },
];

/** Swift LayerTransform.isValid bounds. */
export const TRANSFORM_LIMITS = { maxSize: 300_000, maxOrigin: 1_000_000 } as const;

/** TransformInspector field ranges (typed values are clamped to these). */
export const FIELD_LIMITS = {
  origin: 30_000,
  size: 30_000,
  scalePercentMin: 0.1,
  scalePercentMax: 30_000,
  rotation: 360,
} as const;

/** Scale floor: the Scale field's 0.1 % minimum is a 0.001× scale. */
export const MIN_SCALE = 0.001;

/** Arrow-key nudge steps (EditorCanvas: 1 px, Shift 10 px). */
export const NUDGE_STEP = 1;
export const NUDGE_STEP_LARGE = 10;

export function makeTransform(over: Partial<Transform> = {}): Transform {
  return {
    origin: [0, 0],
    size: [0, 0],
    rotation: 0,
    flipX: false,
    flipY: false,
    sampling: DEFAULT_SAMPLING,
    ...over,
  };
}

function cloneTransform(t: Transform): Transform {
  return { origin: [t.origin[0], t.origin[1]], size: [t.size[0], t.size[1]], rotation: t.rotation, flipX: t.flipX, flipY: t.flipY, sampling: t.sampling };
}

/** Swift `.rounded()` — half away from zero, not JS's half-up. */
function roundHalfAway(x: number): number {
  return Math.sign(x) * Math.round(Math.abs(x));
}

function clamp(x: number, min: number, max: number): number {
  return Math.min(Math.max(x, min), max);
}

// --- Pure geometry (LayerTransform.swift) ---

export function transformCenter(t: Transform): Vec2 {
  return { x: t.origin[0] + t.size[0] / 2, y: t.origin[1] + t.size[1] / 2 };
}

/** Radians, normalized through 360 like Swift's `radians`. */
export function radiansOf(degrees: number): number {
  return ((degrees % 360) * Math.PI) / 180;
}

/** Where the unit square point lands (flips do not move the handles). */
export function transformedPoint(t: Transform, unit: Vec2): Vec2 {
  const c = transformCenter(t);
  const rad = radiansOf(t.rotation);
  const x = (unit.x - 0.5) * t.size[0];
  const y = (unit.y - 0.5) * t.size[1];
  return {
    x: c.x + x * Math.cos(rad) - y * Math.sin(rad),
    y: c.y + x * Math.sin(rad) + y * Math.cos(rad),
  };
}

/** Corners in DistortWarp order: top-left, top-right, bottom-right, bottom-left. */
export function transformCorners(t: Transform): Vec2[] {
  return [
    transformedPoint(t, { x: 0, y: 0 }),
    transformedPoint(t, { x: 1, y: 0 }),
    transformedPoint(t, { x: 1, y: 1 }),
    transformedPoint(t, { x: 0, y: 1 }),
  ];
}

/** Rotated-quad hit test (LayerTransform.contains). */
export function transformContains(t: Transform, p: Vec2): boolean {
  const c = transformCenter(t);
  const rad = radiansOf(t.rotation);
  const x = p.x - c.x;
  const y = p.y - c.y;
  return (
    Math.abs(x * Math.cos(rad) + y * Math.sin(rad)) <= t.size[0] / 2 &&
    Math.abs(-x * Math.sin(rad) + y * Math.cos(rad)) <= t.size[1] / 2
  );
}

export function isValidTransform(t: Transform): boolean {
  return (
    [t.origin[0], t.origin[1], t.size[0], t.size[1], t.rotation].every(Number.isFinite) &&
    t.size[0] >= 1 && t.size[0] <= TRANSFORM_LIMITS.maxSize &&
    t.size[1] >= 1 && t.size[1] <= TRANSFORM_LIMITS.maxSize &&
    Math.abs(t.origin[0]) <= TRANSFORM_LIMITS.maxOrigin &&
    Math.abs(t.origin[1]) <= TRANSFORM_LIMITS.maxOrigin
  );
}

export function transformEquals(a: Transform, b: Transform): boolean {
  return (
    a.origin[0] === b.origin[0] && a.origin[1] === b.origin[1] &&
    a.size[0] === b.size[0] && a.size[1] === b.size[1] &&
    a.rotation === b.rotation && a.flipX === b.flipX && a.flipY === b.flipY &&
    a.sampling === b.sampling
  );
}

/** Whole pixels and whole degrees — what dragging leaves behind (LayerTransform.rounded). */
export function roundedTransform(t: Transform): Transform {
  return {
    ...cloneTransform(t),
    origin: [roundHalfAway(t.origin[0]), roundHalfAway(t.origin[1])],
    size: [Math.max(1, roundHalfAway(t.size[0])), Math.max(1, roundHalfAway(t.size[1]))],
    rotation: roundHalfAway(t.rotation),
  };
}

/** Swift truncatingRemainder(360): keeps the sign, never -0. */
export function normalizeRotation(degrees: number): number {
  return (degrees % 360) + 0;
}

/** Width as a percentage of the pixelSize it places (100 % draws 1:1). */
export function scalePercent(t: Transform, pixelSize: [number, number]): number {
  return (t.size[0] / Math.max(1, pixelSize[0])) * 100;
}

/** Both sides set to `percent` of pixelSize, keeping center, rotation, flips. */
export function scaledToPercent(t: Transform, percent: number, pixelSize: [number, number]): Transform {
  const c = transformCenter(t);
  const size: [number, number] = [(pixelSize[0] * percent) / 100, (pixelSize[1] * percent) / 100];
  return {
    ...cloneTransform(t),
    size,
    origin: [c.x - size[0] / 2, c.y - size[1] / 2],
  };
}

// --- Affine plumbing (CGAffineTransform layout; concat applies `first` first) ---

export function applyAffine(m: Affine, p: Vec2): Vec2 {
  return { x: m.a * p.x + m.c * p.y + m.tx, y: m.b * p.x + m.d * p.y + m.ty };
}

/** `first` followed by `then`. */
export function concatAffine(first: Affine, then: Affine): Affine {
  return {
    a: first.a * then.a + first.b * then.c,
    b: first.a * then.b + first.b * then.d,
    c: first.c * then.a + first.d * then.c,
    d: first.c * then.b + first.d * then.d,
    tx: first.tx * then.a + first.ty * then.c + then.tx,
    ty: first.tx * then.b + first.ty * then.d + then.ty,
  };
}

export function invertAffine(m: Affine): Affine {
  const det = m.a * m.d - m.b * m.c;
  return {
    a: m.d / det,
    b: -m.b / det,
    c: -m.c / det,
    d: m.a / det,
    tx: (m.c * m.ty - m.d * m.tx) / det,
    ty: (m.b * m.tx - m.a * m.ty) / det,
  };
}

/**
 * The unit square (0…1, y down) mapped where the transform places a layer —
 * BrushRaster.pixelToDocument(self, width: 1, height: 1): minus half, scale
 * with flip signs, rotate, then the center (BrushStroke.swift:110).
 */
export function unitToDocument(t: Transform): Affine {
  const c = transformCenter(t);
  const rad = radiansOf(t.rotation);
  const cos = Math.cos(rad);
  const sin = Math.sin(rad);
  const sx = t.size[0] * (t.flipX ? -1 : 1);
  const sy = t.size[1] * (t.flipY ? -1 : 1);
  const a = cos * sx;
  const b = sin * sx;
  const c2 = -sin * sy;
  const d = cos * sy;
  return {
    a,
    b,
    c: c2,
    d,
    tx: c.x - (a + c2) * 0.5,
    ty: c.y - (b + d) * 0.5,
  };
}

/**
 * A transform placing the unit square as `map` does — a rotated, maybe
 * flipped rectangle (shear is dropped). Keeps `keep`'s flipX and the rotation
 * nearest it (LayerTransform.placing, lines 64-77).
 */
function placing(map: Affine, keep: { rotation: number; flipX: boolean }): Transform {
  const sign = keep.flipX ? -1 : 1;
  const angle = Math.atan2(map.b * sign, map.a * sign);
  const along = -map.c * Math.sin(angle) + map.d * Math.cos(angle);
  const middle = applyAffine(map, { x: 0.5, y: 0.5 });
  const size: [number, number] = [Math.hypot(map.a, map.b), Math.abs(along)];
  const degrees = (angle * 180) / Math.PI;
  return makeTransform({
    size,
    rotation: degrees + roundHalfAway((keep.rotation - degrees) / 360) * 360,
    flipX: keep.flipX,
    flipY: along < 0,
    origin: [middle.x - size[0] / 2, middle.y - size[1] / 2],
  });
}

/**
 * This placement carried along as a layer moves from `old` to `new`
 * (LayerTransform.following, lines 79-89): a plain move carries exactly,
 * anything else goes through the placement mapping.
 */
export function followingTransform(t: Transform, old: Transform, to: Transform): Transform {
  if (transformEquals(old, to)) return cloneTransform(t);
  if (
    old.size[0] === to.size[0] && old.size[1] === to.size[1] &&
    old.rotation === to.rotation && old.flipX === to.flipX && old.flipY === to.flipY
  ) {
    // A plain move of the box carries exactly.
    const carried = cloneTransform(t);
    carried.origin[0] += to.origin[0] - old.origin[0];
    carried.origin[1] += to.origin[1] - old.origin[1];
    return carried;
  }
  const map = concatAffine(concatAffine(unitToDocument(t), invertAffine(unitToDocument(old))), unitToDocument(to));
  return placing(map, { rotation: t.rotation, flipX: t.flipX });
}

// --- Flips (LayerFlip.swift) ---

/**
 * This placement mirrored across a vertical line at `axis` (or a horizontal
 * one): the picture flips, its angle turns the other way, and its middle
 * crosses to the other side of the line (LayerTransform.mirrored).
 */
export function flipTransformAbout(t: Transform, horizontally: boolean, axis: number): Transform {
  const c = transformCenter(t);
  const result = cloneTransform(t);
  if (horizontally) {
    result.flipX = !t.flipX;
    result.origin[0] = 2 * axis - c.x - t.size[0] / 2;
  } else {
    result.flipY = !t.flipY;
    result.origin[1] = 2 * axis - c.y - t.size[1] / 2;
  }
  result.rotation = -t.rotation;
  return result;
}

/** Flips the layer about its own middle (EditorSession.flipLayers, single layer). */
export function flipLayerTransform(t: Transform, horizontally: boolean): Transform {
  const c = transformCenter(t);
  return flipTransformAbout(t, horizontally, horizontally ? c.x : c.y);
}

/**
 * Flips the whole canvas: every layer mirrored across the document middle
 * (LayerFlip.flipCanvas). Pure: callers layer selection/guide mirroring on
 * top. `transforms` is keyed by layer id; the input is never mutated.
 */
export function flipCanvasTransforms(
  transforms: Record<string, Transform>,
  horizontally: boolean,
  docWidth: number,
  docHeight: number,
): Record<string, Transform> {
  const axis = horizontally ? docWidth / 2 : docHeight / 2;
  const out: Record<string, Transform> = {};
  for (const id of Object.keys(transforms)) {
    out[id] = flipTransformAbout(transforms[id], horizontally, axis);
  }
  return out;
}

// --- Groups (TransformGroup / EditorSession.commitTransform) ---

/** Applies the same move delta to every selected layer's transform. */
export function transformAll(transforms: Record<string, Transform>, delta: Vec2): Record<string, Transform> {
  const out: Record<string, Transform> = {};
  for (const id of Object.keys(transforms)) {
    const t = transforms[id];
    out[id] = { ...cloneTransform(t), origin: [t.origin[0] + delta.x, t.origin[1] + delta.y] };
  }
  return out;
}

/** The upright box around all members' corners (EditorSession.groupTransformBox). */
export function groupTransformBox(transforms: Record<string, Transform>): Transform | null {
  const ids = Object.keys(transforms);
  if (ids.length === 0) return null;
  let minX = Number.POSITIVE_INFINITY;
  let minY = Number.POSITIVE_INFINITY;
  let maxX = Number.NEGATIVE_INFINITY;
  let maxY = Number.NEGATIVE_INFINITY;
  for (const id of ids) {
    for (const corner of transformCorners(transforms[id])) {
      minX = Math.min(minX, corner.x);
      minY = Math.min(minY, corner.y);
      maxX = Math.max(maxX, corner.x);
      maxY = Math.max(maxY, corner.y);
    }
  }
  return makeTransform({ origin: [minX, minY], size: [maxX - minX, maxY - minY] });
}

/**
 * Where `layer`'s transform sits while the group box moves from `box` to
 * `draft` (EditorSession.pendingTransform → following).
 */
export function followGroup(
  originals: Record<string, Transform>,
  box: Transform,
  draft: Transform,
): Record<string, Transform> {
  const out: Record<string, Transform> = {};
  for (const id of Object.keys(originals)) {
    out[id] = followingTransform(originals[id], box, draft);
  }
  return out;
}

/**
 * The commit-time group carry (EditorSession.commitTransform group branch):
 * each member follows the box, and members whose carried placement is not
 * valid are left out.
 */
export function applyGroupCarry(
  originals: Record<string, Transform>,
  box: Transform,
  draft: Transform,
): Record<string, Transform> {
  const out: Record<string, Transform> = {};
  for (const id of Object.keys(originals)) {
    const moved = followingTransform(originals[id], box, draft);
    if (isValidTransform(moved)) out[id] = moved;
  }
  return out;
}

// --- Nudge (EditorSession.nudgeLayer / EditorCanvas arrow keys) ---

export function nudgeStep(shift: boolean): number {
  return shift ? NUDGE_STEP_LARGE : NUDGE_STEP;
}

/**
 * One arrow press with no edit open: begin → preview → commit in one step
 * (Swift nudgeLayer's not-already-editing path). Pure: returns the moved
 * transform, or the start when the move would be invalid.
 */
export function nudgeTransform(start: Transform, dx: number, dy: number): Transform {
  const moved: Transform = { ...cloneTransform(start), origin: [start.origin[0] + dx, start.origin[1] + dy] };
  return isValidTransform(moved) ? moved : cloneTransform(start);
}

// --- Drag math (TransformDrag.updated / TransformDrag.corners) ---

export type DragMode =
  | { kind: "move" }
  | { kind: "resize"; handle: number }
  | { kind: "rotate" }
  | { kind: "distort"; handle: number };

export interface DragModifiers {
  /** Move/distort: lock the dominant axis. Rotate: 15° steps. Resize: toggles the ratio lock. */
  shift?: boolean;
  /** Resize about the center at double rate (Option-drag). */
  option?: boolean;
  /** Vetoes all snapping for the gesture (Ctrl/Cmd drag). */
  ctrl?: boolean;
  /** Session ratio lock (Swift locksTransformRatio, default true). */
  lockRatio?: boolean;
}

/**
 * TransformDrag.updated: the transform after dragging `mode` from `start` to
 * `point` (document pixels). Returns `original` when the result is not a
 * valid placement, like Swift's `result.isValid ? result : original`.
 */
export function dragUpdated(original: Transform, mode: DragMode, start: Vec2, point: Vec2, mods: DragModifiers = {}): Transform {
  const shift = mods.shift === true;
  const option = mods.option === true;
  const lockRatio = mods.lockRatio !== false;
  const result = cloneTransform(original);
  switch (mode.kind) {
    case "move": {
      let dx = point.x - start.x;
      let dy = point.y - start.y;
      if (shift) {
        if (Math.abs(dx) >= Math.abs(dy)) dy = 0;
        else dx = 0;
      }
      result.origin[0] += dx;
      result.origin[1] += dy;
      break;
    }
    case "rotate": {
      const center = transformCenter(original);
      const delta =
        Math.atan2(point.y - center.y, point.x - center.x) -
        Math.atan2(start.y - center.y, start.x - center.x);
      result.rotation += (delta * 180) / Math.PI;
      if (shift) result.rotation = roundHalfAway(result.rotation / 15) * 15;
      break;
    }
    case "resize": {
      const handle = HANDLES[mode.handle];
      const anchorUnit = option ? { x: 0.5, y: 0.5 } : { x: 1 - handle.x, y: 1 - handle.y };
      const anchor = transformedPoint(original, anchorUnit);
      // The initial handle plus the pointer delta avoids a jump on grab.
      const initialHandle = transformedPoint(original, handle);
      const dx = initialHandle.x + point.x - start.x - anchor.x;
      const dy = initialHandle.y + point.y - start.y - anchor.y;
      const rad = radiansOf(original.rotation);
      const cos = Math.cos(rad);
      const sin = Math.sin(rad);
      // Center-to-handle distances cover half the size on each axis.
      const span = option ? 2 : 1;
      const localX = (dx * cos + dy * sin) * span;
      const localY = (-dx * sin + dy * cos) * span;
      const sx = handle.x * 2 - 1;
      const sy = handle.y * 2 - 1;
      // Dragging past the opposite side turns the layer over rather than
      // stopping at nothing: size stays positive, the axis flips.
      const rawWidth = sx === 0 ? original.size[0] : localX * sx;
      const rawHeight = sy === 0 ? original.size[1] : localY * sy;
      const mirroredX = rawWidth < 0;
      const mirroredY = rawHeight < 0;
      let width = Math.max(1, Math.abs(rawWidth));
      let height = Math.max(1, Math.abs(rawHeight));
      if (lockRatio !== shift) {
        let factor: number;
        if (sx === 0) factor = height / original.size[1];
        else if (sy === 0) factor = width / original.size[0];
        else {
          // Project onto the original diagonal for proportional scaling.
          factor = Math.max(
            1 / Math.min(original.size[0], original.size[1]),
            (localX * sx * original.size[0] + localY * sy * original.size[1]) /
              (original.size[0] * original.size[0] + original.size[1] * original.size[1]),
          );
        }
        width = original.size[0] * factor;
        height = original.size[1] * factor;
      }
      result.size = [width, height];
      if (mirroredX) result.flipX = !result.flipX;
      if (mirroredY) result.flipY = !result.flipY;
      // Turned over, the box lies on the other side of the anchor.
      const offsetX = (0.5 - anchorUnit.x) * width * (mirroredX ? -1 : 1);
      const offsetY = (0.5 - anchorUnit.y) * height * (mirroredY ? -1 : 1);
      const centerX = anchor.x + offsetX * cos - offsetY * sin;
      const centerY = anchor.y + offsetX * sin + offsetY * cos;
      result.origin = [centerX - width / 2, centerY - height / 2];
      break;
    }
    case "distort":
      break; // The corners are the controls; the draft does not move.
  }
  return isValidTransform(result) ? result : original;
}

/**
 * TransformDrag.corners: the four corners after dragging to `point`. A corner
 * handle moves its corner, an edge handle both of that edge's corners, and a
 * move the whole shape; nil for anything else. Shift keeps the drag on one
 * axis.
 */
export function dragCorners(
  originalCorners: readonly Vec2[],
  mode: DragMode,
  start: Vec2,
  point: Vec2,
  shift: boolean,
): Vec2[] | null {
  if (mode.kind !== "distort" && mode.kind !== "move") return null;
  let dx = point.x - start.x;
  let dy = point.y - start.y;
  if (shift) {
    if (Math.abs(dx) >= Math.abs(dy)) dy = 0;
    else dx = 0;
  }
  const moved: number[] =
    mode.kind === "distort"
      ? mode.handle % 2 === 0
        ? [mode.handle / 2]
        : [Math.floor(mode.handle / 2), (Math.floor(mode.handle / 2) + 1) % 4]
      : [0, 1, 2, 3];
  const result = originalCorners.map((c) => ({ x: c.x, y: c.y }));
  for (const index of moved) {
    result[index].x += dx;
    result[index].y += dy;
  }
  return result;
}

// --- Snap hints (TransformSnap / snappedMove / snappedResizePoint) ---

/** A guide line to draw along the edge or center that snapped. */
export interface SnapHint {
  axis: GuideAxis;
  position: number;
}

export interface SnapInput {
  /** View > Snap To targets (guides, grid, layer bounds, document bounds). */
  targets?: SnapTargets;
  /** Sub-switches; defaults to snap.ts's DEFAULT_SNAP_TOGGLES. */
  toggles?: Partial<SnapToggles>;
  /** View > Snap master switch; off vetoes all snapping. */
  enabled?: boolean;
  /** Capture distance in document pixels (Swift TransformSnap.distance is 10 screen points). */
  tolerance?: number;
}

interface SnapContext {
  targets: SnapTargets;
  toggles: SnapToggles;
  tolerance: number;
}

function resolveSnap(input: SnapInput): SnapContext | null {
  if (input.enabled === false || !input.targets) return null;
  return {
    targets: input.targets,
    toggles: { ...DEFAULT_SNAP_TOGGLES, ...input.toggles },
    tolerance: input.tolerance ?? SNAP_TOLERANCE_PX,
  };
}

/**
 * Alignment targets in Guides.swift order — doc bounds (0, length, center),
 * layer bounds (min, mid, max, rounded), grid lines, guides — so the earlier
 * target wins ties.
 */
export function alignmentTargets(targets: SnapTargets, toggles: SnapToggles): { xs: number[]; ys: number[] } {
  const xs: number[] = [];
  const ys: number[] = [];
  if (toggles.docBounds && targets.docBounds) {
    xs.push(0, targets.docBounds.width);
    ys.push(0, targets.docBounds.height);
    xs.push(targets.docBounds.width / 2);
    ys.push(targets.docBounds.height / 2);
  }
  if (toggles.layerBounds && targets.layerBounds) {
    for (const rect of targets.layerBounds) {
      xs.push(Math.round(rect.x), Math.round(rect.x + rect.width / 2), Math.round(rect.x + rect.width));
      ys.push(Math.round(rect.y), Math.round(rect.y + rect.height / 2), Math.round(rect.y + rect.height));
    }
  }
  if (toggles.grid && targets.grid) {
    xs.push(...gridLines(targets.grid, targets.docBounds?.width ?? 0));
    ys.push(...gridLines(targets.grid, targets.docBounds?.height ?? 0));
  }
  if (toggles.guides && targets.guides) {
    for (const guide of targets.guides) {
      if (guide.axis === "vertical") xs.push(guide.position);
      else ys.push(guide.position);
    }
  }
  return { xs, ys };
}

/** TransformSnap.shift: the smallest move that lands a guide on a target. */
function shiftEdges(
  guides: number[],
  targets: number[],
  tolerance: number,
): { move: number; target: number | null } {
  let best: { move: number; target: number } | null = null;
  for (const guideValue of guides) {
    for (const target of targets) {
      const move = target - guideValue;
      if (Math.abs(move) > tolerance) continue;
      // `<=` keeps the earlier target on ties, matching Swift's shift().
      if (best && Math.abs(best.move) <= Math.abs(move)) continue;
      best = { move, target };
    }
  }
  return { move: best?.move ?? 0, target: best?.target ?? null };
}

/**
 * snappedMove: the moving box's left/center/right (and top/center/bottom, of
 * its upright bounding box) snap to the targets, each axis on its own.
 * Returns the moved transform and the guide lines it landed on.
 */
export function snappedMoveTransform(
  draft: Transform,
  input: SnapInput,
): { transform: Transform; guides: SnapHint[] } {
  const ctx = resolveSnap(input);
  if (!ctx) return { transform: cloneTransform(draft), guides: [] };
  const { xs, ys } = alignmentTargets(ctx.targets, ctx.toggles);
  const corners = transformCorners(draft);
  const xsAll = corners.map((c) => c.x);
  const ysAll = corners.map((c) => c.y);
  const box = {
    minX: Math.min(...xsAll),
    maxX: Math.max(...xsAll),
    minY: Math.min(...ysAll),
    maxY: Math.max(...ysAll),
  };
  const horizontal = shiftEdges([box.minX, (box.minX + box.maxX) / 2, box.maxX], xs, ctx.tolerance);
  const vertical = shiftEdges([box.minY, (box.minY + box.maxY) / 2, box.maxY], ys, ctx.tolerance);
  const guides: SnapHint[] = [];
  if (horizontal.target !== null) guides.push({ axis: "vertical", position: horizontal.target });
  if (vertical.target !== null) guides.push({ axis: "horizontal", position: vertical.target });
  const transform = cloneTransform(draft);
  if (horizontal.move !== 0 || vertical.move !== 0) {
    transform.origin = [draft.origin[0] + horizontal.move, draft.origin[1] + vertical.move];
  }
  return { transform, guides };
}

/**
 * snappedResizePoint: the dragged resize handle's edges snap to nearby
 * targets, within tolerance document pixels. Upright layers only — a turned
 * one's edges don't run along the targets (Crop.swift radians == 0 guard).
 * `proportional` keeps only the nearer of the two edges snapping; `update`
 * is the drag's own result for a pointer position.
 */
export function snappedResizePointer(
  point: Vec2,
  drag: { original: Transform; start: Vec2; handle: number },
  proportional: boolean,
  input: SnapInput,
  update: (point: Vec2) => Transform,
): { point: Vec2; guides: SnapHint[] } {
  const ctx = resolveSnap(input);
  if (!ctx || radiansOf(drag.original.rotation) !== 0) {
    return { point: { x: point.x, y: point.y }, guides: [] };
  }
  const handle = HANDLES[drag.handle];
  const { xs, ys } = alignmentTargets(ctx.targets, ctx.toggles);
  const grab = transformedPoint(drag.original, handle);
  // Where the dragged handle is, to tell its edge from the one across from it.
  const at = { x: grab.x + point.x - drag.start.x, y: grab.y + point.y - drag.start.y };
  const edge = (transform: Transform, horizontal: boolean): number => {
    const minX = transform.origin[0];
    const minY = transform.origin[1];
    const maxX = minX + transform.size[0];
    const maxY = minY + transform.size[1];
    return horizontal
      ? Math.abs(minX - at.x) <= Math.abs(maxX - at.x) ? minX : maxX
      : Math.abs(minY - at.y) <= Math.abs(maxY - at.y) ? minY : maxY;
  };
  const nearest = (value: number, lines: number[]): number | null => {
    let best: number | null = null;
    for (const line of lines) {
      if (Math.abs(line - value) > ctx.tolerance) continue;
      if (best !== null && !(Math.abs(line - value) < Math.abs(best - value))) continue;
      best = line;
    }
    return best;
  };
  const draft = update(point);
  let snaps: Array<{ horizontal: boolean; target: number }> = [];
  if (handle.x !== 0.5) {
    const x = nearest(edge(draft, true), xs);
    if (x !== null) snaps.push({ horizontal: true, target: x });
  }
  if (handle.y !== 0.5) {
    const y = nearest(edge(draft, false), ys);
    if (y !== null) snaps.push({ horizontal: false, target: y });
  }
  if (proportional && snaps.length === 2) {
    snaps = [
      snaps.reduce((a, b) =>
        Math.abs(b.target - edge(draft, b.horizontal)) < Math.abs(a.target - edge(draft, a.horizontal)) ? b : a,
      ),
    ];
  }
  // An edge follows the pointer in a straight line along each axis, so one
  // step measured across a pixel lands it.
  const result = { x: point.x, y: point.y };
  for (const snap of snaps) {
    const before = edge(update(result), snap.horizontal);
    const nudged = snap.horizontal ? { x: result.x + 1, y: result.y } : { x: result.x, y: result.y + 1 };
    const perPixel = edge(update(nudged), snap.horizontal) - before;
    if (Math.abs(perPixel) <= 0.01) continue;
    const shift = (snap.target - before) / perPixel;
    if (snap.horizontal) result.x += shift;
    else result.y += shift;
  }
  const guides: SnapHint[] = [
    ...snaps.filter((s) => s.horizontal).map((s) => ({ axis: "vertical" as const, position: s.target })),
    ...snaps.filter((s) => !s.horizontal).map((s) => ({ axis: "horizontal" as const, position: s.target })),
  ];
  return { point: result, guides };
}

// --- Session (EditorSession.transformEdit lifecycle) ---

export interface NumericInput {
  x?: number;
  y?: number;
  w?: number;
  h?: number;
  /** Percent of the asset's pixel size, about the center (Scale field). */
  scalePercent?: number;
  rotation?: number;
  opacity?: number;
  sampling?: Sampling;
  /** Locks the other side of w/h when exactly one of them is given. */
  lockRatio?: boolean;
}

export type CommitOutcome =
  | { kind: "transform"; transform: Transform }
  | { kind: "distort"; corners: Vec2[] }
  | { kind: "unchanged" };

export interface BeginTransformOptions {
  /** The layer's pixel resolution — what 100 % scale draws 1:1. Read-only. */
  assetSize?: [number, number];
}

/**
 * An open transform edit over a starting transform. `beginDrag` + `drag`
 * carry a handle gesture; `preview`/`applyNumeric`/`nudge`/`flip` carry
 * inspector and keyboard edits; `commit` applies the draft and `cancel`
 * restores the start. The session never touches anything but the Transform.
 */
export class TransformSession {
  readonly start: Transform;
  readonly assetSize: [number, number] | null;

  private _draft: Transform;
  private _active = true;
  private _mode: DragMode | null = null;
  private _dragStart: Vec2 | null = null;
  private _corners: Vec2[] | null = null;
  private _snapGuides: SnapHint[] = [];
  private _opacity: number | null = null;

  constructor(start: Transform, options: BeginTransformOptions = {}) {
    this.start = cloneTransform(start);
    this._draft = cloneTransform(start);
    this.assetSize = options.assetSize ? ([...options.assetSize] as [number, number]) : null;
  }

  get draft(): Transform {
    return this._draft;
  }

  get active(): boolean {
    return this._active;
  }

  get mode(): DragMode | null {
    return this._mode;
  }

  /** The distorted corner quad while a ⌘-drag reshapes the frame; null otherwise. */
  get corners(): Vec2[] | null {
    return this._corners;
  }

  /** Guide lines the current gesture snapped to; cleared on commit/cancel. */
  get snapGuides(): SnapHint[] {
    return this._snapGuides;
  }

  /** Opacity typed alongside the numeric fields; lives outside the Transform. */
  get opacity(): number | null {
    return this._opacity;
  }

  /** Starts (or restarts) a handle drag from `startPoint` (document pixels). */
  beginDrag(mode: DragMode, startPoint: Vec2): boolean {
    if (!this._active) return false;
    this._mode = mode;
    this._dragStart = { x: startPoint.x, y: startPoint.y };
    this._corners = mode.kind === "distort" ? transformCorners(this._draft) : null;
    return true;
  }

  /**
   * Drags by `delta` from the drag's start point and returns the preview.
   * The pipeline mirrors EditorCanvas.mouseDragged: resize snaps the pointer
   * (upright only), the drag result rounds to whole pixels and degrees, then
   * a move snaps the rounded draft; rotation is never snapped; Ctrl vetoes.
   */
  drag(delta: Vec2, mods: DragModifiers = {}, snapInput?: SnapInput): Transform | null {
    if (!this._active || !this._mode || !this._dragStart) return null;
    const point = { x: this._dragStart.x + delta.x, y: this._dragStart.y + delta.y };
    if (this._mode.kind === "distort") {
      const next = dragCorners(this._corners ?? [], this._mode, this._dragStart, point, mods.shift === true);
      if (next) this._corners = next;
      return this._draft;
    }
    const m: DragModifiers = { lockRatio: true, ...mods };
    let guides: SnapHint[] = [];
    let target = point;
    if (this._mode.kind === "resize" && !m.ctrl && snapInput) {
      const snapped = snappedResizePointer(
        point,
        { original: this.start, start: this._dragStart, handle: this._mode.handle },
        (m.lockRatio !== false) !== (m.shift === true),
        snapInput,
        (p) => dragUpdated(this.start, this._mode!, this._dragStart!, p, m),
      );
      target = snapped.point;
      guides = snapped.guides;
    }
    let draft = roundedTransform(dragUpdated(this.start, this._mode, this._dragStart, target, m));
    if (this._mode.kind === "move" && !m.ctrl && snapInput) {
      const snapped = snappedMoveTransform(draft, snapInput);
      draft = snapped.transform;
      guides = snapped.guides;
    }
    this._draft = draft;
    this._snapGuides = guides;
    return this._draft;
  }

  /**
   * previewTransform: takes a whole replacement draft, but only a valid one —
   * invalid values leave the previous draft in place.
   */
  preview(value: Transform): boolean {
    if (!this._active || !isValidTransform(value)) return false;
    this._draft = cloneTransform(value);
    return true;
  }

  /** nudgeLayer while an edit is already open: shifts the draft's origin. */
  nudge(dx: number, dy: number): boolean {
    if (!this._active) return false;
    const moved: Transform = { ...cloneTransform(this._draft), origin: [this._draft.origin[0] + dx, this._draft.origin[1] + dy] };
    if (!isValidTransform(moved)) return false;
    this._draft = moved;
    return true;
  }

  /**
   * The TransformInspector's fields (X/Y/W/H/Scale/°/Sampling), applied in
   * field order. Each field is previewed on its own: a value that would make
   * the placement invalid is skipped, the others still apply. Typed values
   * are used as they are — no rounding, so a fraction can be asked for by
   * hand, in the same document-pixel/degree units a drag leaves behind.
   */
  applyNumeric(input: NumericInput): boolean {
    if (!this._active) return false;
    let applied = false;
    const tryApply = (mutate: (t: Transform) => void): boolean => {
      const candidate = cloneTransform(this._draft);
      mutate(candidate);
      if (!isValidTransform(candidate)) return false;
      this._draft = candidate;
      return true;
    };
    if (input.x !== undefined && Number.isFinite(input.x)) {
      const x = clamp(input.x, -FIELD_LIMITS.origin, FIELD_LIMITS.origin);
      applied = tryApply((t) => { t.origin = [x, t.origin[1]]; }) || applied;
    }
    if (input.y !== undefined && Number.isFinite(input.y)) {
      const y = clamp(input.y, -FIELD_LIMITS.origin, FIELD_LIMITS.origin);
      applied = tryApply((t) => { t.origin = [t.origin[0], y]; }) || applied;
    }
    if (input.w !== undefined && Number.isFinite(input.w) && input.w >= 1) {
      const w = Math.min(input.w, FIELD_LIMITS.size);
      applied = tryApply((t) => {
        const size: [number, number] = [t.size[0], t.size[1]];
        if (input.lockRatio === true && input.h === undefined) size[1] = (size[1] * w) / size[0];
        size[0] = w;
        t.size = size;
      }) || applied;
    }
    if (input.h !== undefined && Number.isFinite(input.h) && input.h >= 1) {
      const h = Math.min(input.h, FIELD_LIMITS.size);
      applied = tryApply((t) => {
        const size: [number, number] = [t.size[0], t.size[1]];
        if (input.lockRatio === true && input.w === undefined) size[0] = (size[0] * h) / size[1];
        size[1] = h;
        t.size = size;
      }) || applied;
    }
    if (input.scalePercent !== undefined && Number.isFinite(input.scalePercent) && input.scalePercent > 0) {
      const percent = clamp(input.scalePercent, FIELD_LIMITS.scalePercentMin, FIELD_LIMITS.scalePercentMax);
      const pixelSize = this.assetSize ?? [this._draft.size[0], this._draft.size[1]] as [number, number];
      applied = tryApply((t) => {
        const scaled = scaledToPercent(t, percent, pixelSize);
        t.origin = scaled.origin;
        t.size = scaled.size;
      }) || applied;
    }
    if (input.rotation !== undefined && Number.isFinite(input.rotation)) {
      const rotation = normalizeRotation(clamp(input.rotation, -FIELD_LIMITS.rotation, FIELD_LIMITS.rotation));
      applied = tryApply((t) => { t.rotation = rotation; }) || applied;
    }
    const sampling = input.sampling;
    if (sampling !== undefined && SAMPLINGS.includes(sampling)) {
      applied = tryApply((t) => { t.sampling = sampling; }) || applied;
    }
    if (input.opacity !== undefined && Number.isFinite(input.opacity)) {
      this._opacity = clamp(input.opacity, 0, 1);
      applied = true;
    }
    return applied;
  }

  /** The inspector's Flip H / Flip V buttons: toggle the draft's flip flag. */
  flip(axis: "x" | "y"): boolean {
    if (!this._active) return false;
    if (axis === "x") this._draft.flipX = !this._draft.flipX;
    else this._draft.flipY = !this._draft.flipY;
    return true;
  }

  /**
   * commitTransform: applies the draft as one edit and closes the session. A
   * draft that is invalid or unchanged from the start reports "unchanged" —
   * the document keeps the original, matching Swift's no-op guards. A
   * ⌘-distorted edit reports its corner quad instead (the resampler's input).
   */
  commit(): CommitOutcome {
    if (!this._active) return { kind: "unchanged" };
    this._active = false;
    this._mode = null;
    this._dragStart = null;
    this._snapGuides = [];
    const corners = this._corners;
    this._corners = null;
    if (corners) return { kind: "distort", corners };
    if (!isValidTransform(this._draft) || transformEquals(this._draft, this.start)) {
      return { kind: "unchanged" };
    }
    return { kind: "transform", transform: cloneTransform(this._draft) };
  }

  /** cancelTransform: throws the draft away; the start transform stands. */
  cancel(): Transform {
    const restored = cloneTransform(this.start);
    if (!this._active) return restored;
    this._active = false;
    this._mode = null;
    this._dragStart = null;
    this._corners = null;
    this._snapGuides = [];
    return restored;
  }
}

/** Opens a transform edit over `start` (Swift beginTransform). */
export function beginTransform(start: Transform, options: BeginTransformOptions = {}): TransformSession {
  return new TransformSession(start, options);
}

/**
 * Show Controls (⌘H): the transform box and handles visibility. When hidden,
 * a drag anywhere moves the layer.
 */
export const showsTransformControls = writable<boolean>(true);
