/**
 * WebGPU device probe with explicit CPU fallback (ticket 11 acceptance:
 * WebGPU 不可用时显式回退 CPU 轨且功能不缺失).
 *
 * The probe never throws — every failure path resolves to a declared
 * fallback with a reason, so the shell can render via the CPU track and
 * surface why GPU is unavailable (status bar / diagnostics). The adapter
 * source is injectable so tests run without a browser.
 */

export type RenderTrack = "webgpu" | "cpu-fallback";

export interface ProbeResult<TDevice> {
  track: RenderTrack;
  device?: TDevice;
  reason?: string;
}

/** Minimal structural shape of navigator.gpu — avoids importing WebGPU types. */
export interface GpuAdapterSource {
  requestAdapter(options?: { powerPreference?: string }): Promise<unknown | null>;
}

export type AdapterSource = GpuAdapterSource | undefined;

/**
 * Probes for a usable GPUDevice:
 *  - no navigator.gpu → cpu-fallback (unsupported browser/engine)
 *  - requestAdapter null → cpu-fallback (no adapter: drivers blocked, remote
 *    session, blocklisted GPU)
 *  - requestDevice rejects → cpu-fallback (adapter present but device lost)
 *  - otherwise → webgpu with the device
 */
export async function probeDevice(source: AdapterSource): Promise<ProbeResult<unknown>> {
  if (!source) {
    return { track: "cpu-fallback", reason: "此环境不支持 WebGPU" };
  }
  let adapter: unknown;
  try {
    adapter = await source.requestAdapter({ powerPreference: "high-performance" });
  } catch (err) {
    return { track: "cpu-fallback", reason: `GPU 适配器请求失败: ${String(err)}` };
  }
  if (!adapter) {
    return { track: "cpu-fallback", reason: "没有可用的 GPU 适配器" };
  }
  const deviceSource = adapter as { requestDevice?: () => Promise<unknown> };
  if (typeof deviceSource.requestDevice !== "function") {
    return { track: "cpu-fallback", reason: "GPU 适配器不提供 requestDevice" };
  }
  try {
    const device = await deviceSource.requestDevice();
    return { track: "webgpu", device };
  } catch (err) {
    return { track: "cpu-fallback", reason: `GPU 设备请求失败: ${String(err)}` };
  }
}

/**
 * Picks the render track from a probe result. The CPU track implements the
 * full feature set (it is the export truth), so falling back loses nothing —
 * the caller renders the same document structure through it.
 */
export function pickTrack<TDevice>(probe: ProbeResult<TDevice>): RenderTrack {
  return probe.track;
}
