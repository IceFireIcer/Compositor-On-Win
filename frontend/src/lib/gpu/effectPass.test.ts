import { describe, expect, it, vi } from "vitest";
import { buildEffectChain, buildEffectPasses, EffectOutputCache, type EffectParams } from "./effectPass";

describe("buildEffectPasses", () => {
  it("emits coverage → blur ×2 → outside ring → colorize for a drop shadow", () => {
    const passes = buildEffectPasses({
      kind: "shadow", enabled: true, angle: 90, distance: 8, blur: 6, red: 0, green: 0, blue: 0, opacity: 0.5,
    });
    expect(passes.map((p) => p.pass)).toEqual(["coverage", "blurRow", "blurCol", "ring", "colorize"]);
    const ring = passes[3];
    expect(ring.pass === "ring" && ring.side).toBe("outside");
    // 90° clockwise from up = straight down: dx ≈ 0, dy = +distance.
    expect(ring.pass === "ring" && ring.offsetX).toBeCloseTo(0, 6);
    expect(ring.pass === "ring" && ring.offsetY).toBeCloseTo(8, 6);
  });

  it("inner effects use the inverted ring and compose under", () => {
    const passes = buildEffectPasses({
      kind: "innerShadow", enabled: true, angle: 0, distance: 4, blur: 2, red: 0.1, green: 0.2, blue: 0.3, opacity: 1,
    });
    const ring = passes[3];
    expect(ring.pass === "ring" && ring.side).toBe("inside");
    const colorize = passes[4];
    expect(colorize.pass === "colorize" && colorize.compose).toBe("under");
  });

  it("stroke composes over with inside ring; disabled effects emit nothing", () => {
    const stroke = buildEffectPasses({ kind: "stroke", enabled: true, size: 4, red: 1, green: 0, blue: 0, opacity: 1 });
    expect(stroke[4].pass === "colorize" && stroke[4].compose).toBe("over");
    expect(buildEffectPasses({ kind: "stroke", enabled: false, size: 4, red: 1, green: 0, blue: 0, opacity: 1 })).toEqual([]);
  });

  it("colorOverlay is a single coverage + colorize pair", () => {
    const passes = buildEffectPasses({ kind: "colorOverlay", enabled: true, red: 1, green: 1, blue: 0, opacity: 0.4 });
    expect(passes.map((p) => p.pass)).toEqual(["coverage", "colorize"]);
  });

  it("chains all enabled effects in application order", () => {
    const chain = buildEffectChain({
      colorOverlay: { kind: "colorOverlay", enabled: true, red: 1, green: 1, blue: 1, opacity: 0.2 },
      shadow: { kind: "shadow", enabled: true, angle: 90, distance: 0, blur: 3, red: 0, green: 0, blue: 0, opacity: 1 },
      stroke: { kind: "stroke", enabled: false, size: 2, red: 1, green: 0, blue: 0, opacity: 1 },
    });
    // shadow (5 passes) then colorOverlay (2); disabled stroke absent.
    expect(chain.length).toBe(7);
  });
});

describe("EffectOutputCache", () => {
  const params: EffectParams = { kind: "shadow", enabled: true, angle: 90, distance: 0, blur: 4, red: 0, green: 0, blue: 0, opacity: 1 };

  it("recomputes only when properties change", () => {
    const compute = vi.fn(() => ({ frame: Math.random() }));
    const cache = new EffectOutputCache(1000, compute);
    cache.obtain("a", JSON.stringify(params));
    cache.obtain("a", JSON.stringify(params));
    expect(compute).toHaveBeenCalledTimes(1);
    params.blur = 8; // non-destructive edit → new key
    cache.obtain("a", JSON.stringify(params));
    expect(compute).toHaveBeenCalledTimes(2);
  });

  it("serves the previous output while revalidating — no white flash", () => {
    let frames = 0;
    const cache = new EffectOutputCache(1000, () => ({ frame: ++frames }));
    const first = cache.obtain("a", JSON.stringify(params));
    const stale = cache.obtain("a", `${JSON.stringify(params)}#edited`);
    expect(stale).toBe(first); // previous frame stays readable
    expect(cache.peek("a", `${JSON.stringify(params)}#edited`)).toBeDefined(); // recompute finished
  });

  it("memory pressure evicts through the texture-cache discipline", () => {
    const compute = vi.fn(() => ({ frame: 1 }));
    const cache = new EffectOutputCache(10, compute);
    cache.obtain("a", "k1");
    cache.onMemoryPressure("critical");
    expect(cache.peek("a", "k1")).toBeUndefined();
  });
});
