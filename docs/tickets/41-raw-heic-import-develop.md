# 41 — RAW/HEIC 导入与 develop

**What to build:** 相机 RAW 导入（LibRaw cgo/vcpkg）：解码 + develop 步骤（RawDevelopSheet：先 develop 再进编辑器）+ asShot 默认读取；HEIC 解码（libheif cgo）。cgo 工具链以 vcpkg 固定并进 CI。

**Blocked by:** 40 — 栅格导入（JPEG/PNG/TIFF/SVG）.

**Status:** done（2026-10-07）

实现注记：vcpkg manifest（仓库根 vcpkg.json，baseline pin 2976266a；libheif default-features=false **明确排除 GPL 的 x265**）→ vcpkg_installed/x64-mingw-static 静态 .a 链入 exe（LGPL 静态链接声明见 THIRD-PARTY-NOTICES）。internal/rawio：rawbridge.cpp C ABI（会话缓存已解包帧，develop 不重解码——原版 Queue actor 语义）+ develop.go（曝光×2^档、Planckian 轨迹→XYZ→sRGB 白平衡增益、boost=flat↔S 曲线混合、McCamy 估 asShot Kelvin——往返测试闭合）。internal/heicio：libheif C API 解码 RGBA→预乘。bridge：RAW 扩展名分发表 → 显影表单（RawBegin/RawDevelopPreview/RawFinish/RawCancel，半尺寸预览+全尺寸导入，复用 CommitImportedLayers），HEIC 直入导入批次。前端 RawDevelopSheet.svelte（曝光/色温/色调/增强 + 250ms 防抖预览 + Reset=asShot + RAW 队列逐个）。链接坑实录：zlib 静态名 libzs.a（-lzs）、libheif 需哑 .cpp 强制 g++ 链接驱动（libc++）、libraw 需 -lws2_32（htons）。仓库无 RAW/HEIC 样本（体积+版权），绑定测试钉错误契约，真文件验证在票内完成。

- [x] RAW 解码 + develop 表单 + asShot 默认（RawImporter 语义；asShot 由相机增益经 Planckian/McCamy 估计，develop 数值与 Apple 引擎不逐位一致——转换说明已注明）
- [x] HEIC 打开（含透明度；libheif RGBA→预乘，imports 走批次）
- [x] cgo 依赖在 CI（windows-latest）可复现构建（vcpkg.json manifest + x64-mingw-static + binary cache + choco mingw）
- [x] 不支持变体的错误可操作（LibRaw/libheif 错误文本直通弹窗；非 RAW/HEIC 拒绝路径有测试）

对等矩阵：I02、I04、故事 46（RAW/HEIC 项）。
