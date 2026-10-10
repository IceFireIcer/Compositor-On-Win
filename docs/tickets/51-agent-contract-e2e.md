# 51 — 代理契约端到端

**What to build:** 验收 AI 代理工作流在 Windows 的完整契约（docs/writing-comp-files.md）：代理对已打开项目写 PNG → 原子改名 manifest → ~300ms 重载、保留视口/选区、清撤销、脏文档询问；提供官方示例脚本与验收记录页；文档更新到 Windows 差异（路径/换行/文件锁）。

**Blocked by:** 07 — 外部变更监视与原位重载; 49 — Shell 集成（关联/最近/脏标记）.

**Status:** done（2026-10-10）

- [x] 端到端脚本：真实写入 → 观察重载时序与视口保留
- [x] 并发写入/部分写入（先 PNG 后 manifest 顺序破坏）的容错测试
- [x] Windows 文件锁/OneDrive 占位符等本地怪癖的处理记录
- [x] writing-comp-files.md 增补 Windows 差异节

对等矩阵：P05、故事 56。

## 实现说明（2026-10-10）

票 07 只交付了监视**内核**（`internal/watch`：fsnotify 合并 300ms、sha256 digest、
250ms×2ⁿ 退避），票面里"重载保留视口、清撤销、脏文档询问"两项当时挂着没接线——
`internal/bridge` 里连一个 `internal/watch` 的引用都没有。本票把应用层补上：

**接线（`internal/bridge/watch.go`）。** 每个打开了包（有路径）的标签挂一个
`watch.Watcher` + `watch.Coordinator`：打开、另存为、确认 PSD 导入之后都记一次基线
（`Remember`），关标签时退休（`Workspace.onTabClosed`），退出时全部停掉（`OnShutdown`）。
协调器报出稳定摘要后：**读盘 → 判脏 → 要么原地重载，要么先问用户**。

- **原位重载**（`Workspace.ReloadDocument`）：标签 id/名字/位置不变，撤销栈清空（等同重新
  打开文件），脏标记归零，**活动标签不变**（后台标签的文件被改不会把用户拽走）。
  rev 从旧值 +1 继续：前端的画布缓存键是 `(docId, rev)`，而代理按契约保留 `documentID`，
  rev 不前进的话"刚打开还没编辑过"的标签会一直显示旧像素。
- **视口与选区**：状态在前端，重载**根本不碰** `viewport`/`currentSelection` —— 这就是
  "保留缩放/平移/选区"的实现方式（有前端单测逐字段断言）。
- **脏文档**：绝不静默覆盖，推 `external:changed`（`pending:true`）让前端弹"载入磁盘版本 /
  保留我的修改"，答案经 `ResolveExternalChange(tabId, accept)` 回到 Go。同一份磁盘内容不
  重复追问，新的外部改动会再问一次。
- **坏包静默忽略**：写了一半、缺图、带 BOM 的包读不出来就**不动画面**，等下一次变更
  （coordinator 已把摘要推进，所以代理改好之后会重新报）。

**重载与编辑的互斥。** 有未保存修改时先问（Swift 语义）；**有编辑正在飞**（笔刷拖着、
滤镜对话框开着）时也走询问 —— 重载会把整个会话换掉，笔画/滤镜会落在一个已经没人看的文档上。

**并发写同一个文件（实测补丁）。** `internal/fsatomic` 原来把临时文件固定命名为
`<path>.tmp`：两个写者交错时（A 写完 tmp、B 覆盖同一个 tmp、A 先改名成功、B 改名时 tmp 已不在）
会**静默丢掉一次提交**。改成每次调用取唯一临时名后，"各自成功、最后一个是赢家、内容永远是
完整的一版"；有写者失败也会拿到可见错误。8 个并发写者的测试固化了这一点。

**Windows 文件锁（实测）。** 本机探针发现：目标文件被**任何**进程打开着（哪怕只是只读）
时，`os.Rename` 返回 `Access is denied`、`os.Remove` 返回 `being used by another process`；
目标存在但没被打开时 rename **直接替换成功**（MoveFileEx 语义）。于是：

