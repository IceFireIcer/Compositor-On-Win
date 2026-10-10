package bridge

// Shell 集成（票 49）：把 .comp 从 Windows 外壳送进标签、最近项目菜单与跳转列表、
// 窗口标题与任务栏脏标记、以及单实例（第二次启动把文件交给既有实例）的转发。
//
// 与 macOS 原版的对应：
//   CompositorApplicationDelegate.application(_:open:)  外壳送文件 → ProjectWorkspace.receive
//   RecentProjects.swift                                 最近项目（NSDocumentController 的最近文档）
//   EditorSession 的窗口标题 / isDocumentEdited          窗口标题与脏标记
// Windows 上的落法：路径从 os.Args 或 Wails 的 OnSecondInstanceLaunch 进来，先在
// 队列里等前端就绪，再由前端调 OpenShellPaths 打开；外壳副作用（标题/覆盖图标/
// 跳转列表）走 shellPlane 接缝。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"compositor-win/internal/psd"
	"compositor-win/internal/winchrome"
)

// appName 是窗口标题里的应用名（与 main.go 的 options.App.Title 同一份拼写）。
const appName = "Compositor"

// shellPlane 是 Windows 外壳副作用的接缝：窗口标题走 Wails runtime，任务栏覆盖
// 图标与跳转列表走 internal/winchrome。测试换成内存假实现，免得动真窗口/任务栏。
type shellPlane interface {
	SetTitle(ctx context.Context, title string)
	SetDirtyOverlay(on bool) error
	NoteRecentDocument(path string) error
	ClearRecentDocuments() error
	// RaiseWindow 把既有窗口提到前台（第二次启动转发文件时用）。
	RaiseWindow(ctx context.Context)
	// EmitOpenPaths 推 "shell:openPaths" 事件，让前端去 OpenShellPaths。
	EmitOpenPaths(ctx context.Context, queued int)
	// Emit 推一条带 JSON 负载的事件（外部变更等）。
	Emit(ctx context.Context, name string, payload string)
}

// windowsShell 是真实实现。
type windowsShell struct{}

func (windowsShell) SetTitle(ctx context.Context, title string) { runtime.WindowSetTitle(ctx, title) }
func (windowsShell) SetDirtyOverlay(on bool) error              { return winchrome.SetDirtyOverlay(on) }
func (windowsShell) NoteRecentDocument(path string) error       { return winchrome.NoteJumpListPath(path) }
func (windowsShell) ClearRecentDocuments() error                { return winchrome.ClearJumpList() }

func (windowsShell) RaiseWindow(ctx context.Context) {
	runtime.WindowUnminimise(ctx)
	runtime.WindowShow(ctx)
}

func (windowsShell) EmitOpenPaths(ctx context.Context, queued int) {
	runtime.EventsEmit(ctx, "shell:openPaths", queued)
}

func (windowsShell) Emit(ctx context.Context, name string, payload string) {
	runtime.EventsEmit(ctx, name, payload)
}

// shell 是当前外壳接缝（测试替换）。
var shell shellPlane = windowsShell{}

// ShellHooks 是 main.go 用的外壳入口：装最近项目持久层、排入外壳递来的路径、
// 通知前端"又递了文件进来"。
//
// 它**故意不绑进 Wails**（main.go 只把 Service/Workspace/WindowStore 放进 Bind）：
// 这三个入口只有进程启动代码需要，前端需要的是 Service 上的
// RecentProjects/OpenPath/OpenShellPaths/SyncWindowChrome —— 放在这里，
// wailsjs 绑定里就不会多出 `EnableRecentFiles(bridge.RecentStore)` 这种
// 前端永远不该调的 API（顺手也不往 models.ts 里塞 RecentStore）。
type ShellHooks struct{ svc *Service }

// NewShellHooks 给一个服务装外壳入口。
func NewShellHooks(svc *Service) *ShellHooks { return &ShellHooks{svc: svc} }

// EnableRecentFiles 装上最近项目的持久层。测试不调用它，于是最近项目相关的方法
// 变成 no-op —— 测试套件不会去写用户的配置目录。
func (h *ShellHooks) EnableRecentFiles(store *RecentStore) { h.svc.recent = store }

