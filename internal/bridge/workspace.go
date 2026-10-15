package bridge

import (
	"bytes"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"compositor-win/internal/domain"
	"compositor-win/internal/project"
	"compositor-win/internal/render"
)

// Document is the shell-stage document descriptor: just enough for the
// tabs, the checkerboard and the dirty marker. Since the M2 wiring each
// tab is backed by a real domain.Document + history session (see session);
// ID/Name/Dirty semantics carry over unchanged.
//
// ID invariant: a tab's ID **is** its session document's domain.DocumentID.
// The frontend matches the render URL and the document snapshot on this value
// (App.svelte's loadDocument(expectedDocId), CanvasSurface's renderSrc guard),
// so the two must never be generated independently.
type Document struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Resolution int    `json:"resolution"`
	Dirty      bool   `json:"dirty"`
}

// Snapshot is the full workspace state handed to the frontend after every
// mutation — commands return state instead of diffing, so the store stays a
// dumb mirror.
type Snapshot struct {
	Tabs     []Document `json:"tabs"`
	ActiveID string     `json:"activeId"`
}

// Workspace holds the open document tabs (single window, in-app tabs like
// the macOS ProjectWorkspace). Every tab owns a session: the real domain
// document, its undo history and its in-memory bitmap library. One mutex
// guards tabs + sessions; all rendering and editing funnels through it.
type Workspace struct {
	mu       sync.Mutex
	saveMu   sync.Mutex // serialize package writers without blocking document edits
	tabs     []Document
	active   string
	counter  int
	sessions map[string]*session
	// saveReservations closes the gap between Save-As preflight and the final
	// atomic rename: an open or another save cannot claim a path while its
	// package is being encoded/written.
	saveReservations map[string]string

	// savePackage is the filesystem boundary, replaceable by tests that pause
	// an actual package write. nil uses project.Store.
	savePackage func(path string, doc *domain.Document, assets map[string][]byte) error

	// onTabClosed lets the service retire per-tab resources (the external-change
	// watcher). Set once by NewService; called with the workspace lock held, so it
	// must not call back into the workspace.
	onTabClosed func(id string)
}

// NewWorkspace starts with no documents.
func NewWorkspace() *Workspace {
	return &Workspace{
		sessions:         map[string]*session{},
		saveReservations: map[string]string{},
	}
}

// NewDocument validates the request and appends a tab backed by a real
// document. background is "" / "transparent" (a blank 图层 1), "white" or
// "black" (an opaque Background layer filled with that color); anything else is
// a request error. Name is 未命名, then 未命名 2, 未命名 3, …
//
// 这是新建画布唯一的公开入口（前端 wailsjs 的 Workspace.NewDocument 与它一一
// 对应）。票 57 之前那个建白底「背景」的 3 参入口只剩测试在用，已挪进
// workspace_test.go 的 newDocumentFixture —— 生产 RPC 面不留测试专用方法。
func (w *Workspace) NewDocument(width, height, resolution int, background string) (Snapshot, error) {
	if err := domain.ValidateNewDocument(width, height, resolution); err != nil {
		return Snapshot{}, err
	}
	bg, err := parseCanvasBackground(background)
	if err != nil {
		return Snapshot{}, err
	}
	doc, bitmaps := newDocumentModelWithBackground(width, height, resolution, bg)
	layerSeq := 0
	if bg == canvasBackgroundTransparent {
		// 透明底的第一层占掉 图层 1（原版按"下一个没被用过的 Layer N"取名），
		// 随后新建的图层因此是 图层 2 而不是重名。
		layerSeq = 1
	}
	return w.appendDocument(width, height, resolution, doc, bitmaps, layerSeq)
}

// newDocumentWithWhiteBackground is the pre-ticket-57 shape: a document whose
// first layer is the opaque white 背景. The bridge tests that predate ticket 57
// pin that fixture, so it stays for them; it is unexported on purpose, keeping
// the Wails binding surface to the one create entry the frontend actually calls.
func (w *Workspace) newDocumentWithWhiteBackground(width, height, resolution int) (Snapshot, error) {
	if err := domain.ValidateNewDocument(width, height, resolution); err != nil {
		return Snapshot{}, err
	}
	doc, bitmaps := newDocumentModel(width, height, resolution)
	return w.appendDocument(width, height, resolution, doc, bitmaps, 0)
}

