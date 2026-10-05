# ADR-0002: `.comp` v11 与 macOS 版逐字节双向兼容

- 状态：已接受（2026-10-05）
- 背景：`.comp` 是文件夹包（`manifest.json` + `images/<uuid>.png` + `*.mask.png`），当前版本 v11，加式版本化，权威规范在 `reference/Swift/docs/project-format.md`。重写后若格式分叉，跨平台互开与 AI 代理工作流都会断裂。

## 决策

1. Windows 版读写的 manifest **逐字节兼容**：同样的字段集（v1–v11 全部版本门控）、排序键 pretty JSON、同样的图层记录 schema。
2. 校验规则逐条移植原版 `ProjectStore`（格式/版本/色彩空间、路径安全、尺寸与数量上限、编组深度 ≤64、剪贴链 ≤256、参考线 ≤1000、版本-特性一致性）——拒绝行为与拒绝原因一致。
3. 保存仍是原子替换（临时文件 + rename），PNG 先写、manifest 最后原子改名——这是热重载契约（~300ms 合并监视）的前提。
4. 任何 schema 变更都是破坏性决策：必须新 ADR + 用户确认 + 同步版本门控实现。

## 理由

- 兼容是产品需求：用户跨 macOS/Windows 协作、AI 代理工具链（writing-comp-files.md）按此格式编程。
- 校验一致性保证"两边都拒绝同样的坏文件"，避免一边生成另一边打不开的文档。
- PNG 资产 + JSON manifest 天然跨平台；唯一平台差异是 QuickLook 预览目录（macOS Finder 语义）——Windows 版跳过生成、读到时忽略。

## 后果

- M1 的验收 = 与 macOS 1.4.5 真实 `.comp` 互开回归。
- 原版文档限界（边长 30,000px / 单面 200M 像素 / manifest ≤4MiB / 资产 ≤512MiB）全部继承。
- Windows shell 无包类型感知——文件关联、图标、缩略图由安装器与（可选的）shell 扩展承担（M9）。
