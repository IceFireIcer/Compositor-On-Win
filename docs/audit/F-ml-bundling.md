# ML 模型随包分发检查（2026-10-13）

用户要求：构建时打包模型；本轮不构建应用、不启动 GUI、不调用 Computer Use。
对应票 37 / 50，ADR-0005。保留工作区原有修改；reference/Swift 无改动。

## 实现与代码核对

- `wails.json` 为 Windows 添加预构建缓存校验与后构建离线复制钩子。
- 核对 Wails v2.16.0 源码：钩子工作目录为 build/bin；编译阶段可能清空该
  目录；NSIS 在 Build（含后构建钩子）返回后执行。因此缓存放 build/ml-assets。
- 模型与 DLL 固定版本/哈希；当前 DLL 仅用于 Windows amd64，其他架构失败。
- 缺少缓存 DLL 时通过 Go 模块下载取得锁定版本，支持自定义 GOMODCACHE。
- 安装器显式包含模型、DLL、第三方声明和三份许可证；便携构建保留同样结构。
- Go 按 exe 所在目录优先查找模型；现有文件仍验证哈希；损坏不能静默加载。
- 模型首次下载串行化，避免多个 ML 入口同时写同一临时下载文件。

## 本轮验证

| 检查 | 结果 |
| --- | --- |
| `go test ./internal/ml -count=1` | 通过；随包优先、缓存回退、截断/错误哈希拒绝 |
| `go test ./internal/bridge -run 'Filter' -count=1` | 通过 |
| `go vet ./internal/ml` | 通过 |
| PowerShell 5.1 从 build/bin 执行缓存与离线钩子命令 | 通过；修正参数默认值中脚本目录解析问题 |
| 临时仓库缺少 runtime 缓存，运行 CacheOnly | 通过；锁定模块 DLL 验证并复制，未创建 bin |
| 临时仓库 bin 不存在，Offline 复制资源 | 通过；模型、DLL、声明及三份许可证均存在 |
| 损坏缓存模型 + Offline | 以非零退出码拒绝 |
| windows/arm64 构建参数 | 以非零退出码拒绝不匹配的 DLL |
| 资源准备与发布脚本语法、wails.json 解析 | 通过 |
| `git diff --check` | 通过 |

资源实测：模型 4,574,861 字节，DLL 16,149,344 字节，哈希与
[分发说明](../ml-distribution.md) 一致。准备好的文件位于 build/bin，缓存
位于 build/ml-assets；两者均为忽略的本地产物。

未运行 Wails/npm build、makensis、安装测试或真实 ONNX 推理。上述结果证明
资源准备与校验流程可用，不代表三项 ML 功能已经通过端到端验收。
