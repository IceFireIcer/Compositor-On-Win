import { describe, expect, it, vi } from "vitest";
import { PointerStroke } from "./pointerStroke";

describe("pointer gestures across an asynchronous bridge", () => {
  it("keeps editing and committing when a preview observer fails", async () => {
    const point = vi.fn().mockResolvedValue(undefined);
    const end = vi.fn().mockResolvedValue("committed");
    const healthy = vi.fn();
    const stroke = new PointerStroke({ begin: async () => "token", point, end });
    stroke.onProgress(() => { throw new Error("display failed"); });
    stroke.onProgress(healthy);
    await vi.waitFor(() => expect(healthy).toHaveBeenCalledTimes(1));
    stroke.point(3, 4);
    await vi.waitFor(() => expect(healthy).toHaveBeenCalledTimes(2));
    await expect(stroke.finish()).resolves.toBe("committed");
    expect(point).toHaveBeenCalledExactlyOnceWith("token", 3, 4);
    expect(end).toHaveBeenCalledExactlyOnceWith("token", false);
  });

  it("publishes only acknowledged pixels and stops preview observers at finish", async () => {
    let acceptPoint!: () => void;
    const progress = vi.fn();
    const stroke = new PointerStroke({
      begin: async () => "preview-token",
      point: () => new Promise<void>((resolve) => { acceptPoint = resolve; }),
      end: async () => "committed",
    });
    stroke.onProgress(progress);
    await vi.waitFor(() => expect(progress).toHaveBeenCalledExactlyOnceWith({ token: "preview-token", sequence: 1 }));
    stroke.point(3, 4);
    await vi.waitFor(() => expect(acceptPoint).toBeTypeOf("function"));
    expect(progress).toHaveBeenCalledTimes(1);
    acceptPoint();
    await vi.waitFor(() => expect(progress).toHaveBeenLastCalledWith({ token: "preview-token", sequence: 2 }));
    const late = vi.fn();
    stroke.onProgress(late);
    expect(late).toHaveBeenCalledExactlyOnceWith({ token: "preview-token", sequence: 2 });
    await stroke.finish();
    stroke.onProgress(late);
    expect(late).toHaveBeenCalledTimes(1);
  });

  it("awaits begin and every point before ending a fast gesture", async () => {
    let resolveBegin!: (token: string) => void;
    const calls: string[] = [];
    const stroke = new PointerStroke({
      begin: () => new Promise<string>((resolve) => { resolveBegin = resolve; }),
      point: async (token, x) => { calls.push(`${token}:${x}`); },
      end: async (token, cancel) => { calls.push(`${token}:end:${cancel}`); },
    });
    stroke.point(1, 2);
    stroke.point(3, 4);
    const finished = stroke.finish();
    await Promise.resolve();
    expect(calls).toEqual([]);
    resolveBegin("first");
    await finished;
    expect(calls).toEqual(["first:1", "first:3", "first:end:false"]);
  });

  it("cancels once after a rejected point, and reports the original error", async () => {
    const error = new Error("layer changed");
    const point = vi.fn().mockRejectedValue(error);
    const end = vi.fn().mockResolvedValue("");
    const stroke = new PointerStroke({ begin: async () => "gesture", point, end });
    stroke.point(1, 2);
    stroke.point(3, 4);
    await expect(stroke.finish()).rejects.toBe(error);
    expect(point).toHaveBeenCalledTimes(1);
    expect(end).toHaveBeenCalledExactlyOnceWith("gesture", true);
    await stroke.finish();
    expect(end).toHaveBeenCalledTimes(1);
  });

  it("never sends points/end when begin fails", async () => {
    const error = new Error("no paintable layer");
    const point = vi.fn();
    const end = vi.fn();
    const stroke = new PointerStroke({ begin: async () => { throw error; }, point, end });
    stroke.point(1, 2);
    await expect(stroke.finish()).rejects.toBe(error);
    expect(point).not.toHaveBeenCalled();
    expect(end).not.toHaveBeenCalled();
  });
});
