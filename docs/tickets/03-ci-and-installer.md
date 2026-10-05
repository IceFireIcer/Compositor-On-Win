# 03 — CI 门禁与 NSIS 安装包骨架

**What to build:** GitHub Actions（windows-latest）下每次 push/PR 自动：格式检查、Go 测试（含黄金基准）、Vitest、`wails build`、NSIS 打包；产物上传为 artifact。

**Blocked by:** 01 — 脚手架与开发链路.

**Status:** ready-for-agent

- [ ] 工作流全绿后产物（可安装 exe/msi + 未签名）可下载
- [ ] 任一测试失败即红，阻塞合并（"CI 全绿"为合并门槛）
- [ ] NSIS 安装/卸载在干净 Windows 环境实测成功（冒烟脚本或手动步骤记录）
- [ ] 测试耗时 < 10 分钟的预算记录在案

对等矩阵：P04 的构建层（签名与更新不在本票）。
