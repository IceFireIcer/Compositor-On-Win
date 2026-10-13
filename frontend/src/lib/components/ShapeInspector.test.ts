import { describe, expect, it } from "vitest";
import { render } from "svelte/server";
import ShapeInspector from "./ShapeInspector.svelte";
import type { ShapeTarget } from "../state/shapeStyle";

const target: ShapeTarget = { tabID: "tab-a", layerID: "shape-a", rev: 1,
  shape: { kind: "Rectangle", red: 1, green: 0, blue: 0, cornerRadius: 12 } };

describe("shape inspector mount", () => {
  it("shows honest read-only capability and rectangle ranges without an adapter", () => {
    const html = render(ShapeInspector, { props: { target } }).body;
    expect(html).toContain("Shape style editing is unavailable.");
    expect(html).toMatch(/<fieldset[^>]*disabled/);
    expect(html).toContain('min="0" max="5000"');
    expect(html).toContain("Fill color");
    expect(html).not.toContain("Line width (px)");
  });

  it("mounts line-specific controls only when given a real commit adapter", () => {
    const html = render(ShapeInspector, { props: { target: { ...target,
      shape: { ...target.shape, kind: "Line", lineWidth: 7 } }, commit: async () => {} } }).body;
    expect(html).not.toMatch(/<fieldset[^>]*disabled/);
    expect(html).toContain('min="1" max="5000"');
    expect(html).toContain("Line color");
    expect(html).not.toContain("Corner radius (px)");
  });

  it("renders nothing for a non-shape selection", () => {
    expect(render(ShapeInspector, { props: { target: null } }).body).not.toContain("<section");
  });
});
