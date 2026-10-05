# 50 — 安装包与自动更新

**What to build:** 发布链路：签名 NSIS 安装包（代码签名证书流程脚本化）、自动更新（GitHub Releases + EdDSA 签名 feed、启动/手动检查、更新提示 UI、差量或全量安装器）、appcast 等价 feed 生成脚本。

**Blocked by:** 49 — Shell 集成（关联/最近/脏标记）.

**Status:** ready-for-agent

- [ ] 安装→升级→卸载在干净环境验证（脚本记录）
- [ ] 更新 feed 生成与签名可复现（脚本化，对应 publish.sh 语义）
- [ ] 应用内"检查更新"到应用新版端到端
- [ ] 无证书环境的降级构建路径（不签名）仍可用

对等矩阵：P04、C09、故事 53。