// appendDocument is the path every create entry shares: number the tab, wrap
// the model in a session and activate it. layerSeq seeds the session's 图层 N
// counter when the first layer already took a number.
func (w *Workspace) appendDocument(width, height, resolution int, doc *domain.Document, bitmaps map[string]*render.Bitmap, layerSeq int) (Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.counter++
	name := "未命名"
	if w.counter > 1 {
		name = fmt.Sprintf("未命名 %d", w.counter)
	}
	tab := Document{
		// 标签 ID **就是**文档 ID。前端靠它把 /render/{id}.png 与当前文档对上
		// （App.svelte 的 loadDocument(expectedDocId) 与 CanvasSurface 的
		// renderSrc 守卫都按文档 ID 比较）。曾经两者各生成一个 ID，导致守卫
		// 永远不成立、画布永远不显示合成结果。
		ID:         doc.DocumentID,
		Name:       name,
		Width:      width,
		Height:     height,
		Resolution: resolution,
		Dirty:      true,
	}
	w.tabs = append(w.tabs, tab)
	sess := newSession("", doc, bitmaps)
	sess.layerSeq = layerSeq
	w.sessions[tab.ID] = sess
	w.active = tab.ID
	return w.snapshotLocked(), nil
}

// OpenDocument installs a parsed package as a tab: a fresh session (empty
// history, rev 0) around the given document and its decoded bitmaps. A path
// that already has a tab activates that live session instead of replacing it;
// external disk reloads go through ReloadDocument so an explicit open cannot
// discard unsaved edits or undo history. The tab name comes from the package
// directory.
func (w *Workspace) OpenDocument(path string, doc *domain.Document, bitmaps map[string]*render.Bitmap) (Snapshot, error) {
	if doc == nil {
		return Snapshot{}, fmt.Errorf("文档为空，无法打开")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	name := displayName(path)
	for i := range w.tabs {
		t := &w.tabs[i]
		if sess := w.sessions[t.ID]; sess != nil && sameDocumentPath(sess.path, path) {
			w.active = t.ID
			return w.snapshotLocked(), nil
		}
	}
	// A copied .comp retains its manifest documentID. Tabs, document sessions,
	// and render URLs all use that ID, so installing it as another tab would
	// overwrite the first session in the map and lose its working state.
	if _, exists := w.sessions[doc.DocumentID]; exists {
		return Snapshot{}, fmt.Errorf("%w: %s", errDocumentIDConflict, doc.DocumentID)
	}
	if ownerID, ok := w.reservedPathOwnerLocked(path); ok {
		return Snapshot{}, fmt.Errorf("PATH_CONFLICT: 项目路径正在保存，暂时不能打开: %s (标签 %s)", path, ownerID)
	}
	res := domain.DefaultResolution
	if doc.Resolution != nil {
		res = *doc.Resolution
	}
	tab := Document{
		// 见 NewDocument：标签 ID 与文档 ID 必须一致（.comp 的 documentID 在
		// 同一文件上稳定，所以重复打开同一路径仍然对得上）。
		ID:         doc.DocumentID,
		Name:       name,
		Width:      doc.Width,
		Height:     doc.Height,
		Resolution: res,
		Dirty:      false,
	}
	w.tabs = append(w.tabs, tab)
	w.sessions[tab.ID] = newSession(path, doc, bitmaps)
	w.active = tab.ID
	return w.snapshotLocked(), nil
}

// SelectTab activates an existing tab.
func (w *Workspace) SelectTab(id string) (Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.hasLocked(id) {
		return Snapshot{}, fmt.Errorf("标签不存在: %s", id)
	}
	w.active = id
	return w.snapshotLocked(), nil
}

// CloseTab removes a tab and its session. Closing the active tab activates
// its next neighbor (or the previous one at the end); closing the last tab
// leaves the workspace empty and returns to the welcome screen.
func (w *Workspace) CloseTab(id string) (Snapshot, error) {
	return w.closeTab(id, true)
}

func (w *Workspace) closeTab(id string, discard bool) (Snapshot, error) {
	w.mu.Lock()
	index := -1
	for i, d := range w.tabs {
		if d.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		w.mu.Unlock()
		return Snapshot{}, fmt.Errorf("标签不存在: %s", id)
	}
	if !discard {
		sess := w.sessions[id]
		if w.tabs[index].Dirty || (sess != nil && (sess.stroke != nil || sess.filter != nil || sess.heal != nil || sess.saving)) {
			w.mu.Unlock()
			return Snapshot{}, fmt.Errorf("DOCUMENT_DIRTY: 文档有未保存的更改，需要确认后关闭")
		}
	}
	w.tabs = append(w.tabs[:index], w.tabs[index+1:]...)
	delete(w.sessions, id)
	switch {
	case w.active != id:
		// keep current selection
	case len(w.tabs) == 0:
		w.active = ""
	case index < len(w.tabs):
		w.active = w.tabs[index].ID
	default:
		w.active = w.tabs[len(w.tabs)-1].ID
	}
	snapshot := w.snapshotLocked()
	hook := w.onTabClosed
	w.mu.Unlock()

	// Called outside the lock: it stops a watcher/coordinator (the external-change
	// resource), and holding the workspace lock across that would block every
	// other document operation for no reason.
	if hook != nil {
		hook(id)
	}
	return snapshot, nil
}

// TryCloseTab is the automatic close path; CloseTab is an explicit discard.
func (w *Workspace) TryCloseTab(id string) (Snapshot, error) {
	return w.closeTab(id, false)
}

// ReloadDocument replaces the content of the tab that holds path with a freshly
// read package, but refuses to discard local edits. Callers that have already
// obtained an explicit discard decision use reloadDocument with force=true.
func (w *Workspace) ReloadDocument(path string, doc *domain.Document, bitmaps map[string]*render.Bitmap) (Snapshot, string, bool, error) {
	return w.reloadDocument(path, doc, bitmaps, false)
}

// reloadDocument replaces the content of the tab that holds path with a
// freshly read package: same tab id, name and stack position, empty history
// (a reload clears undo the way reopening a file does) and a clean dirty flag.
// The active tab is left alone — an external edit to one file must not yank the
// user off the tab they are working on.
//
// force permits discarding dirty document state only after the caller has an
// explicit user decision. An in-flight edit or save is always rejected because
// replacing that session would otherwise lose work or let a completion update a
// different session.
//
// Reports ok=false when no tab holds that path any more (the tab was closed, or
// the document was saved somewhere else), which tells the caller to stop
// watching it.
func (w *Workspace) reloadDocument(path string, doc *domain.Document, bitmaps map[string]*render.Bitmap, force bool) (Snapshot, string, bool, error) {
	if doc == nil {
		return Snapshot{}, "", false, fmt.Errorf("文档为空，无法重载")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.tabs {
		tab := &w.tabs[i]
		sess := w.sessions[tab.ID]
		if sess == nil || !sameDocumentPath(sess.path, path) {
			continue
		}
		// `force` discards edits only. It must not permit the disk package to
		// change the ID of a live tab because CanvasSurface and /render/{id}.png
		// use that identity to decide which document is safe to display.
		if doc.DocumentID != tab.ID {
			return Snapshot{}, "", false, fmt.Errorf("%w: 标签 %s，磁盘项目 %s", errDocumentIDConflict, tab.ID, doc.DocumentID)
		}
		if sess.stroke != nil || sess.filter != nil || sess.heal != nil || sess.saving {
			return Snapshot{}, "", false, errDocumentBusy
		}
		if !force && tab.Dirty {
			return Snapshot{}, "", false, errDocumentDirty
		}
		// The new session's rev continues from the old one: the frontend keys the
		// canvas image on (docId, rev) — and docId is deliberately preserved by the
		// agent contract — so a reload that restarted at rev 0 would leave a tab
		// that had never been edited showing the old pixels.
		rev := sess.rev + 1
		reloaded := newSession(path, doc, bitmaps)
		reloaded.rev = rev
		w.sessions[tab.ID] = reloaded
		tab.Name = displayName(path)
		tab.Width, tab.Height = doc.Width, doc.Height
		if doc.Resolution != nil {
			tab.Resolution = *doc.Resolution
		}
		tab.Dirty = false
		return w.snapshotLocked(), tab.ID, true, nil
	}
	return w.snapshotLocked(), "", false, nil
}

// TabState reports one tab's package path and dirty flag. ok is false when the
// tab is gone, which the external-change path uses to retire a watcher.
func (w *Workspace) TabState(id string) (path string, dirty bool, ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.tabs {
		if w.tabs[i].ID != id {
			continue
		}
		sess := w.sessions[id]
		if sess == nil {
			return "", false, false
		}
		return sess.path, w.tabs[i].Dirty, true
	}
	return "", false, false
}

// ApplyMemoryPressure shrinks everything rebuildable in every open session and
// reports how many render caches it dropped (diagnostics only).
//
// It only touches caches and the undo history: documents and their bitmap
// library are the user's work and are never evicted here. The history keeps the
// newest steps — the oldest undo is what goes, exactly like the macOS byte
// budget does.
//
// dropRenderCache is the caller's call because the levels differ: at moderate
// pressure the last composed frame stays (recomposing is exactly the expensive
// thing we are trying to avoid while memory is tight — the macOS GPU canvas
// keeps its last frame the same way), and only a critical machine gives it up.
//
// Note for whoever installs a RetainedBytes estimator: trim would then measure
// snapshot bytes inside this write lock. Keep that estimator cheap or move the
// measurement outside the lock first.
func (w *Workspace) ApplyMemoryPressure(entryLimit int, retainedBytes int64, dropRenderCache bool) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	dropped := 0
	for _, sess := range w.sessions {
		if dropRenderCache && sess.renderPNG != nil {
			sess.renderPNG = nil
			sess.renderRev = 0
			sess.renderFilterRev = 0
			dropped++
		}
		if sess.doc != nil {
			sess.hist.SetLimits(entryLimit, retainedBytes, *sess.doc)
		}
	}
	return dropped
}

// TabBusy reports whether one tab has an edit in flight — a brush stroke being
// dragged or an open filter session. The external-change path must not swap the
// session out from under those: the stroke would then land on a document nobody
// is showing (and its commit would fail on a session that no longer exists).
func (w *Workspace) TabBusy(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.sessions[id]
	if sess == nil {
		return false
	}
	return sess.stroke != nil || sess.filter != nil || sess.heal != nil || sess.saving
}

// TabIDForPath finds the tab holding a package path, if any (one tab per file).
func (w *Workspace) TabIDForPath(path string) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.tabs {
		if sess := w.sessions[w.tabs[i].ID]; sess != nil && sameDocumentPath(sess.path, path) {
			return w.tabs[i].ID, true
		}
	}
	return "", false
}

