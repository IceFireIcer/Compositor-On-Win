/**
 * Selection state machine, in document space (pure TS — no viewport or
 * document imports). Ticket 15; the interaction semantics mirror the
 * original SelectionTests: masks are 0/255 byte bitmaps over whole document
 * pixels, outlines are closed polygon point sets in pixel-edge coordinates
 * for the marching-ants overlay, and an empty selection stays distinct
 * from no selection (null).
 *
 * The mask byte math mirrors the Go CPU kernels (internal/render/wand.go
 * ports the magic wand; the pixel-edge outline walk is the same algorithm
 * as WandTrace), so front and back agree on what "selected" means.
 */

export interface Point {
  x: number;
  y: number;
}

/** A freshly drawn region before it is combined with the current selection. */
export interface Region {
  width: number;
  height: number;
  mask: Uint8Array; // width*height, 255 selected / 0 elsewhere
  loops: Point[][]; // closed outlines for the ants
}

export interface Selection extends Region {
  /** Present-but-empty stays distinct from null (no selection). */
  active: boolean;
  /** Feather radius in pixels (ticket 16): a scalar, applied at render time. */
  feather?: number;
}

/** replace / shift-click add / option-click subtract. */
export type SelectionMode = "replace" | "add" | "subtract";

import { writable } from "svelte/store";

/** The active document-space selection (marching ants + filter payloads).
 * null = no selection; filters then run on the whole layer. */
export const currentSelection = writable<Selection | null>(null);

/** Builds the selection from a doc-space gray mask and stores it with its
 * outlined loops (the model-backed Select Subject / Object Selection). */
export function setSelectionFromMaskData(mask: Uint8Array, width: number, height: number): Selection {
  const selection = selectionFromMask(mask, width, height);
  currentSelection.set(selection);
  return selection;
}

/** The bridge's selection payload: base64 gray mask over the doc rect. */
export function selectionPayload(mask: Uint8Array, width: number, height: number): string {
  let binary = "";
  const chunk = 0x8000;
  for (let i = 0; i < mask.length; i += chunk) {
    binary += String.fromCharCode(...mask.subarray(i, i + chunk));
  }
  return JSON.stringify({ x: 0, y: 0, w: width, h: height, mask: btoa(binary) });
}

/** Clears the active selection. */
export function clearSelection(): void {
  currentSelection.set(null);
}

/** Scroll starts this many pixels from a viewport edge while dragging. */
export const EDGE_SCROLL_MARGIN = 32;

/** Modifier semantics of the original selectionMode(shift:option:). */
export function selectionMode(shift: boolean, option: boolean): SelectionMode {
  if (shift && !option) return "add";
  if (option) return "subtract";
  return "replace";
}

/**
 * Fills closed polygons (pixel-edge coordinates) into a mask with the
 * even-odd rule sampled at pixel centers — the same traversal the Go
 * WandTrace outlines satisfy, so refilling traced loops reproduces the
 * mask exactly. Used by every region builder, which keeps a region's
 * mask and its ants path consistent by construction.
 */
export function fillLoops(loops: Point[][], width: number, height: number): Uint8Array {
  const mask = new Uint8Array(width * height);
  if (width <= 0 || height <= 0) return mask;
  for (let y = 0; y < height; y++) {
    const cy = y + 0.5;
    const crossings: number[] = [];
    for (const loop of loops) {
      const n = loop.length;
      for (let i = 0; i < n; i++) {
        const a = loop[i];
        const b = loop[(i + 1) % n];
        // Half-open span [a.y, b.y): horizontal edges never count, and a
        // vertex shared by two loops is counted exactly once.
        if ((a.y <= cy && b.y > cy) || (b.y <= cy && a.y > cy)) {
          crossings.push(a.x + ((cy - a.y) / (b.y - a.y)) * (b.x - a.x));
        }
      }
    }
    crossings.sort((p, q) => p - q);
    for (let k = 0; k + 1 < crossings.length; k += 2) {
      // Pixel x covers [x, x+1); its center x+0.5 lies in [start, end).
      const from = Math.max(0, Math.ceil(crossings[k] - 0.5));
      const to = Math.min(width - 1, Math.ceil(crossings[k + 1] - 0.5) - 1);
      for (let x = from; x <= to; x++) mask[y * width + x] = 255;
    }
  }
  return mask;
}

