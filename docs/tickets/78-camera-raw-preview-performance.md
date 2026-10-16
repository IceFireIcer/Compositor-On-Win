# 78 — Camera Raw 查表与预览性能

**What to build:** Camera Raw 预览在相同设置下快速更新，最终提交仍使用完整精度和同一套查表数据。

**Blocked by:** 28 — Camera Raw：光/色/曲线; 34 — Camera Raw 面板与示波器; 63 — Camera Raw 光影与细节数值对齐。

**Status:** in-progress（2026-10-16；查表资源与加载器已落地，阶段管线待接）

- [ ] 查表资源随应用加载且校验大小/版本；预览取消旧任务，不让旧结果覆盖新设置。
      **资源与校验已完成**（见下）；预览取消的专项用例待补（前端已有 generation 守卫，
      但 Camera Raw 这条没有单独钉住）。
- [ ] 常见照片预览达到原版 5–20 倍优化的可解释基准，完整提交不走低精度捷径。
      **待接表之后才有意义**：现在没有可基准化的查表阶段。
- [ ] 同一输入和设置的预览、提交、撤销结果一致；透明像素统计不污染降噪。**待做**。

## 已落地（2026-10-16）

**资源**：`reference/Swift/Compositor/Resources/CameraRawTables.bin`（pin `e8c9e29`）逐字节拷到
`internal/render/assets/CameraRawTables.bin`，来源、格式与「为什么必须随包带」写在该目录的
README 里。`go:embed` 打进二进制 —— 产品代码不该去读只读基准区。

**加载器**：`internal/render/cameratables.go`。逐项对应上游 `CameraRawTables.load()`：

| 上游 | 这里 |
| --- | --- |
| `prefix(4) == "CRT3"` | 标识校验 |
| `field(4) == size`、`field(6) == count`、`step > 0` | 几何校验（对着编译期常量，资源不是这一份就报错） |
| `decompressed(using: .lzma)` | `xz.NewReader` —— **Apple 的 `.lzma` 实际解的是 xz 容器**，载荷以 `FD 37 7A 58 5A 00` 开头 |
| `planes.count == count * entries` | `io.ReadFull` 长度校验 |
| 运行差值累加 + 基准级 + `step` | 同一条公式，含 uint8 回绕与 int8 转换 |

**启动校验**：`main()` 在开窗口之前调 `render.ValidateCameraRawTables()`，资源缺失或几何不符
会写进启动日志，而不是等用户第一次拖滑杆才静默失效。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `TestCameraRawTablesResourceHasTheDeclaredGeometry` | 随包资源能解、几何与条目数正确、基准级端点 0/255 与第二级 16 |
| `TestCameraRawIdentityTablesAreIdentity` | **硬证据**：滑杆取 0 的五张表恰好等于恒等（偏离 0 级） |
| `TestCameraRawTableHeaderValidation` | 过短/标识不符/几何不符/步长为零/载荷损坏都报错 |
| `TestCameraRawTableAtBounds` | `tableAt` 越界返回 nil |

## 还剩什么

1. 把 `tableAt` 接进各滑杆的阶段管线（曝光 21 张、白平衡 9×9 混合四张、对比/高光/阴影按
   亮度自适应、混色器/校准/参数曲线/颜色分级）。这是票 63 与票 90 缺的那一步。
2. Camera Raw 预览取消旧任务的专项用例（验收 1 后半）。
3. 5–20× 的可解释基准（验收 2）。
4. 预览/提交/撤销一致性与透明像素统计（验收 3）。

对等矩阵：U25。原版锚点：`CameraRawTables.swift`、`CameraRawTables.bin`、`CameraRawTests.swift`；提交 `7b5a592`、`a8630e4`。
