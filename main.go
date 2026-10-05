package main

import (
	"context"
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"compositor-win/internal/bridge"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	workspace := bridge.NewWorkspace()
	windowStore, err := bridge.DefaultWindowStore()
	if err != nil {
		log.Printf("窗口状态将不持久化: %v", err)
		windowStore = nil
	}

	bind := []interface{}{&bridge.Service{}, workspace}
	if windowStore != nil {
		bind = append(bind, windowStore)
	}

	err = wails.Run(&options.App{
		Title:            "Compositor",
		Width:            1180,
		Height:           780,
		MinWidth:         960,
		MinHeight:        640,
		AssetServer:      &assetserver.Options{Assets: assets},
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
