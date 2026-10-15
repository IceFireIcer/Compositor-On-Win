import { describe, expect, it } from "vitest";
import { WebGpuCompositor, type GpuAdapterSource } from "./compositor";

const snapshot = (blendMode = "Normal") => ({
  rev: 1,
  filterRev: 0,
  filterPreview: false,
  doc: {
    width: 2,
    height: 1,
    layers: [{
      id: "layer",
      isVisible: true,
      blendMode,
      imageFile: "layer.rgba",
      transform: { origin: [0, 0], size: [2, 1], rotation: 0, flipX: false, flipY: false },
    }],
  },
  assets: [{
    layerId: "layer",
    kind: "image",
    url: "/pixel/doc/layer/image.rgba?v=1&asset=hash",
    format: "rgba8-premultiplied-srgb",
    width: 2,
    height: 1,
    hash: "hash",
  }],
});

function response(body: unknown, headers: Record<string, string> = {}): unknown {
  return {
    ok: true,
    status: 200,
    headers: { get: (name: string) => headers[name.toLowerCase()] ?? null },
    json: async () => body,
    arrayBuffer: async () => body,
  };
}

function gpuHarness() {
  const state = {
    configured: undefined as Record<string, unknown> | undefined,
    submits: 0,
    submitError: undefined as Error | undefined,
    textureUploads: [] as Array<{ usage: number; format: unknown }>,
  };
  let resolveLost!: (info: { reason: string }) => void;
  const lost = new Promise<{ reason: string }>((resolve) => {
    resolveLost = resolve;
  });
  const texture = () => ({ createView: () => ({}), destroy: () => undefined });
  const context = {
    configure: (config: Record<string, unknown>) => { state.configured = config; },
    getCurrentTexture: texture,
  };
  const device = {
    queue: {
      writeBuffer: () => undefined,
      writeTexture: (destination: Record<string, unknown>) => {
        const uploaded = destination.texture as { usage: number; format: unknown };
        // GPUTextureUsage.COPY_DST is 0x02; 0x08 is STORAGE_BINDING.
        // Validate the actual upload target using WebGPU's texture bits.
        if ((uploaded.usage & 0x02) === 0) {
          throw new Error("WriteTexture destination usage does not include CopyDst");
        }
        state.textureUploads.push({ usage: uploaded.usage, format: uploaded.format });
      },
      submit: () => {
        state.submits += 1;
        if (state.submitError) throw state.submitError;
      },
    },
    createShaderModule: () => ({}),
    createBindGroupLayout: () => ({}),
    createPipelineLayout: () => ({}),
    createRenderPipeline: () => ({}),
    createTexture: (descriptor: Record<string, unknown>) => ({
      ...texture(),
      usage: descriptor.usage as number,
      format: descriptor.format,
      bytes: new ArrayBuffer(64),
    }),
    createBuffer: () => ({ destroy: () => undefined, bytes: new ArrayBuffer(64) }),
    createSampler: () => ({}),
    createBindGroup: () => ({}),
    createCommandEncoder: () => ({
      beginRenderPass: () => ({
        setPipeline: () => undefined,
        setBindGroup: () => undefined,
        draw: () => undefined,
        end: () => undefined,
      }),
      finish: () => ({}),
    }),
    lost,
  };
  const gpu: GpuAdapterSource = {
    requestAdapter: async () => ({ requestDevice: async () => device }),
  };
  return {
    canvas: { width: 2, height: 1, getContext: () => context },
    gpu: gpu as GpuAdapterSource & { getPreferredCanvasFormat: () => string },
    device,
    state,
    lose: () => resolveLost({ reason: "test-device-lost" }),
  };
}

