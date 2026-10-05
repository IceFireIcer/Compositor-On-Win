import { Modifier, has } from "../state/modifiers";

/**
 * Tool + modifier keys -> CSS cursor. Visual port of the NSCursor family
 * drawn in EditorCanvas.swift (lines 129-517: move/duplicate/distort/rotate
 * cursors, selection-badged crosshairs, wand, eyedropper, zoom in/out).
 *
 * The macOS originals are hand-drawn NSImages; browsers cannot reuse those,
 * so every composite cursor is rebuilt as a small inline SVG data URI with a
 * CSS keyword fallback. These are *visual-smoke-level approximations*: same
 * semantics (crosshair with a "+" / "-" badge for add/subtract selection,
 * magnifier for zoom, dropper, rotate arrows), simplified shapes.
 */

/** One inline SVG wrapped into a CSS cursor with an explicit hotspot. */
function svgCursor(svg: string, fallback: string, hotX: number, hotY: number): string {
  return `url("data:image/svg+xml;utf8,${encodeURIComponent(svg)}") ${hotX} ${hotY}, ${fallback}`;
}

function svgWrap(body: string, size: number): string {
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">${body}</svg>`;
}

/** White under-stroke + black top-stroke: reads on any image (as in Swift). */
function outlined(d: string, white: number, black: number): string {
  return (
    `<path d="${d}" fill="none" stroke="#fff" stroke-width="${white}" stroke-linecap="round" stroke-linejoin="round"/>` +
    `<path d="${d}" fill="none" stroke="#000" stroke-width="${black}" stroke-linecap="round" stroke-linejoin="round"/>`
  );
}

// --- Zoom (EditorCanvas.swift:493-504, lens hotSpot at (10,10)) -------------

function zoomCursor(out: boolean): string {
  const mark = out ? "M7 10h6" : "M7 10h6M10 7v6";
  const body =
    `<circle cx="10" cy="10" r="6" fill="#fff" stroke="#000" stroke-width="2"/>` +
    `<path d="M14.5 14.5L20 20" stroke="#000" stroke-width="2.5" stroke-linecap="round"/>` +
    `<path d="${mark}" fill="none" stroke="#000" stroke-width="2" stroke-linecap="round"/>`;
  return svgCursor(svgWrap(body, 24), out ? "zoom-out" : "zoom-in", 10, 10);
}

export const ZOOM_IN_CURSOR = zoomCursor(false);
export const ZOOM_OUT_CURSOR = zoomCursor(true);

// --- Eyedropper (EditorCanvas.swift:435-460, tip at bottom-left) -------------

const DROPPER = "M19 5L7 17l-2 3 3-2L20 6z";

function eyedropperCursor(badge: "none" | "add" | "remove"): string {
  const badgeMark =
    badge === "add"
      ? `<path d="M15 17h4M17 15v4" fill="none" stroke="#000" stroke-width="1.6" stroke-linecap="round"/>`
      : badge === "remove"
        ? `<path d="M15 17h4" fill="none" stroke="#000" stroke-width="1.6" stroke-linecap="round"/>`
        : "";
  const body =
    outlined(DROPPER, 4.5, 2.2) +
    (badgeMark
      ? `<circle cx="17" cy="17" r="5.5" fill="#fff" stroke="#000" stroke-width="1"/>` +
        badgeMark
      : "");
  return svgCursor(svgWrap(body, 24), "crosshair", 5, 19);
}

export const EYEDROPPER_CURSOR = eyedropperCursor("none");
export const EYEDROPPER_ADD_CURSOR = eyedropperCursor("add");
export const EYEDROPPER_REMOVE_CURSOR = eyedropperCursor("remove");

// --- Rotate (EditorCanvas.swift rotationCursor, hotSpot (12,12)) -------------

const ROTATE_ARC = "M19 12a7 7 0 1 1-2.05-4.95";
const ROTATE_HEAD = "M19 3v4.5h-4.5";
export const ROTATE_CURSOR = svgCursor(
  svgWrap(outlined(ROTATE_ARC, 4, 2) + outlined(ROTATE_HEAD, 4, 2), 24),
  "crosshair",
  12,
  12,
);

// --- Selection crosshairs (EditorCanvas.swift:263-286 + wand at 408-440) -----
// Crosshair with the tool's icon beside it and a "+" (add) or "-" (subtract)
// badge, as Photoshop shows. The crosshair center stays the hot spot, because
// that is where the selection lands.

export type SelectionIcon = "marquee" | "lasso" | "wand";
export type SelectionMode = "replace" | "add" | "subtract";

const CROSSHAIR = outlined("M8 2v12M2 8h12", 3.2, 1.2);