/**
 * Outlines the nonzero pixels of `mask` along pixel edges as closed loops
 * of corner points: outer boundaries clockwise, holes counterclockwise in
 * top-left coordinates. Port of the Go WandTrace walk (WandPixels.c).
 */
export function outlineLoops(mask: Uint8Array, width: number, height: number): Point[][] {
  const EAST = 1,
    SOUTH = 2,
    WEST = 4,
    NORTH = 8;
  const stride = width + 1;
  const out = new Uint8Array(stride * (height + 1));
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      if (!mask[y * width + x]) continue;
      if (y === 0 || !mask[(y - 1) * width + x]) out[y * stride + x] |= EAST;
      if (x + 1 === width || !mask[y * width + x + 1]) out[y * stride + x + 1] |= SOUTH;
      if (y + 1 === height || !mask[(y + 1) * width + x]) out[(y + 1) * stride + x + 1] |= WEST;
      if (x === 0 || !mask[y * width + x - 1]) out[(y + 1) * stride + x] |= NORTH;
    }
  }
  const turnRight = (d: number): number => (d === NORTH ? EAST : d << 1);
  const turnLeft = (d: number): number => (d === EAST ? NORTH : d >> 1);

  const loops: Point[][] = [];
  for (let start = 0; start < out.length; start++) {
    while (out[start]) {
      const loop: Point[] = [];
      let v = start;
      let heading = 0;
      let initial = 0;
      for (;;) {
        const bits = out[v];
        // Where two loops meet at a corner, turning right keeps them apart.
        let d: number;
        if (!heading) d = bits & -bits;
        else if (bits & turnRight(heading)) d = turnRight(heading);
        else if (bits & heading) d = heading;
        else if (bits & turnLeft(heading)) d = turnLeft(heading);
        else d = bits & -bits;
        if (!d) break;
        out[v] &= ~d;
        if (d !== heading) loop.push({ x: v % stride, y: Math.floor(v / stride) });
        if (!heading) initial = d;
        heading = d;
        v = d === EAST ? v + 1 : d === WEST ? v - 1 : d === SOUTH ? v + stride : v - stride;
        if (v === start) break;
      }
      // The start is a corner unless the loop arrives on the heading it left with.
      if (heading === initial && loop.length > 0) loop.shift();
      loops.push(loop);
    }
  }
  return loops;
}

/** A whole-pixel rectangle from a drag, in any direction (rounded ends). */
function wholePixelBox(
  start: Point,
  end: Point,
  square: boolean,
  fromCenter: boolean,
): { x0: number; y0: number; x1: number; y1: number } | null {
  let ax = start.x;
  let ay = start.y;
  let bx = end.x;
  let by = end.y;
  if (fromCenter) {
    ax = 2 * start.x - end.x;
    ay = 2 * start.y - end.y;
  }
  if (square) {
    const side = Math.max(Math.abs(bx - ax), Math.abs(by - ay));
    if (fromCenter) {
      ax = start.x - side / 2;
      ay = start.y - side / 2;
      bx = start.x + side / 2;
      by = start.y + side / 2;
    } else {
      if (bx >= ax) bx = ax + side;
      else ax = bx - side;
      if (by >= ay) by = ay + side;
      else ay = by - side;
    }
  }
  const x0 = Math.round(Math.min(ax, bx));
  const y0 = Math.round(Math.min(ay, by));
  const x1 = Math.round(Math.max(ax, bx));
  const y1 = Math.round(Math.max(ay, by));
  if (x1 <= x0 || y1 <= y0) return null; // a click, or a degenerate drag
  return { x0, y0, x1, y1 };
}

function regionFromLoop(loop: Point[], width: number, height: number): Region {
  return { width, height, mask: fillLoops([loop], width, height), loops: [loop] };
}

/** Rectangular marquee: whole-pixel rectangles drawn in any direction. */
export function rectRegion(
  start: Point,
  end: Point,
  width: number,
  height: number,
  opts: { square?: boolean; fromCenter?: boolean } = {},
): Region {
  const box = wholePixelBox(start, end, opts.square === true, opts.fromCenter === true);
  if (!box) return { width, height, mask: new Uint8Array(width * height), loops: [] };
  const loop: Point[] = [
    { x: box.x0, y: box.y0 },
    { x: box.x1, y: box.y0 },
    { x: box.x1, y: box.y1 },
    { x: box.x0, y: box.y1 },
  ];
  return regionFromLoop(loop, width, height);
}