describe("WebGpuCompositor public contract", () => {
  it("declares CPU fallback when WebGPU is unavailable", async () => {
    const canvas = { width: 1, height: 1, getContext: () => null };
    const compositor = await WebGpuCompositor.create(canvas);

    expect(compositor.status.track).toBe("cpu-fallback");
    if (compositor.status.track === "cpu-fallback") expect(compositor.status.reason).toBeTruthy();
    await expect(
      compositor.render({ docId: "doc", rev: 0, filterRev: 0 }),
    ).resolves.toMatchObject({ track: "cpu-fallback" });
    compositor.destroy();
  });

  it("declares CPU fallback when canvas context acquisition throws", async () => {
    const harness = gpuHarness();
    const compositor = await WebGpuCompositor.create(
      {
        width: 1,
        height: 1,
        getContext: () => {
          throw new Error("context unavailable");
        },
      },
      { gpu: harness.gpu },
    );

    expect(compositor.status.track).toBe("cpu-fallback");
    await expect(compositor.render({ docId: "doc", rev: 0, filterRev: 0 })).resolves.toMatchObject({
      track: "cpu-fallback",
    });
    compositor.destroy();
  });

  it("declares CPU fallback when the device queue cannot upload buffers", async () => {
    const harness = gpuHarness();
    (harness.device.queue as { writeBuffer?: unknown }).writeBuffer = undefined;

    const compositor = await WebGpuCompositor.create(harness.canvas, { gpu: harness.gpu });

    expect(compositor.status.track).toBe("cpu-fallback");
    await expect(compositor.render({ docId: "doc", rev: 0, filterRev: 0 })).resolves.toMatchObject({
      track: "cpu-fallback",
    });
    compositor.destroy();
  });

  it.each([Number.NaN, Number.POSITIVE_INFINITY])(
    "uses the default cache budget for non-finite cacheBytes (%s)",
    async (cacheBytes) => {
      const harness = gpuHarness();
      const fetcher = async (url: string) => {
        if (url.includes(".json")) return response(snapshot());
        return response(new Uint8Array([255, 0, 0, 255, 0, 255, 0, 255]).buffer, {
          "x-pixel-width": "2",
          "x-pixel-height": "1",
          "x-pixel-format": "rgba8-premultiplied-srgb",
          "x-pixel-revision": "hash",
        });
      };
      const compositor = await WebGpuCompositor.create(harness.canvas, {
        gpu: harness.gpu,
        fetch: fetcher as unknown as typeof fetch,
        cacheBytes,
      });

      await expect(compositor.render({ docId: "doc", rev: 1, filterRev: 0 })).resolves.toEqual({
        track: "gpu",
      });
      compositor.destroy();
    },
  );

  it("returns cancellation for a render superseded before its fetch settles", async () => {
    const compositor = await WebGpuCompositor.create(
      { width: 1, height: 1, getContext: () => null },
      { gpu: undefined },
    );

    const first = compositor.render({ docId: "first", rev: 1, filterRev: 0 });
    const second = compositor.render({ docId: "second", rev: 2, filterRev: 0 });
    await expect(first).resolves.toMatchObject({ track: "cancelled" });
    await expect(second).resolves.toMatchObject({ track: "cpu-fallback" });
    compositor.destroy();
  });

  it("executes a supported normal raster snapshot through the GPU transport", async () => {
    const harness = gpuHarness();
    const calls: string[] = [];
    const fetcher = async (url: string) => {
      calls.push(url);
      if (url.includes(".json")) return response(snapshot());
      return response(new Uint8Array([255, 0, 0, 255, 0, 255, 0, 255]).buffer, {
        "x-pixel-width": "2",
        "x-pixel-height": "1",
        "x-pixel-format": "rgba8-premultiplied-srgb",
        "x-pixel-revision": "hash",
      });
    };
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });

    await expect(compositor.render({ docId: "doc", rev: 1, filterRev: 0 })).resolves.toEqual({ track: "gpu" });
    expect(harness.state.configured?.alphaMode).toBe("premultiplied");
    expect(harness.state.submits).toBe(1);
    expect(calls[0]).toContain("/render/doc.json?v=1&filterRev=0");
    expect(calls[1]).toContain("/pixel/doc/layer/image.rgba");
    compositor.destroy();
  });

  it("uploads revision-cached layer textures with WebGPU COPY_DST and sampling usage", async () => {
    const harness = gpuHarness();
    const current = snapshot();
    const fetcher = async (url: string) => {
      if (url.includes(".json")) return response(current);
      return response(new Uint8Array([255, 0, 0, 255, 0, 255, 0, 255]).buffer, {
        "x-pixel-width": "2",
        "x-pixel-height": "1",
        "x-pixel-format": "rgba8-premultiplied-srgb",
        "x-pixel-revision": current.assets[0].hash,
      });
    };
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });

    try {
      await expect(compositor.render({ docId: "doc", rev: 1, filterRev: 0 })).resolves.toEqual({ track: "gpu" });
      // 0x16 = COPY_DST (0x02) | TEXTURE_BINDING (0x04) | RENDER_ATTACHMENT (0x10).
      expect(harness.state.textureUploads).toEqual([{ usage: 0x16, format: "rgba8unorm" }]);

      await expect(compositor.render({ docId: "doc", rev: 1, filterRev: 0 })).resolves.toEqual({ track: "gpu" });
      expect(harness.state.textureUploads).toHaveLength(1);

      current.rev = 2;
      current.assets[0].hash = "updated-hash";
      current.assets[0].url = "/pixel/doc/layer/image.rgba?v=2&asset=updated-hash";
      await expect(compositor.render({ docId: "doc", rev: 2, filterRev: 0 })).resolves.toEqual({ track: "gpu" });
      expect(harness.state.textureUploads).toEqual([
        { usage: 0x16, format: "rgba8unorm" },
        { usage: 0x16, format: "rgba8unorm" },
      ]);
    } finally {
      compositor.destroy();
    }
  });

  it("submits each placed layer with its own uniforms after queued writes settle", async () => {
    const harness = gpuHarness();
    type Buffer = { bytes: ArrayBuffer; destroy(): void };
    type BindGroup = { entries: Array<{ binding: number; resource: unknown }> };
    const submitted: number[][] = [];
    const queue = harness.device.queue as unknown as {
      writeBuffer(buffer: Buffer, offset: number, data: ArrayBuffer): void;
      submit(commands: BindGroup[][]): void;
    };
    queue.writeBuffer = (buffer, offset, data) => {
      new Uint8Array(buffer.bytes).set(new Uint8Array(data), offset);
    };
    queue.submit = (commands) => {
      for (const group of commands.flat()) {
        const resource = group.entries.find((entry) => entry.binding === 2)?.resource as
          | { buffer: Buffer; offset?: number }
          | undefined;
        if (resource?.offset !== undefined) submitted.push(Array.from(new Float32Array(resource.buffer.bytes, resource.offset, 8)));
      }
    };
    (harness.device as unknown as { createBuffer: (descriptor: { size: number }) => Buffer }).createBuffer =
      ({ size }) => ({ bytes: new ArrayBuffer(Math.max(size, 16)), destroy: () => undefined });
    (harness.device as unknown as { createBindGroup: (descriptor: BindGroup) => BindGroup }).createBindGroup =
      (descriptor) => descriptor;
    (harness.device as { createCommandEncoder: () => unknown }).createCommandEncoder = () => {
      const groups: BindGroup[] = [];
      return {
        beginRenderPass: () => {
          let group: BindGroup | undefined;
          return {
            setPipeline: () => undefined,
            setBindGroup: (_index: number, bound: BindGroup) => { group = bound; },
            draw: () => { if (group) groups.push(group); },
            end: () => undefined,
          };
        },
        finish: () => groups,
      };
    };
    const layered = {
      rev: 1,
      filterRev: 0,
      filterPreview: false,
      doc: {
        width: 3,
        height: 1,
        layers: [
      { id: "left", isVisible: true, blendMode: "Normal", imageFile: "left.rgba",
        transform: { origin: [0, 0], size: [1, 1], rotation: 0, flipX: false, flipY: false } },
      { id: "right", isVisible: true, blendMode: "Normal", imageFile: "right.rgba", opacity: 0.5,
        transform: { origin: [2, 0], size: [1, 1], rotation: 0, flipX: false, flipY: false } },
        ],
      },
      assets: ["left", "right"].map((layerId) => ({
      layerId, kind: "image", url: `/pixel/doc/${layerId}/image.rgba?v=1&asset=${layerId}`,
      format: "rgba8-premultiplied-srgb", width: 1, height: 1, hash: layerId,
      })),
    };
    const fetcher = async (url: string) => {
      if (url.includes(".json")) return response(layered);
      const hash = url.includes("/left/") ? "left" : "right";
      return response(new Uint8Array([255, 0, 0, 255]).buffer, {
        "x-pixel-width": "1", "x-pixel-height": "1",
        "x-pixel-format": "rgba8-premultiplied-srgb", "x-pixel-revision": hash,
      });
    };
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu, fetch: fetcher as unknown as typeof fetch,
    });

    await expect(compositor.render({ docId: "doc", rev: 1, filterRev: 0 })).resolves.toEqual({ track: "gpu" });
    expect(submitted).toEqual([
      [0, 0, 1, 1, 3, 1, 255, 0],
      [2, 0, 1, 1, 3, 1, 128, 0],
    ]);
    compositor.destroy();
  });

  it("propagates one render signal through snapshot and pixel fetches", async () => {
    const harness = gpuHarness();
    const signals: Array<AbortSignal | null | undefined> = [];
    const fetcher = async (url: string, init?: RequestInit) => {
      signals.push(init?.signal);
      if (url.includes(".json")) return response(snapshot());
      return response(new Uint8Array([255, 0, 0, 255, 0, 255, 0, 255]).buffer, {
        "x-pixel-width": "2",
        "x-pixel-height": "1",
        "x-pixel-format": "rgba8-premultiplied-srgb",
        "x-pixel-revision": "hash",
      });
    };
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });

    await expect(compositor.render({ docId: "doc", rev: 1, filterRev: 0 })).resolves.toEqual({ track: "gpu" });
    expect(signals).toHaveLength(2);
    expect(signals[0]).toBeInstanceOf(AbortSignal);
    expect(signals[1]).toBe(signals[0]);
    compositor.destroy();
  });

  it("restarts a same-revision asset request after the superseded request aborts", async () => {
    const harness = gpuHarness();
    let pixelCalls = 0;
    let pixelStarted!: () => void;
    const firstPixelStarted = new Promise<void>((resolve) => {
      pixelStarted = resolve;
    });
    const fetcher = async (url: string, init?: RequestInit) => {
      if (url.includes(".json")) return response(snapshot());
      pixelCalls += 1;
      if (pixelCalls === 1) {
        pixelStarted();
        return await new Promise<never>((_resolve, reject) => {
          if (init?.signal?.aborted) {
            reject({ name: "AbortError" });
            return;
          }
          init?.signal?.addEventListener(
            "abort",
            () => reject({ name: "AbortError" }),
            { once: true },
          );
        });
      }
      return response(new Uint8Array([255, 0, 0, 255, 0, 255, 0, 255]).buffer, {
        "x-pixel-width": "2",
        "x-pixel-height": "1",
        "x-pixel-format": "rgba8-premultiplied-srgb",
        "x-pixel-revision": "hash",
      });
    };
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });

    const first = compositor.render({ docId: "doc", rev: 1, filterRev: 0 });
    await firstPixelStarted;
    const second = compositor.render({ docId: "doc", rev: 1, filterRev: 0 });

    await expect(first).resolves.toEqual({ track: "cancelled" });
    await expect(second).resolves.toEqual({ track: "gpu" });
    expect(pixelCalls).toBe(2);
    compositor.destroy();
  });

  it("falls back without submitting when pixel metadata disagrees with the snapshot", async () => {
    const harness = gpuHarness();
    const fetcher = async (url: string) => {
      if (url.includes(".json")) return response(snapshot());
      return response(new Uint8Array([255, 0, 0, 255, 0, 255, 0, 255]).buffer, {
        "x-pixel-width": "1",
        "x-pixel-height": "1",
        "x-pixel-format": "rgba8-premultiplied-srgb",
        "x-pixel-revision": "hash",
      });
    };
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });

    const result = await compositor.render({ docId: "doc", rev: 1, filterRev: 0 });

    expect(result.track).toBe("cpu-fallback");
    expect(harness.state.submits).toBe(0);
    compositor.destroy();
  });

  it("falls back without submitting when pixel byte length is wrong", async () => {
    const harness = gpuHarness();
    const fetcher = async (url: string) => {
      if (url.includes(".json")) return response(snapshot());
      return response(new Uint8Array([255, 0, 0, 255]).buffer, {
        "x-pixel-width": "2",
        "x-pixel-height": "1",
        "x-pixel-format": "rgba8-premultiplied-srgb",
        "x-pixel-revision": "hash",
      });
    };
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });

    const result = await compositor.render({ docId: "doc", rev: 1, filterRev: 0 });

    expect(result.track).toBe("cpu-fallback");
    expect(harness.state.submits).toBe(0);
    compositor.destroy();
  });

  it("changes to CPU fallback after device loss and does not submit later frames", async () => {
    const harness = gpuHarness();
    const fetcher = async (url: string) => {
      if (url.includes(".json")) return response(snapshot());
      return response(new Uint8Array([255, 0, 0, 255, 0, 255, 0, 255]).buffer, {
        "x-pixel-width": "2",
        "x-pixel-height": "1",
        "x-pixel-format": "rgba8-premultiplied-srgb",
        "x-pixel-revision": "hash",
      });
    };
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });

    harness.lose();
    await Promise.resolve();
    expect(compositor.status).toEqual({ track: "cpu-fallback", reason: "GPU 设备丢失: test-device-lost" });

    const result = await compositor.render({ docId: "doc", rev: 1, filterRev: 0 });
    expect(result.track).toBe("cpu-fallback");
    expect(harness.state.submits).toBe(0);
    compositor.destroy();
  });

  it("returns cancellation when destroyed during a pending snapshot request", async () => {
    const harness = gpuHarness();
    let signal: AbortSignal | null | undefined;
    let markStarted!: () => void;
    const started = new Promise<void>((resolve) => {
      markStarted = resolve;
    });
    const fetcher = async (_url: string, init?: RequestInit) => {
      signal = init?.signal;
      markStarted();
      return await new Promise<never>((_resolve, reject) => {
        if (signal?.aborted) {
          reject({ name: "AbortError" });
          return;
        }
        signal?.addEventListener("abort", () => reject({ name: "AbortError" }), { once: true });
      });
    };
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });

    const render = compositor.render({ docId: "doc", rev: 1, filterRev: 0 });
    await started;
    compositor.destroy();

    await expect(render).resolves.toEqual({ track: "cancelled" });
    expect(signal?.aborted).toBe(true);
  });

  it("does not touch the transport after destroy", async () => {
    const harness = gpuHarness();
    let calls = 0;
    const fetcher = async () => {
      calls += 1;
      return response(snapshot());
    };
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });

    compositor.destroy();

    await expect(compositor.render({ docId: "doc", rev: 1, filterRev: 0 })).resolves.toEqual({
      track: "cancelled",
    });
    expect(calls).toBe(0);
  });

  it("rejects invalid requests before touching the render transport", async () => {
    const harness = gpuHarness();
    let calls = 0;
    const fetcher = async () => {
      calls += 1;
      return response(snapshot());
    };
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });

    const result = await compositor.render({ docId: "", rev: -1, filterRev: 0 });

    expect(result).toEqual({ track: "cpu-fallback", reason: "渲染请求修订无效" });
    expect(calls).toBe(0);
    compositor.destroy();
  });

  it("rejects document IDs that could escape the render path", async () => {
    const harness = gpuHarness();
    let calls = 0;
    const fetcher = async () => {
      calls += 1;
      return response(snapshot());
    };
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });

    const result = await compositor.render({ docId: "../other-document", rev: 1, filterRev: 0 });

    expect(result).toEqual({ track: "cpu-fallback", reason: "渲染请求修订无效" });
    expect(calls).toBe(0);
    compositor.destroy();
  });

  it("returns CPU fallback for a blend mode without a byte-faithful GPU pass", async () => {
    const harness = gpuHarness();
    const fetcher = async () => response(snapshot("Multiply"));
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });

    await expect(compositor.render({ docId: "doc", rev: 1, filterRev: 0 })).resolves.toMatchObject({
      track: "cpu-fallback",
    });
    expect(harness.state.submits).toBe(0);
    compositor.destroy();
  });

  it("does not flatten pass-through groups with descendant opacity or masks onto the GPU track", async () => {
    const harness = gpuHarness();
    const grouped = {
      rev: 1,
      filterRev: 0,
      filterPreview: false,
      doc: {
        width: 2,
        height: 1,
        layers: [
          {
            id: "group",
            isVisible: true,
            isGroup: true,
            opacity: 0.5,
            blendMode: "Normal",
            maskFile: "group.mask",
            maskEnabled: true,
          },
          {
            id: "child",
            isVisible: true,
            parentID: "group",
            opacity: 0.5,
            blendMode: "Normal",
            imageFile: "child.rgba",
            transform: { origin: [0, 0], size: [2, 1], rotation: 0, flipX: false, flipY: false },
          },
        ],
      },
      assets: [
        {
          layerId: "group",
          kind: "mask",
          url: "/pixel/doc/group/mask.gray",
          format: "gray8",
          width: 2,
          height: 1,
          hash: "group-mask",
        },
        {
          layerId: "child",
          kind: "image",
          url: "/pixel/doc/child/image.rgba",
          format: "rgba8-premultiplied-srgb",
          width: 2,
          height: 1,
          hash: "child-image",
        },
      ],
    };
    const fetcher = async (url: string) => url.includes(".json")
      ? response(grouped)
      : response(new Uint8Array([255, 0, 0, 255, 255, 0, 0, 255]).buffer, {
        "x-pixel-width": "2",
        "x-pixel-height": "1",
        "x-pixel-format": "rgba8-premultiplied-srgb",
        "x-pixel-revision": "child-image",
      });
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });

    const result = await compositor.render({ docId: "doc", rev: 1, filterRev: 0 });

    expect(result).toEqual({ track: "cpu-fallback", reason: "图层 group 使用了未验证的组路径" });
    expect(harness.state.submits).toBe(0);
    compositor.destroy();
  });

  it.each([true, false])("gates a hierarchy before asset loading when parent visibility is %s", async (groupVisible) => {
    const harness = gpuHarness();
    const grouped = {
      rev: 1,
      filterRev: 0,
      filterPreview: false,
      doc: {
        width: 1,
        height: 1,
        layers: [
          { id: "group", isVisible: groupVisible, isGroup: true, opacity: 0.5, blendMode: "Normal" },
          {
            id: "child",
            isVisible: true,
            parentID: "group",
            opacity: 0.5,
            blendMode: "Normal",
            imageFile: "child.rgba",
            transform: { origin: [0, 0], size: [1, 1], rotation: 0, flipX: false, flipY: false },
          },
        ],
      },
      assets: [{
        layerId: "child",
        kind: "image",
        url: "/pixel/doc/child/image.rgba",
        format: "rgba8-premultiplied-srgb",
        width: 1,
        height: 1,
        hash: "child-image",
      }],
    };
    let pixelFetches = 0;
    const fetcher = async (url: string) => {
      if (url.includes(".json")) return response(grouped);
      pixelFetches += 1;
      return response(new Uint8Array([255, 0, 0, 255]).buffer, {
        "x-pixel-width": "1",
        "x-pixel-height": "1",
        "x-pixel-format": "rgba8-premultiplied-srgb",
        "x-pixel-revision": "child-image",
      });
    };
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });

    await expect(compositor.render({ docId: "doc", rev: 1, filterRev: 0 })).resolves.toEqual({
      track: "cpu-fallback",
      reason: `图层 ${groupVisible ? "group" : "child"} 使用了未验证的组路径`,
    });
    expect(pixelFetches).toBe(0);
    expect(harness.state.submits).toBe(0);
    compositor.destroy();
  });

  it("latches CPU fallback after a GPU submission failure", async () => {
    const harness = gpuHarness();
    const fetcher = async (url: string) => {
      if (url.includes(".json")) return response(snapshot());
      return response(new Uint8Array([255, 0, 0, 255, 0, 255, 0, 255]).buffer, {
        "x-pixel-width": "2",
        "x-pixel-height": "1",
        "x-pixel-format": "rgba8-premultiplied-srgb",
        "x-pixel-revision": "hash",
      });
    };
    const compositor = await WebGpuCompositor.create(harness.canvas, {
      gpu: harness.gpu,
      fetch: fetcher as unknown as typeof fetch,
    });
    harness.state.submitError = new Error("queue submit failed");

    const first = await compositor.render({ docId: "doc", rev: 1, filterRev: 0 });

    expect(first.track).toBe("cpu-fallback");
    expect(compositor.status.track).toBe("cpu-fallback");
    const submitsAfterFailure = harness.state.submits;
    await expect(compositor.render({ docId: "doc", rev: 1, filterRev: 0 })).resolves.toMatchObject({
      track: "cpu-fallback",
    });
    expect(harness.state.submits).toBe(submitsAfterFailure);
    compositor.destroy();
  });
});