- 新增 `internal/fsatomic`：同目录临时文件 + **一次 rename 覆盖** + 瞬态锁短暂重试
  （4 次，25/50/100ms），失败时旧文件原样保留。实测结论固化成测试。
- `internal/project/store.go` 与 `internal/bridge/recent.go` 原来的"**先删旧文件再改名**"
  在这个语义下会在"删成功、改名失败"时**丢掉 manifest/最近列表**，且注释里"Windows 的
  `os.Rename` 不能覆盖已有文件"的断言是错的 —— 两处都改成一次改名，注释一并纠正。

**文档与脚本。** `docs/writing-comp-files.md`（Windows 版契约，含"Windows 差异"与"看不到
更新时怎么排查"）、`docs/acceptance/agent-contract.md`（验收记录页）、
`scripts/agentdemo`（官方示例脚本：PNG 先写 + 清单一次 rename；复用 documentID 与图层 id；
同 seed 逐字节可复现；产物用真实 `project.Store.Open` 验证）。

## 测试

- `internal/bridge/watch_test.go`（9 个端到端，用真实 `project.Store` 扮演代理）：
  原位重载（标签不变、撤销清空、rev 前进）、顺序破坏被忽略然后修复可见、同字节大小替换
  看不到更新、脏文档询问两条路、**编辑在飞（笔刷）时先问不静默换会话**、连写合并成一次、
  后台标签不抢焦点且不带文档信封、关标签退休、坏包忽略。
- `internal/fsatomic`（4 个）：创建/覆盖、被锁时保旧内容并报错（Windows 语义）、重试次数、
  **8 个写者并发提交同一个文件不互相污染**（唯一临时名；固定名时本机实测会丢一次提交）。
- `frontend/src/lib/state/external.test.ts`（9 个）：干净文档贴快照且**视口/选区逐字段不变**、
  后台标签的文档不贴到当前画布、脏文档只记待决定、两条回答路径、坏负载忽略、事件订阅。
- `scripts/agentdemo`（14 个）：首次建包可加载、重跑保身份、-steps 累积、写入顺序下旧清单
  始终可读、参数校验、可复现、保留外来图层、清理过期资产。
- `frontend/src/lib/state/external.test.ts`（8 个）：干净文档贴快照且**视口/选区逐字段不变**、
  脏文档只记待决定、两条回答路径、坏负载忽略、事件订阅。

## 实测时序（诚实记录）

从外部写入到重载完成 **569ms**（合并窗口 300ms + coordinator 首次比较前的一次退避 ~250ms）。
票面写的"~300ms"是合并窗口；应用里看到的更新延迟是 0.5–0.6s。

## 一个刻意的取舍（记录备查）

`external:changed` 每次都带**整份文档信封**（不是只推一个 rev）：前端因此不用再往返一次就能
直接刷新画布，代价是事件负载更大。频次由 coordinator 兜住（300ms 合并 + 每份稳定摘要只报一次），
所以不会刷屏；真出现"代理高频写"的极端场景，再考虑只推 rev + 前端按需拉取。

## 已知差异 / 未验证

1. **手工验收未做**：`docs/acceptance/agent-contract.md` 的 B 节 10 步要人看着画布跑，
   本轮只做了自动化那半（记录页里留空）。
2. **OneDrive/云占位符**：只有云端、未下载的占位文件读取会失败（依赖 provider），本机没有
   OneDrive 目录，未实测；"读失败当作这次不重载"的行为由上面的坏包路径覆盖，但真机未验。
3. **长路径（>260 字符）**：未实测，文档里不断言。
4. **真实杀毒软件/索引器**：重试策略（4 次、总长 ~175ms）在真机第三方杀毒占用下未验证。
5. **"视口保留"的端到端视觉验证**：store 层有单测，肉眼那一步在手工清单里。
6. **与 macOS 原版的互开**：示例脚本的产物只过了本仓库加载器，没有用原版打开过
   （票 05 的字节级对齐仍有 TODO）。
7. **BOM 包被拒**：实测 Go 的 `encoding/json` 遇到 BOM 报 `invalid character 'ï'`，整包被拒；
   这是"静默忽略"路径，用户看不到任何提示（与原版一致：坏包就是不动）。