// Snapshot returns the current state (safe copy).
func (w *Workspace) Snapshot() Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.snapshotLocked()
}

// ActiveDocumentJSON marshals the active tab's document envelope:
// {"rev":N,"doc":{...}} — doc is null (rev 0) when nothing is open.
func (w *Workspace) ActiveDocumentJSON() (string, error) {
	return w.DocumentJSON(w.activeID())
}

// DocumentJSON marshals one tab's document envelope. The external-change path
// needs it for a background tab, not only for the active one.
func (w *Workspace) DocumentJSON(id string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.sessions[id]
	if sess == nil {
		return marshalEnvelope(0, nil)
	}
	return marshalEnvelope(sess.rev, sess.doc)
}

// activeID reports the active tab's id ("" when the workspace is empty).
func (w *Workspace) activeID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active
}

// ActiveRev is the active tab's revision, 0 when nothing is open.
func (w *Workspace) ActiveRev() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if sess := w.activeSessionLocked(); sess != nil {
		return sess.rev
	}
	return 0
}

// ActiveSaveInfo reports the active tab's package path ("" before the
// first save) and its display name, for the save dialog defaults.
func (w *Workspace) ActiveSaveInfo() (path, name string, err error) {
	_, path, name, err = w.activeSaveTarget()
	return path, name, err
}

