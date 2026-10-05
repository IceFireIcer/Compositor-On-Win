import { describe, expect, it } from "vitest";
import {
  DEFAULT_SNAP_TOGGLES,
  SNAP_TOLERANCE_PX,
  gridLines,
  gridStep,
  isMajorGridLine,
  normalizeGrid,
  snap,
  type Rect,
} from "./snap";

describe("layout grid (ticket 14)", () => {
  it("defaults to 64 px spacing with eight subdivisions (Swift LayoutGrid)", () => {
    const g = normalizeGrid(64, 8);
    expect(g.spacing).toBe(64);
    expect(g.subdivisions).toBe(8);
    expect(gridStep(g)).toBe(8);
  });

  it("clamps spacing to 2–4096 and subdivisions to 1–64, never finer than a pixel", () => {
    expect(normalizeGrid(1, 1).spacing).toBe(2);
    expect(normalizeGrid(9999, 1).spacing).toBe(4096);
    expect(normalizeGrid(64, 0).subdivisions).toBe(1);
    expect(normalizeGrid(64, 100).subdivisions).toBe(64);
    // Never finer than one pixel: spacing 2 allows at most 2 subdivisions.
    expect(normalizeGrid(2, 64).subdivisions).toBe(2);
    expect(gridStep(normalizeGrid(2, 64))).toBe(1);
  });

  it("lists grid lines from the origin so uneven steps do not drift off the majors", () => {
    // 64/8 = 8 px step across 100 px: 0..96.
    expect(gridLines(normalizeGrid(64, 8), 100)).toEqual([
      0, 8, 16, 24, 32, 40, 48, 56, 64, 72, 80, 88, 96,
    ]);
    // 10/3 is uneven: counted from the origin, not accumulated, and rounded
    // to whole pixels like Swift's (CGFloat($0) * step).rounded().
    const lines = gridLines(normalizeGrid(10, 3), 10);
    expect(lines).toEqual([0, 3, 7, 10]);
    expect(lines.every((v) => Number.isInteger(v))).toBe(true);
  });

  it("classifies major lines", () => {
    const g = normalizeGrid(64, 8);
    expect(isMajorGridLine(g, 64)).toBe(true);
    expect(isMajorGridLine(g, 128)).toBe(true);
    expect(isMajorGridLine(g, 72)).toBe(false);
  });
});

