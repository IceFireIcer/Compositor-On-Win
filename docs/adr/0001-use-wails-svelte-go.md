# ADR-0001: 采用 Wails v2 + Svelte 5 + Go 技术栈

- 状态：已接受（2026-10-05，用户确认）
- 背景：Swift/macOS 版需要完整重写为 Windows 应用。候选路径：Electron/Tauri/C++(WinUI)/Qt/Wails；UI 层候选：React/Vue/Svelte/Solid；后端候选：Go/Rust/C#。

## 决策

**Wails v2（稳定版）+ Svelte 5 + TypeScript strict 前端 + Go 1.24 后端**。明确排除 Wails v3（alpha，接口未稳）。

## 理由

- **像素吞吐**：图像编辑器的重活（合成/滤镜/编解码/PSD 解析）需要一个无 GC 抖动焦虑、goroutine 天然并行的后端；Go 标准库自带 PNG/JPEG 编解码，纯 Go 工具链避免 Rust/C++ 的构建矩阵负担。
- **Wails 优于 Electron**：单一 WebView2（系统常青）、无捆绑 Chromium、产物小、Go 绑定自动生成 TS 类型；优于 Tauri 的点：Go 侧生态（fsnotify、image 栈）与团队栈匹配（用户指定 Go/TS）。
- **Svelte 5 优于 React/Vue**：runes 信号模型与"文档树镜像 + 视图状态"的心智贴合；编译期优化使覆盖层与面板重渲染开销最小；模板语法接近原版 SwiftUI 声明风格，利于逐屏对照移植。
- **WebView2 已支持 WebGPU**（常青运行时），前端 GPU 管线可行（见 ADR-0004）。

## 备选与放弃原因

- Electron：产物 150MB+、双运行时内存；仅在其 Chromium WebGPU 更稳时才值得。
- Tauri + Rust：性能上限更高，但 Rust 后端 + cgo 式 FFI 对本团队的迭代速度与 AI 代理可维护性不友好。
- WinUI3/Qt：纯 C++/C# 路线放弃了 Web 文本/IME/无障碍生态，文字工具（原版 TextKit 深度依赖）成本陡增。

## 后果

- 依赖 WebView2 常青运行时（Win10 1809+ 预装或首启引导）。
- cgo 需求（libheif/LibRaw/ONNX）引入 mingw/MSVC 构建矩阵——用 vcpkg 固定，独立于主线。
- Wails 绑定是契约面：命令粒度设计错误会放大为 IPC 风暴——像素走 HTTP 面（见 architecture.md §3）。
