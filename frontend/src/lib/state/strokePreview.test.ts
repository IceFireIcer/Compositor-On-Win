import { describe, expect, it, vi } from "vitest";
import { StrokePreview } from "./strokePreview";

function ok(): Response { return new Response(new Blob(["pixels"], { type: "image/png" })); }

describe("live stroke pixels", () => {
  it("invokes the default browser fetch with Window as its receiver", async () => {
    const original = globalThis.fetch;
    const fetcher = vi.fn(function (this: unknown, input: RequestInfo | URL): Promise<Response> {
      if (this !== globalThis) throw new TypeError("fetch receiver was not Window");
      expect(String(input)).toBe("/pixel/stroke/default.png?sequence=1");
      return Promise.resolve(ok());
    }) as unknown as typeof fetch;
    vi.stubGlobal("fetch", fetcher);
    const display = vi.fn();
    const preview = new StrokePreview(display);
    preview.update({ token: "default", sequence: 1 });
    await vi.waitFor(() => expect(display).toHaveBeenCalledWith(expect.stringMatching(/^blob:/)));
    expect(fetcher).toHaveBeenCalledOnce();
    preview.clear();
    vi.stubGlobal("fetch", original);
  });

  it("coalesces acknowledged points while one frame loads and releases replaced URLs", async () => {
    let resolveFrame!: (response: Response) => void;
    const fetcher = vi.fn<typeof fetch>()
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFrame = resolve; }))
      .mockResolvedValue(ok());
    const display = vi.fn();
    const create = vi.fn().mockReturnValueOnce("blob:first").mockReturnValueOnce("blob:latest");
    const revoke = vi.fn();
    const preview = new StrokePreview(display, fetcher, create, revoke);
    preview.update({ token: "stroke", sequence: 1 });
    preview.update({ token: "stroke", sequence: 2 });
    preview.update({ token: "stroke", sequence: 3 });
    expect(fetcher).toHaveBeenCalledTimes(1);
    resolveFrame(ok());
    await vi.waitFor(() => expect(display).toHaveBeenLastCalledWith("blob:latest"));
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(fetcher.mock.calls[1][0]).toBe("/pixel/stroke/stroke.png?sequence=3");
    expect(revoke).toHaveBeenCalledExactlyOnceWith("blob:first");
    preview.clear();
    expect(display).toHaveBeenLastCalledWith(null);
    expect(revoke).toHaveBeenLastCalledWith("blob:latest");
  });

  it("rejects an old tab's delayed response and still loads the next gesture", async () => {
    let resolveOld!: (response: Response) => void;
    const fetcher = vi.fn<typeof fetch>()
      .mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve; }))
      .mockResolvedValue(ok());
    const display = vi.fn();
    const create = vi.fn().mockReturnValue("blob:new");
    const preview = new StrokePreview(display, fetcher, create, vi.fn());
    preview.update({ token: "old-tab", sequence: 1 });
    preview.clear();
    preview.update({ token: "new-tab", sequence: 1 });
    resolveOld(ok());
    await vi.waitFor(() => expect(display).toHaveBeenLastCalledWith("blob:new"));
    expect(create).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[1][0]).toBe("/pixel/stroke/new-tab.png?sequence=1");
    preview.clear();
  });

  it("retains the last frame if End retires the token during a fetch", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValueOnce(ok()).mockResolvedValue(new Response(null, { status: 404 }));
    const display = vi.fn();
    const preview = new StrokePreview(display, fetcher, () => "blob:last", vi.fn());
    preview.update({ token: "ended", sequence: 1 });
    await vi.waitFor(() => expect(display).toHaveBeenCalledWith("blob:last"));
    preview.update({ token: "ended", sequence: 2 });
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
    expect(display).toHaveBeenCalledTimes(1);
    preview.clear();
  });
});
