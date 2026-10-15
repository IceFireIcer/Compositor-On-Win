/**
 * Production WebGPU compositor boundary.
 *
 * The first executor is deliberately conservative: it renders visible,
 * normal-blend raster layers with integer, axis-aligned placement. Anything
 * outside that subset returns an explicit CPU fallback instead of producing
 * pixels whose semantics have not been proven against internal/render.
 */
import { probeDevice, type GpuAdapterSource } from "./device";
import { LayerBitmapStore, type LayerBitmap } from "./layerBitmaps";
import { buildRenderGraph, type GraphLayer, type RenderDoc } from "./renderGraph";
import { registerPressureHandler } from "../state/memory";

export type { GpuAdapterSource } from "./device";

export interface RenderRequest {
  docId: string;
  rev: number;
  filterRev: number;
}

export type CompositorStatus =
  | { track: "webgpu" }
  | { track: "cpu-fallback"; reason: string };

export type RenderResult =
  | { track: "gpu" }
  | { track: "cpu-fallback"; reason: string }
  | { track: "cancelled" };

/** Structural canvas type keeps the public seam usable in Vitest and Wails. */
export interface CanvasLike {
  width: number;
  height: number;
  getContext(contextId: string): unknown;
}

export interface WebGpuCompositorOptions {
  gpu?: GpuAdapterSource;
  fetch?: typeof fetch;
  assetBaseUrl?: string;
  cacheBytes?: number;
}

interface GpuCanvasContextLike {
  configure(configuration: Record<string, unknown>): void;
  getCurrentTexture(): GpuTextureLike;
}

interface GpuTextureLike {
  createView(): unknown;
  destroy?: () => void;
}

interface GpuBufferLike {
  destroy?: () => void;
}

interface GpuPassLike {
  setPipeline(pipeline: unknown): void;
  setBindGroup(index: number, bindGroup: unknown): void;
  draw(vertexCount: number): void;
  end(): void;
}

interface GpuCommandEncoderLike {
  beginRenderPass(descriptor: Record<string, unknown>): GpuPassLike;
  finish(): unknown;
}

interface GpuQueueLike {
  writeBuffer(buffer: GpuBufferLike, bufferOffset: number, data: ArrayBuffer): void;
  writeTexture(
    destination: Record<string, unknown>,
    data: ArrayBufferView,
    dataLayout: Record<string, number>,
    size: Record<string, number>,
  ): void;
  submit(commandBuffers: unknown[]): void;
}

interface GpuDeviceLike {
  queue: GpuQueueLike;
  createShaderModule(descriptor: { code: string }): unknown;
  createBindGroupLayout(descriptor: Record<string, unknown>): unknown;
  createPipelineLayout(descriptor: Record<string, unknown>): unknown;
  createRenderPipeline(descriptor: Record<string, unknown>): unknown;
  createTexture(descriptor: Record<string, unknown>): GpuTextureLike;
  createBuffer(descriptor: Record<string, unknown>): GpuBufferLike;
  createSampler?(descriptor: Record<string, unknown>): unknown;
  createBindGroup(descriptor: Record<string, unknown>): unknown;
  createCommandEncoder(): GpuCommandEncoderLike;
  lost?: Promise<{ reason?: string; message?: string }>;
  destroy?: () => void;
}

interface FetchHeadersLike {
  get(name: string): string | null;
}

interface FetchResponseLike {
  ok?: boolean;
  status?: number;
  headers?: FetchHeadersLike;
  json(): Promise<unknown>;
  arrayBuffer(): Promise<ArrayBuffer>;
}

type RenderFetch = (
  input: string,
  init?: RequestInit,
) => Promise<FetchResponseLike>;

interface WireSnapshot {
  rev: number;
  filterRev: number;
  filterPreview: boolean;
  doc: WireDocument;
  assets: WireAsset[];
}

interface WireDocument {
  width: number;
  height: number;
  layers: WireLayer[];
}

interface WireLayer {
  id: string;
  isVisible: boolean;
  isGroup?: boolean;
  parentID?: string | null;
  opacity?: number;
  blendMode?: string;
  imageFile?: string | null;
  maskFile?: string | null;
  maskEnabled?: boolean;
  maskSourceID?: string | null;
  maskLinked?: boolean;
  adjustment?: unknown;
  shape?: unknown;
  effects?: unknown;
  text?: unknown;
  transform?: WireTransform;
}

interface WireTransform {
  origin?: unknown;
  size?: unknown;
  rotation?: unknown;
  flipX?: unknown;
  flipY?: unknown;
  sampling?: unknown;
}

interface WireAsset {
  layerId: string;
  kind: string;
  url: string;
  format: string;
  width: number;
  height: number;
  hash: string;
}

interface LoadedAsset {
  texture: GpuTextureLike;
  width: number;
  height: number;
}

interface AssetRevision {
  revision: number;
  descriptor: WireAsset;
}

interface RenderTargets {
  width: number;
  height: number;
  textures: [GpuTextureLike, GpuTextureLike];
}

const DEFAULT_CACHE_BYTES = 256 * 1024 * 1024;
const MAX_GPU_PIXELS = 268_435_456;
const GPU_BUFFER_USAGE_UNIFORM = 0x40;
const GPU_BUFFER_USAGE_COPY_DST = 0x08;
const UNIFORM_SLOT_BYTES = 256;
const GPU_TEXTURE_USAGE_COPY_DST = 0x02;
const GPU_TEXTURE_USAGE_TEXTURE_BINDING = 0x04;
const GPU_TEXTURE_USAGE_RENDER_ATTACHMENT = 0x10;

