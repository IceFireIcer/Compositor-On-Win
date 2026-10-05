# 30 — Camera Raw：光学/几何/校准

**What to build:** Camera Raw 内核第三批：镜头校正（`lens_distort` 径向桶形/枕形畸变 + 边缘色差 + 暗角）、几何（Upright 拉直/透视）、相机校准（三原色/色调偏移）、去边（defringe）。

**Blocked by:** 28 — Camera Raw：光/色/曲线.

**Status:** ready-for-agent

- [ ] 光学（畸变/色差/暗角）与 C 黄金基准一致（`lens_distort` 移植）
- [ ] 几何 Upright 与校准参数族一致
- [ ] 与前两批参数全组合的管线顺序一致（CameraRawGeometryCalibration 语义）

对等矩阵：R07、R08、R09（校准部分）。