describe("snap (ticket 14)", () => {
  const doc = { width: 800, height: 600 };

  it("passes the position through when nothing can snap", () => {
    const r = snap({ x: 123.4, y: 77.7 }, {});
    expect(r).toEqual({ x: 123.4, y: 77.7, snappedTo: [] });
  });

  it("the master switch off returns the position unchanged", () => {
    const r = snap({ x: 399, y: 10 }, { docBounds: doc }, { enabled: false });
    expect(r.x).toBe(399);
    expect(r.y).toBe(10);
    expect(r.snappedTo).toEqual([]);
  });

  it("Ctrl bypass vetoes every snap target", () => {
    const targets = { docBounds: doc, guides: [{ axis: "vertical" as const, position: 400 }] };
    const r = snap({ x: 399, y: 10 }, targets, { bypass: true });
    expect(r.x).toBe(399);
    expect(r.snappedTo).toEqual([]);
  });

  it("all Snap To toggles off returns the position unchanged", () => {
    const targets = { docBounds: doc };
    const r = snap({ x: 399, y: 2 }, targets, {
      toggles: { guides: false, grid: false, layerBounds: false, docBounds: false },
    });
    expect(r).toEqual({ x: 399, y: 2, snappedTo: [] });
  });

  it("snaps to document bounds edges and center", () => {
    expect(snap({ x: 796, y: 100 }, { docBounds: doc })).toEqual({
      x: 800,
      y: 100,
      snappedTo: ["docBounds"],
    });
    expect(snap({ x: 100, y: 301 }, { docBounds: doc })).toEqual({
      x: 100,
      y: 300,
      snappedTo: ["docBounds"],
    });
    expect(snap({ x: 1, y: 1 }, { docBounds: doc }).snappedTo).toEqual(["docBounds"]);
  });

  it("snaps to vertical guides on x and horizontal guides on y", () => {
    const targets = {
      guides: [
        { axis: "vertical" as const, position: 400 },
        { axis: "horizontal" as const, position: 250 },
      ],
    };
    const r = snap({ x: 403, y: 247 }, targets);
    expect(r).toEqual({ x: 400, y: 250, snappedTo: ["guides"] });
    // A disabled guides toggle stops guide snapping.
    const off = snap({ x: 403, y: 247 }, targets, {
      toggles: { ...DEFAULT_SNAP_TOGGLES, guides: false },
    });
    expect(off.x).toBe(403);
  });

  it("snaps to grid lines computed from spacing and subdivisions", () => {
    const grid = normalizeGrid(64, 8); // 8 px minor step
    // Grid snapping defaults off (Swift ToolDefaults snapGrid = false).
    const r = snap({ x: 30.6, y: 12.2 }, { grid }, {
      toggles: { ...DEFAULT_SNAP_TOGGLES, grid: true },
    });
    // y=12.2 sits between the 8 and 16 lines; 16 is nearer.
    expect(r).toEqual({ x: 32, y: 16, snappedTo: ["grid"] });
  });

  it("snaps to layer bounds edges and centers", () => {
    const layer: Rect = { x: 100, y: 200, width: 50, height: 40 };
    const targets = { layerBounds: [layer] };
    // Right edge at 150, center x at 125; top edge 200, center y 220.
    expect(snap({ x: 148, y: 100 }, targets).x).toBe(150);
    expect(snap({ x: 124, y: 100 }, targets).x).toBe(125);
    expect(snap({ x: 1000, y: 202 }, targets).y).toBe(200);
    expect(snap({ x: 1000, y: 221 }, targets).y).toBe(220);
    expect(snap({ x: 148, y: 202 }, targets).snappedTo).toEqual(["layerBounds"]);
  });

  it("respects the tolerance: at the edge snaps, beyond does not", () => {
    const targets = { docBounds: doc };
    // Exactly at the tolerance snaps (Swift uses <=).
    expect(snap({ x: 800 - SNAP_TOLERANCE_PX, y: 100 }, targets).x).toBe(800);
    expect(snap({ x: 800 - SNAP_TOLERANCE_PX - 0.01, y: 100 }, targets).x).toBe(
      800 - SNAP_TOLERANCE_PX - 0.01,
    );
    // A custom tolerance widens the capture band.
    expect(snap({ x: 780, y: 100 }, targets, { tolerance: 25 }).x).toBe(800);
  });

  it("picks the nearest target per axis and reports each snapped category", () => {
    const targets = {
      guides: [{ axis: "vertical" as const, position: 300 }],
      docBounds: doc,
    };
    // x=299 is nearer the guide (300) than the document bounds; y=599 snaps to the bottom edge.
    const r = snap({ x: 299, y: 599 }, targets);
    expect(r.x).toBe(300);
    expect(r.y).toBe(600);
    expect(r.snappedTo).toEqual(["guides", "docBounds"]);
  });

  it("breaks distance ties by keeping the earlier target (Swift semantics)", () => {
    const targets = {
      guides: [
        { axis: "vertical" as const, position: 100 },
        { axis: "vertical" as const, position: 104 },
      ],
    };
    // Equidistant (2 px) between both: the first target in list order wins.
    expect(snap({ x: 102, y: 0 }, targets).x).toBe(100);
  });

  it("grid snapping only applies when the grid toggle is on", () => {
    const grid = normalizeGrid(64, 8);
    const targets = { grid };
    const r = snap({ x: 30.6, y: 100 }, targets, {
      toggles: { ...DEFAULT_SNAP_TOGGLES, grid: false },
    });
    expect(r.x).toBe(30.6);
  });
});
