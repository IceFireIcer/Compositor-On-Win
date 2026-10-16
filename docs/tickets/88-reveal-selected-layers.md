# 88 — 画布选层后揭示图层

**What to build:** 用户在画布选中或切换图层后，图层面板自动滚动并揭示对应行。

**Blocked by:** 20 — 图层操作命令族与图层面板; 74 — 从欢迎页和标签栏创建项目。

**Status:** done（2026-10-16）

`LayersPanel.svelte` 里一个 effect：选择变了就把当前选中行滚进视野。

- [x] 画布选择、键盘切层和跨组选择都能定位面板行，已可见行不闪烁。
      不靠「这一行可见吗」来判断，而是用 `scrollIntoView({ block: "nearest" })` —— 它是**最小
      滚动量**，整行已经可见时什么都不做。工具提示的列表本来就没这个逻辑（grep 过）。
      选中的定义直接取面板自己的真值：`[role="option"][aria-selected="true"]`，所以蒙版行
      （`.mask-row`）与图层行走同一条路，不需要复制那份「谁被选中」的判断。
- [x] 隐藏组、窄面板和重命名期间的滚动不改变选择或编辑状态。
      滚动是只读操作，不碰文档与编辑目标。**重命名不会触发滚动**：effect 的依赖只有
      `activeLayerID` 与蒙版目标两个值，名字、缩略图、拖拽意图都不在其中。
- [x] 无对应层、关闭文档和撤销时不抛错。
      查不到行就静默返回（隐藏组里没有这一行、文档刚关掉、层被撤销删掉都走这条路）。
      浏览器测试里挂了 `pageerror`，断言整段过程一个错都没抛。

## 一个被测试纠正的假设

原本以为「快照没变，effect 就不会重跑」，所以只测「rev 刷新不该再滚」。测试立刻报错：
`applyDocumentSnapshot` 是**整体替换** state 对象，effect 因此会重跑。于是加了「键没变就直接返回」
的守卫 —— 行为更符合意图，那条断言也才有意义。

## 验证

`LayersPanel.browser.test.mjs` 里新增一条，用真实浏览器挂面板、改写 `Element.prototype.scrollIntoView`
记录调用：断言滚的是选中行、参数是 `{ block: "nearest" }`、rev 刷新不再滚、没有选中层时不滚也不抛错、
点蒙版行同样揭示。**反向验证过**：注释掉揭示调用，这条测试立刻失败（expected 0 to be greater than 0）。

对等矩阵：U35。原版锚点：`NativeLayerList.swift`、`ProjectWorkspaceTests.swift`；提交 `d2b8aaf`。
