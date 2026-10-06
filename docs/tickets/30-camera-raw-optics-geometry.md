# 30 — Camera Raw：光学/几何/校准

**What to build:** Camera Raw 内核第三批：镜头校正（`lens_distort` 径向桶形/枕形畸变 + 边缘色差 + 暗角）、几何（Upright 拉直/透视）、相机校准（三原色/色调偏移）、去边（defringe）。

**Blocked by:** 28 — Camera Raw：光/色/曲线.

**Status:** done

- [x] 光学（畸变/色差/暗角）与 C 黄金基准一致（`lens_distort` 移植）
- [x] 几何 Upright 与校准参数族一致
- [x] 与前两批参数全组合的管线顺序一致（CameraRawGeometryCalibration 语义）

对等矩阵：R07、R08、R09（校准部分）。

**实现记录**（cameraraw.go，随票 30 提交）：
- `LensDistort` 逐行移植（径向 k、预乘域双线性、视野外透明）；既有 lens case
  （k=0.3，ε=1）首次接入比对。
- `ApplyCameraRawOptics`：lens_distort 畸变→色差移除（径向二次移位，红内蓝外
  采样）→紫/绿去边（色相区间去饱和）→镜头暗角校正（轮廓强度叠加 35%）；
  golden case ε=1（hypot 跨库）。
- `ApplyCameraRawCalibration`：阴影染色（l<0.35 时 h+=tint·0.06）+三主色
  hue/sat 偏移×进程版本缩放（v1 0.55→v6 1.0）；case ε=0 逐位。
- 几何（CoreImage 侧无 C 内核）：guidedCorrections（首线拉直、±45 折返、次线
  ±25 透视）+ outputCorners（Swift 底向上角点翻转向下）+ 单应求解/求逆 +
  双线性透视重采样（像素角空间→索引空间，半像素对齐）+ constrainCrop
  （alpha 边界裁切居中回填）；角点公式与单应往返由单元测试钉死。
- `ApplyCameraRawFilter` 管线组装（Swift apply() 顺序：几何→校准→光色→
  曲线/混色/分级→特效→颗粒→细节/光学），恒等短路返回原位图；≤2048px 预览
  与全尺寸提交同函数不同 Scale。
