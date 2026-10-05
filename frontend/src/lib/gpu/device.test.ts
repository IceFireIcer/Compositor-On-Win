import { describe, expect, it } from "vitest";
import { probeDevice, type GpuAdapterSource } from "./device";

function sourceWith(behavior: {
  adapter?: unknown | null;
  throws?: boolean;
  device?: unknown;
  deviceThrows?: boolean;
}): GpuAdapterSource {
  return {
    async requestAdapter() {
      if (behavior.throws) throw new Error("blocked");
      return behavior.adapter ?? null;
    },
    // adapter carries requestDevice when the test provides one
    ...(behavior.device !== undefined ? { requestDevice: async () => behavior.device } : {}),
  } as GpuAdapterSource;
}

describe("probeDevice", () => {
  it("reports cpu-fallback when WebGPU is absent", async () => {
    const result = await probeDevice(undefined);
    expect(result.track).toBe("cpu-fallback");
    expect(result.reason).toBeTruthy();
    expect(result.device).toBeUndefined();
  });

  it("reports cpu-fallback when no adapter is available", async () => {
    const result = await probeDevice(sourceWith({ adapter: null }));
    expect(result.track).toBe("cpu-fallback");
    expect(result.reason).toContain("适配器");
  });

  it("reports cpu-fallback when requestAdapter throws", async () => {
    const result = await probeDevice(sourceWith({ throws: true }));
    expect(result.track).toBe("cpu-fallback");
    expect(result.reason).toContain("失败");
  });

  it("reports cpu-fallback when requestDevice rejects", async () => {
    const adapter: GpuAdapterSource = {
      async requestAdapter() {
        return {
          async requestDevice() {
            throw new Error("device lost");
          },
        };
      },
    };
    const result = await probeDevice(adapter);
    expect(result.track).toBe("cpu-fallback");
    expect(result.reason).toContain("设备");
  });

  it("returns the device when WebGPU is fully available", async () => {
    const device = { label: "test-gpu" };
    const adapter: GpuAdapterSource = {
      async requestAdapter() {
        return {
          async requestDevice() {
            return device;
          },
        };
      },
    };
    const result = await probeDevice(adapter);
    expect(result.track).toBe("webgpu");
    expect(result.device).toBe(device);
    expect(result.reason).toBeUndefined();
  });
});
