package bridge

// 自动更新（票 50）：检查 feed、下载并校验 Ed25519 签名的安装器、启动静默升级。
// 语义与 appcast/Sparkle 对齐：feed 在 GitHub Releases 上，安装器带签名，只有校验
// 通过的包才会被交给用户。（差量安装器不实现，见票面"已知差异"。）

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"compositor-win/internal/update"
)

// launchUpdateInstaller 是"启动静默升级"的接缝（测试替换，免得真去起一个安装器）。
var launchUpdateInstaller = update.LaunchInstaller

// BuildVersion 是发行版本号：发布流水线用
// `-ldflags "-X compositor-win/internal/bridge.BuildVersion=0.2.0"` 注入，源码里是
// 开发版本（与 wails.json 的 productVersion 同源）。
var BuildVersion = "0.1.0"

// updateStatusReply 是"检查更新"的回复。状态而不是错误：网络不通、没配更新源都是
// 用户该看到的**状态**，不该在控制台刷错误，也不该弹"失败"。
type updateStatusReply struct {
	Status         string `json:"status"` // available | up-to-date | unconfigured | error
	CurrentVersion string `json:"currentVersion"`
	Version        string `json:"version,omitempty"`
	Notes          string `json:"notes,omitempty"`
	PublishedAt    string `json:"publishedAt,omitempty"`
	Size           int64  `json:"size,omitempty"`
	Error          string `json:"error,omitempty"`
}

// CheckForUpdates 检查更新：拉 feed 比版本。C09 的菜单项与启动时的静默检查都走它。
// 有新版本时把发行信息记在 session 里，供 DownloadUpdate 用。
func (s *Service) CheckForUpdates() (string, error) {
	current := Version()
	checker, err := update.NewChecker(current)
	if err != nil {
		return marshalUpdateStatus(updateStatusReply{
			Status: "unconfigured", CurrentVersion: current, Error: err.Error(),
		})
	}
	rel, err := checker.Available(s.backgroundCtx())
	switch {
	case err == nil:
		s.updateMu.Lock()
		s.pendingUpdate = rel
		s.updateFile = ""
		s.updateMu.Unlock()
		return marshalUpdateStatus(updateStatusReply{
			Status: "available", CurrentVersion: current, Version: rel.Version,
			Notes: rel.Notes, PublishedAt: rel.PublishedAt, Size: rel.Installer.Size,
		})
	case errors.Is(err, update.ErrUpToDate):
		return marshalUpdateStatus(updateStatusReply{Status: "up-to-date", CurrentVersion: current})
	default:
		return marshalUpdateStatus(updateStatusReply{
			Status: "error", CurrentVersion: current, Error: err.Error(),
		})
	}
}

// DownloadUpdate 下载并校验安装器（sha256 + Ed25519）。没有先检查过更新的话，这里
// 自己查一次，菜单直接点"下载"也能走通。进度用 update:progress 事件推给前端。
func (s *Service) DownloadUpdate() (string, error) {
	// 重入护栏：前端靠状态换按钮，但绑定调用可能在别的入口被并发触发（菜单/事件/脚本）。
	// 两次下载会往同一个目标文件写，护栏让第二次立刻拿到明确错误而不是白跑一遍。
	s.updateMu.Lock()
	if s.downloading {
		s.updateMu.Unlock()
		return "", fmt.Errorf("已经有一个更新包在下载中")
	}
	s.downloading = true
	rel := s.pendingUpdate
	s.updateMu.Unlock()
	defer func() {
		s.updateMu.Lock()
		s.downloading = false
		s.updateMu.Unlock()
	}()

	current := Version()
	checker, err := update.NewChecker(current)
	if err != nil {
		return "", err
	}
	if rel == nil {
		available, err := checker.Available(s.backgroundCtx())
		if err != nil {
			if errors.Is(err, update.ErrUpToDate) {
				return "", update.ErrUpToDate
			}
			return "", err
		}
		rel = available
		s.updateMu.Lock()
		s.pendingUpdate = rel
		s.updateMu.Unlock()
	}
	// 下载用一个**独立于界面生命周期**的 ctx：前端一刷新/关窗，Wails 的启动 ctx 就被
	// 取消，下载会被腰斩、UI 停在"下载中"。给它自己的超时即可（进度事件照推）。
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	// 只有真的有 Wails 运行时上下文时才推事件（测试/无头启动下 EventsEmit 没有接收方）。
	if runtimeCtx := s.runtimeCtx(); runtimeCtx != nil {
		checker.Progress = func(done, total int64) {
			runtime.EventsEmit(runtimeCtx, "update:progress", done, total)
		}
	}
	dir, err := updateDir()
	if err != nil {
		return "", err
	}
	path, err := checker.Download(ctx, *rel, dir)
	if err != nil {
		return "", err
	}
	s.updateMu.Lock()
	s.updateFile = path
	s.updateMu.Unlock()
	reply, err := json.Marshal(struct {
		Path    string `json:"path"`
		Version string `json:"version"`
		Size    int64  `json:"size"`
	}{Path: path, Version: rel.Version, Size: rel.Installer.Size})
	if err != nil {
		return "", fmt.Errorf("无法编码下载结果: %w", err)
	}
	return string(reply), nil
}

// ApplyUpdate 启动已校验的安装器并退出应用：安装脚本会先等应用退出再静默安装
// （NSIS 覆盖正在运行的 exe 会失败）。
func (s *Service) ApplyUpdate() (string, error) {
	s.updateMu.Lock()
	path := s.updateFile
	s.updateMu.Unlock()
	if path == "" {
		return "", fmt.Errorf("还没有下载好的更新包")
	}
	if err := launchUpdateInstaller(path); err != nil {
		return "", err
	}
	// 安装器起来之后应用就该走了；前端拿到这个回复后 UI 会自己关。
	if ctx := s.runtimeCtx(); ctx != nil {
		go runtime.Quit(ctx)
	}
	return `{"launched":true}`, nil
}

// UpdateInfo 给设置/关于界面用：当前版本 + 这个构建有没有更新源（不联网）。
func (s *Service) UpdateInfo() (string, error) {
	reply, err := json.Marshal(struct {
		CurrentVersion string `json:"currentVersion"`
		Configured     bool   `json:"configured"`
		FeedURL        string `json:"feedUrl,omitempty"`
	}{CurrentVersion: Version(), Configured: update.Configured(), FeedURL: update.FeedURL})
	if err != nil {
		return "", fmt.Errorf("无法编码更新信息: %w", err)
	}
	return string(reply), nil
}

func marshalUpdateStatus(status updateStatusReply) (string, error) {
	data, err := json.Marshal(status)
	if err != nil {
		return "", fmt.Errorf("无法编码更新状态: %w", err)
	}
	return string(data), nil
}

// backgroundCtx 给没有运行时上下文（测试、无头启动）的场景兜一个可取消的 ctx。
func (s *Service) backgroundCtx() context.Context {
	if ctx := s.runtimeCtx(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// updateDir 是下载安装器的地方（用户缓存目录；拿不到就退回系统临时目录）。
func updateDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return os.TempDir(), nil
	}
	return filepath.Join(base, "compositor-on-windows", "updates"), nil
}
