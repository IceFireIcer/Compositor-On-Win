import { beforeEach, describe, expect, it } from "vitest";
import { get } from "svelte/store";
import {
  CRISP_ZOOM,
  INITIAL_VIEWPORT,
  KEYBOARD_ZOOM_LEVELS,
  PIXEL_GRID_ZOOM,
  ZOOM_MAX,
  ZOOM_MIN,
  documentPoint,
  documentRect,
  fit,
  keyboardZoomTarget,
  panBy,
  pointsPerPixel,
  resize,
  setZoom,
  showsPixelGrid,
  translateBy,
  viewPoint,
  viewport,
  fitViewport,
  keyboardZoom,
  panByStore,
  resetViewport,
  resizeViewport,
  zoomAt,
  zoomToValue,
  type ViewportState,
} from "./viewport";

/** A mid-size document in a mid-size view, at 100% CSS pixels. */
function base(): ViewportState {
  return {
    viewWidth: 800,
    viewHeight: 600,
    backingScale: 1,
    zoom: 1,
    panX: 0,
    panY: 0,
    followsFit: true,
  };
}

const DOC_W = 1000;
const DOC_H = 800;

describe("viewport constants (CanvasViewport.swift semantics)", () => {
  it("uses the Swift zoom range 0.001...32", () => {
    expect(ZOOM_MIN).toBe(0.001);
    expect(ZOOM_MAX).toBe(32);
  });

  it("exposes the 17 keyboard zoom levels from CanvasViewport.swift:12-14", () => {
    expect(KEYBOARD_ZOOM_LEVELS).toHaveLength(17);
    const expected = [
      0.125, 1 / 6, 0.25, 1 / 3, 0.5, 2 / 3, 1, 1.25, 1.5, 2, 3, 4, 5, 6, 8,
      12, 16,
    ];
    expected.forEach((v, i) => expect(KEYBOARD_ZOOM_LEVELS[i]).toBeCloseTo(v, 12));
  });

  it("shows the pixel grid from 800% and crisp pixels from 200% (EditorCanvas.swift:920-924)", () => {
    expect(PIXEL_GRID_ZOOM).toBe(8);
    expect(CRISP_ZOOM).toBe(2);
    expect(showsPixelGrid(7.999)).toBe(false);
    expect(showsPixelGrid(8)).toBe(true);
    expect(showsPixelGrid(16)).toBe(true);
  });
});

describe("document <-> view mapping", () => {
  it("maps pointsPerPixel as zoom / backingScale", () => {
    expect(pointsPerPixel(base())).toBe(1);
    expect(pointsPerPixel({ ...base(), zoom: 4, backingScale: 2 })).toBe(2);
  });

  it("centers the document rect at the view center plus pan", () => {
    const r = documentRect(base(), DOC_W, DOC_H);
    // center 400/300, scaled size 1000x800 at zoom 1
    expect(r.x).toBeCloseTo(400 - 500);
    expect(r.y).toBeCloseTo(300 - 400);
    expect(r.width).toBeCloseTo(1000);
    expect(r.height).toBeCloseTo(800);
  });

  it("round-trips document points through view space and back", () => {
    const s: ViewportState = {
      ...base(),
      zoom: 2.5,
      panX: -37,
      panY: 12,
      followsFit: false,
    };
    const doc = { x: 123.5, y: -44.25 };
    const view = viewPoint(s, doc.x, doc.y, DOC_W, DOC_H);
    const back = documentPoint(s, view.x, view.y, DOC_W, DOC_H);
    expect(back.x).toBeCloseTo(doc.x, 9);
    expect(back.y).toBeCloseTo(doc.y, 9);
  });
});