/** Elliptical marquee: the oval inscribed in the drag's box. */
export function ellipseRegion(
  start: Point,
  end: Point,
  width: number,
  height: number,
  opts: { square?: boolean; fromCenter?: boolean } = {},
): Region {
  const box = wholePixelBox(start, end, opts.square === true, opts.fromCenter === true);
  if (!box) return { width, height, mask: new Uint8Array(width * height), loops: [] };
  const cx = (box.x0 + box.x1) / 2;
  const cy = (box.y0 + box.y1) / 2;
  const rx = (box.x1 - box.x0) / 2;
  const ry = (box.y1 - box.y0) / 2;
  const segments = 64;
  const loop: Point[] = [];
  for (let i = 0; i < segments; i++) {
    const angle = (2 * Math.PI * i) / segments;
    loop.push({ x: cx + rx * Math.cos(angle), y: cy + ry * Math.sin(angle) });
  }
  return regionFromLoop(loop, width, height);
}

/** Freehand / polygonal lasso: the point set closes into a polygon. */
export function lassoRegion(points: Point[], width: number, height: number): Region {
  if (points.length < 3) return { width, height, mask: new Uint8Array(width * height), loops: [] };
  return regionFromLoop(points.map((p) => ({ ...p })), width, height);
}

/** Rebuilds a selection's outlines from its mask (e.g. after a wand fill). */
export function selectionFromMask(mask: Uint8Array, width: number, height: number): Selection {
  return { width, height, mask, loops: outlineLoops(mask, width, height), active: true };
}

function hasSelectedPixels(mask: Uint8Array): boolean {
  for (let i = 0; i < mask.length; i++) {
    if (mask[i] === 255) return true;
  }
  return false;
}

export function isEmpty(selection: Selection): boolean {
  return !hasSelectedPixels(selection.mask);
}

/**
 * Combines a freshly drawn region into the current selection — the
 * replace/add/subtract modifier semantics as one pure byte-wise entry
 * point. Replace with nothing (a click) deselects; subtracting with no
 * selection stays no selection; subtracting everything leaves a present,
 * empty selection, distinct from null.
 */
export function select(mode: SelectionMode, current: Selection | null, region: Region): Selection | null {
  if (mode === "replace" && !hasSelectedPixels(region.mask)) return null;
  if (mode === "subtract" && !current) return null;
  const width = current?.width ?? region.width;
  const height = current?.height ?? region.height;
  const base = current?.mask;
  const next = region.mask;
  const mask = new Uint8Array(width * height);
  for (let i = 0; i < mask.length; i++) {
    const a = base !== undefined && i < base.length ? base[i] : 0;
    const b = i < next.length ? next[i] : 0;
    if (mode === "add") mask[i] = a === 255 || b === 255 ? 255 : 0;
    else if (mode === "subtract") mask[i] = b === 255 ? 0 : a;
    else mask[i] = b;
  }
  return { width, height, mask, loops: outlineLoops(mask, width, height), active: true };
}

/**
 * Moves a selection by whole pixels: the outline (the ants and the shape's
 * truth) translates unclipped — moving off-canvas and back keeps the whole
 * shape — while the mask coverage re-rasterizes clipped to the canvas.
 * Pure: returns a new selection, the input is untouched.
 */
export function offsetSelection(selection: Selection, dx: number, dy: number): Selection {
  const loops = selection.loops.map((loop) => loop.map((p) => ({ x: p.x + dx, y: p.y + dy })));
  return {
    width: selection.width,
    height: selection.height,
    mask: fillLoops(loops, selection.width, selection.height),
    loops,
    active: selection.active,
  };
}

/**
 * Edge auto-scroll while dragging: `edgeDist` is the signed distance in
 * pixels to the viewport edge being approached (negative = past it) and
 * `speed` the full scroll rate. Returns the scroll delta — proportional
 * inside the margin, full speed at/past the edge, and 0 once the pointer
 * leaves the margin, which stops the scroll.
 */
export function edgeAutoScroll(edgeDist: number, speed: number): number {
  if (edgeDist >= EDGE_SCROLL_MARGIN) return 0;
  const dist = Math.max(edgeDist, 0);
  return speed * (1 - dist / EDGE_SCROLL_MARGIN);
}
