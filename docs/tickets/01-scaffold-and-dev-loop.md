# 01 — 脚手架与开发链路

**What to build:** 在仓库根目录建立可在 Windows 上运行、可热重载的 Wails v2 + Svelte 5 + TypeScript strict + Go 项目骨架，提供常暗深色主题的空编辑器窗口。

**Blocked by:** None — can start immediately.

**Status:** done（2026-10-05）

- [x] `wails dev` 在 Windows 启动，前端改动热重载生效（vite 1421 监听 + Compositor-dev.exe 运行实测）
- [x] 深色主题外壳（背景/面板/工具栏色板）与空画布区域呈现
- [x] Go 侧最小绑定往返可调用（`bridge.Service.Version` → 状态栏显示）
- [x] `go build` / `npm run build` 均通过；TS strict 无错误（svelte-check 0 错误 0 警告）
- [x] Wails 生成的前端绑定类型纳入版本控制（frontend/wailsjs/go/bridge/Service.d.ts）

对等矩阵：基础设施（无直接功能行）；为 P06 铺路。

> 备注：工程为手工脚手架（未用 `wails init` 模板，以获得 Svelte 5 + Vite 6 + Vitest 3 + TS strict）；`scripts/genicon` 生成 build/appicon.png 与 build/windows/icon.ico；Go 模块代理使用 goproxy.cn（proxy.golang.org 在本网络不可达）。