// Queue 记下外壳递来的路径（启动参数，或第二次启动转发过来的参数）。
func (h *ShellHooks) Queue(paths []string) { h.svc.queueShellPaths(paths) }

// Notify 告诉前端外壳又递了文件进来，并把窗口提到前台。
func (h *ShellHooks) Notify() { h.svc.notifyShellPaths() }

// Shutdown 收尾：停掉所有外部变更监视器（Wails 的 OnShutdown 调它）。
func (h *ShellHooks) Shutdown() { h.svc.stopWatchingProjects() }

// shellPathsReply 是"一串外壳路径"的回复（最近项目列表、清空后的列表）。
type shellPathsReply struct {
	Paths []string `json:"paths"`
}

func marshalShellPaths(paths []string) (string, error) {
	if paths == nil {
		paths = []string{}
	}
	b, err := json.Marshal(shellPathsReply{Paths: paths})
	if err != nil {
		return "", fmt.Errorf("无法编码路径列表: %w", err)
	}
	return string(b), nil
}

// RecentProjects 列最近项目（文件 → 打开最近的子菜单）。已经移动/删除的项目
// 不出现在列表里（RecentProjects.refresh 的过滤在 RecentStore.List 里）。
func (s *Service) RecentProjects() (string, error) {
	return marshalShellPaths(s.recentPaths())
}

// ClearRecentProjects 清空最近项目（菜单的"清除菜单"）：内存列表与 Windows 外壳
// 的最近文档一起清。
func (s *Service) ClearRecentProjects() (string, error) {
	if s.recent != nil {
		if err := s.recent.Clear(); err != nil {
			return "", err
		}
	}
	if err := shell.ClearRecentDocuments(); err != nil {
		// 外壳清了不代表应用列表清了；反过来也一样。失败只记一行。
		fmt.Printf("清除跳转列表失败: %v\n", err)
	}
	return marshalShellPaths(nil)
}

// recentPaths 读最近项目列表；没装持久层（测试）时是空列表。
func (s *Service) recentPaths() []string {
	if s.recent == nil {
		return []string{}
	}
	paths, err := s.recent.List()
	if err != nil {
		fmt.Printf("读取最近项目失败: %v\n", err)
		return []string{}
	}
	return paths
}

// noteRecent 把一个成功打开/保存的项目记进最近列表与外壳的最近文档。
func (s *Service) noteRecent(path string) {
	if s.recent != nil {
		if err := s.recent.Add(path); err != nil {
			fmt.Printf("记录最近项目失败: %v\n", err)
		}
	}
	if err := shell.NoteRecentDocument(path); err != nil {
		fmt.Printf("外壳最近文档不可用: %v\n", err)
	}
}

// OpenPath 打开一个路径（最近项目菜单、或外壳递进来的文件）：.comp 直接进标签，
// .psd/.psb 解析后停在待确认槽等转换报告。回复与 OpenProjectDialog 同形。
func (s *Service) OpenPath(path string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("路径为空")
	}
	return s.openPathReply(path)
}

// shellPathError 是一个外壳递给我们的、打不开的文件。
type shellPathError struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// openShellReply 是 OpenShellPaths 的回复：外壳一次可能递来好几个文件，有的进
// 标签、有的要转换报告、有的读不了，一起报给前端。
type openShellReply struct {
	Snapshot    Snapshot         `json:"snapshot"`
	Opened      []string         `json:"opened"`
	Failed      []shellPathError `json:"failed"`
	Pending     bool             `json:"pending,omitempty"`
	Conversions []psd.Conversion `json:"conversions,omitempty"`
}

// queueShellPaths 记下外壳递来的路径（启动参数，或第二次启动转发过来的参数），
// 等前端就绪后由 OpenShellPaths 打开。空路径被丢掉。未导出：入口在 ShellHooks。
func (s *Service) queueShellPaths(paths []string) {
	queued := make([]string, 0, len(paths))
	for _, path := range paths {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			queued = append(queued, trimmed)
		}
	}
	if len(queued) == 0 {
		return
	}
	s.shellMu.Lock()
	s.shellPaths = append(s.shellPaths, queued...)
	s.shellMu.Unlock()
}

