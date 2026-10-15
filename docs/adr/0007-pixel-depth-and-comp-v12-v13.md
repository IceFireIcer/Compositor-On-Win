# ADR-0007：像素位深扩展（16 位与 HDR）与 .comp v12/v13 版本策略

- 日期：2026-10-15　状态：**已接受**（2026-10-15 用户确认）　相关：[ADR-0002](0002-comp-format-compatibility.md)、[ADR-0003](0003-pixel-kernels-ported-to-go.md)、票 68/69/70/71/82–85
- 本 ADR 即 AGENTS.md 不变量 1 要求的「schema 变更前 ADR + 用户确认」；确认后票 68/69/70/71/82–85 解除 `blocked-by-decision`。

## 背景

上游 1.5 引入两种新像素格式，Windows 当前仍是 `.comp` v11 + 8 位预乘 sRGB（`domain.FormatVersion = 11`，`render.Bitmap` 全链路 8 位）。

本次核对 `reference/Swift`（提交 `e8c9e29701528413cc0e608d61187c1c61790add`）确认的事实：

1. `ProjectManifest.current = 13`，`supported = 1...13`（`ProjectStore.swift:20-23`）。
2. **上游每次保存都写 v13**：`EditorSession.projectSnapshot()` 重建一个 `ProjectManifest(...)`，`version` 取结构体默认值 `current`；加载到的版本不随文档保留（`installProject` 只取 documentID/width/height/resolution/guides，`EditorSession+Projects.swift:15-35`）。多处测试以 `#expect(loaded.manifest.version == ProjectManifest.current)` 钉住这一行为。
3. 因此 `reference/Swift/docs/project-format.md` 开头「version `12` for new saves」是**过期描述**，与代码不符（票 68 要求核实的正是这一点）。
4. **资产种类由文件自身表达，manifest schema 不变**：16 位层 = `<uuid>.png`（16 位 PNG）；HDR 层 = `<uuid>.exr`；蒙版恒为 8 位 PNG。
5. 读取门控（`ProjectStore.swift:183-192, 259`）：`.exr` 需 version ≥ 13；16 位 PNG 需 version ≥ 12 且非蒙版；`.exr` 文件名只在 version ≥ 13 合法。
6. HDR 判定：EXR 峰值 ≤ 1.001 时降级为 16 位层（`DeepLayers.swift:174`）。
7. 8 位显示副本是**派生**的：`DeepLayer.dithered` 用 `dither_quantize16` 重算，不落盘、不反写（`DeepLayers.swift:36-48`）。该内核的 Go 移植已在票 64 落地（`render.Quantize16`）。

## 决策（提议）

1. **像素不变量由单一格式扩展为三种**，全部 sRGB 族、预乘、左上原点：
   - 8 位 sRGB 预乘 RGBA —— 现状，默认；
   - 16 位 sRGB 预乘 RGBA —— native-endian `uint16`，行紧密排列（`width × 8` 字节）；
   - HDR —— half-float **extended sRGB** 预乘，白 = 1.0，保留大于白的光值。
   蒙版**恒为 8 位灰度**，任何位深下都不例外。
2. **manifest schema 零变更**：不新增字段。层种类由资产文件名与位深表达（`.png` 8/16 位、`.exr`）。
3. **8 位副本永远派生**：加载时以 `render.Quantize16` 重算；不写入 `.comp`、不回写 deep 像素。缩略图与屏幕显示（票 83）都从它来——这是「屏幕副本不反向覆盖原始像素」的实现方式。
4. **写出版本 = 内容所需的最低版本**：11（无深层）/ 12（含 16 位层）/ 13（含 EXR 层）；读取接受 1–13。
   - 这是对上游「恒写 13」的**有意偏差**：让不含新特性的 Windows 文档仍能被 macOS 1.4.5 打开，符合 ADR-0002 的互开产品要求。
   - 备选（上游忠实）：恒写 13。实现更简单、保存字节与上游一致，代价是任何 Windows 文档都不再被 macOS ≤1.4.10 打开。
5. **拒绝边界逐条对齐上游**：version ≥ 12 才允许 16 位 PNG；version ≥ 13 才允许 `.exr`；16 位蒙版拒绝；声明 v1–v11 的 manifest 含 16 位资产即拒绝（`missingImage`）。
6. **无损往返验收**：16 位 PNG 往返（预乘 ↔ 非预乘）允许 ≤1 LSB 舍入误差，以测试钉住；HDR EXR 往返高光保真 < 0.01（上游 `DeepLayerTests` 同款阈值）。

## 后果

- `internal/render.Bitmap` 需位深多态；WebGPU 纹理按层选 `rgba8unorm` / `rgba16unorm` / `rgba16float`（票 82–84）。
- 内存与文件体积 2–4 倍，并入票 52 的预算复核。
- 黄金基准新增 16 位/HDR 用例；`DeepPixels.{c,h}` 按 ADR-0003 拷贝进 `tests/golden/c/` 并注明来源提交。
- 绘画/调整精度（82）、屏幕抖动（83）、16 位导出（84）、HDR 编辑（71）、EXR 导出（85）分票落地。

## 确认记录（2026-10-15）

- 像素模型与门控（第 1–3、5、6 条）：**批准**——16 位与 HDR 全量落地。
- 写出版本（第 4 条）：**最低所需版本 11/12/13**。不含新特性的 Windows 文档保持 v11，可被 macOS 1.4.5 打开；这是对上游「恒写 v13」的有意偏差，读取仍接受 1–13。
- 本条同时订正上游文档：`project-format.md` 开头「version `12` for new saves」与 `ProjectManifest.current = 13` 不符，以上游代码为准。