function normalizeCacheBytes(value: number | undefined): number {
  return typeof value === "number" && Number.isFinite(value)
    ? Math.max(1, Math.floor(value))
    : DEFAULT_CACHE_BYTES;
}

/** Exact byte-domain Normal-over shader for premultiplied RGBA8 inputs. */
const NORMAL_COMPOSITE_WGSL = /* wgsl */ `
struct Uniforms {
  origin: vec2f,
  sourceSize: vec2f,
  documentSize: vec2f,
  opacityByte: f32,
  pad: f32,
};

@group(0) @binding(0) var dstTexture: texture_2d<f32>;
@group(0) @binding(1) var srcTexture: texture_2d<f32>;
@group(0) @binding(2) var<uniform> uniforms: Uniforms;

struct VertexOut {
  @builtin(position) position: vec4f,
  @location(0) uv: vec2f,
};

@vertex
fn vsMain(@builtin(vertex_index) index: u32) -> VertexOut {
  let positions = array<vec2f, 6>(
    vec2f(0.0, 0.0), vec2f(1.0, 0.0), vec2f(0.0, 1.0),
    vec2f(0.0, 1.0), vec2f(1.0, 0.0), vec2f(1.0, 1.0)
  );
  var out: VertexOut;
  let uv = positions[index];
  out.position = vec4f(vec2f(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0), 0.0, 1.0);
  out.uv = uv;
  return out;
}

fn byteRound(v: f32) -> f32 {
  return floor(clamp(v, 0.0, 1.0) * 255.0 + 0.5) / 255.0;
}

@fragment
fn fsMain(input: VertexOut) -> @location(0) vec4f {
  let dstCoord = vec2i(floor(input.uv * uniforms.documentSize));
  let dst = textureLoad(dstTexture, dstCoord, 0);
  let dstR = floor(dst.r * 255.0 + 0.5);
  let dstG = floor(dst.g * 255.0 + 0.5);
  let dstB = floor(dst.b * 255.0 + 0.5);
  let dstA = floor(dst.a * 255.0 + 0.5);

  let local = vec2f(dstCoord) - uniforms.origin;
  var src = vec4f(0.0);
  if (local.x >= 0.0 && local.y >= 0.0 &&
      local.x < uniforms.sourceSize.x && local.y < uniforms.sourceSize.y) {
    src = textureLoad(srcTexture, vec2i(local), 0);
  }

  let srcR = floor(src.r * 255.0 + 0.5);
  let srcG = floor(src.g * 255.0 + 0.5);
  let srcB = floor(src.b * 255.0 + 0.5);
  let srcA = floor(src.a * 255.0 + 0.5);
  let scaledA = floor(srcA * uniforms.opacityByte / 255.0);
  if (scaledA <= 0.0) {
    return vec4f(byteRound(dstR / 255.0), byteRound(dstG / 255.0),
      byteRound(dstB / 255.0), byteRound(dstA / 255.0));
  }

  // scaleAlpha in internal/render first floors each premultiplied byte, so
  // use the same byte-domain operation before the Normal equation.
  let scaledR = floor(srcR * uniforms.opacityByte / 255.0);
  let scaledG = floor(srcG * uniforms.opacityByte / 255.0);
  let scaledB = floor(srcB * uniforms.opacityByte / 255.0);
  let sourceCoverage = scaledA / 255.0;
  let backdropCoverage = dstA / 255.0;
  let outA = sourceCoverage + backdropCoverage * (1.0 - sourceCoverage);
  let outR = scaledR / 255.0 + (dstR / 255.0) * (1.0 - sourceCoverage);
  let outG = scaledG / 255.0 + (dstG / 255.0) * (1.0 - sourceCoverage);
  let outB = scaledB / 255.0 + (dstB / 255.0) * (1.0 - sourceCoverage);
  return vec4f(byteRound(outR), byteRound(outG), byteRound(outB), byteRound(outA));
}
`;

