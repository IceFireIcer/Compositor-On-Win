# 41 — RAW/HEIC 导入与 develop

**What to build:** 相机 RAW 导入（LibRaw cgo/vcpkg）：解码 + develop 步骤（RawDevelopSheet：先 develop 再进编辑器）+ asShot 默认读取；HEIC 解码（libheif cgo）。cgo 工具链以 vcpkg 固定并进 CI。

**Blocked by:** 40 — 栅格导入（JPEG/PNG/TIFF/SVG）.

**Status:** ready-for-agent

- [ ] RAW 解码 + develop 表单 + asShot 默认（RawImporter 语义）
- [ ] HEIC 打开（含透明度）
- [ ] cgo 依赖在 CI（windows-latest）可复现构建
- [ ] 不支持变体的错误可操作

对等矩阵：I02、I04、故事 46（RAW/HEIC 项）。
