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
  上采样。分发：模型不进仓库，按需下载（固定 URL+SHA-256 钉版+大小下限）
  缓存 %APPDATA%；runtime 库随安装包。离线/未装时三入口返回可读错误不崩。
  进度事件：u2netp CPU 推理 ~百毫秒，面板"渲染中…"已覆盖反馈，未启
  task:progress（换大模型再启——ADR 附录记录）。
- 三入口：移除背景滤镜（表单：质量/抠边/蒙版对比/边缘偏移；raw 掩码按
  会话缓存，动滑杆只重跑精修链——Swift MaskCache 语义）；选择主体
  （⌥⌘A，合成图上分割→默认精修→选择载荷）；对象选择（主体掩码上点击点
  8 连通域——ADR 预留的降级组合）。选择进入 currentSelection store，
  CanvasSurface 画蚂蚁线（虚线路径），滤镜/填充/修复的选区载荷从此真实
  生效。