/** The tool's icon in a small box below-right of the crosshair. */
function selectionIcon(icon: SelectionIcon): string {
  if (icon === "marquee") {
    const box = "M10 10h10v10h-10z";
    return (
      `<path d="${box}" fill="none" stroke="#fff" stroke-width="2.4"/>` +
      `<path d="${box}" fill="none" stroke="#000" stroke-width="1" stroke-dasharray="2 1.5"/>`
    );
  }
  if (icon === "lasso") {
    const loop = "M10 15a5 5 0 1 1 10 0a5 5 0 0 1-8 4l-2 2";
    return (
      `<path d="${loop}" fill="none" stroke="#fff" stroke-width="2.4" stroke-linecap="round"/>` +
      `<path d="${loop}" fill="none" stroke="#000" stroke-width="1" stroke-dasharray="2 1.5" stroke-linecap="round"/>`
    );
  }
  // Wand: sparkle at the hot spot, stick angled away (EditorCanvas.swift:410-420).
  return (
    outlined("M12 12L20 20", 5, 2.4) +
    outlined("M8 10l2-2M10 8L8 10M8 12l1.5-1.5M9.5 10.5L8 12", 3.2, 1.2)
  );
}

/** "+" badge (with a literal + in the SVG so tests can tell it from "-"). */
const ADD_BADGE =
  "<title>+</title>" +
  outlined("M22 16h-4M20 14v4", 3.2, 1.2);

const SUBTRACT_BADGE = outlined("M22 16h-4", 3.2, 1.2);

export function selectionCursor(icon: SelectionIcon, mode: SelectionMode): string {
  const badge = mode === "add" ? ADD_BADGE : mode === "subtract" ? SUBTRACT_BADGE : "";
  return svgCursor(
    svgWrap(CROSSHAIR + selectionIcon(icon) + badge, 32),
    "crosshair",
    8,
    8,
  );
}

// --- Composite cursor registry ------------------------------------------------
// The Move tool's four-way badge and the duplicate arrow (EditorCanvas.swift:
// 195-215) have near-equivalent CSS keywords; the rest are data URIs above.

export const MOVE_CURSOR = "move";
export const DUPLICATE_CURSOR = "copy";
export const HAND_CURSOR = "grab";
export const GRABBING_CURSOR = "grabbing";

const COMPOSITE_CURSORS: readonly string[] = [
  ZOOM_IN_CURSOR,
  ZOOM_OUT_CURSOR,
  EYEDROPPER_CURSOR,
  EYEDROPPER_ADD_CURSOR,
  EYEDROPPER_REMOVE_CURSOR,
  ROTATE_CURSOR,
  ...(["marquee", "lasso", "wand"] as const).flatMap((icon) =>
    (["replace", "add", "subtract"] as const).map((mode) => selectionCursor(icon, mode)),
  ),
];

/** Number of distinct composite (badge) cursors — ticket V07 wants ~10. */
export const COMPOSITE_CURSOR_COUNT = new Set(COMPOSITE_CURSORS).size;

export function isDataUriCursor(cursor: string): boolean {
  return cursor.startsWith('url("data:image/svg+xml;utf8,');
}

export interface CursorInput {
  tool: string;
  /** Current modifier combination (see ../state/modifiers). */
  bits: number;
  /** True while a pan drag is in flight (closed hand). */
  panning?: boolean;
}

/**
 * The current tool's cursor over the canvas. Mirrors EditorCanvas.swift
 * `toolCursor` (line 1444): Space or the hand tool wins first, then per-tool
 * mapping; selection tools get Shift = add, Alt = subtract badges.
 */
export function cursorForTool({ tool, bits, panning }: CursorInput): string {
  if (panning) return GRABBING_CURSOR;
  if (has(bits, Modifier.Space)) return HAND_CURSOR;
  switch (tool) {
    case "hand":
      return HAND_CURSOR;
    case "type":
      return "text";
    case "idle":
      return "default";
    case "zoom":
      return has(bits, Modifier.Alt) ? ZOOM_OUT_CURSOR : ZOOM_IN_CURSOR;
    case "eyedropper":
      if (has(bits, Modifier.Shift)) return EYEDROPPER_ADD_CURSOR;
      if (has(bits, Modifier.Alt)) return EYEDROPPER_REMOVE_CURSOR;
      return EYEDROPPER_CURSOR;
    case "marquee":
    case "lasso":
    case "wand": {
      const mode: SelectionMode = has(bits, Modifier.Shift)
        ? "add"
        : has(bits, Modifier.Alt)
          ? "subtract"
          : "replace";
      return selectionCursor(tool, mode);
    }
    case "move":
      // Approximation of moveCursor / duplicateCursor: Alt-drag duplicates.
      return has(bits, Modifier.Alt) ? DUPLICATE_CURSOR : MOVE_CURSOR;
    default:
      // crop/brush/spotHealing/cloneStamp/blur/gradient/shape draw over the
      // canvas with their own overlays; a plain crosshair stands in here.
      // Clone Stamp + Alt picks a source: crosshair (EditorCanvas.swift:1467).
      return "crosshair";
  }
}
