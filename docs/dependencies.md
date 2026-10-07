# 依赖对照（Swift 框架 → Windows 方案 → 引入库）

> 2026-10-07 审计：枚举 `reference/Swift` 全部 `import`（20 种），逐项对照 Windows 移植的覆盖情况。
> 结论：**没有游离于功能对等矩阵之外的框架功能**——每个"未引用"的框架都对应已开票的票号；
> 本文件登记这些票将要引入的第三方库与许可。GPL 一律不用；LGPL 单独声明（见 [THIRD-PARTY-NOTICES.md](../THIRD-PARTY-NOTICES.md)）。

## 一、Swift 独有框架（无需第三方库，用平台等价物）

| Swift import | 用途（原版） | Windows 覆盖 | 票 |
| --- | --- | --- | --- |
| AppKit / SwiftUI / Observation / Combine / QuartzCore / ObjectiveC | UI 外壳、手势、光标、面板 | Wails v2 + Svelte 5 外壳 | 01–03、13 等 |
| CoreGraphics | 位图/路径/变换/CGContext 合成 | Go `internal/render` 手写内核 + 金色基准逐位对比 | 08–12、26–37 |
| CoreImage（除 CIRAWFilter 外） | 内核辅助：CIColorMatrix/CIBlendWithMask/CIMotionBlur/CIPerspectiveTransform/CIColorCube/CIEdgePreserveUpsampleFilter 等 | 全部在 Go 手写等价物（guided 上采样、透视、盒模糊等） | 08–12、26–37 |
| Metal | GPU 渲染管线、WGSL 前身 | WebGPU + WGSL | 11、12、22 |
| Vision | 主体/对象选择 | u2netp ONNX + GuidedMatte（ADR-0005） | 37 ☑ |
| Accelerate | vDSP/vImage 加速路径 | 不需要：Go 内核以逐位对齐优先 | — |
| CoreText | Dither 字形、PSDText 渲染 | Dither 用 basicfont（金测通过）；文本引擎见下表 | 36 ☑、39、44–46 |
| CryptoKit | ProjectDigest SHA256 | Go `crypto/sha256` | 05 |
| UniformTypeIdentifiers | 文件对话框类型 | Wails FileFilter | 01 |
| Foundation / Testing / XCTest | 基础/测试 | Go stdlib / go test / vitest | — |

## 二、未引用框架 → 票 → 引入库（本次审计的缺口清单）

| 框架缺口 | 票 | 引入库（license） | 引入方式 |
| --- | --- | --- | --- |
| ImageIO：TIFF 解码 | 40 | [golang.org/x/image](https://pkg.go.dev/golang.org/x/image)（BSD-3） | go.mod，纯 Go |
| ImageIO：EXIF 方向 | 40 | 无依赖：手写 APP1/IFD0 解析（只取 0x0112） | internal/rasterio |
| ImageIO：SVG 光栅化 | 40 | 无依赖：WebView2 原生 SVG → canvas 一次性栅格化（原版 decodeSVG 语义） | 前端 |
| CIRAWFilter：相机 RAW 解码 | 41 ✅ | [LibRaw](https://www.libraw.org) 0.22.2（LGPL-2.1 或 CDDL-1.0 双许可）+ 自写 C ABI/cgo 绑定（internal/rawio） | vcpkg manifest（x64-mingw-static）+ CI binary cache |
| ImageIO：HEIC 解码 | 41 ✅ | [libheif](https://github.com/strukturag/libheif) 1.23.5（LGPL-3.0，未启用 x265/GPL 编码 feature）+ 自写 cgo 绑定（internal/heicio） | vcpkg manifest（x64-mingw-static）+ CI binary cache |
| ImageIO：导出 PNG/JPEG | 42 | 无依赖：Go `image/png`、`image/jpeg` 标准库 | — |
| CoreText：文字引擎（排版/整形/栅格化） | 44 | [go-text/typesetting](https://github.com/go-text/typesetting)（MIT 主模块；子包 BSD-3/Unlicense；含 FreeType 许可条款的栅格化部分——均为宽松许可） | go.mod，纯 Go（票 44 实施时引入） |
| Sparkle：自动更新（appcast.xml） | 50 | [creativeprojects/go-selfupdate](https://github.com/creativeprojects/go-selfupdate)（MIT）+ GitHub Releases + EdDSA 签名（矩阵 P04） | go.mod，纯 Go（票 50 实施时引入） |

## 三、已放弃的候选

- [vegidio/raw-go](https://github.com/vegidio/raw-go)：静态内嵌 LibRaw 0.22.1，免 vcpkg，但版本旧、绑定面窄，且 LGPL 静态内嵌不利于后续替换——按票 41 的 vcpkg 方案走。
- [srwiley/oksvg](https://github.com/srwiley/oksvg) + [rasterx](https://github.com/srwiley/rasterx)（BSD-2）：Go 侧 SVG 光栅化备选；票 40 定了前端栅格化，暂不引入。
- dcraw 及一切 GPL 工具链：按用户约束禁用。
