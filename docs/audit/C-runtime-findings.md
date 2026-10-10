# C. 运行时验证与修复（runtime-verify）

范围：只验证"真跑才会暴露"的问题——静态测试全绿（`go build`/`go test`/`svelte-check`/vitest）
不代表应用真的能跑。本文所有结论都带真实命令与输出，命令一律用 git bash 执行。

写入域：`internal/winchrome/`、`internal/update/`、`internal/watch/`、本文件。
跑测用的临时程序都在 `%TEMP%\winenum\`（仓库外），验证完已清理。

---

## 1. 已修复：每次启动都报"任务栏覆盖图标不可用: 找不到主窗口"

### 现象（可复现）

```
$ ./build/bin/Compositor.exe > /tmp/comp-run1.log 2>&1 &
$ cat /tmp/comp-run1.log
2026/10/11 00:03:13 [WebView2] Environment created successfully
内存压力 "moderate"：历史上限 50 步 / 128MB，丢弃 0 份渲染缓存
任务栏覆盖图标不可用: 找不到主窗口，无法设置任务栏覆盖图标      <-- 每次启动都有
LAUNCH_TO_READY_SECONDS: 0.624
```

### 根因：不是"窗口还没建好"，是"窗口建好了但还没显示"

用 `EnumWindows` 逐帧采样本进程的顶层窗口（临时程序 `%TEMP%\winenum\main.go`）：

```
t=+162ms  hwnd=0x430ba0 visible=false class="wailsWindow" title="Compositor" exstyle=0x50100 owner=0x0
t=+1069ms hwnd=0x430ba0 visible=true  class="wailsWindow" title="Compositor" exstyle=0x50100 owner=0x0
```

主窗口在 +162ms 就存在（标题、类名、无属主都对），但要到 +1069ms 才可见。原因在 Wails：
主窗口用 `WS_OVERLAPPEDWINDOW`（不含 `WS_VISIBLE`）创建，
`internal/frontend/desktop/windows/frontend.go` 的 `navigationCompleted` 回调里才
`win32.ShowWindow`（该文件 1000 行）。而前端在挂载时就同步窗口外壳：

- `frontend/src/App.svelte:100-106` 的 `$effect` 首轮就跑 `syncWindowChrome("", false)`；
- 这一刻早于 `navigationCompleted`，所以**每次启动都必然**落在"窗口已建好、尚未显示"的窗口期。

`internal/winchrome/chromebridge.cpp` 的 `find_main_window` 只认 `IsWindowVisible`，
于是把这一瞬判成"没有主窗口" → `CHROME_ERR_WINDOW` → bridge 打日志。**确定性竞态，不是偶发。**

### 修复（`internal/winchrome/`）

1. `chromebridge.cpp`：搜索改成两顺位——先"可见且标题非空"；没有再退到
   "标题非空、无属主、带 `WS_EX_APPWINDOW`"的隐藏窗口（`WS_EX_APPWINDOW` 用来把
   GDI+ 钩子窗口这类隐藏辅助窗口挡在外面）。
2. `chromebridge.cpp`：只有隐藏候选时返回新码 `CHROME_ERR_NOT_SHOWN`，**不调用**
   `SetOverlayIcon`：那一刻还没有任务栏按钮可画，MSDN 也不保证提前设置会被保留。
3. `chrome_windows.go`：`CHROME_ERR_NOT_SHOWN` → `nil`（不是失败，窗口显示后的下一次
   同步自然生效；前端在文档名/脏标记一变就会再同步一次）。
4. 顺带修掉一个真机实测的地雷：**`GetWindowTextLengthW` 对同进程窗口是跨线程
   `SendMessage`**。原来只在"可见"分支里调它没问题；我第一版把标题检查挪到最前面后，
   测试进程里对一个隐藏窗口调它直接把外壳闸门挂死：实测 `SetDirtyOverlay` 等满
   `shellCallTimeout`（2s）返回 `errShellTimeout`，而那次 C 调用**永远不会返回**
   （调用线程一直卡在 SendMessage 上）。现在隐藏分支只做不发送消息的检查
   （`GetWindow`/`GetWindowLongPtrW`）。
5. 回归测试 `internal/winchrome/chrome_windows_test.go`：
   - `TestSetDirtyOverlayFindsNotYetShownMainWindow`：造一个同形态的隐藏窗口
     （STATIC + 标题 + `WS_EX_APPWINDOW` + 无 `WS_VISIBLE`），断言 `SetDirtyOverlay(false)`
     返回 nil **且不超时**（>1s 即失败，守住"隐藏窗口不真调 SetOverlayIcon"）；
   - `TestSetDirtyOverlayWithoutAnyWindow`：没有候选窗口时仍然是 `errNoMainWindow`
     （守住"退到隐藏窗口"没退化成"随便抓一个窗口"）。

### 验证（重新构建后真跑）

```
$ go build -tags desktop,production -ldflags "-w -s -H windowsgui" -o build/bin/Compositor-rtverify.exe .
$ ./build/bin/Compositor-rtverify.exe > /tmp/comp-rt.log 2>&1 &
$ cat /tmp/comp-rt.log
2026/10/11 00:14:44 [WebView2] Environment created successfully
内存压力 "critical"：历史上限 10 步 / 64MB，丢弃 0 份渲染缓存
LAUNCH_TO_READY_SECONDS: 0.943          <-- 告警消失
```

同一时刻对照旧产物（仍带 bug）确实还在打那行：
`./build/bin/Compositor.exe` → `任务栏覆盖图标不可用: 找不到主窗口…`。

```
$ gofmt -l internal/winchrome/          # 空
$ go vet ./internal/winchrome/          # ok
$ go test -count=1 ./internal/winchrome/  # ok  compositor-win/internal/winchrome
$ go build ./...                        # ok
```

> ⚠️ **`build/bin/Compositor.exe` 还是旧产物**（23:29 构建，仍会打那行告警）。
> 我的验证用 `Compositor-rtverify.exe`（跑完已删）。**发布前需要重新构建一次正式产物。**

---

## 2. 发现未修（归属：`main.go` / Lead）：代理绕过是**死代码**，用户报的 502 并没有被修好

`main.go:28-40` 的 `init()` 设置 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--proxy-bypass-list=<-loopback>`，
注释说这是为了修"系统代理把 wails.localhost 拦成 502"。

