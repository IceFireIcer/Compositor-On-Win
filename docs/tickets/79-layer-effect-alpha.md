# 79 — 半透明像素的图层特效着色

**What to build:** Color Overlay、Inner Glow 和 Inner Shadow 对半透明图层像素完整着色，同时保持 alpha 和阴影层次。

**Blocked by:** 20 — 图层操作命令族与图层面板; 32 — 滤镜菜单核心。

**Status:** done（2026-10-16）

- [x] 半透明像素的 RGB 重新着色与原 alpha 保持一致；透明像素不凭空产生覆盖。
- [x] Inner Shadow 与 Inner Glow 的透明边缘、强度和叠加顺序与原版测试语义一致。
- [x] 特效开关、保存重开、撤销和导出不丢失参数（既有用例覆盖）。

## 修的是什么

上游 `e7e295e`（Color Overlay）与 `9cb57db`（Inner Glow / Inner Shadow）让这三者**重新着色
图层自身的像素并保持 alpha**。真值是 Metal 内核 `effects_compose`（`MetalLayerEffects.swift:339-390`），
注释里就写着旧行为错在哪：

> Laid over the composite instead, it only covered as much as the pixel did and let the old color
> show through. … took only part of a translucent pixel, made it more opaque, and tinted the effects
> beneath.

我们原来的实现正是这个「覆盖在合成上」的旧形状：

| | 修复前 | 修复后（= 上游） |
| --- | --- | --- |
| 颜色叠加的混合权重 | `alpha/255 × opacity` | `opacity` |
| 颜色叠加的目标色 | 非预乘的叠加色 | `叠加色 × alpha`（预乘） |
| 颜色叠加后的 alpha | `coverage×255 + alpha×(1−coverage)`，**被抬高** | **不变** |
| 内特效的混合量 | `opacity × coverage/255 × dst[3]` | `opacity × coverage/255` |
| 内特效的目标色 | 非预乘的特效色 | `特效色 × dst[3]`（预乘） |

**不透明像素两条路结果完全相同**（`alpha = 1` 时各项重合），所以这个 bug 只在半透明像素上
可见 —— 这也解释了为什么既有的不透明用例一直是绿的。

## 验证

三条既有用例的期望值是**修复前的**，已逐条手算核对后更新（例如夹具 `[0,0,128,128]` + 红色
叠加 0.5：上游给出 `[64,0,64,128]`，旧期望 `[64,0,96,160]` 的 alpha 被抬到了 160）。

新增 `effects_translucent_test.go`：

| 用例 | 覆盖 |
| --- | --- |
| `TestColorOverlayKeepsTranslucentAlphaAndRecolorsFully` | 六个 alpha 值下半透明黑 + 白色叠加 = 半透明白，且 alpha 逐字节不变 |
| `TestColorOverlayInventsNoCoverageOnClearPixels` | 全透明像素不被凭空染色 |
| `TestColorOverlayOpacityIsTheMixWeight` | 不透明度就是混合权重（0 时不动，1 时完全换成预乘色） |
| `TestInnerEffectsKeepTranslucentAlpha` | 内发光/内阴影保持 alpha，**并断言确实发生了重新着色** |

最后一条的加强是刻意的：只查 alpha 的话，特效根本没跑也能通过。

对等矩阵：U26。原版锚点：`LayerEffects.swift`、`ColorOverlayTests.swift`；提交 `e7e295e`、`9cb57db`。
