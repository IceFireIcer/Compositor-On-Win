# 第三方许可声明（THIRD-PARTY NOTICES）

本仓库引入的第三方库及许可。**本仓库不使用 GPL 许可的依赖**；
LGPL 依赖在文末单独一节声明（仅以动态库形式使用，源码不入库）。

## MIT

| 库 | 引入票 | 用途 |
| --- | --- | --- |
| [creativeprojects/go-selfupdate](https://github.com/creativeprojects/go-selfupdate) | 50（计划） | 自动更新（GitHub Releases + EdDSA） |
| [go-text/typesetting](https://github.com/go-text/typesetting) 主模块 | 44（计划） | 文字排版/整形 |

## BSD-3-Clause

| 库 | 引入票 | 用途 |
| --- | --- | --- |
| [golang.org/x/image](https://pkg.go.dev/golang.org/x/image) | 40 | TIFF 解码；文本/字体设施 |
| go-text/typesetting 子包（opentype/api 等，BSD-3 或 Unlicense 双许可） | 44（计划） | 字体解析 |

## BSD-2-Clause / Unlicense / FreeType License（宽松许可）

- go-text/typesetting 部分栅格化代码源自 golang/freetype，按 FreeType License（FTL，
  BSD 型宽松许可）与 GPL-2.0 双许可发布，本仓库按 FTL 侧使用；相关版权与致谢条款
  随发行版 NOTICE 一并分发。

> 上述条目在实际引入（对应票提交）时随 go.mod 生效；未引入前仅为登记。

---

## LGPL（单独声明）

以下依赖以 **动态链接库** 形式使用（vcpkg 构建产物，随安装包分发），其源码不进入本仓库；
依 LGPL 义务，用户提供这些库的目标文件与完整源码链接，允许用户替换并重新链接。

| 库 | 许可 | 引入票 | 用途 |
| --- | --- | --- | --- |
| [LibRaw](https://www.libraw.org) 0.22.2（vcpkg，x64-mingw-static） | LGPL-2.1 **或** CDDL-1.0（双许可，本仓库按 LGPL-2.1 侧使用） | 41 ✅ | 相机 RAW 解码（CIRAWFilter 替代） |
| [libheif](https://github.com/strukturag/libheif) 1.23.5（含 libde265，vcpkg，x64-mingw-static；未启用任何编码器 feature——x265 为 GPL，已明确排除） | LGPL-3.0 | 41 ✅ | HEIC 解码 |

LGPL 静态链接说明：上述库以 x64-mingw-static 三角编译为 .a 并链入可执行文件。
依 LGPL-2.1 §6(a)/LGPL-3.0 §4(d)，用户可凭本仓库源码与构建说明（vcpkg.json +
CI 工作流）重新链接修改后的库版本；对应的目标文件随发行版或按需提供。
