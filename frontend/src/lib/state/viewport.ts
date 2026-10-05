import { writable } from "svelte/store";

/**
 * Canvas viewport — semantic port of reference/Swift/Compositor/Rendering/
 * CanvasViewport.swift (macOS original, read-only source of truth).
 *
 * Coordinate spaces: the document is in pixels with a top-left origin; the
 * view is in CSS pixels (the browser's "points"). Zoom 1 means actual display
 * pixels — on a HiDPI screen `backingScale = devicePixelRatio` converts.
 * All functions are pure: they take and return ViewportState so the math is
 * testable without a DOM, and the store actions below just thread the state.
 */

/** Zoom limits, CanvasViewport.swift:11 (`static let zoomRange = 0.001...32`). */
export const ZOOM_MIN = 0.001;
export const ZOOM_MAX = 32;

/**
 * The 17 keyboard zoom levels, verbatim from CanvasViewport.swift:12-14:
 * [0.125, 1/6, 0.25, 1/3, 0.5, 2/3, 1, 1.25, 1.5, 2, 3, 4, 5, 6, 8, 12, 16].
 * The fractions are kept as divisions (not rounded decimals) to match the
 * original CGFloat values bit-for-bit.
 */
export const KEYBOARD_ZOOM_LEVELS: readonly number[] = [
  0.125, 1 / 6, 0.25, 1 / 3, 0.5, 2 / 3, 1, 1.25, 1.5, 2, 3, 4, 5, 6, 8, 12, 16,
];

/**
 * The pixel grid appears from 800% (EditorCanvas.swift:920,
 * `static let pixelGridZoom: CGFloat = 8` — "the pixel grid appears from 800%").
 */
export const PIXEL_GRID_ZOOM = 8;

/**
 * Hard-edged document pixels are shown from 200% (EditorCanvas.swift:923,
 * `static let crispZoom: CGFloat = 2`).
 */
export const CRISP_ZOOM = 2;

export interface ViewportState {
  /** View (stage) size in CSS pixels. */
  viewWidth: number;
  viewHeight: number;
  /** Device pixels per CSS pixel; clamped at 1, as in CanvasViewport.swift:47. */
  backingScale: number;
  zoom: number;
  /** Scroll offset: screen-space shift of the document's center. */
  panX: number;
  panY: number;
  /** True while the view still tracks the Fit command. */
  followsFit: boolean;
}

export const INITIAL_VIEWPORT: ViewportState = {
  viewWidth: 0,
  viewHeight: 0,
  backingScale: 1,
  zoom: 1,
  panX: 0,
  panY: 0,
  followsFit: true,
};

export interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface Point {
  x: number;
  y: number;
}

function clampZoom(value: number): number {
  return Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, value));
}

/** Screen pixels per document pixel — CanvasViewport.swift:15. */
export function pointsPerPixel(s: ViewportState): number {
  return s.zoom / s.backingScale;
}

/**
 * The document's rect in the view: centered, plus the pan offset —
 * CanvasViewport.swift:18-23.
 */
export function documentRect(
  s: ViewportState,
  docWidth: number,
  docHeight: number,
): Rect {
  const ppp = pointsPerPixel(s);
  const w = docWidth * ppp;
  const h = docHeight * ppp;
  return {
    x: s.viewWidth / 2 - w / 2 + s.panX,
    y: s.viewHeight / 2 - h / 2 + s.panY,
    width: w,
    height: h,
  };
}

/** View point -> document pixel — CanvasViewport.swift:25-28. */
export function documentPoint(
  s: ViewportState,
  viewX: number,
  viewY: number,
  docWidth: number,
  docHeight: number,
): Point {
  const origin = documentRect(s, docWidth, docHeight);
  const ppp = pointsPerPixel(s);
  return { x: (viewX - origin.x) / ppp, y: (viewY - origin.y) / ppp };
}

/** Document pixel -> view point — CanvasViewport.swift:30-33. */
export function viewPoint(
  s: ViewportState,
  docX: number,
  docY: number,
  docWidth: number,
  docHeight: number,
): Point {
  const origin = documentRect(s, docWidth, docHeight);
  const ppp = pointsPerPixel(s);
  return { x: origin.x + docX * ppp, y: origin.y + docY * ppp };
}

/**
 * Zoom to `value` keeping the document point under the screen anchor exactly
 * where it is — CanvasViewport.swift:56-64. Order matters: read the anchor's
 * document point *before* changing zoom, then correct the pan by whatever the
 * anchor drifted after the zoom.
 */
export function setZoom(
  s: ViewportState,
  value: number,
  anchorX: number,
  anchorY: number,
  docWidth: number,
  docHeight: number,
): ViewportState {
  if (!Number.isFinite(value)) return s; // CanvasViewport.swift:57
  const pixel = documentPoint(s, anchorX, anchorY, docWidth, docHeight);
  const zoom = clampZoom(value);
  const after: ViewportState = { ...s, zoom, followsFit: false };
  const moved = viewPoint(after, pixel.x, pixel.y, docWidth, docHeight);
  after.panX += anchorX - moved.x;
  after.panY += anchorY - moved.y;
  return after;
}

