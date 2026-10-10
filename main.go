package main

import (
	"context"
	"embed"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"compositor-win/internal/bridge"
)

//go:embed all:frontend/dist
var assets embed.FS

// init runs before the WebView2 environment is created. System proxies
// (Clash/v2ray etc.) answer 502 for wails.localhost: their bypass lists
// cover localhost/127.* but not the wails.localhost virtual host. Force
// WebView2 to bypass any proxy for loopback traffic (user-reported
// HTTP ERROR 502). Existing additional arguments are preserved.
func init() {
	const env = "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"
	const bypass = "--proxy-bypass-list=<-loopback>"
	existing := os.Getenv(env)
	if strings.Contains(existing, bypass) {
		return
	}
	if existing == "" {
		_ = os.Setenv(env, bypass)
		return
	}
	_ = os.Setenv(env, existing+" "+bypass)
}

func main() {
	workspace := bridge.NewWorkspace()
	service := bridge.NewService(workspace)
	windowStore, err := bridge.DefaultWindowStore()
	if err != nil {
		log.Printf("窗口状态将不持久化: %v", err)
		windowStore = nil
	}
	// 外壳入口（票 49）：最近项目持久层 + 启动参数里的文件。这两件事只有进程
	// 启动代码需要，所以走不绑进 Wails 的 ShellHooks（绑进去会让 wailsjs 多出
	// 前端永远不该调的 API）。
	shellHooks := bridge.NewShellHooks(service)
	// 最近项目与 Windows 外壳的最近文档共用一份列表；装不上就退化成空列表，
	// 不影响启动。
	if recent, err := bridge.DefaultRecentStore(); err != nil {
		log.Printf("最近项目将不持久化: %v", err)
	} else {
		shellHooks.EnableRecentFiles(recent)
	}
	// 外壳在启动参数里递来的文件：双击 .comp（文件关联）、把文件拖到图标上、
	// 或命令行启动。前端就绪后调 OpenShellPaths 打开它们。开关（- 开头）不算文件。
	shellHooks.Queue(resolveLaunchedPaths(os.Args[1:]))

	bind := []interface{}{service, workspace}
	if windowStore != nil {
		bind = append(bind, windowStore)
	}

	err = wails.Run(&options.App{
		Title:            "Compositor",
		Width:            1180,
		Height:           780,
		MinWidth:         960,
		MinHeight:        640,
		AssetServer:      &assetserver.Options{Assets: assets, Handler: service.RenderHandler()},
		BackgroundColour: &options.RGBA{R: 30, G: 30, B: 32, A: 255},
		OnStartup: func(ctx context.Context) {
			if windowStore != nil {
				if state, err := windowStore.Load(); err == nil && state.Width > 0 && state.Height > 0 {
					runtime.WindowSetSize(ctx, state.Width, state.Height)
				}
			}
		},
		// 单实例（票 49）：第二次启动不再开一个进程，而是把参数交给既有实例，
		// 由它打开那些文件并把窗口提到前台 —— 与 macOS 版"同一窗口多标签"一致。
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "compositor-on-windows-single-instance",
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				shellHooks.Queue(resolveLaunchedPaths(data.Args))
				shellHooks.Notify()
			},
		},
		Bind: bind,
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			Theme:                windows.Dark,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// resolveLaunchedPaths 把外壳递来的参数整理成绝对路径：丢掉启动开关（`-` 开头），
// 相对路径补成绝对路径。文件关联给的一律是绝对路径，所以这条路主要服务命令行。
//
// 注意：Wails 的 SecondInstanceData.WorkingDirectory 是**可执行文件所在目录**，不是
// 第二实例的工作目录，所以这里不用它；第二实例的相对路径没有可靠来源，用本进程的
// 工作目录是最近的近似（两者不同时只能由用户给绝对路径，属于已知限制）。
func resolveLaunchedPaths(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue // 启动开关不是文件
		}
		if filepath.IsAbs(arg) {
			out = append(out, arg)
			continue
		}
		if abs, err := filepath.Abs(arg); err == nil {
			out = append(out, abs)
			continue
		}
		out = append(out, arg)
	}
	return out
}