// takeShellPaths 排空队列。
func (s *Service) takeShellPaths() []string {
	s.shellMu.Lock()
	defer s.shellMu.Unlock()
	paths := s.shellPaths
	s.shellPaths = nil
	if paths == nil {
		paths = []string{}
	}
	return paths
}

// notifyShellPaths 通知前端"外壳又递了文件进来"（第二次启动的单实例转发），并把
// 既有窗口提到前台 —— 第二次启动的用户以为会看到新窗口，实际该看到既有窗口。
func (s *Service) notifyShellPaths() {
	ctx := s.runtimeCtx()
	if ctx == nil {
		return
	}
	shell.RaiseWindow(ctx)
	s.shellMu.Lock()
	queued := len(s.shellPaths)
	s.shellMu.Unlock()
	shell.EmitOpenPaths(ctx, queued)
}

// OpenShellPaths 打开外壳排队的每个路径并返回聚合结果。前端在启动时与收到
// "shell:openPaths" 事件时调用它。
//
// 转换报告只有一个待确认槽（pendingPSD）：队列里的第一个 Photoshop 文件停下等
// 确认，后面的 Photoshop 文件报"已有待确认的导入"，其余 .comp 照常打开。
func (s *Service) OpenShellPaths() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	paths := s.takeShellPaths()
	reply := openShellReply{Opened: []string{}, Failed: []shellPathError{}}
	// 待确认槽可能已经有一个（上一次打开留下的）：把它一起报给前端，并且不再
	// 让新的 Photoshop 文件挤掉它。
	if reply.Pending = s.hasPendingPSD(); reply.Pending {
		reply.Conversions = s.pendingConversions()
	}
	for _, path := range paths {
		if reply.Pending && isPhotoshopPath(path) {
			reply.Failed = append(reply.Failed, shellPathError{
				Path: path, Error: "已有待确认的 Photoshop 导入，请先处理转换报告",
			})
			continue
		}
		pending, err := s.openPath(path)
		if err != nil {
			reply.Failed = append(reply.Failed, shellPathError{Path: path, Error: err.Error()})
			continue
		}
		if pending {
			reply.Pending = true
			reply.Conversions = s.pendingConversions()
			continue
		}
		reply.Opened = append(reply.Opened, path)
	}
	// 打开完后取一次快照：可能开了新标签；没有路径时它仍是当前状态。
	reply.Snapshot = s.ws.Snapshot()
	b, err := json.Marshal(reply)
	if err != nil {
		return "", fmt.Errorf("无法编码外壳打开结果: %w", err)
	}
	return string(b), nil
}

// SyncWindowChrome 把活动文档的名字与脏标记同步到窗口标题与任务栏覆盖图标
// （原版：窗口标题跟文档名走，isDocumentEdited 画未保存标记）。前端在活动标签的
// 名字/脏标记变化时调用。
func (s *Service) SyncWindowChrome(name string, dirty bool) (string, error) {
	title := windowTitle(name, dirty)
	if s.runtimeCtx() != nil {
		shell.SetTitle(s.runtimeCtx(), title)
	}
	if err := shell.SetDirtyOverlay(dirty); err != nil {
		// 覆盖图标是锦上添花：没有任务栏按钮（测试、远程会话）不该打扰用户。
		fmt.Printf("任务栏覆盖图标不可用: %v\n", err)
	}
	b, err := json.Marshal(struct {
		Title string `json:"title"`
	}{Title: title})
	if err != nil {
		return "", fmt.Errorf("无法编码窗口标题: %w", err)
	}
	return string(b), nil
}

// windowTitle 拼窗口标题：有文档时"名字 - Compositor"，未保存加星号；没有文档
// 时只有应用名（macOS 版的标题跟着文档名，未保存点由系统画）。
func windowTitle(name string, dirty bool) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return appName
	}
	if dirty {
		trimmed += "*"
	}
	return trimmed + " - " + appName
}
