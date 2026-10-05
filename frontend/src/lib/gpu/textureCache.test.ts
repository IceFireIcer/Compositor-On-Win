import { describe, expect, it, vi } from "vitest";
import { TextureCache } from "./textureCache";

function makeValues() {
  return {
    a: { id: "a" },
    b: { id: "b" },
    c: { id: "c" },
    d: { id: "d" },
  };
}

describe("TextureCache", () => {
  it("stores and retrieves values", () => {
    const v = makeValues();
    const cache = new TextureCache(1000);
    cache.put("a", v.a, 100);
    expect(cache.get("a")).toBe(v.a);
    expect(cache.bytes).toBe(100);
    expect(cache.size).toBe(1);
  });

  it("evicts least-recently-used entries when over budget", () => {
    const v = makeValues();
    const cache = new TextureCache(250);
    cache.put("a", v.a, 100);
    cache.put("b", v.b, 100);
    cache.get("a"); // a becomes most recently used
    cache.put("c", v.c, 100); // b is LRU → evicted
    expect(cache.has("b")).toBe(false);
    expect(cache.has("a")).toBe(true);
    expect(cache.has("c")).toBe(true);
  });

  it("never evicts the entry that was just inserted", () => {
    const v = makeValues();
    const cache = new TextureCache(100);
    cache.put("a", v.a, 100);
    cache.put("b", v.b, 100);
    expect(cache.has("b")).toBe(true);
    expect(cache.has("a")).toBe(false);
  });

  it("refuses values that alone exceed the budget", () => {
    const v = makeValues();
    const cache = new TextureCache(100);
    cache.put("huge", v.a, 200);
    expect(cache.has("huge")).toBe(false);
    expect(cache.bytes).toBe(0);
  });

  it("replacing a key releases the old value and accounting", () => {
    const v = makeValues();
    const onDestroy = vi.fn();
    const cache = new TextureCache(1000, onDestroy);
    cache.put("a", v.a, 100);
    cache.put("a", v.b, 50);
    expect(cache.get("a")).toBe(v.b);
    expect(cache.bytes).toBe(50);
    expect(onDestroy).toHaveBeenCalledWith(v.a);
  });

  it("delete calls onDestroy and clears the accounting", () => {
    const v = makeValues();
    const onDestroy = vi.fn();
    const cache = new TextureCache(1000, onDestroy);
    cache.put("a", v.a, 100);
    expect(cache.delete("a")).toBe(true);
    expect(cache.delete("a")).toBe(false);
    expect(cache.bytes).toBe(0);
    expect(onDestroy).toHaveBeenCalledWith(v.a);
  });

  it("moderate pressure trims to half the budget, keeping MRU entries", () => {
    const v = makeValues();
    const cache = new TextureCache(400);
    cache.put("a", v.a, 100);
    cache.put("b", v.b, 100);
    cache.put("c", v.c, 100);
    cache.put("d", v.d, 100);
    cache.get("a"); // touch a
    cache.onMemoryPressure("moderate"); // trim to 200: b and c evicted (a was touched, d is MRU)
    expect(cache.has("a")).toBe(true);
    expect(cache.has("d")).toBe(true);
    expect(cache.has("b")).toBe(false);
    expect(cache.has("c")).toBe(false);
    expect(cache.bytes).toBe(200);
  });

  it("critical pressure drops everything", () => {
    const v = makeValues();
    const onDestroy = vi.fn();
    const cache = new TextureCache(400, onDestroy);
    cache.put("a", v.a, 100);
    cache.put("b", v.b, 100);
    cache.onMemoryPressure("critical");
    expect(cache.size).toBe(0);
    expect(cache.bytes).toBe(0);
    expect(onDestroy).toHaveBeenCalledTimes(2);
  });
});

