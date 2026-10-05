# 29 — Camera Raw：混色器/分级/细节

**What to build:** Camera Raw 内核第二批：HSL 混色器 8 色相、颜色分级（阴影/中间调/高光+混合/平衡）、细节（锐化+亮度盒模糊降噪）、去雾。

**Blocked by:** 28 — Camera Raw：光/色/曲线.

**Status:** ready-for-agent

- [ ] 混色器/分级/细节与 C 黄金基准一致
- [ ] 与第一批参数组合的管线顺序一致
- [ ] 降噪盒模糊与锐化蒙版语义（CameraRawDetailOptics 对应移植）验证

对等矩阵：R04、R05、R06。
