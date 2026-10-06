/**
 * Brush cursor overlay math — pure functions; the Svelte overlay draws what
 * these return.
 *
 * Ported from the semantic truth (read-only):
 *  - BrushCursorOverlay.swift: the circle rect centered on the pointer
 *    (:19) and the dashed hardness ring at the tip's full-strength
 *    fraction (:65-71, shown only while a Shift drag adjusts hardness —
 *    EditorCanvas.updateBrushCursor passes hardness only then).
 *  - EditorCanvas.swift's right-drag tip HUD (:1704-1734): without Shift,
 *    left/right resize — the circle's edge follows the pointer, each view
 *    point moved widening the radius by a view point on screen; with Shift,
 *    hardness spans its full range across 200 view points. The circle stays
 *    where the press landed for the whole drag, and the press's other
 *    setting is left untouched.
 */

/** The options-bar clamp the HUD drag uses (EditorCanvas.swift:1725). */
export const MIN_BRUSH_DIAMETER = 1;
export const MAX_BRUSH_DIAMETER = 2000;
/** "The full range across 200 points" for the Shift hardness drag (:1721). */
export const HARDNESS_DRAG_POINTS = 200;

export interface CursorCircle {
  /** Top-left of the circle's square, in view points. */
  x: number;
  y: number;
  /** Diameter in view points. */
  diameter: number;
}

/**
 * The overlay's circle size in view points — document diameter scaled by the
 * viewport, floored at one view point (EditorCanvas.updateBrushCursor:1516).
 */
export function cursorDiameter(diameter: number, pointsPerPixel: number): number {
  return Math.max(1, diameter * pointsPerPixel);
}

/**
 * The circle the overlay strokes: centered on the pointer, sized in view
 * points (BrushCursorOverlay.update:19 — the rect starts at diameter/2).
 */
export function cursorCircle(
  pointerX: number,
  pointerY: number,
  diameter: number,
  pointsPerPixel: number,
): CursorCircle {
  const d = cursorDiameter(diameter, pointsPerPixel);
  return { x: pointerX - d / 2, y: pointerY - d / 2, diameter: d };
}

/**
 * Radius of the dashed inner ring marking the full-strength fraction of the
 * tip — the circle inset by width·(1 − hardness)/2 (BrushCursorOverlay.swift:
 * 65-71). Null when hardness is 0 (the whole tip feathers, no inner ring),
 * matching the overlay's `hardness > 0` test. Precondition: hardness in [0,1].
 */
export function hardnessRing(circle: CursorCircle, hardness: number): number | null {
  if (!(hardness > 0)) return null;
  return (circle.diameter / 2) * hardness;
}

/** A right-drag tip session, captured at the press (EditorCanvas.swift:1710). */
export interface BrushTipDrag {
  /** Where the press landed; the circle stays here for the whole drag (:1727). */
  start: { x: number; y: number };
  /** Document-pixel diameter at the press. */
  diameter: number;
  /** Hardness at the press, 0–1. */
  hardness: number;
}

/** What one drag move produces: the new settings plus the HUD's presentation. */
export interface BrushTipHud {
  /** Document-pixel diameter, clamped to [1, 2000] (EditorCanvas.swift:1725). */
  diameter: number;
  radius: number;
  /** Hardness, clamped to [0, 1] (:1722). */
  hardness: number;
  /** The HUD shows the hardness ring only while Shift adjusts it (:1519). */
  hardnessShown: boolean;
  mode: "size" | "hardness";
}

/** Captures a right-drag session at the press (EditorCanvas.rightMouseDown). */
export function beginBrushTipDrag(
  startX: number,
  startY: number,
  diameter: number,
  hardness: number,
): BrushTipDrag {
  return { start: { x: startX, y: startY }, diameter, hardness };
}

/**
 * Applies one right-drag move (EditorCanvas.rightMouseDragged:1714-1729).
 *
 * Size drag (no Shift): the circle's edge follows the pointer — each view
 * point moved widens the radius by a view point on screen, i.e. the document
 * diameter grows by 2·Δx / pointsPerPixel (:1724-1725); hardness is left at
 * its press value. Shift drag: hardness moves by Δx across the full range in
 * 200 view points (:1721-1722); diameter is left at its press value. Both
 * read their baseline from the press, so toggling Shift mid-drag never
 * compounds. `pointerX` is in view points; `pointsPerPixel` is the
 * viewport's view points per document pixel.
 */
export function dragBrushTip(
  drag: BrushTipDrag,
  pointerX: number,
  pointsPerPixel: number,
  shift: boolean,
): BrushTipHud {
  const dx = pointerX - drag.start.x;
  if (!shift) {
    // The circle's edge follows the pointer: each point moved widens the
    // radius by a point on screen.
    const perPixel = Math.max(0.0001, pointsPerPixel);
    const raw = drag.diameter + (2 * dx) / perPixel;
    const diameter = Math.min(MAX_BRUSH_DIAMETER, Math.max(MIN_BRUSH_DIAMETER, roundHalfAway(raw)));
    return { diameter, radius: diameter / 2, hardness: drag.hardness, hardnessShown: false, mode: "size" };
  }
  const hardness = Math.min(1, Math.max(0, drag.hardness + dx / HARDNESS_DRAG_POINTS));
  return { diameter: drag.diameter, radius: drag.diameter / 2, hardness, hardnessShown: true, mode: "hardness" };
}

/** Swift's `.rounded()` — halfway cases away from zero (EditorCanvas.swift:1725). */
function roundHalfAway(v: number): number {
  return v < 0 ? -Math.round(-v) : Math.round(v);
}
