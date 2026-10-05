# ADR-0005: Apple Vision ML 能力以 ONNX Runtime 替代

- 状态：已接受（2026-10-05）
- 背景：原版三个功能依赖 Apple Vision 框架的 `VNGenerateForegroundInstanceMaskRequest`：主体选择、对象选择、移除背景（配合 Swift 实现的引导滤波抠边）。Windows 无等价系统能力。

## 决策

1. 前景分割改为 **ONNX Runtime（Windows 直接用原生 DLL，不经 cgo）+ 开源显著性/分割模型**（首选 u2net / ISNet 系，许可与体积在 M6 开票时定案）。
2. 蒙版精修链保留原版算法：引导滤波抠边（GuidedMatte 移植）+ 模糊阈值偏移 + 矩阵对比拉伸——与 C 黄金基准同规则验收。
3. 模型分发：优先随安装包内置；若总体积超预算改为首启按需下载 + 签名校验（M6 定案并归档）。
4. 推理在 Go 侧后台执行（onnxruntime 共享库 + Go 绑定），UI 走 `task:progress` 事件。

## 理由

- ONNX Runtime 是 Windows 生态一等公民（DirectML EP 可用 GPU），许可 MIT，无需引入 Python/SDK 重依赖。
- u2net/ISNet 是抠图任务事实标准模型，效果经 rembg 等成熟项目验证；与原版 Vision 的单主体掩码语义对齐。
- 精修算法（引导滤波等）是纯数学，移植后与原版行为可比——ML 部分只负责"粗掩码"，质量锚点在精修链。

## 备选与放弃原因

- 保留调用云端分割 API：违背离线可用与隐私基线，且引入账号/网络依赖。
- Windows.Media.MachineLearning / WinML：生态收敛于 ONNX Runtime，WinML 已处维护态。

## 后果

- 模型体积（u2net ~170MB fp32 / ~45MB fp16 量化）影响安装包——内置 vs 下载在 M6 用数据定案。
- 分割质量与原版 Vision 存在固有差异——验收标准是"可用且可复核的粗掩码 + 与原版一致的精修链"，不做像素级等同承诺。
- 对象选择（点选实例）若单模型无法对齐，允许 M6 降级为"主体选择 + 点选区域魔棒"的组合实现，记录在票中。