/**
 * The next ladder level in `step` direction. The tolerance
 * `max(1e-9, |zoom| * 1e-9)` (CanvasViewport.swift:68) makes a zoom that only
 * drifted onto a level by float error still count as *at* that level, so
 * repeated +/- steps never stall.
 */
export function keyboardZoomTarget(s: ViewportState, step: number): number {
  if (step === 0) return s.zoom;
  const tolerance = Math.max(0.000000001, Math.abs(s.zoom) * 0.000000001);
  if (step > 0) {
    for (const level of KEYBOARD_ZOOM_LEVELS) {
      if (level > s.zoom + tolerance) return level;
    }
    return s.zoom;
  }
  for (let i = KEYBOARD_ZOOM_LEVELS.length - 1; i >= 0; i--) {
    const level = KEYBOARD_ZOOM_LEVELS[i];
    if (level < s.zoom - tolerance) return level;
  }
  return s.zoom;
}

/**
 * Fit — CanvasViewport.swift:35-41. The 96 margin leaves room around the
 * document; `max(1, view - 96)` keeps tiny views dividing by at least 1.
 * With no view size yet, only the followsFit intent is recorded.
 */
export function fit(
  s: ViewportState,
  docWidth: number,
  docHeight: number,
): ViewportState {
  if (s.viewWidth <= 0 || s.viewHeight <= 0 || docWidth <= 0 || docHeight <= 0) {
    return { ...s, followsFit: true };
  }
  const zoom = clampZoom(
    Math.min(
      Math.max(1, s.viewWidth - 96) / docWidth,
      Math.max(1, s.viewHeight - 96) / docHeight,
    ) * s.backingScale,
  );
  return { ...s, zoom, panX: 0, panY: 0, followsFit: true };
}

/**
 * View resize — CanvasViewport.swift:43-54. While following fit, re-fit;
 * otherwise scale the pan by the pointsPerPixel ratio so the *center document
 * point* (not the corner) stays put when moving between displays.
 */
export function resize(
  s: ViewportState,
  viewWidth: number,
  viewHeight: number,
  backingScale: number,
  docWidth: number,
  docHeight: number,
): ViewportState {
  const oldScale = pointsPerPixel(s);
  const sized: ViewportState = {
    ...s,
    viewWidth,
    viewHeight,
    backingScale: Math.max(1, backingScale),
  };
  if (sized.followsFit) return fit(sized, docWidth, docHeight);
  const ratio = pointsPerPixel(sized) / oldScale;
  return { ...sized, panX: sized.panX * ratio, panY: sized.panY * ratio };
}

/** Scroll/trackpad pan — CanvasViewport.swift:75-79. */
export function translateBy(
  s: ViewportState,
  deltaX: number,
  deltaY: number,
): ViewportState {
  return {
    ...s,
    panX: s.panX + deltaX,
    panY: s.panY + deltaY,
    followsFit: false,
  };
}

/** Alias keeping the port's vocabulary explicit at call sites. */
export const panBy = translateBy;

/** Whether the per-document-pixel grid is visible at this zoom. */
export function showsPixelGrid(zoom: number): boolean {
  return zoom >= PIXEL_GRID_ZOOM;
}

/**
 * Reactive viewport state (view state lives in the frontend; the document
 * itself stays in Go). CanvasSurface drives it through the actions below.
 */
export const viewport = writable<ViewportState>({ ...INITIAL_VIEWPORT });

export function fitViewport(docWidth: number, docHeight: number): void {
  viewport.update((s) => fit(s, docWidth, docHeight));
}

export function zoomAt(
  value: number,
  anchorX: number,
  anchorY: number,
  docWidth: number,
  docHeight: number,
): void {
  viewport.update((s) => setZoom(s, value, anchorX, anchorY, docWidth, docHeight));
}

/** Actual-pixels / Fit-style commands anchor at the view center. */
export function zoomToValue(
  value: number,
  docWidth: number,
  docHeight: number,
): void {
  viewport.update((s) =>
    setZoom(s, value, s.viewWidth / 2, s.viewHeight / 2, docWidth, docHeight),
  );
}

/** One +/- step on the keyboard ladder, anchored at the view center. */
export function keyboardZoom(
  step: number,
  docWidth: number,
  docHeight: number,
): void {
  viewport.update((s) => {
    const target = keyboardZoomTarget(s, step);
    if (target === s.zoom) return s;
    return setZoom(s, target, s.viewWidth / 2, s.viewHeight / 2, docWidth, docHeight);
  });
}

export function panByStore(deltaX: number, deltaY: number): void {
  viewport.update((s) => translateBy(s, deltaX, deltaY));
}

export function resizeViewport(
  viewWidth: number,
  viewHeight: number,
  backingScale: number,
  docWidth: number,
  docHeight: number,
): void {
  viewport.update((s) =>
    resize(s, viewWidth, viewHeight, backingScale, docWidth, docHeight),
  );
}

export function resetViewport(): void {
  viewport.set({ ...INITIAL_VIEWPORT });
}
