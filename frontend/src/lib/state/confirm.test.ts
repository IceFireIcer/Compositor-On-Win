import { describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import { pendingClose, requestClose, resolveClose, type DocTab } from "./confirm";

function doc(dirty: boolean): DocTab {
  return { id: "a", name: "未命名", width: 100, height: 100, resolution: 72, dirty };
}

describe("close confirmation state (review fix I1)", () => {
  it("closes a clean document immediately without confirming", () => {
    pendingClose.set(null);
    const doClose = vi.fn().mockResolvedValue(undefined);
    requestClose(doc(false), doClose);
    expect(doClose).toHaveBeenCalledTimes(1);
    expect(get(pendingClose)).toBeNull();
  });

  it("parks a dirty document in the pending store instead of closing", () => {
    pendingClose.set(null);
    const doClose = vi.fn().mockResolvedValue(undefined);
    const d = doc(true);
    requestClose(d, doClose);
    expect(doClose).not.toHaveBeenCalled();
    expect(get(pendingClose)).toBe(d);
  });

  it("resolveClose(true) closes the pending document and clears it", async () => {
    pendingClose.set(null);
    const doClose = vi.fn().mockResolvedValue(undefined);
    requestClose(doc(true), doClose);
    resolveClose(true, doClose);
    await vi.waitFor(() => expect(doClose).toHaveBeenCalledTimes(1));
    expect(get(pendingClose)).toBeNull();
  });

  it("resolveClose(false) cancels without closing", () => {
    pendingClose.set(null);
    const doClose = vi.fn().mockResolvedValue(undefined);
    requestClose(doc(true), doClose);
    resolveClose(false, doClose);
    expect(doClose).not.toHaveBeenCalled();
    expect(get(pendingClose)).toBeNull();
  });

  it("resolveClose with nothing pending is a no-op", () => {
    pendingClose.set(null);
    const doClose = vi.fn().mockResolvedValue(undefined);
    resolveClose(true, doClose);
    expect(doClose).not.toHaveBeenCalled();
  });
});
