import { describe, expect, it } from "vitest";
import {
  EDGE_SCROLL_MARGIN,
  edgeAutoScroll,
  ellipseRegion,
  fillLoops,
  isEmpty,
  lassoRegion,
  offsetSelection,
  outlineLoops,
  rectRegion,
  select,
  selectionMode,
  type Point,
  type Selection,
} from "./selection";

const W = 100;
const H = 100;

/** Coverage 0/255 at a document pixel of the selection's mask. */
function coverage(selection: Selection | null, x: number, y: number): number {
  if (!selection) throw new Error("no selection");
  return selection.mask[y * selection.width + x];
}

/** The SelectionTests square helper: a clockwise polygon of four corners. */
function square(x: number, y: number, size: number): Point[] {
  return [
    { x, y },
    { x: x + size, y },
    { x: x + size, y: y + size },
    { x, y: y + size },
  ];
}

function shoelace(loop: Point[]): number {
  let sum = 0;
  for (let i = 0; i < loop.length; i++) {
    const a = loop[i];
    const b = loop[(i + 1) % loop.length];
    sum += a.x * b.y - b.x * a.y;
  }
  return sum;
}

describe("modifier semantics", () => {
  it("maps shift/option onto replace/add/subtract", () => {
    expect(selectionMode(false, false)).toBe("replace");
    expect(selectionMode(true, false)).toBe("add");
    expect(selectionMode(true, true)).toBe("subtract");
    expect(selectionMode(false, true)).toBe("subtract");
  });
});

describe("rectangular marquee", () => {
  it("draws whole-pixel rectangles in any direction", () => {
    // SelectionTests: 60.4,70.6 → 20.2,30.3 is rect 20,30 40×41.
    const region = rectRegion({ x: 60.4, y: 70.6 }, { x: 20.2, y: 30.3 }, W, H);
    expect(coverage({ ...region, active: true }, 20, 30)).toBe(255);
    expect(coverage({ ...region, active: true }, 19, 30)).toBe(0);
    expect(coverage({ ...region, active: true }, 59, 70)).toBe(255);
    expect(coverage({ ...region, active: true }, 60, 70)).toBe(0);
    expect(coverage({ ...region, active: true }, 20, 70)).toBe(255);
  });

  it("squares with the square option and grows from the anchor", () => {
    const squared = rectRegion({ x: 10, y: 10 }, { x: 40, y: 20 }, W, H, { square: true });
    expect(squared.loops[0]).toEqual([
      { x: 10, y: 10 },
      { x: 40, y: 10 },
      { x: 40, y: 40 },
      { x: 10, y: 40 },
    ]);
    const centered = rectRegion({ x: 50, y: 50 }, { x: 60, y: 55 }, W, H, { fromCenter: true });
    expect(centered.loops[0]).toEqual([
      { x: 40, y: 45 },
      { x: 60, y: 45 },
      { x: 60, y: 55 },
      { x: 40, y: 55 },
    ]);
    const centeredSquare = rectRegion({ x: 50, y: 50 }, { x: 45, y: 58 }, W, H, {
      square: true,
      fromCenter: true,
    });
    expect(centeredSquare.loops[0]).toEqual([
      { x: 42, y: 42 },
      { x: 58, y: 42 },
      { x: 58, y: 58 },
      { x: 42, y: 58 },
    ]);
  });

  it("a click draws nothing and replacing with it deselects", () => {
    const region = rectRegion({ x: 5, y: 5 }, { x: 5, y: 5 }, W, H);
    expect(region.mask.every((b) => b === 0)).toBe(true);
    expect(select("replace", null, region)).toBeNull();
  });
});

describe("elliptical marquee", () => {
  it("selects an oval in its box", () => {
    const region = ellipseRegion({ x: 10, y: 20 }, { x: 70, y: 60 }, W, H);
    expect(coverage({ ...region, active: true }, 40, 40)).toBe(255); // the middle
    expect(coverage({ ...region, active: true }, 11, 21)).toBe(0); // the box's corner lies outside the oval
  });

  it("the square option makes a circle", () => {
    const region = ellipseRegion({ x: 5, y: 5 }, { x: 45, y: 25 }, W, H, { square: true });
    const loop = region.loops[0];
    const xs = loop.map((p) => p.x);
    const ys = loop.map((p) => p.y);
    expect(Math.max(...xs) - Math.min(...xs)).toBeCloseTo(Math.max(...ys) - Math.min(...ys), 6);
  });
});

describe("lasso", () => {
  it("rasterizes a polygon at pixel centers", () => {
    const region = lassoRegion(
      [
        { x: 0, y: 0 },
        { x: 100, y: 0 },
        { x: 0, y: 100 },
      ],
      W,
      H,
    );
    expect(coverage({ ...region, active: true }, 10, 10)).toBe(255);
    expect(coverage({ ...region, active: true }, 99, 0)).toBe(0); // outside the hypotenuse
    expect(coverage({ ...region, active: true }, 99, 99)).toBe(0);
  });

  it("fewer than three points selects nothing", () => {
    const region = lassoRegion([{ x: 1, y: 1 }, { x: 2, y: 2 }], W, H);
    expect(region.mask.every((b) => b === 0)).toBe(true);
  });
});

