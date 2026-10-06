# 19 — 吸管/色板/颜色选择器/数值控件

**What to build:** 吸管工具（取样环覆盖层、可开关）；前景/背景双色板（含交换）；完整 HSV 颜色选择器表单；基础数值控件：拖标签 scrub、点击定位、方向键步进（原版 NumericScrub/ArrowStepper 语义），供所有工具头与表单复用。

**Blocked by:** 13 — 视口与画布导航.

**Status:** done

- [x] 吸管取样与像素一致；取样环开/关
- [x] HSV ↔ RGB 数值往返测试
- [x] 数值控件三种输入收敛到同一值（拖拽/点击/步进）

对等矩阵：T13、V06、故事 25/54。

**实现说明（2026-10-06，并行子代理）：** `lib/state/color.ts`——HSV↔RGB 双向（灰轴保 hue/纯黑保 s，PickerHSB 语义）+ 往返测试、双色板 swap/reset（X/D）、hex、samplePixel 预乘转直色；`components/numeric.ts`——NumericScrub（每像素 sensitivity、钳制）+ ArrowStepper（Shift=10×），三种输入收敛同一值测试。43 测试。取样环/scrub UI 后接。
