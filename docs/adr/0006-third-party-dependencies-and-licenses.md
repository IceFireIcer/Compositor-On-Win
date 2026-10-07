# ADR-0006：第三方依赖与许可策略

日期：2026-10-07　状态：已接受　相关：[dependencies.md](../dependencies.md)

## 背景

原版只引一个第三方包（Sparkle，MIT），其余全是 Apple 框架。Windows 重写缺的框架能力
（TIFF/RAW/HEIC 解码、文字排版、自动更新）需要引入第三方库补齐。用户约束：**不用 GPL；
LGPL 依赖必须单独声明**。

## 决策

1. 审计先行：逐个 `import` 对照覆盖票（`docs/dependencies.md`），不引入矩阵之外的"顺手库"。
2. 许可白名单：MIT / BSD / Apache-2.0 / Unlicense / FreeType(FTL) 可直接进 go.mod；
   **LGPL 依赖仅限二进制级使用**（vcpkg 动态库 + 自写薄 cgo 绑定，库源码不进本仓库），
   并在 `THIRD-PARTY-NOTICES.md` 单独一节声明版本与来源。GPL 一律不用。
3. 具体选型（引入时机 = 对应票开工时）：
   - 票 40：`golang.org/x/image`（BSD-3，TIFF）；EXIF 方向与 SVG 手写/前端栅格化，不加依赖。
   - 票 41：LibRaw（LGPL-2.1/CDDL）与 libheif（LGPL-3.0）经 vcpkg 以固定 triplet 构建，
     自写 C API 薄绑定；RAW develop 的数值语义与 Apple CIRAWFilter 不逐位对齐（不同解码引擎），
     在转换说明中告知用户。
   - 票 44：`go-text/typesetting`（MIT/BSD/Unlicense/FTL，全部宽松）。
   - 票 50：`creativeprojects/go-selfupdate`（MIT）。
4. 每次引入新依赖时更新 `docs/dependencies.md` 与 `THIRD-PARTY-NOTICES.md`；破坏性 schema 变更仍走 ADR-0002 流程。

## 后果

- CI 需要缓存 vcpkg 安装（票 41）；本地 llvm-mingw 与 vcpkg triplet 以 `x64-mingw-dynamic` 对齐。
- 文字引擎的排版结果与 Apple CoreText 不逐位一致（整形引擎不同），按"语义对等"验收（票 44）。