**这个环境变量根本到不了 WebView2 浏览器进程。** 链路：

- go-webview2 的 `webviewloader` 会主动把它**覆盖**成"选项里传的值"：
  `webviewloader/env_create.go:169`、`webviewloader/native_module.go:160` → `os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", additionalBrowserArguments)`；
- Wails 从环境变量里读不到这个值：它调 `edge.NewChromium()`（无参，
  `internal/frontend/desktop/windows/frontend.go:164`），只在内部追加
  `--disable-features=…` / `--disable-gpu`（同文件 480-495）；
- Wails 的 `windows.Options`（`pkg/options/windows/windows.go`）**没有**暴露
  `edge.Chromium.AdditionalBrowserArgs` 这个字段（`go-webview2/pkg/edge/chromium.go:78` 有，但 Wails 没转出来）。

真机证据（启动时手动带上同一个环境变量，再看浏览器进程命令行）：

```
$ export WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS="--remote-debugging-port=9222"
$ ./build/bin/Compositor-rtverify.exe &
$ # msedgewebview2.exe（--webview-exe-name=Compositor-rtverify.exe）的命令行：
  --embedded-browser-webview=1 --webview-exe-name=Compositor-rtverify.exe
  --user-data-dir="...\Compositor-rtverify.exe\EBWebView" --noerrdialogs
  --embedded-browser-webview-dpi-awareness=1 --disable-features=msSmartScreenProtection
  --mojo-named-platform-channel-pipe=...
$ #   ↑ 有 Wails 自己加的 --disable-features，没有 --proxy-bypass-list，也没有 --remote-debugging-port
$ curl http://127.0.0.1:9222/json/version   # rc=7（没有监听）
```

顺带验证了注册表覆盖这条路也走不通（go-webview2 把环境变量置空正是为了压掉注册表覆盖）：

```
$ reg add "HKCU\Software\Policies\Microsoft\Edge\WebView2" /v AdditionalBrowserArguments /t REG_SZ /d "--remote-debugging-port=9222" /f
$ ./build/bin/Compositor-rtverify.exe &
$ netstat -ano | grep LISTENING | grep -c 9222   # 0
$ curl http://127.0.0.1:9222/json/version         # rc=7
$ reg delete "HKCU\Software\Policies\Microsoft\Edge" /f   # 已清理
```

**影响**：那段 init 是"看起来修好了、其实一行都没生效"的死代码；如果用户仍能复现 502，
照现在的代码改注释/改参数都不会有用。**建议**（三选一，需要 Lead 决策）：

1. 用 `go.mod` 的 `replace` 把 go-webview2 换成仓库内补丁版：把 `preventEnvAndRegistryOverrides`
   改成"环境变量里已有的参数追加而不是覆盖"（改动最小、可控，但要维护一个 fork）；
2. 升级到提供该选项的 Wails 版本（先确认上游确实加了 `AdditionalBrowserArgs` 之类的出口）；
3. 不依赖浏览器参数：把"502"当成网络层问题另解（例如让 Wails 的资源请求不经过代理），
   并把 `init()` 里那段注释改成诚实的"当前无效"。

不管选哪条，都建议补一个**运行时**断言（读 msedgewebview2.exe 命令行里有没有该开关），
否则这类"设置了但没生效"的问题静态检查永远发现不了。

---

## 3. 发现未修（归属：`frontend/src/` + `internal/bridge/`）：前端 JS 错误在正式版里**结构性不可见**

排查过所有可行通道，结论是现在**没有任何办法**看到 WebView2 里的 console/异常：

