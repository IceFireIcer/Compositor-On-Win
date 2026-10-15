# 70 — 决策门槛与 .comp v13 / HDR 图层导入

**What to build:** 在兼容性决策获批准后，用户可导入 EXR 和 Radiance HDR，保留高于白色的光值，保存并重开可与原版 v13 互通的项目。

**Blocked by:** 68 — 决策门槛与 .comp v12 / 16 位图层存取; **v13/扩展色域 ADR 与用户确认（未完成，硬门槛）**。

**Status:** in-progress（2026-10-15）——ADR-0007 已确认；EXR/Radiance 编解码、保存重开与导入已落地。画布高光预览与屏幕抖动属票 71/83。

- [ ] 决策明确 half-float extended sRGB 预乘、v13 `.exr` 资源、与 v11/v12 的兼容和拒绝边界。
- [ ] EXR/HDR 超白值在导入、图层、保存、重开后仍可恢复；完全不超白的 EXR 可按上游规则转成 16 位层。
- [ ] v1–v12 声明的项目拒绝 EXR 图层；路径安全、大小限额、蒙版约束延续。
- [ ] 与上游 `DeepLayerTests` 的 HDR 与版本门控用例互证。

对等矩阵：U17。原版锚点：`DeepLayers.swift`、`ProjectStore.swift`、`docs/project-format.md`；提交 `7f239dc`。本票现在仅登记，不授权改 schema。
