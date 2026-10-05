# Notes for AI agents（Windows 重写仓库）

本仓库正在把 macOS 版 Compositor 重写为 Windows 版（Wails + Svelte 5 + Go + WebGPU）。原版 Swift 源码整体位于 `reference/Swift/`，**只读**——它是功能基准与算法真值，不要修改、构建或"顺手修复"它。根目录 `LICENSE` 适用于整个仓库。

## 文档地图（动手前先读）

- `docs/spec.md` — 规格说明书与用户故事（"功能对等"的定义）
- `docs/feature-parity.md` — **功能对等矩阵（唯一权威功能清单，150+ 行）**：拆票时逐行核对"里程碑"列，验收时逐行勾"状态"列；任何功能不得游离于矩阵之外
- `docs/tickets/` — **53 张实施票**（本地追踪器，`00-README.md` 是索引与依赖总览）：逐票 `/implement`（TDD）+ `/code-review`；只做 frontier 上"Blocked by 全勾"的票
- `docs/plan.md` — M0–M10 路线图：工作项、出口标准、风险登记
- `docs/architecture.md` — 目标架构、Swift→Go/TS 模块映射、IPC 契约
- `docs/testing.md` — 测试缝合点、黄金基准 harness、测试移植策略
- `docs/adr/` — 已定决策（技术栈 / .comp 兼容 / C 内核移植 / WebGPU 渲染 / ONNX）
- `reference/Swift/docs/project-format.md` — `.comp` 格式权威规范（v1–11）
- `reference/Swift/docs/writing-comp-files.md` — AI 代理直接写 `.comp` 的契约
- `reference/Swift/AGENTS.md` — 原版代码的阅读指引

## 硬性不变量

1. `.comp` v11 与 macOS 版双向兼容。任何 manifest schema 变更都是破坏性决策，必须先写 ADR 并获得用户确认。
2. 像素不变量：全链路 8bit RGBA 预乘 + sRGB；蒙版 8bit 灰度；统一光栅工厂创建位图。
3. 特殊混合模式（Linear Burn、Vivid Light、Pin Light、Hard Mix、Subtract、Divide 及 Color Burn/Dodge、Soft Light 的原版数值）在 **sRGB 而非线性光**空间计算——复制原版行为，不要按"正确"实现改写。
4. `reference/Swift/` 内文件不可编辑。黄金基准 harness 需要的 C 内核以**拷贝**方式放进新测试目录（注明拷贝来源提交），不改原件。
5. 视图状态（缩放/平移/工具）在前端，文档真源在 Go 后端——不要在前端复制文档数据。

## 工作流

- 逐票实现：`/to-tickets` 生成的票（docs/tickets/，`00-README.md` 是索引）→ 每票 `/implement`（内部 TDD）→ `/code-review`。
- 当前 frontier：票 04（域模型）、08（黄金基准 harness）可并行开工；顺序建议见 `docs/plan.md` 末节。
- 代码风格：Go 惯用命名；TS strict 模式；美式拼写（"color"）。跟随原版的算法注释密度——移植的内核应保留原 C 代码中的数值注释。

## 环境注意（本机）

- Go 代理已固化 `goproxy.cn`（proxy.golang.org 直连不可达）；CI 上用默认源。
- `npm ci` 报 rollup 原生模块 EPERM 时，先 `taskkill` 残留的 esbuild/vite watcher node 进程（wails dev 强杀后会遗留孤儿进程锁文件）。