// activeSaveTarget captures the initiating tab together with its dialog defaults.
// A native picker may outlive the tab selection that opened it.
func (w *Workspace) activeSaveTarget() (id, path, name string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", "", "", errNoDocument
	}
	if t := w.activeTabLocked(); t != nil {
		name = t.Name
	}
	return w.active, sess.path, name, nil
}

// EditActive runs one named document command against the active session,
// wrapped in history.Begin/End. fn must validate before mutating; if it
// returns an error the working copy is rolled back and no entry is
// recorded. When the command changed the document (history committed an
// entry) the revision bumps and the tab turns dirty. Returns the document
// envelope JSON.
// syncTabSizeLocked mirrors the session document's canvas geometry onto the tab
// descriptor. The workspace snapshot is what the frontend uses for the canvas
// box (CanvasSurface 的 .doc 尺寸)、状态栏尺寸、以及裁剪覆盖层的起始框，所以任何
// 会改变文档尺寸的命令（裁剪 / 画布大小 / 图像大小 / 修剪，以及它们的撤销重做）
// 都必须同步它 —— 否则界面继续按旧尺寸画框，而 /render 已经按新尺寸出图，
// 表现为"提交裁剪后画布和状态栏纹丝不动"。
func (w *Workspace) syncTabSizeLocked(id string, doc *domain.Document) {
	if doc == nil {
		return
	}
	for i := range w.tabs {
		if w.tabs[i].ID != id {
			continue
		}
		w.tabs[i].Width = doc.Width
		w.tabs[i].Height = doc.Height
		if doc.Resolution != nil {
			w.tabs[i].Resolution = *doc.Resolution
		}
		return
	}
}

