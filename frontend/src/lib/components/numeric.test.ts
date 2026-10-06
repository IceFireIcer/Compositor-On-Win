import { describe, expect, it } from "vitest";
import { ARROW_SHIFT_MULTIPLIER, arrows, direct, scrub } from "./numeric";

const BRUSH_SIZE = { min: 1, max: 2000 }; // BrushControls.swift:39 range 1...2000
const HARDNESS = { min: 0, max: 1 }; // BrushControls.swift:50 range 0...1, sensitivity 0.01

describe("scrub (Swift NumericScrub, UI/NumericScrub.swift:29-42)", () => {
  it("adds one sensitivity unit per dragged pixel to the gesture's start value", () => {
    // Swift line 34: proposed = start + translation.width * sensitivity.
    expect(scrub(5, 10, { sensitivity: 1 })).toBe(15);
    expect(scrub(-3, 10, { sensitivity: 1 })).toBe(7);
    expect(scrub(2.5, 10, { sensitivity: 1 })).toBe(12.5);
    // Sensitivity is per-control: hardness drags at 0.01/px (BrushControls.swift:50).
    expect(scrub(25, 0.5, { sensitivity: 0.01 })).toBeCloseTo(0.75, 12);
  });

  it("snaps dragged proposals to multiples of step; typing stays un-snapped (Swift lines 9, 35)", () => {
    // step 1: whole numbers only.
    expect(scrub(5.4, 10, { sensitivity: 1, step: 1 })).toBe(15);
    // step 10: 5 + 7 = 12 snaps to 10.
    expect(scrub(7, 5, { sensitivity: 1, step: 10 })).toBe(10);
    expect(scrub(8, 5, { sensitivity: 1, step: 10 })).toBe(10);
    expect(scrub(9, 5, { sensitivity: 1, step: 10 })).toBe(10);
  });

  it("ignores a step of zero or less (Swift line 35: `if let step, step > 0`)", () => {
    expect(scrub(5, 10, { sensitivity: 1, step: 0 })).toBe(15);
  });

  it("clamps into the range and never wraps (Swift line 36: min(upper, max(lower, ...)))", () => {
    expect(scrub(-100, 5, { sensitivity: 1, range: BRUSH_SIZE })).toBe(1);
    expect(scrub(10000, 1999, { sensitivity: 1, range: BRUSH_SIZE })).toBe(2000);
    expect(scrub(50, 0.5, { sensitivity: 0.01, range: HARDNESS })).toBe(1);
    expect(scrub(-50, 0.5, { sensitivity: 0.01, range: HARDNESS })).toBe(0);
    // Mid-gesture values below the lower bound clamp too.
    expect(scrub(-10, 3, { sensitivity: 1, step: 1, range: BRUSH_SIZE })).toBe(1);
  });

  it("snaps after scaling and before clamping, in Swift's order", () => {
    // -8 px from 5 = -3 -> snaps to -0 (0) -> clamps to 1.
    expect(scrub(-8, 5, { sensitivity: 1, step: 1, range: BRUSH_SIZE })).toBe(1);
    // Without snapping, 1999.6 would stay inside 1...2000; snapped to 2000 it stays 2000.
    expect(scrub(1994.6, 5, { sensitivity: 1, step: 1, range: BRUSH_SIZE })).toBe(2000);
  });

  it("has no Shift fine adjustment: Swift's NumericScrub takes no modifiers", () => {
    // The whole Swift file reads drag translation only; sensitivity is fixed
    // per control, so the default fine multiplier is 1 (a no-op).
    expect(scrub(10, 0, { sensitivity: 1 })).toBe(scrub(10, 0, { sensitivity: 1, fine: 1 }));
    // The knob exists for a future UI wrapper that wants to rescale a drag;
    // with fine 0.1 ten pixels move the value by one unit.
    expect(scrub(10, 0, { sensitivity: 1, fine: 0.1 })).toBeCloseTo(1, 12);
  });
});

