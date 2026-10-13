# 上游 Compositor 参考仓库

`reference/Swift/` 是独立的 Git 子模块，来源为
<https://github.com/robbietilton/Compositor>，跟踪上游 `main`。
主仓库通过 gitlink 固定参考代码的提交，保证其他机器和 CI 检出同一份源码。
参考代码继续只读：同步上游允许更新它；Windows 移植工作不直接修改或构建它。

2026-10-13 首次转换时固定到
`e8c9e29701528413cc0e608d61187c1c61790add`：
`Move Camera Raw scrollbar clear of the controls`。
上游项目版本为 1.5.1（build 47）。

## 克隆与检出主仓库记录的版本

```powershell
git clone --recurse-submodules https://github.com/IceFireIcer/Compositor-On-Win.git
```

已有主仓库在拉取或切换提交后执行：

```powershell
git submodule sync --recursive
git submodule update --init --recursive
```

该命令检出主仓库记录的版本；不会主动升级到上游最新版本。
CI 同样使用递归子模块检出。

## 同步上游最新 main

先检查子模块内是否有本地修改；有修改时先保留或提交，避免覆盖。
下面的流程可从固定版本检出的 detached HEAD 开始执行：

```powershell
git -C reference/Swift status --short
git -C reference/Swift fetch origin
git -C reference/Swift switch main
git -C reference/Swift pull --ff-only origin main
git diff --submodule=log -- reference/Swift
git add reference/Swift
```

审查上游差异后，在主仓库提交子模块指针并推送。
主仓库的 `origin` 指向 Windows 仓库，子模块的 `origin` 指向上游 macOS 仓库。
同步参考代码只需读取上游和向 Windows 主仓库推送指针。

## 功能基准与格式边界

Windows 规格、功能对等矩阵和既有黄金基准以 macOS 1.4.5 / `.comp` v11
为原始基准。此次更新的上游 `ProjectManifest.current` 已为 13，格式文档也涵盖
v12/v13。更新参考代码不代表 Windows 已支持这些新格式或新增功能；Windows
manifest schema 和当前 v11 兼容性目标没有随之改变。
如需扩展支持，须先按根目录 `AGENTS.md` 的要求记录 ADR、获得用户确认，并更新
功能对等矩阵和验证范围。

原 1.4.5 参考快照仍完整保留于主仓库历史，可从转换前提交
`a4c3a0c2989ca09b4b10e15af93bbb39b66354c9` 的 `reference/Swift/` 查阅，例如：

```powershell
git show a4c3a0c:reference/Swift/docs/project-format.md
```

本机转换前的目录另有完整备份，位于
`.git/reference-sync/Swift-before-submodule/`。该备份不随主仓库分发；共享和恢复
原始参考源码应以主仓库历史为准。
