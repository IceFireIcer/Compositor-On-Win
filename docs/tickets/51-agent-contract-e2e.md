# 51 — 代理契约端到端

**What to build:** 验收 AI 代理工作流在 Windows 的完整契约（docs/writing-comp-files.md）：代理对已打开项目写 PNG → 原子改名 manifest → ~300ms 重载、保留视口/选区、清撤销、脏文档询问；提供官方示例脚本与验收记录页；文档更新到 Windows 差异（路径/换行/文件锁）。

**Blocked by:** 07 — 外部变更监视与原位重载; 49 — Shell 集成（关联/最近/脏标记）.

**Status:** ready-for-agent

- [ ] 端到端脚本：真实写入 → 观察重载时序与视口保留
- [ ] 并发写入/部分写入（先 PNG 后 manifest 顺序破坏）的容错测试
- [ ] Windows 文件锁/OneDrive 占位符等本地怪癖的处理记录
- [ ] writing-comp-files.md 增补 Windows 差异节

对等矩阵：P05、故事 56。
