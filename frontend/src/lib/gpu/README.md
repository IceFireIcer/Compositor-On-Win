# lib/gpu — WebGPU 合成器（票 11）

双轨渲染的 GPU 极（ADR-0004）：WGSL 管线是快路径，Go `internal/render` 是
导出真值与回退轨。

## 模块

| 文件 | 职责 | 测试 |
| --- | --- | --- |
| `textureCache.ts` | 纹理 LRU（字节预算 + `onMemoryPressure`：moderate 砍半 / critical 全清） | `textureCache.test.ts` |
| `layerBitmaps.ts` | 修订号失效取图：层变更后**仅重取该层**，并发合并，HTTP 语义（大负载不走 JSON 桥） | `layerBitmaps.test.ts` |
| `device.ts` | WebGPU 探测——每条失败路径都解析为带原因的 `cpu-fallback` 声明（不抛异常） | `device.test.ts` |
| `shaders/blend.ts` | `BLEND_WGSL`：24 种混合模式逐式镜像 `internal/render/blend.go`（PDF 16 + Photoshop 8 + 非可分离 4，sRGB 语义、PDF 合成方程、预乘进出） | 一致性协议（下） |
| `renderGraph.ts` | 设备无关的绘制计划，镜像 composite.go 语义（顺序/组衰减/蒙版/剪贴/不可见/调整层透传） | `renderGraph.test.ts` |

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

## 集成点

- 画布接入随票 13（视口）：`CanvasSurface` 持有 `WebGpuCompositor`（探测
  成功）或声明回退（状态栏显示 `device.reason`，CPU 轨经桥接取
  `render.Render` 输出——HTTP 语义同 `layerBitmaps`）。
- 调整层 GPU 内核随 M5（票 26/27/37）：LUT/cube 数据由后端按票 10 结构
  序列化上传，`renderGraph` 已为其保留 Normal 占位。
