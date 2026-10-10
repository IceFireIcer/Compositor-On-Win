// Package bridge hosts the Wails service bindings the frontend calls.
// It is the thin transport layer: document state lives in the Workspace's
// per-tab sessions (internal/domain model + history + in-memory bitmaps),
// package IO in project.Store, pixels in render. (Ticket 01 started with
// the version round-trip only; the M2 wiring mounted the full document
// contract onto Service.)
package bridge

import (
	"context"
	"sync"
	"sync/atomic"

	"compositor-win/internal/project"
	"compositor-win/internal/render"
	"compositor-win/internal/update"
)

// Version reports the application version to the frontend status bar and the
// update checker. BuildVersion is injected by the release pipeline
// (-ldflags -X compositor-win/internal/bridge.BuildVersion=…); the source value
// is the development version.
func Version() string {
	return BuildVersion
}

// Service is the object bound into the Wails runtime (see main.go Bind).
// The Wails context arrives via Startup and backs the file dialogs; the
// dialog functions are fields so tests can stub them.
type Service struct {
	ws    *Workspace
	store project.Store

	// ctx 是 Wails 运行时上下文，Startup 写一次、之后到处读。它被放在 atomic
	// 指针里：第二次启动的回调（OnSecondInstanceLaunch）在别的 goroutine 上读它，
	// 而 Startup 的写在窗口创建时 —— 普通字段在 -race 下会报竞争。
	ctx atomic.Pointer[context.Context]

	openDialog func(ctx context.Context) (string, error)
	saveDialog func(ctx context.Context) (string, error)

	// Image import batch (ticket 40): files decoded by BeginImageImport
	// wait here for the frontend's SVG rasters before the commit lands.
	importMu      sync.Mutex
	pendingImport []pendingImportFile

	// Export preview (ticket 42): one flattened composite held between
	// quality-slider moves; the dialog's preview re-encodes from it.
	exportMu      sync.Mutex
	exportRaster  *render.Bitmap
	exportPreview *render.Bitmap
	exportStage   []string

	// Staged bitmaps for the HTTP pixel plane (architecture §3.3).
	pixelMu    sync.Mutex
	pixelStage map[string]stagedPixel

	// RAW develop sheet (ticket 41): the open half-size LibRaw session.
	rawMu      sync.Mutex
	rawSession *rawDevelopSession

	// PSD conversion confirmation (ticket 39): a Photoshop file with
	// conversion notes waits here until the user confirms or cancels.
	psdMu      sync.Mutex
	pendingPSD *pendingPSDOpen

	// Shell integration (ticket 49): the recent-projects list (nil until
	// main.go installs it, so tests never touch the user's config dir) and the
	// paths the shell handed over (argv, or a second instance's arguments),
	// waiting for the frontend to open them.
	recent     *RecentStore
	shellMu    sync.Mutex
	shellPaths []string

	// Auto-update (ticket 50): the release CheckForUpdates found and the
	// installer DownloadUpdate verified and saved.
	updateMu      sync.Mutex
	pendingUpdate *update.Release
	updateFile    string
	downloading   bool

	// External changes (tickets 07 + 51): one watcher per open package, keyed by
	// tab id. The coordinator reports a settled digest; the reload path reads the
	// package, decides whether the tab is dirty, and either swaps the document in
	// place or asks the user first.
	watchMu sync.Mutex
	watched map[string]*watchedProject
}

// NewService wires the service onto a workspace (shared with the
// workspace binding in main.go).
func NewService(ws *Workspace) *Service {
	service := &Service{
		ws:         ws,
		openDialog: defaultOpenDialog,
		saveDialog: defaultSaveDialog,
		watched:    map[string]*watchedProject{},
	}
	if ws != nil {
		// Closing a tab retires its external-change watcher; the reload path also
		// re-checks, so a missed hook can never reload into a dead tab.
		ws.onTabClosed = service.unwatchProject
	}
	return service
}

// Startup is the Wails lifecycle hook: captures the runtime context the
// dialogs need.
func (s *Service) Startup(ctx context.Context) {
	s.ctx.Store(&ctx)
}

// runtimeCtx 读回运行时上下文（Startup 之前是 nil）。
func (s *Service) runtimeCtx() context.Context {
	if p := s.ctx.Load(); p != nil {
		return *p
	}
	return nil
}

// Version exposes the app version as a bound method for the frontend.
func (s *Service) Version() string {
	return Version()
}
