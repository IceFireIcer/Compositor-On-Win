# 18 — 裁剪

**What to build:** 裁剪工具：拖动裁剪框（含吸附）、比例预设（3:4、9:16 等）、⌥ 对称裁剪、有选区时从选区开始、提交/取消；裁剪后文档尺寸更新且可撤销。

**Blocked by:** 13 — 视口与画布导航.

**Status:** done

- [x] 裁剪计算与比例/对称语义测试通过（CropTests 语义）
- [x] 选区起点裁剪与吸附正确
- [x] 提交后文档尺寸（含 manifest）正确、撤销可恢复

对等矩阵：T05、故事 36。

**实现说明（2026-10-06，并行子代理）：** `lib/state/crop.ts`——裁剪框会话（Crop.swift 几何逐行移植）、比例预设、⌥ 对称、选区起点、snap 吸附（CropTests 移动用例数值一致）、commitCrop 纯数据可入 history、offsetGuides 平移并删出界（project-format v8）。43 测试。文档化偏差（框钳制进画布、出界参考线删除）已注释。
