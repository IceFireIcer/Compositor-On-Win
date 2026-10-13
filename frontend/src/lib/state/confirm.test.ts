import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import { pendingClose, requestClose, resolveClose, type DocTab } from "./confirm";

vi.mock("../../../wailsjs/go/bridge/Workspace", () => ({ Snapshot: vi.fn() }));
import { Snapshot as GetSnapshot } from "../../../wailsjs/go/bridge/Workspace";
import { bridge } from "../../../wailsjs/go/models";

beforeEach(() => {
  vi.mocked(GetSnapshot).mockResolvedValue(bridge.Snapshot.createFrom({ tabs: [doc(false)], activeId: "a" }));
});

function doc(dirty: boolean): DocTab {
  return { id: "a", name: "未命名", width: 100, height: 100, resolution: 72, dirty };
}

describe("close confirmation state (review fix I1)", () => {
  it("checks backend dirty state before closing a stale clean tab", async () => {
    pendingClose.set(null);
    const current = doc(true);
    vi.mocked(GetSnapshot).mockResolvedValue(bridge.Snapshot.createFrom({ tabs: [current], activeId: "a" }));
    const doClose = vi.fn().mockResolvedValue(undefined);

    await requestClose(doc(false), doClose);

    expect(doClose).not.toHaveBeenCalled();
    expect(get(pendingClose)?.dirty).toBe(true);
  });
  it("closes a clean document immediately without confirming", async () => {
    pendingClose.set(null);
    const doClose = vi.fn().mockResolvedValue(undefined);
    await requestClose(doc(false), doClose);
    expect(doClose).toHaveBeenCalledTimes(1);
    expect(doClose).toHaveBeenCalledWith(expect.objectContaining({ id: "a" }), false);
    expect(get(pendingClose)).toBeNull();
  });

  it("parks a dirty document in the pending store instead of closing", async () => {
    pendingClose.set(null);
    const doClose = vi.fn().mockResolvedValue(undefined);
    const d = doc(true);
    vi.mocked(GetSnapshot).mockResolvedValue(bridge.Snapshot.createFrom({ tabs: [d], activeId: "a" }));
    await requestClose(d, doClose);
    expect(doClose).not.toHaveBeenCalled();
    expect(get(pendingClose)).toEqual(d);
  });

  it("resolveClose(true) closes the pending document and clears it", async () => {
    pendingClose.set(null);
    const doClose = vi.fn().mockResolvedValue(undefined);
    vi.mocked(GetSnapshot).mockResolvedValue(bridge.Snapshot.createFrom({ tabs: [doc(true)], activeId: "a" }));
    await requestClose(doc(true), doClose);
    resolveClose(true, doClose);
    await vi.waitFor(() => expect(doClose).toHaveBeenCalledTimes(1));
    expect(doClose).toHaveBeenCalledWith(expect.objectContaining({ id: "a" }), true);
    expect(get(pendingClose)).toBeNull();
  });

  it("resolveClose(false) cancels without closing", async () => {
    pendingClose.set(null);
    const doClose = vi.fn().mockResolvedValue(undefined);
    vi.mocked(GetSnapshot).mockResolvedValue(bridge.Snapshot.createFrom({ tabs: [doc(true)], activeId: "a" }));
    await requestClose(doc(true), doClose);
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

  it("fails closed when the backend snapshot is unavailable", async () => {
    pendingClose.set(null);
    vi.mocked(GetSnapshot).mockRejectedValueOnce(new Error("bridge unavailable"));
    const doClose = vi.fn().mockResolvedValue(undefined);

    await requestClose(doc(false), doClose);

    expect(doClose).not.toHaveBeenCalled();
    expect(get(pendingClose)?.dirty).toBe(true);
  });
});
