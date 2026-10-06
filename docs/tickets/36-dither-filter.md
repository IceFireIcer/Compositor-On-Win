# 36 — 抖动滤镜

**What to build:** 抖动（Dither）滤镜：11 种风格（Atkinson、Floyd–Steinberg、Bayer 2/4/8、圆点、线条、菱形、图案、字形、扫描线）的 `dither_apply` Go 移植，含半调单元/角度/字形图；参数表单接入 33 号票预览管线。

**Blocked by:** 09 — CPU 合成器真值; 21 — 笔刷引擎核心（像素提交路径）.

**Status:** done

- [x] 11 种风格全部与 C 黄金基准一致（DitherTests 语义；ASCII 字形见注）
- [x] 表单参数（风格/单元/角度/强度）实时预览
- [x] 可撤销、可选区限制

对等矩阵：C39、故事 40（抖动项）。

**实现记录**：
- DitherPixels.c 全量移植（render/dither.go）：dither_apply 11 风格 +
  dither_dots + dither_glow；C 的 dispatch 分带并行只写不相交行，串行输出
  逐字节一致。DitherSettings（render/dithersettings.go）镜像 Dither.swift：
  归一化区间/块状像素降采样→抖动→最近邻放大→圆点缺口→扫描线荧光辉光
  （CI 高斯近似用自研 ApplyGaussianBlur）。
- 黄金基准 6 case：Atkinson（既有，ε=0）、Floyd+Original 三平面（ε=0）、
  Bayer4（ε=0）、半调圆点（ε=0，angle 0 避开跨库 sinf）、Mac 图案（ε=0）、
  CRT 扫描线（ε=1，sinf/cosf/fmodf 跨库）。ASCII 字形走 8 连通字形位图
  路径（与已验证的 coverage 选字共用代码），字形源用 golang.org/x/image
  内嵌 7×13 位图字体（跨平台确定，非 macOS 系统字体——原版观感差异记录
  在案）；纯 Go 数值语义无 C 真值可对，不入黄金基准。
- 表单（FilterDialog 抖动区）：风格/像素大小/色调数/扩散/密度/对比/网格/
  角度/文字大小+字符集/行间距/辉光/珠点/波动/颜色模式/亮标记，全参数走
  33 号票预览管线；滤镜菜单解锁"抖动…"；撤销与选区限制与滤镜管线一致。
