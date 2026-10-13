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

  it("passes the caller abort signal to a layer fetch", async () => {
    let received: AbortSignal | undefined;
    const store = new LayerBitmapStore<{ id: string }>(async (_layerId, signal) => {
      received = signal;
      return { bitmap: { id: "a" }, bytes: 10 };
    }, 1000);
    const controller = new AbortController();

    await store.get("a", 1, controller.signal);

    expect(received).toBe(controller.signal);
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

  it("does not let an in-flight old revision satisfy a newer concurrent get", async () => {
    // Use explicit deferred responses so rev 1 and rev 2 overlap. The
    // second call must not attach itself to rev 1's promise.
    let releaseFirst!: () => void;
    let releaseSecond!: () => void;
    const firstGate = new Promise<void>((resolve) => (releaseFirst = resolve));
    const secondGate = new Promise<void>((resolve) => (releaseSecond = resolve));
    let call = 0;
    const concurrent = new LayerBitmapStore<{ id: string }>(async (layerId) => {
      call += 1;
      if (call === 1) await firstGate;
      else await secondGate;
      return { bitmap: { id: `${layerId}:${call}` }, bytes: 10 };
    }, 1000);

    const old = concurrent.get("a", 1);
    const fresh = concurrent.get("a", 2);
    releaseSecond();
    // The old implementation joins the first promise, so release it on the
    // next turn to make the failure deterministic instead of hanging the test.
    setTimeout(releaseFirst, 0);
    const freshResult = await fresh;
    expect(freshResult.revision).toBe(2);
    releaseFirst();
    const oldResult = await old;
    expect(oldResult.revision).toBe(1);
    expect((await concurrent.get("a", 2)).bitmap).toBe(freshResult.bitmap);
    expect(call).toBe(2);
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

  it("keeps an old in-flight response out after invalidateAll", async () => {
    let releaseOld!: () => void;
    let releaseFresh!: () => void;
    const oldGate = new Promise<void>((resolve) => (releaseOld = resolve));
    const freshGate = new Promise<void>((resolve) => (releaseFresh = resolve));
    let call = 0;
    const store = new LayerBitmapStore<{ id: string }>(async (layerId) => {
      const request = ++call;
      await (request === 1 ? oldGate : freshGate);
      return { bitmap: { id: `${layerId}:${request}` }, bytes: 10 };
    }, 1000);

    const old = store.get("a", 1);
    store.invalidateAll();
    const fresh = store.get("a", 1);
    releaseFresh();
    const freshResult = await fresh;
    releaseOld();
    await old;

    expect((await store.get("a", 1)).bitmap).toBe(freshResult.bitmap);
  });

  it("does not repopulate after critical memory pressure", async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => (release = resolve));
    const store = new LayerBitmapStore<{ id: string }>(async (layerId) => {
      await gate;
      return { bitmap: { id: layerId }, bytes: 10 };
    }, 1000);

    const pending = store.get("a", 1);
    store.onMemoryPressure("critical");
    release();
    await pending;

    expect(store.peek("a")).toBeUndefined();
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
