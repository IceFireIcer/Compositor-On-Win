# 49 — Shell 集成（关联/最近/脏标记）

**What to build:** Windows shell 集成：`.comp` 文件关联与"打开方式"、最近文件（跳转列表）、命令行参数打开文件、窗口脏标记（任务栏图标覆盖 + 标题）、单实例协议（二次打开归到既有窗口新标签）。

**Blocked by:** 03 — CI 门禁与 NSIS 安装包骨架; 05 — .comp 存取校验与互开.

**Status:** done（2026-10-10）

- [x] 双击 .comp 用本应用打开并进入标签
- [x] 最近文件跳转列表与清除
- [x] 脏文档任务栏覆盖图标与标题变化
- [x] 单实例：第二次启动把文件交给既有实例

对等矩阵：C03、P03、P08、P09。

## 实现说明（2026-10-10）

**"打开一个文件"收敛成一条路（Go）**：`project_io.go` 的 `openPath`/`openPathReply` 是唯一入口
——`.comp` 进标签、`.psd/.psb` 解析后停在待确认槽。打开对话框、最近项目菜单、外壳递来的文件
三条入口都走它，不会各写一遍。

**外壳送文件（验收 1、4）。** main.go 把 `os.Args[1:]` 排进队列（双击关联、命令行、"打开方式"
都落在这里）；Wails 的 `SingleInstanceLock`（UniqueId `compositor-on-windows-single-instance`）
在第二次启动时把参数交给既有实例（相对路径按第二实例的工作目录解析成绝对路径），再
`NotifyShellPaths`：Unminimise + Show + 推 `shell:openPaths` 事件。前端在启动时与事件到达时调
`OpenShellPaths()` → 逐条打开并返回聚合结果（快照 / 打开清单 / 失败清单 / 待确认的转换报告）。
Photoshop 只有一个待确认槽：已有待确认时后续 PSD 报错而不是挤掉它（回复里也把已有的那份报给前端，
让表单照常弹）。

**最近项目与跳转列表（验收 2）。** `RecentStore`（recent.go）把列表落成 JSON
（`%AppData%\compositor-on-windows\recent.json`）：最近优先、大小写不敏感去重、上限 10、读取时
过滤已移动/删除的文件（对齐 `RecentProjects.refresh` 的 fileExists 过滤）。成功打开与保存都记一笔
（`noteRecent`），同时调 `winchrome.NoteJumpListPath`（`SHAddToRecentDocs`）——Windows 会把它们
放进本应用任务栏跳转列表的"最近"分类。文件菜单逐条列出"打开最近：<文件名>"（完整路径在
tooltip/hint），末尾是"清除最近记录"：清应用列表 + 外壳最近文档（`SHARD_PIDL`）。

**窗口标题与脏标记（验收 3）。** `SyncWindowChrome(name, dirty)` 设窗口标题
（`名字* - Compositor`，无文档时只有 `Compositor`）与任务栏覆盖图标
（`winchrome.SetDirtyOverlay` → `ITaskbarList3::SetOverlayIcon`，红色圆点）。前端在活动标签的
名字/脏标记真的变化时才过桥（`App.svelte` 的 `lastChromeKey`）。覆盖图标失败只记一行日志：
没有任务栏按钮的会话（远程桌面、测试）不该被打扰。

**真机实测出来的一个坑（重要）。** 子代理用真实可见窗口插桩后发现
`ITaskbarList3::SetOverlayIcon` 是跨进程同步调用，**会卡住**：同一进程里
`CoInitializeEx`/`CoCreateInstance`/`HrInit`/GDI 建图标都瞬间返回，卡的就是
`SetOverlayIcon` 本身（6 次调用里 5 次 >6s，最长一次 >60s；外壳本身响应正常）。因此
`winchrome` 里有一把**有界**的闸门：每次外壳调用最多等 2s，超时返回中文错误、卡住的那次
继续占名额让后续同类调用立刻失败，等它真返回后自动恢复 —— 最坏只有一个线程被外壳拖住，
编辑器永不被挂起。另外**覆盖图标与"最近文档"各用一把独立闸门**：覆盖图标卡住时最近文档
照常记录，否则菜单列表与任务栏跳转列表会长期不同步而没人发现。

**P09 窗口尺寸记忆**：票 02 已落地（`WindowStore` + main.go `OnStartup` 恢复 +
`App.svelte` resize 防抖保存），本票核对后勾矩阵。

