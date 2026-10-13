# Windows 模型随包分发

Windows amd64 的普通 `wails build`、`wails build -nsis`、本地发布脚本和 CI
使用 `wails.json` 的构建钩子准备同一组固定资源。

## 构建流程

1. 预构建钩子运行 `scripts/prepare-ml-assets.ps1 -CacheOnly`，把固定版本的
   `u2netp.onnx` 和 `onnxruntime.dll` 校验并缓存到 `build/ml-assets/`。
   模型从 rembg release 下载；DLL 从 `onnxruntime_go@v1.36.0` 的
   `test_data/onnxruntime.dll` 取得。干净检出会先下载该锁定 Go 模块。
2. Wails 编译会清理 `build/bin`；`build/ml-assets` 不受影响。
3. 后构建钩子以 `-Offline` 再次校验缓存并复制资源和许可到 `build/bin/`。
4. `-nsis` 在钩子完成后打包这些文件。任何必需资源缺失或哈希不符都会失败。

模型和 DLL 不提交 Git。当前 DLL 是 x64 CPU runtime；其他 Windows 架构
会在预构建阶段明确失败，不能混用此 DLL。

## 安装和便携目录

```text
Compositor.exe
onnxruntime.dll
THIRD-PARTY-NOTICES.md
models/
  u2netp.onnx
licenses/
  u2netp-Apache-2.0.txt
  onnxruntime-MIT.txt
  onnxruntime-go-MIT.txt
```

安装器内置这些文件。便携分发需要保留整个目录结构，单独复制 exe 不包含
模型和 runtime。资源原始体积合计约 20.7 MB（压缩后安装包增量另行测量）。

程序按 **exe 所在目录** 查找资源，不依赖启动时的工作目录，也不需要把
模型写入安装目录。随包模型每次准备推理时验证 SHA-256；损坏会返回错误。
开发检出缺少随包模型时，才使用 `%APPDATA%/Compositor/models` 缓存及按需下载。

## 提前准备与离线构建

在仓库根目录先执行：

```powershell
powershell.exe -NoProfile -File scripts/prepare-ml-assets.ps1 -CacheOnly
```

完整缓存准备好后，构建钩子复用缓存，无需再次下载 ML 资源。也可单独检查和
复制资源（不编译程序）：

```powershell
powershell.exe -NoProfile -File scripts/prepare-ml-assets.ps1 -Offline
```

离线参数在缺失或损坏缓存时失败。完全离线构建仍须提前准备 Go/npm 依赖、
Wails 和原生工具链；此脚本只负责 ML 资源。

## 固定资源

| 文件 | 字节数 | SHA-256 |
| --- | ---: | --- |
| `u2netp.onnx` | 4,574,861 | `309c8469258dda742793dce0ebea8e6dd393174f89934733ecc8b14c76f4ddd8` |
| `onnxruntime.dll` | 16,149,344 | `69d8e6d3879a3b4001cdc74c8ed9ccc7e7f799a5b847059738323404519ec471` |

分发决策见 [ADR-0005](adr/0005-vision-ml-replaced-by-onnx.md)。资源校验通过
不代表三项 ML 功能已经完成真实推理、GUI 或安装验收。
