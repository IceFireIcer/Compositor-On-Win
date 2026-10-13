# lib/gpu — WebGPU 合成器（票 11）

双轨渲染的 GPU 极（ADR-0004）：WGSL 管线是快路径，Go `internal/render` 是
导出真值与回退轨。

## 模块

| 文件 | 职责 | 测试 |
| --- | --- | --- |
| `textureCache.ts` | 纹理 LRU（字节预算 + `onMemoryPressure`：moderate 砍半 / critical 全清） | `textureCache.test.ts` |
| `layerBitmaps.ts` | 修订号失效取图：层变更后**仅重取该层**，并发合并，HTTP 语义（大负载不走 JSON 桥） | `layerBitmaps.test.ts` |
| `device.ts` | WebGPU 探测——每条失败路径都解析为带原因的 `cpu-fallback` 声明（不抛异常） | `device.test.ts` |
| `compositor.ts` | 生产执行器：快照/像素 HTTP 传输、修订取消、RGBA8 纹理缓存、双目标 Normal 合成与显式 CPU 回退 | `compositor.test.ts` |
| `shaders/blend.ts` | `BLEND_WGSL`：24 种混合模式逐式镜像 `internal/render/blend.go`（PDF 16 + Photoshop 8 + 非可分离 4，sRGB 语义、PDF 合成方程、预乘进出） | 一致性协议（下） |
| `renderGraph.ts` | 设备无关的绘制计划，镜像 composite.go 语义（顺序/组衰减/蒙版/剪贴/不可见/调整层透传） | `renderGraph.test.ts` |
| `shaders/adjust.ts` | 调整层 GPU 应用：1D LUT 与 3D cube 纹理化采样（消费票 10 的 LUT/cube 数据结构，含顶边钳位语义） | 一致性协议 |
| `shaders/effects.ts` | 特效通道：覆盖率→行/列分离模糊→环形合成（内/外沿）→着色，角度/距离语义同 ShadowEffect | 一致性协议 |
| `effectPass.ts` | 特效→GPU pass 链（6 种，E01–E06 顺序）；特效输出缓存按"修订+属性"键控、stale-while-revalidate 不闪白 | `effectPass.test.ts` |

## 一致性协议（GPU 输出 vs CPU 真值，逐模式）

1. 同一份 `RenderDoc` 同时喂 `buildRenderGraph`（GPU）与 Go `render.Render`
   （CPU 真值）；用例参数沿 `tests/golden` 的 cases 语义（每模式至少一例，
   复用其 ε 政策）。
2. Playwright（Chromium flag `--enable-unsafe-webgpu` + SwiftShader）加载
   fixture 页 → 设备探测 → 执行图 → `copyExternalImageToTexture`/readback
   取回像素 → 与 Go 侧 PNG 逐字节（或 case ε）比对，复用 `Compare` 协议。
3. 失败输出首差异字节偏移（与黄金基准同格式）。

## 性能基准（60fps）

`performance.now()` 环形采样 120 帧（组/蒙版/剪贴复杂 .comp），命令行入口
随票 52 性能票与真实设备矩阵落地；此处先定义采样协议：帧耗时 p50/p95 与
掉帧数，纹理缓存命中率同步记录。

## 生产执行器的支持边界

`WebGpuCompositor` 已经是可调用的生产边界，但为了不产生未经验证的错误像素，
GPU 快路径目前只接受：可见的普通栅格层、`Normal` 混合、无组/蒙版/剪贴/调整/特效/
形状/文字、整数轴对齐变换（旋转与翻转为零），且源资产必须是带尺寸与哈希校验的
预乘 RGBA8 sRGB。它用 `/render/{id}.json` 取权威快照，用 `/pixel/.../image.rgba`
取原始字节，两个 RGBA8 文档目标来回合成，再以 `alphaMode: "premultiplied"`
呈现到画布。

以下情况返回 `{ track: "cpu-fallback", reason }`，不会尝试用近似 WGSL 绘制：

- WebGPU、设备、画布上下文、`fetch` 或管线初始化失败；
- 快照/像素端点修订、尺寸、格式、哈希或字节数不匹配；
- 非 `Normal` 混合，以及组、蒙版、剪贴、调整层、特效、形状、文字或非整数轴对齐变换；
- 滤镜预览或设备丢失。

### Exported API

The production boundary exports these values and types from `compositor.ts`:

```ts
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

export interface WebGpuCompositorOptions {
  gpu?: GpuAdapterSource;
  fetch?: typeof fetch;
  assetBaseUrl?: string;
  cacheBytes?: number;
}

export class WebGpuCompositor {
  static create(canvas: CanvasLike, options?: WebGpuCompositorOptions): Promise<WebGpuCompositor>;
  readonly status: CompositorStatus;
  render(request: RenderRequest): Promise<RenderResult>;
  destroy(): void;
}
```

`GpuAdapterSource` is re-exported from `./device`; `CanvasLike` is the
structural canvas input (`width`, `height`, and `getContext`). A new `render`
call aborts the previous snapshot and pixel requests. The superseded call
resolves as `{ track: "cancelled" }`; an explicit `destroy()` does the same and
also releases cached textures and render targets. The caller must keep its CPU
PNG visible for `cpu-fallback` and must not replace it for `cancelled`.

### Canvas contract

```ts
const compositor = await WebGpuCompositor.create(canvas, {
  // Optional: defaults to navigator.gpu and the Wails asset origin.
  assetBaseUrl: "",
});

const result = await compositor.render({ docId, rev, filterRev });
// result.track === "gpu" | "cpu-fallback" | "cancelled"
```

Create one instance per canvas. Re-render whenever `rev` or `filterRev` changes;
keep the existing `/render/{docId}.png?v=${rev}-${filterRev}` image visible for a
CPU fallback; ignore `cancelled`; call `destroy()` on teardown. `status.track` reports
device availability (`webgpu` or `cpu-fallback`), while `render().track` reports the
result of the individual frame. `LayerBitmapStore` is registered with
`registerPressureHandler`, so the memory-pressure bridge evicts GPU textures.

- 调整层 GPU 内核随 M5（票 26/27/37）：LUT/cube 数据由后端按票 10 结构
  序列化上传，`renderGraph` 已为其保留 Normal 占位。