**NSIS 关联**：`wails.json` 的 `info.fileAssociations`（ext `comp`、name
`Compositor.Project`、icon `appicon`、role Editor）—— `wails build -nsis` 由它生成
`wails_tools.nsh` 的 `wails.associateFiles` 宏（`APP_ASSOCIATE` 写
`Software\Classes\.comp` 与 ProgID 的 `shell\open\command`）。
注意：`fileAssociations` **不属于** `build/windows/info.json`（那是版本资源的 StringTable，
塞进去会让 `wails build` 报 `cannot unmarshal array into ... version.StringTable`）；
`wails_tools.nsh` 是生成文件，下次 `wails build -nsis` 会按 wails.json 重写它。

## 测试

- Go：`recent_test.go`（5 组：最近优先/去重/上限/过滤已删除/坏文件）、`shell_test.go`（9 组：
  外壳路径打开并记最近、失败清单不动文档、待确认槽不被挤掉、标题与覆盖图标、覆盖图标失败不致命、
  最近菜单与清除、无持久层退化、`isPhotoshopPath`、`OpenPath` 只记成功）、`main_test.go`
  （2 组：第二实例参数解析）、`internal/winchrome/chrome_test.go`（跳转列表路径规范化等）。
- 前端：`state/shell.test.ts`（9 组：列表刷新/读取失败退化/清空、`OpenPath` 的三种结果、
  `OpenShellPaths` 的聚合结果与异常退化、`SyncWindowChrome` 的调用）。

## 已知差异 / 未实现（诚实登记）

1. **外壳 API 有真机验证、但没有自动化断言**：`SetOverlayIcon` 与 `SHAddToRecentDocs` 都在真实
   窗口上跑通过（也正因此发现了上面那个卡顿），但 CI 里没有断言（桌面会话的可用性不可控），
   也没截到任务栏上的红点（验证机桌面被全屏窗口挡住）。Go 侧纯逻辑（路径规范化、闸门、队列、
   回复）有测试。
   另外：覆盖图标卡住的那次会让**后续覆盖图标调用**立刻跳过（错误只落日志），所以那段时间里
   图标可能停在旧状态，直到外壳恢复 —— 这是"绝不挂起编辑器"的代价，选择保留。
2. **跳转列表用 `SHAddToRecentDocs` 而不是 `ICustomDestinationList`**：前者零依赖，Windows 自动
   把它归到跳转列表的"最近"分类；代价是分类名/顺序由系统决定，且不能自定义任务项。
3. **文件关联只由安装包写**：便携版没有"注册关联"入口。`build/windows/appicon.ico` 是
   `wails build` 由 `build/appicon.png` 生成的（关联的 DefaultIcon，NSIS 宏里有
   `File "..\appicon.ico"`），所以它**入库**：否则只跑 makensis 的步骤会因为缺文件失败。
4. **第二次启动只转发文件参数**：`-` 开头的开关被丢掉（`resolveLaunchedPaths`）。相对路径
   按**本进程的工作目录**补全：Wails 的 `SecondInstanceData.WorkingDirectory` 是"可执行文件
   所在目录"而不是第二实例的工作目录，第二实例的 CWD 没有可靠来源（文件关联给的都是绝对
   路径，不受影响）。
5. **没有 `SetForegroundWindow`**：Wails 没暴露这个 API，所以第二次启动只做
   `WindowUnminimise + WindowShow`；窗口没被最小化、只是被别的窗口盖住时，Windows 的前台
   锁定策略可能不让它跳上来（macOS 版有 `application.activate()`）。
6. **"清除最近记录"也清系统"最近"文件夹里本应用的条目**：`SHAddToRecentDocs(SHARD_PIDL, NULL)`
   是 Windows 的官方清空方式（只清本应用记的），但副作用是资源管理器的"最近使用的文件"里
   本应用的快捷方式一起消失 —— 这正是菜单项想做的事，登记备查。
7. **从"最近"打开 PSD 后取消转换报告不会记进列表**：只有确认并真的打开才算"打开过"
   （原版 `noteNewRecentDocumentURL` 在打开流程里，取消同样不入列表）。
8. **列表刷新时机**：原版在应用激活时重刷（`didBecomeActive`），这里在打开"文件"菜单时与
   打开/保存后重刷 —— 菜单驱动的列表，效果等价。
5. **多文件里的 PSD 只停一个**：转换报告一次一个（原版也是逐个确认）。
6. **最近项目的"打开失败"不自动从列表里删**：下次打开仍会列出（读取时只过滤"不存在"，
   不过滤"读不了"）。