describe("anchored zoom (setZoom)", () => {
  it("keeps the anchor document point on the same screen point", () => {
    const before = { ...base(), zoom: 1, followsFit: false };
    // Pick a screen point and the document pixel under it.
    const anchor = { x: 512, y: 233 };
    const doc = documentPoint(before, anchor.x, anchor.y, DOC_W, DOC_H);
    const after = setZoom(before, 4, anchor.x, anchor.y, DOC_W, DOC_H);
    expect(after.zoom).toBe(4);
    const moved = viewPoint(after, doc.x, doc.y, DOC_W, DOC_H);
    expect(moved.x).toBeCloseTo(anchor.x, 9);
    expect(moved.y).toBeCloseTo(anchor.y, 9);
  });

  it("keeps the anchor while zooming out with a retina backing scale", () => {
    const before: ViewportState = {
      ...base(),
      zoom: 8,
      backingScale: 2,
      followsFit: false,
    };
    const anchor = { x: 100, y: 500 };
    const doc = documentPoint(before, anchor.x, anchor.y, DOC_W, DOC_H);
    const after = setZoom(before, 0.5, anchor.x, anchor.y, DOC_W, DOC_H);
    const moved = viewPoint(after, doc.x, doc.y, DOC_W, DOC_H);
    expect(moved.x).toBeCloseTo(anchor.x, 9);
    expect(moved.y).toBeCloseTo(anchor.y, 9);
  });

  it("clamps to the zoom range and ignores non-finite values", () => {
    const s = base();
    expect(setZoom(s, 100, 0, 0, DOC_W, DOC_H).zoom).toBe(ZOOM_MAX);
    expect(setZoom(s, 0.0000001, 0, 0, DOC_W, DOC_H).zoom).toBe(ZOOM_MIN);
    const untouched = setZoom(s, NaN, 0, 0, DOC_W, DOC_H);
    expect(untouched.zoom).toBe(s.zoom);
    expect(untouched.panX).toBe(s.panX);
  });

  it("clears followsFit on a manual zoom", () => {
    expect(setZoom(base(), 2, 0, 0, DOC_W, DOC_H).followsFit).toBe(false);
  });
});

describe("keyboard zoom ladder (keyboardZoomTarget)", () => {
  it("steps up and down the 17-level ladder", () => {
    const s = { ...base(), zoom: 1 };
    expect(keyboardZoomTarget(s, 1)).toBeCloseTo(1.25, 12);
    expect(keyboardZoomTarget(s, -1)).toBeCloseTo(2 / 3, 12);
    expect(keyboardZoomTarget(s, 0)).toBe(s.zoom);
  });

  it("finds the next level from between levels", () => {
    const s = { ...base(), zoom: 1.3 };
    expect(keyboardZoomTarget(s, 1)).toBeCloseTo(1.5, 12);
    expect(keyboardZoomTarget(s, -1)).toBe(1.25);
  });

  it("stops at the ladder ends", () => {
    const top = { ...base(), zoom: 16 };
    expect(keyboardZoomTarget(top, 1)).toBe(16);
    const bottom = { ...base(), zoom: 0.125 };
    expect(keyboardZoomTarget(bottom, -1)).toBe(0.125);
  });

  it("tolerates float drift around a level (Swift tolerance: max(1e-9, zoom*1e-9))", () => {
    const s = { ...base(), zoom: 1 + 1e-10 };
    // Still "at" 100%: the next step skips past 1.25? No — 1.25 > 1+tol, so it is next.
    expect(keyboardZoomTarget(s, 1)).toBeCloseTo(1.25, 12);
    // But stepping down from exactly 1.0000000001 does not land on 1 again.
    expect(keyboardZoomTarget(s, -1)).toBeCloseTo(2 / 3, 12);
  });
});

describe("fit (Fit command)", () => {
  it("fits with the 96pt margin and centers, per CanvasViewport.swift:35-41", () => {
    // min(max(1, 800-96)/1000, max(1, 600-96)/800) = min(0.704, 0.63) = 0.63
    const s = fit(base(), DOC_W, DOC_H);
    expect(s.zoom).toBeCloseTo(0.63, 12);
    expect(s.panX).toBe(0);
    expect(s.panY).toBe(0);
    expect(s.followsFit).toBe(true);
    // The fitted rect must be fully inside the view.
    const r = documentRect(s, DOC_W, DOC_H);
    expect(r.x).toBeGreaterThanOrEqual(0);
    expect(r.y).toBeGreaterThanOrEqual(0);
    expect(r.x + r.width).toBeLessThanOrEqual(800);
    expect(r.y + r.height).toBeLessThanOrEqual(600);
  });

  it("clamps the fit zoom to the range for tiny documents", () => {
    const s = fit(base(), 10, 10);
    expect(s.zoom).toBe(ZOOM_MAX);
  });

  it("keeps followsFit (and zoom) when the view has no size yet", () => {
    const s = fit({ ...base(), viewWidth: 0, viewHeight: 0, zoom: 3 }, DOC_W, DOC_H);
    expect(s.followsFit).toBe(true);
    expect(s.zoom).toBe(3);
  });
});