- Wails v2.16.0 与 go-webview2 v1.0.22 **都没有** console-message 支持
  （`grep -r "ConsoleMessage" ` 两个模块，0 命中）；
- CDP（`--remote-debugging-port`）走不通：环境变量被覆盖（见第 2 节），注册表覆盖也无效；
- 前端也**没有**全局兜底：`frontend/src/` 里只有一处组件内的 `onerror`（`CanvasSurface.svelte:694`），
  没有 `window.addEventListener("error"/"unhandledrejection")`。

也就是说：正式版里一个未捕获异常、一次 WebGPU 初始化失败、一次 `console.error`，
**只会表现为"界面没反应/画布空白"，不留任何日志**。当前能证明前端启动没崩的唯一信号是
`LAUNCH_TO_READY_SECONDS`（`bridge/launch.go:54`）——它出现了说明 Svelte 挂载完成、快照拿到了。

**建议**（两处都要改，分属两个写入域）：

- `internal/bridge/`（go-rpc）：加一个绑定，例如
  `func (s *Service) ReportFrontendError(message, stack string)`，打到 stdout 时带固定前缀
  （`FRONTEND_ERROR: …`），便于 `scripts/launchbench.ps1` 之类的启动脚本 grep；
- `frontend/src/`（Lead）：在入口注册
  `window.addEventListener("error", …)` 与 `window.addEventListener("unhandledrejection", …)`，
  转发给上面的绑定（失败就吞掉，绝不能因为上报再抛一次）。

---

## 4. 已用真机验证**正常**的链路（含证据）

| 链路 | 证据 |
| --- | --- |
| 冷启动 | 连续 8 次启动全部存活，`LAUNCH_TO_READY_SECONDS` 0.55–1.03s，无告警行 |
| 启动参数打开 `.comp` → 打开 → 记最近项目 | 用文档契约手写一个 v11 包（`%TEMP%\winenum\demo.comp`，8bit RGBA PNG + manifest），`Compositor-rtverify.exe <pkg>` 启动后 `%APPDATA%\compositor-on-windows\recent.json` 出现该路径 |
| 外部修改 → 热重载（watcher 真的在跑） | 删掉图层 PNG → `外部变更未载入（…）: 清单引用的资产 … 不存在`；manifest 写成非法 JSON → `manifest.json 无法解析: invalid character 't' …`；修好后再破坏仍然会报 → 监视器在"失败+成功"之后依然活着 |
| 单实例 | 第二个进程退出码 0、stdout 为空（参数交给既有实例，与 macOS 同窗口多标签一致） |
| 优雅退出 | `taskkill /IM`（= WM_CLOSE）后进程 3s 内退出，无残留进程（WebView2 子进程也收干净） |
| 内存压力档位 | 本机真实读数：`TotalPhys=15.18 GiB, AvailPhys=1.57 GiB, ratio=0.1032, MemoryLoad=89%` → `critical` 是**真读数**，启动即把历史上限压到 10 步属于设计行为，不是 bug |

---

## 5. 已知限制（与参考实现一致，**不建议**改）

- **打开中的包目录被整个删掉后，监视器不会自愈。** 复现：`rm -rf <pkg>` 之后再重建同名包、
  再改坏它，应用**一声不响**（没有任何 `外部变更…` 日志），即磁盘事件再也不进应用。
  根因在 `internal/watch/watcher.go:244-271`：re-arm 只在"有事件"时触发，且最多重试
  20×50ms，路径消失即放弃；目录被删后所有 watch 都没了，也就没有下一个事件。
  该注释明确写着这是对齐 Swift `ProjectWatcher` 的 20 次重试行为，所以按"复制原版行为"
  处理，不改。用户可见后果：包被删/被移走期间画布保持旧内容，直到重新打开文件。
- **不同构建共用同一个单实例锁**（`compositor-on-windows-single-instance`）。
  实测：`Compositor.exe` 在跑时再启动 `Compositor-rtverify.exe` → 后者立刻以 code 0
  静默退出、**stdout 一个字都没有**。对最终用户无害（只会有一个构建），但并行验证时
  很容易被误判成"应用启动即崩溃"。我因此踩过两次，特此记录：**看到"启动即静默退出、
  日志为空"先查有没有别的实例在跑。**

---

## 6. 残留风险 / 下一步建议

1. **重新构建正式产物**：`build/bin/Compositor.exe` 还是 23:29 的旧货，包含第 1 节的 bug。
2. **WebGPU 是否真的可用没人验证过**：前端渲染走 WebGPU，如果初始化失败只会表现为
   画布空白（见第 3 节）。加上前端错误上报后，值得专门跑一次"新建画布 → 画一笔 → 导出 PNG"
   的真机流程并确认 `renderBroken` 没被置位。本轮没有可用的自动化通道（CDP 被封死），
   只能在 UI 上人工点一遍。
3. **第 2 节的代理死代码**建议尽快决策，它直接影响"用户报的 bug 到底修没修"。
