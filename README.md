# Compositor for Windows

Compositor 是一款免费开源的图层式图像编辑器，围绕 Photoshop 式的合成与修图工作流构建。**本仓库是它的 Windows 版重写**：以 macOS 原版为唯一功能基准，用 Wails v2 + Svelte 5 + Go + WebGPU 从零实现，达到全功能对等。

- macOS 原版（Swift/Xcode）：[reference/Swift/README.md](reference/Swift/README.md) · 原版仓库 <https://github.com/IceFireIcer/Compositor-On-Win>（历史）
- 上游 macOS 发行版：<https://robbietilton.com/compositor>

## 仓库布局

| 路径 | 内容 |
| --- | --- |
| `reference/Swift/` | macOS 原版 1.4.5 完整源码（Swift/Xcode/脚本），**只读参考**——功能基准、算法真值、测试先例 |
| `docs/` | Windows 版执行文档：[spec](docs/spec.md) · [功能对等矩阵](docs/feature-parity.md) · [plan](docs/plan.md) · [architecture](docs/architecture.md) · [testing](docs/testing.md) · [ADR](docs/adr/) · [实施票 ×53](docs/tickets/) |
| `internal/`、`frontend/` | （M0 起创建）Go 后端与 Svelte 前端 |
| `LICENSE` | MIT，适用于整个仓库（含移植代码） |

## 当前状态

📋 **规格与计划阶段**——尚未开始编码。路线图 M0–M10 见 [docs/plan.md](docs/plan.md)；第一个可验证里程碑是 M2 结束时"打开并忠实渲染真实 `.comp` 项目"。

## 构建要求（规划，M0 落地）

- Windows 10 1809+（WebView2 常青运行时）
- Go 1.24+、Node 20+、Wails v2
- GPU：支持 WebGPU 的设备（不可用时自动回退 Go CPU 合成）

## 关键不变量

1. `.comp` 项目格式（manifest v11）与 macOS 版**逐字节双向兼容**——规范见 [reference/Swift/docs/project-format.md](reference/Swift/docs/project-format.md)。
2. 像素链路统一 8bit RGBA 预乘 + sRGB；蒙版为 8bit 灰度。
3. 特殊混合模式在 sRGB（非线性）空间计算——复制原版行为，不做"修正"。

## 许可

MIT —— 见 [LICENSE](LICENSE)。
