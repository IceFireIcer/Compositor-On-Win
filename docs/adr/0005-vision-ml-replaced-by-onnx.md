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

## 附录：M6 定案（票 37，2026-10）

**模型**：u2netp（显著对象分割，4.6 MB fp32 ONNX，Apache-2.0，Qin 等人 u²-net
项目的 rembg 转换版）。选它而非完整版 u2net（~170 MB）或 ISNet（~170 MB）：
体积差 35 倍而抠图质量在"精修链兜底"的验收口径下足够；Apache-2.0 许可允许
再分发与二次转换。

**分发**：模型**不进仓库**（保持克隆轻量、许可文件随模型走）。运行时按需
下载：固定 URL（rembg release）→ SHA-256 校验（校验和由发布流程钉进
`-ldflags -X ml.ModelSHA256=…`，见仓库发布清单）→ 缓存到
`%APPDATA%/Compositor/models/u2netp.onnx`。下载失败/离线时三个功能入口返回
"分割模型未安装"的可读错误，不崩溃。onnxruntime 共享库（MIT）随安装包内置；
开发检出在程序目录/PATH 放置 `onnxruntime.dll`。

**推理**：Go 绑定 `github.com/yalue/onnxruntime_go`（动态加载共享库，无
cgo）。输入 1×3×320×320 NCHW 0–1；输出 d0 图按 min/max 拉伸回 0–1 后上采样，
再走精修链。推理同步执行于后台 worker（滤镜预览管线）或端点 goroutine；
体量小（u2netp CPU ~100ms 量级），暂不引入进度事件通道——面板已有
"渲染中…"反馈，若将来换完整版模型再启用 `task:progress`。

**对象选择降级**：u2netp 只出单张显著图（无实例区分）。点选对象 = 主体
掩码上包含点击点的 8 连通域（render.LargestSubjectAt），即 ADR 预留的
"主体选择 + 点选区域"组合实现，已在票中记录。
