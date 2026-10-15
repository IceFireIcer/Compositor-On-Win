# ADR-0008：色彩管理与 CMYK 软打样（ICC）

- 日期：2026-10-15　状态：**已接受**（2026-10-15 用户确认）　相关：[ADR-0002](0002-comp-format-compatibility.md)、[ADR-0006](0006-third-party-dependencies-and-licenses.md)、票 72/86/87
- 本 ADR 即色彩空间不变量变更前所需的「ADR + 用户确认」；确认后票 72/86/87 解除 `blocked-by-decision`。

## 背景

上游 `CMYK.swift` 把 CMYK 定义为**面向印刷的视图与导出变换，文档始终是 RGB**：「Ink values are never edited directly; what printing changes is mostly how saturated a color can be, which the proof shows on the RGB screen.」

实现方式（`CMYK.swift:114-184`）：sRGB →（相对色度意图、白底压平）CMYK → 回 sRGB，在 33³ 格点上采样，产出三张 3D LUT：`proof`（校样）、`warning`（校样 + 色域警告）、`marked`（仅警告）。色域判定为 `max|往返 − 原值| > 10`（8 位单位），越界像素标灰 0.5。配置来源是 ColorSync 目录与 app 自存的 `CMYK Profiles`。

Windows 无 ColorSync，也没有等价的一体化 ICC 引擎——这是本 ADR 要定的核心。

## 决策（提议）

1. **文档恒为 RGB**：CMYK 只是视图变换与导出变换，**永不**进入图层像素，也**永不**写入 manifest 的 `colorSpace`（保持 `sRGB`）。校样属视图状态（前端，不变量 5），关闭即还原，不影响保存与 RGB 导出。
2. **色彩引擎 = Little CMS 2（lcms2，MIT）**：cgo + vcpkg，沿用 ADR-0006 对 LibRaw/libheif 的既有方式；登记进 `docs/dependencies.md` 与 `THIRD-PARTY-NOTICES.md`（MIT 属白名单，无需 LGPL 单独声明）。
   - 备选：Windows `mscms`/WCS（零新依赖，但 CMYK 配置处理与跨平台结果一致性都更弱）；备选：不做 CMYK（放弃 U19、U32–U34）。
3. **校样实现与上游同构**，以便同一配置下结果可比：33³ sRGB 格点 → 白底、相对色度意图过配置 → 回 sRGB → 三张 LUT；色域阈值与标灰值照抄上游。LUT 作为 WebGPU 3D 纹理在画布着色器里采样，**不参与** CPU 合成真值。
4. **配置来源**：系统 ICM 目录（`%SystemRoot%\System32\spool\drivers\color`）+ 应用数据目录 `CMYK Profiles`（Load Profile 复制进来）。macOS 的 Generic CMYK Profile 在 Windows 上随 SKU 而异，**不可依赖**，因此随包内置一个宽松许可的通用 CMYK 配置作为兜底。
5. **容差按语义对等验收**：lcms2 与 ColorSync 不逐位一致，写入 known-differences；`CMYKTests` 的移植断言结构性质（LUT 维度、白点不动、色域边界单调性）而非字节相等。

## 后果

- 新增 cgo 依赖与 CI 的 vcpkg 构建（并入票 41 已建立的二进制缓存）。
- 校样是画布着色器的一次 LUT 采样；色域警告只改显示。
- CMYK 导入（86）与导出（87）复用同一引擎与同一配置登记。

## 确认记录（2026-10-15）

- 「文档恒 RGB + 校样为纯视图变换」：**批准**。
- 引擎选型：**lcms2（MIT）**，cgo + vcpkg，按 ADR-0006 登记进 `docs/dependencies.md` 与 `THIRD-PARTY-NOTICES.md`。