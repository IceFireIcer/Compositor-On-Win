import { beforeEach, describe, expect, it } from "vitest";
import { render } from "svelte/server";
import ToolOptionsBar from "./ToolOptionsBar.svelte";
import { activeTool, toolModes } from "../state/tools";

describe("rendered tool options", () => {
  beforeEach(() => {
    activeTool.set("move");
    toolModes.set({ marquee: "rect", lasso: "freehand", wand: "wand", brush: "paint", blur: "liquify" });
  });

  it("offers one strength control and only blur mode's radius, without brush smoothing", () => {
    activeTool.set("blur");
    let html = render(ToolOptionsBar).body;
    expect(html.match(/Strength/g)).toHaveLength(1);
    expect(html).not.toContain("Smoothing");
    expect(html).not.toContain("Blur radius");

    toolModes.update((modes) => ({ ...modes, blur: "blur" }));
    html = render(ToolOptionsBar).body;
    expect(html.match(/Strength/g)).toHaveLength(1);
    expect(html).toContain("Blur radius");
  });
});
