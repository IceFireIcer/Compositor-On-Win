# 38 — PSD/PSB 解析器

**What to build:** 手写 PSD/PSB 解析器移植：文件头/图层记录/通道（RLE + Zip）、合成与图层像素、混合模式映射、`levl`/`curv`/`hue2` 调整数据→活动调整图层、超大层裁剪到画布；8bit RGB 限定（CMYK 拒绝并提示）。

**Blocked by:** 04 — 文档域模型与限额; 05 — .comp 存取校验与互开.

**Status:** ready-for-agent

- [ ] 原版 PSD 内存夹具族（PSDFixture 程序化字节流）语义移植全部通过
- [ ] PSB（大文件版）打开正确（PSBImportTests 语义）
- [ ] PSD 调整数据转为活动调整层（PSDAdjustmentTests 语义）
- [ ] 非法/不支持变体给出可操作错误而非崩溃

对等矩阵：I05（解析侧）。