func (w *Workspace) EditActive(name string, fn func(*session) error) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	before := cloneDocForEdit(sess.doc)
	revBefore := sess.hist.Revision()
	sess.hist.Begin(name, *sess.doc, sess.doc.ActiveLayerID)
	err := fn(sess)
	if err != nil {
		sess.doc = before
	}
	sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
	if err != nil {
		return "", err
	}
	if sess.hist.Revision() != revBefore {
		sess.rev++
		w.setTabDirtyLocked(w.active, true)
	}
	w.syncTabSizeLocked(w.active, sess.doc)
	return marshalEnvelope(sess.rev, sess.doc)
}

// UndoActive rolls the active document back one history entry and returns
// the envelope. Undo is itself an observable change, so rev bumps. Filter
// commits restore their pixels through the raster journal at the revision
// being left.
func (w *Workspace) UndoActive() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	leaving := sess.hist.Revision()
	snap, ok := sess.hist.Undo()
	if !ok {
		return "", fmt.Errorf("没有可撤销的操作")
	}
	doc := snap.Document
	doc.ActiveLayerID = snap.ActiveLayerID
	sess.doc = &doc
	sess.applyRasterJournal(leaving, false)
	sess.rev++
	w.setTabDirtyLocked(w.active, sess.path == "" || sess.hist.IsModified())
	w.syncTabSizeLocked(w.active, sess.doc)
	return marshalEnvelope(sess.rev, sess.doc)
}

// RedoActive replays the newest undone entry.
func (w *Workspace) RedoActive() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	snap, ok := sess.hist.Redo()
	if !ok {
		return "", fmt.Errorf("没有可重做的操作")
	}
	doc := snap.Document
	doc.ActiveLayerID = snap.ActiveLayerID
	sess.doc = &doc
	sess.applyRasterJournal(snap.Revision, true)
	sess.rev++
	w.setTabDirtyLocked(w.active, sess.path == "" || sess.hist.IsModified())
	w.syncTabSizeLocked(w.active, sess.doc)
	return marshalEnvelope(sess.rev, sess.doc)
}

