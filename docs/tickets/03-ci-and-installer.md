# 03 — CI 门禁与 NSIS 安装包骨架

**What to build:** GitHub Actions（windows-latest）下每次 push/PR 自动：格式检查、Go 测试（含黄金基准）、Vitest、`wails build`、NSIS 打包；产物上传为 artifact。

**Blocked by:** 01 — 脚手架与开发链路.

**Status:** done（2026-10-05）

- [x] 工作流 `.github/workflows/verify.yml`（windows-latest）：npm ci → svelte-check → vitest → go vet → go test → wails CLI → choco install nsis → `wails build -nsis` → 上传 `build/bin/*.exe` 产物；push main / PR / 手动触发均运行。本地已逐步实测同款命令（工作流本身待 push 后首次运行确认）
- [x] 每一步都阻塞后续步骤，任一失败即红；产物上传 `if-no-files-found: error` 兜底
- [x] NSIS：实测确认 `wails build -nsis` 不会自动安装 NSIS（缺 makensis 仅警告跳过），CI 用 `choco install nsis` 补齐。安装/卸载手动冒烟步骤（首次产物产出后执行）：下载 artifact → 运行 `Compositor-amd64-installer.exe` → 核对开始菜单/卸载列表出现 Compositor → 控制面板卸载 → 确认安装目录清除
- [x] 耗时预算：本地实测前端+Go 检查段 18s、wails 构建 ~10s；预估 runner 全程（含 CLI/依赖安装与 NSIS）4–7 分钟，低于 10 分钟预算；已配 npm/Go 模块缓存与并发取消

对等矩阵：P04 的构建层（签名与自动更新在票 50）。

> 环境记录：本机 Go 代理需 goproxy.cn（CI 上用默认源即可）；`npm ci` 前若报 rollup 原生模块 EPERM，先结束残留的 esbuild/vite watcher 进程（wails dev 强杀后可能遗留孤儿 node 进程）。
