# 28 — Camera Raw：光/色/曲线

**What to build:** Camera Raw 内核第一批（`adjust_camera_raw*` 族）：曝光/对比/高光/阴影/白/黑、白平衡/自然饱和度/饱和度、参数化+点曲线；≤2048px 预览与全尺寸提交同源。

**Blocked by:** 26 — 调整内核 A.

**Status:** ready-for-agent

- [ ] 光/色/曲线参数族与 C 黄金基准一致（CameraRawTests 语义分批）
- [ ] 参数族可独立组合（管线顺序与原版一致）
- [ ] 高光/阴影修正的浮点 ε 用例显式标注

对等矩阵：R01、R02、R03。
