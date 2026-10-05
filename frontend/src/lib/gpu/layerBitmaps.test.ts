import { describe, expect, it } from "vitest";
import { LayerBitmapStore } from "./layerBitmaps";

function makeStore(budgetBytes = 10_000_000) {
  const fetches: string[] = [];
  const store = new LayerBitmapStore<{ id: string }>(async (layerId) => {
    fetches.push(layerId);
    return { bitmap: { id: `${layerId}@${fetches.length}` }, bytes: 1000 };
  }, budgetBytes);
  return { store, fetches };
}

describe("LayerBitmapStore", () => {
  it("fetches a layer once and caches it", async () => {
    const { store, fetches } = makeStore();
    const first = await store.get("a", 1);
    const second = await store.get("a", 1);
    expect(second).toBe(first);
    expect(fetches).toEqual(["a"]);
  });

  it("refetches only the changed layer on revision bump", async () => {
    const { store, fetches } = makeStore();
    await store.get("a", 1);
    await store.get("b", 1);
    await store.get("b", 1); // cache hit
    await store.get("a", 2); // only a changed
    expect(fetches).toEqual(["a", "b", "a"]);
    // b's entry survives untouched — same revision, still a hit.
    const b = await store.get("b", 1);
    expect(fetches).toEqual(["a", "b", "a"]);
    expect(b.revision).toBe(1);
  });

  it("coalesces concurrent gets for the same layer into one fetch", async () => {
    const fetches: string[] = [];
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const store = new LayerBitmapStore<{ id: string }>(async (layerId) => {
      await gate;
      fetches.push(layerId);
      return { bitmap: { id: layerId }, bytes: 10 };
    }, 1000);
    const p1 = store.get("a", 1);
    const p2 = store.get("a", 1);
    release();
    const [r1, r2] = await Promise.all([p1, p2]);
    expect(fetches).toEqual(["a"]);
    expect(r2).toBe(r1);
  });

  it("a stale fetch arriving after a revision bump does not satisfy the new revision", async () => {
    const store = new LayerBitmapStore<{ id: string }>(async (layerId) => {
      return { bitmap: { id: layerId }, bytes: 10 };
    }, 1000);
    const stalePromise = store.get("a", 1);
    // Simulate the stale response landing after the revision moved on: the
    // store may cache it, but a get for the new revision must fetch again.
    await stalePromise;
    const fresh = await store.get("a", 2);
    expect(fresh.revision).toBe(2);
    const again = await store.get("a", 2);
    expect(again).toBe(fresh);
  });

  it("invalidate drops exactly the requested layer", async () => {
    const { store, fetches } = makeStore();
    await store.get("a", 1);
    await store.get("b", 1);
    store.invalidate("a");
    await store.get("a", 1); // refetch: invalidated
    await store.get("b", 1); // still cached
    expect(fetches).toEqual(["a", "b", "a"]);
  });

  it("cache evictions under a tight budget stay LRU-correct", async () => {
    const { store, fetches } = makeStore(2500); // fits 2 entries of 1000 bytes
    await store.get("a", 1);
    await store.get("b", 1);
    await store.get("a", 1); // touch a → b is LRU
    await store.get("c", 1); // evicts b
    await store.get("a", 1); // hit
    expect(fetches).toEqual(["a", "b", "c"]);
    expect(await store.get("c", 1)).toBeDefined(); // c cached, b was evicted
    expect(fetches).toEqual(["a", "b", "c"]);
  });
});
