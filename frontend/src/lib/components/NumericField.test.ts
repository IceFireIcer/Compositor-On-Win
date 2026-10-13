import { describe, expect, it } from "vitest";
import { fieldArrow, fieldDirect, fieldDisplay, fieldScrub } from "./numericField";

const hardness = { min: 0, max: 1, scale: 100, sensitivity: 0.01, step: 1 };

describe("numeric tool field", () => {
  it("converges drag, Shift+arrow and direct percentage entry in store units", () => {
    expect(fieldDisplay(0.75, hardness.scale)).toBe("75");
    expect(fieldScrub(10, 0.75, hardness)).toBeCloseTo(0.85);
    expect(fieldArrow("up", 0.75, { ...hardness, shift: true })).toBeCloseTo(0.85);
    expect(fieldDirect("85", hardness)).toBeCloseTo(0.85);
    expect(fieldDirect("250", hardness)).toBe(1);
    expect(fieldDirect("", hardness)).toBeNull();
  });
});
