# 47 — 快捷键系统与重映射

**What to build:** 快捷键基础设施：~114 条定义表移植（Cmd→Ctrl 系统映射、修饰键位掩码）、冲突校验（保留键位）、录制器控件、重映射表单（KeyboardShortcutsSheet）、覆盖持久化；画布/文本/菜单三域分发语义（对应原版 menu/canvasEvent/textEvent 分流）。

**Blocked by:** 13 — 视口与画布导航.

**Status:** ready-for-agent

- [ ] 定义表与原版 114 条一一对应（表驱动测试）
- [ ] 重映射 UI：录制、冲突提示、恢复默认、持久化
- [ ] 三域分流：文本框内不抢画布快捷键（BlendShortcutTests 语义）
- [ ] 全部工具/命令挂接快捷键（与 C 系菜单对齐）

对等矩阵：P01、C14、故事 52。