describe("resize (moving between displays)", () => {
  it("re-fits while followsFit is set", () => {
    const s = resize(base(), 1200, 900, 1, DOC_W, DOC_H);
    expect(s.viewWidth).toBe(1200);
    expect(s.followsFit).toBe(true);
    // min(max(1, 1200-96)/1000, max(1, 900-96)/800) = min(1.104, 1.005)
    expect(s.zoom).toBeCloseTo(1.005, 12);
  });

  it("preserves the center document point across a plain resize", () => {
    const before = { ...base(), zoom: 2, panX: -120, panY: 40, followsFit: false };
    const center = documentPoint(
      before,
      before.viewWidth / 2,
      before.viewHeight / 2,
      DOC_W,
      DOC_H,
    );
    const after = resize(before, 1024, 768, 1, DOC_W, DOC_H);
    const again = documentPoint(
      after,
      after.viewWidth / 2,
      after.viewHeight / 2,
      DOC_W,
      DOC_H,
    );
    expect(again.x).toBeCloseTo(center.x, 9);
    expect(again.y).toBeCloseTo(center.y, 9);
  });

  it("clamps the backing scale at 1", () => {
    expect(resize(base(), 800, 600, 0.5, DOC_W, DOC_H).backingScale).toBe(1);
  });
});

describe("panning", () => {
  it("adds the delta and clears followsFit", () => {
    const s = panBy(translateBy(base(), 10, -5), -20, 8);
    expect(s.panX).toBe(-10);
    expect(s.panY).toBe(3);
    expect(s.followsFit).toBe(false);
  });
});

describe("viewport store actions", () => {
  // The surface measures its view on mount and reports it once; mimic that
  // here so the store's view center and Fit math have a real view size.
  beforeEach(() => {
    resetViewport();
    resizeViewport(800, 600, 1, DOC_W, DOC_H);
  });

  it("zoomAt zooms the store around a screen anchor", () => {
    zoomAt(2, 400, 300, DOC_W, DOC_H);
    const s = get(viewport);
    expect(s.zoom).toBe(2);
    expect(s.followsFit).toBe(false);
  });

  it("zoomToValue(1) returns to actual pixels anchored at the view center", () => {
    zoomAt(4, 400, 300, DOC_W, DOC_H);
    zoomToValue(1, DOC_W, DOC_H);
    const s = get(viewport);
    expect(s.zoom).toBe(1);
    // Anchor = view center stays at the view center: the pan is unchanged.
    expect(s.panX).toBeCloseTo(0, 9);
    expect(s.panY).toBeCloseTo(0, 9);
  });

  it("keyboardZoom walks the ladder from the store", () => {
    zoomToValue(1, DOC_W, DOC_H); // leave the fitted 63% for a known rung
    // One level per call regardless of |step| (CanvasViewport.swift:66-73
    // searches first/last without repeating the step).
    keyboardZoom(2, DOC_W, DOC_H);
    expect(get(viewport).zoom).toBeCloseTo(1.25, 12);
    keyboardZoom(-1, DOC_W, DOC_H);
    expect(get(viewport).zoom).toBeCloseTo(1, 12);
  });

  it("keyboardZoom is a no-op at the ladder ends", () => {
    zoomToValue(16, DOC_W, DOC_H);
    const before = get(viewport).panX;
    keyboardZoom(1, DOC_W, DOC_H);
    expect(get(viewport).zoom).toBe(16);
    expect(get(viewport).panX).toBe(before);
  });

  it("fitViewport centers the document", () => {
    panByStore(50, 50);
    fitViewport(DOC_W, DOC_H);
    const s = get(viewport);
    expect(s.followsFit).toBe(true);
    expect(s.panX).toBe(0);
    expect(s.zoom).toBeCloseTo(0.63, 12);
  });

  it("resizeViewport keeps the view size in the store", () => {
    resizeViewport(1024, 768, 1, DOC_W, DOC_H);
    const s = get(viewport);
    expect(s.viewWidth).toBe(1024);
    expect(s.viewHeight).toBe(768);
  });

  it("resetViewport returns the initial state", () => {
    zoomAt(8, 0, 0, DOC_W, DOC_H);
    panByStore(5, 5);
    resetViewport();
    expect(get(viewport)).toEqual({ ...INITIAL_VIEWPORT });
  });
});