const PRESENT_WGSL = /* wgsl */ `
@group(0) @binding(0) var sourceSampler: sampler;
@group(0) @binding(1) var sourceTexture: texture_2d<f32>;
@group(0) @binding(2) var proofTexture: texture_3d<f32>;
@group(0) @binding(3) var<uniform> proofMode: vec4u;

struct VertexOut {
  @builtin(position) position: vec4f,
  @location(0) uv: vec2f,
};

@vertex
fn vsMain(@builtin(vertex_index) index: u32) -> VertexOut {
  let positions = array<vec2f, 6>(
    vec2f(0.0, 0.0), vec2f(1.0, 0.0), vec2f(0.0, 1.0),
    vec2f(0.0, 1.0), vec2f(1.0, 0.0), vec2f(1.0, 1.0)
  );
  var out: VertexOut;
  let uv = positions[index];
  out.position = vec4f(vec2f(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0), 0.0, 1.0);
  out.uv = uv;
  return out;
}

fn proofSample(color: vec3f) -> vec4f {
  let n = f32(textureDimensions(proofTexture).x);
  let coord = clamp(color, vec3f(0.0), vec3f(1.0)) * (n - 1.0);
  let base = vec3u(floor(coord));
  let next = min(base + vec3u(1u), vec3u(u32(n) - 1u));
  let f = fract(coord);
  let c000 = textureLoad(proofTexture, vec3u(base.x, base.y, base.z), 0);
  let c100 = textureLoad(proofTexture, vec3u(next.x, base.y, base.z), 0);
  let c010 = textureLoad(proofTexture, vec3u(base.x, next.y, base.z), 0);
  let c110 = textureLoad(proofTexture, vec3u(next.x, next.y, base.z), 0);
  let c001 = textureLoad(proofTexture, vec3u(base.x, base.y, next.z), 0);
  let c101 = textureLoad(proofTexture, vec3u(next.x, base.y, next.z), 0);
  let c011 = textureLoad(proofTexture, vec3u(base.x, next.y, next.z), 0);
  let c111 = textureLoad(proofTexture, vec3u(next.x, next.y, next.z), 0);
  let c00 = mix(c000, c100, f.x);
  let c10 = mix(c010, c110, f.x);
  let c01 = mix(c001, c101, f.x);
  let c11 = mix(c011, c111, f.x);
  return mix(mix(c00, c10, f.y), mix(c01, c11, f.y), f.z);
}

@fragment
fn fsMain(input: VertexOut) -> @location(0) vec4f {
  let src = textureSampleLevel(sourceTexture, sourceSampler, input.uv, 0.0);
  if (proofMode.x == 0u) {
    return src;
  }
  // Upstream flattens onto white before the cube (EditorCanvas proof).
  // The canvas sample is premultiplied, so un-premultiply, composite over
  // white, then sample. The lookup already holds the gray gamut mark.
  let alpha = src.a;
  let straight = select(vec3f(0.0), src.rgb / max(alpha, 0.0001), alpha > 0.0);
  let overWhite = straight * alpha + vec3f(1.0 - alpha);
  let looked = proofSample(overWhite);
  return vec4f(looked.rgb, 1.0);
}
`;

function asRecord(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" ? (value as Record<string, unknown>) : null;
}

function finiteInt(value: unknown): number | null {
  return typeof value === "number" && Number.isSafeInteger(value) ? value : null;
}

