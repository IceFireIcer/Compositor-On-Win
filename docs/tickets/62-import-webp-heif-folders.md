# 62 — WebP 与相机 HEIF 导入

**What to build:** 用户可从文件对话框或拖放导入 WebP 及相机 `.hif`/`.heif` 文件。

**Blocked by:** 40 — 栅格导入; 41 — RAW/HEIC 导入与 develop; 48 — 剪贴板与拖放。

**Status:** done（2026-10-14）

实现注记：rasterio 注册 `golang.org/x/image/webp`（无需新依赖）并把 `webp` 加进扩展名表；WebP 的 EXIF 方向从 RIFF 的 `EXIF` 块读（裸 TIFF 与 `Exif\0\0` 前缀两种写法都认），alpha 走既有 `BitmapFromImage` 预乘约定。heicio 把 libheif 的 `(code, subcode)` 映射抽成纯函数 `classifyHEIFError`：缺解码器/不支持的文件类型（AV1、JPEG 2000、VVC…）报 `ErrUnsupportedCodec`，与「文件已损坏」的 `ErrUnreadable` 分开，并附上 libheif 原始说明；`.hif`/`.heif`/`.heic` 仍走同一条 libheif 路径。bridge 导入对话框补 `*.webp`（`*.heic;*.heif;*.hif` 已在）。前端 dropimport 增加相机 HEIF 的 MIME/扩展名名单：WebView2 解不了 HEIF，但没有磁盘路径时也必须进失败清单（此前被静默过滤掉）。测试夹具沿用原版 `ImageImportTests` 的两个 64×32 base64 WebP（x/image/webp 只解码、不带编码器）。

- [x] 文件对话框、拖放、导入分发器一致支持 WebP 与 `.hif`/`.heif`，透明度、方向和错误报告正确。
- [x] 不支持的编解码格式给出明确错误，不悄悄把 HEIF 当普通 HEIC 处理。
- [x] 导入、保存重开与导出对照 `ImageImportTests` 的 WebP 用例（WebP 往返有测试）。**相机 HEIF 只有路由测试**：上游 `ImageImportTests` 也没有 `.hif`/相机夹具，Windows 侧只把一个合成的 `相机.hif` 送进 libheif 并断言失败路径，没有真实 HEIF 解码用例。

对等矩阵：U09。原版锚点：`ImageImporter.swift`、`ImageImportTests.swift`；提交 `5724099`、`70b3e14`。文件夹批量导入见 81。
