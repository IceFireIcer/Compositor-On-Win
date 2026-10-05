# 测试策略 — Compositor for Windows

> spec 的测试决策在此展开为可执行策略。原则：**只测外部行为**（manifest 字节、渲染输出 PNG、交互后的文档状态），不测内部实现。

## 1. 缝合点（Seams，从高到低）

| # | 缝合点 | 测什么 | 对位原版 |
| --- | --- | --- | --- |
| 1 | **ProjectStore 往返**（最高） | 任意文档 → 保存 → 重开 → manifest 逐字节等价 + 像素逐位等价；校验器拒绝非法 manifest（逐条规则） | `ProjectTests` / `ExternalChangeTests` |
| 2 | **文档 API** | 经公开文档接口驱动工具/变换/调整，断言文档状态与撤销栈 | 517 个 Swift 逻辑测试的语义移植 |
| 3 | **黄金基准** | Go 移植内核 vs 原版 C 内核输出（参考 PNG） | 原版 C 内核即真值 |
| 4 | **CPU↔GPU 一致性** | 同一文档 Go 合成 vs WGSL 合成输出对比 | 原版 `GPUCanvasTests` 的"GPU=Cisco CPU 导出"思路 |
| 5 | **前端单元** | 视口数学（缩放梯/锚点/吸附）、快捷键表、状态机 | `CanvasViewport` 纯结构可直测 |
| 6 | **E2E 冒烟** | 新建→绘制→导出 主流程（Playwright 走 Wails dev server） | `CompositorUITests` 2 条流程 |

缝合点数量刻意收敛：**Go 侧一个主缝合（文档 API + ProjectStore），前端侧一个主缝合（视口/状态库）**。新缝合需在票中说明理由。

## 2. 黄金基准 harness（像素内核的验收机制）

**动机**：9 个 C 内核（约 2,528 行）是算法真值。Go 移植必须逐位（或规定 ε）复现，而不是"看起来差不多"。

**机制**：
1. `tests/golden/` 内放一份 C 内核**拷贝**（拷贝自 `reference/Swift/Compositor/Rendering/*.c`，注明来源提交哈希——reference 区保持只读）+ 一个极小的 C 驱动（读参数 JSON → 输出 PNG）。
2. Windows 原生编译（mingw/MSVC 均可，内核是纯 C99）→ 生成**参考 PNG** 库（提交入库，参数 JSON 同存，可复现）。
3. Go 测试对同一参数运行移植内核，与参考 PNG 逐像素比较；默认要求逐位一致，浮点敏感内核（如 Camera Raw 高光修复）允许 ≤1/255 的 ε，且须在测试名中显式标注。
4. 新增内核移植 = 新增参数集夹具；参数集覆盖原版测试里出现过的典型值（对齐原版测试的输入）。

**产物**：`tests/golden/ref/*.png`（参考）、`tests/golden/cases/*.json`（参数）、Go 比较测试。

## 3. 测试移植（517 个 Swift 测试）

- 原版测试是纯逻辑测试（Swift Testing 框架、共享 `@MainActor`、无 UI），语义可 1:1 翻译。
- 移植单位：按测试文件族（Project/Layer/History/Brush/Selection/Adjustment/CameraRaw/PSD/…），随对应里程碑落地（映射见 plan.md 各里程碑出口标准）。
- 原版字节级 PSD 夹具（`PSDFixture` 程序化生成 PSD 字节流）移植为 Go 夹具——不依赖外部 PSD 文件。
- 窗口耦合套件（FloatingPanelTests / SliderSnapTests / TitleBarDragTests / CanvasEntryTests）不移植——AppKit 特定行为在 Web 侧以 Playwright 冒烟覆盖。

## 4. 各层测试技术

| 层 | 框架 | 范围 |
| --- | --- | --- |
| Go 单元/集成 | `go test` | domain/project/history/pixel/render/psd 全量 |
| 黄金基准 | `go test` + C harness | 内核逐位验收 |
| CPU↔GPU | `go test` + headless 浏览器（Vitest/Playwright 内调 WGSL 渲染读回） | 合成一致性 |
| 前端单元 | Vitest | 视口/快捷键/状态库/桥接层 |
| E2E | Playwright | 新建→绘制→导出、打开→热重载 两条主流程 |
| 性能基准 | Go bench + 浏览器 Performance | 4K 笔刷回显 <16ms（对位 `brush-performance.md` 基准） |

## 5. CI 门禁（GitHub Actions，windows-latest）

1. `gofmt`/`go vet` + `go test ./...`（含黄金基准）
2. `npm ci` + `vitest run`
3. `wails build`（无签名）→ 产物上传
4. Playwright 冒烟（dev server 模式）
5. 全绿才可合并；`.comp` 互开回归测试在每条 PR 上必跑。

## 6. 好测试的标准

- 断言外部可见结果：保存的字节、渲染的像素、撤销后的文档状态、事件序列——不断言内部调用次序或私有结构。
- 失败信息可定位：黄金基准失败输出差异图；往返失败输出首个不等字节偏移。
- 快：像素级测试用小画布（≤512px）常态运行；大画布基准进显式 opt-in 的性能测试。
