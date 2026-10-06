package main

import (
	"context"
	"embed"
	"log"
	"os"
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
