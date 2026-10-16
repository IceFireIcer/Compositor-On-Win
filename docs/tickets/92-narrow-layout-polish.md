# 92 — 窄窗口图层与工具栏布局

**What to build:** 窄窗口下图层面板和工具选项栏保持可操作，按钮、名称编辑和滚动区域不跳动或溢出。

**Blocked by:** 02 — 编辑器外壳 UI 与新建画布; 20 — 图层操作命令族与图层面板。

**Status:** in-progress（2026-10-16）—— 一项查出并修掉真缺陷；其余经测量已满足并被测试钉住。

- [x] 工具选项栏可横向滚动且没有多余滚动条。
      实测：240px 下 `scrollWidth > clientWidth`（内容被挤出而不是被压扁，各控件宽度都 > 0），
      `scrollHeight <= clientHeight`（没有第二条竖滚动条）；1600px 下两个方向都不滚。
      新建 `ToolOptionsBar.browser.test.mjs` 把这两条钉住（用画笔，它是选项最多的一支；默认工具那点
      控件在 240px 下本来就放得下，测不出东西）。
- [x] 窄图层面板的按钮和文本保持在边界内 —— **这一条原本是坏的**。
      `.mask-row` 的第三方栏（蒙版菜单）原先是 `minmax(88px, auto)`：**硬下限 88px** 再加 26px 缩进，
      面板压到 148px 以下时整个菜单被顶出右边界（实测面板 140px 时控件 right = 156）。改成
      `minmax(0, 88px)` —— 封顶 88px 好读、允许收缩，**宁可截断，不要溢出**。
      扫描测试覆盖 200/160/140/120/100px，断言 `scrollWidth === clientWidth` 且没有控件越界。
- [x] 重命名时行高稳定。180px 下量：进入重命名前后 `.layer-entry` 高度差 ≤ 1px。
      禁用按钮与空状态**不依赖浏览器默认样式**：`app.css` 有全局 `button:disabled`，图层面板另有
      `.layer-menu button:disabled`，空状态用 `--text-dim` 等主题令牌，与菜单同一套。
- [ ] 小窗口、深色主题和键盘焦点下无遮挡或布局跳变。**部分**：焦点样式逐处存在（`.row`/`.mask-row`、
      选项栏的 input/select、各处 sheet 的 `:focus-visible`，都是 2px `--accent` 描边）。两处 `outline: none`
      是刻意的（画布文本框用虚线边框、命令面板搜索框是模态里唯一焦点），不是缺陷。**未做**：整壳在
      极小窗口下的遮挡检查（要在真实编辑器外壳里量），以及深色主题下的对比度实测。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `ToolOptionsBar.browser.test.mjs` 两条 | 窄宽度横滚且无第二滚动条、无压扁控件；宽窗口完全不滚 |
| `LayersPanel.browser.test.mjs`「down to 100px」 | 宽度扫描：面板不横向溢出、没有控件越界（**测出过真缺陷**） |
| `LayersPanel.browser.test.mjs`「narrow width … rename」 | 180px 下按钮在界内、重命名前后行高一致 |

两条新测试都**反向验证过**：把 CSS 改回 `minmax(88px, auto)`，扫描测试立刻在 140px 处失败。

对等矩阵：U39。原版锚点：`LayersPanel.swift`、`ProjectTabs.swift`；提交 `68396ff`、`790195f`、`0a5c8d9`。
