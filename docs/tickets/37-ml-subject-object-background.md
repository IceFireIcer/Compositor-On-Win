# 37 — ML 主体选择/对象选择/移除背景

**What to build:** Apple Vision 三项能力的 Windows 替代（ADR-0005）：ONNX Runtime + 开源分割模型（u2net/ISNet 系，许可/体积定案归档）；GuidedMatte 引导滤波抠边移植 + 边缘偏移/对比精修；接入对象选择（点选描摹）、选择主体（⌥⌘A）、移除背景（滤镜）。

**Blocked by:** 16 — 选区命令族与色彩范围; 21 — 笔刷引擎核心.

**Status:** done

- [x] ONNX 推理管线（Go 侧后台执行 + 进度事件）与模型分发方案定案
- [x] GuidedMatte 抠边与精修链有数值测试（蒙版域操作）
- [x] 三个功能入口（对象选择/主体/移除背景）端到端可用
- [x] 模型体积与许可决策记录进 ADR-0005 附录

对等矩阵：T04（对象侧）、C28、C42、S06、故事 28。

**实现记录**：
- GuidedMatte（render/guidedmatte.go）：盒均值双跑窗、引导滤波、精修链
  （引导抠边 + 模糊阈值边缘偏移 + 中灰对比拉伸 + limit 降采样回放）全
  float32；数值测试 5 项（常数守恒/沿引导边贴合/偏移与对比语义/预乘蒙版
  应用/点选连通域）。
- ONNX 管线（internal/ml/subject.go）：u2netp（4.6MB，Apache-2.0）经
  yalue/onnxruntime_go 动态加载；1×3×320×320 NCHW → d0 min/max 拉伸 →
  上采样。分发（2026-10-13 修订）：模型不进仓库，Windows amd64 构建钩子
  校验固定 SHA-256 后把模型和 runtime 随安装包分发；运行时优先 exe 同级
  models 目录，开发版缺失时才按需下载并缓存 %APPDATA%。详见
  [分发说明](../ml-distribution.md)；本次只验证资源准备与校验，未做真实推理、
  GUI 或安装验收。
  进度事件：u2netp CPU 推理 ~百毫秒，面板"渲染中…"已覆盖反馈，未启
  task:progress（换大模型再启——ADR 附录记录）。
## A10 反相（Invert）：补上内核级断言（2026-10-16）

A10 那行记的是「内核已移植（`ApplyInvert`）且菜单可达，但 `tests/golden/c/` 无 invert 内核、render 测试里
也没有 `ApplyInvert` 断言」。核实结果分两半：

1. **黄金 C 内核不该造**：上游反相是 Swift + vImage（`PixelInvert.swift`），**没有 C 内核**。invariant #4
   说黄金 harness 的 C 内核是**上游 C 的拷贝**；凭空写一份「黄金 C」会把参照降级成手写期望。所以正确
   的形式是 `render_test.go` 里混合模式那种：**手工期望值 + 注明 Swift 出处**。
2. **真缺口在判别力上**：桥接侧那条既有断言（`filter_test.go:908`）只测了**全不透明**的像素 ——
   而 `A − C` 与朴素的 `255 − C` 在全不透明时**完全同值**，分辨不出实现对不对。

新增 `internal/render/invert_test.go`：

- 部分透明（A=128、R=100）→ 28；朴素实现会给 155 —— 这条才是定义性的；
- 全不透明 → 155（即通常说的 255 − v）；
- 全透明（A=0）→ 颜色只能是 0；
- 非法预乘（C > A）钳到 0，不通回绕。

**变异验证**：改成 `255 − c` → 失败 ✓；去掉低位钳制 → 失败 ✓。

- 三入口：移除背景滤镜（表单：质量/抠边/蒙版对比/边缘偏移；raw 掩码按
  会话缓存，动滑杆只重跑精修链——Swift MaskCache 语义）；选择主体
  （⌥⌘A，合成图上分割→默认精修→选择载荷）；对象选择（主体掩码上点击点
  8 连通域——ADR 预留的降级组合）。选择进入 currentSelection store，
  CanvasSurface 画蚂蚁线（虚线路径），滤镜/填充/修复的选区载荷从此真实
  生效。