function finiteNumber(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function validDocumentId(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0 && value !== "." && value !== ".." &&
    !/[\\/?#\u0000-\u001f]/.test(value);
}

function pair(value: unknown): [number, number] | null {
  if (!Array.isArray(value) || value.length !== 2) return null;
  const x = finiteNumber(value[0]);
  const y = finiteNumber(value[1]);
  return x === null || y === null ? null : [x, y];
}

function header(response: FetchResponseLike, name: string): string | null {
  return response.headers?.get(name) ?? null;
}

function defaultGpuSource(): GpuAdapterSource | undefined {
  const runtime = globalThis as typeof globalThis & {
    navigator?: { gpu?: GpuAdapterSource };
  };
  return runtime.navigator?.gpu;
}

function joinURL(base: string, path: string): string {
  if (!base) return path;
  return new URL(path, base.endsWith("/") ? base : `${base}/`).toString();
}

function isAbortError(error: unknown): boolean {
  const errorName = asRecord(error)?.name;
  return (typeof DOMException !== "undefined" && error instanceof DOMException && error.name === "AbortError") ||
    errorName === "AbortError";
}

function isUsableDevice(value: unknown): value is GpuDeviceLike {
  const device = value as Partial<GpuDeviceLike> | null;
  const queue = asRecord(device?.queue);
  return !!device && typeof device === "object" && !!queue &&
    typeof queue.writeBuffer === "function" &&
    typeof queue.writeTexture === "function" &&
    typeof queue.submit === "function" &&
    typeof device.createShaderModule === "function" &&
    typeof device.createBindGroupLayout === "function" &&
    typeof device.createPipelineLayout === "function" &&
    typeof device.createRenderPipeline === "function" &&
    typeof device.createTexture === "function" &&
    typeof device.createBuffer === "function" &&
    typeof device.createBindGroup === "function" &&
    typeof device.createCommandEncoder === "function";
}

function textureUsage(): number {
  return GPU_TEXTURE_USAGE_COPY_DST | GPU_TEXTURE_USAGE_TEXTURE_BINDING |
    GPU_TEXTURE_USAGE_RENDER_ATTACHMENT;
}

/**
 * 着色器该用哪个校样模式。没有查找表就只能直通：把一个非 0 的 mode 放过去，
 * 着色器会去采样上一次绑定的那张纹理（旧配置的，或者 1×1 占位），于是菜单勾了
 * 新配置、画布还按旧的校样显示（票 72）。
 */
export function proofModeFor(mode: number, lookup: Uint8Array | null, dimension: number): number {
  return lookup && dimension >= 2 ? mode : 0;
}

export class WebGpuCompositor {
  private statusValue: CompositorStatus;
  private readonly context: GpuCanvasContextLike | null;
  private readonly device: GpuDeviceLike | null;
  private readonly fetcher: RenderFetch | null;
  private readonly assetBaseUrl: string;
  private readonly layerStore: LayerBitmapStore<LoadedAsset>;
  private readonly assetDescriptors = new Map<string, AssetRevision>();
  private readonly assetRevisions = new Map<string, AssetRevision>();
  private assetRevisionCounter = 0;
  private unregisterPressure: (() => void) | null = null;
  private renderTargets: RenderTargets | null = null;
  private compositePipeline: unknown;
  private compositeLayout: unknown;
  private presentPipeline: unknown;
  private presentLayout: unknown;
  private presentSampler: unknown;
  private proofTexture: { createView: () => unknown; destroy?: () => void } | null = null;
  private proofBuffer: GpuBufferLike | null = null;
  private proofMode = 0;
  private uniformBuffer: GpuBufferLike | null = null;
  private uniformCapacity = 0;
  private canvasFormat = "rgba8unorm";
  private renderGeneration = 0;
  private activeAbort: AbortController | null = null;
  private destroyed = false;

  private constructor(
    canvas: CanvasLike,
    options: WebGpuCompositorOptions,
    status: CompositorStatus,
    device: GpuDeviceLike | null,
  ) {
    this.statusValue = status;
    this.device = device;
    let context: GpuCanvasContextLike | null = null;
    if (device) {
      try {
        context = canvas.getContext("webgpu") as GpuCanvasContextLike | null;
      } catch {
        // Context acquisition is an ordinary capability failure; create()
        // will convert the null context into the declared CPU track.
        context = null;
      }
    }
    this.context = context;
    this.assetBaseUrl = options.assetBaseUrl ?? "";
    const candidate = options.fetch ?? (
      typeof globalThis.fetch === "function" ? globalThis.fetch.bind(globalThis) : undefined
    );
    this.fetcher = candidate as RenderFetch | undefined ?? null;
    this.layerStore = new LayerBitmapStore<LoadedAsset>(
      (layerId, signal, revision) => this.fetchLayerAsset(layerId, revision, signal),
      normalizeCacheBytes(options.cacheBytes),
      (asset) => asset.texture.destroy?.(),
    );
  }

  static async create(
    canvas: CanvasLike,
    options: WebGpuCompositorOptions = {},
  ): Promise<WebGpuCompositor> {
    const source = options.gpu ?? defaultGpuSource();
    const probe = await probeDevice(source);
    const device = probe.track === "webgpu" && isUsableDevice(probe.device)
      ? probe.device
      : null;
    const status: CompositorStatus = device
      ? { track: "webgpu" }
      : { track: "cpu-fallback", reason: probe.reason ?? "GPU 设备接口不完整" };
    const compositor = new WebGpuCompositor(canvas, options, status, device);
    if (!device) return compositor;
    try {
      compositor.initializeGpu(source);
    } catch (error) {
      compositor.setFallback(`GPU 管线初始化失败: ${String(error)}`);
    }
    return compositor;
  }

  get status(): CompositorStatus {
    return this.statusValue;
  }

  async render(request: RenderRequest): Promise<RenderResult> {
    if (this.destroyed) return { track: "cancelled" };
    const generation = ++this.renderGeneration;
    this.activeAbort?.abort();
    const abort = new AbortController();
    this.activeAbort = abort;
    try {
      const result = await this.renderInternal(request, generation, abort.signal);
      return this.isCurrent(generation) ? result : { track: "cancelled" };
    } catch (error) {
      if (!this.isCurrent(generation)) return { track: "cancelled" };
      if (isAbortError(error)) return { track: "cancelled" };
      return { track: "cpu-fallback", reason: `GPU 渲染失败: ${String(error)}` };
    } finally {
      if (this.activeAbort === abort) this.activeAbort = null;
    }
  }

  /**
   * 校样是显示变换（票 72）。mode 0 直通，非 0 时按查找表替换颜色；标灰是查找表
   * 自己带的内容（`marked` 表把色域外像素写成灰 0.5），着色器不再另加洋红。
   * lookup 是 33³ 的 RGBA float32，小端。关闭时不传表。
   */
  setProof(mode: number, lookup: Uint8Array | null, dimension: number): void {
    this.proofMode = proofModeFor(mode, lookup, dimension);
    if (!this.device || !lookup || dimension < 2) return;
    this.proofTexture?.destroy?.();
    this.proofTexture = this.device.createTexture({
      size: { width: dimension, height: dimension, depthOrArrayLayers: dimension },
      dimension: "3d",
      format: "rgba32float",
      usage: textureUsage(),
    }) as { createView: () => unknown; destroy?: () => void };
    this.device.queue.writeTexture(
      { texture: this.proofTexture },
      lookup,
      { bytesPerRow: dimension * 16, rowsPerImage: dimension },
      { width: dimension, height: dimension, depthOrArrayLayers: dimension },
    );
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    this.renderGeneration += 1;
    this.activeAbort?.abort();
    this.activeAbort = null;
    this.unregisterPressure?.();
    this.unregisterPressure = null;
    this.layerStore.invalidateAll();
    this.destroyRenderTargets();
    this.uniformBuffer?.destroy?.();
    this.uniformBuffer = null;
    this.uniformCapacity = 0;
    this.proofTexture?.destroy?.();
    this.proofTexture = null;
    this.proofBuffer?.destroy?.();
    this.proofBuffer = null;
    this.device?.destroy?.();
  }

  private initializeGpu(source: GpuAdapterSource | undefined): void {
    if (!this.device || !this.context) {
      throw new Error("canvas 不提供 webgpu 上下文");
    }
    const sourceWithFormat = source as GpuAdapterSource & {
      getPreferredCanvasFormat?: () => string;
    };
    this.canvasFormat = sourceWithFormat.getPreferredCanvasFormat?.() ?? "rgba8unorm";
    this.context.configure({
      device: this.device,
      format: this.canvasFormat,
      alphaMode: "premultiplied",
    });

    const compositeShader = this.device.createShaderModule({ code: NORMAL_COMPOSITE_WGSL });
    const compositeEntries = [
      { binding: 0, visibility: 2, texture: { sampleType: "float", viewDimension: "2d" } },
      { binding: 1, visibility: 2, texture: { sampleType: "float", viewDimension: "2d" } },
      { binding: 2, visibility: 2, buffer: { type: "uniform" } },
    ];
    this.compositeLayout = this.device.createBindGroupLayout({ entries: compositeEntries });
    const compositePipelineLayout = this.device.createPipelineLayout({
      bindGroupLayouts: [this.compositeLayout],
    });
    this.compositePipeline = this.device.createRenderPipeline({
      layout: compositePipelineLayout,
      vertex: { module: compositeShader, entryPoint: "vsMain" },
      fragment: {
        module: compositeShader,
        entryPoint: "fsMain",
        targets: [{ format: "rgba8unorm" }],
      },
      primitive: { topology: "triangle-list" },
    });

    const presentShader = this.device.createShaderModule({ code: PRESENT_WGSL });
    const presentEntries = [
      { binding: 0, visibility: 2, sampler: { type: "non-filtering" } },
      { binding: 1, visibility: 2, texture: { sampleType: "float", viewDimension: "2d" } },
      { binding: 2, visibility: 2, texture: { sampleType: "float", viewDimension: "3d" } },
      { binding: 3, visibility: 2, buffer: { type: "uniform" } },
    ];
    this.presentLayout = this.device.createBindGroupLayout({ entries: presentEntries });
    const presentPipelineLayout = this.device.createPipelineLayout({
      bindGroupLayouts: [this.presentLayout],
    });
    this.presentPipeline = this.device.createRenderPipeline({
      layout: presentPipelineLayout,
      vertex: { module: presentShader, entryPoint: "vsMain" },
      fragment: {
        module: presentShader,
        entryPoint: "fsMain",
        targets: [{ format: this.canvasFormat }],
      },
      primitive: { topology: "triangle-list" },
    });
    this.presentSampler = this.device.createSampler?.({
      magFilter: "nearest",
      minFilter: "nearest",
    });
    if (!this.presentSampler) throw new Error("GPU sampler 创建失败");
    this.proofTexture = this.device.createTexture({
      size: { width: 1, height: 1, depthOrArrayLayers: 1 },
      dimension: "3d",
      format: "rgba32float",
      usage: textureUsage(),
    }) as { createView: () => unknown; destroy?: () => void };
    this.proofBuffer = this.device.createBuffer({
      size: 16,
      usage: GPU_BUFFER_USAGE_UNIFORM | GPU_BUFFER_USAGE_COPY_DST,
    });
    if (this.device.lost) {
      void this.device.lost.then((info) => {
        if (!this.destroyed) this.setFallback(`GPU 设备丢失: ${info.reason ?? info.message ?? "未知原因"}`);
      }).catch(() => undefined);
    }
    this.unregisterPressure = registerPressureHandler((level) => {
      if (level === "moderate" || level === "critical") this.layerStore.onMemoryPressure(level);
    });
  }

  private async renderInternal(
    request: RenderRequest,
    generation: number,
    signal: AbortSignal,
  ): Promise<RenderResult> {
    if (!request || !validDocumentId(request.docId) || !Number.isSafeInteger(request.rev) || request.rev < 0 ||
        !Number.isSafeInteger(request.filterRev) || request.filterRev < 0) {
      return { track: "cpu-fallback", reason: "渲染请求修订无效" };
    }
    if (this.statusValue.track !== "webgpu" || !this.device || !this.context) {
      await Promise.resolve();
      const reason = this.statusValue.track === "cpu-fallback"
        ? this.statusValue.reason
        : "WebGPU 不可用";
      return { track: "cpu-fallback", reason };
    }
    if (!this.fetcher) return { track: "cpu-fallback", reason: "浏览器 fetch 不可用" };
    const snapshot = await this.fetchSnapshot(request, signal);
    if (!this.isCurrent(generation)) return { track: "cancelled" };
    const unsupported = this.unsupportedReason(snapshot, request);
    if (unsupported) return { track: "cpu-fallback", reason: unsupported };
    this.prepareAssets(snapshot.assets);

    const imageLayers = snapshot.doc.layers.filter(
      (layer) => layer.isVisible && typeof layer.imageFile === "string",
    );
    const loaded = new Map<string, LayerBitmap<LoadedAsset>>();
    for (const layer of imageLayers) {
      if (!this.isCurrent(generation)) return { track: "cancelled" };
      const revision = this.assetRevisions.get(layer.id)?.revision;
      if (revision === undefined) return { track: "cpu-fallback", reason: `图层 ${layer.id} 缺少像素资产` };
      try {
        loaded.set(layer.id, await this.layerStore.get(layer.id, revision, signal));
      } catch (error) {
        if (!this.isCurrent(generation)) return { track: "cancelled" };
        return { track: "cpu-fallback", reason: `图层 ${layer.id} 像素读取失败: ${String(error)}` };
      }
    }
    if (!this.isCurrent(generation)) return { track: "cancelled" };
    try {
      this.execute(snapshot, loaded);
    } catch (error) {
      // A command-encoding or submission failure is a device-level failure,
      // not a document feature mismatch. Latch the CPU track so the next
      // frame does not repeatedly submit to a broken device.
      const reason = `GPU 渲染失败: ${String(error)}`;
      this.setFallback(reason, false);
      return { track: "cpu-fallback", reason };
    }
    return { track: "gpu" };
  }

  private async fetchSnapshot(request: RenderRequest, signal: AbortSignal): Promise<WireSnapshot> {
    const path = `/render/${encodeURIComponent(request.docId)}.json?v=${encodeURIComponent(String(request.rev))}` +
      `&filterRev=${encodeURIComponent(String(request.filterRev))}`;
    const response = await this.fetcher!(joinURL(this.assetBaseUrl, path), {
      method: "GET",
      cache: "no-store",
      signal,
    });
    const status = response.status ?? (response.ok === false ? 500 : 200);
    if (status !== 200) throw new Error(`渲染快照 HTTP ${status}`);
    const raw = await response.json();
    const record = asRecord(raw);
    if (!record) throw new Error("渲染快照格式无效");
    const rev = finiteInt(record.rev);
    const filterRev = finiteInt(record.filterRev);
    const docRecord = asRecord(record.doc);
    if (rev === null || filterRev === null || !docRecord) throw new Error("渲染快照缺少修订或文档");
    if (rev !== request.rev || filterRev !== request.filterRev) throw new Error("渲染快照修订不匹配");
    const width = finiteInt(docRecord.width);
    const height = finiteInt(docRecord.height);
    const rawLayers = docRecord.layers;
    const rawAssets = record.assets;
    if (width === null || height === null || width <= 0 || height <= 0 ||
        width * height > MAX_GPU_PIXELS ||
        !Array.isArray(rawLayers) || !Array.isArray(rawAssets)) {
      throw new Error("渲染快照尺寸或数组无效");
    }
    const layers: WireLayer[] = [];
    const layerIDs = new Set<string>();
    for (const rawLayer of rawLayers) {
      const layer = asRecord(rawLayer);
      const id = typeof layer?.id === "string" ? layer.id : "";
      if (!layer || !id) throw new Error("渲染快照包含无效图层");
      if (layerIDs.has(id)) throw new Error("渲染快照包含重复图层");
      layerIDs.add(id);
      layers.push({
        id,
        isVisible: layer.isVisible === true,
        isGroup: layer.isGroup === true,
        parentID: typeof layer.parentID === "string" ? layer.parentID : null,
        opacity: layer.opacity === undefined ? 1 : finiteNumber(layer.opacity) ?? Number.NaN,
        blendMode: typeof layer.blendMode === "string" ? layer.blendMode : "Normal",
        imageFile: typeof layer.imageFile === "string" ? layer.imageFile : null,
        maskFile: typeof layer.maskFile === "string" ? layer.maskFile : null,
        maskEnabled: layer.maskEnabled !== false,
        maskSourceID: typeof layer.maskSourceID === "string" ? layer.maskSourceID : null,
        maskLinked: layer.maskLinked !== false,
        adjustment: layer.adjustment,
        shape: layer.shape,
        effects: layer.effects,
        text: layer.text,
        transform: asTransform(layer.transform),
      });
    }
    const assets: WireAsset[] = rawAssets.map((rawAsset) => {
      const asset = asRecord(rawAsset);
      return {
        layerId: typeof asset?.layerId === "string" ? asset.layerId : "",
        kind: typeof asset?.kind === "string" ? asset.kind : "",
        url: typeof asset?.url === "string" ? asset.url : "",
        format: typeof asset?.format === "string" ? asset.format : "",
        width: finiteInt(asset?.width) ?? -1,
        height: finiteInt(asset?.height) ?? -1,
        hash: typeof asset?.hash === "string" ? asset.hash : "",
      };
    });
    return {
      rev,
      filterRev,
      filterPreview: record.filterPreview === true,
      doc: { width, height, layers },
      assets,
    };
  }

  private unsupportedReason(snapshot: WireSnapshot, request: RenderRequest): string | null {
    if (snapshot.filterPreview) return "滤镜预览由 CPU 轨渲染";
    if (snapshot.rev !== request.rev || snapshot.filterRev !== request.filterRev) return "渲染修订已过期";
    // Swift folders are pass-through: ancestor opacity and masks are applied
    // to each descendant before it blends with the backdrop. The current GPU
    // executor only composites independent raster layers, so gate the entire
    // hierarchy before validating assets or constructing a flat draw list.
    for (const layer of snapshot.doc.layers) {
      if (layer.isVisible && (layer.isGroup === true || layer.parentID)) {
        return `图层 ${layer.id} 使用了未验证的组路径`;
      }
    }
    const assets = new Map<string, WireAsset>();
    for (const asset of snapshot.assets) {
      if (!asset.layerId || asset.kind !== "image" || asset.format !== "rgba8-premultiplied-srgb" ||
          !asset.url.startsWith("/pixel/") || !asset.hash ||
          asset.width <= 0 || asset.height <= 0 || asset.width * asset.height > MAX_GPU_PIXELS ||
          !this.layerStore.canCache(asset.width * asset.height * 4)) {
        return "渲染快照包含无效像素资产";
      }
      const key = `${asset.layerId}:${asset.kind}`;
      if (assets.has(key)) return "渲染快照包含重复像素资产";
      assets.set(key, asset);
    }
    const graphDoc: RenderDoc = {
      width: snapshot.doc.width,
      height: snapshot.doc.height,
      layers: snapshot.doc.layers.map((layer): GraphLayer => ({
        id: layer.id,
        parentId: layer.parentID ?? undefined,
        isGroup: layer.isGroup === true,
        isVisible: layer.isVisible,
        opacity: layer.opacity ?? Number.NaN,
        blendMode: layer.blendMode ?? "Normal",
        imageFile: layer.imageFile ?? undefined,
        revision: request.rev,
        maskSourceId: layer.maskSourceID ?? undefined,
        maskFile: layer.maskFile ?? undefined,
        maskEnabled: layer.maskEnabled !== false,
        maskLinked: layer.maskLinked !== false,
        adjustment: asRecord(layer.adjustment) ? { kind: String(asRecord(layer.adjustment)?.kind ?? "") } : undefined,
      })),
    };
    try {
      buildRenderGraph(graphDoc);
    } catch (error) {
      return `渲染图结构无效: ${String(error)}`;
    }
    for (const layer of snapshot.doc.layers) {
      if (!layer.isVisible) continue;
      if (layer.adjustment != null) return `图层 ${layer.id} 使用了未验证的调整层`;
      if (layer.shape != null || layer.text != null || layer.effects != null) {
        return `图层 ${layer.id} 使用了未验证的图层类型或特效`;
      }
      if (layer.maskFile || layer.maskSourceID) return `图层 ${layer.id} 使用了未验证的蒙版`;
      if (layer.blendMode !== "Normal") return `图层 ${layer.id} 的混合模式 ${layer.blendMode} 暂走 CPU 轨`;
      const opacity = layer.opacity ?? Number.NaN;
      if (!Number.isFinite(opacity) || opacity < 0 || opacity > 1) {
        return `图层 ${layer.id} 的不透明度无效`;
      }
      if (layer.imageFile) {
        const asset = assets.get(`${layer.id}:image`);
        if (!asset) return `图层 ${layer.id} 缺少像素资产`;
        const transform = layer.transform;
        const origin = pair(transform?.origin);
        const size = pair(transform?.size);
        const rotation = finiteNumber(transform?.rotation) ?? Number.NaN;
        const flipX = transform?.flipX === true;
        const flipY = transform?.flipY === true;
        if (!origin || !size || !Number.isInteger(origin[0]) || !Number.isInteger(origin[1]) ||
            !Number.isInteger(size[0]) || !Number.isInteger(size[1]) || size[0] !== asset.width ||
            size[1] !== asset.height || rotation !== 0 || flipX || flipY) {
          return `图层 ${layer.id} 使用了未验证的变换`;
        }
      }
    }
    return null;
  }

  private prepareAssets(assets: WireAsset[]): void {
    const current = new Set<string>();
    for (const descriptor of assets) {
      if (descriptor.kind !== "image") continue;
      current.add(descriptor.layerId);
      const previous = this.assetRevisions.get(descriptor.layerId);
      const revision = previous?.descriptor.hash === descriptor.hash
        ? previous.revision
        : ++this.assetRevisionCounter;
      const entry = { revision, descriptor };
      this.assetRevisions.set(descriptor.layerId, entry);
      this.assetDescriptors.set(`${descriptor.layerId}:${revision}`, entry);
    }
    for (const layerId of [...this.assetRevisions.keys()]) {
      if (!current.has(layerId)) {
        this.assetRevisions.delete(layerId);
        this.layerStore.invalidate(layerId);
      }
    }
  }

  private async fetchLayerAsset(
    layerId: string,
    revision?: number,
    signal?: AbortSignal,
  ): Promise<{ bitmap: LoadedAsset; bytes: number }> {
    if (!this.fetcher || revision === undefined) throw new Error("像素资产请求不可用");
    const entry = this.assetDescriptors.get(`${layerId}:${revision}`);
    if (!entry) throw new Error("像素资产修订不存在");
    const response = await this.fetcher(joinURL(this.assetBaseUrl, entry.descriptor.url), {
      method: "GET",
      cache: "no-cache",
      signal,
    });
    const status = response.status ?? (response.ok === false ? 500 : 200);
    if (status !== 200) throw new Error(`像素资产 HTTP ${status}`);
    const width = finiteInt(header(response, "X-Pixel-Width") ? Number(header(response, "X-Pixel-Width")) : null);
    const height = finiteInt(header(response, "X-Pixel-Height") ? Number(header(response, "X-Pixel-Height")) : null);
    const format = header(response, "X-Pixel-Format");
    const hash = header(response, "X-Pixel-Revision");
    if (width !== entry.descriptor.width || height !== entry.descriptor.height ||
        format !== entry.descriptor.format || hash !== entry.descriptor.hash) {
      throw new Error("像素资产元数据与快照不匹配");
    }
    const bytes = new Uint8Array(await response.arrayBuffer());
    const expected = width * height * 4;
    if (bytes.byteLength !== expected) throw new Error("像素资产长度无效");
    const texture = this.device!.createTexture({
      size: { width, height, depthOrArrayLayers: 1 },
      format: "rgba8unorm",
      usage: textureUsage(),
    });
    try {
      const rowBytes = width * 4;
      const alignedRowBytes = Math.ceil(rowBytes / 256) * 256;
      const upload = new Uint8Array(alignedRowBytes * height);
      for (let y = 0; y < height; y++) {
        upload.set(bytes.subarray(y * rowBytes, (y + 1) * rowBytes), y * alignedRowBytes);
      }
      this.device!.queue.writeTexture(
        { texture },
        upload,
        { bytesPerRow: alignedRowBytes, rowsPerImage: height },
        { width, height, depthOrArrayLayers: 1 },
      );
    } catch (error) {
      texture.destroy?.();
      throw error;
    }
    return { bitmap: { texture, width, height }, bytes: expected };
  }

  private execute(snapshot: WireSnapshot, loaded: Map<string, LayerBitmap<LoadedAsset>>): void {
    const { width, height } = snapshot.doc;
    const imageLayers = snapshot.doc.layers.filter((layer) => layer.isVisible && layer.imageFile);
    this.ensureUniformCapacity(imageLayers.length);
    this.ensureRenderTargets(width, height);
    const targets = this.renderTargets!;
    const encoder = this.device!.createCommandEncoder();
    let current = 0;
    const clearPass = encoder.beginRenderPass({
      colorAttachments: [{
        view: targets.textures[current].createView(),
        loadOp: "clear",
        storeOp: "store",
        clearValue: { r: 0, g: 0, b: 0, a: 0 },
      }],
    });
    clearPass.end();

    for (const [index, layer] of imageLayers.entries()) {
      const asset = loaded.get(layer.id);
      if (!asset || !this.uniformBuffer) throw new Error(`图层 ${layer.id} 纹理不存在`);
      const transform = layer.transform!;
      const origin = pair(transform.origin)!;
      const size = pair(transform.size)!;
      const opacityByte = Math.floor(layer.opacity! * 255 + 0.5);
      const uniform = new Float32Array([
        origin[0], origin[1], size[0], size[1], width, height, opacityByte, 0,
      ]);
      const uniformOffset = index * UNIFORM_SLOT_BYTES;
      this.device!.queue.writeBuffer(this.uniformBuffer, uniformOffset, uniform.buffer);
      const output = 1 - current;
      const pass = encoder.beginRenderPass({
        colorAttachments: [{
          view: targets.textures[output].createView(),
          loadOp: "clear",
          storeOp: "store",
          clearValue: { r: 0, g: 0, b: 0, a: 0 },
        }],
      });
      pass.setPipeline(this.compositePipeline);
      pass.setBindGroup(0, this.device!.createBindGroup({
        layout: this.compositeLayout,
        entries: [
          { binding: 0, resource: targets.textures[current].createView() },
          { binding: 1, resource: asset.bitmap.texture.createView() },
          { binding: 2, resource: { buffer: this.uniformBuffer, offset: uniformOffset, size: uniform.byteLength } },
        ],
      }));
      pass.draw(6);
      pass.end();
      current = output;
    }

    const canvasTexture = this.context!.getCurrentTexture();
    const present = encoder.beginRenderPass({
      colorAttachments: [{
        view: canvasTexture.createView(),
        loadOp: "clear",
        storeOp: "store",
        clearValue: { r: 0, g: 0, b: 0, a: 0 },
      }],
    });
    present.setPipeline(this.presentPipeline);
    // Only proofMode.x is read (the shader picks its table on the CPU side), so
    // the rest is padding for the vec4u alignment.
    if (this.proofBuffer) this.device!.queue.writeBuffer(this.proofBuffer, 0, new Uint32Array([this.proofMode, 0, 0, 0]));
    present.setBindGroup(0, this.device!.createBindGroup({
      layout: this.presentLayout,
      entries: [
        { binding: 0, resource: this.presentSampler },
        { binding: 1, resource: targets.textures[current].createView() },
        { binding: 2, resource: this.proofTexture!.createView() },
        { binding: 3, resource: { buffer: this.proofBuffer } },
      ],
    }));
    present.draw(6);
    present.end();
    this.device!.queue.submit([encoder.finish()]);
  }

  private ensureUniformCapacity(layerCount: number): void {
    if (layerCount <= this.uniformCapacity) return;
    const buffer = this.device!.createBuffer({
      size: layerCount * UNIFORM_SLOT_BYTES,
      usage: GPU_BUFFER_USAGE_UNIFORM | GPU_BUFFER_USAGE_COPY_DST,
    });
    this.uniformBuffer?.destroy?.();
    this.uniformBuffer = buffer;
    this.uniformCapacity = layerCount;
  }

  private ensureRenderTargets(width: number, height: number): void {
    if (this.renderTargets?.width === width && this.renderTargets.height === height) return;
    this.destroyRenderTargets();
    const create = (): GpuTextureLike => this.device!.createTexture({
      size: { width, height, depthOrArrayLayers: 1 },
      format: "rgba8unorm",
      usage: GPU_TEXTURE_USAGE_TEXTURE_BINDING | GPU_TEXTURE_USAGE_RENDER_ATTACHMENT,
    });
    this.renderTargets = { width, height, textures: [create(), create()] };
  }

  private destroyRenderTargets(): void {
    this.renderTargets?.textures.forEach((texture) => texture.destroy?.());
    this.renderTargets = null;
  }

  private setFallback(reason: string, cancelActiveRender = true): void {
    this.statusValue = { track: "cpu-fallback", reason };
    if (cancelActiveRender) {
      this.renderGeneration += 1;
      this.activeAbort?.abort();
      this.activeAbort = null;
    }
    this.layerStore.invalidateAll();
    this.destroyRenderTargets();
  }

  private isCurrent(generation: number): boolean {
    return !this.destroyed && generation === this.renderGeneration;
  }
}

function asTransform(value: unknown): WireTransform {
  const record = asRecord(value);
  return {
    origin: record?.origin,
    size: record?.size,
    rotation: record?.rotation,
    flipX: record?.flipX,
    flipY: record?.flipY,
    sampling: record?.sampling,
  };
}