// SaveActive writes the active document to the package at path: every
// referenced asset is encoded from the in-memory bitmap library, the store
// commits the package, and the session records its path and saved state.
// Returns the revision captured for disk, even if editing advanced while writing.
func (w *Workspace) SaveActive(path string) (int, error) {
	return w.saveTab(w.activeID(), path)
}

func (w *Workspace) saveTab(id, path string) (int, error) {
	w.saveMu.Lock()
	defer w.saveMu.Unlock()
	w.mu.Lock()
	sess := w.sessions[id]
	if sess == nil {
		w.mu.Unlock()
		return 0, errNoDocument
	}
	if ownerID, ok := w.pathOwnerLocked(id, path); ok {
		w.mu.Unlock()
		return 0, fmt.Errorf("PATH_CONFLICT: 项目路径已由另一个标签打开: %s (标签 %s)", path, ownerID)
	}
	if ownerID, ok := w.reservedPathOwnerLocked(path); ok {
		w.mu.Unlock()
		return 0, fmt.Errorf("PATH_CONFLICT: 项目路径正在保存，暂时不能覆盖: %s (标签 %s)", path, ownerID)
	}
	if sess.stroke != nil || sess.filter != nil || sess.heal != nil {
		w.mu.Unlock()
		return 0, fmt.Errorf("DOCUMENT_BUSY: 文档有尚未完成的编辑，完成或取消后再保存")
	}
	doc := cloneDocForEdit(sess.doc)
	bitmaps := map[string]*render.Bitmap{}
	if err := alignAssetNames(doc, sess.bitmaps); err != nil {
		w.mu.Unlock()
		return 0, err
	}
	for _, name := range referencedAssetNames(doc) {
		bmp, ok := sess.bitmaps[name]
		if !ok || bmp == nil {
			w.mu.Unlock()
			return 0, fmt.Errorf("内存位图库缺少资产 %s，无法保存", name)
		}
		bitmaps[name] = bmp.Clone()
	}
	rev, historyRevision := sess.rev, sess.hist.Revision()
	write := w.savePackage
	if write == nil {
		var store project.Store
		write = store.Save
	}
	reservationKey := absoluteCleanPath(path)
	if reservationKey != "" {
		if w.saveReservations == nil {
			w.saveReservations = map[string]string{}
		}
		w.saveReservations[reservationKey] = id
	}
	sess.saving = true
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		if reservationKey != "" && w.saveReservations[reservationKey] == id {
			delete(w.saveReservations, reservationKey)
		}
		sess.saving = false
		w.mu.Unlock()
	}()

	assets := map[string][]byte{}
	for name, bmp := range bitmaps {
		data, err := encodeAsset(name, bmp)
		if err != nil {
			return 0, fmt.Errorf("编码资产 %s 失败: %w", name, err)
		}
		assets[name] = data
	}
	if err := write(path, doc, assets); err != nil {
		return 0, err
	}
	w.mu.Lock()
	// A close or an explicit external reload may have retired this session.
	// The package still saved successfully, but its completion cannot mark a
	// different live session clean or change which tab is selected.
	if w.sessions[id] == sess {
		sess.path = path
		sess.hist.MarkSavedRevision(historyRevision)
		w.setTabDirtyLocked(id, sess.hist.IsModified())
		w.markTabSavedLocked(id, path)
	}
	w.mu.Unlock()
	return rev, nil
}