describe("arrows (Swift ArrowStepper, ContentView.swift:390-417)", () => {
  it("Up adds one step, Down subtracts one (Swift line 405)", () => {
    expect(arrows("up", 10, { step: 1 })).toBe(11);
    expect(arrows("down", 10, { step: 1 })).toBe(9);
    expect(arrows("up", 0.5, { step: 0.25 })).toBeCloseTo(0.75, 12);
  });

  it("Shift multiplies the step by ten (Swift line 404), the constant ARROW_SHIFT_MULTIPLIER", () => {
    expect(ARROW_SHIFT_MULTIPLIER).toBe(10);
    expect(arrows("up", 10, { step: 1, shift: true })).toBe(20);
    expect(arrows("down", 10, { step: 1, shift: true })).toBe(0);
    expect(arrows("up", 10, { step: 1 })).toBe(11); // Shift released: plain step
  });

  it("clamps into the range when given (Swift leaves clamping to the binding, ContentView.swift:445)", () => {
    // Swift callers clamp where they store, e.g. ColorPickerSheet.swift:141
    // `min(255, max(0, newValue.rounded()))`; the port offers the same clamp.
    expect(arrows("down", 1, { step: 1, range: BRUSH_SIZE })).toBe(1);
    expect(arrows("up", 2000, { step: 1, range: BRUSH_SIZE })).toBe(2000);
    expect(arrows("down", 0.05, { step: 0.25, range: HARDNESS })).toBe(0);
  });

  it("does not snap to a step grid (only drags snap, NumericScrub.swift:35)", () => {
    expect(arrows("up", 10.3, { step: 1 })).toBe(11.3);
  });
});

describe("direct (typed field input, clamped like ColorPickerSheet.swift:128)", () => {
  it("parses decimal input, trimmed, with optional sign and decimals", () => {
    expect(direct("15")).toBe(15);
    expect(direct(" 12.5 ")).toBe(12.5);
    expect(direct("-5")).toBe(-5);
    expect(direct("+3")).toBe(3);
    expect(direct("1e2")).toBe(100);
  });

  it("clamps into the range and never wraps", () => {
    expect(direct("5000", BRUSH_SIZE)).toBe(2000);
    expect(direct("-3", BRUSH_SIZE)).toBe(1);
    expect(direct("300", { min: 0, max: 255 })).toBe(255);
    expect(direct("-1", { min: 0, max: 255 })).toBe(0);
  });

  it("returns null for empty or non-numeric text", () => {
    expect(direct("")).toBeNull();
    expect(direct("   ")).toBeNull();
    expect(direct("abc")).toBeNull();
    expect(direct("NaN")).toBeNull();
    expect(direct("Infinity")).toBeNull();
    expect(direct("0x10")).toBeNull(); // Swift's number format parses plain decimals only
    expect(direct("1,5")).toBeNull(); // no locale comma
  });
});

describe("convergence: drag, typing and stepping reach the same value (ticket 19 acceptance)", () => {
  it("brush size: drag 5 px = typing 15 = five Up presses from 10", () => {
    const opts = { sensitivity: 1, step: 1, range: BRUSH_SIZE };
    const dragged = scrub(5, 10, opts);
    const typed = direct("15", BRUSH_SIZE);
    let stepped = 10;
    for (let i = 0; i < 5; i++) stepped = arrows("up", stepped, { step: 1, range: BRUSH_SIZE });
    expect(dragged).toBe(15);
    expect(typed).toBe(15);
    expect(stepped).toBe(15);
  });

  it("with fractional drags, step snapping makes drag = typing = stepping (16)", () => {
    const opts = { sensitivity: 1, step: 1, range: BRUSH_SIZE };
    const dragged = scrub(5.6, 10, opts); // 15.6 snaps to 16
    const typed = direct("16", BRUSH_SIZE);
    let stepped = 10;
    for (let i = 0; i < 6; i++) stepped = arrows("up", stepped, { step: 1, range: BRUSH_SIZE });
    expect(dragged).toBe(16);
    expect(typed).toBe(16);
    expect(stepped).toBe(16);
  });

  it("one Shift-Up equals typing the value ten steps away equals the equivalent drag", () => {
    const opts = { sensitivity: 1, step: 1, range: BRUSH_SIZE };
    const stepped = arrows("up", 10, { step: 1, shift: true, range: BRUSH_SIZE });
    const typed = direct("20", BRUSH_SIZE);
    const dragged = scrub(10, 10, opts);
    expect(stepped).toBe(20);
    expect(typed).toBe(20);
    expect(dragged).toBe(20);
  });

  it("hardness at 0.01/px: drag 25 px = typing 0.75 = one 0.25 step from 0.5", () => {
    const dragged = scrub(25, 0.5, { sensitivity: 0.01, range: HARDNESS });
    const typed = direct("0.75", HARDNESS);
    const stepped = arrows("up", 0.5, { step: 0.25, range: HARDNESS });
    expect(dragged).toBeCloseTo(0.75, 12);
    expect(typed).toBeCloseTo(0.75, 12);
    expect(stepped).toBeCloseTo(0.75, 12);
  });

  it("every route clamps to the same ceiling: 2000", () => {
    const opts = { sensitivity: 1, step: 1, range: BRUSH_SIZE };
    expect(scrub(99999, 1990, opts)).toBe(2000);
    expect(direct("99999", BRUSH_SIZE)).toBe(2000);
    expect(arrows("up", 1995, { step: 1, shift: true, range: BRUSH_SIZE })).toBe(2000);
  });
});