describe("replace/add/subtract combine (SelectionTests semantics)", () => {
  const square10 = lassoRegion(square(10, 10, 40), W, H);
  const square50 = lassoRegion(square(50, 50, 40), W, H);
  const square20 = lassoRegion(square(20, 20, 20), W, H);
  const square60 = lassoRegion(square(60, 10, 20), W, H);

  it("combines outlines through replace, add and subtract", () => {
    let selection = select("replace", null, square10);
    expect(coverage(selection, 30, 30)).toBe(255);
    expect(coverage(selection, 70, 70)).toBe(0);
    selection = select("add", selection, square50);
    expect(coverage(selection, 30, 30)).toBe(255);
    expect(coverage(selection, 70, 70)).toBe(255);
    selection = select("subtract", selection, square20);
    expect(coverage(selection, 30, 30)).toBe(0);
    expect(coverage(selection, 15, 15)).toBe(255);
    selection = select("replace", selection, square60);
    expect(coverage(selection, 70, 20)).toBe(255);
    expect(coverage(selection, 70, 70)).toBe(0);
    expect(coverage(selection, 15, 15)).toBe(0);
  });

  it("clips the selection to the canvas", () => {
    const offscreen = lassoRegion(square(-50, -50, 100), W, H);
    const selection = select("replace", null, offscreen);
    expect(coverage(selection, 0, 0)).toBe(255);
    expect(coverage(selection, 45, 45)).toBe(255);
    expect(selection?.width).toBe(W);
    expect(selection?.height).toBe(H);
  });

  it("keeps no selection, empty selection and selection distinct", () => {
    // Nothing to subtract from.
    expect(select("subtract", null, square20)).toBeNull();
    // Subtracting everything leaves a present, empty selection.
    let selection = select("replace", null, square10);
    selection = select("subtract", selection, lassoRegion(square(0, 0, 60), W, H));
    expect(selection).not.toBeNull();
    expect(isEmpty(selection as Selection)).toBe(true);
    expect(coverage(selection, 20, 20)).toBe(0);
  });
});

describe("moving a selection", () => {
  it("moves the outline and the coverage as one", () => {
    const selection = select("replace", null, rectRegion({ x: 10, y: 10 }, { x: 30, y: 30 }, W, H));
    const moved = offsetSelection(selection as Selection, 40, 40);
    expect(coverage(moved, 55, 55)).toBe(255);
    expect(coverage(moved, 15, 15)).toBe(0);
    expect(moved.loops[0][0]).toEqual({ x: 50, y: 50 });
  });

  it("moving off canvas and back keeps the whole shape", () => {
    const selection = select("replace", null, rectRegion({ x: 10, y: 10 }, { x: 30, y: 30 }, W, H));
    const off = offsetSelection(selection as Selection, -25, 0);
    expect(coverage(off, 0, 20)).toBe(255);
    expect(coverage(off, 5, 20)).toBe(0); // clipped while off canvas
    const back = offsetSelection(off, 25, 0);
    expect(coverage(back, 10, 10)).toBe(255);
    expect(coverage(back, 29, 29)).toBe(255);
    expect(back.loops).toEqual(selection?.loops);
  });

  it("is a pure function: the input selection is untouched", () => {
    const selection = select("replace", null, rectRegion({ x: 10, y: 10 }, { x: 30, y: 30 }, W, H));
    const before = selection?.mask.slice();
    offsetSelection(selection as Selection, 5, 7);
    expect(selection?.mask).toEqual(before);
    expect(selection?.loops[0][0]).toEqual({ x: 10, y: 10 });
  });
});

describe("outline tracing for the marching ants", () => {
  it("outlines a block as one clockwise corner loop", () => {
    const mask = new Uint8Array(25);
    for (let y = 1; y <= 3; y++) {
      for (let x = 1; x <= 3; x++) mask[y * 5 + x] = 255;
    }
    const loops = outlineLoops(mask, 5, 5);
    expect(loops).toHaveLength(1);
    expect(loops[0]).toEqual([
      { x: 1, y: 1 },
      { x: 4, y: 1 },
      { x: 4, y: 4 },
      { x: 1, y: 4 },
    ]);
    expect(shoelace(loops[0])).toBeGreaterThan(0); // clockwise on screen
  });

  it("runs holes counterclockwise", () => {
    const mask = new Uint8Array(25).fill(255);
    mask[2 * 5 + 2] = 0;
    const loops = outlineLoops(mask, 5, 5);
    expect(loops).toHaveLength(2);
    expect(shoelace(loops[0])).toBeGreaterThan(0);
    expect(shoelace(loops[1])).toBeLessThan(0);
  });

  it("refilling traced loops reproduces the mask", () => {
    const mask = new Uint8Array(25).fill(255);
    mask[2 * 5 + 2] = 0;
    const refilled = fillLoops(outlineLoops(mask, 5, 5), 5, 5);
    expect(Array.from(refilled)).toEqual(Array.from(mask));
  });
});

describe("edge auto-scroll while dragging", () => {
  it("does nothing away from the edge", () => {
    expect(edgeAutoScroll(EDGE_SCROLL_MARGIN, 10)).toBe(0);
    expect(edgeAutoScroll(200, 10)).toBe(0);
  });

  it("scrolls proportionally inside the margin and stops when leaving it", () => {
    expect(edgeAutoScroll(0, 10)).toBeCloseTo(10, 6); // at the edge: full speed
    expect(edgeAutoScroll(-5, 10)).toBeCloseTo(10, 6); // past the edge: clamped full speed
    expect(edgeAutoScroll(EDGE_SCROLL_MARGIN / 2, 10)).toBeCloseTo(5, 6);
    expect(edgeAutoScroll(EDGE_SCROLL_MARGIN + 1, 10)).toBe(0); // left the margin: stopped
  });
});