// BeginStroke starts a default-settings brush stroke (size 30, hardness
// 0.5, black, opacity 1) on the active layer's bitmap.
func (w *Workspace) BeginStroke(x, y float64) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	if sess.stroke != nil {
		return "", fmt.Errorf("已有进行中的笔刷")
	}
	if sess.doc.ActiveLayerID == nil {
		return "", fmt.Errorf("没有活动图层")
	}
	idx := findLayerIndex(sess.doc, *sess.doc.ActiveLayerID)
	if idx < 0 {
		return "", fmt.Errorf("活动图层不存在: %s", *sess.doc.ActiveLayerID)
	}
	l := &sess.doc.Layers[idx]
	if l.IsGroupLayer() {
		return "", fmt.Errorf("编组不能绘制")
	}
	if l.ImageFile == nil {
		return "", fmt.Errorf("图层 %s 没有位图，无法绘制", l.ID)
	}
	base, ok := sess.bitmaps[*l.ImageFile]
	if !ok || base == nil {
		return "", fmt.Errorf("内存位图库缺少资产 %s", *l.ImageFile)
	}
	st, err := render.NewStroke(render.TileGridFromBitmap(base), defaultBrushSettings())
	if err != nil {
		return "", err
	}
	st.Append(render.Point{X: x, Y: y})
	sess.stroke = st
	sess.strokeKey = *l.ImageFile
	return marshalEnvelope(sess.rev, sess.doc)
}

// StrokePoint extends the in-flight stroke. The manifest is untouched, so
// rev stays put; the canvas updates through /render after EndStroke.
func (w *Workspace) StrokePoint(x, y float64) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	if sess.stroke == nil {
		return "", fmt.Errorf("没有进行中的笔刷")
	}
	sess.stroke.Append(render.Point{X: x, Y: y})
	return marshalEnvelope(sess.rev, sess.doc)
}

// EndStroke commits the stroke back into the bitmap library and bumps rev
// so the render cache invalidates.
//
// A stroke changes pixels, not the document model, so it needs the same
// treatment as a filter commit: the history transaction is closed with
// EndForced (End would drop it as a no-op — the document JSON is identical)
// and the before/after bitmaps go into the raster journal so undo and redo
// restore the pixels. Without that pair the stroke was silently
// un-undoable: Ctrl+Z found an empty stack and did nothing while the paint
// stayed on the canvas.
func (w *Workspace) EndStroke() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	if sess.stroke == nil {
		return "", fmt.Errorf("没有进行中的笔刷")
	}
	grid := sess.stroke.Commit()
	sess.stroke = nil
	key := sess.strokeKey
	if key == "" {
		return "", fmt.Errorf("笔刷没有目标资产")
	}
	sess.strokeKey = ""
	before := sess.bitmaps[key]
	if before == nil {
		return "", fmt.Errorf("内存位图库缺少资产 %s", key)
	}
	after := grid.Bitmap()
	// A stroke whose commit reproduces the previous pixels byte for byte must
	// not push an entry, bump rev or dirty the tab. (BeginStroke always appends
	// a dab, so in practice this catches a dab that landed fully outside the
	// layer or at zero coverage.)
	if before.W == after.W && before.H == after.H && bytes.Equal(before.Pix, after.Pix) {
		return marshalEnvelope(sess.rev, sess.doc)
	}
	// A 16-bit or HDR layer keeps its full-precision samples: only the pixels
	// the stroke actually changed become the stroke's 8-bit values, and the
	// rest of the layer stays at its own depth (upstream DeepLayer.flattened
	// and deep_take_painted). Without this a single dab would flatten the
	// layer to 8 bits and Save would lose the highlights.
	takePainted(after, before)
	sess.hist.Begin("画笔", *sess.doc, sess.doc.ActiveLayerID)
	sess.bitmaps[key] = after
	sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
	sess.journalRasters(sess.hist.Revision(),
		map[string]*render.Bitmap{key: before},
		map[string]*render.Bitmap{key: after})
	sess.rev++
	w.setTabDirtyLocked(w.active, true)
	return marshalEnvelope(sess.rev, sess.doc)
}

// RenderPNG composes the document identified by tab ID or domain
// documentID into PNG bytes, served from the per-session cache until rev
// (or the filter preview revision) changes. ok=false means no such
// document. While a filter dialog is open the composite shows that
// session's preview bitmap and grown transform in place of the layer's
// stored pixels.
func (w *Workspace) RenderPNG(docID string) (png []byte, ok bool, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.sessionByIDLocked(docID)
	if sess == nil {
		return nil, false, nil
	}
	filterRev := 0
	if sess.filter != nil {
		filterRev = sess.filter.previewRev
	}
	if sess.renderPNG == nil || sess.renderRev != sess.rev || sess.renderFilterRev != filterRev {
		doc, source := sess.doc, sess.pixelSource()
		if sess.filter != nil {
			doc, source = sess.filterRenderSource()
		}
		bmp, err := render.Render(doc, source)
		if err != nil {
			return nil, true, err
		}
		data, err := render.EncodePNG(bmp)
		if err != nil {
			return nil, true, err
		}
		sess.renderPNG = data
		sess.renderRev = sess.rev
		sess.renderFilterRev = filterRev
	}
	return sess.renderPNG, true, nil
}

func (w *Workspace) activeSessionLocked() *session {
	if w.active == "" {
		return nil
	}
	return w.sessions[w.active]
}

func (w *Workspace) activeTabLocked() *Document {
	for i := range w.tabs {
		if w.tabs[i].ID == w.active {
			return &w.tabs[i]
		}
	}
	return nil
}

// sessionByIDLocked resolves a render URL's docID: tab ID first, then the
// domain documentID (both are unique identifier namespaces).
func (w *Workspace) sessionByIDLocked(id string) *session {
	if sess, ok := w.sessions[id]; ok {
		return sess
	}
	for _, t := range w.tabs {
		if sess := w.sessions[t.ID]; sess != nil && sess.doc.DocumentID == id {
			return sess
		}
	}
	return nil
}

func (w *Workspace) setTabDirtyLocked(id string, dirty bool) {
	for i := range w.tabs {
		if w.tabs[i].ID == id {
			w.tabs[i].Dirty = dirty
			return
		}
	}
}

// markTabSavedLocked renames the tab after the saved package. Dirty state is
// decided from the captured history revision, independently of the name.
func (w *Workspace) markTabSavedLocked(id, path string) {
	for i := range w.tabs {
		if w.tabs[i].ID == id {
			w.tabs[i].Name = displayName(path)
			return
		}
	}
}

// pathOwnerLocked enforces the one-tab-per-package invariant before a save
// writes anything. Save As must not overwrite a package that another live tab
// owns and then leave both sessions pointing at the same directory.
func (w *Workspace) pathOwnerLocked(id, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	for tabID, sess := range w.sessions {
		if tabID == id || sess == nil || sess.path == "" {
			continue
		}
		if sameDocumentPath(sess.path, path) {
			return tabID, true
		}
	}
	return "", false
}

func (w *Workspace) reservedPathOwnerLocked(path string) (string, bool) {
	for reservedPath, ownerID := range w.saveReservations {
		if sameDocumentPath(reservedPath, path) {
			return ownerID, true
		}
	}
	return "", false
}

func (w *Workspace) hasLocked(id string) bool {
	for _, d := range w.tabs {
		if d.ID == id {
			return true
		}
	}
	return false
}

func (w *Workspace) snapshotLocked() Snapshot {
	tabs := make([]Document, len(w.tabs))
	copy(tabs, w.tabs)
	return Snapshot{Tabs: tabs, ActiveID: w.active}
}

// displayName strips the .comp suffix (any case) from a package path for
// tab display.
func displayName(path string) string {
	base := filepath.Base(path)
	if ext := filepath.Ext(base); strings.EqualFold(ext, ".comp") {
		base = base[:len(base)-len(ext)]
	}
	return base
}

// sameDocumentPath compares package paths using the filesystem's identity
// rules. Picker paths are normally absolute, but normalizing here also covers
// shell/bridge callers that use a relative spelling or different separators.
func sameDocumentPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a = absoluteCleanPath(a)
	b = absoluteCleanPath(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func absoluteCleanPath(path string) string {
	path = filepath.Clean(path)
	if absolute, err := filepath.Abs(path); err == nil {
		return filepath.Clean(absolute)
	}
	return path
}
